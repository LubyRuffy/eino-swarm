package engine

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A second acceptance check must be refused at the tool boundary while the
// first worker is still running. Prompt wording is not the gate: the host
// counts one verify and one fix, and a same-role steer does not get a second
// model request.
func TestNuwaVerificationBudgetRefusesAnotherCheckBeforeAWorkerStarts(t *testing.T) {
	const (
		verifyTask = "Verify the recorded deliverable."
		fixTask    = "Repair the recorded deliverable."
		workTask   = "Implement the remaining assigned work."
	)
	var releaseOnce sync.Once
	release := make(chan struct{})
	releaseVerify := func() { releaseOnce.Do(func() { close(release) }) }
	var hits struct {
		mu                sync.Mutex
		verify, fix, work int
	}
	count := func(kind string) {
		hits.mu.Lock()
		defer hits.mu.Unlock()
		switch kind {
		case "verify":
			hits.verify++
		case "fix":
			hits.fix++
		case "work":
			hits.work++
		}
	}
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, _ int, request scheduleCompletionRequest) {
		if !nuwaBudgetPlannerRequest(request) {
			text := joinedContent(request.Messages)
			switch {
			case strings.Contains(text, verifyTask):
				count("verify")
				select {
				case <-release:
				case <-r.Context().Done():
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The recorded deliverable was checked."}, "stop")
			case strings.Contains(text, fixTask):
				count("fix")
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The recorded deliverable was repaired."}, "stop")
			case strings.Contains(text, workTask):
				count("work")
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The assigned implementation was finished."}, "stop")
			default:
				t.Errorf("unexpected worker request: %s", text)
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "Unexpected work was refused."}, "stop")
			}
			return
		}
		if nuwaValidationReceipt(request, "budget-record") == "" {
			nuwaBudgetAssertSpawnSchema(t, request)
			nuwaValidationTool(w, request, "budget-record", "record_work", `{"kind":"fact","title":"Recorded deliverable","summary":"The assigned result is recorded."}`)
			return
		}
		var recorded struct {
			Node struct {
				ID string `json:"id"`
			} `json:"node"`
		}
		if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "budget-record")), &recorded); err != nil || recorded.Node.ID == "" {
			t.Errorf("record_work receipt: %s", nuwaValidationReceipt(request, "budget-record"))
			releaseVerify()
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The shared record was unavailable."}, "stop")
			return
		}
		target := recorded.Node.ID
		if nuwaValidationReceipt(request, "budget-verify") == "" {
			nuwaBudgetSpawn(w, request, "budget-verify", "checker", verifyTask, "verify", target)
			return
		}
		verifyReceipt := nuwaValidationReceipt(request, "budget-verify")
		verifyID := nuwaBudgetAgentID(verifyReceipt)
		if verifyID == "" || strings.Contains(verifyReceipt, `"error"`) {
			t.Errorf("first verify was refused: %s", verifyReceipt)
		}
		if nuwaValidationReceipt(request, "budget-verify-again") == "" {
			nuwaBudgetSpawn(w, request, "budget-verify-again", "checker", verifyTask, "verify", target)
			return
		}
		again := nuwaValidationReceipt(request, "budget-verify-again")
		if nuwaBudgetAgentID(again) != "" || !strings.Contains(again, "already used its one verify") {
			t.Errorf("second verify started another worker: %s", again)
		}
		releaseVerify()
		if nuwaValidationReceipt(request, "budget-fix") == "" {
			nuwaBudgetSpawn(w, request, "budget-fix", "repairer", fixTask, "fix", target)
			return
		}
		fixID := nuwaBudgetAgentID(nuwaValidationReceipt(request, "budget-fix"))
		if fixID == "" {
			t.Errorf("first fix was refused: %s", nuwaValidationReceipt(request, "budget-fix"))
		}
		if nuwaValidationReceipt(request, "budget-fix-again") == "" {
			nuwaBudgetSpawn(w, request, "budget-fix-again", "repairer", fixTask, "fix", target)
			return
		}
		fixAgain := nuwaValidationReceipt(request, "budget-fix-again")
		if nuwaBudgetAgentID(fixAgain) != "" || !strings.Contains(fixAgain, "already used its one fix") {
			t.Errorf("second fix started another worker: %s", fixAgain)
		}
		if nuwaValidationReceipt(request, "budget-work") == "" {
			nuwaBudgetSpawn(w, request, "budget-work", "implementer", workTask, "", "")
			return
		}
		workID := nuwaBudgetAgentID(nuwaValidationReceipt(request, "budget-work"))
		if workID == "" || strings.Contains(nuwaValidationReceipt(request, "budget-work"), `"error"`) {
			t.Errorf("ordinary work was charged against the verification allowance: %s", nuwaValidationReceipt(request, "budget-work"))
		}
		body, err := json.Marshal(map[string]any{"agent_ids": []string{verifyID, fixID, workID}, "timeout_s": 5})
		if err != nil {
			t.Error(err)
		}
		// wait_agents settles only the generation it actually observes. A worker
		// that finishes after the first receipt still needs another wait.
		if nuwaValidationReceipt(request, "budget-wait") == "" {
			nuwaValidationTool(w, request, "budget-wait", "wait_agents", string(body))
			return
		}
		if nuwaValidationReceipt(request, "budget-wait-again") == "" {
			nuwaValidationTool(w, request, "budget-wait-again", "wait_agents", string(body))
			return
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The unmet criterion remains after the allowed check and repair."}, "stop")
	})
	e.cfg.Memory.Enabled = false
	e.cfg.Memory.AutoReview = false
	project, err := e.CreateProject("Verification allowance", "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("One check and one repair", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Record the deliverable, check it once, and repair it once.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	releaseVerify()
	if finished.Status != store.TurnDone {
		t.Fatalf("verification refusal failed the turn: %+v", finished)
	}
	hits.mu.Lock()
	defer hits.mu.Unlock()
	if hits.verify != 1 || hits.fix != 1 || hits.work != 1 {
		t.Fatalf("verification budget changed worker model requests: verify=%d fix=%d work=%d", hits.verify, hits.fix, hits.work)
	}
}

