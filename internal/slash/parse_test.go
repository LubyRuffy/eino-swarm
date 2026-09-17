package slash

import "testing"

func TestSplitCutsTheNameAtTheFirstNonIdentifier(t *testing.T) {
	name, arg, ok := Split("/goal keep going")
	if !ok || name != "goal" || arg != "keep going" {
		t.Fatalf("space split: %q %q ok=%v", name, arg, ok)
	}
	name, arg, ok = Split("  /GOAL Keep going  ")
	if !ok || name != "goal" || arg != "Keep going" {
		t.Fatalf("trim+case: %q %q ok=%v", name, arg, ok)
	}
	name, arg, ok = Split("/goal持续推进")
	if !ok || name != "goal" || arg != "持续推进" {
		t.Fatalf("Codex whitespace-split would eat this as one name, got %q %q ok=%v", name, arg, ok)
	}
	name, arg, ok = Split("／goal keep going")
	if !ok || name != "goal" || arg != "keep going" {
		t.Fatalf("fullwidth slash: %q %q ok=%v", name, arg, ok)
	}
	if _, _, ok = Split("/goals keep going"); !ok {
		t.Fatal("/goals still has a name; Lookup must reject it")
	}
	if _, _, ok = Split("goal"); ok {
		t.Fatal("no slash is not a command")
	}
	if _, _, ok = Split(" /goal"); !ok {
		t.Fatal("submit trims, so a leading space still parses")
	}
	if _, ok := Leading(" /goal"); ok {
		t.Fatal("a draft with a leading space is not a slash menu")
	}
}

func TestLookupMatchesKnownNamesOnly(t *testing.T) {
	arg, ok := Lookup("/goal keep going", "goal", "compact")
	if !ok || arg != "keep going" {
		t.Fatalf("got %q ok=%v", arg, ok)
	}
	if _, ok = Lookup("/goals keep going", "goal"); ok {
		t.Fatal("/goals is not /goal")
	}
	if _, ok = Lookup("/nope", "goal"); ok {
		t.Fatal("unknown names are not commands")
	}
	arg, ok = Lookup("/goal", "goal")
	if !ok || arg != "" {
		t.Fatalf("bare /goal: %q ok=%v", arg, ok)
	}
}
