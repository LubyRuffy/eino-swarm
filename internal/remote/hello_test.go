package remote

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestHelloStoresThePhoneLabelOnThisFingerprint(t *testing.T) {
	e := testEngine(t)
	pump, log := testPump(t, e)
	pump.deviceFP = "aa11bb22cc33dd44"
	pump.devices = e.Store()

	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "h", Op: OpHello, Text: "Phone 1.0 Device"})
	pump.Dispatch(raw)
	got := log.waitID(t, "h", 2*time.Second)
	if !got.OK {
		t.Fatalf("%+v", got)
	}
	row, err := e.Store().RemoteDevice("aa11bb22cc33dd44")
	if err != nil {
		t.Fatal(err)
	}
	if row.Label != "Phone 1.0 Device" {
		t.Fatalf("label=%q", row.Label)
	}

	// A later empty hello is a keepalive, not a wipe.
	raw, _ = json.Marshal(Request{V: ProtocolV, ID: "h2", Op: OpHello})
	pump.Dispatch(raw)
	_ = log.waitID(t, "h2", 2*time.Second)
	row, _ = e.Store().RemoteDevice("aa11bb22cc33dd44")
	if row.Label != "Phone 1.0 Device" {
		t.Fatalf("empty hello wiped: %q", row.Label)
	}
}

func TestHelloWithoutAFingerprintStillAcks(t *testing.T) {
	e := testEngine(t)
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "h", Op: OpHello, Text: "Phone 1.0 Device"})
	pump.Dispatch(raw)
	got := log.waitID(t, "h", 2*time.Second)
	if !got.OK {
		t.Fatalf("%+v", got)
	}
	rows, err := e.Store().ListRemoteDevices()
	if err != nil || len(rows) != 0 {
		t.Fatalf("no fp must not invent a row: %v %+v", err, rows)
	}
}

func TestHelloIsLinkScopedNotAStatelessOp(t *testing.T) {
	e := testEngine(t)
	resp := Handle(e, e.Config().Remote, Request{ID: "h", Op: OpHello, Text: "Phone 1.0 Device"}, "relay", "s")
	if resp.OK || resp.Code != "unknown_op" {
		t.Fatalf("hello without the link fingerprint: %+v", resp)
	}
}

func TestHelloClipsAHostileLabel(t *testing.T) {
	e := testEngine(t)
	pump, _ := testPump(t, e)
	pump.deviceFP = "aa11bb22cc33dd44"
	pump.devices = e.Store()
	long := "Phone 1.0 Device " + string(bytes.Repeat([]byte("x"), store.DeviceLabelMax))
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "h", Op: OpHello, Text: long})
	pump.Dispatch(raw)
	row, err := e.Store().RemoteDevice("aa11bb22cc33dd44")
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(row.Label); n != store.DeviceLabelMax {
		t.Fatalf("clip runes=%d %q", n, row.Label)
	}
}
