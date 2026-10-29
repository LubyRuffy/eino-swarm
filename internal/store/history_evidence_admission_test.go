package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func historyReadEvidence(t *testing.T, f historyEvidenceFixture) *HistoryEvidence {
	t.Helper()
	sources, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs(), 20000)
	if err != nil {
		t.Fatal(err)
	}
	return &HistoryEvidence{ProjectID: f.project.ID, Sources: sources}
}

func historyPendingAnswer(t *testing.T, f historyEvidenceFixture) PendingScheduleAnswer {
	t.Helper()
	p := PendingScheduleAnswer{ThreadID: f.recipient.ID, TurnID: f.turn.ID, Kind: "answer", Payload: `{"private":"candidate"}`}
	if err := f.s.SavePendingScheduleAnswer(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHistoryAnswerAdmissionAtomicallyPublishesAndKeepsAcceptedIdentity(t *testing.T) {
	for _, failure := range []string{"", "events", "messages", "pending_schedule_answers"} {
		t.Run(failure, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			evidence := historyReadEvidence(t, f)
			candidate := historyPendingAnswer(t, f)
			if failure != "" {
				op := "INSERT"
				if failure == "pending_schedule_answers" {
					op = "UPDATE"
				}
				if err := f.s.DB().Exec("CREATE TRIGGER refuse_bound_answer BEFORE " + op + " ON " + failure + " BEGIN SELECT RAISE(ABORT,'fixture atomic admission failure'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			answer := []Event{{Kind: "agent_message", AgentID: "manager", Text: "Complete bound source report\x00"}}
			err := f.s.AdmitScheduleAnswerWithHistory(f.recipient.ID, f.turn.ID, nil, answer, evidence, candidate.ID)
			stored, getErr := f.s.GetPendingScheduleAnswer(candidate.ID)
			if getErr != nil {
				t.Fatal(getErr)
			}
			if failure != "" {
				if err == nil || stored.State != "pending" {
					t.Fatalf("failed transaction admitted candidate: %v %#v", err, stored)
				}
				assertNoAdmittedAnswer(t, f.s, f.recipient.ID)
				return
			}
			if err != nil || stored.State != "accepted" {
				t.Fatalf("valid original source rejected: %v %#v", err, stored)
			}
			events, err := f.s.ListTurnEvents(f.turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			messages, err := f.s.ListMessages(f.recipient.ID)
			if err != nil || len(events) != 1 || len(messages) != 1 || events[0].Text != answer[0].Text || messages[0].Content != answer[0].Text {
				t.Fatalf("public event/replay did not commit together: %#v %#v %v", events, messages, err)
			}
			if err := f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("content", "source changed after accepted commit").Error; err != nil {
				t.Fatal(err)
			}
			if err := f.s.AdmitScheduleAnswerWithHistory(f.recipient.ID, f.turn.ID, nil, answer, evidence, candidate.ID); !errors.Is(err, ErrScheduleAnswerAlreadyAdmitted) {
				t.Fatalf("accepted identity revalidated/replayed instead of remaining idempotent: %v", err)
			}
			events, err = f.s.ListTurnEvents(f.turn.ID)
			if err != nil || len(events) != 1 {
				t.Fatalf("accepted report was published twice: %#v %v", events, err)
			}
		})
	}
}

func TestHistoryAnswerAdmissionRejectsEveryChangedCanonicalField(t *testing.T) {
	mutations := map[string]func(historyEvidenceFixture) error{
		"agent": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("agent_id", "other-agent").Error
		},
		"images": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("images", `[{"id":"replacement","mime":"image/png"}]`).Error
		},
		"timestamp": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("created_at", f.message.CreatedAt.Add(time.Second)).Error
		},
		"nul": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("content", strings.TrimSuffix(f.message.Content, "\x00")).Error
		},
		"whitespace": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("content", f.message.Content+" ").Error
		},
		"row-identity": func(f historyEvidenceFixture) error {
			if err := f.s.DB().Delete(&f.message).Error; err != nil {
				return err
			}
			copy := f.message
			copy.ID = f.message.ID + 100
			return f.s.DB().Create(&copy).Error
		},
		"project-deleted": func(f historyEvidenceFixture) error { return f.s.DB().Delete(&f.project).Error },
		"event-ambiguous": func(f historyEvidenceFixture) error {
			copy := f.event
			copy.ID = 0
			return f.s.DB().Create(&copy).Error
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			evidence := historyReadEvidence(t, f)
			if err := mutate(f); err != nil {
				t.Fatal(err)
			}
			if err := f.s.AdmitScheduleAnswerWithHistory(f.recipient.ID, f.turn.ID, nil, admissionAnswer(), evidence); !errors.Is(err, ErrHistoryEvidenceChanged) {
				t.Fatalf("changed canonical field was admitted: %v", err)
			}
			assertNoAdmittedAnswer(t, f.s, f.recipient.ID)
		})
	}
}

