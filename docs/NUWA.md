# Nuwa project collaboration

Nuwa separates planning from execution: the conversation manager plans,
delegates, coordinates and accepts results from worker evidence. Workers perform
the required operations. Agents choose their division of work and configured
models while keeping shared tasks, reported facts and results inspectable.
Enable **Nuwa collaboration** when creating or editing a project, then put the objective,
constraints and acceptance criteria in **Instruction**. Existing projects default
to ordinary mode. No domain workflow or predefined executor role list is required.

Nuwa workers receive **Instruction** directly as shared project context, even
with project memory disabled. They do not have to rely on the planner's task
summary to learn the project's constraints and acceptance criteria. Requirements
addressed to the coordinator do not change an executor's assigned role or tools;
the manager's system prompt and install-wide personality remain separate.
This supplies the original requirements, not proof that an agent followed them.
Nonempty saved worker instructions restore as recorded. Legacy empty instructions
retain the existing fallback to the resolved worker configuration. These are
first-restore semantics, not a promise that all later resumed generations freeze
the same project text.

Connections, credentials and tools remain platform resources configured in
Settings. An instruction can explain how to use them; it cannot connect an
absent service, grant permission or supply missing data. The workspace remains
an anchor with the agent's normal access.

Enable the mode, enter your instruction and open a conversation in the project.
This starter prompt leaves worker roles, task division and model choices to the agent.

```text
Achieve [objective] using [available inputs].
Respect [constraints]. The result is acceptable when [observable checks pass].
Plan from the available evidence and delegate any required operations to workers.
Choose worker roles, task scopes and available models to fit the objective.
Accept the result using the workers' evidence against the acceptance criteria.
For shared findings and results, record their sources and artifact paths.
```

During the turn, open **Collaboration**. Use **Expand graph** when work has
been recorded, then select a node and open **Details and evidence** to inspect
its sources.

## Planning, models and delegation

The conversation model remains the manager. It does not mount workspace
execution tools or MCP tools, and its provider-hosted builtin execution tools
are disabled. Workers retain their configured provider-hosted tools; ordinary
mode retains its existing configuration. Reading or changing workspace files, running
commands and using external execution tools belong to workers; the manager
coordinates their work and checks their reported evidence. Pure discussion that
requires no such operations does not require spawning a worker. This boundary
does not prescribe a domain, role list, workflow or worker model.

Nuwa managers and workers receive
`list_models`, `read_work` and, outside planning mode, `record_work`. Workers can
delegate again. All descendants share one Registry, concurrency budget, Stop
and restart lifecycle. A worker may wait for, resume or close its own
descendants; lifecycle control outside its subtree refuses. Messages retain
their existing peer/manager semantics.

`list_models` returns configured, ready choices with exact `ref`, provider/model,
label and known context window. Names are resources, not capability ratings.
Copy its opaque ref into `spawn_agent(..., model_ref: ref)` without constructing
or decoding it. This tool token is separate from the UI/CLI provider ChoiceID.
`default_scope: "conversation"` means the catalog's `default` identifies the
model chosen for the owning conversation turn. It does not identify the model
running a worker. When a worker calls `list_models`, `current` separately
reports its ID, role, parent if any, generation and recorded model binding.
Choose a new worker using `models[].ref`, not the current binding's provenance.
With multiple ready choices, each new role must supply an exact `model_ref`;
omission returns a correctable error pointing to `list_models` before context
forking, dispatch or any worker model request. With at most one ready choice,
omission retains default selection. Same-role steering, resume and recovery
retain the concrete binding; an explicit conflict refuses before queuing a
message or changing a generation. Roles remain globally unique in the Registry.
The schema keeps `model_ref` optional for existing-role reuse. This changes the
former default fallback for new roles in multi-model Nuwa projects; no stored
data migration is needed. Ordinary mode and the generic Registry retain their
existing default behavior. Model choice remains the agent's decision.

