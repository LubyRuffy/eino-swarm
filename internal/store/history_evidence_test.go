package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type historyEvidenceFixture struct {
	s                        *Store
	path                     string
	project, foreign         Project
	recipient, source, other Thread
	turn                     Turn
	message                  Message
	event                    Event
}

func newHistoryEvidenceFixture(t *testing.T) historyEvidenceFixture {
	t.Helper()
	f := historyEvidenceFixture{path: filepath.Join(t.TempDir(), "history-evidence.db")}
	var err error
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.s.Close() })
	f.project.Name = "Current project"
	f.foreign.Name = "Other project"
	for _, p := range []*Project{&f.project, &f.foreign} {
		if err := f.s.CreateProject(p); err != nil {
			t.Fatal(err)
		}
	}
	f.recipient = Thread{Title: "Report", ProjectID: f.project.ID}
	f.source = Thread{Title: "Source discussion", ProjectID: f.project.ID}
	f.other = Thread{Title: "Unrelated", ProjectID: f.foreign.ID}
	for _, th := range []*Thread{&f.recipient, &f.source, &f.other} {
		if err := f.s.CreateThread(th); err != nil {
			t.Fatal(err)
		}
	}
	f.turn = Turn{ThreadID: f.recipient.ID}
	if err := f.s.CreateTurn(&f.turn); err != nil {
		t.Fatal(err)
	}
	if err := f.s.AppendMessages(f.other.ID, "", []Message{{Role: "user", Content: "other original"}}); err != nil {
		t.Fatal(err)
	}
	rows := []Message{{Role: "user", Source: "human", Content: "Do not deploy.\nKeep the original condition.\x00", Reasoning: "private reasoning", ToolCalls: `[{"private":"arguments"}]`, Images: []ImageRef{{ID: "image", Name: "original.png", MIME: "image/png"}}}}
	if err := f.s.AppendMessages(f.source.ID, "", rows); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB().Where("thread_id = ?", f.source.ID).First(&f.message).Error; err != nil {
		t.Fatal(err)
	}
	f.event = Event{ThreadID: f.source.ID, Kind: "tool_result", AgentID: "worker-1", Role: "researcher", Text: "Original observation: action did not complete.\x00", Err: "permission denied"}
	if err := f.s.AppendEvent(&f.event); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f historyEvidenceFixture) refs() []HistoryReference {
	return []HistoryReference{{Kind: "message", ThreadID: f.source.ID, Seq: f.message.Seq}, {Kind: "event", ThreadID: f.source.ID, Seq: f.event.Seq}}
}

func TestCanonicalHistorySourcesCopyFullBodyAndAttributionInReferenceOrder(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	sources, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs(), 20000)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].Reference != f.refs()[0] || sources[0].RowID != f.message.ID || int64(sources[0].RowID) == sources[0].Reference.Seq || sources[0].Text != f.message.Content || sources[0].Role != "user" || sources[0].Source != "human" || len(sources[0].Images) != 1 || sources[0].ThreadTitle != f.source.Title || sources[0].ProjectID != f.project.ID {
		t.Fatalf("message original/identity changed: %#v", sources)
	}
	if sources[1].Reference != f.refs()[1] || sources[1].Text != f.event.Text || sources[1].Err != f.event.Err || sources[1].EventKind != "tool_result" || sources[1].Role != "researcher" || sources[1].Source != "" || sources[1].AgentID != "worker-1" {
		t.Fatalf("event observation changed: %#v", sources[1])
	}
	body, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "private reasoning") || strings.Contains(string(body), "private\\\"") {
		t.Fatalf("private model payload reached sources: %s", body)
	}
}

func TestCanonicalHistorySourcesRejectInvalidUnavailablePrivateAndForeignReferences(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	ref := f.refs()[0]
	for _, refs := range [][]HistoryReference{
		{{Kind: "unknown", ThreadID: f.source.ID, Seq: 1}}, {{Kind: "message", Seq: 1}}, {{Kind: "event", ThreadID: f.source.ID}}, {ref, ref},
	} {
		if _, err := f.s.ReadHistorySources(context.Background(), f.project.ID, refs, 20000); !errors.Is(err, ErrHistoryReferenceInvalid) {
			t.Fatalf("invalid refs=%#v err=%v", refs, err)
		}
	}
	private := Event{ThreadID: f.source.ID, Kind: "engine_restart_checkpoint", Text: "private checkpoint"}
	if err := f.s.AppendEvent(&private); err != nil {
		t.Fatal(err)
	}
	for _, refs := range [][]HistoryReference{
		{{Kind: "message", ThreadID: f.source.ID, Seq: 999}}, {{Kind: "message", ThreadID: f.other.ID, Seq: 1}}, {{Kind: "event", ThreadID: f.source.ID, Seq: private.Seq}}, {{Kind: "event", ThreadID: "missing-thread", Seq: 1}},
	} {
		if got, err := f.s.ReadHistorySources(context.Background(), f.project.ID, refs, 20000); !errors.Is(err, ErrHistorySourceUnavailable) || got != nil {
			t.Fatalf("unavailable source=%#v %#v %v", refs, got, err)
		}
	}
	if _, err := f.s.ReadHistorySources(context.Background(), "missing-project", f.refs(), 20000); !errors.Is(err, ErrHistorySourceUnavailable) {
		t.Fatalf("missing bound project=%v", err)
	}
	if _, err := f.s.ReadHistorySources(context.Background(), "", []HistoryReference{{Kind: "message", ThreadID: f.other.ID, Seq: 1}}, 20000); err != nil {
		t.Fatalf("standalone global scope changed: %v", err)
	}
}

