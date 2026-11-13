package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestDesktopStartupExplainsFailureAndAClearedSetting(t *testing.T) {
	var gotTitle, gotMessage string
	var failure bool
	var calls int
	previous := showStartupAlert
	showStartupAlert = func(title, message string, failed bool) {
		calls++
		gotTitle, gotMessage, failure = title, message, failed
	}
	t.Cleanup(func() { showStartupAlert = previous })

	reportStartup(nil, nil, "zh")
	if calls != 0 {
		t.Fatal("a clean start opened a dialog")
	}
	reportStartup(errors.New("config: parse config.yaml: bad"), []config.ConfigRepair{{
		Setting: "remote.auth_mode", Removed: "legacy", Current: config.RemoteAuthAccount,
	}}, "zh")
	if calls != 1 || !failure || !strings.Contains(gotMessage, "parse config.yaml") || !strings.Contains(gotMessage, "窗口没有打开") || !strings.Contains(gotMessage, "legacy") {
		t.Fatalf("failure was not shown: failure=%v title=%s message=%s", failure, gotTitle, gotMessage)
	}
	reportStartup(nil, []config.ConfigRepair{{
		Setting: "remote.auth_mode", Removed: "legacy", Current: config.RemoteAuthAccount,
	}}, "zh")
	if calls != 2 || failure || !strings.Contains(gotMessage, "legacy") || !strings.Contains(gotMessage, "账号登录") {
		t.Fatalf("cleared setting was not shown: failure=%v title=%s message=%s", failure, gotTitle, gotMessage)
	}
}
