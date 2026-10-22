package desktop

import (
	"errors"
	"testing"
	"time"
)

func TestFatalBeforeNativeStartupDoesNotOpenAWindow(t *testing.T) {
	want := errors.New("native presence lost")
	fatal := make(chan error, 1)
	fatal <- want
	err := runWithFatal(func() error { t.Fatal("window opened after terminal failure"); return nil }, func(error) { t.Fatal("Quit before app startup") }, make(chan struct{}), fatal)
	if !errors.Is(err, want) {
		t.Fatalf("fatal cause lost: %v", err)
	}
}

func TestNativeFatalWaitsForAppStartupThenQuitsAndReturnsCause(t *testing.T) {
	for _, alreadyStarted := range []bool{false, true} {
		t.Run(map[bool]string{false: "during startup", true: "running"}[alreadyStarted], func(t *testing.T) {
			started := make(chan struct{})
			if alreadyStarted {
				close(started)
			}
			entered := make(chan struct{})
			quit := make(chan struct{})
			fatal := make(chan error)
			done := make(chan error, 1)
			want := errors.New("same-base recovery exhausted")
			go func() {
				done <- runWithFatal(func() error { close(entered); <-quit; return nil }, func(cause error) {
					if !errors.Is(cause, want) {
						t.Errorf("Quit lost the typed terminal cause: %v", cause)
					}
					close(quit)
				}, started, fatal)
			}()
			<-entered
			fatal <- want
			if !alreadyStarted {
				select {
				case <-quit:
					t.Fatal("Wails Quit would be ignored before startup")
				default:
				}
				close(started)
			}
			select {
			case err := <-done:
				if !errors.Is(err, want) {
					t.Fatalf("fatal lost: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("fatal left native app running")
			}
		})
	}
}

func TestNativeStartupFailureDoesNotWaitForAStartedEvent(t *testing.T) {
	entered := make(chan struct{})
	leave := make(chan struct{})
	fatal := make(chan error)
	done := make(chan error, 1)
	want := errors.New("presence recovery failed before app started")
	go func() {
		done <- runWithFatal(func() error { close(entered); <-leave; return errors.New("native startup failed") }, func(error) { t.Error("Quit before app started") }, make(chan struct{}), fatal)
	}()
	<-entered
	fatal <- want
	close(leave)
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("lost pending fatal: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting for startup after Run failed")
	}
}

func TestNativeRunPreservesNormalAndStartupResults(t *testing.T) {
	want := errors.New("native startup error")
	for _, mode := range []string{"nil channel", "closed channel", "open channel", "nil notice"} {
		t.Run(mode, func(t *testing.T) {
			var fatal chan error
			if mode != "nil channel" {
				fatal = make(chan error, 1)
			}
			if mode == "closed channel" {
				close(fatal)
			}
			if mode == "nil notice" {
				fatal <- nil
			}
			err := runWithFatal(func() error { return want }, func(error) { t.Error("normal Run failure triggered Quit") }, make(chan struct{}), fatal)
			if !errors.Is(err, want) {
				t.Fatalf("Run error lost: %v", err)
			}
		})
	}
}

func TestClosingPresenceNormallyWhileAppRunsIsNotFatal(t *testing.T) {
	entered := make(chan struct{})
	leave := make(chan struct{})
	fatal := make(chan error)
	done := make(chan error, 1)
	go func() {
		done <- runWithFatal(func() error { close(entered); <-leave; return nil }, func(error) { t.Error("normal release triggered Quit") }, make(chan struct{}), fatal)
	}()
	<-entered
	close(fatal)
	close(leave)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("normal shutdown watcher leaked")
	}
}

func TestQueuedNativeFatalIsNotLostToConcurrentRunExit(t *testing.T) {
	for i := 0; i < 100; i++ {
		fatal := make(chan error, 1)
		want := errors.New("terminal native cause")
		err := runWithFatal(func() error { fatal <- want; return nil }, func(error) { t.Fatal("app never reached started") }, make(chan struct{}), fatal)
		if !errors.Is(err, want) {
			t.Fatalf("concurrent fatal lost: %v", err)
		}
	}
}

// Quit dispatches synchronously to Wails' native loop. If user shutdown ends
// that loop concurrently, returning the connection cause must not await Quit.
func TestNativeRunExitDoesNotWaitForAnInFlightQuit(t *testing.T) {
	started := make(chan struct{})
	close(started)
	entered := make(chan struct{})
	leave := make(chan struct{})
	quitting := make(chan struct{})
	quitReturned := make(chan struct{})
	fatal := make(chan error)
	done := make(chan error, 1)
	want := errors.New("native recovery exhausted during close")
	go func() {
		done <- runWithFatal(func() error { close(entered); <-leave; return nil }, func(error) { close(quitting); <-quitReturned }, started, fatal)
	}()
	<-entered
	fatal <- want
	<-quitting
	close(leave)
	select {
	case err := <-done:
		close(quitReturned)
		if !errors.Is(err, want) {
			t.Fatalf("lost terminal cause: %v", err)
		}
	case <-time.After(time.Second):
		close(quitReturned)
		<-done
		t.Fatal("native Run already left but an in-flight Quit blocked its error return")
	}
}
