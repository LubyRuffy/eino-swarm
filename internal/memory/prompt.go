package memory

import (
	"fmt"
	"strings"
)

// PromptSections renders what a project adds to the manager's system prompt:
// the user's own instruction, the notes, and an index of the skills.
//
// Two decisions are worth knowing before changing this.
//
// The memory block is a **snapshot taken when the turn starts** and does not
// change while the turn runs. The agent's writes land on disk immediately and
// the tool results report the live state, but the prompt keeps its prefix
// stable so the provider's cache still hits. Re-rendering it mid-turn would
// invalidate that cache on every write.
//
// The skills block lists **names and summaries only**. Inlining the procedures
// would make the prompt grow with the store, which is the cost the index and
// skill_view exist to avoid: ten skills and a hundred cost nearly the same.
//
// It must stay task-agnostic. It describes where memory lives and when to
// write to it; nothing about any particular request belongs here.
func PromptSections(systemPrompt string, snap Snapshot, skills []SkillInfo, indexMax int, memoryEnabled bool) string {
	var b strings.Builder

	if instruction := strings.TrimSpace(systemPrompt); instruction != "" {
		b.WriteString("## This project\n\n")
		b.WriteString(instruction)
		b.WriteString("\n\n")
	}

	if !memoryEnabled {
		return b.String()
	}

	b.WriteString("## Memory\n\n")
	fmt.Fprintf(&b, "Notes carried over from earlier conversations in this project [%d%%, %d/%d characters]:\n\n",
		snap.Percent(), snap.Chars, snap.Limit)
	if len(snap.Entries) == 0 {
		b.WriteString("(nothing yet)\n\n")
	} else {
		for _, e := range snap.Entries {
			b.WriteString("- ")
			b.WriteString(strings.ReplaceAll(e, "\n", "\n  "))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(`This block is a snapshot from the start of this turn. Use the ` + ToolMemory + ` tool to
keep it true: store a durable fact, a stated preference, a convention or a
correction the human made, and remove one that has gone stale. Leave out
anything specific to this request and anything you could look up again. When
the store is nearly full, consolidate overlapping notes instead of adding.

`)

	b.WriteString("## Skills\n\n")
	if len(skills) == 0 {
		b.WriteString("No skills recorded yet.\n")
	} else {
		shown := skills
		if indexMax > 0 && len(shown) > indexMax {
			shown = shown[:indexMax]
		}
		b.WriteString("Procedures recorded in this project. Only the summaries are here:\n\n")
		for _, s := range shown {
			fmt.Fprintf(&b, "- %s — %s\n", s.Name, s.Description)
		}
		if len(skills) > len(shown) {
			fmt.Fprintf(&b, "- … and %d more\n", len(skills)-len(shown))
		}
		fmt.Fprintf(&b, "\nCall %s(name) to read one in full before doing work it may already cover.\n", ToolSkillView)
	}
	fmt.Fprintf(&b, `Use %s to record a procedure worth following again: a workflow with several
steps that worked, a recovery from a failure, a correction you were given.
Patch the skill that already covers a subject rather than adding a second one.

`, ToolSkillManage)

	return b.String()
}

// ReviewPrompt is the instruction for the review that runs after a turn.
//
// It is a separate agent with no sub-agents and only the memory tools: its job
// is to decide what was worth keeping, not to continue the work. The bar is
// deliberately high, because a store that fills with restated requests is one
// nobody reads.
//
// Task-agnostic, like every prompt here.
func ReviewPrompt() string {
	return fmt.Sprintf(`You are reviewing a conversation that has just finished, on behalf of the
assistant that held it. You are not continuing the work and you are not
replying to the human — nobody is waiting for your answer.

Decide what, if anything, is worth carrying into future conversations in this
project, and write it yourself:

- %s — a durable fact about the environment, a preference the human stated, a
  convention this project follows, or a correction you were given. One or two
  short notes at most, and only when they would change how a later conversation
  behaves. The store is bounded: if a write does not fit, consolidate
  overlapping notes with replace or drop stale ones with remove, then retry.
- %s — a procedure worth following again: several steps that worked, a recovery
  from a failure, a workaround for something that behaved unexpectedly. Patch
  the skill that already covers the subject instead of creating a near-duplicate;
  call %s first when you are not sure what one contains.

Most conversations produce nothing worth storing. Restating the request, the
answer, or facts that are easy to look up again makes the store worse. When
there is nothing to keep, write nothing and say so in one line.

Finish with one short line naming what you stored, or that you stored nothing.`,
		ToolMemory, ToolSkillManage, ToolSkillView)
}
