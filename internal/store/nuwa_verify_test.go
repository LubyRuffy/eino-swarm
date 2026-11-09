package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func (f nuwaFixture) verifyAdmission(call, intent string) NuwaVerifyAdmission {
	return NuwaVerifyAdmission{
		ProjectID: f.project.ID, ThreadID: f.thread.ID, TurnID: f.turn.ID,
		AgentID: "manager", Generation: 0, CallID: call, Intent: intent, TargetID: "",
	}
}

func TestNuwaVerifyBudgetAllowsOneVerifyAndOneFixThenSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zwai.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f := nuwaFixture{s: s, project: Project{Name: "collaboration", NuwaEnabled: true}}
	if err := f.s.CreateProject(&f.project); err != nil {
		t.Fatal(err)
	}
	f.thread = Thread{Title: "source", ProjectID: f.project.ID}
	if err := f.s.CreateThread(&f.thread); err != nil {
		t.Fatal(err)
	}
	f.turn = Turn{ThreadID: f.thread.ID}
	if err := f.s.CreateTurn(&f.turn); err != nil {
		t.Fatal(err)
	}
	node := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &node, 0); err != nil {
		t.Fatal(err)
	}
	first := f.verifyAdmission("call-verify", "verify")
	first.TargetID = node.ID
	if err := f.s.AdmitNuwaVerification(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := f.s.AdmitNuwaVerification(context.Background(), first); err != nil {
		t.Fatal("replaying the same verification call consumed another slot")
	}
	second := first
	second.CallID = "call-verify-again"
	var budget *NuwaVerifyBudgetError
	if err := f.s.AdmitNuwaVerification(context.Background(), second); !errors.As(err, &budget) || budget.Intent != "verify" {
		t.Fatalf("second verify err=%v", err)
	}
	fix := first
	fix.CallID = "call-fix"
	fix.Intent = "fix"
	if err := f.s.AdmitNuwaVerification(context.Background(), fix); err != nil {
		t.Fatal(err)
	}
	fixAgain := fix
	fixAgain.CallID = "call-fix-again"
	if err := f.s.AdmitNuwaVerification(context.Background(), fixAgain); !errors.As(err, &budget) || budget.Intent != "fix" {
		t.Fatalf("second fix err=%v", err)
	}
	changed := first
	changed.TargetID = "other-record"
	if err := f.s.AdmitNuwaVerification(context.Background(), changed); !errors.Is(err, ErrNuwaVerifyConflict) {
		t.Fatalf("same call with a different target err=%v", err)
	}
	var slots int64
	if err := f.s.db.Model(&NuwaVerifySlot{}).Count(&slots).Error; err != nil || slots != 2 {
		t.Fatalf("slots=%d err=%v", slots, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	f.s = reopened
	if err := f.s.AdmitNuwaVerification(context.Background(), second); !errors.As(err, &budget) {
		t.Fatalf("restart cleared the verification allowance: %v", err)
	}
	if err := f.s.AdmitNuwaVerification(context.Background(), first); err != nil {
		t.Fatal("restart treated the original call as a new admission")
	}
}

func TestNuwaVerifyBudgetRejectsUnknownCancelledAndRemovedTurns(t *testing.T) {
	f := newNuwaFixture(t)
	node := f.node("task")
	if err := f.s.UpsertNuwaNode(context.Background(), &node, 0); err != nil {
		t.Fatal(err)
	}
	missing := f.verifyAdmission("missing-target", "verify")
	missing.TargetID = "nw_missing"
	if err := f.s.AdmitNuwaVerification(context.Background(), missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown record err=%v", err)
	}
	cancelled := f.verifyAdmission("cancelled", "verify")
	cancelled.TargetID = node.ID
	if err := f.s.FinishTurn(f.turn.ID, TurnCancelled, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.s.AdmitNuwaVerification(context.Background(), cancelled); !errors.Is(err, ErrInvalidNuwaNode) {
		t.Fatalf("cancelled turn err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.s.AdmitNuwaVerification(ctx, cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context err=%v", err)
	}
	var slots int64
	if err := f.s.db.Model(&NuwaVerifySlot{}).Count(&slots).Error; err != nil || slots != 0 {
		t.Fatalf("refused admissions wrote slots=%d err=%v", slots, err)
	}

	kept := newNuwaFixture(t)
	keptNode := kept.node("fact")
	if err := kept.s.UpsertNuwaNode(context.Background(), &keptNode, 0); err != nil {
		t.Fatal(err)
	}
	ok := kept.verifyAdmission("kept", "verify")
	ok.TargetID = keptNode.ID
	if err := kept.s.AdmitNuwaVerification(context.Background(), ok); err != nil {
		t.Fatal(err)
	}
	if err := kept.s.TruncateFromEventSeq(kept.thread.ID, kept.event.Seq); err != nil {
		t.Fatal(err)
	}
	if err := kept.s.db.Model(&NuwaVerifySlot{}).Count(&slots).Error; err != nil || slots != 0 {
		t.Fatalf("rewind left verification slots=%d err=%v", slots, err)
	}
}

func TestNuwaVerifySlotsLeaveWithTheirThreadAndProject(t *testing.T) {
	f := newNuwaFixture(t)
	node := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &node, 0); err != nil {
		t.Fatal(err)
	}
	admission := f.verifyAdmission("thread-slot", "verify")
	admission.TargetID = node.ID
	if err := f.s.AdmitNuwaVerification(context.Background(), admission); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteThread(f.thread.ID); err != nil {
		t.Fatal(err)
	}
	var slots int64
	if err := f.s.db.Model(&NuwaVerifySlot{}).Count(&slots).Error; err != nil || slots != 0 {
		t.Fatalf("deleted conversation left verification slots=%d err=%v", slots, err)
	}

	project := newNuwaFixture(t)
	projectNode := project.node("fact")
	if err := project.s.UpsertNuwaNode(context.Background(), &projectNode, 0); err != nil {
		t.Fatal(err)
	}
	projectAdmission := project.verifyAdmission("project-slot", "fix")
	projectAdmission.TargetID = projectNode.ID
	if err := project.s.AdmitNuwaVerification(context.Background(), projectAdmission); err != nil {
		t.Fatal(err)
	}
	if err := project.s.DeleteProject(project.project.ID); err != nil {
		t.Fatal(err)
	}
	if err := project.s.db.Model(&NuwaVerifySlot{}).Count(&slots).Error; err != nil || slots != 0 {
		t.Fatalf("deleted project left verification slots=%d err=%v", slots, err)
	}
	if err := deleteNuwaVerifySlots(project.s.db, "", "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestNuwaVerifyBudgetRejectsBadInputAndDoesNotRecordAFailedWrite(t *testing.T) {
	f := newNuwaFixture(t)
	bad := f.verifyAdmission("bad-intent", "audit")
	bad.TargetID = "nw_missing"
	if err := f.s.AdmitNuwaVerification(context.Background(), bad); !errors.Is(err, ErrInvalidNuwaNode) {
		t.Fatalf("illegal intent err=%v", err)
	}
	bad.Intent = "verify"
	bad.CallID = ""
	if err := f.s.AdmitNuwaVerification(context.Background(), bad); !errors.Is(err, ErrInvalidNuwaNode) {
		t.Fatalf("missing call err=%v", err)
	}
	node := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &node, 0); err != nil {
		t.Fatal(err)
	}
	finished := f.verifyAdmission("finished-turn", "verify")
	finished.TargetID = node.ID
	if err := f.s.FinishTurn(f.turn.ID, TurnDone, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.s.AdmitNuwaVerification(context.Background(), finished); !errors.Is(err, ErrInvalidNuwaNode) || !strings.Contains(err.Error(), "no longer running") {
		t.Fatalf("finished turn err=%v", err)
	}

	live := newNuwaFixture(t)
	liveNode := live.node("fact")
	if err := live.s.UpsertNuwaNode(context.Background(), &liveNode, 0); err != nil {
		t.Fatal(err)
	}
	admission := live.verifyAdmission("storage", "fix")
	admission.TargetID = liveNode.ID
	const readFailure = "nuwa_verify_read_failure"
	if err := live.s.db.Callback().Query().Before("gorm:query").Register(readFailure, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "nuwa_verify_slots" {
			_ = tx.AddError(errors.New("verification read failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = live.s.db.Callback().Query().Remove(readFailure) })
	var budget *NuwaVerifyBudgetError
	if err := live.s.AdmitNuwaVerification(context.Background(), admission); err == nil || errors.Is(err, ErrNotFound) || errors.As(err, &budget) {
		t.Fatalf("storage failure looked like a budget decision: %v", err)
	}
	if err := live.s.db.Callback().Query().Remove(readFailure); err != nil {
		t.Fatal(err)
	}
	var slots int64
	if err := live.s.db.Model(&NuwaVerifySlot{}).Count(&slots).Error; err != nil || slots != 0 {
		t.Fatalf("failed verification wrote slots=%d err=%v", slots, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	const cancelBeforeWrite = "nuwa_verify_cancel_before_write"
	if err := live.s.db.Callback().Query().After("gorm:query").Register(cancelBeforeWrite, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "nuwa_verify_slots" && strings.Contains(strings.ToLower(tx.Statement.SQL.String()), "count") {
			cancel()
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = live.s.db.Callback().Query().Remove(cancelBeforeWrite) })
	if err := live.s.AdmitNuwaVerification(ctx, admission); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled verification err=%v", err)
	}
	if err := live.s.db.Model(&NuwaVerifySlot{}).Count(&slots).Error; err != nil || slots != 0 {
		t.Fatalf("cancelled verification wrote slots=%d err=%v", slots, err)
	}
	identity := &NuwaVerifyBudgetError{Intent: "verify"}
	if identity.Error() == "" || !errors.Is(identity, errNuwaVerifyBudget) {
		t.Fatalf("budget error identity: %v", identity)
	}
}
