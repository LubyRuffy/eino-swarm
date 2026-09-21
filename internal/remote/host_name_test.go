package remote

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestHelloAndListNameThisComputer(t *testing.T) {
	e := testEngine(t)
	pump, log := testPumpCfg(t, e, config.RemoteConfig{DisplayName: "desk-one"})
	pump.deviceFP = "aa11bb22cc33dd44"
	pump.devices = e.Store()

	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "h", Op: OpHello, Text: "Phone 1.0 Device"})
	pump.Dispatch(raw)
	hello := log.waitID(t, "h", 2*time.Second)
	if !hello.OK || hello.Host != "desk-one" {
		t.Fatalf("hello must name this PC: %+v", hello)
	}

	listed := Handle(e, config.RemoteConfig{DisplayName: "desk-one"}, Request{ID: "l", Op: OpList}, "relay", "s")
	if !listed.OK || listed.Host != "desk-one" {
		t.Fatalf("list must name this PC: %+v", listed)
	}

	blank := Handle(e, config.RemoteConfig{}, Request{ID: "b", Op: OpList}, "relay", "s")
	if blank.Host != "" {
		t.Fatalf("an unnamed host must omit host, got %q", blank.Host)
	}
	for _, leak := range []string{"Office", "MacBook Pro", "iMac", "codex-apps"} {
		if hello.Host == leak || listed.Host == leak {
			t.Fatalf("wire name must not be a sample label %q", leak)
		}
	}
}

func TestWithHostClipsAHostileName(t *testing.T) {
	long := strings.Repeat("n", config.MaxRemoteDisplayName+12)
	got := withHost(okBase("1", "relay", "s"), config.RemoteConfig{DisplayName: long})
	if got.Host != strings.Repeat("n", config.MaxRemoteDisplayName) {
		t.Fatalf("clip %q", got.Host)
	}
	failResp := withHost(fail("1", "relay", "s", "busy", "x"), config.RemoteConfig{DisplayName: "desk-one"})
	if failResp.Host != "" {
		t.Fatalf("errors must not carry host: %+v", failResp)
	}
}
