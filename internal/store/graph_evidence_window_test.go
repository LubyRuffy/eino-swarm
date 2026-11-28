package store

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
)

func TestGraphEvidenceKeepsNewlyVerifiedReceiptsAfterTheWindowFills(t *testing.T) {
	f := newGraphFixture(t, false)
	history := make([]graph.Evidence, graph.MaxEvidence)
	for i := range history {
		history[i] = graph.Evidence{URL: fmt.Sprintf("https://example.test/observations/%d", i)}
	}
	created := f.apply(graph.Batch{
		Subjects: []graph.SubjectInput{
			{Ref: "address", Kind: "ip", Status: graph.StatusConfirmed, Attrs: map[string]any{"ip": "203.0.113.7"}},
			{Ref: "name", Kind: "domain", Attrs: map[string]any{"domain": "example.test"}},
		},
		Links:    []graph.LinkInput{{From: "address", To: "name", Rel: "serves"}},
		Evidence: history,
	})
	addressID, nameID := created.Subjects[0].ID, created.Subjects[1].ID
	fresh := f.event(f.thread, f.turn, "new observation")
	batch := graph.Batch{
		Subjects: []graph.SubjectInput{{ID: addressID}},
		Links:    []graph.LinkInput{{From: addressID, To: nameID, Rel: "serves"}},
		Evidence: []graph.Evidence{fresh},
	}
	updated := f.apply(batch)
	if updated.Revision != created.Revision+1 || updated.Subjects[0].Version != created.Subjects[0].Version ||
		updated.Links[0].ID != created.Links[0].ID {
		t.Fatalf("evidence refresh changed row identity or subject version: %+v", updated)
	}
	// The same receipt in a later batch must not duplicate it or evict another.
	f.apply(batch)
	fresh.TurnID = f.turn.ID
	want := append(slices.Clone(history[1:]), fresh)
	if evidence := f.readSubject(addressID).Evidence; !slices.Equal(evidence, want) {
		t.Errorf("subject dropped the latest evidence or evicted the wrong entry: %+v", evidence)
	}
	view := f.read(GraphQuery{All: true})
	if len(view.Links) != 1 || !slices.Equal(view.Links[0].Evidence, want) {
		t.Fatalf("link dropped the latest evidence or evicted the wrong entry: %+v", view.Links)
	}
}

func TestGraphEvidenceDoesNotFollowAReusedSequenceAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zwai.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	f := &graphFixture{t: t, s: s}
	f.thread, f.turn = f.newTurn()
	f.owner = f.thread.ID
	retained := f.event(f.thread, f.turn, "retained observation")
	later := &Turn{ThreadID: f.thread.ID}
	if err := s.CreateTurn(later); err != nil {
		t.Fatal(err)
	}
	removed := f.event(f.thread, later, "observation that will be removed")
	result := f.apply(graph.Batch{
		Subjects: []graph.SubjectInput{ip("203.0.113.7")},
		Evidence: []graph.Evidence{retained, removed},
	})
	// A process can exit after the deletion commits and before the rewind
	// marker is durable. Reopening then seeds from the last retained event.
	if err := s.TruncateFromEventSeq(f.thread.ID, removed.Seq); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	f.s = reopened
	replacement := &Turn{ThreadID: f.thread.ID}
	if err := reopened.CreateTurn(replacement); err != nil {
		t.Fatal(err)
	}
	newEvent := f.event(f.thread, replacement, "unrelated observation")
	if newEvent.Seq != removed.Seq || replacement.ID == later.ID {
		t.Fatalf("did not replace seq %d with a distinct turn: %+v", removed.Seq, newEvent)
	}
	id := result.Subjects[0].ID
	evidence := f.readSubject(id).Evidence
	if len(evidence) != 2 || evidence[0].TurnID != f.turn.ID || evidence[0].Unavailable ||
		evidence[1].TurnID != later.ID || !evidence[1].Unavailable {
		t.Fatalf("the retained receipt must stay available and the removed receipt must stay unavailable: %+v", evidence)
	}
	f.apply(graph.Batch{Subjects: []graph.SubjectInput{{ID: id}}, Evidence: []graph.Evidence{newEvent}})
	evidence = f.readSubject(id).Evidence
	if len(evidence) != 3 || !evidence[1].Unavailable || evidence[2].TurnID != replacement.ID || evidence[2].Unavailable {
		t.Fatalf("a new citation at the reused sequence must not repair the removed citation: %+v", evidence)
	}
}

func TestGraphReadKeepsAFullPageWithManyDistinctEvidenceReferences(t *testing.T) {
	f := newGraphFixture(t, false)
	const subjects = 1025
	// Stored histories can cite enough distinct events from one thread to
	// exceed SQLite's variable limit even within a valid subject page.
	retained := f.event(f.thread, f.turn, "retained observation")
	rows := make([]GraphSubject, subjects)
	for i := range rows {
		evidence := make([]graph.Evidence, graph.MaxEvidence)
		for j := range evidence {
			evidence[j] = graph.Evidence{ThreadID: f.thread.ID, TurnID: f.turn.ID, Seq: int64(i*graph.MaxEvidence+j) + retained.Seq}
		}
		rows[i] = GraphSubject{ID: fmt.Sprintf("sj_%04d", i), OwnerID: f.owner, Kind: "ip", Status: graph.StatusLead, Version: 1, Evidence: evidence}
	}
	if err := f.s.DB().CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	view, err := f.s.ReadGraph(context.Background(), f.owner, GraphQuery{All: true, Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Subjects) != subjects || view.More {
		t.Fatalf("read %d of %d stored subjects, more=%v", len(view.Subjects), subjects, view.More)
	}
	var available, unavailable int
	for _, row := range view.Subjects {
		for _, entry := range row.Evidence {
			if entry.Unavailable {
				unavailable++
			} else if entry.Seq != retained.Seq {
				t.Fatalf("a missing event was marked available: %+v", entry)
			} else {
				available++
			}
		}
	}
	if available != 1 || unavailable != subjects*graph.MaxEvidence-1 {
		t.Fatalf("available=%d unavailable=%d", available, unavailable)
	}
}
