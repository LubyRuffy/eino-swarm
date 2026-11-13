package main

import (
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/desktop"
)

// showStartupAlert is the system dialog. Tests replace it so a failure does
// not open a window on the machine running the suite.
var showStartupAlert = desktop.PresentStartupAlert

// reportStartup tells the person who launched the app what happened before
// any window exists. A terminal still receives the same error afterwards.
func reportStartup(err error, repairs []config.ConfigRepair, locale string) {
	if err != nil {
		title, message := desktop.StartupFailureText(err, locale)
		if len(repairs) > 0 {
			_, cleared := desktop.StartupRepairText(repairs, locale)
			message += "\n\n" + cleared
		}
		showStartupAlert(title, message, true)
		return
	}
	if len(repairs) == 0 {
		return
	}
	title, message := desktop.StartupRepairText(repairs, locale)
	showStartupAlert(title, message, false)
}
