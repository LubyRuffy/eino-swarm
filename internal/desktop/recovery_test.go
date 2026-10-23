package desktop

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestUnresponsiveRendererReloadsThenUsesOnlyItsBoundedSuccessorBudget(t *testing.T) {
	now := time.Unix(100, 0)
	s := newRecoveryState(now, 1)
	s.healthy(now)
	var actions []recoveryAction
	for i := 1; i <= 60; i++ {
		a := s.tick(now.Add(time.Duration(i) * time.Second))
		if a == recoveryReload || a == recoveryReplace || a == recoveryExhausted {
			actions = append(actions, a)
		}
	}
	if len(actions) != 2 || actions[0] != recoveryReload || actions[1] != recoveryReplace || s.attempts != 2 {
		t.Fatalf("missing bounded reload/replacement: actions=%v state=%+v", actions, s)
	}
	s.replacementFailed(now.Add(time.Minute))
	if got := s.tick(now.Add(time.Minute + recoveryRetryDelay)); got != recoveryExhausted {
		t.Fatalf("replacement failure restarted an exhausted lineage: %v", got)
	}
	for i := 0; i < 10; i++ {
		if got := s.tick(now.Add(2 * time.Minute)); got != recoveryNoAction {
			t.Fatalf("terminal failure repeated recovery: %v", got)
		}
	}
	s.healthy(now.Add(time.Hour))
	s.setActive(false, now.Add(2*time.Hour))
	s.setActive(true, now.Add(3*time.Hour))
	if s.stage != recoveryExhausted || s.tick(now.Add(4*time.Hour)) != recoveryNoAction {
		t.Fatal("late successful pixels or focus resurrected terminal recovery")
	}
}

func TestBackgroundAndSleepingDesktopGetsANewForegroundGrace(t *testing.T) {
	now := time.Unix(100, 0)
	s := newRecoveryState(now, 0)
	s.healthy(now)
	s.setActive(false, now)
	if got := s.tick(now.Add(time.Hour)); got != recoveryNoAction {
		t.Fatalf("background desktop recovered: %v", got)
	}
	s.setActive(true, now.Add(time.Hour))
	for i := 0; i < int(recoveryHealthTimeout/time.Second); i++ {
		got := s.tick(now.Add(time.Hour + time.Duration(i)*time.Second))
		if got == recoveryReload || got == recoveryReplace {
			t.Fatalf("foreground grace was skipped at %ds: %v", i, got)
		}
	}
	s.setActive(false, now.Add(2*time.Hour))
	s.setActive(true, now.Add(3*time.Hour))
	s.healthy(now.Add(3*time.Hour + time.Second))
	if s.attempts != 0 || s.reloadUsed {
		t.Fatal("sleep or foregrounding consumed recovery budget")
	}
}

func TestRendererBudgetOnlyResetsAfterContinuousVerifiedPixels(t *testing.T) {
	now := time.Unix(100, 0)
	s := newRecoveryState(now, 2)
	s.reloadUsed = true
	s.healthy(now)
	s.healthy(now.Add(recoveryStablePeriod - time.Second))
	if s.attempts != 2 || !s.reloadUsed {
		t.Fatal("one early health receipt erased the inherited budget")
	}
	s.healthy(now.Add(recoveryStablePeriod))
	if s.attempts != 0 || s.reloadUsed {
		t.Fatal("stable verified renderer never regained recovery budget")
	}
	s.attempts = 2
	s.reloadUsed = true
	s.stage = recoveryReload
	s.setActive(false, now.Add(time.Hour))
	s.setActive(true, now.Add(2*time.Hour))
	s.healthy(now.Add(2*time.Hour + time.Second))
	if s.attempts != 2 || !s.reloadUsed {
		t.Fatal("sleep reset the inherited recovery budget")
	}
}

func TestOnlyCurrentMainFrameChallengeCanReportRendererHealth(t *testing.T) {
	const expected = "http://127.0.0.1:1234/?shell=desktop"
	message := `{"type":"zwai:recovery","session":"session","generation":2,"challenge":"current"}`
	valid := &application.OriginInfo{Origin: expected, IsMainFrame: true}
	for _, tc := range []struct {
		name, message string
		origin        *application.OriginInfo
		want          bool
	}{
		{"current", message, valid, true},
		{"nil origin", message, nil, false},
		{"other origin", message, &application.OriginInfo{Origin: "http://127.0.0.1:5678", IsMainFrame: true}, false},
		{"subframe", message, &application.OriginInfo{Origin: expected}, false},
		{"other top origin", message, &application.OriginInfo{Origin: expected, TopOrigin: "https://example.invalid", IsMainFrame: true}, false},
		{"old challenge", strings.ReplaceAll(message, "current", "old"), valid, false},
		{"old generation", strings.ReplaceAll(message, `:2`, `:1`), valid, false},
		{"other session", strings.ReplaceAll(message, "session\"", "other\""), valid, false},
		{"bad JSON", "{", valid, false},
		{"oversized", strings.Repeat(" ", recoveryMessageLimit+1), valid, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validRecoveryMessage(expected, "session", 2, "current", tc.message, tc.origin); got != tc.want {
				t.Fatalf("health receipt accepted=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestSnapshotMustContainBothCurrentChallengeColours(t *testing.T) {
	first := color.NRGBA{R: 40, G: 100, B: 190, A: 255}
	second := color.NRGBA{R: 215, G: 155, B: 65, A: 255}
	const challenge = "a53c9e1172d4b608"
	pixels := make([]byte, recoverySnapshotSide*recoverySnapshotSide*4)
	if snapshotMatchesProbe(pixels, first, second, challenge) {
		t.Fatal("a blank callback passed rendering acceptance")
	}
	pixels = probePixels(challenge, first, first, 0, 0, false)
	if snapshotMatchesProbe(pixels, first, second, challenge) {
		t.Fatal("a partial/stale probe passed rendering acceptance")
	}
	pixels = probePixels(challenge, first, second, 0, 0, false)
	if !snapshotMatchesProbe(pixels, first, second, challenge) {
		t.Fatal("verified challenge pixels were rejected")
	}
	if snapshotMatchesProbe(pixels, color.NRGBA{R: 99, G: 88, B: 77, A: 255}, second, challenge) || snapshotMatchesProbe(nil, first, second, challenge) {
		t.Fatal("wrong challenge or missing pixels passed rendering acceptance")
	}
	if script := recoveryProbeScript("http://127.0.0.1:1234", "session", 2, challenge, first, second); !strings.Contains(script, "requestAnimationFrame") || !strings.Contains(script, "location.origin") {
		t.Fatal("probe lost its asynchronous paint or receiving-origin guard")
	}
}
