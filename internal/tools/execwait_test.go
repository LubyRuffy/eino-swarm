package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type stubExec struct {
	ran string
}

func (s *stubExec) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "exec", Desc: "Execute bash commands."}, nil
}

func (s *stubExec) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	s.ran = args
	return "ran", nil
}

func TestExecSleepWaitParsesTheLongestPause(t *testing.T) {
	// Duration table input is a sample, not a product rule. The rule is
	// "longest numeric sleep", so a padded wait can be biased short.
	cases := []struct {
		cmd  string
		want time.Duration
	}{
		{`sleep 150`, 150 * time.Second},
		{`sleep 150; echo done`, 150 * time.Second},
		{`sleep 2m`, 2 * time.Minute},
		{`Start-Sleep -Seconds 90`, 90 * time.Second},
		{`Start-Sleep 60`, 60 * time.Second},
		{`Start-Sleep -Milliseconds 8000`, 8 * time.Second},
		{`printf a; sleep 0.3; printf b`, 300 * time.Millisecond},
		{`sleep 10; sleep 1`, 10 * time.Second},
		{`sleep 2h`, 2 * time.Hour},
		{`sleep 1d`, 24 * time.Hour},
		{`sleep infinity`, 0},
		{``, 0},
		{`true`, 0},
	}
	for _, tc := range cases {
		if got := execSleepWait(tc.cmd); got != tc.want {
			t.Fatalf("execSleepWait(%q)=%s want %s", tc.cmd, got, tc.want)
		}
	}
}