Every Nuwa worker model call also receives that same current assignment as a
separate runtime System message. It describes the binding already in effect
for that generation, independently of the catalog and shared project data.
Resume and cold recovery use the saved resolved binding, even when the default
or catalog changes. A legacy worker without a recorded binding reports
`model_binding_status: "unknown"`, with no inferred model. The assignment
message does not become history or a fork seed. A tool receipt can still keep
the `current` field from its own call; that historical result does not identify
a later invocation. The assignment leaves project instructions, delegation and
ordinary worker prompts as before.

A delegating agent can call `list_worker_tools` to discover the available worker
tool catalog, including configured MCP tools. Discovery does not mount these
tools on the manager. Workers retain the full optional catalog allowed by their
configuration even though the manager's own tool surface excludes execution.
For a new role, `spawn_agent` accepts an optional `tool_names` array of exact
names. Omit the array to use the available worker tools, or pass `[]` to omit
optional workspace tools for that worker. Worker
lifecycle and shared-work tools remain available as allowed by the current mode.
Put instructions for using those tools in the task. If a dispatch lists an
already mounted shared-work tool in `tool_names`, the refusal explains how to
correct the selection while retaining the task instructions. The project
instruction can leave workspace tool selection to the agent.

Unknown and repeated names, malformed selections and an explicit scope change
for an existing role are refused before dispatch or steering. A descendant can
inherit the selection or choose a subset. The selection controls the tools
mounted on that worker. A selected shell tool retains its normal access.

Agents can inspect existing project-scoped model-call observations through
`query_history`: discover its schema, then query `llm_calls` for recorded
provider/model, agent, tokens and durations. Workers use the history guidance
when the tool is listed in their own tool set. If a task needs history outside
that selection, the worker can report the missing evidence to its delegator.
Recorded calls describe historical observations, not a price or capability rating. An unfinished request has no final usage yet;
stored numeric zeros cannot distinguish unreported usage from an explicit zero.

`fork_context` copies the spawning agent's conversation as historical evidence,
excluding System messages. A child's context therefore comes from its parent.
Closing a subtree records the affected generations and retains
`already_finished` for results that had already ended.

## Shared work

Before each Nuwa manager or worker model call, the engine adds a fresh, bounded
preview of the original turn's project graph. It includes node versions, reported
states, dependencies and source identity. Managers see recent updates first.
Workers prioritize records whose titles or summaries match the task supplied
when their current generation started, then use update time and ID for stable
ordering. Matching uses words and CJK character pairs, not semantic inference;
without a useful match, ordering remains recent first. Full detail is left to
`read_work`. The complete preview, including its explanation and JSON,
uses the existing **tool result character limit**. Oversized nodes are omitted
whole and counted explicitly; they are still available through `read_work`.
Each model call and shared-work read takes its budget from one committed config
snapshot, so live Settings updates do not race or change the cap during encoding.
This bounds the preview, not the entire model context. Provider-internal retries
may reuse one preview.

The preview is untrusted reported data, not instructions, verification or new
human authority. It is added only to the model request, leaving transcript,
compact input and fork history unchanged. A resumed worker keeps its original
configuration and project owner while reading that project's latest graph.
Related records keep their current versions and dependency warnings. Relevance
does not establish truth or successful reuse. Inbox messages do not rewrite the
generation's initial task; resume and restore use the task supplied to that new
execution. The task travels separately from planner instructions and fork history.
Cold recovery retains a known checkpoint assignment separately from its normal
recovery message. Legacy records without a known assignment use recent-first
previews rather than guessing from saved instructions or recovery guidance.
A failed read or changed project membership supplies `status: unavailable`,
without records, a revision or database error text. It does not turn an optional
preview into a new lifetime guard for existing work. Actual shared-work tools
and new claims still validate ownership. Cancellation and budget or encoding
failures refuse the model call; an unavailable preview is never a current-looking
empty graph. Ordinary mode receives no automatic preview.

Open **Collaboration** in the right panel. **Entire project** shows all recorded
work; **This conversation** retains cross-conversation prerequisites so their
paths remain visible. Select a node for detail, sources and correction identity,
or expand the collaboration graph. Solid arrows show recorded work dependencies.
For a record without a visible work edge, dashed arrows show its original turn
and author. These arrows use the saved identity and do not assert that one
result relied on another. Worker rows show durable lifecycle status and
recorded model bindings. Source links open the source conversation and, when a
turn is given, its Trace; results use the existing file API.

