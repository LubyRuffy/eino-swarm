# The swarm library

The repository root is an importable Go package: Codex-style multi-agent
orchestration for [Eino](https://github.com/cloudwego/eino) ADK — asynchronous
sub-agent spawning, mid-flight steering, wait/collect, and cancellation. The zwai
app is one consumer of it; your program can be another.

```go
import swarm "github.com/LubyRuffy/eino-swarm"
```

`adk.NewAgentTool` runs sub-agents in parallel (when the host model issues several
tool calls in one message), but it is **synchronous** and gives sub-agents **no
communication channel**. This package fills that gap with five lifecycle tools
over one concurrency-safe registry:

| tool | semantics |
|---|---|
| `spawn_agent(role, task, fork_context)` | start a sub-agent in the background; returns `{"agent_id": …}` immediately. **One worker per role:** a later call with the same role while it is running queues the new task (`steered`) instead of minting a twin; if it already finished, continues that same id (`resumed_from`). `fork_context: true` only applies when this role has no worker yet, and then replays **this manager conversation** into it. |
| `send_message(agent_id, text)` | steer a **running** agent, or the host when `agent_id` is `manager`. Queued for the target's **next turn boundary**. `agent_id` is the id `spawn_agent` returned, that worker's role, or `manager`. `delivered: true` means queued. A finished or unknown target returns `delivered: false` with `notified: manager` when a **worker** sent it — the host gets the text and should take the next step. A Go error here is a `NodeRunError` that kills the caller. |
| `wait_agents(agent_ids, timeout_s)` | return as soon as the next listed agent reaches a final status (or the timeout); reports every agent's status (`running`/`done`/`failed`), the finished ones' results, leftover steering that never reached a model call (`undelivered`), and the running ones' last activity, plus `timed_out`. It hands control back per-finish so the manager can report progress and wait again, instead of dead-waiting on the whole batch |
| `close_agent(agent_id)` | cancel a running agent |
| `resume_agent(agent_id, task)` | continue a finished or failed worker **in place** under the same `agent_id`, seeded with that worker's conversation. Returns `{"agent_id": …, "resumed_from": …}` with the same id. Rejects a still-running id (`send_message` instead). Survives `Stats()` pruning the live handle. Do not `spawn_agent` a second worker with the same role to replace one that failed. |

Give sub-agents `SendTool()` as well and any agent can message any other (a mesh
rather than a star).

## Minimal use

```go
shared, _ := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{ /* … */ })

reg := swarm.NewRegistry()
reg.ModelBuilder = func(role, agentID string) model.BaseChatModel { return shared }

final, err := reg.Run(ctx, task, func(n swarm.Notification) {
    switch n.Kind {
    case swarm.NotifyAgentMessage: // manager or worker completed a message
    case swarm.NotifySpawned:      // a sub-agent started (n.Role, n.AgentID, n.Text=instruction)
    case swarm.NotifyDone:         // run finished; n.Text is the final answer
    case swarm.NotifyError:        // fatal; n.Err
    }
})
```

`Run` wires the manager agent, the five lifecycle tools, the `fork_context`
middleware and signal handling (SIGINT/SIGTERM → context cancel → every agent
cancelled).

## Multi-turn conversations

`Run` is one-shot. For a conversation that continues, use `RunWith`, which takes
prior messages and hands back the transcript to store:

```go
res, err := reg.RunWith(ctx, swarm.RunConfig{
    Instruction:   systemPrompt,
    Messages:      priorMessages, // what the model should already know
    ManagerTools:  tools,
    MaxIterations: 32,
}, swarm.Callback(onNotify))

// res.Final     the answer
// res.Transcript everything to persist and replay into the next turn
```

`RunWith` does **not** install signal handling — that belongs to a `main`, not to
a library call inside a server. This is what zwai's engine uses.
Hitting `MaxIterations` still returns the transcript **including tool results
from that last round**, so the next `RunWith` can continue instead of repeating
the same calls. `SetHistory` keeps wrap-appended tool results that a later
model-step snapshot would otherwise drop.

`RunConfig.ManagerMiddlewares` are appended after the swarm's history recorder
and steering injector. eino's `adk/middlewares/summarization` is fine on a
manager that uses `spawn_agent` **if** Finalize keeps the in-flight ReAct
tail and rehydrates worker ids from `spawned`/`finished` (or
`RestoreWorkers` / `FinishedWorkers`). `DefaultFinalize` does neither.
zwai's auto-compact uses eino's Generate with a task-agnostic
`UserInstruction` and that custom Finalize. The keep-or-delete scorecard is
`TestCompactEffectComparedWithEinoDefault` — structure, not briefing prose.

A host that is restarting an unfinished run can put leftover workers on
`RunConfig.RestoreWorkers` / `FinishedWorkers`. They are applied after the
spawn hook is installed, so the roster sees the same `agent_id`s come back.
`Registry.Restore` and `Registry.PlantFinished` are the same operations for a
host that is not going through `RunWith`.

Steering the manager itself (not just a worker) is `reg.SteerManager(text)`.
A pasted image rides with `reg.SteerManagerMessage(msg)` so the inbox holds
`*schema.Message`, not bare strings. Put the caption in
`UserInputMultiContent` only; OpenAI cannot marshal `Content` on the same
message. `SteerManager` and `SteerManagerMessage` both land before the
manager's next model call. Anything queued but never read is recoverable with
`reg.TakePendingSteers()` (captions) or `reg.TakePendingSteerMessages()` (the
messages themselves, images included), so a send that arrived a moment before
the run ended is not silently lost.

## Notifications

```go
type Notification struct {
    Kind       NotifyKind
    AgentID    string
    Role       string
    Text       string
    Err        error
    ToolCallID string
}
```

| kind | `Text` |
|---|---|
| `NotifyAgentMessage` | a completed assistant message |
| `NotifySpawned` | the worker's system prompt (`Role` is the role, `AgentID` is its id). Older hosts may still send the role in `Text` |
| `NotifyFinished` | its result; `Err` set when it failed |
| `NotifyToolCall` | `name(args)` |
| `NotifyToolResult` | the tool's stdout, **newlines kept**, clipped at 64k runes so a huge `exec` cannot blow up the event log |
| `NotifyTurn` | `turn N` |
| `NotifyDelta` | the streamed answer **so far** |
| `NotifyReasoningDelta` | the streamed reasoning **so far** |
| `NotifyDone` | the final answer |
| `NotifyError` | fatal failure |

Two properties a consumer depends on:

- **Deltas carry the full text so far, not a fragment.** Replace your buffer with
  it. A dropped delta then costs nothing.
- **`ToolCallID` pairs a call with its result.** Agents issue several tool calls
  in one message and they return out of order; matching by name or by arrival
  order produces the wrong pairing exactly when a run gets interesting.

`NotifyKind.String()` and `ParseNotifyKind` round-trip the names, so they can be
stored and read back.

## Dropping to the primitives

```go
reg := &swarm.Registry{
    MaxConcurrent: 8,
    ModelBuilder: func(role, agentID string) model.BaseChatModel { return shared },
    SubAgentTools: []tool.BaseTool{searchTool, fetchTool},
    // Optional. Prepended to every sub-agent's Instruction so workers know
    // facts they cannot see in the manager prompt (OS, shell, date).
    WorkerPreamble: hostEnv,
}
```

manager, _ := adk.NewChatModelAgent(ctx, reg.ManagerConfig(
    "manager", "swarm manager", managerModel,
    swarm.WithInstruction(managerPrompt),
    swarm.WithMaxIterations(32),
    swarm.WithManagerTool(readFileTool),
    swarm.WithManagerHandler(myMiddleware),
))
```

`Spawn` also works with no LLM in the loop:

```go
h, _ := reg.Spawn(ctx, "researcher", "find foobar", modelBuilder, extraTools...)
<-h.Done()
result, err, _ := h.Result()
```

## Reporting progress

A host that wants to show what a run is doing while it is doing it reads
`Registry.Progress()`: one read-only row per tracked sub-agent, ordered by spawn
time, with its role, whether it is still running, how long it has been at it, its
last streamed tail (`Activity`) and, if it failed, why.

```go
for _, a := range reg.Progress() {
    fmt.Printf("%s %s %s %s\n", a.AgentID, a.Role, a.Elapsed, a.Activity)
}
```

**Poll `Progress`, never `Stats`.** `Stats` prunes the finished agents it counts
so that a long-lived registry cannot grow without bound — which means a poll
landing between a sub-agent finishing and `wait_agents` collecting it turns a
completed agent into an unknown one, and the manager loses the result it just
paid for (`TestPollingProgressKeepsAFinishedAgentsResult`).

## Guarantees

- Steering never interrupts an in-flight model call or tool execution; messages
  land at the next turn boundary (the same semantics as Codex steering).
- `send_message` to a finished or unknown agent returns `{"delivered": false}`
  rather than an error. When a **worker** sends that miss, the host manager is
  notified with the text (`notified: manager`) so the worker can finish with a
  final answer instead of inventing another id. `send_message(manager, …)`
  delivers to the host the same way. `delivered: true` means the text was
  queued for that agent. If a running agent finishes before another model call,
  `wait_agents` reports leftover steering as `undelivered`. `agent_id` may be
  the worker's role; an invented suffix is not resolved by similarity.
- `resume_agent` continues a finished worker's conversation on the **same** id.
  `fork_context` is the manager's conversation, not a previous worker's findings.
  A still-running worker is steered with `send_message`, not resumed.
- `Restore` / `RunConfig.RestoreWorkers` restarts a worker that was still
  running when the previous process died, under the same id. `PlantFinished`
  puts an already-completed worker back so `wait_agents` does not report
  unknown after a restart.
- Sub-agents never outlive Close/Cleanup/Handle.Cancel, even when nobody calls `close_agent`:

| mechanism | what it catches |
|---|---|
| Spawn context | **not** the worker lifetime. Cancelling the manager (a `/goal` session yield) must not kill in-flight sub-agents. |
| `AgentTimeout` (default 10m) | a hung model or endpoint; the handle's error records the timeout |
| `MaxTurns` (default 20) | a model looping forever; ends with eino's `ErrExceedMaxIterations` |
| `ManagerMaxIterations` | the manager's ReAct cap when `RunConfig.MaxIterations` is unset; `<=0` keeps eino's own default |
| `Registry.Close` | cancels everything and rejects further spawns; safe even for an agent whose cancel was not yet wired, because contexts are created synchronously inside `Spawn` |
| `Registry.Cleanup` | kills whatever is still running at the end of a turn and reports how many |
| bounded registry | finished handles are pruned by `Stats`, so a long session cannot grow the map without bound. Use `Progress` for reporting: it never prunes. Finished conversations stay in a separate archive (default 32) for `resume_agent`. |

Each of those has a test: `TestCallerContextCancelDoesNotReleaseAgents`,
`TestWorkerSurvivesParentContextCancel`,
`TestWatchdogTimeoutReleasesAgent`, `TestMaxTurnsEndsBrokenModel`,
`TestRegistryClose`, `TestForkContextInheritsHistory`,
`TestForkContextSeedsBeforeFirstModelCall`,
`TestWaitAndCleanupEndATurn`, `TestResumeSeesFinishedWorkersHistory`,
`TestResumeAfterFailureKeepsTheSameID`,
`TestResumeStillWorksAfterStatsPrune`,
`TestSpawnAgentReusesAFinishedWorkerWithTheSameRole`,
`TestSpawnAgentSteersARunningWorkerWithTheSameRole`,
`TestUndeliveredSteerSurfacesWhenTheAgentFinishes`. Run them with `-race`.

## No pre-registration

`spawn_agent(role, task, fork_context)` takes the role and task the model invents
at runtime and `ModelBuilder` constructs the agent on the fly — matching Codex's
`spawn_agent(task_name, message)`. A later `spawn_agent` with the same role does
not mint a twin: a running worker is steered with the new task; a finished one
continues in place. `fork_context: true` replays the manager's conversation (kept
fresh by `Registry.ManagerMiddleware()` on the manager's `Handlers`) into a
worker that does not exist yet, the `fork_turns` equivalent. Continuing a
**specific** leftover id is `resume_agent(agent_id, task)`. The roster identity
is the id. Inbox seed is applied **before** the worker goroutine starts, so the
first model call cannot lose the race against `fork_context` / resume.

## Examples

`examples/` builds against the library:

| example | shows |
|---|---|
| `codex-lite-min` | the smallest complete run |
| `codex-lite` | a fuller manager with tools |
| `swarm-real` | a real endpoint, real tools |
| `swarmwatch` | consuming the notification stream |

The terminal renderer is not an example: it is `internal/tui`, reachable as
`zwai tui` (interactive) or `zwai tui --task "…"` (one-shot).

```bash
go test -race ./...
```
