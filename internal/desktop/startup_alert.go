package desktop

import (
	"os"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// PresentStartupAlert shows a system dialog in front of other windows.
// A unit test must not open one; the text helpers cover what it would say.
func PresentStartupAlert(title, message string, failure bool) {
	if testing.Testing() {
		return
	}
	presentStartupAlert(title, message, failure)
}

// StartupFailureText is the dialog shown when the desktop cannot open.
func StartupFailureText(err error, locale string) (string, string) {
	detail := ""
	if err != nil {
		detail = clipAlert(err.Error())
	}
	if startupAlertLanguage(locale) == config.LocaleZh {
		return "zwai 无法启动", "启动没有完成，窗口没有打开。\n\n" + detail
	}
	return "zwai could not start", "Startup did not finish, so the window did not open.\n\n" + detail
}

// StartupRepairText is the dialog shown after unrecognized settings were
// cleared and the rest of the configuration was loaded again.
func StartupRepairText(repairs []config.ConfigRepair, locale string) (string, string) {
	zh := startupAlertLanguage(locale) == config.LocaleZh
	var lines []string
	for _, repair := range repairs {
		label := repairLabel(repair.Setting, zh)
		removed := repair.Removed
		if removed == "" {
			removed = "—"
		}
		if zh {
			lines = append(lines, "• "+label+"：已清除「"+removed+"」，现在是"+repairCurrent(repair, true)+"。")
			continue
		}
		lines = append(lines, "• "+label+": cleared \""+removed+"\"; now "+repairCurrent(repair, false)+".")
	}
	body := strings.Join(lines, "\n")
	if zh {
		return "已重置无法识别的设置", "这些设置的取值无法识别，已经清除并按默认重新加载。其它配置没有改动。\n\n" + body
	}
	return "Unrecognized settings were reset", "These settings had values this version does not understand. They were cleared and loaded again at their defaults. Everything else was kept.\n\n" + body
}

func startupAlertLanguage(locale string) string {
	switch config.NormalizeLocale(locale) {
	case config.LocaleZh:
		return config.LocaleZh
	case config.LocaleEn:
		return config.LocaleEn
	default:
		lang := strings.ToLower(os.Getenv("LC_ALL") + " " + os.Getenv("LANG"))
		if strings.Contains(lang, "zh") {
			return config.LocaleZh
		}
		return config.LocaleEn
	}
}

func repairLabel(setting string, zh bool) string {
	switch setting {
	case "remote.auth_mode":
		if zh {
			return "远程登录方式"
		}
		return "Remote sign-in"
	case "product.mode":
		if zh {
			return "安装模式"
		}
		return "Install mode"
	case "tools.web_execution":
		if zh {
			return "网页工具执行位置"
		}
		return "Web tool location"
	case "models.default_source":
		if zh {
			return "默认模型来源"
		}
		return "Default model source"
	case "models.account_presentation":
		if zh {
			return "账号模型展示"
		}
		return "Account model list"
	}
	if id, ok := strings.CutPrefix(setting, "models.providers."); ok {
		if providerID, found := strings.CutSuffix(id, ".api_mode"); found {
			if zh {
				return "模型服务 " + providerID + " 的接口"
			}
			return "Model service " + providerID + " protocol"
		}
		if providerID, found := strings.CutSuffix(id, ".builtin_tools"); found {
			if zh {
				return "模型服务 " + providerID + " 的云端工具"
			}
			return "Model service " + providerID + " cloud tool"
		}
	}
	return setting
}

func repairCurrent(repair config.ConfigRepair, zh bool) string {
	if repair.Current == "" {
		if zh {
			return "默认值"
		}
		return "the default"
	}
	if !zh {
		return repair.Current
	}
	switch repair.Setting {
	case "remote.auth_mode":
		switch repair.Current {
		case config.RemoteAuthAccount:
			return "账号登录"
		case config.RemoteAuthIndependent:
			return "独立配对"
		}
	case "product.mode":
		switch repair.Current {
		case config.ProductModeFOFA:
			return "FOFA"
		case config.ProductModeLocal:
			return "本地模型"
		}
	case "tools.web_execution":
		switch repair.Current {
		case config.WebExecutionAuto:
			return "自动"
		case config.WebExecutionLocal:
			return "本机"
		case config.WebExecutionCloud:
			return "账号服务器"
		}
	case "models.default_source":
		switch repair.Current {
		case config.DefaultSourceAccount:
			return "账号模型"
		case config.DefaultSourceProvider:
			return "本机模型服务"
		}
	case "models.account_presentation":
		switch repair.Current {
		case config.AccountPresentationOfficial:
			return "官方列表"
		case config.AccountPresentationFull:
			return "完整列表"
		}
	}
	return repair.Current
}

func clipAlert(raw string) string {
	raw = strings.TrimSpace(raw)
	runes := []rune(raw)
	if len(runes) > 1200 {
		return string(runes[:1200])
	}
	return raw
}