func nuwaBudgetPlannerRequest(request scheduleCompletionRequest) bool {
	for _, message := range request.Messages {
		if message.Role == schema.System && strings.Contains(message.Content, "The host allows one verify and one fix") {
			return true
		}
	}
	return false
}

func nuwaBudgetAssertSpawnSchema(t *testing.T, request scheduleCompletionRequest) {
	t.Helper()
	for _, tool := range request.Tools {
		if tool.Function.Name != "spawn_agent" {
			continue
		}
		if _, ok := tool.Function.Parameters.Properties["intent"]; !ok {
			t.Error("Nuwa spawn_agent hid intent from the model")
		}
		if _, ok := tool.Function.Parameters.Properties["target"]; !ok {
			t.Error("Nuwa spawn_agent hid target from the model")
		}
		if !strings.Contains(tool.Function.Description, "one verify and one fix") {
			t.Error("Nuwa spawn_agent description does not say the host enforces the allowance")
		}
		return
	}
	t.Error("planner request lost spawn_agent")
}

func nuwaBudgetSpawn(w http.ResponseWriter, request scheduleCompletionRequest, id, role, task, intent, target string) {
	args := map[string]any{"role": role, "task": task}
	if intent != "" {
		args["intent"] = intent
		args["target"] = target
	}
	body, err := json.Marshal(args)
	if err != nil {
		http.Error(w, "invalid fixture", http.StatusInternalServerError)
		return
	}
	nuwaValidationTool(w, request, id, "spawn_agent", string(body))
}

func nuwaBudgetAgentID(receipt string) string {
	var body struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(receipt), &body); err != nil {
		return ""
	}
	return body.AgentID
}
