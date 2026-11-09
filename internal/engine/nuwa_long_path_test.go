package engine

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A long chain is the case the shared graph is for: later work needs every
// earlier accepted result, and a folded conversation does not still contain
// those results. Notes are the default arm's path; they have a much smaller
// budget. The graph has to keep the whole chain, including its first step.
func TestLongPathRemainsVisibleAfterTheConversationIsFolded(t *testing.T) {
	const stages = 80
	e := newManualCompactEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	project, err := e.CreateProject("long path", "Continue a chain of accepted results.", t.TempDir(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("long path", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(thread.ID, "accept "+pathToken(0))
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	second, err := e.StartTurn(thread.ID, "List every accepted result in stage order.")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, second.ID)

	notes := e.ProjectMemory(project.ID)
	notesHeld := 0
	for i := 0; i < stages; i++ {
		if _, _, err := notes.Add("stage " + pathToken(i)); err != nil {
			break
		}
		notesHeld++
	}
	var previous store.NuwaNode
	for i := 0; i < stages; i++ {
		node := store.NuwaNode{
			ProjectID: project.ID, Kind: "fact", Title: fmt.Sprintf("stage-%03d", i),
			Summary: pathToken(i), Status: "reported",
			ThreadID: thread.ID, TurnID: first.ID, AgentID: "manager",
		}
		if i > 0 {
			node.DependsOn = []string{previous.ID}
			node.DependencyVersions = map[string]int64{previous.ID: previous.Version}
		}
		if err := e.store.UpsertNuwaNode(t.Context(), &node, 0); err != nil {
			t.Fatalf("record stage %d: %v", i, err)
		}
		previous = node
	}
	const briefing = "Earlier stages were reviewed. Continue from the recorded path."
	if err := e.store.UpdateThread(thread.ID, map[string]any{"session_memory": briefing}); err != nil {
		t.Fatal(err)
	}
	folded, err := e.CompactThread(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	history, err := e.replayHistory(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var replay strings.Builder
	for _, message := range history {
		if message != nil {
			replay.WriteString(message.Content)
			replay.WriteString("\n")
		}
	}
	chat := folded.CompactSummary + "\n" + replay.String()
	if strings.Contains(chat, "path-token-") {
		t.Fatalf("folded conversation still holds the path:\n%s", chat)
	}
	pc, err := e.projectContextFor(folded)
	if err != nil {
		t.Fatal(err)
	}
	noteText := pc.promptSections()
	noteHits := countPathTokens(noteText, stages)
	if noteHits == 0 || noteHits == stages {
		t.Fatalf("notes held %d/%d steps; the comparison needs a partial note path", noteHits, stages)
	}
	revision, nodes, err := e.store.NuwaSnapshot(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := renderNuwaContext(project.ID, revision, nodes, "List every accepted result in stage order.", e.Config().Tools.ToolResultLimit())
	if err != nil {
		t.Fatal(err)
	}
	graphHits := countPathTokens(preview, stages)
	if graphHits != stages || !strings.Contains(preview, pathToken(0)) {
		t.Fatalf("shared graph delivered %d/%d steps after the conversation was folded; notes delivered %d; first step present=%v",
			graphHits, stages, noteHits, strings.Contains(preview, pathToken(0)))
	}
}

func pathToken(i int) string {
	return fmt.Sprintf("path-token-%03d-%08x", i, uint32(i)*0x9E3779B1)
}

func countPathTokens(text string, stages int) int {
	hits := 0
	for i := 0; i < stages; i++ {
		if strings.Contains(text, pathToken(i)) {
			hits++
		}
	}
	return hits
}

func TestNuwaPathFollowsDependenciesAndDropsOnlyAnOversizedSummary(t *testing.T) {
	early := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	later := early.Add(time.Hour)
	parent := store.NuwaNode{ID: "parent", Title: "parent", Summary: "parent-token", Status: "reported", CreatedAt: later}
	child := store.NuwaNode{ID: "child", Title: "child", Summary: "child-token", Status: "reported", DependsOn: []string{parent.ID}, CreatedAt: early}
	ordered := nuwaPathOrder([]store.NuwaNode{child, parent})
	if len(ordered) != 2 || ordered[0].ID != parent.ID || ordered[1].ID != child.ID {
		t.Fatalf("a later prerequisite must stay ahead of the step that needs it: %+v", ordered)
	}
	var cycleA, cycleB store.NuwaNode
	cycleA.ID, cycleB.ID = "cycle-a", "cycle-b"
	cycleA.DependsOn, cycleB.DependsOn = []string{cycleB.ID}, []string{cycleA.ID}
	cycled := nuwaPathOrder([]store.NuwaNode{cycleB, cycleA})
	if len(cycled) != 2 {
		t.Fatalf("a dependency cycle dropped a recorded step: %+v", cycled)
	}
	huge := store.NuwaNode{ID: "huge", Title: "huge", Summary: strings.Repeat("大", 8000), Status: "reported", UpdatedAt: later}
	small := store.NuwaNode{ID: "small", Title: "small", Summary: "kept-token", Status: "reported", UpdatedAt: early}
	fitted, err := renderNuwaContext("project", 1, []store.NuwaNode{small}, "", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	text, err := renderNuwaContext("project", 1, []store.NuwaNode{huge, small}, "", utf8.RuneCountInString(fitted))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, huge.Summary) || !strings.Contains(text, small.ID) {
		t.Fatalf("oversized summary displaced a node that fits:\n%s", text)
	}
	ancestor := store.NuwaNode{ID: "ancestor", Title: "ancestor", Summary: strings.Repeat("大", 8000), Status: "reported", UpdatedAt: early}
	dependent := store.NuwaNode{ID: "dependent", Title: "dependent", Summary: "kept-token", Status: "reported", DependsOn: []string{ancestor.ID}, UpdatedAt: later}
	kept, err := renderNuwaContext("project", 1, []store.NuwaNode{dependent}, "", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := renderNuwaContext("project", 1, []store.NuwaNode{ancestor, dependent}, "", utf8.RuneCountInString(kept))
	if err != nil || strings.Contains(blocked, ancestor.Summary) || !strings.Contains(blocked, dependent.ID) {
		t.Fatalf("a prerequisite that cannot fit removed the step that can:\n%s err=%v", blocked, err)
	}
}

// The folded chain has to change the answer, not only the preview helper.
// Both arms get the same notes, briefing and question. The scripted model
// copies the path tokens present in that request and does not read storage.
func TestLongPathAnswersFollowTheContextThatSurvivesFolding(t *testing.T) {
	ordinary := solveLongPath(t, false)
	shared := solveLongPath(t, true)
	t.Logf("folded conversation held no path tokens; notes answered %d/%d; shared graph answered %d/%d", ordinary.answerHits, longPathStages, shared.answerHits, longPathStages)
	if ordinary.noteHits == 0 || ordinary.noteHits == longPathStages || ordinary.noteHits != shared.noteHits {
		t.Fatalf("notes were not the same partial path: ordinary=%d shared=%d", ordinary.noteHits, shared.noteHits)
	}
	if ordinary.solveCalls != 1 || shared.solveCalls != 1 {
		t.Fatalf("solve requests ordinary=%d shared=%d", ordinary.solveCalls, shared.solveCalls)
	}
	if ordinary.sawGraph || !shared.sawGraph {
		t.Fatalf("graph injection ordinary=%v shared=%v", ordinary.sawGraph, shared.sawGraph)
	}
	if ordinary.requestHits != ordinary.noteHits || ordinary.answerHits != ordinary.noteHits {
		t.Fatalf("default answer did not follow its notes: request=%d answer=%d notes=%d", ordinary.requestHits, ordinary.answerHits, ordinary.noteHits)
	}
	if shared.requestHits != longPathStages || shared.answerHits != longPathStages {
		t.Fatalf("shared graph answer missed the chain: request=%d answer=%d", shared.requestHits, shared.answerHits)
	}
	if strings.Contains(ordinary.answer, pathToken(longPathStages-1)) || !strings.Contains(shared.answer, pathToken(0)) || !strings.Contains(shared.answer, pathToken(longPathStages-1)) {
		t.Fatalf("tail stayed with the wrong arm:\nordinary=%s\nshared=%s", ordinary.answer, shared.answer)
	}
}

const longPathStages = 80

const longPathQuestion = "List every accepted result in stage order."

const longPathBriefing = "Earlier stages were reviewed. Continue from the recorded path."

type longPathOutcome struct {
	noteHits, requestHits, answerHits, solveCalls int
	sawGraph                                      bool
	answer                                        string
}

func solveLongPath(t *testing.T, nuwa bool) longPathOutcome {
	t.Helper()
	var mu sync.Mutex
	var out longPathOutcome
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		text := joinedContent(request.Messages)
		if !strings.Contains(text, longPathQuestion) {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "accepted"}, "stop")
			return
		}
		var listed strings.Builder
		for i := 0; i < longPathStages; i++ {
			token := pathToken(i)
			if strings.Contains(text, token) {
				if listed.Len() > 0 {
					listed.WriteByte('\n')
				}
				listed.WriteString(token)
			}
		}
		mu.Lock()
		out.solveCalls++
		out.requestHits = pathPrefixHits(text, longPathStages)
		out.sawGraph = strings.Contains(text, nuwaContextPrefix)
		mu.Unlock()
		writeContextFlowReply(w, request.Stream, map[string]any{"content": listed.String()}, "stop")
	})
	e.sessions.stop()
	if !e.sessions.wait(5 * time.Second) {
		t.Fatal("long-path fixture has an active background briefing")
	}
	e.cfg.Memory.AutoReview = false
	e.cfg.Swarm.CompactKeepMessages = 2
	project, err := e.CreateProject("long path", "Continue a chain of accepted results.", t.TempDir(), true, nuwa)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("long path", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(thread.ID, "accept "+pathToken(0))
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	second, err := e.StartTurn(thread.ID, "Continue from the recorded chain.")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, second.ID)
	notes := e.ProjectMemory(project.ID)
	for i := 0; i < longPathStages; i++ {
		if _, _, err := notes.Add("stage " + pathToken(i)); err != nil {
			break
		}
		out.noteHits++
	}
	var previous store.NuwaNode
	for i := 0; i < longPathStages; i++ {
		node := store.NuwaNode{
			ProjectID: project.ID, Kind: "fact", Title: fmt.Sprintf("stage-%03d", i),
			Summary: pathToken(i), Status: "reported",
			ThreadID: thread.ID, TurnID: first.ID, AgentID: "manager",
		}
		if i > 0 {
			node.DependsOn = []string{previous.ID}
			node.DependencyVersions = map[string]int64{previous.ID: previous.Version}
		}
		if err := e.store.UpsertNuwaNode(t.Context(), &node, 0); err != nil {
			t.Fatalf("record stage %d: %v", i, err)
		}
		previous = node
	}
	if err := e.store.UpdateThread(thread.ID, map[string]any{"session_memory": longPathBriefing}); err != nil {
		t.Fatal(err)
	}
	folded, err := e.CompactThread(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	history, err := e.replayHistory(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	var replay strings.Builder
	for _, message := range history {
		if message != nil {
			replay.WriteString(message.Content)
			replay.WriteString("\n")
		}
	}
	if strings.Contains(folded.CompactSummary+"\n"+replay.String(), "path-token-") {
		t.Fatal("folded conversation still holds the path")
	}
	turn, err := e.StartTurn(thread.ID, longPathQuestion)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("solve turn ended %s: %s", finished.Status, finished.Error)
	}
	messages, err := e.store.ListMessages(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.TurnID == turn.ID && message.Role == "assistant" {
			out.answer = message.Content
		}
	}
	mu.Lock()
	defer mu.Unlock()
	out.answerHits = pathPrefixHits(out.answer, longPathStages)
	if out.noteHits != pathPrefixHits(notesText(t, e, folded), longPathStages) {
		t.Fatalf("notes were not a prefix of %d steps", out.noteHits)
	}
	return out
}

func notesText(t *testing.T, e *Engine, thread *store.Thread) string {
	t.Helper()
	pc, err := e.projectContextFor(thread)
	if err != nil {
		t.Fatal(err)
	}
	return pc.promptSections()
}

func pathPrefixHits(text string, stages int) int {
	hits := 0
	for hits < stages && strings.Contains(text, pathToken(hits)) {
		hits++
	}
	for i := hits; i < stages; i++ {
		if strings.Contains(text, pathToken(i)) {
			return -1
		}
	}
	return hits
}