// A separate WAL writer can change the original after verification while this
// transaction holds its read snapshot. SQLite must refuse the stale snapshot's
// promotion to a writer; candidate acceptance must roll back with publication.
func TestHistoryAnswerAdmissionRejectsAConcurrentExternalSourceChange(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	evidence := historyReadEvidence(t, f)
	candidate := historyPendingAnswer(t, f)
	other, err := sql.Open("sqlite", f.path+"?_pragma=busy_timeout(2000)")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	if err := f.s.DB().Callback().Create().Before("gorm:create").Register("history_source_commit_barrier", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	admitted := make(chan error, 1)
	go func() {
		admitted <- f.s.AdmitScheduleAnswerWithHistory(f.recipient.ID, f.turn.ID, nil, admissionAnswer(), evidence, candidate.ID)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		<-admitted
		t.Fatal("answer did not reach the actual post-verification transaction barrier")
	}
	_, updateErr := other.Exec("UPDATE messages SET content=? WHERE id=?", "external changed original", f.message.ID)
	close(release)
	admitErr := <-admitted
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	var sqliteError interface{ Code() int }
	if !errors.As(admitErr, &sqliteError) || sqliteError.Code() != 517 { // SQLITE_BUSY_SNAPSHOT
		t.Fatalf("stale WAL read snapshot was published: %v", admitErr)
	}
	assertNoAdmittedAnswer(t, f.s, f.recipient.ID)
	stored, err := f.s.GetPendingScheduleAnswer(candidate.ID)
	if err != nil || stored.State != "pending" {
		t.Fatalf("failed publication accepted private candidate: %#v %v", stored, err)
	}
	if err := f.s.AdmitScheduleAnswerWithHistory(f.recipient.ID, f.turn.ID, nil, admissionAnswer(), evidence, candidate.ID); !errors.Is(err, ErrHistoryEvidenceChanged) {
		t.Fatalf("next transaction did not reject changed source: %v", err)
	}
}

func TestHistoryAnswerAdmissionSerializesSameStoreSourceWrites(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	evidence := historyReadEvidence(t, f)
	entered, release := make(chan struct{}), make(chan struct{})
	if err := f.s.DB().Callback().Create().Before("gorm:create").Register("history_same_store_barrier", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	admitted, changed := make(chan error, 1), make(chan error, 1)
	go func() {
		admitted <- f.s.AdmitScheduleAnswerWithHistory(f.recipient.ID, f.turn.ID, nil, admissionAnswer(), evidence)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		<-admitted
		t.Fatal("admission did not reach public insert")
	}
	go func() {
		changed <- f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("content", "changed after public commit").Error
	}()
	select {
	case err := <-changed:
		close(release)
		<-admitted
		t.Fatalf("same-store source mutation overtook source-bound commit: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	events, err := f.s.ListTurnEvents(f.turn.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("valid commit before later source mutation lost: %#v %v", events, err)
	}
}
