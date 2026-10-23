package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestAnswerAdmissionWriteFailureLogsItsOriginalCauseAndTurn(t *testing.T) {
	e := newTestEngine(t)
	var logs bytes.Buffer
	e.log = slog.New(slog.NewJSONHandler(&logs, nil))
	th, _ := e.CreateThread("admission diagnostics", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	idle := make(chan struct{})
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		t.Fatal("runtime ownership not established")
	}
	t.Cleanup(func() { rt.release(cancel, idle, turn.ID); reg.Close() })
	if err := e.store.DB().Exec("CREATE TRIGGER refuse_logged_answer BEFORE INSERT ON events WHEN NEW.kind='agent_message' BEGIN SELECT RAISE(ABORT, 'admission fixture unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	failure := e.admitScheduleAnswer(th.ID, turn.ID, nil, []store.Event{{Kind: "agent_message", AgentID: "manager", Text: "Candidate text must not be copied into diagnostics."}})
	if failure == nil || failure.Code != "answer_storage_failed" {
		t.Fatalf("wrong failure category: %+v", failure)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
		t.Fatalf("missing original storage diagnostic: %s error=%v", logs.String(), err)
	}
	detail, _ := record["err"].(string)
	if record["level"] != "WARN" || record["thread"] != th.ID || record["turn"] != turn.ID || record["code"] != "answer_storage_failed" || !strings.Contains(detail, "admission fixture unavailable") {
		t.Fatalf("diagnostic lost cause or attribution: %+v", record)
	}
	if strings.Contains(logs.String(), "Candidate text") {
		t.Fatalf("diagnostic copied unadmitted prose: %s", logs.String())
	}
}
