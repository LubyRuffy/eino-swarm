package memory

import (
	"strings"
	"testing"
)

// The reviewer must repair the cited operation from source bytes, rather than
// spending its remaining rounds on the same invalid reference or number.
func TestReviewPromptRepairsCitationsAndAllUnsupportedNumbers(t *testing.T) {
	prompt := ReviewPrompt()
	for _, required := range []string{
		"visible raw event text",
		"preserving JSON escapes",
		"Only the quotes cited by that operation",
		"the note being replaced",
		"issues array",
		"fix all issues in one revised complete batch",
		"top-level fields repeat the first issue for compatibility",
		"index and repair",
		"invalid_evidence",
		"admitted source sequences",
		"source_text",
		"unsupported_numbers",
		"supported_numbers",
		"all listed defects in that operation",
		"resubmit the complete batch",
		"Do not spell numbers in words to bypass",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("review repair guidance lacks %q", required)
		}
	}
	for _, preserved := range []string{
		"never cite tool_call",
		"derived summaries are not new evidence",
		"existing iteration budget",
		"not a way to hide blocked writes",
		"revision-checked commit",
	} {
		if !strings.Contains(prompt, preserved) {
			t.Errorf("repair guidance weakened %q", preserved)
		}
	}
}
