# Nuwa project collaboration

Nuwa lets agents choose their plan, division of work and configured models while
keeping shared tasks, reported facts and results inspectable. Enable **Nuwa
collaboration** when creating or editing a project, then put the objective,
constraints and acceptance criteria in **Instruction**. Existing projects default
to ordinary mode. No domain workflow, predefined role list or fixed hierarchy is
required.

Connections, credentials and tools remain platform resources configured in
Settings. An instruction can explain how to use them; it cannot connect an
absent service, grant permission or supply missing data. The workspace remains
an anchor with the agent's normal access.

## Planning, models and delegation

The conversation model remains the manager. Nuwa managers and workers receive
`list_models`, `read_work` and, outside planning mode, `record_work`. Workers can
delegate again. All descendants share one Registry, concurrency budget, Stop
and restart lifecycle. A worker may wait for, resume or close its own
descendants; lifecycle control outside its subtree refuses. Messages retain
their existing peer/manager semantics.

`list_models` returns configured, ready choices with exact `ref`, provider/model,
label and known context window. Names are resources, not capability ratings.
Copy its opaque ref into `spawn_agent(..., model_ref: ref)` without constructing
or decoding it. This tool token is separate from the UI/CLI provider ChoiceID.
Omitting it for a new
worker resolves that worker's default. Same-role steering, resume and recovery
retain the concrete binding; an explicit conflict refuses before queuing a
message or changing a generation. Roles remain globally unique in the Registry.

`fork_context` copies the spawning agent's conversation as historical evidence,
excluding System messages. A child's context therefore comes from its parent.
Closing a subtree records the affected generations and retains
`already_finished` for results that had already ended.

## Shared work

Open **Collaboration** in the right panel. **Entire project** shows all recorded
work; **This conversation** retains cross-conversation prerequisites so their
paths remain visible. Select a node for detail, sources and correction identity,
or expand the dependency graph. Worker rows show durable lifecycle status and
recorded model bindings. Source links open the source conversation and, when a
turn is given, its Trace; results use the existing file API.

| kind | status | meaning |
|---|---|---|
| task | `pending`, `running`, `done`, `blocked`, `cancelled` | an agent's reported task state |
| fact | `reported`, `superseded`, `retracted` | a claim and its correction state |
| artifact | `reported`, `superseded`, `retracted` | a reference to an existing workspace file |

An evidence reference binds to an existing event in this project by conversation
and event `seq`; an optional turn must match. This checks identity and
availability, not whether the evidence supports the claim. Empty evidence stays
explicitly unverified. Worker lifecycle is a separate projection: a shared task
marked `done` does not finish an engine turn or collect a worker result.

`read_work` returns bounded summaries, then detail and provenance by `id`.
`offset`/`limit` page list entries or detail runes. JSON remains complete under
the existing tool-result budget; oversized metadata refuses explicitly.

`record_work` creates without `id`/`version`, or patches an existing node using
its current version. Omitted fields remain; `[]` explicitly clears dependencies
or evidence. Creation requires kind/title. Artifacts must name existing regular
files within the workspace. A saved path is not an immutable content receipt.

The host supplies project, conversation, turn, caller and generation identity.
Corrections preserve the original author and record the actual updating caller.
Node CAS, project revision and full version audit commit together. Stale versions
refuse; reread and reconcile. After current ownership and input-validity checks,
re-delivery of one real tool-call identity with the same arguments returns its
exact saved receipt without another write. Idempotency does not bypass
cancellation, deleted owners/targets or artifact-file validity. Different
arguments under that identity refuse. That receipt may predate a later correction.

Dependencies must exist in the same project without cycles. Writes retain the
source versions on which a result depends. Correction, supersession or retraction
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
An actual switch change also advances the project revision. The view remains
available if the switch is turned off before the first pending result arrives;
ordinary edits and repeated settings do not advance that revision.

Checkpoints retain parent, generation, model binding and configuration tag.
The app's tag refers to the original durable turn. Restore validates its
conversation/project and rebuilds the tool surface from saved collaboration,
planning and Web execution mode. In-memory workers retain their tool
configuration across later manager turns. Arbitrary Go tools and credentials
are not serialized; current resources must still be available. Bad tags or
unavailable bindings refuse before claiming/resuming a worker, with no silent
model fallback. Legacy untagged/unbound records keep their compatibility path.

Stop, collection, generation identity and uncertain external effects retain
their existing contracts. A shared report is not new human authorization.

## Verification boundaries

See [testing](TESTING.md) and the [paired evaluation protocol](../evals/nuwa/README.md).
Offline and scripted HTTP tests establish their particular tool, storage,
identity, replay and UI boundaries. `--mock` validates the harness/API lifecycle;
it cannot establish real-model task quality, speed, token savings or billing.
Unit tests do not establish native macOS acceptance.

CyberGym execution was stopped by platform safety review. There is no CyberGym
score or comparative performance result, and that requirement remains incomplete.

Related boundaries: `F-136`, `C-037`, `C-019`, `C-027`. See [API](API.md),
[data model](DATA_MODEL.md) and [library](LIBRARY.md) for integration details.
