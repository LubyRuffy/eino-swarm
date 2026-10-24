package remote

import (
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"testing"
)

func TestPhoneRecoveryMetadataAndWorkGate(t *testing.T) {
	e := testEngine(t)
	e.BeginRestore()
	e.SetRestoreProgress(3, 8)
	meta := Handle(e, config.RemoteConfig{}, Request{Op: OpMeta}, "relay", "session")
	if !meta.OK || meta.Startup != "restoring" || meta.Restore == nil || meta.Restore.Completed != 3 || meta.Restore.Total != 8 {
		t.Fatalf("bad metadata: %+v", meta)
	}
	for _, op := range []string{OpStart, OpSend, OpSteer, OpPreempt, OpAnswer, OpRunNow, OpCancelWait, OpResumeGoal, OpFollowupSteer, OpTune, OpSetTimeouts} {
		got := Handle(e, config.RemoteConfig{}, Request{Op: op}, "relay", "session")
		if got.OK || got.Code != "restoring" {
			t.Fatalf("%s bypassed restore: %+v", op, got)
		}
	}
	if got := Handle(e, config.RemoteConfig{}, Request{Op: OpList}, "relay", "session"); !got.OK {
		t.Fatalf("history blocked: %+v", got)
	}
	e.FinishRestore()
	meta = Handle(e, config.RemoteConfig{}, Request{Op: OpMeta}, "relay", "session")
	if !meta.OK || meta.Startup != "ready" {
		t.Fatalf("stale metadata: %+v", meta)
	}
	if got := Handle(e, config.RemoteConfig{}, Request{Op: OpSteer}, "relay", "session"); got.Code == "restoring" {
		t.Fatalf("ready still blocked: %+v", got)
	}
}