When the selected scope has no shared records or workers, Collaboration shows
the open conversation's latest turn status and links to its Trace and Files.
A discussion turn can complete without workers or a recorded graph. A task that
requires operations still assigns those operations to workers.

| kind | status | meaning |
|---|---|---|
| task | `pending`, `running`, `done`, `blocked`, `cancelled` | an agent's reported task state |
| fact | `reported`, `superseded`, `retracted` | a claim and its correction state |
| artifact | `reported`, `superseded`, `retracted` | a reference to an existing workspace file |

On creation, a task defaults to `pending`. Facts and artifacts default to
`reported`.

An evidence reference binds to an existing event in this project by conversation
and event `seq`; an optional turn must match. This checks identity and
availability, not whether the evidence supports the claim. Empty evidence stays
explicitly unverified. Worker lifecycle is a separate projection: a shared task
marked `done` does not finish an engine turn or collect a worker result.

An agent can also pass `own_tool_call_ids` to `record_work` using exact IDs from
its earlier tool replies. The host resolves up to 16 IDs in the current
manager turn or worker execution. A retained worker keeps that execution
scope when a later turn takes over its notifications. The fact keeps its
original author and the evidence points to the turn where the result was
recorded. Recovery starts a fresh execution scope for new tool calls.
Earlier receipts remain referable by explicit `evidence`, and a retry of an
already committed write returns its saved response.

Each ID needs a unique earlier call and its result. A missing, still-uncommitted
or ambiguous receipt refuses the entire write. The two evidence fields are
mutually exclusive, including when either array is empty. On an update, supply
one field to replace evidence, use an empty array to clear it, or omit both to
preserve it.

`read_work` returns bounded summaries, then detail and provenance by `id`.
`offset`/`limit` page list entries or detail runes. JSON remains complete under
the existing tool-result budget; oversized metadata refuses explicitly.

`record_work` creates without `id`/`version`, or patches an existing node using
its current version. Omitted fields remain; `[]` explicitly clears dependencies
or evidence. Creation requires kind/title. An artifact requires an
`artifact_path` that resolves to an existing regular file within the
conversation workspace. Use a workspace-relative or absolute path. A saved path
is not an immutable content receipt.
An unknown update ID is refused with guidance to omit the ID and version for
creation or retrieve an existing ID and version with `read_work` for an update.

The host supplies project, conversation, turn, caller and generation identity.
Corrections preserve the original author and record the actual updating caller.
Node CAS, project revision and full version audit commit together. Stale versions
refuse; reread and reconcile. After current ownership and input-validity checks,
re-delivery of one real tool-call identity with the same arguments returns its
exact saved receipt without another write. Idempotency does not bypass
cancellation, deleted owners/targets or artifact-file validity. Different
arguments under that identity refuse. That receipt may predate a later correction.

Dependencies must exist in the same project without cycles. Writes retain the
source versions on which a result depends. Use exact IDs from `record_work` or
`read_work` for material prerequisites the work actually relied on. Independent
work can omit `depends_on` on creation. Correction, supersession or retraction
marks downstream results **Needs review**. An unrelated patch cannot silently
acknowledge new evidence: reread and explicitly supply the reviewed `depends_on`
list. Deleted prerequisites and unavailable events remain visibly unavailable.
The graph records reasoning and progress; it does not schedule or execute nodes.

## Updates and recovery

`GET /api/projects/:id/nuwa` reads the project snapshot. Its separate SSE stream
at `.../nuwa/events` sends `nuwa_updated` revision invalidations; clients fetch
the snapshot. Revisions are not conversation event sequences. Reconnect sends
the current revision; durable revision sampling repairs missed notifications.
Clients reject old revisions and previous-project responses.

