package engine

import (
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/model"
)

func TestManagerPromptIncludesTheLiveHost(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	set := buildTestToolset(t, e, th.ID)
	prompt := managerPrompt(set, e.Config(), "")

	if !strings.Contains(prompt, "## Environment") {
		t.Fatal("the manager prompt has no Environment section")
	}
	if !strings.Contains(prompt, goruntime.GOOS) {
		t.Fatalf("the manager prompt does not name this OS %q", goruntime.GOOS)
	}
	if !strings.Contains(prompt, time.Now().Format("2006-01-02")) {
		t.Fatal("the manager prompt does not name today's date")
	}
	if sh := hostShell(); !strings.Contains(prompt, sh) {
		t.Fatalf("the manager prompt does not name the live shell %q", sh)
	}
	for _, leak := range []string{"find -printf", "-printf", "bf_local_check"} {
		if strings.Contains(prompt, leak) {
			t.Fatalf("the prompt hardcodes the motivating example %q", leak)
		}
	}
}

func TestTurnRegistryHandsWorkersTheHostEnvironment(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	set := buildTestToolset(t, e, th.ID)
	reg := e.newTurnRegistry(func(role, id string) model.BaseChatModel { return nil }, set)
	if !strings.Contains(reg.WorkerPreamble, goruntime.GOOS) {
		t.Fatal("workers would not know which OS they are on")
	}
	if !strings.Contains(reg.WorkerPreamble, time.Now().Format("2006-01-02")) {
		t.Fatal("workers would not know today's date")
	}
	if len(reg.SubAgentTools) == 0 {
		t.Fatal("the helper dropped the toolset")
	}
}

func TestManagerPromptAppendsProjectExtraLast(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	set := buildTestToolset(t, e, th.ID)
	prompt := managerPrompt(set, e.Config(), "## This project\n\nBe terse.\n")
	if !strings.Contains(prompt, "Be terse.") {
		t.Fatal("project extra did not land")
	}
	if i := strings.Index(prompt, "## Environment"); i < 0 || strings.LastIndex(prompt, "Be terse.") < i {
		t.Fatal("project extra must come after the generic sections")
	}
}

func TestManagerPromptOmitsToolsWhenNoneAreRegistered(t *testing.T) {
	prompt := managerPrompt(&tools.Set{WorkspaceDir: "/tmp/ws"}, &config.Config{}, "")
	if !strings.Contains(prompt, "/tmp/ws") {
		t.Fatal("workspace missing")
	}
	if strings.Contains(prompt, "## Tools") {
		t.Fatal("an empty toolset must not advertise tools")
	}
}

func TestConversationExtraPutsGoalLast(t *testing.T) {
	th := &store.Thread{Goal: "keep going", CompactSummary: "briefing"}
	extra := conversationExtra(th, nil)
	if !strings.Contains(extra, "## Earlier conversation") || !strings.Contains(extra, "briefing") {
		t.Fatalf("missing briefing:\n%s", extra)
	}
	if !strings.Contains(extra, "## Goal") || !strings.Contains(extra, "keep going") {
		t.Fatalf("missing goal:\n%s", extra)
	}
	if i, j := strings.Index(extra, "## Earlier"), strings.Index(extra, "## Goal"); i < 0 || j < i {
		t.Fatalf("goal must come after the briefing so it is the last thing read:\n%s", extra)
	}
}

// A reload, zwai trace, and the agent chrome all read this row. If it only
// stores the role, the prompt viewer has nothing to show.
func TestSpawnedEventRecordsTheWorkerInstruction(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	asked := "compare the two inputs"
	turn, err := e.StartTurn(th.ID, asked)
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)

	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var spawned []store.Event
	for _, ev := range events {
		if ev.Kind == swarm.NotifySpawned.String() {
			spawned = append(spawned, ev)
		}
	}
	if len(spawned) != 2 {
		t.Fatalf("want two spawned workers, got %d", len(spawned))
	}
	for _, ev := range spawned {
		if ev.Role == "" {
			t.Fatalf("spawned missing role: %+v", ev)
		}
		if ev.Text == ev.Role {
			t.Fatalf("spawned text is still the role; instruction was not recorded: %+v", ev)
		}
		if !strings.Contains(ev.Text, "## Environment") {
			t.Fatalf("worker instruction missing the host snapshot:\n%s", ev.Text)
		}
		if !strings.Contains(ev.Text, asked) {
			t.Fatalf("worker instruction missing the task it was spawned with:\n%s", ev.Text)
		}
	}
}
