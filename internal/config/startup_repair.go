package config

import "strings"

// ConfigRepair is one stored value this version does not understand.
// Load clears it, writes the default back, and keeps every other setting.
type ConfigRepair struct {
	Setting string
	Removed string
	Current string
}

// StartupRepairs returns values cleared while loading this file.
func (c *Config) StartupRepairs() []ConfigRepair {
	if c == nil || len(c.repairs) == 0 {
		return nil
	}
	return append([]ConfigRepair(nil), c.repairs...)
}

// repairUnrecognized drops enumerated values this process cannot apply.
// A save still rejects those values, so a caller cannot write one back.
func (c *Config) repairUnrecognized() {
	if !knownRemoteAuth(c.Remote.AuthMode) {
		removed := c.Remote.AuthMode
		c.Remote.AuthMode = ""
		c.noteRepair("remote.auth_mode", removed, RemoteAuthAccount)
	}
	switch strings.ToLower(strings.TrimSpace(c.Product.Mode)) {
	case "", ProductModeFOFA, ProductModeLocal:
	default:
		removed := c.Product.Mode
		c.Product.Mode = ""
		c.noteRepair("product.mode", removed, ProductModeFOFA)
	}
	if _, err := ParseWebExecution(c.Tools.WebExecution); err != nil {
		removed := c.Tools.WebExecution
		c.Tools.WebExecution = ""
		c.noteRepair("tools.web_execution", removed, WebExecutionAuto)
	}
	if _, err := ParseDefaultSource(c.Models.DefaultSource); err != nil {
		removed := c.Models.DefaultSource
		c.Models.DefaultSource = ""
		c.noteRepair("models.default_source", removed, "")
	}
	if _, err := ParseAccountPresentation(c.Models.AccountPresentation); err != nil {
		removed := c.Models.AccountPresentation
		c.Models.AccountPresentation = ""
		c.noteRepair("models.account_presentation", removed, AccountPresentationOfficial)
	}
	for i := range c.Models.Providers {
		provider := &c.Models.Providers[i]
		switch provider.APIMode {
		case "", APIModeChatCompletions, APIModeResponses:
		default:
			removed := provider.APIMode
			provider.APIMode = ""
			c.noteRepair("models.providers."+provider.ID+".api_mode", removed, APIModeChatCompletions)
		}
		removedTools := dropUnrecognizedBuiltinTools(provider)
		for _, kind := range removedTools {
			c.noteRepair("models.providers."+provider.ID+".builtin_tools", kind, "")
		}
	}
}

func knownRemoteAuth(mode string) bool {
	switch mode {
	case "", RemoteAuthIndependent, RemoteAuthAccount:
		return true
	default:
		return false
	}
}

func dropUnrecognizedBuiltinTools(provider *Provider) []string {
	if len(provider.BuiltinTools) == 0 {
		return nil
	}
	kept := make([]map[string]any, 0, len(provider.BuiltinTools))
	var removed []string
	for _, tool := range provider.BuiltinTools {
		kind, _ := tool["type"].(string)
		switch kind {
		case "web_search", "web_search_preview", "file_search", "code_interpreter":
			kept = append(kept, tool)
		default:
			removed = append(removed, kind)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	if len(kept) == 0 {
		provider.BuiltinTools = nil
	} else {
		provider.BuiltinTools = kept
	}
	return removed
}

func (c *Config) refreshRepairCurrents() {
	for i := range c.repairs {
		switch c.repairs[i].Setting {
		case "remote.auth_mode":
			c.repairs[i].Current = c.Remote.AuthMode
		case "product.mode":
			c.repairs[i].Current = c.Product.Resolved()
		case "tools.web_execution":
			c.repairs[i].Current = c.Tools.WebExecution
		case "models.default_source":
			c.repairs[i].Current = c.Models.DefaultSource
		case "models.account_presentation":
			c.repairs[i].Current = c.Models.Presentation()
		default:
			if id, ok := strings.CutPrefix(c.repairs[i].Setting, "models.providers."); ok {
				if providerID, found := strings.CutSuffix(id, ".api_mode"); found {
					if provider, exists := c.Provider(providerID); exists {
						mode := provider.APIMode
						if mode == "" {
							mode = APIModeChatCompletions
						}
						c.repairs[i].Current = mode
					}
				}
			}
		}
	}
}

func (c *Config) noteRepair(setting, removed, current string) {
	c.repairs = append(c.repairs, ConfigRepair{
		Setting: setting,
		Removed: clipRepairValue(removed),
		Current: current,
	})
}

func clipRepairValue(raw string) string {
	raw = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, raw))
	runes := []rune(raw)
	if len(runes) > 80 {
		return string(runes[:80])
	}
	return raw
}
