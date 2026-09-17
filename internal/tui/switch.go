package tui

import (
	"fmt"
	"strings"
	"sync"
)

// Switcher is the TUI's live model and thinking-level choice. The composer
// mutates it; pumpSession installs it onto the registry at the start of each
// turn so a swap cannot race an in-flight ModelBuilder call.
type Switcher struct {
	mu        sync.Mutex
	Model     string
	Reasoning string
	Catalog   []string
	Levels    []string
	Rebuild   func(model, effort string) error
}

func reasoningCycle(levels []string) []string {
	out := make([]string, 0, len(levels)+1)
	out = append(out, "")
	for _, l := range levels {
		if l == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}

func cycleNext(current string, items []string) string {
	if len(items) == 0 {
		return current
	}
	for i, v := range items {
		if v == current {
			return items[(i+1)%len(items)]
		}
	}
	return items[0]
}

func pickCatalogName(name string, catalog []string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	for _, m := range catalog {
		if m == name {
			return m, true
		}
	}
	for _, m := range catalog {
		if strings.EqualFold(m, name) {
			return m, true
		}
	}
	return "", false
}

func parseReasoningArg(arg string, levels []string) (string, bool) {
	arg = strings.ToLower(strings.TrimSpace(arg))
	if arg == "" || arg == "default" {
		return "", true
	}
	for _, l := range levels {
		if l != "" && strings.EqualFold(l, arg) {
			return l, true
		}
	}
	return "", false
}

func reasoningLabel(effort string) string {
	if strings.TrimSpace(effort) == "" {
		return "default"
	}
	return effort
}

func (s *Switcher) snapshot() (model, effort string) {
	if s == nil {
		return "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Model, s.Reasoning
}

// Line is the Codex-style status under the composer.
func (s *Switcher) Line() string {
	if s == nil {
		return ""
	}
	model, effort := s.snapshot()
	if strings.TrimSpace(model) == "" {
		model = "unset"
	}
	return model + " · " + reasoningLabel(effort) + " · shift+tab reason · /model"
}

func (s *Switcher) CurrentModel() string {
	m, _ := s.snapshot()
	return m
}

func (s *Switcher) CurrentReasoning() string {
	_, e := s.snapshot()
	return e
}

func (s *Switcher) SetModel(name string) error {
	if s == nil {
		return fmt.Errorf("no model switcher")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := pickCatalogName(name, s.Catalog)
	if !ok {
		return fmt.Errorf("unknown model %q", strings.TrimSpace(name))
	}
	s.Model = got
	return nil
}

func (s *Switcher) SetReasoning(arg string) error {
	if s == nil {
		return fmt.Errorf("no model switcher")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := parseReasoningArg(arg, s.Levels)
	if !ok {
		return fmt.Errorf("unknown reasoning level %q", strings.TrimSpace(arg))
	}
	s.Reasoning = got
	return nil
}

func (s *Switcher) CycleModel() (string, error) {
	if s == nil {
		return "", fmt.Errorf("no model switcher")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Catalog) == 0 {
		return "", fmt.Errorf("no models in the catalog")
	}
	s.Model = cycleNext(s.Model, s.Catalog)
	return s.Model, nil
}

func (s *Switcher) CycleReasoning() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Reasoning = cycleNext(s.Reasoning, s.Levels)
	return s.Reasoning
}

// Install writes the current choice onto the swarm. Called from the session
// loop, not from the UI thread, so ModelBuilder is not swapped mid-Generate.
func (s *Switcher) Install() error {
	if s == nil || s.Rebuild == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Rebuild(s.Model, s.Reasoning)
}
