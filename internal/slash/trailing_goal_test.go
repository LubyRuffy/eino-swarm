package slash

import (
	"encoding/json"
	"os"
	"testing"
)

// PC and engine consume the same cases so a composer fallback cannot turn a
// quoted command into a standing objective, or lose an actual trailing marker.
func TestTrailingGoalUsesTheTaskBeforeTheMarker(t *testing.T) {
	raw, err := os.ReadFile("testdata/trailing-goal.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Text      string  `json:"text"`
		Objective *string `json:"objective"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Text, func(t *testing.T) {
			arg, ok := Lookup(tc.Text, "goal")
			if tc.Objective == nil {
				if ok {
					t.Fatalf("literal became goal %q", arg)
				}
				return
			}
			if !ok || arg != *tc.Objective {
				t.Fatalf("arg=%q ok=%v want=%q", arg, ok, *tc.Objective)
			}
		})
	}
}

func TestTrailingGoalDoesNotChangeOtherCommands(t *testing.T) {
	for _, line := range []string{"Inspect /plan", "Inspect /compact", "Inspect /goal"} {
		if arg, ok := Lookup(line, "plan", "compact"); ok {
			t.Fatalf("%q became %q", line, arg)
		}
	}
}
