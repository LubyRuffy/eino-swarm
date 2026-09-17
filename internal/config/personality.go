package config

// PersonalityConfig is the install-wide description of how the human wants
// the manager to work with them: tone, language habits, standing preferences.
// It is not a task and not a project's business context. When the two
// conflict, the project's instruction wins.
type PersonalityConfig struct {
	Instructions string `yaml:"instructions" json:"instructions"`
}
