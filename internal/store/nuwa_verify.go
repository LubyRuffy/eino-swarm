package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// ErrNuwaVerifyConflict means this tool call already admitted a different
// verification. Replaying the same arguments does not consume another slot.
var ErrNuwaVerifyConflict = errors.New("store: verification call conflicts")

// NuwaVerifyBudgetError is the host's one-verify and one-fix allowance.
// The model can correct its next spawn; it is not a turn failure.
type NuwaVerifyBudgetError struct {
	Intent string
}

func (e *NuwaVerifyBudgetError) Error() string {
	return fmt.Sprintf("store: verification budget exhausted for %s", e.Intent)
}

func (e *NuwaVerifyBudgetError) Unwrap() error { return errNuwaVerifyBudget }

var errNuwaVerifyBudget = errors.New("store: verification budget exhausted")

// NuwaVerifyAdmission is one durable verify or fix of a shared record in a turn.
type NuwaVerifyAdmission struct {
	ProjectID  string
	ThreadID   string
	TurnID     string
	AgentID    string
	Generation int
	CallID     string
	Intent     string
	TargetID   string
}

// NuwaVerifySlot is the committed receipt. Restart reads these rows; it does
// not reset the allowance. The same tool-call key returns without a new row.
type NuwaVerifySlot struct {
	ID          uint   `gorm:"primaryKey"`
	ProjectID   string `gorm:"index;size:64"`
	ThreadID    string `gorm:"index;size:64"`
	TurnID      string `gorm:"uniqueIndex:idx_nuwa_verify_budget,priority:1;size:64"`
	TargetID    string `gorm:"uniqueIndex:idx_nuwa_verify_budget,priority:2;size:64"`
	Intent      string `gorm:"uniqueIndex:idx_nuwa_verify_budget,priority:3;size:16"`
	RequestKey  string `gorm:"uniqueIndex;size:64"`
	RequestHash string `gorm:"size:64"`
	CreatedAt   int64
}

// AdmitNuwaVerification records one verify or fix for this turn and record.
// A repeated tool call with the same arguments is the same admission. A second
// distinct call for the same intent is refused and writes nothing.
func (s *Store) AdmitNuwaVerification(ctx context.Context, in NuwaVerifyAdmission) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	in.Intent = strings.ToLower(strings.TrimSpace(in.Intent))
	in.TargetID = strings.TrimSpace(in.TargetID)
	in.CallID = strings.TrimSpace(in.CallID)
	if in.Intent != "verify" && in.Intent != "fix" {
		return invalidNuwa("verification intent must be verify or fix")
	}
	if in.ProjectID == "" || in.ThreadID == "" || in.TurnID == "" || in.AgentID == "" || in.Generation < 0 || in.CallID == "" || in.TargetID == "" {
		return invalidNuwa("verification requires project, turn, caller, tool call and target")
	}
	keyRaw := strings.Join([]string{in.ThreadID, in.TurnID, in.AgentID, strconv.Itoa(in.Generation), in.CallID}, "\x00")
	hashRaw := in.Intent + "\x00" + in.TargetID
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(keyRaw)))
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(hashRaw)))
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing NuwaVerifySlot
		err := tx.Where("request_key = ?", key).Take(&existing).Error
		if err == nil {
			if existing.RequestHash != hash {
				return ErrNuwaVerifyConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var node NuwaNode
		err = tx.Select("id").Where("id = ? AND project_id = ?", in.TargetID, in.ProjectID).Take(&node).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := validateNuwaOwner(tx, NuwaNode{ProjectID: in.ProjectID, ThreadID: in.ThreadID, TurnID: in.TurnID, AgentID: in.AgentID, Generation: in.Generation, Title: "verification"}, false); err != nil {
			return err
		}
		var turn Turn
		if err := tx.Select("status").First(&turn, "id = ?", in.TurnID).Error; err != nil {
			return err
		}
		if turn.Status != TurnRunning {
			return invalidNuwa("verification turn is no longer running")
		}
		var used int64
		if err := tx.Model(&NuwaVerifySlot{}).Where("turn_id = ? AND target_id = ? AND intent = ?", in.TurnID, in.TargetID, in.Intent).Count(&used).Error; err != nil {
			return err
		}
		if used >= 1 {
			return &NuwaVerifyBudgetError{Intent: in.Intent}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return tx.Create(&NuwaVerifySlot{
			ProjectID: in.ProjectID, ThreadID: in.ThreadID, TurnID: in.TurnID,
			TargetID: in.TargetID, Intent: in.Intent, RequestKey: key, RequestHash: hash,
		}).Error
	})
}

func deleteNuwaVerifySlots(tx *gorm.DB, projectID, threadID string, turnIDs []string) error {
	q := tx
	switch {
	case projectID != "":
		q = q.Where("project_id = ?", projectID)
	case len(turnIDs) > 0:
		q = q.Where("turn_id IN ?", turnIDs)
	case threadID != "":
		q = q.Where("thread_id = ?", threadID)
	default:
		return nil
	}
	return q.Delete(&NuwaVerifySlot{}).Error
}
