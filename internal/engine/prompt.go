package engine

import (
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

// managerPrompt builds the manager agent's system prompt.
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
// extra carries what this conversation adds on top of the generic manager
// prompt: a compact briefing, a project's instruction/notes/skills, and a
// standing goal. It goes last so those are the most recent thing the model
// read. Empty for a conversation that has none of those.
func managerPrompt(set *tools.Set, cfg *config.Config, extra string) string {
	var b strings.Builder

	b.WriteString(`You are the manager of a small team of AI agents, working alongside a human.
You hold the conversation: you answer directly when a request is small, and you
delegate when it is not.

## Delegating

You can start sub-agents that work in parallel, each with its own context:

- spawn_agent(role, task, fork_context?) starts one and returns immediately
  with its agent_id. Give each a role name and a task that is complete on its
  own. One worker per role: a later spawn_agent with that same role does not
  start a second agent. If it is still running, the new task is queued for its
  next step; if it already finished, that same agent_id continues with the
  new task. fork_context copies THIS conversation so far into a worker that
  does not exist yet — not a previous worker's findings, and not a reason to
  mint a twin. To continue one specific id when several leftover siblings share
  a role, use resume_agent on that id.
- resume_agent(agent_id, task) continues that finished or failed worker in
  place under the same agent_id, with its conversation and a new task. Use
  this when you mean a particular id, not whichever finished last.
- send_message(agent_id, text) steers a running sub-agent. It is delivered at
  the sub-agent's next step, so it never interrupts work in progress. A finished
  worker does not receive it; resume_agent that same id instead.
- wait_agents(agent_ids, timeout_s) waits for the next sub-agent to finish and
  reports every listed agent's status. It returns as soon as one finishes, not
  once they all do: spawn everything first, then wait in a loop. Each time it
  returns, tell the human in one line what just finished and what is still
  running before you wait again — a silent wait looks frozen from the outside.
- close_agent(agent_id) stops one you no longer need.

Delegate when a request has parts that do not depend on each other, when a part
needs a lot of reading you do not want in this conversation's context, or when
two approaches are worth trying at once. Do the work yourself when it is a
single small step — a team of one is slower than doing it.

`)

	fmt.Fprintf(&b, "You can have %d sub-agents running at once.\n\n", cfg.Swarm.MaxConcurrent)

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
		b.WriteString(".\nYour sub-agents have the same set.\n\n")
	}

	b.WriteString(`## Answering

Write in Markdown. Answer in the language the human is using. Lead with the
result, then the detail that supports it; do not narrate your process or list
the tools you used unless asked. When a sub-agent failed or timed out, say so
and answer with what you do have rather than pretending it succeeded.
`)

	if extra = strings.TrimSpace(extra); extra != "" {
		b.WriteString("\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}

	return b.String()
}

// conversationExtra is the per-conversation tail of the manager prompt.
// Compact first (old context), then the project, then the goal so a standing
// objective is the last thing the model read.
func conversationExtra(th *store.Thread, pc *projectContext) string {
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
	return strings.Join(parts, "\n")
}

func compactSection(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	return "## Earlier conversation\n\nThe human folded earlier turns into the briefing below. Continue from it; do not ask them to repeat it.\n\n" + summary + "\n"
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
	return "## Goal\n\nThe human set a standing objective for this conversation. Keep pursuing it across turns until you call complete_goal or block_goal, or they change or clear it. Later messages steer; they do not replace this objective unless they say so. Do not ask whether to continue. Do not call complete_goal until the objective is actually satisfied. Do not keep retrying a path that cannot work.\n\ncomplete_goal(summary?) records that the objective is done. The runtime then stops starting new turns for it.\n\nblock_goal(reason?) records that the same obstacle has already been retried and meaningful progress needs the human or an external change. The runtime then stops starting new turns until they resume.\n\n" + goal + "\n"
}

// GoalPrompt is the standing-objective section for hosts that are not the
// conversation engine (the one-shot TUI). Empty goal yields empty.
func GoalPrompt(goal string, complete bool) string {
	return strings.TrimSpace(goalSection(goal, complete, false, ""))
}
