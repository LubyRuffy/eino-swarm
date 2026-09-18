package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// What a project wrote to its memory is part of the turn that caused it. If the
// review needed a second id to find, nobody chasing "why does it think that"
// would ever reach it.
func TestTraceShowsTheReviewThatFollowedTheTurn(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	pj := &store.Project{Name: "Grouped", MemoryEnabled: true}
	if err := st.CreateProject(pj); err != nil {
		t.Fatal(err)
	}
	th := &store.Thread{Title: "Reviewed", ProjectID: pj.ID}
	if err := st.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "carry on"}
	if err := st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID,
		Kind: engine.KindMemoryReview, AgentID: engine.ReviewAgentID,
		Text: `{"changed":true,"memory":{"added":1},"skills":[]}`}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLLMCall(&store.LLMCall{ThreadID: th.ID, TurnID: turn.ID,
		AgentID: engine.ReviewAgentID, Model: "some-model", InputMsgs: 2,
		InputChars: 90, OutputChars: 20, DurationMS: 120}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runTrace([]string{turn.ID, "--data-dir", dir}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{engine.KindMemoryReview, engine.ReviewAgentID, "model calls (1)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("trace is missing %q:\n%s", want, out)
		}
	}
}
