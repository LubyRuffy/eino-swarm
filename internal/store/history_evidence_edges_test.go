package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

func TestCanonicalHistoryBudgetIncludesWholeMetadataAndAllSources(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	if err := f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("content", "中文\x00原文").Error; err != nil {
		t.Fatal(err)
	}
	sources, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs(), 20000)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	budget := utf8.RuneCount(body)
	if utf8.RuneCount(body) == len(body) {
		t.Fatal("fixture no longer exercises Unicode budget")
	}
	if got, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs(), budget); err != nil || len(got) != len(sources) || !sameHistorySource(got[0], sources[0]) || !sameHistorySource(got[1], sources[1]) {
		t.Fatalf("exact complete JSON source budget rejected: %#v %v", got, err)
	}
	if got, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs(), budget-1); !errors.Is(err, ErrHistoryEvidenceLimit) || got != nil {
		t.Fatalf("overbudget second source returned partial originals: %#v %v", got, err)
	}
	if got, err := f.s.ReadHistorySources(context.Background(), f.project.ID, nil, 2); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty original list changed: %#v %v", got, err)
	}
	if err := f.s.DB().Model(&Thread{}).Where("id=?", f.source.ID).Update("title", strings.Repeat("long source title", 1000)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs()[:1], 1000); !errors.Is(err, ErrHistoryEvidenceLimit) {
		t.Fatalf("metadata escaped source budget: %v", err)
	}
}

func TestCanonicalHistoryEmptyTextRetainsImageAndIdentity(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	if err := f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("content", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB().Model(&Event{}).Where("id=?", f.event.ID).Updates(map[string]any{"text": "", "images": "[]", "err": ""}).Error; err != nil {
		t.Fatal(err)
	}
	sources, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs(), 20000)
	if err != nil || len(sources) != 2 || sources[0].Text != "" || len(sources[0].Images) != 1 || sources[0].Reference != f.refs()[0] || sources[1].Text != "" || len(sources[1].Images) != 0 {
		t.Fatalf("empty published original lost identity/images: %#v %v", sources, err)
	}
	// JSON recovery can turn empty slices into nil through omitempty. That is
	// the same absent image list, without weakening any nonempty image identity.
	sources[1].Images = nil
	if err := f.s.AdmitScheduleAnswerWithHistory(f.recipient.ID, f.turn.ID, nil, admissionAnswer(), &HistoryEvidence{ProjectID: f.project.ID, Sources: sources}); err != nil {
		t.Fatalf("empty source could not be bound after recovery: %v", err)
	}
}

