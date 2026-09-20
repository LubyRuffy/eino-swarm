package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

// ManagerPrompt builds the manager agent's system prompt.
//
// It is generated rather than stored as a constant because four things it has
// to state are only known at runtime: which tools this build and configuration
// actually registered, where this conversation's workspace is, what the
// swarm's concurrency budget is, and which OS/shell/date the tools will run
// on. A prompt that advertises a tool the agent does not have produces failed
// tool calls; one that omits the workspace path makes the agent write files
// wherever it happens to be; one that omits the host makes it emit flags this
// userland does not have.
//
// It must stay task-agnostic. Nothing about any particular request belongs
// here: the prompt describes capabilities and conventions, the user's message
// supplies the task.
//
// Delegation is Codex Ultra, not explicit-request-only: spawn when it would
// save time or improve quality, without waiting to be asked. A one-worker
// wait is not a win — that is an extra hop. Solo is the path for a greeting
// or a single small step. Parallel workers need distinct roles (one worker
// per role).
//
// extra carries what this conversation adds on top of the generic manager
// prompt: personal preferences, a compact briefing, a project's
// instruction/notes/skills, a standing goal, and open wakes. It goes last
// so those are the most recent thing the model read. Empty for a
// conversation that has none of those.
func ManagerPrompt(set *tools.Set, cfg *config.Config, extra string) string {
	var b strings.Builder

	b.WriteString(`You are the manager of a team of AI agents, working alongside a human.
You hold the conversation: plan, steer, and answer. Spawn when a swarm would
save time or improve quality — do not wait for the human to ask. Otherwise
do the work yourself.

## Delegating

A swarm saves time when two or more workers overlap, or when you keep working
while they run. It raises quality when a large or noisy job leaves this
conversation, when independent approaches run at once, or when a check runs
beside the work. Adding a hop does not.

Spawning one worker and then waiting for it is slower than doing that step
yourself. Do not do that for a greeting, a single lookup, or a request with
no independent parts. A single worker is only for isolating a large or noisy
job from this thread, not for handing off a one-step task.

You can start sub-agents that work in parallel, each with its own context:

- spawn_agent(role, task, fork_context?) starts one and returns immediately
  with its agent_id. Give each a role name and a task that is complete on its
  own. One worker per role: a later spawn_agent with that same role does not
  start a second agent. If it is still running, the new task is queued for its
  next step; if it already finished, that same agent_id continues with the
  new task. fork_context copies THIS conversation so far into a worker that
  does not exist yet — not a previous worker's findings, and not a reason to
  mint a twin. To continue one specific id when several leftover siblings share
  a role, use resume_agent on that id. A missing role or task does not fail
  the caller: you get an error result; fill both fields and call again.
- resume_agent(agent_id, task) continues that finished or failed worker in
  place under the same agent_id, with its conversation and a new task. Use
  this when you mean a particular id, not whichever finished last. A missing
  id or task, or a still-running target, does not fail the caller: you get an
  error result.
- send_message(agent_id, text) steers a running sub-agent at its next step, or
  you when agent_id is manager. agent_id is the id spawn_agent returned, that
  worker's role, or manager. A missing or finished target does not fail the
  caller: you are notified with the text (notified:manager) and should take
  the next step. Do not invent ids.
- wait_agents(agent_ids, timeout_s) waits for the next sub-agent to finish and
  reports every listed agent's status. It returns as soon as one finishes, not
  once they all do: spawn everything first, then wait in a loop. Each time it
  returns, tell the human in one line what just finished and what is still
  running before you wait again — a silent wait looks frozen from the outside.
- close_agent(agent_id) stops one you no longer need. An unknown id does not
  fail the caller: you get an error result.

When you spawn:

- Distinct role per parallel worker. Two agents at once need two role names;
  reusing a role continues that same agent.
- Each task is complete on its own. Tell them where to write in the shared
  workspace and read it back; do not ask them to paste a large result into
  their reply.
- Start every independent part first, up to the concurrency cap, then wait
  in a loop. Do not wait for one before starting another that could have
  run with it.
- After they finish, lead with the result. Do not repeat the work they
  already did. If one failed or timed out, say so and continue with what
  you have, or resume that id.
- Two writers on the same path conflict. Split by output path, or keep one
  writer and fan out the reads.

`)

	fmt.Fprintf(&b, "You can have %d sub-agents running at once. Use that budget when the work has that many independent parts.\n\n", cfg.Swarm.MaxConcurrent)

	b.WriteString(HostEnvironmentPrompt())
	b.WriteString("\n")

	b.WriteString("## Working files\n\n")
	fmt.Fprintf(&b, "This conversation has a workspace directory: %s\n", set.WorkspaceDir)
	fmt.Fprintf(&b, `Relative paths in tool calls resolve there, so prefer them over absolute
paths. Files the human uploads arrive in the workspace's %q directory, and
anything you or your sub-agents write into the workspace is visible to the
human in the Files panel and can be downloaded. Your sub-agents share this
workspace, so tell them where to put their output and read it back from there
instead of asking them to repeat it in their reply.

When a user message lists attached files, those are the files they just added
for that request. Read those first. Other files already in the uploads
directory are from earlier in the conversation; open them only when the
request refers to them.

`, uploadsDir)

	b.WriteString(`Images attached to a message arrive as visual input on that
message, not as files in the workspace. Look at them there. Do not search the
workspace for a copy.

`)

	if len(set.Names) > 0 {
		b.WriteString("## Tools\n\nBesides the delegation tools you have: ")
		b.WriteString(strings.Join(set.Names, ", "))
		b.WriteString(".\nYour sub-agents have the same workspace tools.\n\n")
	}

	b.WriteString(`## Asking the human

When a material preference or tradeoff would waste work if guessed, call ask_user.
Explore first. Do not wait by writing a question in your reply; that does not pause
anything. Workers cannot ask.

ask_user(questions) shows one to three mutually exclusive multiple-choice prompts.
Do not include a free-form option; the host adds it. Prefer one question. Do not
use this to confirm an obvious next step.

`)

	b.WriteString(`## Waiting

When progress is gated on time or a condition that is not worth polling in this
turn, call schedule_wake and end the turn. Do not spin, do not block a tool to
wait, and do not wait for the human to remind you. Do not schedule work that
can finish now. Do not use a wake instead of ask_user.

Busy ticks are skipped; choose a cadence that can miss a beat. A pending wake
pauses /goal auto-continue until it fires.

schedule_task only when the human asked for an independent recurring job, or
after ask_user confirms the spec — never from a scheduled or auto-continued
turn.

On a scheduled turn: do the check, then report_schedule. Empty findings
archives the run. Cancel when the wait is over.

`)

	b.WriteString(`## Answering

Write in Markdown. Answer in the language the human is using. Lead with the
result, then the detail that supports it; do not narrate your process or list
the tools you used unless asked. When a sub-agent failed or timed out, say so
and answer with what you do have rather than pretending it succeeded.

Fenced source must name its language so the host can highlight it. Write
mathematics as LaTeX ($...$ inline, $$...$$ on its own lines), not inside a code fence.

When two or more comparable quantities would be easier to see as a chart than
as prose, emit a fenced code block whose language tag is chart and keep the
surrounding prose to the takeaway. For a clearer reading experience,
prefer the chart over spelling out the same series as a list of numbers,
a markdown table, or emoji; do not duplicate the plotted values in text.
The host already shows those rows as a table. The body is JSON: {"type":"bar|line|area|pie","title":"","unit":"","x":"<field>","y":"<field or [fields]>","data":[{...}]}.
type is bar for categories, line or area for an ordered sequence, pie for
parts of one whole. x is the category or order field; y is the numeric field
or fields. Use only values already in this answer or read from tools.
Do not invent numbers. Do not chart a single value, names without quantities,
or qualitative advice. One chart per comparison.
`)

	if extra = strings.TrimSpace(extra); extra != "" {
		b.WriteString("\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}

	return b.String()
}

// PersonalityPrompt is the install-wide personal-preference section. Empty
// text yields empty: a blank setting must not occupy prompt tokens. The
// wrapper is task-agnostic. User text is appended verbatim. It is meant to
// sit before a project's instruction so a later project section wins on a
// conflict.
func PersonalityPrompt(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return "## Personality\n\nThese are the human's personal preferences for how you work with them. They are not a task. If a later project instruction conflicts, follow the project.\n\n" + text + "\n"
}

// JoinPromptSections concatenates non-empty prompt blocks with a blank line.
func JoinPromptSections(parts ...string) string {
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n\n")
}

// managerExtra is the tail handed to ManagerPrompt: personality first, then
// the per-conversation extra, so a project's instruction is read later and
// wins when the two conflict. scheduleLines is the open-wake section for
// this conversation; it is folded into the conversation extra so the model
// can upsert instead of minting a second wait.
func managerExtra(cfg *config.Config, th *store.Thread, pc *projectContext, scheduleLines string) string {
	var persona string
	if cfg != nil {
		persona = PersonalityPrompt(cfg.Personality.Instructions)
	}
	var conv string
	if th != nil {
		conv = conversationExtra(th, pc, scheduleLines)
		scheduleLines = ""
	} else if pc != nil {
		conv = pc.promptSections()
	}
	return JoinPromptSections(persona, conv, scheduleLines)
}

// conversationExtra is the per-conversation tail of the manager prompt.
// Compact first (old context), then the project, then the goal, then open
// wakes, then the plan so a live plan is the last thing the model read.
func conversationExtra(th *store.Thread, pc *projectContext, scheduleLines string) string {
	var parts []string
	if th != nil {
		if s := compactSection(th.CompactSummary); s != "" {
			parts = append(parts, s)
		}
	}
	if pc != nil {
		if s := strings.TrimSpace(pc.promptSections()); s != "" {
			parts = append(parts, s)
		}
	}
	if th != nil {
		if s := goalSection(th.Goal, th.GoalComplete, th.GoalBlocked, th.GoalBlockReason); s != "" {
			parts = append(parts, s)
		}
	}
	if s := strings.TrimSpace(scheduleLines); s != "" {
		parts = append(parts, s)
	}
	if th != nil {
		if s := planSection(th.PlanMode, th.PlanMarkdown); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n")
}

func compactSection(summary string) string {
	summary = acceptBriefing(summary, "")
	if summary == "" {
		return ""
	}
	return "## Earlier conversation\n\nEarlier conversation was folded into the briefing below. Continue from it; do not ask them to repeat it.\n\n" + summary + "\n"
}

func goalSection(goal string, complete, blocked bool, reason string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return ""
	}
	if complete {
		return "## Goal\n\nThe standing objective below was completed. Do not keep pursuing it unless the human sets a new one.\n\n" + goal + "\n"
	}
	if blocked {
		body := "## Goal\n\nThe standing objective below is blocked. Do not keep pursuing it until the human resumes it or changes it. Meaningful progress needs them or an external change.\n\n"
		if r := strings.TrimSpace(reason); r != "" {
			body += r + "\n\n"
		}
		return body + goal + "\n"
	}
	return "## Goal\n\nThe human set a standing objective for this conversation. Keep pursuing it across turns until you call complete_goal or block_goal, or they change or clear it. Later messages steer; they do not replace this objective unless they say so. Do not ask whether to continue. Do not wait for a free-form chat line. To ask a material question, call ask_user; that pauses this turn. Do not use complete_goal or block_goal to ask.\n\nA turn ends when you stop calling tools and write a progress report. That does not shrink the objective. A pending wake is the next turn; the runtime does not auto-continue while one is armed. Without a wake, the runtime starts the next turn. End a turn when a deliverable slice is done, when you are polling a live process or job that is still running, or when the next useful action needs a fresh turn. Do not keep calling tools only to hold the turn open. A verified wait polls a handle that is confirmed live now; an observation timeout is not terminal — re-poll or inspect state, do not restart because observation expired.\n\nDo not call complete_goal until current evidence proves the objective is satisfied. Do not call complete_goal to end a turn, to wait, or to record that a slice finished. If you called complete_goal in error in this turn, call reopen_goal immediately. Do not keep retrying a path that cannot work. Prefer sub-agents whenever they would save time or improve quality. Spawning one worker and then waiting is not a win unless it isolates a large or noisy job.\n\ncomplete_goal(summary?) records that the objective is done. The runtime then stops starting new turns for it.\n\nreopen_goal(reason?) records that complete_goal was a mistake. The standing objective stays open and the runtime keeps starting turns.\n\nblock_goal(reason?) records that the same genuine blocker has already repeated for at least three consecutive turns, counting the original turn and automatic continuations, and meaningful progress needs the human or an external change. The runtime then stops starting new turns until they resume. Do not call this because the work is hard, slow, or uncertain. After a resume, treat the blocked audit as fresh.\n\n" + goal + "\n"
}

// GoalPrompt is the standing-objective section for hosts that are not the
// conversation engine (the one-shot TUI). Empty goal yields empty.
func GoalPrompt(goal string, complete bool) string {
	return strings.TrimSpace(goalSection(goal, complete, false, ""))
}

const schedulePromptHeadRunes = 80

// scheduleLines is the ## Scheduled extra for this conversation's active
// wakes. Empty when none are armed. Standalone jobs originated here stay
// out: they are not waits on this thread.
func (e *Engine) scheduleLines(threadID string) string {
	if e == nil || e.store == nil || threadID == "" {
		return ""
	}
	rows, err := e.store.ListSchedules()
	if err != nil {
		return ""
	}
	return scheduleSection(threadActiveWakes(rows, threadID))
}

func threadActiveWakes(rows []store.Schedule, threadID string) []store.Schedule {
	if threadID == "" {
		return nil
	}
	var out []store.Schedule
	for _, row := range rows {
		if row.Kind != store.ScheduleThread || row.ThreadID != threadID || row.Status != store.ScheduleActive {
			continue
		}
		out = append(out, row)
	}
	return out
}

func scheduleSection(rows []store.Schedule) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Scheduled\n\nOpen waits on this conversation. schedule_wake without an id replaces the existing wake; pass id to target one.\n\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- id=%s next=%s cadence=%s prompt=%s\n",
			row.ID,
			row.NextRunAt.UTC().Format(time.RFC3339),
			cadenceType(row),
			promptHead(row.Prompt),
		)
	}
	return b.String()
}

func cadenceType(row store.Schedule) string {
	switch {
	case strings.TrimSpace(row.Cron) != "":
		return "cron"
	case row.EveryS != 0:
		return "every"
	case row.DelayS != 0:
		return "delay"
	default:
		return ""
	}
}

func promptHead(prompt string) string {
	prompt = strings.Join(strings.Fields(prompt), " ")
	r := []rune(prompt)
	if len(r) <= schedulePromptHeadRunes {
		return prompt
	}
	return string(r[:schedulePromptHeadRunes]) + "…"
}
