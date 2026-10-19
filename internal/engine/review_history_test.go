package engine

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// Lifecycle context corrects worker claims, but it cannot cite itself into
// persistent project knowledge. Historical identity must survive turn cuts.
func TestReviewHistoryCorrectsRepeatedWorkerIDsWithoutWritableDerivedEvidence(t *testing.T) {
	history := []store.Event{
		{Seq: 1, TurnID: "old", Kind: swarm.NotifySpawned.String(), AgentID: "existing"},
		{Seq: 2, TurnID: "current", Kind: KindUser, Text: "Keep the explicit project convention."},
		{Seq: 3, TurnID: "current", Kind: swarm.NotifySpawned.String(), AgentID: "existing"},
		{Seq: 4, TurnID: "current", Kind: swarm.NotifyToolCall.String(), AgentID: "reader", ToolCallID: "inspect", Text: "inspect({})"},
		{Seq: 5, TurnID: "current", Kind: swarm.NotifyToolResult.String(), AgentID: "reader", ToolCallID: "inspect", Text: "observed source bytes 界 …"},
		{Seq: 6, TurnID: "current", Kind: swarm.NotifyAgentMessage.String(), Text: "I created new workers."},
	}
	briefing := workerLifecycleFacts(history, []string{"old"}) + workerBriefingLabel + "prior model claim"
	input := selectReviewInputWithHistory(history[1:], history, briefing, "done")
	row := readWorkerFact(t, input.Transcript, "current")
	if row.New != 0 || row.Resumed != 1 || strings.Count(input.Transcript, `"source":"persisted_worker_lifecycle"`) != 1 {
		t.Fatalf("historical identity or replaced briefing facts were lost: %+v\n%s", row, input.Transcript)
	}
	if strings.Index(input.Transcript, workerEvidenceStart) != 0 || !strings.Contains(input.Transcript, "session briefing (model-written claims)") {
		t.Fatal("claims outranked authoritative lifecycle context")
	}
	for _, source := range input.Events {
		if source.TurnID != "current" || strings.Contains(source.Text, "persisted_worker_lifecycle") || strings.Contains(source.Text, "prior model claim") {
			t.Fatalf("derived or historical context gained writable source authority: %+v", source)
		}
	}
	sources := reviewEvidenceSources(history[1:], input.Events)
	if len(sources) != 2 || sources[2].Text != history[1].Text || sources[5].Text != history[4].Text {
		t.Fatalf("current human/tool bytes lost their source identity: %+v", sources)
	}
}

func TestReviewHistoryFactsShareTheSourceBudgetAndPreserveLateHumanInput(t *testing.T) {
	var history []store.Event
	for i := 0; i < workerFactTurnLimit; i++ {
		history = append(history, store.Event{Seq: int64(i + 1), TurnID: fmt.Sprintf("turn-%d", i), Kind: swarm.NotifySpawned.String(), AgentID: "same-worker"})
	}
	const current = "turn-3"
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("call-%d", i)
		history = append(history,
			store.Event{Seq: int64(len(history) + 1), TurnID: current, Kind: swarm.NotifyToolCall.String(), AgentID: "reader", ToolCallID: id, Text: "inspect({})"},
			store.Event{Seq: int64(len(history) + 2), TurnID: current, Kind: swarm.NotifyToolResult.String(), AgentID: "reader", ToolCallID: id, Text: strings.Repeat("observed 界 ", 1000)},
		)
	}
	history = append(history, store.Event{Seq: int64(len(history) + 1), TurnID: current, Kind: KindSteer, Text: "late human convention 界 …"})
	input := selectReviewInputWithHistory(history, history, strings.Repeat("prior claim ", reviewMaxChars), strings.Repeat("final claim ", reviewMaxChars))
	if utf8.RuneCountInString(input.Transcript) > reviewMaxChars || !utf8.ValidString(input.Transcript) || !strings.Contains(input.Transcript, reviewSourceOmitted) {
		t.Fatalf("lifecycle context escaped the full source budget: %d", utf8.RuneCountInString(input.Transcript))
	}
	if row := readWorkerFact(t, input.Transcript, current); row.New != 0 || row.Resumed != 1 {
		t.Fatalf("budgeting lost historical identity: %+v", row)
	}
	last := history[len(history)-1]
	if input.Events[last.Seq].Text != last.Text {
		t.Fatal("authoritative context crowded out late human evidence")
	}
}

