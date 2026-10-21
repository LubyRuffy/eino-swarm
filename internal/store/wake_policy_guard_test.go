package store

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func wakeMutation(t *testing.T, s *Store, scope *GoalTurnScope, row *Schedule, operation string) func() error {
	t.Helper()
	switch operation {
	case "create":
		return func() error {
			return s.CreateScheduleForTurn(scope, &Schedule{Kind: ScheduleThread, ThreadID: row.ThreadID, CreatedBy: ScheduleCreatedManager, NextRunAt: time.Now().Add(time.Hour)}, 50)
		}
	case "replace":
		return func() error {
			return s.ReplaceThreadWakeForTurn(scope, row.ThreadID, row.ID, map[string]any{"prompt": "next check", "every_s": 97, "next_run_at": time.Now().Add(time.Hour)})
		}
	default:
		return func() error {
			return s.RearmScheduleForTurn(scope, row.ID, 50, map[string]any{"prompt": "next check", "every_s": 97, "next_run_at": time.Now().Add(time.Hour)})
		}
	}
}

func TestHumanWakePolicyRejectsEveryToolMutationWithoutChangingWait(t *testing.T) {
	for _, operation := range []string{"create", "replace", "rearm"} {
		t.Run(operation, func(t *testing.T) {
			s, scope, row := storedGoalTurn(t)
			if err := s.UpdateSchedule(row.ID, map[string]any{"status": ScheduleActive}); err != nil {
				t.Fatal(err)
			}
			original, err := s.GetSchedule(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			input := wakeHuman(t, s, scope.ThreadID, "user_message", "Remain active until the requested work is present.")
			mutate := wakeMutation(t, s, &scope, row, operation)
			err = mutate()
			var pending *WakePolicyPendingError
			if !errors.Is(err, ErrWakePolicyPending) || !errors.As(err, &pending) || pending.Seq != input.Seq || !strings.Contains(err.Error(), fmt.Sprint(input.Seq)) {
				t.Fatalf("unclassified human input did not block: %v", err)
			}
			saveWakeDecision(t, s, scope.ThreadID, input, WakeDecisionDeny, "Remain active")
			err = mutate()
			var denied *ThreadWakeDeniedError
			if !errors.Is(err, ErrThreadWakeDenied) || !errors.As(err, &denied) || denied.Policy.SourceSeq != input.Seq || denied.Policy.SourceText != input.Text || denied.Policy.Quote != "Remain active" || !strings.Contains(err.Error(), fmt.Sprint(input.Seq)) {
				t.Fatalf("denial lost human evidence: %v", err)
			}
			current, err := s.GetSchedule(row.ID)
			if err != nil || !reflect.DeepEqual(current, original) {
				t.Fatalf("refused mutation changed wait: before=%+v after=%+v err=%v", original, current, err)
			}
			rows, err := s.ListSchedules()
			if err != nil || len(rows) != 1 {
				t.Fatalf("refused mutation created wait: %+v %v", rows, err)
			}
			release := wakeHuman(t, s, scope.ThreadID, "user_message", "You may wait and return later.")
			saveWakeDecision(t, s, scope.ThreadID, release, WakeDecisionAllow, "may wait")
			if err := mutate(); err != nil {
				t.Fatalf("later human consent did not release write: %v", err)
			}
		})
	}
}

func TestHumanScheduleFormsAndStandaloneJobsKeepTheirOwnRules(t *testing.T) {
	s, scope, row := storedGoalTurn(t)
	input := wakeHuman(t, s, scope.ThreadID, "user_message", "Continue active work.")
	saveWakeDecision(t, s, scope.ThreadID, input, WakeDecisionDeny, "Continue active work.")
	if err := s.UpdateSchedule(row.ID, map[string]any{"status": ScheduleActive}); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"create", "replace", "rearm"} {
		if err := wakeMutation(t, s, nil, row, operation)(); err != nil {
			t.Fatalf("public %s inherited a manager tool restriction: %v", operation, err)
		}
	}
	job := &Schedule{Kind: ScheduleStandalone, CreatedBy: ScheduleCreatedManager}
	if err := s.CreateScheduleForTurn(&scope, job, 50); err != nil {
		t.Fatalf("standalone creation inherited thread policy: %v", err)
	}
	// Stopping the wait is always valid while suspension is prohibited.
	if _, err := s.CancelScheduleForTurn(&scope, row.ID); err != nil {
		t.Fatalf("policy prevented stopping a wait: %v", err)
	}
}

