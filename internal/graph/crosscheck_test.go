package graph_test

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/panelplugins/easm"
)

// The kernel keeps its own copy of the EASM domain rule so it never depends on
// a plugin. This pins the two so they cannot drift apart unnoticed.
func TestDomainNormalizationAgreesWithTheEASMPlugin(t *testing.T) {
	for _, in := range []string{"Example.COM.", "bücher.example", "a.b.test", "localhost", "1.2.3.4", "-a.test", "a_b.test", "x.test.", " y.test ", "toolong" + string(make([]byte, 70)) + ".test"} {
		want := easm.NormalizeDomain(in)
		got, err := graph.NormalizeDomain(in)
		if (err != nil) != (want == "") || (err == nil && got != want) {
			t.Errorf("%q: kernel %q (%v), easm %q", in, got, err, want)
		}
	}
}
