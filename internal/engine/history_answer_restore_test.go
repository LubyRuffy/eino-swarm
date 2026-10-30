package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

func TestPendingHistoryReportRestoresTypedSourcesAndRechecksChanges(t *testing.T) {
	e, turn, peer, b, set := historyAnswerFixture(t)
	historyReportRead(e, turn, "manager")
	res, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, historyAnswerJSON(peer.ID)), nil)
	if failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	original, failure := b.prepare(turn, res, nil)
	if failure != nil {
		t.Fatal(failure)
	}
	row, candidate, failure := e.restorePendingScheduleAnswer(turn)
	if failure != nil || row == nil || candidate == nil || candidate.HistoryReport == nil || row.ID != original.ID || candidate.Final != res.Final {
		t.Fatalf("typed report restore=%+v %+v %+v", row, candidate, failure)
	}
	if failure := e.verifyPendingHistoryAnswer(t.Context(), turn, candidate); failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	if err := e.store.DB().Model(&store.Message{}).Where("thread_id = ?", peer.ID).Update("content", "A later correction changed the source.").Error; err != nil {
		t.Fatal(err)
	}
	if failure := e.verifyPendingHistoryAnswer(t.Context(), turn, candidate); failure == nil || failure.Code != "history_source_changed" {
		t.Fatalf("restored report kept stale source: %+v", failure)
	}
}

func TestPendingHistoryRestorePreservesEmptyImagesAcrossPrivateJSON(t *testing.T) {
	e, turn, peer, b, set := historyAnswerFixture(t)
	if err := e.store.DB().Model(&store.Message{}).Where("thread_id = ?", peer.ID).Update("images", "[]").Error; err != nil {
		t.Fatal(err)
	}
	historyReportRead(e, turn, "manager")
	res, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, historyAnswerJSON(peer.ID)), nil)
	if failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	if _, failure := b.prepare(turn, res, nil); failure != nil {
		t.Fatal(failure)
	}
	_, candidate, failure := e.restorePendingScheduleAnswer(turn)
	if failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	if failure := e.verifyPendingHistoryAnswer(t.Context(), turn, candidate); failure != nil {
		t.Fatalf("unchanged source rejected after private JSON: %v cause=%v", failure, failure.Cause)
	}
	a := []store.HistorySource{{CreatedAt: time.Unix(100, 0).UTC(), Images: []store.ImageRef{}}}
	bSources := []store.HistorySource{{CreatedAt: time.Unix(100, 0).In(time.FixedZone("fixture", 3600))}}
	if !sameHistorySourceSnapshots(a, bSources) {
		t.Fatal("serialization-only time/image representation changed source identity")
	}
	bSources[0].Role = "user"
	if sameHistorySourceSnapshots(a, bSources) {
		t.Fatal("canonical comparison lost source authority")
	}
}

func TestHistoryRestoreRejectsMissingInvalidAndCancelledSourceChecks(t *testing.T) {
	e, turn, peer, b, set := historyAnswerFixture(t)
	if err := validateStoredHistoryReport(&pendingScheduleAnswer{}, e.cfg.Tools.ToolResultLimit()); err != nil {
		t.Fatal("ordinary candidate requires a fabricated history payload")
	}
	e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: "history_report_requested", AgentID: "manager"})
	if failure := e.verifyPendingHistoryAnswer(t.Context(), turn, &pendingScheduleAnswer{}); failure == nil || failure.Code != "history_read_required" {
		t.Fatalf("missing read restored: %+v", failure)
	}
	historyReadEvent(e, turn, "manager")
	if failure := e.verifyPendingHistoryAnswer(t.Context(), turn, &pendingScheduleAnswer{}); failure == nil || failure.Code != "history_report_invalid" {
		t.Fatalf("unstructured candidate restored: %+v", failure)
	}
	res, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, historyAnswerJSON(peer.ID)), nil)
	if failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	row, failure := b.prepare(turn, res, nil)
	if failure != nil {
		t.Fatal(failure)
	}
	var candidate pendingScheduleAnswer
	if err := json.Unmarshal([]byte(row.Payload), &candidate); err != nil {
		t.Fatal(err)
	}
	copy := candidate
	report := *candidate.HistoryReport
	copy.HistoryReport = &report
	copy.HistoryReport.Request.Version = 0
	if failure := e.verifyPendingHistoryAnswer(t.Context(), turn, &copy); failure == nil || failure.Code != "history_report_invalid" {
		t.Fatalf("invalid typed protocol restored: %+v", failure)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if failure := e.verifyPendingHistoryAnswer(ctx, turn, &candidate); failure == nil || isHistoryAnswerCorrection(failure) {
		t.Fatalf("cancelled canonical reread invited tool repetition: %+v", failure)
	}
}

