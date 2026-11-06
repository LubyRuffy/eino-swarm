package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

type nuwaContextUnitView struct {
	ProjectID string           `json:"project_id"`
	Status    string           `json:"status"`
	Revision  int64            `json:"revision"`
	Total     int              `json:"total"`
	Omitted   int              `json:"omitted"`
	Nodes     []store.NuwaNode `json:"nodes"`
}

func decodeNuwaContextUnit(t *testing.T, text string) nuwaContextUnitView {
	t.Helper()
	if !strings.HasPrefix(text, nuwaContextPrefix) || !utf8.ValidString(text) {
		t.Fatal("shared-state explanation or complete UTF-8 was lost")
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimPrefix(text, nuwaContextPrefix)))
	var view nuwaContextUnitView
	if err := decoder.Decode(&view); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF || view.Status != "available" || view.Nodes == nil || view.Total != view.Omitted+len(view.Nodes) {
		t.Fatalf("incomplete JSON or incorrect omission accounting: view=%+v err=%v", view, err)
	}
	return view
}

func TestNuwaContextPreviewRespectsCompleteUnicodeBudget(t *testing.T) {
	nodes := []store.NuwaNode{{ID: "unicode", Title: "证据😀", Summary: strings.Repeat("观察😀", 9), Detail: "private-detail-must-remain-on-demand", Status: "reported", UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}
	before, err := json.Marshal(nodes)
	if err != nil {
		t.Fatal(err)
	}
	full, err := renderNuwaContext("项目😀", 17, nodes, "", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	budget := utf8.RuneCountInString(full)
	if len(full) <= budget {
		t.Fatal("fixture must distinguish UTF-8 bytes from the configured character budget")
	}
	exact, err := renderNuwaContext("项目😀", 17, nodes, "", budget)
	if err != nil || exact != full {
		t.Fatalf("complete block did not fit its exact character budget: err=%v", err)
	}
	view := decodeNuwaContextUnit(t, exact)
	if view.ProjectID != "项目😀" || view.Revision != 17 || view.Total != 1 || view.Omitted != 0 || len(view.Nodes) != 1 || view.Nodes[0].Summary != nodes[0].Summary || view.Nodes[0].Detail != "" || strings.Contains(exact, nodes[0].Detail) {
		t.Fatalf("preview truncated summary or exposed on-demand detail: %+v", view)
	}
	limited, err := renderNuwaContext("项目😀", 17, nodes, "", budget-1)
	if err != nil || utf8.RuneCountInString(limited) > budget-1 {
		t.Fatalf("one-character reduction exceeded the whole-block limit: err=%v", err)
	}
	if view := decodeNuwaContextUnit(t, limited); view.Omitted != 1 || len(view.Nodes) != 0 {
		t.Fatalf("a partial node survived an insufficient budget: %+v", view)
	}
	after, err := json.Marshal(nodes)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("preview changed the source node or its detail: err=%v", err)
	}
}

func TestNuwaContextPreviewSkipsLargeNodesAndSortsDeterministically(t *testing.T) {
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	small := store.NuwaNode{ID: "small", Title: "small", UpdatedAt: at}
	large := store.NuwaNode{ID: "large", Title: "large", Summary: strings.Repeat("大😀", 10000), UpdatedAt: at.Add(time.Second)}
	text, err := renderNuwaContext("project", 9, []store.NuwaNode{small}, "", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	budget := utf8.RuneCountInString(text)
	text, err = renderNuwaContext("project", 9, []store.NuwaNode{large, small}, "", budget)
	if err != nil || utf8.RuneCountInString(text) > budget {
		t.Fatalf("skip-large preview failed: err=%v", err)
	}
	view := decodeNuwaContextUnit(t, text)
	if view.Total != 2 || view.Omitted != 1 || len(view.Nodes) != 1 || view.Nodes[0].ID != small.ID {
		t.Fatalf("oversized newest node hid a later node that fits: %+v", view)
	}
	a, b, c := small, small, small
	a.ID, b.ID, c.ID = "a", "b", "c"
	c.UpdatedAt = at.Add(time.Second)
	original := []store.NuwaNode{b, c, a}
	first, err := renderNuwaContext("project", 9, original, "", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderNuwaContext("project", 9, []store.NuwaNode{a, b, c}, "", 1<<20)
	if err != nil || first != second {
		t.Fatalf("equal timestamps changed preview order with source order: err=%v", err)
	}
	view = decodeNuwaContextUnit(t, first)
	if len(view.Nodes) != 3 {
		t.Fatalf("complete sorting fixture omitted nodes: %+v", view)
	}
	if got := []string{view.Nodes[0].ID, view.Nodes[1].ID, view.Nodes[2].ID}; !reflect.DeepEqual(got, []string{"c", "a", "b"}) || original[0].ID != "b" || original[1].ID != "c" || original[2].ID != "a" {
		t.Fatalf("preview changed source order or lost timestamp/ID ordering: %+v", view)
	}
}

func TestNuwaContextPreviewHandlesEmptyAndRefusesInvalidEnvelopes(t *testing.T) {
	for _, nodes := range [][]store.NuwaNode{nil, {}} {
		text, err := renderNuwaContext("project", 0, nodes, "", 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		view := decodeNuwaContextUnit(t, text)
		if view.Total != 0 || view.Omitted != 0 || len(view.Nodes) != 0 {
			t.Fatalf("empty project invented work: %+v", view)
		}
		budget := utf8.RuneCountInString(text)
		if exact, err := renderNuwaContext("project", 0, nodes, "", budget); err != nil || exact != text {
			t.Fatalf("empty envelope did not fit exactly: err=%v", err)
		}
		for _, tooSmall := range []int{-1, 0, budget - 1} {
			if text, err := renderNuwaContext("project", 0, nodes, "", tooSmall); err == nil || text != "" {
				t.Fatalf("undersized envelope sent partial state: budget=%d text=%q err=%v", tooSmall, text, err)
			}
		}
	}
	invalid := store.NuwaNode{ID: "invalid-time", Title: "invalid-time", UpdatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}
	text, err := renderNuwaContext("project", 0, []store.NuwaNode{invalid}, "", 1<<20)
	var marshalError *json.MarshalerError
	if text != "" || !errors.As(err, &marshalError) {
		t.Fatalf("invalid time JSON error became a current/empty board: text=%q err=%v", text, err)
	}
}

type nuwaContextUnitProbe struct {
	requests [][]*schema.Message
	methods  []string
	advance  bool
}

func (m *nuwaContextUnitProbe) reply(method string, messages []*schema.Message) *schema.Message {
	m.methods = append(m.methods, method)
	m.requests = append(m.requests, append([]*schema.Message(nil), messages...))
	if m.advance && len(m.requests) == 1 {
		return schema.AssistantMessage("", []schema.ToolCall{{ID: "context-unit-step", Type: "function", Function: schema.FunctionCall{Name: "context_unit_step", Arguments: "{}"}}})
	}
	return schema.AssistantMessage("The current state was inspected.", nil)
}

func (m *nuwaContextUnitProbe) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return m.reply("generate", messages), nil
}

func (m *nuwaContextUnitProbe) Stream(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{m.reply("stream", messages)}), nil
}

func nuwaContextUnitFixture(t *testing.T) (*Engine, *store.Project, *store.Thread, *store.Turn, store.NuwaNode) {
	t.Helper()
	e := newTestEngineAt(t, t.TempDir())
	project, err := e.CreateProject("Shared context unit", "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Explicit context owner", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, NuwaEnabled: true}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	node := store.NuwaNode{ProjectID: project.ID, ThreadID: thread.ID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Kind: "fact", Title: store.NewID("source_"), Detail: store.NewID("detail_")}
	if err := e.store.UpsertNuwaNode(t.Context(), &node, 0); err != nil {
		t.Fatal(err)
	}
	return e, project, thread, turn, node
}

func runNuwaContextUnitADK(ctx context.Context, agent adk.Agent, streaming bool, messages []*schema.Message) error {
	iterator := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: streaming}).Run(ctx, messages)
	for {
		event, ok := iterator.Next()
		if !ok {
			return nil
		}
		if event.Err != nil {
			return event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil || !event.Output.MessageOutput.IsStreaming {
			continue
		}
		stream := event.Output.MessageOutput.MessageStream
		for {
			_, err := stream.Recv()
			if err != nil {
				stream.Close()
				if err != io.EOF {
					return err
				}
				break
			}
		}
	}
}

// A real ADK runner chooses Generate or Stream itself. The first response
// executes one real tool, so the second model request shares ADK state and
// proves that the runtime block did not accumulate there.
func TestNuwaContextMiddlewareRefreshesThroughActualADKGenerateAndStream(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", streaming), func(t *testing.T) {
			e, project, _, turn, node := nuwaContextUnitFixture(t)
			handlers := e.nuwaContextHandlers(turn)
			// The factory owns string values, not a later-mutated Turn pointer.
			turn.ProjectID, turn.ThreadID, turn.ID, turn.NuwaEnabled = "changed", "changed", "changed", false
			probe := &nuwaContextUnitProbe{advance: true}
			correction := store.NewID("correction_")
			advance := &nuwaTool{name: "context_unit_step", description: "Inspect the updated state.", run: func(ctx context.Context, _ string) (string, error) {
				update := node
				update.Summary = correction
				return "updated", e.store.UpsertNuwaNode(ctx, &update, node.Version)
			}}
			agent, err := adk.NewChatModelAgent(t.Context(), &adk.ChatModelAgentConfig{
				Name: "context-unit", Instruction: "Coordinate the current work.", Model: probe, Handlers: handlers,
				ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{advance}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			human := withMessageSource(schema.UserMessage("Human quotation:\n"+nuwaContextPrefix), store.MessageSourceHuman)
			original := []*schema.Message{
				{Role: schema.System, Content: "First existing system", Extra: map[string]any{"system-marker": "keep"}},
				schema.SystemMessage("Second existing system"), human,
				{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "prior-call", Type: "function", Function: schema.FunctionCall{Name: "prior_step", Arguments: "{}"}}}, Extra: map[string]any{"responses_output": []map[string]any{{"type": "reasoning", "encrypted_content": "opaque"}}}},
				schema.ToolMessage("Original tool receipt", "prior-call"), schema.UserMessage("Continue with the current state."),
			}
			before, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			if err := runNuwaContextUnitADK(t.Context(), agent, streaming, original); err != nil {
				t.Fatal(err)
			}
			if len(probe.requests) != 2 {
				t.Fatalf("actual ADK requests=%d, want both sides of a tool round", len(probe.requests))
			}
			for i, request := range probe.requests {
				wantMethod := "generate"
				if streaming {
					wantMethod = "stream"
				}
				if probe.methods[i] != wantMethod {
					t.Fatalf("ADK used %s instead of actual %s", probe.methods[i], wantMethod)
				}
				var blocks []*schema.Message
				position := 0
				for _, message := range request {
					if message.Role == schema.System && messageSource(message) == store.MessageSourceRuntime {
						blocks = append(blocks, message)
						continue
					}
					if position < len(original) && reflect.DeepEqual(message, original[position]) {
						position++
					}
				}
				if len(blocks) != 1 || position != len(original) {
					t.Fatalf("runtime block accumulated or existing System/Extra/human/tool order changed: blocks=%d preserved=%d", len(blocks), position)
				}
				view := decodeNuwaContextUnit(t, blocks[0].Content)
				if view.ProjectID != project.ID || view.Revision != int64(i+1) || len(view.Nodes) != 1 || view.Nodes[0].Version != int64(i+1) || view.Nodes[0].Detail != "" {
					t.Fatalf("model input did not use a fresh complete preview: %+v", view)
				}
				if i == 1 && view.Nodes[0].Summary != correction {
					t.Fatal("second ADK model input retained the pre-tool snapshot")
				}
			}
			after, err := json.Marshal(original)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("middleware changed the caller's original messages: err=%v", err)
			}
		})
	}
}

