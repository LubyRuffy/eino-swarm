package config

import "strings"

// UIConfig is chrome the webview remembers: language, type, size, named
// color set, how wide the conversation column is, and how chatty the
// transcript is.
// Agents still answer in the language the human is using.
type UIConfig struct {
	// Locale is system, en or zh. system follows the browser.
	Locale string `yaml:"locale" json:"locale"`
	// Font is the window chrome typeface: system, serif or mono.
	Font string `yaml:"font" json:"font"`
	// UIFontSize is small, medium or large. Sidebar / Settings follow this.
	UIFontSize string `yaml:"ui_font_size" json:"ui_font_size"`
	// ContentFont is ui, system, serif or mono. ui follows Font.
	ContentFont string `yaml:"content_font" json:"content_font"`
	// FontSize is the conversation size: ui, small, medium or large.
	// ui follows UIFontSize so sidebar and body match until someone splits them.
	FontSize string `yaml:"font_size" json:"font_size"`
	// CodeFont is ui, system, serif or mono. Default mono.
	CodeFont string `yaml:"code_font" json:"code_font"`
	// CodeFontSize is ui, content, small, medium or large. content follows FontSize.
	CodeFontSize string `yaml:"code_font_size" json:"code_font_size"`
	// ContentWidth is comfortable or full. comfortable is the current
	// reading column; full fills the space between the sidebars.
	ContentWidth string `yaml:"content_width" json:"content_width"`
	// TranscriptMode is user or developer. user folds thinking and tool
	// calls behind a one-line ticker; developer keeps every row visible.
	TranscriptMode string `yaml:"transcript_mode" json:"transcript_mode"`
	// Palette is the named color set. zwai is the current chrome;
	// fofa is the intelligence-console tokens (both have light and dark).
	Palette string `yaml:"palette" json:"palette"`
}

// UI languages. system follows the browser; en and zh pin the chrome.
const (
	LocaleSystem = "system"
	LocaleEn     = "en"
	LocaleZh     = "zh"

	FontSystem = "system"
	FontSerif  = "serif"
	FontMono   = "mono"
	FontUI     = "ui"

	FontSizeSmall   = "small"
	FontSizeMedium  = "medium"
	FontSizeLarge   = "large"
	FontSizeUI      = "ui"
	FontSizeContent = "content"

	ContentWidthComfortable = "comfortable"
	ContentWidthFull        = "full"

	TranscriptModeUser      = "user"
	TranscriptModeDeveloper = "developer"

	PaletteZWAI = "zwai"
	PaletteFOFA = "fofa"

	DefaultLocale         = LocaleSystem
	DefaultFont           = FontSystem
	DefaultUIFontSize     = FontSizeMedium
	DefaultContentFont    = FontUI
	DefaultFontSize       = FontSizeUI
	DefaultCodeFont       = FontMono
	DefaultCodeFontSize   = FontSizeContent
	DefaultContentWidth   = ContentWidthComfortable
	DefaultTranscriptMode = TranscriptModeUser
	DefaultPalette        = PaletteZWAI
)

// NormalizeLocale maps any input to a known preference. Junk becomes system
// so a typo cannot blank the UI or invent a third language.
func NormalizeLocale(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case LocaleEn:
		return LocaleEn
	case LocaleZh:
		return LocaleZh
	default:
		return LocaleSystem
	}
}

// NormalizeFont maps any input to a known typeface. Junk becomes system so a
// hand-edit cannot leave the window on an empty font-family.
func NormalizeFont(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FontSerif:
		return FontSerif
	case FontMono:
		return FontMono
	default:
		return FontSystem
	}
}

// NormalizeContentFont maps conversation typeface. ui follows the chrome font.
func NormalizeContentFont(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FontSystem:
		return FontSystem
	case FontSerif:
		return FontSerif
	case FontMono:
		return FontMono
	default:
		return FontUI
	}
}

// NormalizeCodeFont maps fenced-code typeface. Blank is mono, not the UI sans.
func NormalizeCodeFont(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FontUI:
		return FontUI
	case FontSystem:
		return FontSystem
	case FontSerif:
		return FontSerif
	default:
		return FontMono
	}
}

// NormalizeUIFontSize maps chrome size. Junk becomes medium.
func NormalizeUIFontSize(s string) string {
	return NormalizeFontSizeToken(s, FontSizeMedium)
}