func TestHistoryPublicationRefusesAnUnreadableExplicitRequestMarker(t *testing.T) {
	e, turn, _, b, set := historyAnswerFixture(t)
	if err := e.store.DB().Callback().Row().Before("gorm:row").Register("history_marker_read_outage", func(tx *gorm.DB) {
		for _, value := range tx.Statement.Vars {
			if value == "history_report_requested" {
				_ = tx.AddError(errors.New("private marker read unavailable"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, "ordinary prose"), nil); failure == nil || failure.Code != "history_state_unavailable" || isHistoryAnswerCorrection(failure) {
		t.Fatalf("unknown explicit mode was admitted: %+v", failure)
	}
}

func TestAcceptedHistoryReportRestoresTheSameFrozenRenderingWithoutAnotherPublication(t *testing.T) {
	e, turn, peer, b, set := historyAnswerFixture(t)
	historyReportRead(e, turn, "manager")
	res, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, historyAnswerJSON(peer.ID)), nil)
	if failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	row, failure := b.prepare(turn, res, nil)
	if failure != nil {
		t.Fatal(failure)
	}
	var candidate pendingScheduleAnswer
	if err := json.Unmarshal([]byte(row.Payload), &candidate); err != nil {
		t.Fatal(err)
	}
	if failure := e.admitScheduleAnswerWithHistory(turn.ThreadID, turn.ID, nil, candidate.Events, &candidate.HistoryReport.Evidence, row.ID); failure != nil {
		t.Fatal(failure)
	}
	if err := e.store.DB().Where("thread_id = ?", peer.ID).Delete(&store.Message{}).Error; err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(turn.ThreadID)
	rt.mu.Lock()
	reg := rt.reg
	rt.mu.Unlock()
	// An accepted candidate is already public. Restoration uses its exact
	// admitted bytes even when originals subsequently disappear.
	got, err := rt.runManager(t.Context(), turn, reg, newAccumulator(e, turn.ThreadID, turn.ID, 0), set, nil, []adk.Message{schema.UserMessage("Restore the original turn.")})
	if err != nil || got.Final != res.Final {
		t.Fatalf("accepted restore reran or rewrote output: final=%q err=%v", got.Final, err)
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	public := 0
	for _, event := range events {
		if event.Kind == "agent_message" {
			public++
			if event.Text != res.Final {
				t.Fatal("recovery changed admitted text")
			}
		}
	}
	if public != 1 {
		t.Fatalf("accepted recovery rebroadcast count=%d", public)
	}
}

func TestRestoredHistoryPayloadCannotReintroduceGeneratedProseOrForgedRendering(t *testing.T) {
	for _, corrupt := range []string{"rendered", "final", "text", "events", "content", "missing-transcript", "multimodal", "responses"} {
		t.Run(corrupt, func(t *testing.T) {
			e, turn, peer, b, set := historyAnswerFixture(t)
			historyReportRead(e, turn, "manager")
			res, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, historyAnswerJSON(peer.ID)), nil)
			if failure != nil {
				t.Fatal(failure, failure.Cause)
			}
			row, failure := b.prepare(turn, res, nil)
			if failure != nil {
				t.Fatal(failure)
			}
			var payload pendingScheduleAnswer
			if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
				t.Fatal(err)
			}
			last := payload.Transcript[len(payload.Transcript)-1]
			switch corrupt {
			case "rendered":
				payload.HistoryReport.Rendered = "forged rendered answer"
			case "final":
				payload.Final = "free generated conclusion"
			case "text":
				payload.Text = []string{"free generated conclusion"}
			case "events":
				payload.Events = append(payload.Events, payload.Events[0])
			case "content":
				last.Content = "free generated conclusion"
			case "missing-transcript":
				payload.Transcript = nil
			case "multimodal":
				last.AssistantGenMultiContent = []schema.MessageOutputPart{{Type: schema.ChatMessagePartTypeText, Text: "free generated conclusion"}}
			case "responses":
				last.Extra["responses_output"] = json.RawMessage(`[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"free generated conclusion"}]}]`)
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.store.DB().Model(&store.PendingScheduleAnswer{}).Where("id = ?", row.ID).Update("payload", string(raw)).Error; err != nil {
				t.Fatal(err)
			}
			if _, _, failure := e.restorePendingScheduleAnswer(turn); failure == nil || failure.Code != "answer_restore_invalid" {
				t.Fatalf("corrupt history report restored: %+v", failure)
			}
		})
	}
}

func TestHistoryModeReadFailuresAndToolRoundBoundariesDoNotPublishProse(t *testing.T) {
	e, turn, peer, b, set := historyAnswerFixture(t)
	historyReportRead(e, turn, "manager")
	res := historyCandidate(b, "")
	res.Transcript = append(res.Transcript, markPendingAnswer(schema.AssistantMessage("intermediate prose", nil), b.id))
	got, failure := b.prepareHistoryAnswer(t.Context(), turn, set, res, adk.ErrExceedMaxIterations)
	if failure != nil || got.Final != "" || strings.Contains(joinedContent(got.Transcript), "intermediate prose") || len(b.complete) != 0 {
		t.Fatalf("tool-round cap published an incomplete report: %+v %+v", got, failure)
	}
	row, failure := b.prepare(turn, got, adk.ErrExceedMaxIterations)
	if failure != nil {
		t.Fatal(failure)
	}
	var candidate pendingScheduleAnswer
	if err := json.Unmarshal([]byte(row.Payload), &candidate); err != nil {
		t.Fatal(err)
	}
	if failure := e.verifyPendingHistoryAnswer(t.Context(), turn, &candidate); failure != nil {
		t.Fatal(failure)
	}
	if _, failure := b.prepareHistoryAnswer(t.Context(), turn, nil, historyCandidate(b, historyAnswerJSON(peer.ID)), nil); failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	if err := e.store.DB().Exec("ALTER TABLE events RENAME TO unavailable_history_events").Error; err != nil {
		t.Fatal(err)
	}
	if _, failure := b.prepareHistoryAnswer(t.Context(), turn, set, res, nil); failure == nil || failure.Code != "history_state_unavailable" || isHistoryAnswerCorrection(failure) {
		t.Fatalf("unreadable durable mode was treated as a retryable format error: %+v", failure)
	}
}
