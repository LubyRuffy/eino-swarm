package config

import (
	"net/url"
	"strings"
	"unicode"
)

// AcceptedAvatarURL returns a public image address a client may store and
// request. It follows the Gateway's public avatar rule: an absolute http or
// https URL, without userinfo, a fragment, whitespace, control characters or
// a backslash, at most 2048 bytes. A query string is part of the address.
// Anything else is omitted rather than stored.
func AcceptedAvatarURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 || (!strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://")) || strings.ContainsAny(raw, " \t\r\n\\#") || strings.IndexFunc(raw, unicode.IsControl) >= 0 || strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" {
		return ""
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return ""
	}
	return raw
}