func TestCanonicalHistoryPreflightAvoidsLoadingHugeBodyOrPrivateColumns(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	if err := f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("content", strings.Repeat("x", 1<<20)).Error; err != nil {
		t.Fatal(err)
	}
	fullBodyReads := 0
	if err := f.s.DB().Callback().Query().Before("gorm:query").Register("history_guard_projection", func(tx *gorm.DB) {
		selected := strings.Join(tx.Statement.Selects, ",")
		if strings.Contains(selected, "reasoning") || strings.Contains(selected, "tool_calls") {
			t.Error("canonical reader selected private model data")
		}
		if strings.Contains(selected, "s.content AS text") {
			fullBodyReads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs()[:1], 1000); !errors.Is(err, ErrHistoryEvidenceLimit) || fullBodyReads != 0 {
		t.Fatalf("large body loaded before allocation guard: reads=%d err=%v", fullBodyReads, err)
	}
	if err := f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("content", "small source").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs(), 20000); err != nil || fullBodyReads != 1 {
		t.Fatalf("canonical public projection did not execute: reads=%d err=%v", fullBodyReads, err)
	}
}

func TestCanonicalHistoryStorageFailuresKeepTheirOriginalCause(t *testing.T) {
	for _, stage := range []string{"project", "sizes", "body"} {
		t.Run(stage, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			cause := errors.New("fixture canonical " + stage + " read failure")
			if stage == "project" {
				if err := f.s.DB().Callback().Row().Before("gorm:row").Register("history_project_failure", func(tx *gorm.DB) { _ = tx.AddError(cause) }); err != nil {
					t.Fatal(err)
				}
			} else if err := f.s.DB().Callback().Query().Before("gorm:query").Register("history_source_failure", func(tx *gorm.DB) {
				selected := strings.Join(tx.Statement.Selects, ",")
				if (stage == "sizes" && strings.Contains(selected, "body_bytes")) || (stage == "body" && strings.Contains(selected, "s.content AS text")) {
					_ = tx.AddError(cause)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if sources, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs(), 20000); sources != nil || !errors.Is(err, cause) || errors.Is(err, ErrHistorySourceUnavailable) || errors.Is(err, ErrHistoryEvidenceLimit) {
				t.Fatalf("storage failure became mechanical correction: %#v %v", sources, err)
			}
		})
	}
	closed := newHistoryEvidenceFixture(t)
	if err := closed.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := closed.s.ReadHistorySources(context.Background(), closed.project.ID, closed.refs(), 20000); err == nil {
		t.Fatal("closed DB looked like empty history")
	}
}

func TestHistoryEvidenceVerificationRejectsCorruptRecoveryAndStorageErrors(t *testing.T) {
	for _, stage := range []string{"missing-recipient", "recipient-read", "corrupt-time", "invalid-ref", "source-read"} {
		t.Run(stage, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			evidence := historyReadEvidence(t, f)
			cause := errors.New("fixture verification storage failure")
			switch stage {
			case "missing-recipient":
				if err := f.s.DB().Delete(&f.recipient).Error; err != nil {
					t.Fatal(err)
				}
			case "recipient-read", "source-read":
				if err := f.s.DB().Callback().Query().Before("gorm:query").Register("history_verify_failure", func(tx *gorm.DB) {
					if (stage == "recipient-read" && tx.Statement.Table == "threads") || (stage == "source-read" && strings.Contains(strings.Join(tx.Statement.Selects, ","), "body_bytes")) {
						_ = tx.AddError(cause)
					}
				}); err != nil {
					t.Fatal(err)
				}
			case "corrupt-time":
				evidence.Sources[0].CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			case "invalid-ref":
				evidence.Sources[0].Reference.Kind = "alias"
			}
			err := f.s.DB().Transaction(func(tx *gorm.DB) error { return verifyHistoryEvidence(tx, f.recipient.ID, evidence) })
			if stage == "recipient-read" || stage == "source-read" {
				if !errors.Is(err, cause) || errors.Is(err, ErrHistoryEvidenceChanged) {
					t.Fatalf("storage failure mislabeled changed-source: %v", err)
				}
			} else if !errors.Is(err, ErrHistoryEvidenceChanged) {
				t.Fatalf("corrupt recovered snapshot was accepted: %v", err)
			}
		})
	}
}

func TestDurableHistoryReportModeKeepsTurnIdentityAndStorageFailure(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	for _, ids := range [][2]string{{"", f.turn.ID}, {f.recipient.ID, ""}} {
		if got, err := f.s.TurnHasHistoryQuery(ids[0], ids[1]); err != nil || got {
			t.Fatalf("empty identity activated history: %v %v", got, err)
		}
	}
	for _, ev := range []Event{
		{ThreadID: f.source.ID, TurnID: f.turn.ID, Kind: "tool_call", Text: `query_history({})`},
		{ThreadID: f.recipient.ID, TurnID: "other-turn", Kind: "tool_call", Text: `query_history({})`},
		{ThreadID: f.recipient.ID, TurnID: f.turn.ID, Kind: "tool_result", Text: `query_history({})`},
	} {
		if err := f.s.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := f.s.TurnHasHistoryQuery(f.recipient.ID, f.turn.ID); err != nil || got {
		t.Fatalf("other identity/result activated history: %v %v", got, err)
	}
	if err := f.s.DB().Exec("DROP INDEX idx_evt_thread_turn_kind_seq").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TurnHasHistoryQuery(f.recipient.ID, f.turn.ID); err == nil {
		t.Fatal("missing required index silently fell back to full history scan")
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TurnHasHistoryReportRequest(f.recipient.ID, f.turn.ID); err == nil {
		t.Fatal("closed DB silently hid durable report request")
	}
}