func TestBiasExecSleepShortensALongWaitToAThird(t *testing.T) {
	// sleep 150 is the user's example of a padded remaining-time wait, not
	// a duration we bake into the product. The rule is about a third.
	cases := []struct {
		in, want string
	}{
		{`sleep 150`, `sleep 50`},
		{`sleep 150; echo done`, `sleep 50; echo done`},
		{`sleep 2m`, `sleep 40`},
		{`printf a; sleep 0.3; printf b`, `printf a; sleep 0.3; printf b`},
		{`sleep 4`, `sleep 4`},
		{`Start-Sleep -Seconds 90`, `Start-Sleep -Seconds 30`},
		{`Start-Sleep -Seconds 2`, `Start-Sleep -Seconds 2`},
		{`Start-Sleep 60`, `Start-Sleep 20`},
		{`Start-Sleep -Milliseconds 8000`, `Start-Sleep -Milliseconds 2666`},
		{`Start-Sleep -Milliseconds 400`, `Start-Sleep -Milliseconds 400`},
		{`sleep infinity`, `sleep infinity`},
		{`sleep 10; sleep 1`, `sleep 3.333; sleep 1`},
		{`true`, `true`},
	}
	for _, tc := range cases {
		if got := biasExecSleepCommand(tc.in); got != tc.want {
			t.Fatalf("biasExecSleepCommand(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestExecRunsABiasedSleepInsteadOfRefusingIt(t *testing.T) {
	inner := &stubExec{}
	gate, err := wrapExecSleepBias(inner)
	if err != nil {
		t.Fatal(err)
	}
	out, err := gate.(tool.InvokableTool).InvokableRun(context.Background(),
		`{"timeout_ms":200000,"command":"sleep 150; echo done"}`)
	if err != nil || out != "ran" {
		t.Fatalf("a progress-poll sleep must still run: %q %v", out, err)
	}
	var got struct {
		Command   string `json:"command"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	if err := json.Unmarshal([]byte(inner.ran), &got); err != nil {
		t.Fatalf("inner args %q: %v", inner.ran, err)
	}
	if got.Command != "sleep 50; echo done" {
		t.Fatalf("command=%q; the sleep must run at about a third, not be refused", got.Command)
	}
	if got.TimeoutMS != 200000 {
		t.Fatalf("timeout_ms=%d; other exec fields must survive the rewrite", got.TimeoutMS)
	}
}

func TestExecLeavesAShortPauseBetweenCommands(t *testing.T) {
	inner := &stubExec{}
	gate, err := wrapExecSleepBias(inner)
	if err != nil {
		t.Fatal(err)
	}
	out, err := gate.(tool.InvokableTool).InvokableRun(context.Background(),
		`{"command":"printf a; sleep 0.3; printf b"}`)
	if err != nil || out != "ran" {
		t.Fatalf("short pause: %q %v", out, err)
	}
	if !strings.Contains(inner.ran, `sleep 0.3`) {
		t.Fatalf("a sub-second pause must stay as written: %s", inner.ran)
	}
}

func TestExecInfoBiasesARemainingTimeSleep(t *testing.T) {
	gate, err := wrapExecSleepBias(&stubExec{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := gate.Info(context.Background())
	if err != nil || info == nil {
		t.Fatalf("info: %+v %v", info, err)
	}
	if info.Name != "exec" {
		t.Fatalf("name=%q", info.Name)
	}
	if !strings.Contains(info.Desc, "about a third of that estimate") {
		t.Fatalf("exec Info must bias a remaining-time sleep short:\n%s", info.Desc)
	}
	if strings.Contains(info.Desc, "Do not sleep") {
		t.Fatal("a parallel progress-poll sleep is allowed; Info must not ban it")
	}
	for _, leak := range []string{"git", "merge", "deploy", "150"} {
		if strings.Contains(strings.ToLower(info.Desc), leak) {
			t.Fatalf("Info leaked %q: %s", leak, info.Desc)
		}
	}
}

func TestBuiltExecInfoBiasesSleep(t *testing.T) {
	cfg := configFor(t)
	set, err := Build(context.Background(), cfg, filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := find(t, set, "exec").Info(context.Background())
	if err != nil || info == nil {
		t.Fatalf("info: %+v %v", info, err)
	}
	if !strings.Contains(info.Desc, "about a third of that estimate") {
		t.Fatalf("built exec must carry the sleep bias:\n%s", info.Desc)
	}
}

func TestWrapExecSleepBiasRejectsANonInvokable(t *testing.T) {
	_, err := wrapExecSleepBias(infoOnlyTool{})
	if err == nil {
		t.Fatal("a non-invokable exec must fail the wrap")
	}
}

type infoOnlyTool struct{}

func (infoOnlyTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "exec"}, nil
}

func TestParseSleepAmountRejectsJunk(t *testing.T) {
	if d := parseSleepAmount("nope", ""); d != 0 {
		t.Fatalf("junk number=%s", d)
	}
	if d := parseSleepAmount("-1", "s"); d != 0 {
		t.Fatalf("negative=%s", d)
	}
	if d := parseSleepAmount("1", "x"); d != 0 {
		t.Fatalf("unknown unit=%s", d)
	}
	if d := parseSleepMillis("nope"); d != 0 {
		t.Fatalf("junk millis=%s", d)
	}
	if d := parseSleepMillis("-1"); d != 0 {
		t.Fatalf("negative millis=%s", d)
	}
}

func TestExecSleepBiasInfoPassesInnerFailures(t *testing.T) {
	gate, err := wrapExecSleepBias(&errInfoExec{err: errInfo})
	if err != nil {
		t.Fatal(err)
	}
	info, got := gate.Info(context.Background())
	if info != nil || got != errInfo {
		t.Fatalf("inner Info error: %+v %v", info, got)
	}
	nilGate, err := wrapExecSleepBias(&nilInfoExec{})
	if err != nil {
		t.Fatal(err)
	}
	info, got = nilGate.Info(context.Background())
	if info != nil || got != nil {
		t.Fatalf("nil Info: %+v %v", info, got)
	}
	labelled, err := wrapExecSleepBias(&stubExecDesc{})
	if err != nil {
		t.Fatal(err)
	}
	info, err = labelled.Info(context.Background())
	if err != nil || info == nil {
		t.Fatalf("labelled Info: %+v %v", info, err)
	}
	if n := strings.Count(info.Desc, "about a third of that estimate"); n != 1 {
		t.Fatalf("appended the bias warning twice: %q", info.Desc)
	}
}

func TestExecSleepBiasPassesInvalidJSONToInner(t *testing.T) {
	inner := &stubExec{}
	gate, err := wrapExecSleepBias(inner)
	if err != nil {
		t.Fatal(err)
	}
	out, err := gate.(tool.InvokableTool).InvokableRun(context.Background(), `{`)
	if err != nil || out != "ran" {
		t.Fatalf("invalid json: %q %v", out, err)
	}
	if inner.ran != `{` {
		t.Fatalf("inner got %q", inner.ran)
	}
}

func TestBiasExecSleepArgsLeavesNonCommandJSON(t *testing.T) {
	if got := biasExecSleepArgs(`{"timeout_ms":1}`); got != `{"timeout_ms":1}` {
		t.Fatalf("no command: %s", got)
	}
	in := `{"command":1}`
	if got := biasExecSleepArgs(in); got != in {
		t.Fatalf("non-string command: %s", got)
	}
}

type errInfoExec struct {
	stubExec
	err error
}

var errInfo = errInfoSentinel("info failed")

type errInfoSentinel string

func (e errInfoSentinel) Error() string { return string(e) }

func (e *errInfoExec) Info(context.Context) (*schema.ToolInfo, error) {
	return nil, e.err
}

type nilInfoExec struct{ stubExec }

func (*nilInfoExec) Info(context.Context) (*schema.ToolInfo, error) {
	return nil, nil
}

type stubExecDesc struct{ stubExec }

func (*stubExecDesc) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "exec",
		Desc: "Execute. A sleep that polls remaining time uses about a third of that estimate.",
	}, nil
}
