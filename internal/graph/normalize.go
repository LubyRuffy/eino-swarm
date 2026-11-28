package graph

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// separator joins the parts of a composite identity value. It cannot appear
// in a normalized part.
const separator = "\x1f"

// Normalize applies one named normalizer to a single value. The asset
// normalizer takes several parts and is reached through NormalizeAsset.
func Normalize(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", invalid("empty value")
	}
	switch name {
	case "ip":
		return NormalizeIP(value)
	case "domain":
		return NormalizeDomain(value)
	case "url":
		return NormalizeURL(value)
	case "host":
		return NormalizeHost(value)
	case "repo":
		return NormalizeRepo(value)
	case "repo_path":
		return normalizeRepoPath(value)
	case "query":
		return NormalizeQuery(value), nil
	case "email":
		return NormalizeEmail(value)
	case "identifier":
		return NormalizeIdentifier(value), nil
	case "text":
		return NormalizeText(value), nil
	case "path":
		return NormalizePath(value)
	}
	return "", invalid("unknown normalizer %q", name)
}

// NormalizeIP returns the canonical text of an address without a zone;
// IPv4-mapped IPv6 addresses collapse to IPv4.
func NormalizeIP(value string) (string, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || ip.Zone() != "" {
		return "", invalid("%q is not an IP address", value)
	}
	return ip.Unmap().String(), nil
}

// NormalizeDomain applies IDNA and the usual hostname rules. It deliberately
// mirrors the EASM plugin's rule without importing it; a test compares them.
func NormalizeDomain(value string) (string, error) {
	domain, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.TrimSpace(value), "."))
	domain = strings.ToLower(domain)
	if err != nil || len(domain) > 253 || !strings.Contains(domain, ".") || net.ParseIP(domain) != nil {
		return "", invalid("%q is not a domain name", value)
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", invalid("%q is not a domain name", value)
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return "", invalid("%q is not a domain name", value)
			}
		}
	}
	return domain, nil
}

// NormalizeHost lowercases a host and drops a trailing dot. A host may carry
// a port; IPv6 literals keep their brackets.
func NormalizeHost(value string) (string, error) {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if host == "" || strings.ContainsAny(host, "/\\ \t\r\n@") {
		return "", invalid("%q is not a host", value)
	}
	return host, nil
}

var defaultPorts = map[string]string{"http": "80", "https": "443"}

func canonicalOrigin(u *url.URL) string {
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if defaultPorts[u.Scheme] == port {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host
}

// NormalizeURL lowercases scheme and host, drops default ports, the fragment
// and any embedded credentials, and gives an empty path as "/".
func NormalizeURL(value string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Hostname() == "" || u.Scheme == "" || strings.ContainsAny(u.Host, "\\\r\n\t ") {
		return "", invalid("%q is not a URL with a scheme and host", value)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	out := canonicalOrigin(u)
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	out += path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out, nil
}

var scpAddress = regexp.MustCompile(`^(?:[^@/\s]+@)?([^:/\s@]+):([^/\s].*)$`)

// splitRepoAddress extracts host and path from a URL or an scp-like address.
func splitRepoAddress(value string) (host, path string, ok bool) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.Hostname() == "" {
			return "", "", false
		}
		host = strings.ToLower(u.Hostname())
		if p := u.Port(); p != "" && p != "22" && p != "80" && p != "443" && p != "9418" {
			host += ":" + p
		}
		return host, u.Path, true
	}
	if m := scpAddress.FindStringSubmatch(value); m != nil {
		return strings.ToLower(m[1]), m[2], true
	}
	return "", "", false
}

func trimRepoPath(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	path = strings.TrimSuffix(path, ".git")
	return strings.Trim(path, "/")
}

// NormalizeRepo turns a repository address into host/path. SSH and HTTPS
// spellings of one repository, with or without .git, give the same value.
// The path keeps its case because not every host ignores it.
func NormalizeRepo(value string) (string, error) {
	host, path, ok := splitRepoAddress(value)
	if !ok {
		return "", invalid("%q is not a repository address with a host", value)
	}
	if path = trimRepoPath(path); path == "" {
		return "", invalid("%q has no repository path", value)
	}
	return host + "/" + path, nil
}

