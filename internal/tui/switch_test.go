package tui

import (
	"strings"
	"testing"
)

func TestCycleNextWrapsAndSkipsUnknown(t *testing.T) {
	items := []string{"alpha", "beta", "gamma"}
	if got := cycleNext("alpha", items); got != "beta" {
		t.Fatalf("cycle=%q", got)
	}
	if got := cycleNext("gamma", items); got != "alpha" {
		t.Fatalf("wrap=%q", got)
	}
	if got := cycleNext("missing", items); got != "alpha" {
		t.Fatalf("unknown current should land on the first name, got %q", got)
	}
	if got := cycleNext("x", nil); got != "x" {
		t.Fatalf("empty list=%q", got)
	}
}

func TestReasoningCycleLeadsWithTheEmptyDefault(t *testing.T) {
	got := reasoningCycle([]string{"low", "medium", "high"})
	if len(got) != 4 || got[0] != "" || got[3] != "high" {
		t.Fatalf("cycle=%q", got)
	}
	if reasoningLabel("") != "default" || reasoningLabel("high") != "high" {
		t.Fatal("empty effort must display as default, not as a blank")
	}
}

func TestPickCatalogNameIsExactThenFold(t *testing.T) {
	cat := []string{"Alpha", "beta"}
	got, ok := pickCatalogName("Alpha", cat)
	if !ok || got != "Alpha" {
		t.Fatalf("exact=%q ok=%v", got, ok)
	}
	got, ok = pickCatalogName("BETA", cat)
	if !ok || got != "beta" {
		t.Fatalf("fold=%q ok=%v", got, ok)
	}
	if _, ok = pickCatalogName("missing", cat); ok {
		t.Fatal("unknown names must not silently switch")
	}
}

func TestParseReasoningArgAcceptsDefaultAndKnownLevels(t *testing.T) {
	levels := reasoningCycle([]string{"low", "medium", "high"})
	for in, want := range map[string]string{
		"":        " ", // sentinel: ok empty
		"default": "",
		"HIGH":    "high",
		"medium":  "medium",
	} {
		if in == "" {
			got, ok := parseReasoningArg("", levels)
			if !ok || got != "" {
				t.Fatalf("empty arg=%q ok=%v", got, ok)
			}
			continue
		}
		got, ok := parseReasoningArg(in, levels)
		if !ok || got != want {
			t.Fatalf("parseReasoningArg(%q)=%q ok=%v want %q", in, got, ok, want)
		}
	}
	if _, ok := parseReasoningArg("bogus", levels); ok {
		t.Fatal("unknown levels must not collapse to default")
	}
}

func TestParseTUICommandOnlyInterceptsModelAndReason(t *testing.T) {
	name, arg, ok := parseTUICommand("  /model  beta  ")
	if !ok || name != "model" || arg != "beta" {
		t.Fatalf("got %q %q ok=%v", name, arg, ok)
	}
	name, arg, ok = parseTUICommand("/reason high")
	if !ok || name != "reason" || arg != "high" {
		t.Fatalf("reason %q %q", name, arg)
	}
	if _, _, ok = parseTUICommand("look into it"); ok {
		t.Fatal("plain text is not a command")
	}
}

func TestSwitcherCyclesAndRejectsUnknownNames(t *testing.T) {
	s := &Switcher{
		Model:     "alpha",
		Reasoning: "",
		Catalog:   []string{"alpha", "beta"},
		Levels:    reasoningCycle([]string{"low", "medium", "high"}),
	}
	if got, err := s.CycleModel(); err != nil || got != "beta" {
		t.Fatalf("cycle model=%q err=%v", got, err)
	}
	if got := s.CycleReasoning(); got != "low" {
		t.Fatalf("cycle reason=%q", got)
	}
	if err := s.SetModel("missing"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("unknown model err=%v", err)
	}
	if err := s.SetReasoning("bogus"); err == nil {
		t.Fatal("unknown reasoning must fail")
	}
	if err := s.SetReasoning("high"); err != nil || s.CurrentReasoning() != "high" {
		t.Fatalf("set reason: %v %q", err, s.CurrentReasoning())
	}
	line := s.Line()
	if !strings.Contains(line, "beta") || !strings.Contains(line, "high") ||
		!strings.Contains(line, "/model") || !strings.Contains(line, "shift+tab") {
		t.Fatalf("status line=%q", line)
	}
}

func TestSwitcherInstallRebuildsWithTheCurrentChoice(t *testing.T) {
	var model, effort string
	s := &Switcher{
		Model:     "alpha",
		Reasoning: "low",
		Rebuild: func(m, e string) error {
			model, effort = m, e
			return nil
		},
	}
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if model != "alpha" || effort != "low" {
		t.Fatalf("install model=%q effort=%q", model, effort)
	}
}

func TestSwitcherNilAndEmptyCatalogAreSafe(t *testing.T) {
	var s *Switcher
	if s.Line() != "" || s.CurrentModel() != "" || s.CycleReasoning() != "" {
		t.Fatal("a nil switcher must be inert")
	}
	if err := s.SetModel("alpha"); err == nil {
		t.Fatal("nil SetModel")
	}
	if err := s.SetReasoning("high"); err == nil {
		t.Fatal("nil SetReasoning")
	}
	if _, err := s.CycleModel(); err == nil {
		t.Fatal("nil CycleModel")
	}
	empty := &Switcher{}
	if _, err := empty.CycleModel(); err == nil {
		t.Fatal("empty catalog must not pretend to cycle")
	}
	if got := empty.Line(); !strings.Contains(got, "unset") {
		t.Fatalf("empty model line=%q", got)
	}
	if _, ok := pickCatalogName("", []string{"alpha"}); ok {
		t.Fatal("blank names are not in the catalog")
	}
	if got := reasoningCycle([]string{"", "low"}); len(got) != 2 || got[1] != "low" {
		t.Fatalf("duplicate empty level=%q", got)
	}
}
