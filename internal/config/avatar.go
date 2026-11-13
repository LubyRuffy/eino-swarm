package config

import (
	"net/url"
	"strings"
	"unicode"
)

// avatarTrustedRoots are the registrable domains an account picture may come
// from. A subdomain is the same site; any other host is not requested.
var avatarTrustedRoots = []string{"fofa.info", "nosec.org", "baimaohui.net"}

// AcceptedAvatarURL returns a public image address a client may store and
// request. It follows the Gateway's public avatar rule, then keeps only
// fofa.info, nosec.org, baimaohui.net and their subdomains: an absolute http
// or https URL, without userinfo, a fragment, whitespace, control characters
// or a backslash, at most 2048 bytes. A query string is part of the address.
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
	if (parsed.Scheme != "https" && parsed.Scheme != "http") || !avatarHostTrusted(parsed.Hostname()) {
		return ""
	}
	return raw
}

func avatarHostTrusted(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" {
			return false
		}
	}
	for _, root := range avatarTrustedRoots {
		if host == root || strings.HasSuffix(host, "."+root) {
			return true
		}
	}
	return false
}