func historyBoundEvidence(t *testing.T, f historyEvidenceFixture) *HistoryEvidence {
	t.Helper()
	// These snapshots come from the fixture's actual stored rows. Constructing
	// them independently lets transaction tests expose stale admission even
	// before the separate canonical-reader implementation is available.
	refs := f.refs()
	return &HistoryEvidence{ProjectID: f.project.ID, Sources: []HistorySource{
		{Reference: refs[0], RowID: f.message.ID, ThreadTitle: f.source.Title, ProjectID: f.project.ID, Role: f.message.Role, Source: f.message.Source, AgentID: f.message.AgentID, Text: f.message.Content, CreatedAt: f.message.CreatedAt, Images: f.message.Images},
		{Reference: refs[1], RowID: f.event.ID, ThreadTitle: f.source.Title, ProjectID: f.project.ID, Role: f.event.Role, AgentID: f.event.AgentID, Text: f.event.Text, EventKind: f.event.Kind, Err: f.event.Err, CreatedAt: f.event.CreatedAt, Images: f.event.Images},
	}}
}

func TestHistoryAnswerAdmissionRejectsChangedOriginalOrRecipientScope(t *testing.T) {
	mutations := map[string]func(historyEvidenceFixture) error{
		"body": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("content", "changed original").Error
		},
		"role": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("role", "assistant").Error
		},
		"source": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("source", "runtime").Error
		},
		"title": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Thread{}).Where("id=?", f.source.ID).Update("title", "renamed source").Error
		},
		"membership": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Thread{}).Where("id=?", f.source.ID).Update("project_id", f.foreign.ID).Error
		},
		"recipient": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Thread{}).Where("id=?", f.recipient.ID).Update("project_id", f.foreign.ID).Error
		},
		"event-error": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Event{}).Where("id=?", f.event.ID).Update("err", "").Error
		},
		"event-kind": func(f historyEvidenceFixture) error {
			return f.s.DB().Model(&Event{}).Where("id=?", f.event.ID).Update("kind", "engine_restart_checkpoint").Error
		},
		"deleted": func(f historyEvidenceFixture) error { return f.s.DB().Delete(&f.message).Error },
		"ambiguous": func(f historyEvidenceFixture) error {
			copy := f.message
			copy.ID = 0
			return f.s.DB().Create(&copy).Error
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f := newHistoryEvidenceFixture(t)
			evidence := historyBoundEvidence(t, f)
			if err := mutate(f); err != nil {
				t.Fatal(err)
			}
			if err := f.s.AdmitScheduleAnswerWithHistory(f.recipient.ID, f.turn.ID, nil, []Event{{Kind: "agent_message", AgentID: "manager", Text: "Bound report"}}, evidence); !errors.Is(err, ErrHistoryEvidenceChanged) {
				t.Fatalf("changed source published: %v", err)
			}
			assertNoAdmittedAnswer(t, f.s, f.recipient.ID)
		})
	}
}

func TestDurableHistoryReportModeUsesExactIndexedTurnEvents(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	if got, err := f.s.TurnHasHistoryQuery(f.recipient.ID, f.turn.ID); err != nil || got {
		t.Fatalf("unqueried turn=%v %v", got, err)
	}
	for _, text := range []string{`exec({"text":"query_history({})"})`, `query_history_extra({})`, `Query_History({})`} {
		if err := f.s.AppendEvent(&Event{ThreadID: f.recipient.ID, TurnID: f.turn.ID, Kind: "tool_call", AgentID: "manager", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := f.s.TurnHasHistoryQuery(f.recipient.ID, f.turn.ID); err != nil || got {
		t.Fatalf("lookalike call activated report mode=%v %v", got, err)
	}
	if err := f.s.AppendEvent(&Event{ThreadID: f.recipient.ID, TurnID: f.turn.ID, Kind: "tool_call", AgentID: "worker-1", Text: `query_history({})`}); err != nil {
		t.Fatal(err)
	}
	if got, err := f.s.TurnHasHistoryQuery(f.recipient.ID, f.turn.ID); err != nil || !got {
		t.Fatalf("real worker query was lost=%v %v", got, err)
	}
	if got, err := f.s.TurnHasHistoryReportRequest(f.recipient.ID, f.turn.ID); err != nil || got {
		t.Fatalf("unrequested turn=%v %v", got, err)
	}
	if err := f.s.AppendEvent(&Event{ThreadID: f.recipient.ID, TurnID: f.turn.ID, Kind: "history_report_requested"}); err != nil {
		t.Fatal(err)
	}
	if got, err := f.s.TurnHasHistoryReportRequest(f.recipient.ID, f.turn.ID); err != nil || !got {
		t.Fatalf("durable report request was lost=%v %v", got, err)
	}
}

func TestCanonicalHistoryBudgetsAndCancellationAreExplicit(t *testing.T) {
	f := newHistoryEvidenceFixture(t)
	for _, limit := range []int{0, 1, 100} {
		if got, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs(), limit); !errors.Is(err, ErrHistoryEvidenceLimit) || got != nil {
			t.Fatalf("source/metadata silently clipped: limit=%d got=%#v err=%v", limit, got, err)
		}
	}
	if err := f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("content", strings.Repeat("原", 10000)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReadHistorySources(context.Background(), f.project.ID, f.refs(), 1000); !errors.Is(err, ErrHistoryEvidenceLimit) {
		t.Fatalf("oversized source read=%v", err)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := fmt.Errorf("human canceled canonical source lookup")
	cancel(cause)
	if _, err := f.s.ReadHistorySources(ctx, f.project.ID, f.refs(), 20000); !errors.Is(err, cause) {
		t.Fatalf("lost cancellation cause: %v", err)
	}
	if err := f.s.DB().Model(&Message{}).Where("id=?", f.message.ID).Update("created_at", time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
}