func TestWakeRearmChecksTheActualDestinationInTheWriteTransaction(t *testing.T) {
	s, scope, row := storedGoalTurn(t)
	other := &Thread{}
	if err := s.CreateThread(other); err != nil {
		t.Fatal(err)
	}
	input := wakeHuman(t, s, other.ID, "user_message", "Keep this conversation active.")
	saveWakeDecision(t, s, other.ID, input, WakeDecisionDeny, "Keep this conversation active.")
	if err := s.UpdateSchedule(row.ID, map[string]any{"thread_id": other.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.RearmScheduleForTurn(&scope, row.ID, 50, map[string]any{"every_s": 60}); !errors.Is(err, ErrThreadWakeDenied) {
		t.Fatalf("retargeted destination bypassed human authority: %v", err)
	}
}

func TestPendingHumanInputClosesTheClassificationAndArmingRace(t *testing.T) {
	s, scope, row := storedGoalTurn(t)
	first := wakeHuman(t, s, scope.ThreadID, "user_message", "Waiting is allowed initially.")
	saveWakeDecision(t, s, scope.ThreadID, first, WakeDecisionAllow, "Waiting is allowed")
	deny := wakeHuman(t, s, scope.ThreadID, "steer", "Continue active work until the check succeeds.")
	var wg sync.WaitGroup
	errorsOut := make(chan error, 17)
	wg.Add(1)
	go func() {
		defer wg.Done()
		errorsOut <- s.SaveWakeDecision(scope.ThreadID, deny, WakeDecisionDeny, "Continue active work")
	}()
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.RearmScheduleForTurn(&scope, row.ID, 50, map[string]any{"next_run_at": time.Now().Add(time.Hour)})
			if errors.Is(err, ErrThreadWakeDenied) || errors.Is(err, ErrWakePolicyPending) {
				errorsOut <- nil
				return
			}
			errorsOut <- fmt.Errorf("pending human input was bypassed: %v", err)
		}()
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Error(err)
		}
	}
	got, err := s.GetSchedule(row.ID)
	if err != nil || got.Status != ScheduleDone || got.ArmedTurnID != "" {
		t.Fatalf("racing tool rearmed a denied wake: %+v %v", got, err)
	}
}

func TestWakePolicyWriteFailuresRollbackDecisionsAndWaits(t *testing.T) {
	for _, failure := range []string{"decision", "schedule"} {
		t.Run(failure, func(t *testing.T) {
			s, scope, row := storedGoalTurn(t)
			input := wakeHuman(t, s, scope.ThreadID, "user_message", "Waiting is allowed.")
			table, operation := "thread_wake_decisions", "INSERT"
			if failure == "schedule" {
				saveWakeDecision(t, s, scope.ThreadID, input, WakeDecisionAllow, "Waiting is allowed")
				table, operation = "schedules", "UPDATE"
			}
			if err := s.DB().Exec(fmt.Sprintf("CREATE TRIGGER fail_wake BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'wake write rejected'); END", operation, table)).Error; err != nil {
				t.Fatal(err)
			}
			var err error
			if failure == "decision" {
				err = s.SaveWakeDecision(scope.ThreadID, input, WakeDecisionAllow, "Waiting is allowed")
			} else {
				err = s.RearmScheduleForTurn(&scope, row.ID, 50, map[string]any{"every_s": 77})
			}
			if err == nil || !strings.Contains(err.Error(), "wake write rejected") {
				t.Fatalf("storage refusal missing: %v", err)
			}
			policy, _, readErr := s.ReadThreadWakePolicy(scope.ThreadID)
			if readErr != nil || (failure == "decision" && policy.PendingSeq != input.Seq) {
				t.Fatalf("classification partially committed: %+v %v", policy, readErr)
			}
			got, err := s.GetSchedule(row.ID)
			if err != nil || got.EveryS != 0 || got.ArmedTurnID != "" || got.Status != ScheduleDone {
				t.Fatalf("schedule partially committed: %+v %v", got, err)
			}
		})
	}
}

func TestWakePolicyRewindAndDeletionRemoveDecisionAuthority(t *testing.T) {
	for _, operation := range []string{"rewind", "thread", "project"} {
		t.Run(operation, func(t *testing.T) {
			s := open(t)
			project := &Project{Name: "project"}
			if err := s.CreateProject(project); err != nil {
				t.Fatal(err)
			}
			thread := &Thread{ProjectID: project.ID}
			if err := s.CreateThread(thread); err != nil {
				t.Fatal(err)
			}
			turn := seedTurn(t, s, thread.ID, "user_message", "A human directive.")
			events, err := s.ListEvents(thread.ID, 0, 0)
			if err != nil || len(events) == 0 {
				t.Fatalf("missing source: %v", err)
			}
			input := WakePolicyInput{Seq: events[0].Seq, Text: events[0].Text, Kind: events[0].Kind}
			saveWakeDecision(t, s, thread.ID, input, WakeDecisionDeny, input.Text)
			switch operation {
			case "rewind":
				err = s.TruncateFromEventSeq(thread.ID, input.Seq)
			case "thread":
				err = s.DeleteThread(thread.ID)
			case "project":
				err = s.DeleteProject(project.ID)
			}
			if err != nil {
				t.Fatalf("%s failed (turn %s): %v", operation, turn.ID, err)
			}
			var count int64
			if err := s.DB().Model(&ThreadWakeDecision{}).Where("thread_id=?", thread.ID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("removed human authority retained: %d %v", count, err)
			}
		})
	}
}

