package tui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	tea "github.com/charmbracelet/bubbletea"
)

// ClientConfig is one terminal attached to a running engine.
type ClientConfig struct {
	BaseURL     string
	Task        string
	Goal        string
	Plan        string
	Model       string
	Reasoning   string
	Interactive bool
	Workspace   string
}

// Client talks to zwai engine over loopback HTTP. It does not open the database.
type Client struct {
	Base string
	HTTP *http.Client
}

func NewClient(base string) Client {
	return Client{Base: strings.TrimRight(base, "/"), HTTP: http.DefaultClient}
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func (c Client) do(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var ae apiError
		_ = json.Unmarshal(raw, &ae)
		if ae.Code != "" {
			return &statusError{Status: resp.StatusCode, Code: ae.Code, Msg: ae.Error}
		}
		if ae.Error != "" {
			return &statusError{Status: resp.StatusCode, Msg: ae.Error}
		}
		return &statusError{Status: resp.StatusCode, Msg: strings.TrimSpace(string(raw))}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

type statusError struct {
	Status int
	Code   string
	Msg    string
}

func (e *statusError) Error() string {
	if e.Code != "" {
		return e.Code + ": " + e.Msg
	}
	return e.Msg
}

// OpenThread creates a conversation, or reuses the latest one when reuse is set
// and the inbox is not empty.
func (c Client) OpenThread(ctx context.Context, cfg ClientConfig) (string, error) {
	var projectID string
	if dir := strings.TrimSpace(cfg.Workspace); dir != "" {
		var created struct {
			Project struct {
				ID string `json:"id"`
			} `json:"project"`
		}
		if err := c.do(ctx, http.MethodPost, "/api/projects", map[string]any{
			"name": "terminal", "workdir": dir,
		}, &created); err != nil {
			return "", err
		}
		projectID = created.Project.ID
	}
	if cfg.Interactive && strings.TrimSpace(cfg.Task) == "" && strings.TrimSpace(cfg.Goal) == "" && projectID == "" {
		var list struct {
			Threads []struct {
				ID string `json:"id"`
			} `json:"threads"`
		}
		if err := c.do(ctx, http.MethodGet, "/api/threads", nil, &list); err != nil {
			return "", err
		}
		if len(list.Threads) > 0 && list.Threads[0].ID != "" {
			return list.Threads[0].ID, nil
		}
	}
	body := map[string]any{}
	if projectID != "" {
		body["project_id"] = projectID
	}
	var created struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/threads", body, &created); err != nil {
		return "", err
	}
	if created.Thread.ID == "" {
		return "", errors.New("tui: engine did not return a conversation")
	}
	patch := map[string]any{}
	if g := strings.TrimSpace(cfg.Goal); g != "" && strings.TrimSpace(cfg.Task) != "" {
		patch["goal"] = g
	}
	if m := strings.TrimSpace(cfg.Model); m != "" {
		patch["model"] = m
	}
	if r := strings.TrimSpace(cfg.Reasoning); r != "" {
		patch["reasoning_effort"] = r
	}
	// /plan as the first message already enters planning. A plan flag next
	// to a task or a goal is the same mode, set before that message runs.
	if p := strings.TrimSpace(cfg.Plan); p != "" && !strings.HasPrefix(strings.TrimSpace(cfg.Task), "/plan ") {
		patch["plan_mode"] = true
	}
	if len(patch) > 0 {
		if err := c.do(ctx, http.MethodPatch, "/api/threads/"+created.Thread.ID, patch, nil); err != nil {
			return "", err
		}
	}
	return created.Thread.ID, nil
}

// Send starts a turn, or queues a follow-up when one is already running.
// An ask_user that is waiting is an answer, not a second turn.
func (c Client) Send(ctx context.Context, threadID, text string, asking bool) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if asking {
		return c.do(ctx, http.MethodPost, "/api/threads/"+threadID+"/answers", map[string]any{"text": text}, nil)
	}
	if done, err := c.applySessionCommand(ctx, threadID, text); done {
		return err
	}
	err := c.do(ctx, http.MethodPost, "/api/threads/"+threadID+"/turns", map[string]any{"text": text}, nil)
	var se *statusError
	if errors.As(err, &se) && se.Status == http.StatusConflict && se.Code == "busy" {
		return c.do(ctx, http.MethodPost, "/api/threads/"+threadID+"/followups", map[string]any{"text": text}, nil)
	}
	return err
}

