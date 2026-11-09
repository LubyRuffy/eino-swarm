package mcpclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/compose"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxProgressMessageRunes = 160
	maxProgressMessageScan  = 1024
	maxLoggedProgressPhases = 64
)

type activeProgress struct {
	ctx     context.Context
	session *mcp.ClientSession
	emit    func(string)
	last    float64
	log     *slog.Logger
	started time.Time
	logged  int
}

func (e *entry) callTool(ctx context.Context, session *mcp.ClientSession, params *mcp.CallToolParams, logger *slog.Logger) (*mcp.CallToolResult, error, string) {
	emit := tools.ToolOutputEmitter(ctx)
	if emit == nil {
		result, err := session.CallTool(ctx, params)
		return result, err, ""
	}
	token := rand.Text()
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	params.SetProgressToken(token)
	log := logger.With("server_id", e.server.ID, "tool_call_id", compose.GetToolCallID(ctx),
		"tool", progressMessage(params.Name), "progress_token_sha256", digest)
	e.progressMu.Lock()
	if e.progress == nil {
		e.progress = make(map[string]*activeProgress)
	}
	e.progress[token] = &activeProgress{
		ctx: ctx, session: session, emit: emit, last: -1, log: log, started: time.Now(),
	}
	e.progressMu.Unlock()
	defer func() {
		e.progressMu.Lock()
		delete(e.progress, token)
		e.progressMu.Unlock()
	}()
	result, err := session.CallTool(ctx, params)
	return result, err, digest
}

func (e *entry) handleProgress(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
	if req == nil || req.Params == nil {
		return
	}
	p := req.Params
	token, ok := p.ProgressToken.(string)
	if !ok || !validProgressNumber(p.Progress) || !validProgressNumber(p.Total) || (p.Total > 0 && p.Progress > p.Total) {
		return
	}
	// Keep publication ordered with registration removal. The tool result
	// cannot overtake an accepted live update, and late notifications see no sink.
	e.progressMu.Lock()
	defer e.progressMu.Unlock()
	call := e.progress[token]
	if call == nil || call.session != req.Session || call.ctx.Err() != nil || e.ctx.Err() != nil || p.Progress <= call.last {
		return
	}
	message := progressMessage(p.Message)
	// The SDK decodes both absent progress and a genuine zero as 0. A safe
	// phase label still makes an initial zero useful to the user.
	if p.Progress == 0 && message == "" {
		return
	}
	count := strconv.FormatFloat(p.Progress, 'g', -1, 64)
	if p.Total > 0 {
		count += " / " + strconv.FormatFloat(p.Total, 'g', -1, 64)
	}
	if message != "" {
		count = message + " · " + count
	}
	call.last = p.Progress
	if call.logPhase(p) && message == "" && p.Meta["io.github.lubyruffy/phase-event"] == "finish" {
		return
	}
	call.emit(count)
}

// Persist only fixed stage metadata from an active notification. A server's
// free-form progress text belongs to the live UI and may contain task data.
func (call *activeProgress) logPhase(p *mcp.ProgressNotificationParams) bool {
	if call.log == nil || call.logged >= maxLoggedProgressPhases {
		return false
	}
	phase, _ := p.Meta["io.github.lubyruffy/phase"].(string)
	switch phase {
	case "task", "prepare_model", "model_generate", "source_access", "source_schema", "source_read":
	default:
		return false
	}
	event, _ := p.Meta["io.github.lubyruffy/phase-event"].(string)
	attrs := []slog.Attr{
		slog.String("phase", phase), slog.String("event", event),
		slog.Float64("progress", p.Progress),
		slog.Int64("elapsed_ms", time.Since(call.started).Milliseconds()),
	}
	switch event {
	case "start":
	case "finish":
		code, _ := p.Meta["io.github.lubyruffy/phase-code"].(string)
		switch code {
		case "ok", "canceled", "timeout", "failed", "busy", "partial", "no_evidence", "no_data":
		default:
			return false
		}
		duration, ok := p.Meta["io.github.lubyruffy/phase-duration-ms"].(float64)
		if !ok || duration < 0 || duration > float64(time.Hour.Milliseconds()) || math.Trunc(duration) != duration {
			return false
		}
		attrs = append(attrs, slog.String("code", code), slog.Int64("duration_ms", int64(duration)))
	default:
		return false
	}
	call.logged++
	call.log.LogAttrs(call.ctx, slog.LevelInfo, "MCP tool progress", attrs...)
	return true
}

func validProgressNumber(value float64) bool {
	return value >= 0 && !math.IsInf(value, 0)
}

func progressMessage(message string) string {
	line := make([]rune, 0, min(len(message), maxProgressMessageRunes))
firstLine:
	for offset, r := range message {
		if offset >= maxProgressMessageScan || len(line) >= maxProgressMessageRunes {
			break
		}
		switch r {
		case '\r', '\n', '\v', '\f', '\u0085', '\u2028', '\u2029':
			break firstLine
		}
		if unicode.IsSpace(r) {
			if len(line) > 0 && line[len(line)-1] != ' ' {
				line = append(line, ' ')
			}
		} else if unicode.IsPrint(r) {
			line = append(line, r)
		}
	}
	return strings.TrimSpace(string(line))
}
