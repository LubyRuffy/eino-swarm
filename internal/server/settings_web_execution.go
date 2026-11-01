package server

import "github.com/LubyRuffy/eino-swarm/internal/config"

// Older settings clients omit this new preference when saving other tools.
// Missing and null preserve the user's explicit execution location.
type putToolsSettings struct {
	config.ToolsConfig
	WebExecution *string `json:"web_execution"`
}
