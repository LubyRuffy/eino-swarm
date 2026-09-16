package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// People type the subject of the command first. Go's flag package stops at
// the first positional argument, so without reordering `zwai trace <id>
// --data-dir X` would read the wrong database and report "no such turn".
func TestFlagsAreAcceptedAfterPositionalArguments(t *testing.T) {
	takesValue := map[string]bool{"data-dir": true, "task": true}
	for _, tc := range []struct {
		name string
		in   []string
		want []string
	}{
		{"flag after positional", []string{"tn_1", "--data-dir", "/tmp/x"},
			[]string{"--data-dir", "/tmp/x", "tn_1"}},
		{"already in order", []string{"--data-dir", "/tmp/x", "tn_1"},
			[]string{"--data-dir", "/tmp/x", "tn_1"}},
		{"equals form", []string{"tn_1", "--data-dir=/tmp/x"},
			[]string{"--data-dir=/tmp/x", "tn_1"}},
		{"boolean flag keeps the next positional", []string{"tn_1", "--full", "tn_2"},
			[]string{"--full", "tn_1", "tn_2"}},
		{"everything after -- is positional", []string{"--full", "--", "--not-a-flag"},
			[]string{"--full", "--not-a-flag"}},
		{"nothing", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := reorderFlags(tc.in, takesValue)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("reorderFlags(%v)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

// `zwai config show` is what people paste into a bug report.
func TestConfigShowHidesTheAPIKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_BASE_URL", "http://endpoint.invalid/v1")
	t.Setenv("OPENAI_API_KEY", "super-secret-key")
	t.Setenv("OPENAI_MODEL", "some-model")

	out := captureStdout(t, func() {
		if err := runConfig([]string{"show", "--data-dir", dir}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "super-secret-key") {
		t.Fatalf("the api key is in the output:\n%s", out)
	}
	if !strings.Contains(out, "api_key: <set, hidden>") {
		t.Fatalf("the output should say a key is set:\n%s", out)
	}
	// the rest is still useful, or there is no point printing it
	for _, want := range []string{"endpoint.invalid", "some-model", "max_concurrent"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRedactKeysLeavesEmptyKeysAlone(t *testing.T) {
	in := "providers:\n    - id: a\n      api_key: \"\"\n    - id: b\n      api_key: abc\n"
	got := redactKeys(in)
	if strings.Contains(got, "abc") {
		t.Fatalf("a key survived redaction:\n%s", got)
	}
	if !strings.Contains(got, `api_key: ""`) {
		t.Fatalf("an unset key should stay visibly unset:\n%s", got)
	}
}

func TestConfigPathAndInit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")

	out := captureStdout(t, func() {
		if err := runConfig([]string{"path", "--data-dir", dir}); err != nil {
			t.Fatal(err)
		}
	})
	path := strings.TrimSpace(out)
	if filepath.Dir(path) != dir {
		t.Fatalf("path=%q is not inside %q", path, dir)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the config file was not created: %v", err)
	}

	// With nothing configured, init has to say what to do next rather than
	// reporting success and leaving the user at a dead end.
	out = captureStdout(t, func() {
		if err := runConfig([]string{"init", "--data-dir", dir}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "No model is configured") {
		t.Fatalf("init did not point at the next step:\n%s", out)
	}
}

// One id in, the whole run out: the timeline and every model call.
func TestTracePrintsATurn(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	th := &store.Thread{Title: "Traced"}
	if err := st.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "look into the thing",
		ProviderID: "default", Model: "some-model", ReasoningEffort: "high"}
	if err := st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []store.Event{
		{ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message", AgentID: "manager", Text: "look into the thing"},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: "spawned", AgentID: "researcher-1",
			Role: "researcher", Text: "## Environment\n\nhost facts for this worker"},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: "error", AgentID: "researcher-1", Err: "the endpoint refused the connection"},
	} {
		ev := ev
		if err := st.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.AppendLLMCall(&store.LLMCall{ThreadID: th.ID, TurnID: turn.ID,
		AgentID: "manager", Model: "some-model", InputMsgs: 3, InputChars: 120,
		OutputChars: 40, PromptTokens: 90, CompletionTokens: 12, TotalTokens: 102,
		CachedTokens: 20, DurationMS: 250}); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishTurn(turn.ID, store.TurnDone, "all done", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	byTurn := captureStdout(t, func() {
		if err := runTrace([]string{turn.ID, "--data-dir", dir}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		turn.ID, th.ID, "done", "some-model", "look into the thing",
		// the thinking level the turn ran with rides the one-id trace path
		"thinking high",
		"spawned", "researcher-1", "## Environment",
		// a failure has to be visible in the timeline, not just in the status
		"the endpoint refused the connection",
		"model calls (1)", "250ms", "90/12 tok", "cache 20",
	} {
		if !strings.Contains(byTurn, want) {
			t.Fatalf("trace is missing %q:\n%s", want, byTurn)
		}
	}

	// the conversation id works too, because that is what the address bar has
	byThread := captureStdout(t, func() {
		if err := runTrace([]string{th.ID, "--data-dir", dir}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(byThread, turn.ID) {
		t.Fatalf("tracing a conversation did not include its turn:\n%s", byThread)
	}

	if err := runTrace([]string{"nope", "--data-dir", dir}); err == nil {
		t.Fatal("an unknown id should be an error, not empty output")
	}
	if err := runTrace([]string{"--data-dir", dir}); err == nil {
		t.Fatal("trace with no id should explain what it needs")
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("  a\nb  ", false); got != "a ⏎ b" {
		t.Fatalf("oneLine=%q", got)
	}
	if got := oneLine("a\nb", true); got != "a\nb" {
		t.Fatalf("--full should keep the text intact, got %q", got)
	}
	// Truncation must not split a multi-byte character into a broken glyph.
	long := strings.Repeat("目标", 200)
	got := oneLine(long, false)
	if !strings.HasSuffix(got, "…") || strings.Contains(got, "\uFFFD") {
		t.Fatalf("truncation mangled the text: %q", got)
	}
	if len([]rune(got)) != 110 {
		t.Fatalf("truncated to %d runes, want 110", len([]rune(got)))
	}
}

func TestEventTextPrefersTheError(t *testing.T) {
	if got := eventText(store.Event{Text: "partial", Err: "it broke"}); got != "error: it broke" {
		t.Fatalf("eventText=%q", got)
	}
	if got := eventText(store.Event{Text: "fine"}); got != "fine" {
		t.Fatalf("eventText=%q", got)
	}
}

func TestTUINeedsATask(t *testing.T) {
	_, _, _, err := buildTUISwarm(context.Background(),
		[]string{"--data-dir", t.TempDir(), "--mock"})
	if err == nil || !strings.Contains(err.Error(), "task") {
		t.Fatalf("err=%v, want a message about the missing task", err)
	}
}

// The terminal shell has to be the same swarm as the app: same limits, same
// tools, or reproducing a problem there proves nothing.
func TestTUIBuildsTheConfiguredSwarm(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Swarm.MaxConcurrent = 3
	cfg.Swarm.MaxTurns = 7
	cfg.Swarm.ManagerMaxIterations = 9
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	workspace := t.TempDir()
	reg, text, cleanup, err := buildTUISwarm(context.Background(), []string{
		"--mock", "--data-dir", dir, "--workspace", workspace,
		"--task", "look into the thing",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if text != "look into the thing" {
		t.Fatalf("task=%q", text)
	}
	if reg.MaxConcurrent != 3 || reg.MaxTurns != 7 || reg.ManagerMaxIterations != 9 {
		t.Fatalf("the swarm ignored the configuration: concurrent=%d turns=%d iters=%d",
			reg.MaxConcurrent, reg.MaxTurns, reg.ManagerMaxIterations)
	}
	if len(reg.SubAgentTools) == 0 {
		t.Fatal("sub-agents were given no tools")
	}
	if reg.ModelBuilder == nil || reg.ModelBuilder("worker", "worker-1") == nil {
		t.Fatal("no model builder")
	}
	if !strings.Contains(reg.WorkerPreamble, "OS:") {
		t.Fatal("the terminal swarm would not tell workers which OS they are on")
	}

	// words after the flags are part of the task, so quoting is optional
	_, text, cleanup2, err := buildTUISwarm(context.Background(), []string{
		"--mock", "--data-dir", dir, "--workspace", workspace, "summarise", "the", "notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()
	if text != "summarise the notes" {
		t.Fatalf("task=%q", text)
	}
}

func TestTUIGoalRidesInTheSession(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	setup, err := assembleTUI(context.Background(), []string{
		"--mock", "--data-dir", t.TempDir(), "--task", "look into the thing",
		"--goal", "keep the standing objective",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer setup.cleanup()
	if setup.session.Task != "look into the thing" {
		t.Fatalf("task=%q", setup.session.Task)
	}
	if !strings.Contains(setup.session.Extra, "## Goal") ||
		!strings.Contains(setup.session.Extra, "keep the standing objective") ||
		!strings.Contains(setup.session.Extra, engine.ToolCompleteGoal) ||
		!strings.Contains(setup.session.Extra, engine.ToolBlockGoal) {
		t.Fatalf("goal extra=%q", setup.session.Extra)
	}
	if len(setup.session.ManagerTools) != 2 {
		t.Fatalf("complete_goal and block_goal must be on the manager, got %d", len(setup.session.ManagerTools))
	}
	if setup.session.ShouldContinue == nil || setup.session.ContinueTask == "" {
		t.Fatal("a --goal run must auto-continue until complete_goal")
	}
	if setup.session.MaxContinues <= 0 {
		t.Fatal("auto-continue cap missing")
	}
}

// Without a workspace the terminal run gets a scratch directory that is
// cleaned up, rather than writing into whatever directory it was started in.
func TestTUIUsesAScratchWorkspace(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	reg, _, cleanup, err := buildTUISwarm(context.Background(),
		[]string{"--mock", "--data-dir", t.TempDir(), "--task", "anything"})
	if err != nil {
		t.Fatal(err)
	}
	if reg == nil {
		t.Fatal("no registry")
	}
	cleanup()
}

func TestTUIRefusesAnUnconfiguredProvider(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	_, _, _, err := buildTUISwarm(context.Background(),
		[]string{"--data-dir", t.TempDir(), "--task", "anything"})
	if err == nil {
		t.Fatal("want an error when no model is configured")
	}
	if !strings.Contains(err.Error(), "Settings") && !strings.Contains(err.Error(), "configured") {
		t.Fatalf("the error should say how to fix it, got %v", err)
	}
}

// The desktop command has to bring up a real local server before the window
// can load anything.
func TestDesktopStartsALocalServer(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	opts, a, err := startDesktopServer([]string{"--mock", "--data-dir", t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(opts.URL, "http://127.0.0.1:") {
		t.Fatalf("the window would load %q, which is not a local server", opts.URL)
	}
	if opts.OnShutdown == nil {
		t.Fatal("closing the window must shut the engine down")
	}

	resp, err := http.Get(opts.URL + "/api/meta")
	if err != nil {
		t.Fatalf("the window would have loaded a dead URL: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// the desktop shell advertises what only it can do
	if !strings.Contains(string(body), `"mode":"desktop"`) ||
		!strings.Contains(string(body), `"reveal":true`) ||
		!strings.Contains(string(body), `"open_url":true`) {
		t.Fatalf("meta=%s", body)
	}

	opts.OnShutdown()
	if _, err := http.Get(opts.URL + "/api/meta"); err == nil {
		t.Fatal("closing the window left the server running")
	}
	if a == nil {
		t.Fatal("no app was returned")
	}
}

// `zwai web` has to serve the app and then shut down cleanly: a Ctrl-C that
// leaves turns marked running makes the next start look broken.
func TestWebServesAndShutsDownCleanly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")

	stop := make(chan os.Signal, 1)
	urls := make(chan string, 1)
	errs := make(chan error, 1)
	go func() {
		errs <- serveWeb(
			[]string{"--mock", "--no-open", "--addr", "127.0.0.1:0", "--data-dir", dir},
			stop,
			func(url string) { urls <- url },
		)
	}()

	var url string
	select {
	case url = <-urls:
	case err := <-errs:
		t.Fatalf("the server stopped before it was listening: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("the server never came up")
	}

	resp, err := http.Get(url + "/api/meta")
	if err != nil {
		t.Fatalf("GET meta: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"mode":"web"`) {
		t.Fatalf("meta: %d %s", resp.StatusCode, body)
	}
	// start a turn so shutdown has something in flight to abandon
	resp, err = http.Post(url+"/api/threads", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Thread store.Thread `json:"thread"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	resp, err = http.Post(url+"/api/threads/"+created.Thread.ID+"/turns",
		"application/json", strings.NewReader(`{"text":"something long"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	stop <- os.Interrupt
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("web returned %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the server did not stop on a signal")
	}
	if _, err := http.Get(url + "/api/meta"); err == nil {
		t.Fatal("the server is still serving after shutdown")
	}

	// Quit is not a user Stop: leftover turns stay running so the next start
	// continues them. A turn that finished before the signal landed is done.
	st, err := store.Open(filepath.Join(dir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	turns, err := st.ListTurns(created.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) == 0 {
		t.Fatal("the turn was not recorded")
	}
	for _, turn := range turns {
		if turn.Status == store.TurnCancelled {
			t.Fatalf("quit recorded a user stop: %+v", turn)
		}
	}
}

func TestWebRejectsAnUnusableAddress(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	err := serveWeb([]string{"--mock", "--no-open", "--data-dir", t.TempDir(),
		"--addr", "127.0.0.1:not-a-port"}, make(chan os.Signal), nil)
	if err == nil {
		t.Fatal("want an error for an address that cannot be bound")
	}
}

func TestDispatchReportsUnknownCommands(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := dispatch([]string{"nonsense"}, &out, &errOut); code != 2 {
		t.Fatalf("exit code %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown command") ||
		!strings.Contains(errOut.String(), "usage:") {
		t.Fatalf("an unknown command should say what is available:\n%s", errOut.String())
	}

	out.Reset()
	if code := dispatch([]string{"version"}, &out, &errOut); code != 0 {
		t.Fatalf("version exit code %d", code)
	}
	if !strings.Contains(out.String(), version) {
		t.Fatalf("version output=%q", out.String())
	}

	out.Reset()
	if code := dispatch([]string{"help"}, &out, &errOut); code != 0 {
		t.Fatalf("help exit code %d", code)
	}
	for _, want := range []string{"desktop", "web", "tui", "trace", "config", "--mock"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help does not mention %q:\n%s", want, out.String())
		}
	}

	// a failing command reports the failure and exits non-zero
	errOut.Reset()
	if code := dispatch([]string{"trace"}, &out, &errOut); code != 1 {
		t.Fatalf("a failing command should exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "zwai:") {
		t.Fatalf("stderr=%q", errOut.String())
	}
}

// The built-in help is the only documentation most people read, and it drifts
// silently: a flag added to a subcommand does not fail anything by being absent
// from usage(). So read the flags out of this package's own source and require
// each one to be mentioned.
func TestUsageMentionsEveryFlag(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := regexp.MustCompile(`fs\.(?:String|Bool|Int)\("([a-z-]+)"`)

	var help bytes.Buffer
	usage(&help)
	text := help.String()

	seen := map[string]bool{}
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range declared.FindAllStringSubmatch(string(raw), -1) {
			seen[m[1]] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("found no flags to check; the pattern no longer matches the code")
	}
	for name := range seen {
		if !strings.Contains(text, "--"+name) {
			t.Errorf("usage() never mentions --%s:\n%s", name, text)
		}
	}
}

// The usage line reads `zwai [desktop] [--data-dir DIR] [--mock]`, so a flag
// with no subcommand has to open the app rather than report an unknown command.
func TestFlagsWithNoSubcommandOpenTheApp(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want []string
	}{
		{nil, []string{"desktop"}},
		{[]string{"--data-dir", "/tmp/x"}, []string{"desktop", "--data-dir", "/tmp/x"}},
		{[]string{"--mock"}, []string{"desktop", "--mock"}},
		// asking about the program itself is not asking it to open
		{[]string{"--version"}, []string{"--version"}},
		{[]string{"-v"}, []string{"-v"}},
		{[]string{"--help"}, []string{"--help"}},
		{[]string{"-h"}, []string{"-h"}},
		{[]string{"web", "--mock"}, []string{"web", "--mock"}},
	} {
		got := withDefaultCommand(tc.in)
		if strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Fatalf("withDefaultCommand(%v)=%v want %v", tc.in, got, tc.want)
		}
	}
}

// A failed turn is the reason people reach for trace, so the failure has to be
// in the output — the turn's error, the failing model call, and an event that
// belongs to the run rather than to an agent.
func TestTraceShowsWhatWentWrong(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	th := &store.Thread{Title: "Broken"}
	if err := st.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	first := &store.Turn{ThreadID: th.ID, UserText: "first"}
	if err := st.CreateTurn(first); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishTurn(first.ID, store.TurnDone, "fine", ""); err != nil {
		t.Fatal(err)
	}
	second := &store.Turn{ThreadID: th.ID, UserText: "second"}
	if err := st.CreateTurn(second); err != nil {
		t.Fatal(err)
	}
	// so the recorded duration rounds to something rather than nothing
	time.Sleep(2 * time.Millisecond)
	// an event with no agent behind it: the engine's own bookkeeping
	if err := st.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: second.ID,
		Kind: "cleanup", Text: "cancelled at turn end"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLLMCall(&store.LLMCall{ThreadID: th.ID, TurnID: second.ID,
		AgentID: "manager", Model: "some-model", DurationMS: 90,
		Err: "the endpoint returned 429"}); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishTurn(second.ID, store.TurnError, "", "gave up after 429"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := runTrace([]string{th.ID, "--data-dir", dir}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		first.ID, second.ID, // a conversation prints every turn, in order
		"gave up after 429",                // the turn's own error, next to its status
		"ERROR: the endpoint returned 429", // and the call that caused it
		"cleanup",
		"took", // a finished turn reports how long it took
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("trace is missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, first.ID) > strings.Index(out, second.ID) {
		t.Fatalf("turns are out of order:\n%s", out)
	}
}

// The data directory is a flag like any other: it has to work before the
// subcommand as well as after it, and an unusable one has to be reported rather
// than silently falling back to the real one.
func TestConfigAcceptsTheDataDirBeforeTheSubcommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")

	out := captureStdout(t, func() {
		if err := runConfig([]string{"--data-dir", dir, "path"}); err != nil {
			t.Fatal(err)
		}
	})
	if filepath.Dir(strings.TrimSpace(out)) != dir {
		t.Fatalf("path=%q is not inside %q", strings.TrimSpace(out), dir)
	}

	// a file where the data directory should be: nothing can be written there
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(blocked, "zwai")
	if err := runConfig([]string{"show", "--data-dir", nested}); err == nil {
		t.Fatal("an unusable data directory should be reported")
	}
	if err := runTrace([]string{"tn_1", "--data-dir", nested}); err == nil {
		t.Fatal("trace should report an unusable data directory")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()
	func() {
		defer func() {
			os.Stdout = prev
			_ = w.Close()
		}()
		fn()
	}()
	select {
	case out := <-done:
		return out
	case <-time.After(10 * time.Second):
		t.Fatal("timed out reading stdout")
		return ""
	}
}

// Typing the command wrong and the command failing are different things, and
// scripts tell them apart by the exit code.
func TestDispatchSeparatesMisuseFromFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")

	var out, errOut bytes.Buffer
	if code := dispatch([]string{"config", "frobnicate", "--data-dir", dir}, &out, &errOut); code != 2 {
		t.Fatalf("exit code %d, want 2 for a misspelt subcommand", code)
	}
	if !strings.Contains(errOut.String(), "zwai config [path|init|show]") {
		t.Fatalf("stderr should list the subcommands:\n%s", errOut.String())
	}
}

// --full is there for the events that matter most: a stack trace or a model
// reply truncated to one line is useless when you are chasing a bug.
func TestTraceFullKeepsLongTextIntact(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	th := &store.Thread{Title: "Long"}
	if err := st.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "go"}
	if err := st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	long := strings.TrimSpace(strings.Repeat("detail ", 60))
	if err := st.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID,
		Kind: "assistant_message", AgentID: "manager", Text: long}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	short := captureStdout(t, func() {
		if err := runTrace([]string{turn.ID, "--data-dir", dir}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(short, long) {
		t.Fatal("without --full the event text should be shortened")
	}
	full := captureStdout(t, func() {
		if err := runTrace([]string{turn.ID, "--full", "--data-dir", dir}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(full, long) {
		t.Fatalf("--full dropped part of the text:\n%s", full)
	}
}

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
