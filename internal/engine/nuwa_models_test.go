package engine

import (
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

// The Nuwa planner must treat verification as a mandatory, independent,
// adversarial phase, not optional advice. A producer that only re-runs its own
// cases ships confident-but-wrong results (the spec-edges regression), so the
// doctrine requires a separate verifier that derives the hardest inputs from the
// acceptance criteria. The wording stays task-agnostic: no sample request,
// domain, file or rule may leak into a prompt (see rule 2 in AGENTS.md).
func TestNuwaPlannerInstructionMandatesIndependentVerification(t *testing.T) {
	instruction := strings.Join(strings.Fields(nuwaPlannerInstruction), " ")

	for _, want := range []string{
		"verify it with an independent executor",
		"not the one that produced it",
		"use the project's executor model when one is saved",
		"omit model_ref or pass that exact ref",
		"a different model is refused",
		"derives its own checks from the stated objective, constraints and acceptance criteria",
		"boundary and extreme values",
		"every explicitly required rule or case",
		"producer's own passing cases are a claim, not verification",
		"Record the verification outcome and its evidence with record_work",
		`Pass intent "verify"`,
		"The host allows one verify and one fix",
		"refuses another before a worker starts",
		`intent "fix"`,
		"Report the unmet criterion instead of retrying a refused check",
	} {
		if !strings.Contains(instruction, want) {
			t.Errorf("planner instruction lost the mandatory-verification discipline: %q", want)
		}
	}

	// The reached planner prompt, not only the constant, carries the discipline.
	assembled := strings.Join(strings.Fields(managerPrompt(&tools.Set{}, "", true, false)), " ")
	if !strings.Contains(assembled, "verify it with an independent executor") {
		t.Fatal("the assembled planner prompt did not include independent verification")
	}

	// No evaluation task, domain or sample input may be hardcoded here.
	lowered := strings.ToLower(instruction)
	for _, leak := range []string{
		"notes.md", "e.g.", "for example", "money", "slug", "digit",
		"set_int_max_str_digits", "4300", "5000", "researcher", "pytest",
		"sandbox", "沙箱", "ctf", "flag", "pentest",
	} {
		if strings.Contains(lowered, leak) {
			t.Errorf("planner instruction hardcodes example-specific text %q", leak)
		}
	}
}
