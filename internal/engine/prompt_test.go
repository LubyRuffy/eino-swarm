package engine

import (
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
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
	reg := e.newTurnRegistry(func(role, id string) model.BaseChatModel { return nil }, set, nil)
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
		"schedule_wake",
		"do not wait for the human to remind",
		"report_schedule",
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

func TestManagerPromptDoesNotTreatLeftoverWorkersAsSomethingToClose(t *testing.T) {
	prompt := ManagerPrompt(&tools.Set{WorkspaceDir: "/tmp/ws"}, &config.Config{}, "")
	for _, need := range []string{
		"already stopped",
		"leftover roster",
		"already_finished",
		"resume_agent",
	} {
		if !strings.Contains(prompt, need) {
			t.Fatalf("missing %q:\n%s", need, prompt)
		}
	}
	for _, leak := range []string{"canary", "金丝雀"} {
		if strings.Contains(strings.ToLower(prompt), strings.ToLower(leak)) {
			t.Fatalf("close_agent copy leaked %q", leak)
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
	if !strings.Contains(open, "A pending wake is the next turn") {
		t.Fatal("an open goal must not imply every ended wait-turn auto-continues immediately")
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

func TestManagerPromptSaysWorkersShareWorkspaceTools(t *testing.T) {
	prompt := ManagerPrompt(&tools.Set{WorkspaceDir: "/tmp/ws", Names: []string{"exec"}}, &config.Config{}, "")
	if !strings.Contains(prompt, "Your sub-agents have the same workspace tools") {
		t.Fatalf("missing worker tool surface:\n%s", prompt)
	}
	if strings.Contains(prompt, "the same set") {
		t.Fatal("the old wording implied workers had write tools")
	}
}

func TestManagerPromptWaitingCopyStaysGeneric(t *testing.T) {
	prompt := ManagerPrompt(&tools.Set{WorkspaceDir: "/tmp/ws"}, &config.Config{}, "")
	for _, need := range []string{
		"## Waiting",
		"schedule_wake",
		"do not wait for the human to remind",
		"report_schedule",
		"schedule_task",
		"about a third",
		"Extra checks",
		"Do not pad",
		"named clock time",
		"do not stretch",
		"A parallel exec",
		"Do not sleep the full remaining time",
		"does not take an id",
		"Do not invent an id",
	} {
		if !strings.Contains(prompt, need) {
			t.Fatalf("missing %q:\n%s", need, prompt)
		}
	}
	if strings.Contains(prompt, "Do not call exec to sleep") {
		t.Fatal("a parallel progress-poll sleep is allowed; the prompt must not ban it")
	}
	if strings.Contains(prompt, "can miss a beat") {
		t.Fatal("the conservative miss-a-beat cadence came back")
	}
	if strings.Contains(prompt, "sleep 150") {
		t.Fatal("a sample wait duration leaked into the prompt")
	}
	ask := strings.Index(prompt, "## Asking the human")
	wait := strings.Index(prompt, "## Waiting")
	if ask < 0 || wait < ask {
		t.Fatalf("Waiting must follow Asking the human:\n%s", prompt)
	}
	for _, leak := range []string{"deploy", "pull request", "cron job"} {
		if strings.Contains(strings.ToLower(prompt), leak) {
			t.Fatalf("Waiting copy leaked %q:\n%s", leak, prompt)
		}
	}
	if managerPromptHasCIToken(prompt) {
		t.Fatal(`Waiting copy leaked "CI"`)
	}
}

// "CI" as a token, not the letters inside "specific".
func managerPromptHasCIToken(s string) bool {
	lower := strings.ToLower(s)
	for i := 0; i+2 <= len(lower); i++ {
		if lower[i:i+2] != "ci" {
			continue
		}
		if i > 0 && managerPromptIdentByte(lower[i-1]) {
			continue
		}
		if i+2 < len(lower) && managerPromptIdentByte(lower[i+2]) {
			continue
		}
		return true
	}
	return false
}

func managerPromptIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_'
}

func TestConversationExtraPutsPlanAfterTheGoal(t *testing.T) {
	th := &store.Thread{Goal: "keep going", PlanMode: true, PlanMarkdown: "# Plan\n"}
	extra := conversationExtra(th, nil, "")
	if i, j := strings.Index(extra, "## Goal"), strings.Index(extra, "## Plan"); i < 0 || j < i {
		t.Fatalf("plan must come after the goal:\n%s", extra)
	}
}

func TestConversationExtraPutsGoalLast(t *testing.T) {
	th := &store.Thread{Goal: "keep going", CompactSummary: "briefing"}
	extra := conversationExtra(th, nil, "")
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
	extra := conversationExtra(th, nil, "")
	if strings.Contains(extra, "Earlier conversation") || strings.Contains(extra, "elapsed_ms") {
		t.Fatalf("a stored dump must not be fed to the manager:\n%s", extra)
	}
	if !strings.Contains(extra, "keep going") {
		t.Fatalf("the goal vanished:\n%s", extra)
	}
}

func TestConversationExtraListsOpenWakes(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	extra := conversationExtra(th, nil, e.scheduleLines(th.ID))
	if !strings.Contains(extra, "## Scheduled") {
		t.Fatalf("open wakes must land in extra:\n%s", extra)
	}
	if !strings.Contains(extra, sch.ID) {
		t.Fatalf("missing id:\n%s", extra)
	}
	if !strings.Contains(extra, sch.NextRunAt.UTC().Format(time.RFC3339)) {
		t.Fatalf("missing next:\n%s", extra)
	}
	if !strings.Contains(extra, "cadence=every") {
		t.Fatalf("missing cadence type:\n%s", extra)
	}
	if !strings.Contains(extra, scheduleWaitPrompt) {
		t.Fatalf("missing prompt head:\n%s", extra)
	}
}

func TestConversationExtraOmitsInactiveAndForeignWakes(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	other, _ := e.CreateThread("", "", "")
	own, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: other.ID,
		Prompt: "Continue the other wait.", EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: th.ID,
		Prompt: "Continue the independent job.", EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	}); err != nil {
		t.Fatal(err)
	}
	paused, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: "Continue the paused wait.", EveryS: 120,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(paused.ID, map[string]any{"status": store.SchedulePaused}); err != nil {
		t.Fatal(err)
	}
	extra := conversationExtra(th, nil, e.scheduleLines(th.ID))
	if !strings.Contains(extra, own.ID) {
		t.Fatalf("the active wake vanished:\n%s", extra)
	}
	if strings.Contains(extra, other.ID) {
		t.Fatalf("another conversation's wake leaked:\n%s", extra)
	}
	if strings.Contains(extra, "independent job") {
		t.Fatalf("a standalone job is not a wake on this thread:\n%s", extra)
	}
	if strings.Contains(extra, paused.ID) || strings.Contains(extra, "paused wait") {
		t.Fatalf("a paused wake must not occupy extra:\n%s", extra)
	}
}

func TestEmptyScheduleLinesAddNothing(t *testing.T) {
	th := &store.Thread{Goal: "keep going"}
	extra := conversationExtra(th, nil, "  \n")
	if strings.Contains(extra, "## Scheduled") {
		t.Fatalf("blank schedule lines must omit the heading:\n%s", extra)
	}
}

func TestScheduleLinesEmptyWhenNothingIsArmed(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if got := e.scheduleLines(th.ID); got != "" {
		t.Fatalf("an idle conversation leaked wakes:\n%s", got)
	}
	if got := e.scheduleLines(""); got != "" {
		t.Fatalf("blank id leaked %q", got)
	}
	var none *Engine
	if got := none.scheduleLines("th_x"); got != "" {
		t.Fatalf("nil engine leaked %q", got)
	}
}

func TestScheduleSectionFormatsCadenceTypes(t *testing.T) {
	next := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	got := scheduleSection([]store.Schedule{
		{ID: "sch_delay", DelayS: 30, NextRunAt: next, Prompt: scheduleWaitPrompt},
		{ID: "sch_cron", Cron: "expr", NextRunAt: next, Prompt: scheduleWaitPrompt},
		{ID: "sch_none", NextRunAt: next, Prompt: scheduleWaitPrompt},
	})
	if got == "" {
		t.Fatal("rows must emit a section")
	}
	if !strings.Contains(got, "cadence=delay") || !strings.Contains(got, "cadence=cron") {
		t.Fatalf("cadence types missing:\n%s", got)
	}
	if !strings.Contains(got, "next=2026-09-18T12:00:00Z") {
		t.Fatalf("next missing:\n%s", got)
	}
	if !strings.Contains(got, "does not take an id") || !strings.Contains(got, "cancel_schedule") {
		t.Fatalf("listed ids are for cancel, not for arming:\n%s", got)
	}
	if strings.Contains(got, "Pass one of these ids") {
		t.Fatalf("the section must not tell the model to pass an id to schedule_wake:\n%s", got)
	}
	if scheduleSection(nil) != "" {
		t.Fatal("no rows must omit the heading")
	}
}

func TestPromptHeadIsOneLineAndTruncates(t *testing.T) {
	if got := promptHead("  Continue\nthe wait.  "); got != "Continue the wait." {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("wait ", 40)
	got := promptHead(long)
	if strings.ContainsAny(got, "\n\r") {
		t.Fatal("head must be one line")
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("want truncated, got %q", got)
	}
	if r := []rune(got); len(r) != schedulePromptHeadRunes+1 {
		t.Fatalf("len=%d", len(r))
	}
}

func TestManagerExtraKeepsScheduleLinesWithoutAThread(t *testing.T) {
	extra := managerExtra(&config.Config{}, nil, nil, "## Scheduled\n\n- id=sch_x\n")
	if !strings.Contains(extra, "## Scheduled") || !strings.Contains(extra, "sch_x") {
		t.Fatalf("schedule lines dropped:\n%s", extra)
	}
}

func TestConversationExtraPutsScheduledBeforeThePlan(t *testing.T) {
	th := &store.Thread{Goal: "keep going", PlanMode: true, PlanMarkdown: "# Plan\n"}
	extra := conversationExtra(th, nil, "## Scheduled\n\n- id=sch_x next=x cadence=every prompt=head\n")
	if i, j := strings.Index(extra, "## Scheduled"), strings.Index(extra, "## Plan"); i < 0 || j < i {
		t.Fatalf("plan must stay last:\n%s", extra)
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
	extra := managerExtra(&config.Config{}, nil, nil, "")
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
	extra := managerExtra(cfg, th, pc, "")
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

func TestManagerPromptDescribesChartsWithoutASampleTask(t *testing.T) {
	prompt := ManagerPrompt(&tools.Set{WorkspaceDir: "/tmp/ws"}, &config.Config{}, "")
	for _, need := range []string{
		"language tag is chart",
		`"type":"bar|line|area|pie"`,
		"Do not invent numbers",
		"One chart per comparison",
		"prefer the chart over spelling out the same",
		"clearer reading experience",
		"do not duplicate the plotted values in text",
		"keys on each data object",
		"not axis titles",
		"markdown table",
		"emoji",
	} {
		if !strings.Contains(prompt, need) {
			t.Fatalf("missing %q:\n%s", need, prompt)
		}
	}
	for _, leak := range []string{"revenue", "sales", "month", "quarter", "gdp", "katex", "knapsack"} {
		if strings.Contains(strings.ToLower(prompt), leak) {
			t.Fatalf("chart instructions leaked a sample domain %q:\n%s", leak, prompt)
		}
	}
}

func TestManagerPromptDescribesCodeFencesAndMath(t *testing.T) {
	prompt := ManagerPrompt(&tools.Set{WorkspaceDir: "/tmp/ws"}, &config.Config{}, "")
	for _, need := range []string{
		"Fenced source must name its language",
		"$...$",
		"$$...$$",
		"not inside a code fence",
	} {
		if !strings.Contains(prompt, need) {
			t.Fatalf("missing %q:\n%s", need, prompt)
		}
	}
	for _, leak := range []string{"highlight.js", "prism", "katex", "knapsack", "latex.js"} {
		if strings.Contains(strings.ToLower(prompt), leak) {
			t.Fatalf("math/code instructions leaked %q:\n%s", leak, prompt)
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

func TestSpawnedWorkersReceiveTheProjectMemorySnapshot(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("P", "the project's own instruction", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.ProjectMemory(p.ID).Add("a note from an earlier conversation"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ProjectMemory(p.ID).WriteSkill("a-procedure", "when it applies", "steps"); err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("t", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
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
		if !strings.Contains(ev.Text, "a note from an earlier conversation") {
			t.Fatalf("worker instruction missing the notes snapshot:\n%s", ev.Text)
		}
		if !strings.Contains(ev.Text, "a-procedure") {
			t.Fatalf("worker instruction missing the skills index:\n%s", ev.Text)
		}
		if !strings.Contains(ev.Text, "Reporting back") {
			t.Fatalf("worker instruction missing the return contract:\n%s", ev.Text)
		}
		if strings.Contains(ev.Text, "the project's own instruction") {
			t.Fatalf("a worker received the project instruction:\n%s", ev.Text)
		}
		for _, write := range []string{memory.ToolMemory, memory.ToolSkillManage} {
			if strings.Contains(ev.Text, write) {
				t.Fatalf("worker instruction named the write tool %s:\n%s", write, ev.Text)
			}
		}
	}
	if spawned == 0 {
		t.Fatal("the scripted run spawned no workers; the check never ran")
	}
}

func TestManagerPromptIsGenericAndGrounded(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	set := buildTestToolset(t, e, th.ID)
	prompt := ManagerPrompt(set, e.Config(), "")

	// it must tell the agent the things only the runtime knows
	if !strings.Contains(prompt, e.WorkspaceDir(th.ID)) {
		t.Fatal("the prompt does not tell the agent where its workspace is")
	}
	for _, name := range set.Names {
		if !strings.Contains(prompt, name) {
			t.Fatalf("the prompt does not mention the %q tool the agent actually has", name)
		}
	}
	for _, tool := range []string{"spawn_agent", "send_message", "wait_agents", "close_agent", "resume_agent"} {
		if !strings.Contains(prompt, tool) {
			t.Fatalf("the prompt does not explain %s", tool)
		}
	}
	if !strings.Contains(prompt, "notified:manager") {
		t.Fatal("the manager must be told it receives a missed handoff")
	}
	if !strings.Contains(prompt, "error result") {
		t.Fatal("a missing spawn_agent field must be an error result, not a crashed turn")
	}
	for _, leak := range []string{"NodeRunError", "ToolNode"} {
		if strings.Contains(prompt, leak) {
			t.Fatalf("%q leaked into the manager prompt", leak)
		}
	}
	if !strings.Contains(prompt, "visual input") {
		t.Fatal("the prompt must say pasted images arrive on the message, not on disk")
	}
	if !strings.Contains(prompt, "lists attached files") {
		t.Fatal("the prompt must say files named on a message are this request's uploads")
	}
	if !strings.Contains(prompt, "<selected_text>") || !strings.Contains(prompt, "<user_request>") {
		t.Fatal("the prompt must say how a quoted highlight is tagged on the user message")
	}
	if !strings.Contains(prompt, "save time or improve quality") {
		t.Fatal("the manager must spawn when a swarm would save time or improve quality")
	}
	if !strings.Contains(prompt, "do not wait for the human to ask") {
		t.Fatal("delegation is proactive; the human should not have to request a swarm")
	}
	if !strings.Contains(prompt, "Spawning one worker and then waiting") {
		t.Fatal("a one-worker wait must be called out as slower, not as a swarm win")
	}
	if strings.Contains(prompt, "Proactive multi-agent work is the default") {
		t.Fatal("unconditional spawn-first came back")
	}
	if strings.Contains(prompt, "you answer directly when a request is small") {
		t.Fatal("the conservative solo-first policy came back")
	}
	if !strings.Contains(prompt, "ask_user") {
		t.Fatal("the manager must be told to ask through ask_user")
	}
	if !strings.Contains(prompt, "schedule_wake") {
		t.Fatal("the manager must be told to arm a wake instead of spinning")
	}
	if !strings.Contains(prompt, "do not wait for the human to remind") {
		t.Fatal("the manager must not ask the human to poke it when a wait is the next step")
	}
	if !strings.Contains(prompt, "about a third") {
		t.Fatal("estimated waits must be biased short, not padded to miss a beat")
	}
	if strings.Contains(prompt, "can miss a beat") {
		t.Fatal("the conservative miss-a-beat cadence came back")
	}
	if !strings.Contains(prompt, "report_schedule") {
		t.Fatal("a scheduled turn must be told to report_schedule")
	}
	for _, leak := range []string{"deploy", "pull request", "cron job"} {
		if strings.Contains(strings.ToLower(prompt), leak) {
			t.Fatalf("the prompt hardcodes example-specific text %q", leak)
		}
	}
	// "CI" as a token, not the letters inside "specific".
	if managerPromptHasCIToken(prompt) {
		t.Fatal(`the prompt hardcodes example-specific text "CI"`)
	}
	if strings.Contains(strings.ToLower(prompt), "sandbox") || strings.Contains(prompt, "沙箱") {
		t.Fatal("the prompt must not call the workspace a sandbox")
	}
	if !strings.Contains(prompt, "language tag is chart") {
		t.Fatal("the manager must be told when to emit a chart fence")
	}
	if !strings.Contains(prompt, "Do not invent numbers") {
		t.Fatal("a chart must not become a place to fabricate values")
	}
	if !strings.Contains(prompt, "prefer the chart over spelling out the same") {
		t.Fatal("the manager must prefer a chart over restating the series as text")
	}
	if !strings.Contains(prompt, "do not duplicate the plotted values in text") {
		t.Fatal("a chart must replace a number dump, not sit next to one")
	}
	// and it must not smuggle in a particular task
	for _, leak := range []string{"summarize", "researcher", "reviewer", "notes/", "re-research", "xlsx", "spreadsheet", "revenue", "sales"} {
		if strings.Contains(strings.ToLower(prompt), strings.ToLower(leak)) {
			t.Fatalf("the prompt hardcodes example-specific text %q", leak)
		}
	}
}
