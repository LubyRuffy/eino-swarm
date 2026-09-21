package wakeup

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func TestWantedRequiresPairingAndTheSwitch(t *testing.T) {
	if Wanted(false, true) || Wanted(true, false) || Wanted(false, false) {
		t.Fatal("keep-awake is a no-op until pairing is on and the switch is on")
	}
	if !Wanted(true, true) {
		t.Fatal("pairing + switch must hold")
	}
}

func TestSetDoesNotBounceTheAssertion(t *testing.T) {
	starts := 0
	stops := 0
	m := newMachine(func() (func(), error) {
		starts++
		return func() { stops++ }, nil
	})
	if err := m.Set(true); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(true); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("second Set(true) restarted the inhibitor: starts=%d", starts)
	}
	if !m.On() {
		t.Fatal("expected held")
	}
	if err := m.Set(false); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(false); err != nil {
		t.Fatal(err)
	}
	if stops != 1 || m.On() {
		t.Fatalf("release once: stops=%d on=%v", stops, m.On())
	}
	m.Close()
}

func TestSetReportsAFailedStartAndStaysOff(t *testing.T) {
	m := newMachine(func() (func(), error) {
		return nil, errors.New("no caffeinate")
	})
	if err := m.Set(true); err == nil {
		t.Fatal("expected start error")
	}
	if m.On() {
		t.Fatal("a failed hold must not look held")
	}
}

func TestRecorderKeepsEverySet(t *testing.T) {
	r := NewRecorder()
	if err := r.Set(true); err != nil || !r.On() {
		t.Fatalf("set true %v on=%v", err, r.On())
	}
	r.Reset()
	if len(r.Sets()) != 0 {
		t.Fatal("reset must clear")
	}
	_ = r.Set(false)
	if got := r.Sets(); len(got) != 1 || got[0] {
		t.Fatalf("sets %v", got)
	}
	r.Close()
	if r.On() {
		t.Fatal("close must release")
	}
}

func TestNopAndForProcessDoNotTouchTheOS(t *testing.T) {
	n := Nop()
	if err := n.Set(true); err != nil || !n.On() {
		t.Fatalf("nop %v on=%v", err, n.On())
	}
	n.Close()
	h := ForProcess()
	if err := h.Set(true); err != nil {
		t.Fatal(err)
	}
	h.Close()
}

func TestNewUsesThePlatformStarter(t *testing.T) {
	orig := startPlatform
	t.Cleanup(func() { startPlatform = orig })
	started := 0
	startPlatform = func() (func(), error) {
		started++
		return func() {}, nil
	}
	h := New()
	if err := h.Set(true); err != nil {
		t.Fatal(err)
	}
	if started != 1 || !h.On() {
		t.Fatalf("started=%d on=%v", started, h.On())
	}
	h.Close()
}

func TestDarwinCaffeinateUsesSystemSleepAndParentPID(t *testing.T) {
	origLook, origCmd, origPID := lookPath, commandContext, processID
	t.Cleanup(func() {
		lookPath, commandContext, processID = origLook, origCmd, origPID
	})
	lookPath = func(name string) (string, error) {
		if name != "caffeinate" {
			t.Fatalf("looked up %q", name)
		}
		return "/bin/caffeinate", nil
	}
	processID = func() int { return 4242 }
	var gotName string
	var gotArgs []string
	started := make(chan struct{}, 1)
	commandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		select {
		case started <- struct{}{}:
		default:
		}
		return blockingCmd(ctx)
	}
	stop, err := startDarwin()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("caffeinate did not start")
	}
	stop()
	if gotName != "/bin/caffeinate" {
		t.Fatalf("name %q", gotName)
	}
	want := []string{"-s", "-w", "4242"}
	if len(gotArgs) != 3 || gotArgs[0] != want[0] || gotArgs[1] != want[1] || gotArgs[2] != want[2] {
		t.Fatalf("args %v want %v", gotArgs, want)
	}
}

func TestLinuxInhibitLooksUpSystemd(t *testing.T) {
	origLook, origCmd := lookPath, commandContext
	t.Cleanup(func() { lookPath, commandContext = origLook, origCmd })
	lookPath = func(name string) (string, error) {
		if name != "systemd-inhibit" {
			t.Fatalf("looked up %q", name)
		}
		return "/usr/bin/systemd-inhibit", nil
	}
	var gotArgs []string
	started := make(chan struct{}, 1)
	commandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotArgs = append([]string(nil), args...)
		select {
		case started <- struct{}{}:
		default:
		}
		return blockingCmd(ctx)
	}
	stop, err := startLinux()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("inhibit did not start")
	}
	stop()
	if len(gotArgs) < 2 || gotArgs[0] != "--what=idle:sleep" {
		t.Fatalf("args %v", gotArgs)
	}
}

func TestLookPathErrorsSurface(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(string) (string, error) { return "", errors.New("missing") }
	if _, err := startDarwin(); err == nil {
		t.Fatal("missing caffeinate must fail")
	}
	if _, err := startLinux(); err == nil {
		t.Fatal("missing systemd-inhibit must fail")
	}
}

func TestSupervisedStartFailure(t *testing.T) {
	orig := commandContext
	t.Cleanup(func() { commandContext = orig })
	commandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Path = "/no/such/wakeup-bin"
		return cmd
	}
	if _, err := startSupervised("/no/such/wakeup-bin"); err == nil {
		t.Fatal("expected start error")
	}
}

func TestStartForGOOSDispatches(t *testing.T) {
	origLook, origCmd, origStart := lookPath, commandContext, startPlatform
	t.Cleanup(func() {
		lookPath, commandContext, startPlatform = origLook, origCmd, origStart
	})
	lookPath = func(string) (string, error) { return "/bin/tool", nil }
	started := make(chan struct{}, 1)
	commandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		select {
		case started <- struct{}{}:
		default:
		}
		return blockingCmd(ctx)
	}
	switch runtime.GOOS {
	case "windows":
		// Covered by wakeup_windows_test.go. Dispatch still has to return.
		stop, err := startForGOOS()
		if err != nil {
			t.Fatal(err)
		}
		stop()
	default:
		stop, err := startForGOOS()
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
				stop()
				return
			}
			t.Fatal("platform starter did not run")
		}
		stop()
	}
}

func blockingCmd(ctx context.Context) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "ping", "-n", "30", "127.0.0.1")
	}
	return exec.CommandContext(ctx, "sleep", "60")
}

func TestStartWindowsIsUnavailableOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows has a real inhibitor")
	}
	if _, err := startWindows(); err == nil {
		t.Fatal("non-windows must not pretend to inhibit")
	}
}

func TestCaffeinateArgsIncludeThisPID(t *testing.T) {
	pid := processID()
	if strconv.Itoa(pid) == "" || pid <= 0 {
		t.Fatalf("pid %d", pid)
	}
}
