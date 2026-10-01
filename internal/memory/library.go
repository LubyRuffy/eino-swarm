package memory

import (
	"fmt"
	"strings"
)

// LibraryID is the origin id of the shared skill library. It is not a project
// id — those are minted as pj_… — so copy and pull can tell a library file
// from a project row. The directory itself lives under the data directory;
// this package only stores the id that SKILL.md records.
const LibraryID = "library"

// LibraryReviewMark is the first line of a library review's user message.
// The instruction is fixed; the mark is how a scripted reviewer can tell this
// job from a project review without baking a task into the prompt.
const LibraryReviewMark = "Shared skill library:"

// LibraryReviewPrompt is the post-turn review for a conversation that belongs
// to no project. It may record procedures in the shared library. It has no
// note store: a fact about "this project" has nowhere to live, and a note
// written here would be injected into every later conversation.
func LibraryReviewPrompt() string {
	return fmt.Sprintf(`You are reviewing a conversation that has just finished and belongs to no project.
You are not continuing the work and you are not replying to the human — nobody is waiting for your answer.

Most conversations produce nothing worth storing. Restating the request, the
answer, a remaining count, a list of completed items, or facts that are easy
to look up again must not become a skill. When there is nothing to keep,
write nothing and say so in one line.

A procedure worth following again — several steps that worked, a recovery
from a failure, a workaround for something that behaved unexpectedly — goes
in the shared skill library with %s. That library is not a project. A person
may later copy a skill from it into one project's own catalog. Do not write
notes; this review has no note store.

One subject is one skill. The index already lists each name and summary.
Call %s only when a summary might be the same subject, and only enough to
decide — do not open the catalog one by one. A create that collides is
refused and names the existing skill — patch that one, or delete it first.
Do not add a second skill whose name is the first plus a suffix, and do not
add a chapter-skill that shares a name stem with one already recorded.
Names that share a stem are folded into one skill after you finish, so do
not merge the whole catalog. If this conversation just added a chapter
beside an existing stem, merge that one group with %s (name is the skill
to keep, sources are the others).

Finish with one short line naming what you stored, or that you stored nothing.`,
		ToolSkillManage, ToolSkillView, ToolSkillManage)
}

// LibraryTidyPrompt is the Memory panel's tidy for the shared library.
// Same job as a project's catalog tidy, without a project to attribute it to.
func LibraryTidyPrompt() string {
	return strings.Replace(CatalogTidyPrompt(),
		"this project's recorded skills",
		"the shared skill library", 1)
}

// LibraryCatalog is the live index for a library review. The header must not
// say "this project": that is how a reviewer invents notes for a project
// that is not there.
func LibraryCatalog(skills []SkillInfo, families [][]string) string {
	cat := ReviewCatalog(skills, families)
	if cat == "" {
		return ""
	}
	return strings.Replace(cat,
		"Skills already recorded in this project:",
		"Skills already recorded in the shared library:", 1)
}

// LibraryPromptSections is what a conversation in no project adds to the
// manager prompt: the shared skill index, and nothing else. Notes stay in
// projects. The live turn can open a skill; it cannot write the library.
// Writing happens in the post-turn review.
func LibraryPromptSections(skills []SkillInfo, indexMax int) string {
	var b strings.Builder
	b.WriteString("## Skills\n\n")
	b.WriteString(formatSkillBullets(skills, indexMax,
		"Procedures recorded in the shared skill library. Only the summaries are here:\n\n"))
	b.WriteString(skillViewWorkspaceRule())
	fmt.Fprintf(&b, "\nEvery conversation that belongs to no project sees this index. Sub-agents receive the same index and %s. A skill is recorded after the turn finishes, not by a tool on this turn.\n", ToolSkillView)
	return b.String()
}

// LibraryWorkerPromptSections is the same index for a sub-agent. No write
// tools are named, because a worker does not have them.
func LibraryWorkerPromptSections(skills []SkillInfo, indexMax int) string {
	var b strings.Builder
	b.WriteString("## Skills\n\n")
	b.WriteString(formatSkillBullets(skills, indexMax,
		"Procedures recorded in the shared skill library. Only the summaries are here:\n\n"))
	b.WriteString(skillViewWorkspaceRule())
	b.WriteString(`
## Reporting back

Your final message is for the manager, not the human. Lead with the result of
this task. Do not paste a large workspace file into it.
`)
	return b.String()
}

// LibraryReviewMessage is the user turn for a conversation that belongs to
// no project. The mark stays even when the catalog is empty, so the review
// is still a library review and not a project one.
func LibraryReviewMessage(transcript, catalog string) string {
	body := LibraryReviewMark + "\n\n" + strings.TrimSpace(transcript)
	return AttachReviewCatalog(body, catalog)
}