func TestReviewAutomaticAndManualPathsUseFullThreadWorkerHistory(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprint("manual=", manual), func(t *testing.T) {
			e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
			_, th := projectThread(t, e)
			pc, err := e.projectContextFor(th)
			if err != nil {
				t.Fatal(err)
			}
			old := &store.Turn{ThreadID: th.ID, Status: store.TurnDone}
			current := &store.Turn{ThreadID: th.ID, Status: store.TurnDone}
			for _, turn := range []*store.Turn{old, current} {
				if err := e.store.CreateTurn(turn); err != nil {
					t.Fatal(err)
				}
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifySpawned.String(), AgentID: "existing-worker"})
			}
			e.record(store.Event{ThreadID: th.ID, TurnID: current.ID, Kind: KindUser, Text: "Retain the project convention."})
			if manual {
				got, err := e.ReviewTurn(th.ID)
				if err != nil || got == nil || got.ID != current.ID {
					t.Fatalf("manual review selected a different turn: %+v %v", got, err)
				}
			} else {
				e.scheduleReview(th.ID, current, store.TurnDone, pc, "done")
			}
			settleReviewProtocol(t, e)
			requests := fixture.snapshot()
			if len(requests) != 1 {
				t.Fatalf("review calls=%d", len(requests))
			}
			body := reviewProtocolUserInput(t, requests[0])
			if row := readWorkerFact(t, body, current.ID); row.New != 0 || row.Resumed != 1 {
				t.Fatalf("full-thread identity was replaced by a turn-local count: %+v", row)
			}
		})
	}
}

// Failed-turn learning is human-only and must not acquire the successful
// lifecycle/briefing path, even when history lookup itself would fail.
func TestFailedReviewDoesNotReadOrIncludeLifecycleHistory(t *testing.T) {
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "", nil })
	th, turn, pc := qualityReviewTurn(t, e, store.TurnError, []store.Event{
		{Kind: KindUser, Text: "Retain the explicit human convention."},
		{Kind: swarm.NotifySpawned.String(), AgentID: "worker"},
		{Kind: swarm.NotifyToolResult.String(), Text: "task claim must stay out"},
	})
	var reads atomic.Int32
	const callback = "review_failed_turn_history_forbidden"
	if err := e.store.DB().Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "events" && strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"].Expression), "thread_id = ? AND seq > ?") {
			reads.Add(1)
			_ = tx.AddError(errors.New("history must not be used for failed-turn learning"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.store.DB().Callback().Query().Remove(callback) })
	e.scheduleReview(th.ID, turn, store.TurnError, pc, "final claim must stay out")
	settleReviewProtocol(t, e)
	requests := fixture.snapshot()
	if len(requests) != 1 || reads.Load() != 0 {
		t.Fatalf("failed review requests=%d history reads=%d", len(requests), reads.Load())
	}
	body := reviewProtocolUserInput(t, requests[0])
	for _, excluded := range []string{workerEvidenceStart, "task claim must stay out", "final claim must stay out"} {
		if strings.Contains(body, excluded) {
			t.Fatalf("failed-turn input acquired %q", excluded)
		}
	}
}

func TestReviewCompatibilityWrapperKeepsCanonicalHistoryAndFailedHumanAuthority(t *testing.T) {
	for _, status := range []string{store.TurnDone, store.TurnError} {
		for _, canonical := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/canonical=%t", status, canonical), func(t *testing.T) {
				const requirement = "Retain the explicit human convention."
				var citation reviewCitation
				e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) {
					return "", []reviewOperation{{Target: "memory", Action: "add", Content: requirement, Evidence: []reviewCitation{citation}}}
				})
				e.cfg.Memory.ReviewMaxIterations = 1
				th, turn, pc := qualityReviewTurn(t, e, status, []store.Event{
					{Kind: KindUser, Text: requirement},
					{Kind: swarm.NotifySpawned.String(), AgentID: "worker"},
					{Kind: swarm.NotifyToolResult.String(), Text: "unverified task claim"},
				})
				citation = reviewProtocolCitation(t, e, turn.ID, requirement)
				history, err := e.store.ListEvents(th.ID, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				events := reviewEventsForTurn(history, turn.ID)
				input := selectReviewInputWithHistory(events, history, "", turn.Final)
				if status == store.TurnError {
					input = selectReviewInput(reviewHumanEvents(events), "", "")
				}
				transcript := input.Transcript
				if !canonical {
					transcript += "\nDiagnostic text is not canonical source evidence."
				}
				e.runReview(th.ID, turn, pc, transcript)
				out := decodeReview(t, waitForReview(t, e, turn.ID))
				snap, err := pc.memory.Read()
				if err != nil || out.Changed != canonical {
					t.Fatalf("canonical authority=%t notes=%+v outcome=%+v err=%v", canonical, snap, out, err)
				}
				if canonical {
					if out.Err != "" || len(snap.Entries) != 1 || snap.Entries[0] != requirement {
						t.Fatalf("canonical source failed to commit: %+v %+v", snap, out)
					}
				} else if out.Err == "" || len(snap.Entries) != 0 || len(out.Refused) != 1 {
					t.Fatalf("diagnostic text gained source authority: %+v %+v", snap, out)
				}
				requests := fixture.snapshot()
				if len(requests) != 1 {
					t.Fatalf("unexpected wrapper model calls: %d", len(requests))
				}
				body := reviewProtocolUserInput(t, requests[0])
				if status == store.TurnError && (strings.Contains(body, workerEvidenceStart) || strings.Contains(body, "unverified task claim")) {
					t.Fatal("failed-turn wrapper sent non-human evidence")
				}
			})
		}
	}
}
