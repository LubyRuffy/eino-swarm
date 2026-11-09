package mcpclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPProgressArrivesBeforeResultAndOnlyReportsSafeIncreasingSnapshots(t *testing.T) {
	tokens := make(chan any, 1)
	advance := make(chan struct{})
	finish := make(chan struct{})
	remote := progressFixture(t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		token := req.Params.GetProgressToken()
		tokens <- token
		if token == nil {
			return progressFixtureResult(), nil
		}
		for _, notification := range []mcp.ProgressNotificationParams{
			{ProgressToken: "unrelated", Progress: 9, Total: 10, Message: "wrong token"},
			{ProgressToken: map[string]any{"token": token}, Progress: 9, Total: 10, Message: "malformed token"},
			{ProgressToken: token, Progress: -1, Total: 10, Message: "negative progress"},
			{ProgressToken: token, Progress: 1, Total: -10, Message: "negative total"},
			{ProgressToken: token, Progress: 11, Total: 10, Message: "over total"},
			{ProgressToken: token, Progress: 0, Total: 10},
			{ProgressToken: token, Progress: 0, Total: 10, Message: "  Preparing\trows\x00\u202e\nprivate worker trace"},
		} {
			if err := req.Session.NotifyProgress(ctx, &notification); err != nil {
				return nil, err
			}
		}
		select {
		case <-advance:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		for _, notification := range []mcp.ProgressNotificationParams{
			{ProgressToken: token, Progress: 0, Total: 10, Message: "duplicate zero"},
			{ProgressToken: token, Progress: 2, Total: 10, Message: strings.Repeat("行", 210)},
			{ProgressToken: token, Progress: 1, Total: 10, Message: "out of order"},
			{ProgressToken: token, Progress: 3, Total: 10},
		} {
			if err := req.Session.NotifyProgress(ctx, &notification); err != nil {
				return nil, err
			}
		}
		select {
		case <-finish:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return progressFixtureResult(), nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	updates, result := runProgressTool(t, ctx, remote, `{}`)
	if token, ok := awaitProgress(t, tokens).(string); !ok || token == "" {
		t.Fatalf("actual MCP call did not request progress with a string token: %v", token)
	}
	if got := awaitProgress(t, updates); got != "Preparing rows · 0 / 10" {
		t.Fatalf("first progress snapshot %q", got)
	}
	select {
	case got := <-result:
		t.Fatalf("tool completed before the server released its result: %+v", got)
	default:
	}
	close(advance)
	if got := awaitProgress(t, updates); got != strings.Repeat("行", 160)+" · 2 / 10" {
		t.Fatalf("long message was not clipped to a safe snapshot: %q", got)
	}
	if got := awaitProgress(t, updates); got != "3 / 10" {
		t.Fatalf("numeric fallback %q", got)
	}
	close(finish)
	assertProgressFixtureResult(t, awaitProgress(t, result))
}

func TestMCPProgressKeepsParallelCallsSeparateAfterResultOrCancellation(t *testing.T) {
	for _, terminal := range []string{"completed", "cancelled"} {
		t.Run(terminal, func(t *testing.T) {
			type tokenObservation struct {
				label   string
				token   string
				session *mcp.ServerSession
			}
			observed := make(chan tokenObservation, 2)
			finishFirst := make(chan struct{})
			finishSecond := make(chan struct{})
			lateToken := make(chan string, 1)
			remote := progressFixture(t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				var args struct{ Label string }
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return nil, err
				}
				token, _ := req.Params.GetProgressToken().(string)
				observed <- tokenObservation{args.Label, token, req.Session}
				if token == "" {
					return nil, fmt.Errorf("missing progress token")
				}
				if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token, Progress: 1, Message: args.Label}); err != nil {
					return nil, err
				}
				if args.Label == "first" {
					select {
					case <-finishFirst:
						return progressFixtureResult(), nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				select {
				case stale := <-lateToken:
					// Both notifications use the still-open second response stream.
					// Seeing the second establishes that the late frame was read.
					for _, notification := range []mcp.ProgressNotificationParams{
						{ProgressToken: stale, Progress: 2, Message: "late first"},
						{ProgressToken: token, Progress: 2, Message: "still second"},
					} {
						if err := req.Session.NotifyProgress(ctx, &notification); err != nil {
							return nil, err
						}
					}
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				select {
				case <-finishSecond:
					return progressFixtureResult(), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			})
			firstCtx, cancelFirst := context.WithCancel(t.Context())
			defer cancelFirst()
			secondCtx, cancelSecond := context.WithCancel(t.Context())
			defer cancelSecond()
			firstUpdates, firstResult := runProgressTool(t, firstCtx, remote, `{"label":"first"}`)
			secondUpdates, secondResult := runProgressTool(t, secondCtx, remote, `{"label":"second"}`)
			first, second := awaitProgress(t, observed), awaitProgress(t, observed)
			if first.label == "second" {
				first, second = second, first
			}
			if first.session != second.session || first.token == "" || second.token == "" || first.token == second.token {
				t.Fatal("parallel calls did not use distinct progress tokens on the shared MCP session")
			}
			if got := awaitProgress(t, firstUpdates); got != "first · 1" {
				t.Fatalf("first call received %q", got)
			}
			if got := awaitProgress(t, secondUpdates); got != "second · 1" {
				t.Fatalf("second call received %q", got)
			}
			if terminal == "completed" {
				close(finishFirst)
				assertProgressFixtureResult(t, awaitProgress(t, firstResult))
			} else {
				cancelFirst()
				got := awaitProgress(t, firstResult)
				if got.err != nil || !strings.Contains(got.text, `"code":"cancelled"`) {
					t.Fatalf("cancelled tool result %+v", got)
				}
			}
			lateToken <- first.token
			if got := awaitProgress(t, secondUpdates); got != "still second · 2" {
				t.Fatalf("late peer update leaked into the active tool: %q", got)
			}
			select {
			case got := <-firstUpdates:
				t.Fatalf("terminal call accepted a late update: %q", got)
			default:
			}
			close(finishSecond)
			assertProgressFixtureResult(t, awaitProgress(t, secondResult))
		})
	}
}

func TestMCPProgressIsOptInAndRequiresTheOwningSession(t *testing.T) {
	remote := progressFixture(t, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if req.Params.GetProgressToken() != nil {
			return nil, fmt.Errorf("unobserved call unexpectedly requested progress")
		}
		return progressFixtureResult(), nil
	})
	text, err := remote.InvokableRun(t.Context(), `{"name":"work","arguments":{}}`)
	assertProgressFixtureResult(t, progressToolResult{text, err})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner := new(mcp.ClientSession)
	var updates []string
	e := &entry{ctx: t.Context(), progress: map[string]*activeProgress{
		"active": {ctx: ctx, session: owner, emit: func(text string) { updates = append(updates, text) }},
	}}
	params := &mcp.ProgressNotificationParams{ProgressToken: "active", Progress: 2}
	e.handleProgress(t.Context(), nil)
	e.handleProgress(t.Context(), &mcp.ProgressNotificationClientRequest{Session: owner})
	e.handleProgress(t.Context(), &mcp.ProgressNotificationClientRequest{Session: new(mcp.ClientSession), Params: params})
	cancel()
	e.handleProgress(t.Context(), &mcp.ProgressNotificationClientRequest{Session: owner, Params: params})
	if len(updates) != 0 {
		t.Fatalf("invalid, foreign or cancelled notification reached the sink: %v", updates)
	}
}

func TestMCPProgressLogsOnlySafePhasesFromTheActiveRequest(t *testing.T) {
	const (
		phaseKey       = "io.github.lubyruffy/phase"
		eventKey       = "io.github.lubyruffy/phase-event"
		durationKey    = "io.github.lubyruffy/phase-duration-ms"
		codeKey        = "io.github.lubyruffy/phase-code"
		privateMessage = "private-data-not-for-logs"
	)
	var output bytes.Buffer
	tokens := make(chan string, 1)
	release := make(chan struct{})
	remote := progressFixture(t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		token, _ := req.Params.GetProgressToken().(string)
		tokens <- token
		start := mcp.Meta{phaseKey: "model_generate", eventKey: "start"}
		for _, p := range []mcp.ProgressNotificationParams{
			{ProgressToken: "unrelated", Progress: 1, Meta: start},
			{ProgressToken: token, Progress: 1, Meta: start, Message: privateMessage},
			{ProgressToken: token, Progress: 1, Meta: start, Message: "duplicate"},
			{ProgressToken: token, Progress: 2, Meta: mcp.Meta{phaseKey: privateMessage, eventKey: "start"}},
			{ProgressToken: token, Progress: 3, Meta: mcp.Meta{phaseKey: "model_generate", eventKey: "finish", durationKey: -1, codeKey: "ok"}},
			{ProgressToken: token, Progress: 4, Meta: mcp.Meta{phaseKey: "model_generate", eventKey: "finish", durationKey: 12, codeKey: "ok"}},
			{ProgressToken: token, Progress: 5, Meta: mcp.Meta{phaseKey: "source_read", eventKey: "finish", durationKey: 10, codeKey: privateMessage}},
		} {
			if err := req.Session.NotifyProgress(ctx, &p); err != nil {
				return nil, err
			}
		}
		select {
		case <-release:
			return progressFixtureResult(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, slog.New(slog.NewJSONHandler(&output, nil)))
	updates, result := runProgressTool(t, t.Context(), remote, "{}")
	token := awaitProgress(t, tokens)
	for _, want := range []string{privateMessage + " · 1", "2", "3", "5"} {
		if got := awaitProgress(t, updates); got != want {
			t.Fatalf("progress %q, want %q", got, want)
		}
	}
	close(release)
	assertProgressFixtureResult(t, awaitProgress(t, result))

	logged := output.String()
	if token == "" || strings.Contains(logged, token) || strings.Contains(logged, privateMessage) {
		t.Fatal("raw progress token or remote text reached durable diagnostics")
	}
	var phases []map[string]any
	var outcome map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		switch row["msg"] {
		case "MCP tool progress":
			phases = append(phases, row)
		case "MCP tool call":
			outcome = row
		}
	}
	receipt := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	if len(phases) != 2 || outcome["progress_token_sha256"] != receipt {
		t.Fatalf("want two correlated phases and a matching outcome, got %+v, %+v", phases, outcome)
	}
	for i, event := range []string{"start", "finish"} {
		row := phases[i]
		if row["phase"] != "model_generate" || row["event"] != event || row["progress_token_sha256"] != receipt || row["server_id"] != "progress" {
			t.Fatalf("phase %d not correlated: %+v", i, row)
		}
	}
	if phases[1]["duration_ms"] != float64(12) || phases[1]["code"] != "ok" {
		t.Fatalf("missing sanitized phase duration: %+v", phases[1])
	}
}

func TestMCPPhaseLogHasAPerCallRecordLimit(t *testing.T) {
	var output bytes.Buffer
	call := &activeProgress{
		ctx: t.Context(), started: time.Now(),
		log: slog.New(slog.NewJSONHandler(&output, nil)),
	}
	for i := 1; i <= maxLoggedProgressPhases+1; i++ {
		call.logPhase(&mcp.ProgressNotificationParams{
			Progress: float64(i),
			Meta:     mcp.Meta{"io.github.lubyruffy/phase": "source_read", "io.github.lubyruffy/phase-event": "start"},
		})
	}
	if got := strings.Count(output.String(), "\n"); got != maxLoggedProgressPhases {
		t.Fatalf("wrote %d phase records, want %d", got, maxLoggedProgressPhases)
	}
}

func progressFixture(t *testing.T, handler mcp.ToolHandler, loggers ...*slog.Logger) tool.InvokableTool {
	t.Helper()
	s := mcp.NewServer(&mcp.Implementation{Name: "progress-fixture", Version: "test"}, nil)
	s.AddTool(&mcp.Tool{Name: "work", InputSchema: map[string]any{"type": "object"}}, handler)
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	t.Cleanup(h.Close)
	m := NewManager(loggers...)
	t.Cleanup(m.Close)
	cfg := config.MCPServer{ID: "progress", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 10}
	set, err := m.Tools(t.Context(), []config.MCPServer{cfg}, t.TempDir(), 10000)
	if err != nil || len(set) != 2 {
		t.Fatalf("MCP proxies: %d, %v", len(set), err)
	}
	return set[1].(tool.InvokableTool)
}

type progressToolResult struct {
	text string
	err  error
}

func runProgressTool(t *testing.T, ctx context.Context, remote tool.InvokableTool, arguments string) (<-chan string, <-chan progressToolResult) {
	t.Helper()
	info, err := remote.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan string, 16)
	done := make(chan progressToolResult, 1)
	ctx = tools.BindToolOutput(ctx, func(text string) { updates <- text }, info.Name, "progress-call")
	go func() {
		text, err := remote.InvokableRun(ctx, `{"name":"work","arguments":`+arguments+`}`)
		done <- progressToolResult{text, err}
	}()
	return updates, done
}

func awaitProgress[T any](t *testing.T, event <-chan T) T {
	t.Helper()
	select {
	case value := <-event:
		return value
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for MCP progress fixture")
		var zero T
		return zero
	}
}

func progressFixtureResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "completed external work"}}}
}

func assertProgressFixtureResult(t *testing.T, got progressToolResult) {
	t.Helper()
	if want := jsonResult(progressFixtureResult()); got.err != nil || got.text != want {
		t.Fatalf("progress altered the terminal tool result: %+v, want %s", got, want)
	}
}
