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
	prompt := ManagerPrompt(set, e.Config(), "")

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
	prompt := ManagerPrompt(set, e.Config(), "## This project\n\nBe terse.\n")
	if !strings.Contains(prompt, "Be terse.") {
		t.Fatal("project extra did not land")
	}
	if i := strings.Index(prompt, "## Environment"); i < 0 || strings.LastIndex(prompt, "Be terse.") < i {
		t.Fatal("project extra must come after the generic sections")
	}
}

func TestManagerPromptPrefersProactiveDelegation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Swarm.MaxConcurrent = 6
	prompt := ManagerPrompt(&tools.Set{WorkspaceDir: "/tmp/ws"}, cfg, "")
	for _, need := range []string{
		"save time or improve quality",
		"do not wait for the human to ask",
		"Spawning one worker and then waiting",
		"two or more workers overlap",
		"Distinct role per parallel worker",
		"You can have 6 sub-agents running at once",
		"Use that budget when the work has that many independent parts",
		"Two writers on the same path conflict",
	} {
		if !strings.Contains(prompt, need) {
			t.Fatalf("missing %q:\n%s", need, prompt)
		}
	}
	for _, old := range []string{
		"you answer directly when a request is small",
		"Delegate when a request has parts that do not depend on each other",
		"Proactive multi-agent work is the default",
	} {
		if strings.Contains(prompt, old) {
			t.Fatalf("conservative policy came back: %q", old)
		}
	}
}

func TestOpenGoalPrefersProactiveDelegation(t *testing.T) {
	open := goalSection("keep going", false, false, "")
	if !strings.Contains(open, "Prefer sub-agents whenever they would save time or improve quality") {
		t.Fatalf("an open goal must keep the ultra spawn prior:\n%s", open)
	}
	if !strings.Contains(open, "Spawning one worker and then waiting is not a win") {
		t.Fatal("an open goal must not treat a one-worker wait as a swarm")
	}
	done := goalSection("keep going", true, false, "")
	if strings.Contains(done, "Prefer sub-agents") {
		t.Fatal("a completed goal must not keep instructing pursuit by swarm")
	}
}

func TestManagerPromptOmitsToolsWhenNoneAreRegistered(t *testing.T) {
	prompt := ManagerPrompt(&tools.Set{WorkspaceDir: "/tmp/ws"}, &config.Config{}, "")
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

func TestConversationExtraOmitsATranscriptCompactSummary(t *testing.T) {
	th := &store.Thread{
		Goal:           "keep going",
		CompactSummary: `Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`,
	}
	extra := conversationExtra(th, nil)
	if strings.Contains(extra, "Earlier conversation") || strings.Contains(extra, "elapsed_ms") {
		t.Fatalf("a stored dump must not be fed to the manager:\n%s", extra)
	}
	if !strings.Contains(extra, "keep going") {
		t.Fatalf("the goal vanished:\n%s", extra)
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

func TestEmptyPersonalityAddsNothingToTheManagerPrompt(t *testing.T) {
	if got := PersonalityPrompt("  \n\t"); got != "" {
		t.Fatalf("blank personality must omit the section, got %q", got)
	}
	extra := managerExtra(&config.Config{}, nil, nil)
	if extra != "" {
		t.Fatalf("empty config must add nothing, got %q", extra)
	}
	prompt := ManagerPrompt(&tools.Set{WorkspaceDir: "/tmp/ws"}, &config.Config{}, extra)
	if strings.Contains(prompt, "## Personality") {
		t.Fatal("an empty personality must not occupy the prompt")
	}
}

func TestPersonalityLandsBeforeTheProjectInstruction(t *testing.T) {
	const persona = "prefer compact replies"
	const project = "prefer exhaustive replies"
	cfg := &config.Config{Personality: config.PersonalityConfig{Instructions: persona}}
	pc := &projectContext{sections: "## This project\n\n" + project + "\n"}
	th := &store.Thread{CompactSummary: "briefing", Goal: "keep going"}
	extra := managerExtra(cfg, th, pc)
	prompt := ManagerPrompt(&tools.Set{WorkspaceDir: "/tmp/ws"}, cfg, extra)

	if !strings.Contains(prompt, persona) {
		t.Fatalf("personality missing:\n%s", prompt)
	}
	if !strings.Contains(prompt, project) {
		t.Fatalf("project missing:\n%s", prompt)
	}
	pi, pr := strings.Index(prompt, persona), strings.Index(prompt, project)
	if pi < 0 || pr < 0 || pr < pi {
		t.Fatalf("project must come after personality so it wins on a conflict:\n%s", prompt)
	}
	if i, j := strings.Index(prompt, "## Personality"), strings.Index(prompt, "## This project"); i < 0 || j < i {
		t.Fatalf("personality section must precede the project:\n%s", prompt)
	}
	if i, j := strings.Index(prompt, "## This project"), strings.Index(prompt, "## Goal"); i < 0 || j < i {
		t.Fatalf("goal must still come last:\n%s", prompt)
	}
}

func TestPersonalityWrapperIsGenericAndGrounded(t *testing.T) {
	const body = "prefer compact replies"
	got := PersonalityPrompt(body)
	wrapper, rest, ok := strings.Cut(got, body)
	if !ok {
		t.Fatalf("user text missing:\n%s", got)
	}
	if !strings.Contains(wrapper, "follow the project") {
		t.Fatalf("conflict rule missing from the wrapper:\n%s", wrapper)
	}
	if strings.TrimSpace(rest) != "" {
		t.Fatalf("nothing belongs after the user's text:\n%s", rest)
	}
	for _, leak := range []string{"summarize", "researcher", "reviewer", "notes/", "re-research", "xlsx", "spreadsheet", body} {
		if strings.Contains(strings.ToLower(wrapper), strings.ToLower(leak)) {
			t.Fatalf("the wrapper hardcodes example-specific text %q:\n%s", leak, wrapper)
		}
	}
}

func TestJoinPromptSectionsDropsBlankParts(t *testing.T) {
	got := JoinPromptSections("  ", "## A\n\nx", "", "## B\n\ny")
	if got != "## A\n\nx\n\n## B\n\ny" {
		t.Fatalf("got %q", got)
	}
}

// Sub-agents see one task. Personality is a manager preference; copying it
// onto every worker would also make a project's override impossible to
// express, because workers never see the project instruction.
func TestWorkersDoNotReceivePersonality(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Personality.Instructions = "prefer compact replies"
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
	var spawned int
	for _, ev := range events {
		if ev.Kind != swarm.NotifySpawned.String() {
			continue
		}
		spawned++
		if strings.Contains(ev.Text, "## Personality") || strings.Contains(ev.Text, "prefer compact replies") {
			t.Fatalf("a worker received the install personality:\n%s", ev.Text)
		}
	}
	if spawned == 0 {
		t.Fatal("the scripted run spawned no workers; the check never ran")
	}
}
