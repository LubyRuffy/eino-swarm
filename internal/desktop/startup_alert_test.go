package desktop

import (
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestStartupRepairTextNamesTheClearedValueAndTheDefault(t *testing.T) {
	repairs := []config.ConfigRepair{{
		Setting: "remote.auth_mode",
		Removed: "legacy",
		Current: config.RemoteAuthAccount,
	}}
	title, message := StartupRepairText(repairs, "zh")
	if !strings.Contains(title, "无法识别") || !strings.Contains(message, "legacy") || !strings.Contains(message, "账号登录") || !strings.Contains(message, "其它配置没有改动") {
		t.Fatalf("chinese notice did not explain the reset:\n%s\n%s", title, message)
	}
	title, message = StartupRepairText(repairs, "en")
	if !strings.Contains(title, "Unrecognized") || !strings.Contains(message, "legacy") || !strings.Contains(message, config.RemoteAuthAccount) || !strings.Contains(message, "Everything else was kept") {
		t.Fatalf("english notice did not explain the reset:\n%s\n%s", title, message)
	}
}

func TestStartupRepairTextCoversEachResetSetting(t *testing.T) {
	repairs := []config.ConfigRepair{
		{Setting: "remote.auth_mode", Removed: "legacy", Current: config.RemoteAuthIndependent},
		{Setting: "product.mode", Removed: "kiosk", Current: config.ProductModeFOFA},
		{Setting: "product.mode", Removed: "kiosk", Current: config.ProductModeLocal},
		{Setting: "tools.web_execution", Removed: "somewhere", Current: config.WebExecutionAuto},
		{Setting: "tools.web_execution", Removed: "somewhere", Current: config.WebExecutionLocal},
		{Setting: "tools.web_execution", Removed: "somewhere", Current: config.WebExecutionCloud},
		{Setting: "models.default_source", Removed: "neither", Current: config.DefaultSourceAccount},
		{Setting: "models.default_source", Removed: "neither", Current: config.DefaultSourceProvider},
		{Setting: "models.account_presentation", Removed: "poster", Current: config.AccountPresentationOfficial},
		{Setting: "models.account_presentation", Removed: "poster", Current: config.AccountPresentationFull},
		{Setting: "models.providers.old.api_mode", Removed: "soap", Current: config.APIModeChatCompletions},
		{Setting: "models.providers.old.builtin_tools", Removed: "crystal-ball", Current: ""},
		{Setting: "unknown.setting", Removed: "", Current: "kept"},
	}
	_, message := StartupRepairText(repairs, "zh")
	_, english := StartupRepairText(repairs, "en")
	for _, part := range []string{"独立配对", "FOFA", "本地模型", "自动", "本机", "账号服务器", "账号模型", "本机模型服务", "官方列表", "完整列表", "chat_completions", "云端工具", "默认值", "unknown.setting", "kept"} {
		if !strings.Contains(message, part) {
			t.Fatalf("missing %s\n%s", part, message)
		}
	}
	for _, part := range []string{"Remote sign-in", "independent", "Install mode", "Web tool location", "Default model source", "Account model list", "protocol", "cloud tool", "the default", "unknown.setting"} {
		if !strings.Contains(english, part) {
			t.Fatalf("missing %s\n%s", part, english)
		}
	}
	title, english := StartupFailureText(errors.New(strings.Repeat("x", 1300)), "en")
	if !strings.Contains(title, "could not start") || len([]rune(english)) >= 1300 {
		t.Fatalf("english failure was not clipped: %s", title)
	}
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	if title, _ = StartupFailureText(errors.New("disk"), ""); !strings.Contains(title, "无法启动") {
		t.Fatal("system Chinese locale was ignored")
	}
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	if title, _ = StartupFailureText(errors.New("disk"), "system"); !strings.Contains(title, "could not start") {
		t.Fatal("system English locale was ignored")
	}
	PresentStartupAlert("unused", "unused", true)
}

func TestStartupFailureTextIsAForegroundExplanation(t *testing.T) {
	title, message := StartupFailureText(errors.New("remote: invalid authentication mode"), "zh")
	if !strings.Contains(title, "无法启动") || !strings.Contains(message, "窗口没有打开") || !strings.Contains(message, "invalid authentication mode") {
		t.Fatalf("failure notice hid the cause:\n%s\n%s", title, message)
	}
}
