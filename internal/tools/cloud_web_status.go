package tools

import (
	"errors"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// WebExecutionStatus describes only this PC's configured route and local account
// receipt. Signed-in does not assert a current server-side Key/tool grant; every
// invocation still checks that authority at the Gateway.
type WebExecutionStatus struct {
	Preference   string `json:"preference"`
	Execution    string `json:"execution"`
	CloudAccount string `json:"cloud_account"`
}

// DescribeWebExecution never returns credentials, account identifiers or server
// addresses, and performs no network requests.
func DescribeWebExecution(cfg *config.Config) WebExecutionStatus {
	snapshot := cfg.Snapshot()
	preference := snapshot.Tools.WebExecution
	if preference == "" {
		preference = config.WebExecutionAuto
	}
	status := WebExecutionStatus{Preference: preference, Execution: preference, CloudAccount: "signed_in"}
	_, _, err := cloudWebAccount(snapshot)
	switch {
	case errors.Is(err, errCloudLoginRequired):
		status.CloudAccount = "sign_in_required"
	case err != nil:
		status.CloudAccount = "unavailable"
	}
	if preference == config.WebExecutionAuto {
		status.Execution = config.WebExecutionCloud
		if errors.Is(err, errCloudLoginRequired) {
			status.Execution = config.WebExecutionLocal
		}
	}
	return status
}
