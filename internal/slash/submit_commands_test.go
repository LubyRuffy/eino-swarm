package slash

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// engineCommand is the order Engine.StartTurnInput applies: a leading /plan
// owns any trailing /goal in its argument, otherwise /goal is looked up.
func engineCommand(line string) (name, arg string) {
	if arg, ok := Lookup(line, "plan"); ok {
		return "plan", arg
	}
	if arg, ok := Lookup(line, "goal"); ok {
		return "goal", arg
	}
	return "", ""
}

// The desktop acts on its own parse (setting a goal, entering plan mode,
// compacting) while the phone and HTTP send raw text for the engine to read.
// If the two disagree, one text does different things depending on where it
// was typed, and the desktop drops the words its parse did not keep.
// frontend/src/lib/slash.test.ts reads the same cases.
func TestSubmittedCommandsMatchTheComposer(t *testing.T) {
	raw, err := os.ReadFile("testdata/submit-commands.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text    string `json:"text"`
		Command string `json:"command"`
		Arg     string `json:"arg"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Text, func(t *testing.T) {
			want, wantArg := tc.Command, tc.Arg
			if want == "compact" {
				// The composer compacts locally; the engine never reads it.
				want, wantArg = "", ""
			}
			if name, arg := engineCommand(tc.Text); name != want || arg != wantArg {
				t.Fatalf("engine read %q %q, want %q %q", name, arg, want, wantArg)
			}
		})
	}
}

// SLASH_DIFF_IN is a JSONL file of composer parses written by
// frontend/src/lib/slash-differential.test.ts; see docs/TESTING.md.
func TestComposerParsesMatchTheEngine(t *testing.T) {
	path := os.Getenv("SLASH_DIFF_IN")
	if path == "" {
		t.Skip("set SLASH_DIFF_IN to compare a composer parse corpus")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var diffs []string
	total := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r struct {
			Text    string `json:"text"`
			Command string `json:"command"`
			Arg     string `json:"arg"`
			Leading bool   `json:"leading"`
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		total++
		if r.Command == "compact" && r.Leading {
			// The desktop compacts locally and never sends this text.
			continue
		}
		if name, arg := engineCommand(r.Text); name != r.Command || arg != r.Arg {
			diffs = append(diffs, fmt.Sprintf("%q: composer %q %q, engine %q %q", r.Text, r.Command, r.Arg, name, arg))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if total == 0 {
		t.Fatal("empty corpus")
	}
	if len(diffs) > 0 {
		t.Fatalf("%d of %d parses differ, first: %v", len(diffs), total, diffs[:min(10, len(diffs))])
	}
}
