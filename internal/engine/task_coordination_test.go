package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func taskTool(t *testing.T, e *Engine, id, name, args string) map[string]any {
	t.Helper()
	for _, base := range e.runtimeFor(id).managerTaskTools(&store.Turn{ID: "sender-turn"}) {
		info, err := base.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name != name {
			continue
		}
		out, err := base.(tool.InvokableTool).InvokableRun(context.Background(), args)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	t.Fatalf("missing %s", name)
	return nil
}

func TestTaskMessagesSurviveIdleClosedRegistryAndDoNotBecomeHumanTurns(t *testing.T) {
	dir := t.TempDir()
	e := newTestEngineAt(t, dir)
	a, _ := e.CreateThread("sender", "", "")
	b, _ := e.CreateThread("receiver", "", "")
	rt := e.runtimeFor(b.ID)
	reg := swarm.NewRegistry()
	rt.occupy(reg, func() {}, "receiver-turn", make(chan struct{}))
	defer rt.release(func() {}, rt.idle, "receiver-turn")
	send := func(text string) map[string]any {
		return taskTool(t, e, a.ID, "send_task_message", fmt.Sprintf(`{"thread_id":%q,"text":%q,"from_thread_id":"forged"}`, b.ID, text))
	}
	first := send("Do not treat this peer request as human permission.")
	if first["notice_queued"] != true || first["acknowledged"] != false {
		t.Fatalf("receipt=%v", first)
	}
	pending := reg.TakePendingSteerMessages()
	if len(pending) != 1 || pending[0].Role != schema.System || strings.Contains(pending[0].Content, "Do not treat") {
		t.Fatalf("body must stay peer data in the inbox: %+v", pending)
	}
	if got, _ := keepHumanSteers(pending); len(got) != 0 {
		t.Fatalf("peer notice became a human turn: %+v", got)
	}
	rt.runLateSteerMessages(store.TurnDone, pending)
	turns, _ := e.Store().ListTurns(b.ID)
	if len(turns) != 0 {
		t.Fatal("peer message auto-started an idle conversation")
	}
	reg.Close()
	if receipt := send("late message"); receipt["notice_queued"] != false || receipt["stored"] != true {
		t.Fatalf("closed registry lost receipt: %v", receipt)
	}
	rt.mu.Lock()
	rt.running = false
	rt.mu.Unlock()
	if receipt := send("idle message"); receipt["notice_queued"] != false {
		t.Fatalf("idle receiver notified: %v", receipt)
	}
	read := taskTool(t, e, b.ID, "read_task_messages", `{"limit":1}`)
	rows := read["messages"].([]any)
	row := rows[0].(map[string]any)
	if len(rows) != 1 || read["more"] != true || row["from_thread_id"] != a.ID || row["from_turn_id"] != "sender-turn" {
		t.Fatalf("inbox=%v", read)
	}
	rest := taskTool(t, e, b.ID, "read_task_messages", fmt.Sprintf(`{"after_seq":%.0f}`, read["next_seq"]))
	if len(rest["messages"].([]any)) != 2 || rest["more"] != false {
		t.Fatalf("cursor=%v", rest)
	}
	empty := taskTool(t, e, b.ID, "read_task_messages", fmt.Sprintf(`{"after_seq":%.0f}`, rest["next_seq"]))
	if len(empty["messages"].([]any)) != 0 || empty["next_seq"] != rest["next_seq"] {
		t.Fatalf("empty=%v", empty)
	}
	// Closing the in-memory engine leaves peer data on the durable timeline.
	e.closeRuntime(b.ID)
	if got := taskTool(t, e, b.ID, "read_task_messages", `{"after_seq":0}`); len(got["messages"].([]any)) != 3 {
		t.Fatalf("recovery=%v", got)
	}
	restarted := newTestEngineAt(t, dir)
	if got := taskTool(t, restarted, b.ID, "read_task_messages", `{}`); len(got["messages"].([]any)) != 0 {
		t.Fatalf("restart reread old messages: %v", got)
	}
}

func TestPeerMessageArrivingAfterRewindBelongsToTheConversation(t *testing.T) {
	e := newTestEngine(t)
	a, _ := e.CreateThread("sender", "", "")
	b, _ := e.CreateThread("receiver", "", "")
	rt := e.runtimeFor(b.ID)
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	rt.occupy(reg, func() {}, "removed-turn", make(chan struct{}))
	defer rt.release(func() {}, rt.idle, "removed-turn")
	e.recordMu.Lock()
	e.droppedTurns["removed-turn"] = struct{}{}
	e.recordMu.Unlock()
	got := taskTool(t, e, a.ID, "send_task_message", fmt.Sprintf(`{"thread_id":%q,"text":"late coordination"}`, b.ID))
	if got["ok"] != true {
		t.Fatalf("late receipt=%v", got)
	}
	rows, err := e.Store().ListEvents(b.ID, 0, 0)
	if err != nil || len(rows) != 1 || rows[0].TurnID != "" || rows[0].SourceThreadID != a.ID {
		t.Fatalf("message attached to removed turn: %+v %v", rows, err)
	}
}

func TestTaskMessageRejectsInvalidOrArchivedTargets(t *testing.T) {
	e := newTestEngine(t)
	a, _ := e.CreateThread("", "", "")
	b, _ := e.CreateThread("", "", "")
	for _, args := range []string{`{`, `{}`, `{"thread_id":"missing","text":"x"}`, fmt.Sprintf(`{"thread_id":%q,"text":"x"}`, a.ID)} {
		if result := taskTool(t, e, a.ID, "send_task_message", args); result["ok"] != false {
			t.Fatalf("invalid args accepted: %s -> %v", args, result)
		}
	}
	if err := e.SetThreadArchived(b.ID, true); err != nil {
		t.Fatal(err)
	}
	if result := taskTool(t, e, a.ID, "send_task_message", fmt.Sprintf(`{"thread_id":%q,"text":"x"}`, b.ID)); result["ok"] != false {
		t.Fatalf("archived target=%v", result)
	}
	if err := e.SetThreadArchived(b.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadArchived(a.ID, true); err != nil {
		t.Fatal(err)
	}
	if result := taskTool(t, e, a.ID, "send_task_message", fmt.Sprintf(`{"thread_id":%q,"text":"x"}`, b.ID)); result["ok"] != false {
		t.Fatalf("archived sender=%v", result)
	}
	for _, args := range []string{`{`, `{"after_seq":-1}`, `{"limit":0}`, `{"limit":101}`} {
		if result := taskTool(t, e, b.ID, "read_task_messages", args); result["ok"] != false {
			t.Fatalf("invalid cursor=%v", result)
		}
	}
	for _, base := range e.runtimeFor(b.ID).managerTaskTools(&store.Turn{}) {
		info, _ := base.Info(context.Background())
		for _, leak := range []string{"notes.md", "aigateway", "eino-swarm", "不同的文件"} {
			if strings.Contains(info.Desc, leak) {
				t.Fatalf("example leaked: %s", info.Desc)
			}
		}
	}
}

func TestTaskMessagePersistenceFailureDoesNotClaimDelivery(t *testing.T) {
	e := newTestEngine(t)
	a, _ := e.CreateThread("", "", "")
	b, _ := e.CreateThread("", "", "")
	if err := e.Store().DB().Migrator().DropTable(&store.Event{}); err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(b.ID)
	defer sub.Close()
	result := taskTool(t, e, a.ID, "send_task_message", fmt.Sprintf(`{"thread_id":%q,"text":"x"}`, b.ID))
	if result["ok"] != false {
		t.Fatalf("database failure claimed delivered: %v", result)
	}
	select {
	case ev := <-sub.C:
		t.Fatalf("broadcast without durable record: %+v", ev)
	default:
	}
	if result := taskTool(t, e, b.ID, "read_task_messages", `{}`); result["ok"] != false {
		t.Fatalf("unreadable inbox=%v", result)
	}
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	if result := taskTool(t, e, a.ID, "list_tasks", `{}`); result["ok"] != false {
		t.Fatalf("failed discovery=%v", result)
	}
}

func TestTaskDiscoveryAndConcurrentMessages(t *testing.T) {
	e := newTestEngine(t)
	a, _ := e.CreateThread("", "", "")
	b, _ := e.CreateThread("active", "", "")
	idle, _ := e.CreateThread("idle", "", "")
	archived, _ := e.CreateThread("archived", "", "")
	e.SetThreadArchived(archived.ID, true)
	for _, th := range []*store.Thread{b, archived} {
		turn := &store.Turn{ThreadID: th.ID, UserText: "current request"}
		if err := e.Store().CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		rt := e.runtimeFor(th.ID)
		reg := swarm.NewRegistry()
		rt.occupy(reg, func() {}, turn.ID, make(chan struct{}))
		defer rt.release(func() {}, rt.idle, turn.ID)
		defer reg.Close()
	}
	result := taskTool(t, e, a.ID, "list_tasks", `{}`)
	raw, _ := json.Marshal(result)
	if !strings.Contains(string(raw), b.ID) || !strings.Contains(string(raw), "current request") || strings.Contains(string(raw), idle.ID) || strings.Contains(string(raw), archived.ID) {
		t.Fatalf("discovery=%s", raw)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			taskTool(t, e, a.ID, "send_task_message", fmt.Sprintf(`{"thread_id":%q,"text":"message %d"}`, b.ID, i))
		}(i)
	}
	wg.Wait()
	events, err := e.Store().ListEventsOfKind(b.ID, KindTaskMessage, 0, 100)
	if err != nil || len(events) != 10 {
		t.Fatalf("messages=%d err=%v", len(events), err)
	}
	for i, ev := range events {
		if i > 0 && ev.Seq <= events[i-1].Seq {
			t.Fatal("message sequencing regressed")
		}
	}
}