Changing the switch affects subsequent turns. Current work retains its mode;
turning it off does not erase recorded work. `/plan` keeps shared writes
unmounted, including for recovered planning workers.
The manager delegates read-only exploration to workers before proposing a plan.
Accepting that plan keeps execution on workers. Fresh and recovered managers
use the turn's saved Nuwa mode even if the project switch has since changed.
An actual switch change also advances the project revision. The view remains
available if the switch is turned off before the first pending result arrives;
ordinary edits and repeated settings do not advance that revision.
Moving a conversation refreshes the affected Collaboration views. A saved
evidence link is unavailable while its source conversation belongs to another
project. Moving the source back restores the link when the event still exists.
Editing and resending a message refreshes Collaboration when it removes
earlier evidence. Existing references to removed events show **Source
unavailable**. Explicitly deleting a conversation removes the shared work it created
and its version history from every project that still holds it. References
from other conversations stay visible as unavailable.

Checkpoints and worker-spawn events retain the explicit workspace tool selection.
A resumed or restored worker receives that same selection. A missing selected
tool causes recovery to refuse. Earlier records without a selection retain the
existing recovery behavior. Checkpoints also retain parent, generation, model
binding and configuration tag.
The app's tag refers to the original durable turn. Restore validates its
conversation/project and rebuilds the tool surface from saved collaboration,
planning and Web execution mode. In-memory workers retain their tool
configuration across later manager turns. Arbitrary Go tools and credentials
are not serialized; current resources must still be available. Bad tags or
unavailable bindings refuse before claiming/resuming a worker, with no silent
model fallback. Legacy untagged/unbound records keep their compatibility path.

If a conversation move completes before a new turn claims its Registry, the
turn must use the destination project. An in-flight admission with an older
project snapshot refuses and can be retried from the destination.

Startup materializes legacy turn owners using the
[turn ownership migration](DATA_MODEL.md#turns--one-request-and-everything-done-to-answer-it).
Tagged workers and Nuwa turns still refuse recovery after a recorded project
change, including moving a turn's standalone conversation into a project.
The same check applies before a new manager can adopt a parked worker. A
worker's shared reads also check its original turn against current project
membership. When a saved owner conflicts, goal Continue returns the conflict
before opening a new turn. Existing workers and checkpoints remain available
for Stop or for recovery after moving the conversation back.
Cold recovery also keeps an uncollected result with its original project.
Previously collected or discarded results remain in history and are omitted
from a fresh Registry in the destination project.

Stop, collection, generation identity and uncertain external effects retain
their existing contracts. A shared report is not new human authorization.

## Verification boundaries

The implementation reference is [Sapper-v2 at c91881d](https://github.com/N1nEmAn/sapper-v2/tree/c91881dc1629409bbf278577dfdaa6b8f383b438).
Its generic graph hook supplies the work frontier and compact recent history on
user submission; its dispatch renderers refresh the graph and recall related
prior nodes. Eino adapts automatic shared-state visibility to each model-call
entry. The reference's commander scripts separately rely on board/STATE usage
and process recovery; they do not inject the board into every internal call.
This adaptation preserves Eino's enforced planner/worker tool separation and
does not import domain tools, predefined roles or an assumed model ranking.

See [testing](TESTING.md) and the [paired evaluation protocol](../evals/nuwa/README.md).
Offline and scripted HTTP tests establish their particular tool, storage,
identity, replay and UI boundaries. `--mock` validates the harness/API lifecycle;
it cannot establish real-model task quality, speed, token savings or billing.
Unit tests do not establish native macOS acceptance.

Earlier evaluations that allowed the manager to execute operations do not
validate this separation contract. The retained six-row f3 calibration had
official results A 1/2, B 2/2 and C 1/2, with no worker or DS requests. It remains
historical evidence for that source and policy only. New source-boundary tests,
a new identified ELF and image, and new calibration are required before claims
about the separated mode; the formal hard54 cohort has executed zero rows.
See the [calibration boundary](../evals/nuwa/RESULTS.md#legacy-f3-calibration-2026-11-06).

CyberGym execution was stopped by platform safety review. There is no CyberGym
score or comparative performance result, and that requirement remains incomplete.

Related boundaries: `F-136`, `C-037`, `C-019`, `C-027`. See [API](API.md),
[data model](DATA_MODEL.md) and [library](LIBRARY.md) for integration details.
