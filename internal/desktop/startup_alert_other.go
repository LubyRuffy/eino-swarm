//go:build !darwin && !windows

package desktop

import "os/exec"

func presentStartupAlert(title, message string, failure bool) {
	kind := "--warning"
	if failure {
		kind = "--error"
	}
	if _, err := exec.LookPath("zenity"); err == nil {
		_ = exec.Command("zenity", kind, "--title", title, "--text", message).Run()
	}
}