// Steer injects into the running turn. An idle engine starts one instead.
func (c Client) Steer(ctx context.Context, threadID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return c.do(ctx, http.MethodPost, "/api/threads/"+threadID+"/steer", map[string]any{"text": text}, nil)
}

// Interrupt cancels the running turn. The screen stays attached.
func (c Client) Interrupt(ctx context.Context, threadID string) error {
	return c.do(ctx, http.MethodPost, "/api/threads/"+threadID+"/interrupt", nil, nil)
}

// applySessionCommand turns /model and /reason into a thread patch. Those
// lines are not turns; the engine would otherwise run them as user text.
func (c Client) applySessionCommand(ctx context.Context, threadID, text string) (bool, error) {
	name, arg, ok := parseTUICommand(text)
	if !ok {
		return false, nil
	}
	switch name {
	case "model":
		arg = strings.TrimSpace(arg)
		if arg == "" {
			return true, errors.New("tui: /model needs a name")
		}
		return true, c.do(ctx, http.MethodPatch, "/api/threads/"+threadID, map[string]any{"model": arg}, nil)
	case "reason":
		arg = strings.TrimSpace(arg)
		if strings.EqualFold(arg, "default") {
			arg = ""
		}
		return true, c.do(ctx, http.MethodPatch, "/api/threads/"+threadID, map[string]any{"reasoning_effort": arg}, nil)
	default:
		return false, nil
	}
}

