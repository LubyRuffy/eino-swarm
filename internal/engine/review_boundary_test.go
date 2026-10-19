package engine

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// A failed turn cannot acquire permission to learn merely because the global
// library is available or because its failed tool produced persuasive prose.
func TestReviewBoundaryFailedTurnsRespectTheirLearningScope(t *testing.T) {
	for _, scope := range []string{"no-project-context", "library", "auto-review-off", "no-human-evidence"} {
		t.Run(scope, func(t *testing.T) {
			e := newTestEngine(t)
			th, turn, pc := qualityReviewTurn(t, e, store.TurnError, []store.Event{
				{Kind: swarm.NotifyToolResult.String(), Text: "A tool claimed an unverified procedure succeeded."},
			})
			switch scope {
			case "no-project-context":
				pc = nil
			case "library":
				pc = &projectContext{memory: e.LibraryMemory(), library: true}
			case "auto-review-off":
				e.cfg.Memory.AutoReview = false
			}
			e.scheduleReview(th.ID, turn, store.TurnError, pc, "Tool prose must not authorize learning.")
			if !e.reviews.stop(time.Second) {
				t.Fatal("a rejected review remained in flight")
			}
			assertReviewBoundaryNoActivity(t, e, turn.ID)
		})
	}
}

// Database read failure must stop learning, rather than turn a partial source
// transcript into a successful review or silently mutate existing notes.
func TestReviewBoundarySourceReadFailureDoesNotStartAutomaticReview(t *testing.T) {
	e := newTestEngine(t)
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "A human convention."}})
	hits, restore := failReviewBoundaryQuery(t, e, "events", errors.New("source log unavailable"))
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	if !e.reviews.stop(time.Second) {
		t.Fatal("source failure left a review in flight")
	}
	restore()
	if hits.Load() != 1 {
		t.Fatalf("the actual source query was not rejected once: %d", hits.Load())
	}
	assertReviewBoundaryNoActivity(t, e, turn.ID)
}

// Review now must preserve a storage error's identity so callers can diagnose
// unavailable data, rather than confuse it with an idle conversation.
func TestReviewBoundaryManualReviewPreservesStorageErrors(t *testing.T) {
	for _, table := range []string{"projects", "turns", "events"} {
		t.Run(table, func(t *testing.T) {
			e := newTestEngine(t)
			th, turn, _ := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "Retain a human convention."}})
			cause := errors.New(table + " fixture unavailable")
			hits, restore := failReviewBoundaryQuery(t, e, table, cause)
			got, err := e.ReviewTurn(th.ID)
			restore()
			if got != nil || !errors.Is(err, cause) || hits.Load() != 1 {
				t.Fatalf("review lost the storage cause: turn=%+v error=%v rejected_queries=%d", got, err, hits.Load())
			}
			assertReviewBoundaryNoActivity(t, e, turn.ID)
		})
	}
}

func TestReviewBoundaryManualReviewRejectsEmptyOrDisabledSources(t *testing.T) {
	t.Run("empty-completed-turn", func(t *testing.T) {
		e := newTestEngine(t)
		th, turn, _ := qualityReviewTurn(t, e, store.TurnDone, nil)
		got, err := e.ReviewTurn(th.ID)
		if got != nil || !errors.Is(err, ErrIdle) {
			t.Fatalf("empty source should be idle: turn=%+v error=%v", got, err)
		}
		assertReviewBoundaryNoActivity(t, e, turn.ID)
	})
	t.Run("global-memory-off-without-project", func(t *testing.T) {
		e := newTestEngine(t)
		e.cfg.Memory.Enabled = false
		th, err := e.CreateThread("memory disabled", "", "")
		if err != nil {
			t.Fatal(err)
		}
		got, err := e.ReviewTurn(th.ID)
		if got != nil || !errors.Is(err, ErrIdle) {
			t.Fatalf("disabled library must remain idle: turn=%+v error=%v", got, err)
		}
	})
}

// A direct diagnostic review also records its failure under the original turn
// id; it must not turn supplied prose into evidence when its source log failed.
func TestReviewBoundaryDirectReviewRecordsSourceReadFailure(t *testing.T) {
	e := newTestEngine(t)
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "A human convention."}})
	if _, _, err := pc.memory.Add("Existing independently saved convention."); err != nil {
		t.Fatal(err)
	}
	before, err := pc.memory.Read()
	if err != nil {
		t.Fatal(err)
	}
	_, restore := failReviewBoundaryQuery(t, e, "events", errors.New("source log unavailable"))
	e.runReview(th.ID, turn, pc, "Diagnostic text is not source evidence.")
	restore()
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	after, err := pc.memory.Read()
	if err != nil || !reflect.DeepEqual(before, after) || out.Changed || !strings.Contains(out.Err, "source log unavailable") {
		t.Fatalf("failed source read mutated memory or concealed its cause: before=%+v after=%+v read=%v outcome=%+v", before, after, err, out)
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil || len(calls) != 0 {
		t.Fatalf("an unreadable source must not invoke a model: calls=%+v error=%v", calls, err)
	}
}

