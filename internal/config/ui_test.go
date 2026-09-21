package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeFontMapsToKnownTypefaces(t *testing.T) {
	cases := map[string]string{
		"system": FontSystem,
		"SERIF":  FontSerif,
		" mono ": FontMono,
		"":       FontSystem,
		"comic":  FontSystem,
	}
	for in, want := range cases {
		if got := NormalizeFont(in); got != want {
			t.Fatalf("NormalizeFont(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestNormalizeFontSizeMapsToKnownSizes(t *testing.T) {
	cases := map[string]string{
		"small":  FontSizeSmall,
		"MEDIUM": FontSizeMedium,
		" large": FontSizeLarge,
		"":       FontSizeMedium,
		"huge":   FontSizeMedium,
	}
	for in, want := range cases {
		if got := NormalizeFontSize(in); got != want {
			t.Fatalf("NormalizeFontSize(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestNormalizeContentWidthMapsToKnownColumns(t *testing.T) {
	cases := map[string]string{
		"comfortable": ContentWidthComfortable,
		"FULL":        ContentWidthFull,
		" full ":      ContentWidthFull,
		"":            ContentWidthComfortable,
		"wide":        ContentWidthComfortable,
	}
	for in, want := range cases {
		if got := NormalizeContentWidth(in); got != want {
			t.Fatalf("NormalizeContentWidth(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestNormalizeTranscriptModeMapsToKnownViews(t *testing.T) {
	cases := map[string]string{
		"user":        TranscriptModeUser,
		"DEVELOPER":   TranscriptModeDeveloper,
		" developer ": TranscriptModeDeveloper,
		"":            TranscriptModeUser,
		"verbose":     TranscriptModeUser,
	}
	for in, want := range cases {
		if got := NormalizeTranscriptMode(in); got != want {
			t.Fatalf("NormalizeTranscriptMode(%q)=%q, want %q", in, got, want)
		}
	}
}

// A language-only PATCH must not reset the typeface; a font-only PATCH must
// not reset the language. That is what keeps the title-bar 中/EN control from
// wiping Settings → General.
func TestMergeUIKeepsBlankFields(t *testing.T) {
	cur := UIConfig{
		Locale:         LocaleZh,
		Font:           FontSerif,
		FontSize:       FontSizeLarge,
		ContentWidth:   ContentWidthFull,
		TranscriptMode: TranscriptModeDeveloper,
	}
	got := MergeUI(cur, UIConfig{Locale: LocaleEn})
	if got.Locale != LocaleEn || got.Font != FontSerif ||
		got.FontSize != FontSizeLarge || got.ContentWidth != ContentWidthFull ||
		got.TranscriptMode != TranscriptModeDeveloper {
		t.Fatalf("language patch wiped chrome: %+v", got)
	}

	got = MergeUI(cur, UIConfig{Font: FontMono, FontSize: FontSizeSmall})
	if got.Locale != LocaleZh || got.Font != FontMono ||
		got.FontSize != FontSizeSmall || got.ContentWidth != ContentWidthFull {
		t.Fatalf("font patch wiped chrome: %+v", got)
	}

	got = MergeUI(cur, UIConfig{Font: "nope", ContentWidth: "wide"})
	if got.Font != FontSystem || got.ContentWidth != ContentWidthComfortable {
		t.Fatalf("junk must become defaults, got %+v", got)
	}

	cur.Palette = PaletteFOFA
	got = MergeUI(cur, UIConfig{Locale: LocaleEn})
	if got.Palette != PaletteFOFA {
		t.Fatalf("language patch wiped palette: %+v", got)
	}
	got = MergeUI(cur, UIConfig{Palette: "codex"})
	if got.Palette != PaletteZWAI {
		t.Fatalf("junk palette must become zwai, got %+v", got)
	}
}

// A config written before these keys existed must come up as the current
// reading column and the UI sans stack, not as an empty font-family.
func TestOlderConfigGetsDefaultChrome(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("ui:\n  locale: zh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.UI.Locale != LocaleZh {
		t.Fatalf("pinned language must survive: %+v", got.UI)
	}
	if got.UI.Font != DefaultFont || got.UI.UIFontSize != DefaultUIFontSize ||
		got.UI.ContentFont != DefaultContentFont || got.UI.FontSize != DefaultFontSize ||
		got.UI.CodeFont != DefaultCodeFont || got.UI.CodeFontSize != DefaultCodeFontSize ||
		got.UI.ContentWidth != DefaultContentWidth ||
		got.UI.TranscriptMode != DefaultTranscriptMode ||
		got.UI.Palette != DefaultPalette {
		t.Fatalf("older config must keep the current column and type: %+v", got.UI)
	}
}

func TestPinnedChromeSurvivesLoad(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("ui:\n  locale: en\n  font: mono\n  font_size: small\n  content_width: full\n  palette: fofa\n")
	if err := os.WriteFile(filepath.Join(dir, FileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.UI.Font != FontMono || got.UI.FontSize != FontSizeSmall ||
		got.UI.ContentWidth != ContentWidthFull || got.UI.Palette != PaletteFOFA {
		t.Fatalf("pinned chrome must survive load: %+v", got.UI)
	}
	if got.UI.ContentFont != DefaultContentFont || got.UI.CodeFont != DefaultCodeFont {
		t.Fatalf("missing faces must default: %+v", got.UI)
	}
}

func TestNormalizeContentAndCodeFaces(t *testing.T) {
	if NormalizeContentFont("") != FontUI || NormalizeContentFont("SERIF") != FontSerif {
		t.Fatal("content font should follow UI unless named")
	}
	if NormalizeCodeFont("") != FontMono || NormalizeCodeFont("ui") != FontUI {
		t.Fatal("code font should stay mono unless named")
	}
	if NormalizeFontSize("ui") != FontSizeUI {
		t.Fatal("conversation size ui follows chrome")
	}
	if NormalizeCodeFontSize("") != FontSizeContent {
		t.Fatal("code size follows the conversation until named")
	}
}

func TestNormalizePaletteMapsToKnownSets(t *testing.T) {
	cases := map[string]string{
		"zwai":   PaletteZWAI,
		"FOFA":   PaletteFOFA,
		" fofa ": PaletteFOFA,
		"":       PaletteZWAI,
		"codex":  PaletteZWAI,
	}
	for in, want := range cases {
		if got := NormalizePalette(in); got != want {
			t.Fatalf("NormalizePalette(%q)=%q, want %q", in, got, want)
		}
	}
}
