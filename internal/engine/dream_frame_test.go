package engine

import (
	"strings"
	"testing"
)

func TestDreamFrameAcceptsOnlyOneCompleteHumanCommand(t *testing.T) {
	const good = `{"name":"check-file","before":"Check the name.","after":"Check the digest."}`
	frame := func(body string) string { return dreamCorrectionFence + "\n" + body + "\n```" }
	for _, tc := range []struct {
		name, message string
		found, valid  bool
	}{
		{"complete", frame(good), true, true},
		{"whitespace", " \n" + frame(good) + "\n", true, true},
		{"quoted", "Here's an example:\n" + frame(good), false, false},
		{"trailing conversation", frame(good) + "\nCompare it.", false, false},
		{"unfinished", dreamCorrectionFence + "\n" + good, false, false},
		{"duplicate", frame(`{"name":"other",` + good[1:]), true, false},
		{"unknown", frame(`{"other":"no",` + good[1:]), true, false},
		{"multiple values", frame(good + ` {}`), true, false},
		{"oversized", frame(strings.Repeat("a", 64*1024+1)), true, false},
		{"bad name", frame(strings.Replace(good, "check-file", "../file", 1)), true, false},
		{"missing", frame(`{"name":"check-file","before":"x"}`), true, false},
		{"null", frame(`{"name":"check-file","before":"x","after":null}`), true, false},
		{"array", frame(`[]`), true, false},
		{"unclosed object", frame(strings.TrimSuffix(good, "}")), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, found := dreamFrame(tc.message)
			_, valid := parseDreamCorrection(body)
			if found != tc.found || valid != tc.valid {
				t.Fatalf("frame=%t valid=%t, want frame=%t valid=%t", found, valid, tc.found, tc.valid)
			}
		})
	}
}