func normalizeRepoPath(value string) (string, error) {
	path := trimRepoPath(value)
	if path == "" || strings.Contains(path, "://") {
		return "", invalid("%q is not a repository path", value)
	}
	return path, nil
}

// RepoPath is the owner/name part of a repository address.
func RepoPath(value string) string {
	if _, path, ok := splitRepoAddress(value); ok {
		return trimRepoPath(path)
	}
	return ""
}

// NormalizeQuery trims the statement and collapses runs of whitespace outside
// double quotes. Quoted text, case and condition order are left alone: the
// host does not parse query syntax.
func NormalizeQuery(value string) string {
	var b strings.Builder
	inQuote, escaped, pendingSpace := false, false, false
	for _, r := range strings.TrimSpace(value) {
		switch {
		case inQuote:
			b.WriteRune(r)
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == '"' {
				inQuote = false
			}
		case unicode.IsSpace(r):
			pendingSpace = true
		default:
			if pendingSpace {
				b.WriteByte(' ')
				pendingSpace = false
			}
			b.WriteRune(r)
			if r == '"' {
				inQuote = true
			}
		}
	}
	return b.String()
}

// NormalizeEmail lowercases an address and requires a local part and a domain.
func NormalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || !strings.Contains(email[at+1:], ".") || strings.ContainsAny(email, " \t\r\n") {
		return "", invalid("%q is not an email address", value)
	}
	return email, nil
}

// NormalizeIdentifier removes all whitespace and upper-cases a registry
// number, serial or filing number.
func NormalizeIdentifier(value string) string {
	return strings.ToUpper(strings.Join(strings.Fields(value), ""))
}

// NormalizeText folds case and collapses whitespace.
func NormalizeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// NormalizePath is the identity of one file or directory. Slashes collapse
// and a trailing separator is dropped, while case stays: two paths that
// differ only by case are not the same file on every system. A location
// with a scheme is an address, not a path.
func NormalizePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "://") {
		return "", invalid("%q is not a file path", value)
	}
	value = strings.ReplaceAll(value, "\\", "/")
	for strings.HasPrefix(value, "./") {
		value = strings.TrimPrefix(value, "./")
	}
	absolute := strings.HasPrefix(value, "/")
	var parts []string
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", invalid("%q is not a file path", value)
	}
	path := strings.Join(parts, "/")
	if absolute {
		path = "/" + path
	}
	return path, nil
}

// NormalizeAsset builds the identity of one network service from IP, port,
// protocol and host. It follows the FOFA world plugin's asset identity: an
// omitted and an explicitly written default port are the same origin, and a
// web asset without a host is identified by its IP and port.
func NormalizeAsset(ipText, portText, protocolText, hostText string) (string, error) {
	ip, err := NormalizeIP(ipText)
	if err != nil {
		return "", err
	}
	port, err := strconv.Atoi(strings.TrimSpace(portText))
	if err != nil || port < 1 || port > 65535 {
		return "", invalid("%q is not a port", portText)
	}
	protocol := strings.ToLower(strings.TrimSpace(protocolText))
	origin := strings.ToLower(strings.TrimSpace(hostText))
	if protocol == "http" || protocol == "https" {
		host := strings.TrimSpace(hostText)
		if host == "" {
			host = net.JoinHostPort(ip, strconv.Itoa(port))
		}
		if !strings.Contains(host, "://") {
			host = protocol + "://" + host
		}
		u, err := url.Parse(host)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Scheme != protocol || strings.ContainsAny(u.Host, "\\\r\n\t ") {
			return "", invalid("%q is not a host for a %s asset", hostText, protocol)
		}
		origin = canonicalOrigin(u)
	}
	return strings.Join([]string{ip, strconv.Itoa(port), protocol, origin}, separator), nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
