package config

// DefaultToolResultMaxChars bounds one workspace tool result in the model's
// context, about ten thousand tokens. A directory listing outside the
// workspace once grew a manager's context from 15K to 232K characters, and
// every later call in that turn paid for it again.
const DefaultToolResultMaxChars = 40_000

// ToolResultLimit is the configured cap, or the default when unset.
func (t ToolsConfig) ToolResultLimit() int {
	if t.ResultMaxChars <= 0 {
		return DefaultToolResultMaxChars
	}
	return t.ResultMaxChars
}
