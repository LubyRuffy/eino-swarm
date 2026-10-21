package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	WakeDecisionDeny      = "deny"
	WakeDecisionAllow     = "allow"
	WakeDecisionUnchanged = "unchanged"
)

var (
	ErrThreadWakeDenied     = errors.New("store: the human prohibited suspending this conversation")
	ErrWakePolicyPending    = errors.New("store: human wake policy has unclassified input")
	ErrWakeDecisionConflict = errors.New("store: the human wake decision is stale or already classified differently")
)

// ThreadWakeDecision caches a semantic decision about one effective human event.
// The event identity and digest prevent a revised or rewound input from inheriting
// an old decision. Neither compact summaries nor assistant messages grant consent.
type ThreadWakeDecision struct {
	ThreadID    string    `gorm:"primaryKey;size:64" json:"thread_id"`
	EventSeq    int64     `gorm:"primaryKey;autoIncrement:false" json:"event_seq"`
	EventID     uint      `json:"event_id"`
	InputDigest string    `gorm:"size:64" json:"input_digest"`
	Decision    string    `gorm:"size:16" json:"decision"`
	Quote       string    `json:"quote"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WakePolicyInput is original human text with unread-steer edits applied.
// Decision and Quote are cached output, never authority supplied by a caller.
type WakePolicyInput struct {
	Seq      int64  `json:"seq"`
	Text     string `json:"text"`
	Kind     string `json:"kind"`
	Decision string `json:"decision,omitempty"`
	Quote    string `json:"quote,omitempty"`
}

type ThreadWakePolicy struct {
	Denied     bool   `json:"denied"`
	SourceSeq  int64  `json:"source_seq,omitempty"`
	SourceText string `json:"source_text,omitempty"`
	Quote      string `json:"quote,omitempty"`
	PendingSeq int64  `json:"pending_seq,omitempty"`
}

type ThreadWakeDeniedError struct{ Policy ThreadWakePolicy }

func (e *ThreadWakeDeniedError) Error() string {
	return fmt.Sprintf("%s (human event %d)", ErrThreadWakeDenied, e.Policy.SourceSeq)
}
func (e *ThreadWakeDeniedError) Unwrap() error { return ErrThreadWakeDenied }

type WakePolicyPendingError struct{ Seq int64 }

func (e *WakePolicyPendingError) Error() string {
	return fmt.Sprintf("%s (human event %d)", ErrWakePolicyPending, e.Seq)
}
func (e *WakePolicyPendingError) Unwrap() error { return ErrWakePolicyPending }

func wakeInputDigest(input WakePolicyInput) string {
	digest := sha256.Sum256([]byte(input.Kind + "\x00" + input.Text))
	return hex.EncodeToString(digest[:])
}

// effectiveWakeInputs deliberately reads the event log rather than compacted
// model messages. A generated scheduled prompt cannot become a human decision.
func effectiveWakeInputs(tx *gorm.DB, threadID string) ([]WakePolicyInput, map[int64]uint, error) {
	var events []Event
	if err := tx.Where("thread_id = ? AND kind IN ? AND COALESCE(source_thread_id, '') = '' AND COALESCE(source_turn_id, '') = '' AND COALESCE(role, '') = ''", threadID,
		[]string{"user_message", "steer", "steer_revised", "steer_retracted"}).Order("seq ASC").Find(&events).Error; err != nil {
		return nil, nil, err
	}
	inputs := make([]WakePolicyInput, 0)
	ids := make(map[int64]uint)
	positions := make(map[int64]int)
	retracted := make(map[int64]bool)
	for _, ev := range events {
		switch ev.Kind {
		case "user_message", "steer":
			positions[ev.Seq] = len(inputs)
			ids[ev.Seq] = ev.ID
			inputs = append(inputs, WakePolicyInput{Seq: ev.Seq, Text: ev.Text, Kind: ev.Kind})
		case "steer_revised":
			var revision struct {
				Seq  int64   `json:"seq"`
				Text *string `json:"text"`
			}
			if json.Unmarshal([]byte(ev.Text), &revision) != nil || revision.Text == nil {
				continue
			}
			i, ok := positions[revision.Seq]
			if ok && inputs[i].Kind == "steer" && !retracted[revision.Seq] {
				inputs[i].Text = *revision.Text
			}
		case "steer_retracted":
			var removal struct {
				Seq int64 `json:"seq"`
			}
			if json.Unmarshal([]byte(ev.Text), &removal) != nil {
				removal.Seq, _ = strconv.ParseInt(strings.TrimSpace(ev.Text), 10, 64)
			}
			if i, ok := positions[removal.Seq]; ok && inputs[i].Kind == "steer" {
				retracted[removal.Seq] = true
			}
		}
	}
	effective := inputs[:0]
	for _, input := range inputs {
		if !retracted[input.Seq] && strings.TrimSpace(input.Text) != "" {
			effective = append(effective, input)
		}
	}
	return effective, ids, nil
}

func readThreadWakePolicy(tx *gorm.DB, threadID string) (ThreadWakePolicy, []WakePolicyInput, error) {
	var policy ThreadWakePolicy
	var thread Thread
	if err := tx.Select("id").First(&thread, "id = ?", threadID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = ErrNotFound
		}
		return policy, nil, err
	}
	inputs, ids, err := effectiveWakeInputs(tx, threadID)
	if err != nil {
		return policy, nil, err
	}
	var decisions []ThreadWakeDecision
	if err := tx.Where("thread_id = ?", threadID).Find(&decisions).Error; err != nil {
		return policy, nil, err
	}
	bySeq := make(map[int64]ThreadWakeDecision, len(decisions))
	for _, decision := range decisions {
		bySeq[decision.EventSeq] = decision
	}
	for i, input := range inputs {
		decision, ok := bySeq[input.Seq]
		valid := ok && decision.EventID == ids[input.Seq] && decision.InputDigest == wakeInputDigest(input)
		if valid {
			switch decision.Decision {
			case WakeDecisionDeny, WakeDecisionAllow:
				valid = strings.TrimSpace(decision.Quote) != "" && strings.Contains(input.Text, decision.Quote)
			case WakeDecisionUnchanged:
				valid = decision.Quote == ""
			default:
				valid = false
			}
		}
		if !valid {
			if policy.PendingSeq == 0 {
				policy.PendingSeq = input.Seq
			}
			continue
		}
		inputs[i].Decision, inputs[i].Quote = decision.Decision, decision.Quote
		if decision.Decision != WakeDecisionUnchanged {
			policy.Denied = decision.Decision == WakeDecisionDeny
			policy.SourceSeq, policy.SourceText, policy.Quote = input.Seq, input.Text, decision.Quote
		}
	}
	return policy, inputs, nil
}

func (s *Store) WakePolicyInputs(threadID string) ([]WakePolicyInput, error) {
	_, inputs, err := s.ReadThreadWakePolicy(threadID)
	return inputs, err
}

// ReadThreadWakePolicy returns pending inputs alongside the last classified
// human decision. Pending input is fail-closed at the actual schedule write.
func (s *Store) ReadThreadWakePolicy(threadID string) (ThreadWakePolicy, []WakePolicyInput, error) {
	var policy ThreadWakePolicy
	var inputs []WakePolicyInput
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		policy, inputs, err = readThreadWakePolicy(tx, threadID)
		return err
	})
	return policy, inputs, err
}

// SaveWakeDecision accepts a classifier result only while its exact original
// human input is current. Reclassifying identical text cannot release a denial.
func (s *Store) SaveWakeDecision(threadID string, input WakePolicyInput, decision, quote string) error {
	switch decision {
	case WakeDecisionDeny, WakeDecisionAllow:
		if strings.TrimSpace(quote) == "" || !strings.Contains(input.Text, quote) {
			return fmt.Errorf("store: wake decision requires an exact nonempty human quote")
		}
	case WakeDecisionUnchanged:
		if quote != "" {
			return fmt.Errorf("store: unchanged wake decision must not quote a permission")
		}
	default:
		return fmt.Errorf("store: invalid wake decision %q", decision)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		inputs, ids, err := effectiveWakeInputs(tx, threadID)
		if err != nil {
			return err
		}
		for _, current := range inputs {
			if current.Seq != input.Seq || current.Text != input.Text || current.Kind != input.Kind {
				continue
			}
			digest := wakeInputDigest(current)
			var cached ThreadWakeDecision
			err := tx.Where("thread_id = ? AND event_seq = ?", threadID, current.Seq).First(&cached).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && cached.EventID == ids[current.Seq] && cached.InputDigest == digest {
				if cached.Decision != decision || cached.Quote != quote {
					return ErrWakeDecisionConflict
				}
				return nil
			}
			row := ThreadWakeDecision{ThreadID: threadID, EventSeq: current.Seq, EventID: ids[current.Seq], InputDigest: digest, Decision: decision, Quote: quote}
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "thread_id"}, {Name: "event_seq"}},
				DoUpdates: clause.AssignmentColumns([]string{"event_id", "input_digest", "decision", "quote", "updated_at"})}).Create(&row).Error
		}
		return ErrWakeDecisionConflict
	})
}

func checkThreadWakePolicy(tx *gorm.DB, threadID string) error {
	policy, _, err := readThreadWakePolicy(tx, threadID)
	if err != nil {
		return err
	}
	if policy.PendingSeq != 0 {
		return &WakePolicyPendingError{Seq: policy.PendingSeq}
	}
	if policy.Denied {
		return &ThreadWakeDeniedError{Policy: policy}
	}
	return nil
}