func TestWakePolicyIgnoresStaleAndCorruptCachedRows(t *testing.T) {
	for _, fault := range []string{"identity", "digest", "enum", "quote", "empty-quote", "unchanged-quote"} {
		t.Run(fault, func(t *testing.T) {
			s, scope, _ := storedGoalTurn(t)
			input := wakeHuman(t, s, scope.ThreadID, "user_message", "Human source.")
			saveWakeDecision(t, s, scope.ThreadID, input, WakeDecisionDeny, input.Text)
			fields := map[string]any{}
			switch fault {
			case "identity":
				fields["event_id"] = 999
			case "digest":
				fields["input_digest"] = "stale"
			case "enum":
				fields["decision"] = "unknown"
			case "quote":
				fields["quote"] = "not in the source"
			case "empty-quote":
				fields["quote"] = " "
			case "unchanged-quote":
				fields["decision"] = WakeDecisionUnchanged
			}
			if err := s.DB().Model(&ThreadWakeDecision{}).Where("thread_id=?", scope.ThreadID).Updates(fields).Error; err != nil {
				t.Fatal(err)
			}
			policy, _, err := s.ReadThreadWakePolicy(scope.ThreadID)
			if err != nil || policy.PendingSeq != input.Seq || policy.Denied {
				t.Fatalf("bad cache was authority: %+v %v", policy, err)
			}
		})
	}
}

func TestWakePolicyStorageFailuresAreNeverConsent(t *testing.T) {
	for _, fault := range []string{"events", "decisions", "closed", "missing-thread"} {
		t.Run(fault, func(t *testing.T) {
			s, scope, _ := storedGoalTurn(t)
			input := wakeHuman(t, s, scope.ThreadID, "user_message", "Human input.")
			switch fault {
			case "events":
				if err := s.DB().Migrator().DropTable(&Event{}); err != nil {
					t.Fatal(err)
				}
			case "decisions":
				if err := s.DB().Migrator().DropTable(&ThreadWakeDecision{}); err != nil {
					t.Fatal(err)
				}
			case "closed":
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			case "missing-thread":
				scope.ThreadID = "missing"
			}
			if _, _, err := s.ReadThreadWakePolicy(scope.ThreadID); err == nil {
				t.Fatal("failed policy read became consent")
			}
			if err := s.SaveWakeDecision(scope.ThreadID, input, WakeDecisionUnchanged, ""); err == nil {
				t.Fatal("failed classification write became consent")
			}
			if fault == "missing-thread" {
				if _, _, err := s.ReadThreadWakePolicy(scope.ThreadID); !errors.Is(err, ErrNotFound) {
					t.Fatalf("missing thread lost error category: %v", err)
				}
			}
			if err := s.DB().Transaction(func(tx *gorm.DB) error { return checkThreadWakePolicy(tx, scope.ThreadID) }); err == nil {
				t.Fatal("failed transactional read became consent")
			}
		})
	}
}

func TestReportWithNextDelayRejectsBeforeCancellingTheWait(t *testing.T) {
	s, scope, row := storedGoalTurn(t)
	if err := s.UpdateSchedule(row.ID, map[string]any{"status": ScheduleActive}); err != nil {
		t.Fatal(err)
	}
	original, err := s.GetSchedule(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := wakeHuman(t, s, scope.ThreadID, "user_message", "Continue actively.")
	changed, err := s.CancelScheduleForTurnWithWakePolicy(&scope, row.ID)
	if changed != nil || !errors.Is(err, ErrWakePolicyPending) {
		t.Fatalf("pending report cancelled its wait: %+v %v", changed, err)
	}
	saveWakeDecision(t, s, scope.ThreadID, input, WakeDecisionDeny, input.Text)
	changed, err = s.CancelScheduleForTurnWithWakePolicy(&scope, row.ID)
	if changed != nil || !errors.Is(err, ErrThreadWakeDenied) {
		t.Fatalf("denied report cancelled its wait: %+v %v", changed, err)
	}
	got, err := s.GetSchedule(row.ID)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("refused report changed wait: %+v %v", got, err)
	}
	allow := wakeHuman(t, s, scope.ThreadID, "user_message", "A later check is allowed.")
	saveWakeDecision(t, s, scope.ThreadID, allow, WakeDecisionAllow, allow.Text)
	changed, err = s.CancelScheduleForTurnWithWakePolicy(&scope, row.ID)
	if err != nil || changed == nil || changed.Status != ScheduleCancelled {
		t.Fatalf("human release did not allow report: %+v %v", changed, err)
	}
}