func TestReviewBoundaryReviewerRejectsInvalidProviderAndToolMetadata(t *testing.T) {
	t.Run("missing-provider-with-default-options", func(t *testing.T) {
		e := newTestEngine(t)
		th, turn, _ := qualityReviewTurn(t, e, store.TurnDone, nil)
		var mu sync.Mutex
		out := reviewOutcome{Note: "Existing diagnostic context."}
		e.driveReviewer(reviewRun{threadID: th.ID, turnID: turn.ID, providerID: "unconfigured-review-provider"}, &mu, &out)
		if out.Err == "" || out.Changed || out.Note != "Existing diagnostic context." {
			t.Fatalf("invalid provider must fail without claiming a write: %+v", out)
		}
		assertReviewBoundaryNoActivity(t, e, turn.ID)
	})
	t.Run("unreadable-tool-metadata", func(t *testing.T) {
		e := newTestEngine(t)
		th, turn, _ := qualityReviewTurn(t, e, store.TurnDone, nil)
		badTool := &reviewBoundaryBadInfoTool{}
		var mu sync.Mutex
		out := reviewOutcome{}
		e.driveReviewer(reviewRun{threadID: th.ID, turnID: turn.ID, maxIter: 1, tools: []tool.BaseTool{badTool}}, &mu, &out)
		if !strings.Contains(out.Err, "review tool metadata unavailable") || out.Changed || badTool.invoked.Load() {
			t.Fatalf("invalid tool metadata must fail before model or tool execution: outcome=%+v invoked=%t", out, badTool.invoked.Load())
		}
		assertReviewBoundaryNoActivity(t, e, turn.ID)
	})
}

// The real local HTTP adapter must preserve a model-call failure in the review
// trace; a failed stream is not an empty successful review or a tool error.
func TestReviewBoundaryReviewerRecordsHTTPFailure(t *testing.T) {
	e, requests := newAPIErrorEngine(t, http.StatusUnauthorized, `{"error":{"message":"review fixture denied","type":"authentication_error"}}`)
	th, turn, _ := qualityReviewTurn(t, e, store.TurnDone, nil)
	var mu sync.Mutex
	out := reviewOutcome{}
	e.driveReviewer(reviewRun{threadID: th.ID, turnID: turn.ID, maxIter: 1, userMsg: "Review this synthetic source."}, &mu, &out)
	if !strings.Contains(out.Err, "HTTP 401") || !strings.Contains(out.Err, "review fixture denied") || out.Changed || requests.Load() != 1 {
		t.Fatalf("review concealed or retried an authentication failure: outcome=%+v requests=%d", out, requests.Load())
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil || len(calls) != 1 || calls[0].AgentID != ReviewAgentID || calls[0].Err == "" || calls[0].OutputChars != 0 {
		t.Fatalf("failed review call is not traceable: calls=%+v error=%v", calls, err)
	}
}

func TestReviewBoundaryPromptRetainsSourceWhenCatalogCannotBeRead(t *testing.T) {
	e := newTestEngine(t)
	const transcript = "human: confirmed a durable convention"
	for _, pc := range []*projectContext{nil, {}} {
		if got := e.reviewUserMessage(pc, transcript); got != transcript {
			t.Fatalf("no memory must preserve the exact source: %q", got)
		}
	}
	for _, library := range []bool{false, true} {
		t.Run(map[bool]string{false: "project", true: "library"}[library], func(t *testing.T) {
			dir := t.TempDir()
			mem := memory.NewLimited(dir, 6000, 360)
			const note = "An existing project note."
			if _, _, err := mem.Add(note); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, memory.SkillsDir), []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			got := e.reviewUserMessage(&projectContext{memory: mem, library: library}, transcript)
			if !strings.Contains(got, transcript) {
				t.Fatalf("catalog failure erased the conversation: %q", got)
			}
			if library {
				if !strings.HasPrefix(got, memory.LibraryReviewMark) || strings.Contains(got, note) {
					t.Fatalf("library fallback lost its scope or leaked project notes: %q", got)
				}
			} else if !strings.Contains(got, note) || !strings.Contains(got, "Current project notes") {
				t.Fatalf("catalog failure discarded readable project notes: %q", got)
			}
		})
	}
}

type reviewBoundaryBadInfoTool struct{ invoked atomic.Bool }

func (*reviewBoundaryBadInfoTool) Info(context.Context) (*schema.ToolInfo, error) {
	return nil, errors.New("review tool metadata unavailable")
}

func (t *reviewBoundaryBadInfoTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	t.invoked.Store(true)
	return "", errors.New("unavailable tool must not execute")
}

func assertReviewBoundaryNoActivity(t *testing.T, e *Engine, turnID string) {
	t.Helper()
	calls, err := e.store.ListLLMCalls(turnID)
	if err != nil || len(calls) != 0 {
		t.Fatalf("rejected review invoked a model: calls=%+v error=%v", calls, err)
	}
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == KindMemoryReview {
			t.Fatalf("rejected review reported completion: %+v", event)
		}
	}
}

func failReviewBoundaryQuery(t *testing.T, e *Engine, table string, cause error) (*atomic.Int32, func()) {
	t.Helper()
	hits := new(atomic.Int32)
	name := "test:review-boundary:" + t.Name()
	query := e.store.DB().Callback().Query()
	if err := query.Before("gorm:query").Register(name, func(db *gorm.DB) {
		if db.Statement.Table == table {
			hits.Add(1)
			_ = db.AddError(cause)
		}
	}); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	restore := func() {
		once.Do(func() {
			if err := query.Remove(name); err != nil {
				t.Errorf("restore query callback: %v", err)
			}
		})
	}
	t.Cleanup(restore)
	return hits, restore
}