func (c Client) Running(ctx context.Context, threadID string) bool {
	var body struct {
		Status struct {
			Running bool `json:"running"`
		} `json:"status"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/threads/"+threadID, nil, &body); err != nil {
		return false
	}
	return body.Status.Running
}

// HoldPresence blocks until ctx ends. The engine counts this connection.
func (c Client) HoldPresence(ctx context.Context) error {
	var reserved struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/presence", map[string]any{"surface": "tui", "pid": os.Getpid()}, &reserved); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/api/presence/"+reserved.ID, nil)
	if err != nil {
		return err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// Stream emits transcript messages for one conversation. It replays stored
// events and then follows the live stream.
func (c Client) Stream(ctx context.Context, threadID string) (<-chan notificationMsg, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/api/threads/"+threadID+"/events?since=0", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("tui: event stream %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	out := make(chan notificationMsg, 64)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		readSSE(resp.Body, func(event, data string) {
			for _, msg := range wireMessages(event, data) {
				select {
				case <-ctx.Done():
					return
				case out <- msg:
				}
			}
		})
	}()
	return out, nil
}

func wireMessages(event, data string) []notificationMsg {
	var ev struct {
		Kind       string `json:"kind"`
		AgentID    string `json:"agent_id"`
		Role       string `json:"role"`
		Text       string `json:"text"`
		ToolCallID string `json:"tool_call_id"`
		Err        string `json:"err"`
		Status     *struct {
			Running bool `json:"running"`
		} `json:"status"`
	}
	_ = json.Unmarshal([]byte(data), &ev)
	kindName := event
	if kindName == "" {
		kindName = ev.Kind
	}
	switch kindName {
	case "ready", "ping":
		return nil
	case "user_message":
		if strings.TrimSpace(ev.Text) == "" {
			return nil
		}
		return []notificationMsg{{userText: ev.Text}}
	}
	kind, ok := swarm.ParseNotifyKind(kindName)
	if !ok {
		if text := strings.TrimSpace(ev.Text); text != "" && len(text) < 240 {
			return []notificationMsg{{notice: text}}
		}
		return nil
	}
	n := swarm.Notification{
		Kind: kind, AgentID: ev.AgentID, Role: ev.Role, Text: ev.Text, ToolCallID: ev.ToolCallID,
	}
	if ev.Err != "" {
		n.Err = errors.New(ev.Err)
	}
	msg := notificationMsg{Notification: n}
	if kind == swarm.NotifyDone || kind == swarm.NotifyError {
		return []notificationMsg{msg, {idle: true}}
	}
	return []notificationMsg{msg}
}

func readSSE(r io.Reader, fn func(event, data string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	var event, data string
	flush := func() {
		if data == "" && event == "" {
			return
		}
		fn(event, data)
		event, data = "", ""
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			if data != "" {
				data += "\n"
			}
			data += value
		}
	}
	flush()
}

// RunClient paints the engine's event stream and sends what you type.
func RunClient(ctx context.Context, cfg ClientConfig) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	tm := newModel(nil)
	tm.remote = true
	notifCh := make(chan notificationMsg, 512)
	var prompts chan string
	if cfg.Interactive {
		prompts = tm.openComposer("")
		tm.steers = make(chan string, 8)
		tm.stops = make(chan struct{}, 4)
	}
	go pumpClient(ctx, cfg, tm.asking, notifCh, prompts, tm.steers, tm.stops)
	tm.notifications = notifCh

	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithContext(ctx)}
	if cfg.Interactive {
		tm.ime = &imeAnchor{}
		opts = append(opts, tea.WithOutput(newIMEWriter(os.Stdout, tm.ime)))
	}
	finalModel, progErr := tea.NewProgram(tm, opts...).Run()
	if progErr != nil {
		fmt.Fprintln(os.Stderr, "error:", progErr)
	}
	final := finalOf(tm, finalModel)
	fmt.Println()
	fmt.Println(final.DumpTranscript())
	if final.finErr != nil {
		fmt.Fprintln(os.Stderr, "error:", final.finErr)
		exitProcess(1)
	}
}

// exitProcess is os.Exit in the product. Tests replace it so a failed turn
// does not kill the test binary.
var exitProcess = os.Exit

func pumpClient(ctx context.Context, cfg ClientConfig, asking *atomic.Bool, out chan<- notificationMsg, prompts, steers <-chan string, stops <-chan struct{}) {
	defer close(out)
	c := NewClient(cfg.BaseURL)
	go func() { _ = c.HoldPresence(ctx) }()
	threadID, err := c.OpenThread(ctx, cfg)
	if err != nil {
		out <- notificationMsg{Notification: swarm.Notification{Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: err}}
		return
	}
	events, err := c.Stream(ctx, threadID)
	if err != nil {
		out <- notificationMsg{Notification: swarm.Notification{Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: err}}
		return
	}
	if text := firstSend(cfg); text != "" {
		if err := c.Send(ctx, threadID, text, false); err != nil {
			out <- notificationMsg{Notification: swarm.Notification{Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: err}}
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-events:
			if !ok {
				return
			}
			out <- msg
			if msg.idle && prompts == nil && !c.settledRunning(ctx, threadID) {
				return
			}
		case text, ok := <-promptsOrNil(prompts):
			if !ok {
				return
			}
			if err := c.Send(ctx, threadID, text, asking != nil && asking.Load()); err != nil {
				out <- notificationMsg{Notification: swarm.Notification{Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: err}}
			}
		case text, ok := <-promptsOrNil(steers):
			if !ok {
				return
			}
			if err := c.Steer(ctx, threadID, text); err != nil {
				out <- notificationMsg{Notification: swarm.Notification{Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: err}}
			}
		case _, ok := <-stopsOrNil(stops):
			if !ok {
				return
			}
			if err := c.Interrupt(ctx, threadID); err != nil {
				out <- notificationMsg{Notification: swarm.Notification{Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: err}}
			}
		}
	}
}

func promptsOrNil(ch <-chan string) <-chan string { return ch }

func stopsOrNil(ch <-chan struct{}) <-chan struct{} { return ch }

func (c Client) settledRunning(ctx context.Context, threadID string) bool {
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		if c.Running(ctx, threadID) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(40 * time.Millisecond):
		}
	}
	return c.Running(ctx, threadID)
}

func firstSend(cfg ClientConfig) string {
	if text := strings.TrimSpace(cfg.Task); text != "" {
		return text
	}
	if g := strings.TrimSpace(cfg.Goal); g != "" {
		return "/goal " + g
	}
	return ""
}