// NormalizeFontSize maps conversation size. ui follows the chrome size.
func NormalizeFontSize(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FontSizeUI:
		return FontSizeUI
	default:
		return NormalizeFontSizeToken(s, FontSizeMedium)
	}
}

// NormalizeCodeFontSize maps code size. content follows the conversation.
func NormalizeCodeFontSize(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FontSizeUI:
		return FontSizeUI
	case FontSizeSmall, FontSizeMedium, FontSizeLarge:
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return FontSizeContent
	}
}

// NormalizeFontSizeToken maps small/medium/large. Junk becomes fallback.
func NormalizeFontSizeToken(s, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FontSizeSmall:
		return FontSizeSmall
	case FontSizeLarge:
		return FontSizeLarge
	case FontSizeMedium:
		return FontSizeMedium
	default:
		return fallback
	}
}

// NormalizeContentWidth maps any input to a known column. Junk becomes
// comfortable so a typo cannot stretch the transcript to the window edge.
func NormalizeContentWidth(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ContentWidthFull:
		return ContentWidthFull
	default:
		return ContentWidthComfortable
	}
}

// NormalizeTranscriptMode maps any input to a known transcript view. Junk
// becomes user so a typo cannot dump every tool row on a first launch.
func NormalizeTranscriptMode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case TranscriptModeDeveloper:
		return TranscriptModeDeveloper
	default:
		return TranscriptModeUser
	}
}

// NormalizePalette maps any input to a named color set. Junk becomes zwai
// so a hand-edit cannot leave the window on an empty token sheet.
func NormalizePalette(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PaletteFOFA:
		return PaletteFOFA
	default:
		return PaletteZWAI
	}
}

// Normalize repairs every chrome field to a known token.
func (u *UIConfig) Normalize() {
	if u == nil {
		return
	}
	u.Locale = NormalizeLocale(u.Locale)
	u.Font = NormalizeFont(u.Font)
	if strings.TrimSpace(u.UIFontSize) == "" {
		u.UIFontSize = DefaultUIFontSize
	} else {
		u.UIFontSize = NormalizeUIFontSize(u.UIFontSize)
	}
	u.ContentFont = NormalizeContentFont(u.ContentFont)
	if strings.TrimSpace(u.FontSize) == "" {
		u.FontSize = DefaultFontSize
	} else {
		u.FontSize = NormalizeFontSize(u.FontSize)
	}
	u.CodeFont = NormalizeCodeFont(u.CodeFont)
	u.CodeFontSize = NormalizeCodeFontSize(u.CodeFontSize)
	u.ContentWidth = NormalizeContentWidth(u.ContentWidth)
	u.TranscriptMode = NormalizeTranscriptMode(u.TranscriptMode)
	u.Palette = NormalizePalette(u.Palette)
}

// MergeUI keeps current values for any blank field in patch so a
// language-only PUT cannot reset the typeface, and a font-only PUT cannot
// reset the language. Known tokens in patch win; Normalize runs last.
func MergeUI(cur, patch UIConfig) UIConfig {
	out := cur
	if strings.TrimSpace(patch.Locale) != "" {
		out.Locale = patch.Locale
	}
	if strings.TrimSpace(patch.Font) != "" {
		out.Font = patch.Font
	}
	if strings.TrimSpace(patch.UIFontSize) != "" {
		out.UIFontSize = patch.UIFontSize
	}
	if strings.TrimSpace(patch.ContentFont) != "" {
		out.ContentFont = patch.ContentFont
	}
	if strings.TrimSpace(patch.FontSize) != "" {
		out.FontSize = patch.FontSize
	}
	if strings.TrimSpace(patch.CodeFont) != "" {
		out.CodeFont = patch.CodeFont
	}
	if strings.TrimSpace(patch.CodeFontSize) != "" {
		out.CodeFontSize = patch.CodeFontSize
	}
	if strings.TrimSpace(patch.ContentWidth) != "" {
		out.ContentWidth = patch.ContentWidth
	}
	if strings.TrimSpace(patch.TranscriptMode) != "" {
		out.TranscriptMode = patch.TranscriptMode
	}
	if strings.TrimSpace(patch.Palette) != "" {
		out.Palette = patch.Palette
	}
	out.Normalize()
	return out
}