type nuwaContextUnitFaultKey struct{}

func decodeNuwaUnavailableUnit(t *testing.T, text, projectID, reason string) {
	t.Helper()
	if !strings.HasPrefix(text, "## Nuwa shared project state\n") || !utf8.ValidString(text) {
		t.Fatal("unavailable state lost its runtime heading or complete UTF-8")
	}
	start := strings.Index(text, "{")
	if start < 0 {
		t.Fatal("unavailable state has no JSON envelope")
	}
	decoder := json.NewDecoder(strings.NewReader(text[start:]))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("unavailable state has incomplete or trailing JSON: %v", err)
	}
	for field, want := range map[string]string{"project_id": projectID, "status": "unavailable", "reason": reason} {
		var got string
		if err := json.Unmarshal(fields[field], &got); err != nil || got != want {
			t.Fatalf("unavailable %s=%q want=%q err=%v", field, got, want, err)
		}
	}
	for _, field := range []string{"revision", "nodes"} {
		if _, present := fields[field]; present {
			t.Fatalf("unavailable state fabricated a current %s value", field)
		}
	}
}

func TestNuwaUnavailableContextRespectsCompleteCharacterBudget(t *testing.T) {
	for _, reason := range []string{"owner_unavailable", "read_failed"} {
		text, err := renderNuwaUnavailableContext("项目😀", reason, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		decodeNuwaUnavailableUnit(t, text, "项目😀", reason)
		budget := utf8.RuneCountInString(text)
		if exact, err := renderNuwaUnavailableContext("项目😀", reason, budget); err != nil || exact != text {
			t.Fatalf("unavailable envelope did not fit its exact character budget: err=%v", err)
		}
		for _, limit := range []int{1, budget - 1} {
			if text, err := renderNuwaUnavailableContext("项目😀", reason, limit); err == nil || text != "" || !strings.Contains(err.Error(), "budget") {
				t.Fatalf("unavailable envelope exceeded budget or lost its failure: budget=%d text=%q err=%v", limit, text, err)
			}
		}
	}
}

func TestNuwaContextMiddlewareHandlesSnapshotFailuresAtActualADKModel(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, failure := range []string{"store", "project-moved", "budget"} {
			t.Run(fmt.Sprintf("stream=%v/%s", streaming, failure), func(t *testing.T) {
				e, project, thread, turn, node := nuwaContextUnitFixture(t)
				probe := &nuwaContextUnitProbe{advance: failure != "budget"}
				ctx := t.Context()
				var fault atomic.Bool
				privateError := errors.New("private Nuwa snapshot query failure")
				reason := "owner_unavailable"
				if failure == "store" {
					reason = "read_failed"
					fault.Store(true)
					if err := e.store.DB().Callback().Query().Before("gorm:query").Register("nuwa_context_private_fault", func(tx *gorm.DB) {
						if active, ok := tx.Statement.Context.Value(nuwaContextUnitFaultKey{}).(*atomic.Bool); ok && active.Load() {
							tx.AddError(privateError)
						}
					}); err != nil {
						t.Fatal(err)
					}
					ctx = context.WithValue(ctx, nuwaContextUnitFaultKey{}, &fault)
				} else if failure == "project-moved" {
					other, err := e.CreateProject("Moved owner", "", "", false, true)
					if err != nil {
						t.Fatal(err)
					}
					if err := e.store.UpdateThread(thread.ID, map[string]any{"project_id": other.ID}); err != nil {
						t.Fatal(err)
					}
				} else {
					e.cfg.Tools.ResultMaxChars = 1
				}
				advance := &nuwaTool{name: "context_unit_step", description: "Inspect the restored state.", run: func(context.Context, string) (string, error) {
					fault.Store(false)
					if failure == "project-moved" {
						if err := e.store.UpdateThread(thread.ID, map[string]any{"project_id": project.ID}); err != nil {
							return "", err
						}
					}
					return "restored", nil
				}}
				agent, err := adk.NewChatModelAgent(t.Context(), &adk.ChatModelAgentConfig{
					Name: "context-availability", Model: probe, Handlers: e.nuwaContextHandlers(turn),
					ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{advance}}},
				})
				if err != nil {
					t.Fatal(err)
				}
				err = runNuwaContextUnitADK(ctx, agent, streaming, []*schema.Message{schema.UserMessage("Inspect current state.")})
				if failure == "budget" {
					if err == nil || !strings.Contains(err.Error(), "shared state envelope") || !strings.Contains(err.Error(), "budget") || len(probe.requests) != 0 {
						t.Fatalf("budget refusal reached inner model or lost its explicit cause: requests=%d err=%v", len(probe.requests), err)
					}
					return
				}
				if err != nil || len(probe.requests) != 2 {
					t.Fatalf("optional preview failure interrupted the actual ADK lifecycle: requests=%d err=%v", len(probe.requests), err)
				}
				for i, request := range probe.requests {
					wantMethod := "generate"
					if streaming {
						wantMethod = "stream"
					}
					var blocks []*schema.Message
					for _, message := range request {
						if message.Role == schema.System && messageSource(message) == store.MessageSourceRuntime {
							blocks = append(blocks, message)
						}
					}
					if probe.methods[i] != wantMethod || len(blocks) != 1 {
						t.Fatalf("actual %s input has %d runtime blocks instead of one fresh state", probe.methods[i], len(blocks))
					}
					if i == 0 {
						decodeNuwaUnavailableUnit(t, blocks[0].Content, project.ID, reason)
						for _, forbidden := range []string{privateError.Error(), "source thread belongs to another project", node.ID, node.Title, node.Detail} {
							if strings.Contains(blocks[0].Content, forbidden) {
								t.Fatalf("unavailable preview disclosed source data or raw error: %q", forbidden)
							}
						}
						continue
					}
					view := decodeNuwaContextUnit(t, blocks[0].Content)
					if view.ProjectID != project.ID || view.Revision != 1 || len(view.Nodes) != 1 || view.Nodes[0].ID != node.ID || view.Nodes[0].Version != 1 || view.Nodes[0].Detail != "" {
						t.Fatalf("same ADK wrapper did not recover a valid current snapshot: %+v", view)
					}
				}
			})
		}
	}
}

