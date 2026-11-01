package config

import (
	"errors"
	"strings"
)

const (
	// WebExecutionAuto uses the signed-in account, otherwise this device.
	WebExecutionAuto = "auto"
	// WebExecutionLocal keeps both Web tools on this device after sign-in.
	WebExecutionLocal = "local"
	// WebExecutionCloud requires an authorized server account.
	WebExecutionCloud = "cloud"
)

func (c *Config) validateWebExecution() error {
	mode, err := ParseWebExecution(c.Tools.WebExecution)
	if err != nil {
		return err
	}
	if mode == "" {
		mode = WebExecutionAuto
	}
	c.Tools.WebExecution = mode
	return nil
}

// ParseWebExecution validates an execution preference. Empty means inherit the
// PC setting on a conversation; the global configuration repairs it to auto.
func ParseWebExecution(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", WebExecutionAuto, WebExecutionLocal, WebExecutionCloud:
		return mode, nil
	default:
		return "", errors.New("web_execution must be auto, local, or cloud")
	}
}
