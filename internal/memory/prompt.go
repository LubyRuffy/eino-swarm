package memory

import (
	"fmt"
	"strings"
)

// PressurePercent is when the prompt stops implying that replace will make
// room and says a growing write will be refused. Below this, the general
// budget rule is enough; at this fill, a longer replacement is the next
// refusal waiting to happen.
const PressurePercent = 80

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
	b.WriteString(formatNotesList(snap))
	b.WriteString(`This block is a snapshot from the start of this turn. Use the ` + ToolMemory + ` tool to
keep it true: store a durable fact, a stated preference, a convention or a
correction the human made, in one or two sentences, and remove one that has
gone stale. Leave out anything specific to this request, a remaining count or
other status that will change again, and any procedure — those belong in a
skill. A note that restates a recorded skill is refused. A replace that grows
a note still has to fit the budget and the per-note cap; when usage is high,
the only write that lands is one that reduces the character count.

`)
	if snap.Percent() >= PressurePercent {
		fmt.Fprintf(&b, "The store is at %d%% of its budget (%d/%d). A write that increases the character count will be refused, including replacing a note with a longer one. Shorten or drop notes before storing anything new.\n\n",
			snap.Percent(), snap.Chars, snap.Limit)
	}

	b.WriteString("## Skills\n\n")
	b.WriteString(formatSkillsIndex(skills, indexMax))
	// skill_view looks in this project's memory, not the workspace. The same
	// SKILL.md layout often lives in the repository for other tools; treating
	// a directory listing as an index entry is how a missing-name call happens.
	b.WriteString(skillViewWorkspaceRule())
	fmt.Fprintf(&b, `
Use %s to record a procedure worth following again: a workflow with several
steps that worked, a recovery from a failure, a correction you were given.
One subject is one skill. Names that share a stem are the same subject —
patch or merge rather than adding a second name. A create that collides with
an existing skill is refused and names that skill. Leftover families are
folded into one skill after a turn.

Sub-agents receive the same notes snapshot and skills index, and they have %s.
They cannot call %s or %s. Their final message is the task result; they may
append a short durable convention or procedure. Most tasks have nothing to add.
If they do, store it yourself when it would change later work. Do not paste
that note into the human-facing answer unless they asked.

`, ToolSkillManage, ToolSkillView, ToolMemory, ToolSkillManage)

	return b.String()
}

// WorkerPromptSections is what a sub-agent reads about this project's memory.
// Notes and the skills index are the same snapshot the manager saw; write
// tools are not named, because workers do not have them. Empty when memory
// is off — the caller must not inject a section that advertises a missing tool.
func WorkerPromptSections(snap Snapshot, skills []SkillInfo, indexMax int) string {
	var b strings.Builder
	b.WriteString("## Memory\n\n")
	b.WriteString(formatNotesList(snap))
	b.WriteString(`This block is a snapshot from the start of this turn. Read it. You cannot
add, replace or remove notes, and you cannot record a skill.

`)
	b.WriteString("## Skills\n\n")
	b.WriteString(formatSkillsIndex(skills, indexMax))
	b.WriteString(skillViewWorkspaceRule())
	b.WriteString(`
## Reporting back

Your final message is for the manager, not the human. Lead with the result of
this task. Do not paste a large workspace file into it.

If you learned a durable convention or a procedure that would change later
work in this project — not findings specific to this task, not a remaining
count or other status that will change again — append a short note the
manager can store. Most tasks have nothing to add; then add nothing. Do not
write a recap of the task as experience.
`)
	return b.String()
}

func formatNotesList(snap Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Notes carried over from earlier conversations in this project [%d%%, %d/%d characters]:\n\n",
		snap.Percent(), snap.Chars, snap.Limit)
	if len(snap.Entries) == 0 {
		b.WriteString("(nothing yet)\n\n")
		return b.String()
	}
	for _, e := range snap.Entries {
		b.WriteString("- ")
		b.WriteString(strings.ReplaceAll(e, "\n", "\n  "))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

func formatSkillsIndex(skills []SkillInfo, indexMax int) string {
	var b strings.Builder
	if len(skills) == 0 {
		b.WriteString("No skills recorded yet. " + ToolSkillView +
			" opens only names from this index, so it has nothing to open until one is recorded.\n")
		return b.String()
	}
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
	return b.String()
}

func skillViewWorkspaceRule() string {
	return fmt.Sprintf("A procedure found as a file in the workspace is a file — read it; %s does not open workspace files.\n", ToolSkillView)
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

Most conversations produce nothing worth storing. Restating the request, the
answer, a remaining count, a list of completed items, a commit identifier, or
facts that are easy to look up again makes the store worse. When there is
nothing to keep, write nothing and say so in one line.

Decide what, if anything, is worth carrying into future conversations in this
project, and write it yourself:

- %s — one or two short sentences: a durable fact about the environment, a
  preference the human stated, a convention this project follows, or a
  correction you were given, and only when they would change how a later
  conversation behaves. Leave out a status that will change again. A runbook,
  a connection sequence, or a multi-step recovery is a skill, not a note. The
  store is bounded by a total and by a per-note cap: a write that grows past
  either is refused, including replace with a longer note. If a write does not
  fit, the result says why; do not retry the same text — shorten the note,
  drop a stale one, or record a procedure as a skill.
- %s — a procedure worth following again: several steps that worked, a
  recovery from a failure, a workaround for something that behaved
  unexpectedly. One subject is one skill. Before creating, call %s on every
  index entry whose name or summary might already cover the subject. A create
  that collides is refused and names the existing skill — patch that one, or
  delete it first. Do not add a second skill whose name is the first plus a
  suffix, and do not add a chapter-skill that shares a name stem with one
  already recorded. If the catalog lists a family of names that share a
  subject, merge them with %s (name is the skill to keep, sources are the
  others) so a later conversation is not handed competing procedures.

Finish with one short line naming what you stored, or that you stored nothing.`,
		ToolMemory, ToolSkillManage, ToolSkillView, ToolSkillManage)
}

// ReviewCatalog is appended to the reviewer's user message so it can see the
// skills already recorded. The system prompt is a fixed instruction and does
// not carry the live index — without this, the reviewer invents a second
// name for a subject that is already stored.
func ReviewCatalog(skills []SkillInfo, families [][]string) string {
	if len(skills) == 0 && len(families) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Skills already recorded in this project:\n\n")
	if len(skills) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, s := range skills {
			fmt.Fprintf(&b, "- %s — %s\n", s.Name, s.Description)
		}
	}
	if len(families) > 0 {
		b.WriteString("\nThese recorded skills share a subject and must become one skill. Merge each group so a later conversation is not handed competing procedures:\n")
		for _, fam := range families {
			fmt.Fprintf(&b, "- %s\n", strings.Join(fam, ", "))
		}
	}
	return b.String()
}

// AttachReviewCatalog puts the live index after the conversation. An empty
// catalog is a no-op so an empty turn stays empty and is not reviewed.
func AttachReviewCatalog(transcript, catalog string) string {
	catalog = strings.TrimSpace(catalog)
	if catalog == "" {
		return transcript
	}
	transcript = strings.TrimRight(transcript, "\n")
	if strings.TrimSpace(transcript) == "" {
		return catalog
	}
	return transcript + "\n\n" + catalog
}
