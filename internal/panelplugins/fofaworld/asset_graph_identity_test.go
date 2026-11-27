package fofaworld

import (
	"strconv"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
)

// The graph kernel keeps its own copy of this plugin's asset identity so it
// does not depend on a plugin. Two records are the same asset here exactly
// when the kernel says so; this pins the copies together.
func TestGraphAssetIdentityAgreesWithThisPlugin(t *testing.T) {
	type rec struct {
		ip       string
		port     int
		protocol string
		host     string
	}
	records := []rec{
		{"127.0.0.1", 443, "https", "EXAMPLE.invalid"},
		{"127.0.0.1", 443, "https", "example.invalid:443"},
		{"127.0.0.1", 443, "https", "https://example.invalid/path"},
		{"127.0.0.1", 443, "https", "example.invalid:8443"},
		{"127.0.0.2", 443, "https", "example.invalid"},
		{"127.0.0.1", 443, "https", ""},
		{"127.0.0.1", 80, "http", ""},
		{"127.0.0.1", 80, "http", "http://127.0.0.1:80"},
		{"127.0.0.1", 80, "http", "http://[::1]"},
		{"::ffff:127.0.0.1", 80, "http", ""},
		{"127.0.0.1", 22, "ssh", "Box.invalid"},
		{"127.0.0.1", 22, "ssh", "box.invalid"},
		{"127.0.0.1", 22, "SSH", "box.invalid"},
		{"127.0.0.1", 22, "ssh", ""},
		{"127.0.0.1", 22, "tcp", "box.invalid"},
	}
	ids := make([]string, len(records))
	keys := make([]string, len(records))
	for i, r := range records {
		a, err := decodeAsset(row(r.ip, r.port, r.protocol, r.host))
		key, kerr := graph.NormalizeAsset(r.ip, strconv.Itoa(r.port), r.protocol, r.host)
		if (err != nil) != (kerr != nil) {
			t.Fatalf("%+v: plugin error %v, kernel error %v", r, err, kerr)
		}
		ids[i], keys[i] = a.ID, key
	}
	for i := range records {
		for j := i + 1; j < len(records); j++ {
			if (ids[i] == ids[j]) != (keys[i] == keys[j]) {
				t.Errorf("%+v and %+v: plugin same=%t, kernel same=%t", records[i], records[j], ids[i] == ids[j], keys[i] == keys[j])
			}
		}
	}
}