func TestNuwaContextCancellationAndDisabledModesDoNotReadOrCallModel(t *testing.T) {
	e := &Engine{}
	for _, turn := range []*store.Turn{nil, {}, {ProjectID: "project"}, {NuwaEnabled: true}} {
		if handlers := e.nuwaContextHandlers(turn); len(handlers) != 0 {
			t.Fatalf("disabled/standalone turn gained a shared-state middleware: %+v", turn)
		}
	}
	for _, streaming := range []bool{false, true} {
		probe := &nuwaContextUnitProbe{}
		handlers := e.nuwaContextHandlers(&store.Turn{ProjectID: "project", ThreadID: "thread", ID: "turn", NuwaEnabled: true})
		wrapped, err := handlers[0].WrapModel(t.Context(), probe, nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		// A nil Store makes a missed early guard fail rather than allowing ADK's
		// own cancellation to mask an unsafe wrapper implementation.
		if streaming {
			_, err = wrapped.Stream(ctx, nil)
		} else {
			_, err = wrapped.Generate(ctx, nil)
		}
		if !errors.Is(err, context.Canceled) || len(probe.requests) != 0 {
			t.Fatalf("cancelled wrapper read Store or called model: stream=%v calls=%d err=%v", streaming, len(probe.requests), err)
		}
	}
}

// Spare caller capacity is observable state too. A plain append to the input
// slice can overwrite a fork/checkpoint owner's backing array without changing
// the original slice length, so the ADK-path assertions alone cannot catch it.
func TestNuwaContextWrapperPreservesCallerBackingArray(t *testing.T) {
	e, _, _, turn, _ := nuwaContextUnitFixture(t)
	probe := &nuwaContextUnitProbe{}
	wrapped, err := e.nuwaContextHandlers(turn)[0].WrapModel(t.Context(), probe, nil)
	if err != nil {
		t.Fatal(err)
	}
	messages := make([]*schema.Message, 2, 3)
	messages[0], messages[1] = schema.SystemMessage("Original System"), schema.UserMessage("Original human")
	sentinel := schema.UserMessage("Caller-owned spare capacity")
	backing := messages[:cap(messages)]
	backing[2] = sentinel
	if _, err := wrapped.Generate(t.Context(), messages); err != nil {
		t.Fatal(err)
	}
	stream, err := wrapped.Stream(t.Context(), messages)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if backing[2] != sentinel || len(messages) != 2 || len(probe.requests) != 2 {
		t.Fatal("temporary insertion overwrote the caller's backing array")
	}
	for _, request := range probe.requests {
		if len(request) != 3 || request[0] != messages[0] || request[2] != messages[1] || request[1].Role != schema.System || messageSource(request[1]) != store.MessageSourceRuntime {
			t.Fatal("wrapper changed original messages or accumulated a previous runtime block")
		}
	}
}
