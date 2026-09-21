package store

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTouchRemoteDeviceKeepsLabelAcrossQuietReconnects(t *testing.T) {
	s := open(t)
	if err := s.TouchRemoteDevice("", "ignored"); err != nil {
		t.Fatalf("empty fp: %v", err)
	}
	if _, err := s.RemoteDevice(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty lookup: %v", err)
	}
	if err := s.TouchRemoteDevice("aa11bb22cc33dd44", "Phone 1.0 Device"); err != nil {
		t.Fatal(err)
	}
	got, err := s.RemoteDevice("aa11bb22cc33dd44")
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "Phone 1.0 Device" {
		t.Fatalf("label=%q", got.Label)
	}
	first := got.SeenAt
	time.Sleep(5 * time.Millisecond)
	// Reconnect often arrives before hello. Wiping the label would
	// flash the fingerprint in Settings for a second.
	if err := s.TouchRemoteDevice("aa11bb22cc33dd44", ""); err != nil {
		t.Fatal(err)
	}
	got, err = s.RemoteDevice("aa11bb22cc33dd44")
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "Phone 1.0 Device" {
		t.Fatalf("quiet reconnect wiped the label: %q", got.Label)
	}
	if !got.SeenAt.After(first) {
		t.Fatalf("seen_at did not move: %v → %v", first, got.SeenAt)
	}
	if err := s.TouchRemoteDevice("aa11bb22cc33dd44", "Phone 2.0 Device"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.RemoteDevice("aa11bb22cc33dd44")
	if got.Label != "Phone 2.0 Device" {
		t.Fatalf("replace=%q", got.Label)
	}
	list, err := s.ListRemoteDevices()
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %+v", err, list)
	}
	if _, err := s.RemoteDevice("missingfp00000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestClipDeviceLabelStripsControlsAndCapsLength(t *testing.T) {
	if got := ClipDeviceLabel("  Phone\n1.0\tDevice  "); got != "Phone 1.0 Device" {
		t.Fatalf("collapse=%q", got)
	}
	if got := ClipDeviceLabel("a\x00b\nc"); got != "a b c" {
		t.Fatalf("controls=%q", got)
	}
	long := strings.Repeat("x", DeviceLabelMax+20)
	if got := ClipDeviceLabel(long); utf8.RuneCountInString(got) != DeviceLabelMax {
		t.Fatalf("cap=%d", utf8.RuneCountInString(got))
	}
	if ClipDeviceLabel("   ") != "" {
		t.Fatal("blank")
	}
}

func TestNilStoreRemoteDeviceIsANoop(t *testing.T) {
	var s *Store
	if err := s.TouchRemoteDevice("fp", "x"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListRemoteDevices()
	if err != nil || rows != nil {
		t.Fatalf("list %v %+v", err, rows)
	}
	if _, err := s.RemoteDevice("fp"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lookup: %v", err)
	}
}
