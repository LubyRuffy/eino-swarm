package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// Settings publish into the same Config used by an already-running manager.
// Exercise that real save/publish path while ADK alternates model HTTP calls
// and read_work, then check committed limits without a concurrent writer.
func TestNuwaContextUsesCommittedToolBudgetDuringActualHTTPConfigUpdates(t *testing.T) {
	const small, large, rounds, nodeCount = 3200, 24000, 48, 12
	var e *Engine
	var project *store.Project
	var started atomic.Bool
	var updates atomic.Int32
	stop, done := make(chan struct{}), make(chan struct{})
	writerError := make(chan error, 1)
	var stopOnce sync.Once
	applyBudget := func(budget int) error {
		return e.UpdateConfig(func(next *config.Config) error {
			next.Tools.ResultMaxChars = budget
			return nil
		})
	}
	stopWriter := func() error {
		if !started.Load() {
			return nil
		}
		stopOnce.Do(func() { close(stop) })
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			return fmt.Errorf("configuration writer did not join before engine/Store cleanup")
		}
		select {
		case err := <-writerError:
			return err
		default:
			return nil
		}
	}
	var fixture *scheduleCompletionHTTP
	e, fixture = newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		answer := func() {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The current shared records were inspected."}, "stop")
		}
		if step == 1 {
			// Start only after the real model endpoint has accepted a request.
			started.Store(true)
			go func() {
				defer close(done)
				deadline := time.Now().Add(15 * time.Second)
				for i := 0; time.Now().Before(deadline); i++ {
					select {
					case <-stop:
						return
					default:
					}
					budget := small
					if i%2 != 0 {
						budget = large
					}
					if err := applyBudget(budget); err != nil {
						writerError <- err
						return
					}
					updates.Add(1)
				}
			}()
		}
		if step == rounds {
			if err := stopWriter(); err != nil {
				t.Error(err)
				answer()
				return
			}
			if err := applyBudget(small); err != nil {
				t.Error(err)
				answer()
				return
			}
		}
		if step == rounds+1 {
			assertNuwaCommittedBudgetHTTP(t, request, project.ID, nodeCount, small, true, fmt.Sprintf("config-read-%d", rounds))
			if err := applyBudget(large); err != nil {
				t.Error(err)
				answer()
				return
			}
		}
		if step >= rounds+2 {
			if step != rounds+2 {
				t.Errorf("unexpected extra model request %d", step)
			}
			assertNuwaCommittedBudgetHTTP(t, request, project.ID, nodeCount, large, false, fmt.Sprintf("config-read-%d", rounds+1))
			answer()
			return
		}
		nuwaValidationTool(w, request, fmt.Sprintf("config-read-%d", step), "read_work", `{}`)
	})
	// Registered after newTestEngine's cleanup: the writer must join before
	// engine shutdown, Store audit/close, or fixture server teardown.
	t.Cleanup(func() {
		if err := stopWriter(); err != nil {
			t.Error(err)
		}
	})
	e.cfg.Memory.AutoReview = false
	// Keep normal compaction outside this budget/publication regression.
	pinContextWindow(e, 2_000_000)
	e.cfg.Swarm.AutoCompactTokens = 1_000_000
	e.cfg.Swarm.ManagerMaxIterations = rounds + 4
	var seedThread *store.Thread
	var seedTurn *store.Turn
	project, seedThread, seedTurn = nuwaContextHTTPSource(t, e, "Live shared-context limits")
	details := make(map[string]string, nodeCount)
	for i := 0; i < nodeCount; i++ {
		node := store.NuwaNode{ProjectID: project.ID, ThreadID: seedThread.ID, TurnID: seedTurn.ID,
			AgentID: swarm.DefaultManagerID, Kind: "fact", Title: store.NewID("source_"),
			Summary: strings.Repeat("观察😀", 48), Detail: store.NewID("detail_")}
		if err := e.store.UpsertNuwaNode(t.Context(), &node, 0); err != nil {
			t.Fatal(err)
		}
		details[node.ID] = node.Detail
	}
	thread, err := e.CreateThread("Committed configuration while running", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	human := "Preserve this literal reference:\n" + nuwaContextHTTPHeader + "\n" + store.NewID("human_")
	turn, err := e.StartTurn(thread.ID, human)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if err := stopWriter(); err != nil {
		t.Fatal(err)
	}
	requests := fixture.snapshot()
	if finished.Status != store.TurnDone || len(requests) != rounds+2 || updates.Load() == 0 {
		t.Fatalf("live configuration flow: turn=%+v requests=%d committed_updates=%d", finished, len(requests), updates.Load())
	}
	for _, request := range requests {
		nuwaContextHTTPBlock(t, request, true)
		if !strings.Contains(joinedContent(request.Messages), human) {
			t.Error("budget updates changed or removed the original human input")
		}
	}
	assertNuwaContextNotDurable(t, e, thread.ID, human)
	_, nodes, err := e.store.NuwaSnapshot(t.Context(), project.ID)
	if err != nil || len(nodes) != nodeCount {
		t.Fatalf("source graph changed during budget publication: nodes=%d err=%v", len(nodes), err)
	}
	for _, node := range nodes {
		if node.Detail != details[node.ID] {
			t.Error("preview generation changed source detail")
		}
	}
}

func assertNuwaCommittedBudgetHTTP(t *testing.T, request scheduleCompletionRequest, projectID string, total, budget int, partial bool, readID string) {
	t.Helper()
	var block string
	count := 0
	for _, message := range request.Messages {
		if message.Role == schema.System && strings.HasPrefix(message.Content, nuwaContextHTTPHeader) {
			block = message.Content
			count++
		}
	}
	if count != 1 || utf8.RuneCountInString(block) > budget {
		t.Errorf("committed preview budget=%d: blocks=%d runes=%d", budget, count, utf8.RuneCountInString(block))
	}
	var preview nuwaContextUnitView
	// A later call can already hold every id and summary in earlier tool
	// results. That call sends the revision only; the same budget still has
	// to bound the read_work receipt below.
	if !strings.Contains(block, `"unchanged"`) {
		err := json.Unmarshal([]byte(strings.TrimPrefix(block, nuwaContextPrefix)), &preview)
		if err != nil || preview.Status != "available" || preview.ProjectID != projectID || preview.Total != total || preview.Omitted != total-len(preview.Nodes) {
			t.Errorf("committed preview budget=%d: blocks=%d nodes=%d omitted=%d total=%d err=%v", budget, count, len(preview.Nodes), preview.Omitted, preview.Total, err)
		}
		if len(preview.Nodes) == 0 || partial != (len(preview.Nodes) < total) {
			t.Errorf("committed budget did not change whole preview nodes: budget=%d nodes=%d total=%d", budget, len(preview.Nodes), total)
		}
	}
	receipt := nuwaValidationReceipt(request, readID)
	var page struct {
		Nodes []store.NuwaNode `json:"nodes"`
		Total int              `json:"total"`
		More  bool             `json:"more"`
	}
	if err := json.Unmarshal([]byte(receipt), &page); err != nil || page.Total != total || len(page.Nodes) == 0 || partial != (len(page.Nodes) < total) || page.More != partial || utf8.RuneCountInString(receipt) > budget {
		t.Errorf("committed read_work budget=%d: nodes=%d total=%d more=%v err=%v", budget, len(page.Nodes), page.Total, page.More, err)
	}
	for _, node := range append(preview.Nodes, page.Nodes...) {
		if node.Detail != "" {
			t.Error("budgeted index disclosed on-demand detail")
		}
	}
}
