package config

import "strings"

// UIConfig is chrome the webview remembers: language, type, size, and how
// wide the conversation column is. Agents still answer in the language the
// human is using.
type UIConfig struct {
	// Locale is system, en or zh. system follows the browser.
	Locale string `yaml:"locale" json:"locale"`
	// Font is system, serif or mono. system is the UI sans stack.
	Font string `yaml:"font" json:"font"`
	// FontSize is small, medium or large. medium matches the CSS root.
	FontSize string `yaml:"font_size" json:"font_size"`
	// ContentWidth is comfortable or full. comfortable is the current
	// reading column; full fills the space between the sidebars.
	ContentWidth string `yaml:"content_width" json:"content_width"`
}

// UI languages. system follows the browser; en and zh pin the chrome.
const (
	LocaleSystem = "system"
	LocaleEn     = "en"
	LocaleZh     = "zh"

	FontSystem = "system"
	FontSerif  = "serif"
	FontMono   = "mono"

	FontSizeSmall  = "small"
	FontSizeMedium = "medium"
	FontSizeLarge  = "large"

	ContentWidthComfortable = "comfortable"
	ContentWidthFull        = "full"

	DefaultLocale       = LocaleSystem
	DefaultFont         = FontSystem
	DefaultFontSize     = FontSizeMedium
	DefaultContentWidth = ContentWidthComfortable
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

// NormalizeFontSize maps any input to a known size. Junk becomes medium, the
// size the CSS root already uses, so a typo is a no-op rather than a shrink.
func NormalizeFontSize(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case FontSizeSmall:
		return FontSizeSmall
	case FontSizeLarge:
		return FontSizeLarge
	default:
		return FontSizeMedium
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

// Normalize repairs every chrome field to a known token.
func (u *UIConfig) Normalize() {
	if u == nil {
		return
	}
	u.Locale = NormalizeLocale(u.Locale)
	u.Font = NormalizeFont(u.Font)
	u.FontSize = NormalizeFontSize(u.FontSize)
	u.ContentWidth = NormalizeContentWidth(u.ContentWidth)
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
	if strings.TrimSpace(patch.FontSize) != "" {
		out.FontSize = patch.FontSize
	}
	if strings.TrimSpace(patch.ContentWidth) != "" {
		out.ContentWidth = patch.ContentWidth
	}
	out.Normalize()
	return out
}
