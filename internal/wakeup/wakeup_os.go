package wakeup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
)

// startPlatform is swapped in tests so we never spawn a real inhibitor
// unless a test asks for the argv contract.
var startPlatform = startForGOOS

var (
	lookPath       = exec.LookPath
	commandContext = exec.CommandContext
	processID      = os.Getpid
)

func startForGOOS() (func(), error) {
	switch runtime.GOOS {
	case "darwin":
		return startDarwin()
	case "linux":
		return startLinux()
	case "windows":
		return startWindows()
	default:
		return func() {}, nil
	}
}

func startDarwin() (func(), error) {
	path, err := lookPath("caffeinate")
	if err != nil {
		return nil, err
	}
	// -s is PreventSystemSleep: the kernel ignores it on battery, which
	// is the "plugged in" half of the setting. -w ties the child to us
	// so a crash cannot leave a stray assertion.
	return startSupervised(path, "-s", "-w", strconv.Itoa(processID()))
}

func startLinux() (func(), error) {
	path, err := lookPath("systemd-inhibit")
	if err != nil {
		return nil, err
	}
	return startSupervised(path,
		"--what=idle:sleep",
		"--who=zwai",
		"--why=phone-remote",
		"--mode=block",
		"sleep", "infinity",
	)
}

func startSupervised(name string, args ...string) (func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		first := true
		for ctx.Err() == nil {
			cmd := commandContext(ctx, name, args...)
			if err := cmd.Start(); err != nil {
				if first {
					started <- err
				}
				return
			}
			if first {
				started <- nil
				first = false
			}
			_ = cmd.Wait()
			// Child died while we still want the assertion. Restart
			// immediately — a pause here is how idle sleep wins.
		}
	}()
	if err := <-started; err != nil {
		cancel()
		wg.Wait()
		return nil, fmt.Errorf("wakeup: start %s: %w", name, err)
	}
	return func() {
		cancel()
		wg.Wait()
	}, nil
}
