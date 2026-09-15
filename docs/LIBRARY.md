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
communication channel**. This package fills that gap with four lifecycle tools
over one concurrency-safe registry:

| tool | semantics |
|---|---|
| `spawn_agent(role, task, fork_context)` | start a sub-agent in the background; returns `{"agent_id": …}` immediately. `fork_context: true` replays the manager's conversation into it. |
| `send_message(agent_id, text)` | steer a running agent; delivered at its **next turn boundary** via a `ChatModelAgentMiddleware` |
| `wait_agents(agent_ids, timeout_s)` | block until all listed agents finish; returns their results as JSON |
| `close_agent(agent_id)` | cancel a running agent |

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
    case swarm.NotifySpawned:      // a sub-agent started (n.Role, n.AgentID)
    case swarm.NotifyDone:         // run finished; n.Text is the final answer
    case swarm.NotifyError:        // fatal; n.Err
    }
})
```

`Run` wires the manager agent, the four lifecycle tools, the `fork_context`
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

Steering the manager itself (not just a worker) is `reg.SteerManager(text)`;
it is injected before the manager's next model call. Anything queued but never
read is recoverable with `reg.TakePendingSteers()`, so a message typed a moment
before the run ended is not silently lost.

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
| `NotifySpawned` | the sub-agent's role (`AgentID` is its id) |
| `NotifyFinished` | its result; `Err` set when it failed |
| `NotifyToolCall` | `name(args)` |
| `NotifyToolResult` | the (truncated) result |
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
}

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

## Guarantees

- Steering never interrupts an in-flight model call or tool execution; messages
  land at the next turn boundary (the same semantics as Codex steering).
- `send_message` to a finished agent returns `{"delivered": false}` rather than an
  error: the caller lost a race, it did not make a mistake.
- Sub-agents never outlive their lineage, even when nobody calls `close_agent`:

| mechanism | what it catches |
|---|---|
| context lineage | an agent's context derives from its spawner's, so a dead host cancels everything it spawned. No orphan goroutines. |
| `AgentTimeout` (default 10m) | a hung model or endpoint; the handle's error records the timeout |
| `MaxTurns` (default 20) | a model looping forever; ends with eino's `ErrExceedMaxIterations` |
| `Registry.Close` | cancels everything and rejects further spawns; safe even for an agent whose cancel was not yet wired, because contexts are created synchronously inside `Spawn` |
| `Registry.Cleanup` | kills whatever is still running at the end of a turn and reports how many |
| bounded registry | finished handles are pruned, so a long session cannot grow the map without bound |

Each of those has a test: `TestCallerContextCancelReleasesAgents`,
`TestWatchdogTimeoutReleasesAgent`, `TestMaxTurnsEndsBrokenModel`,
`TestRegistryClose`, `TestForkContextInheritsHistory`,
`TestWaitAndCleanupEndATurn`. Run them with `-race`.

## No pre-registration

`spawn_agent(role, task, fork_context)` takes the role and task the model invents
at runtime and `ModelBuilder` constructs the agent on the fly — matching Codex's
`spawn_agent(task_name, message)`. `fork_context: true` replays the manager's
conversation (kept fresh by `Registry.ManagerMiddleware()` on the manager's
`Handlers`) into the new agent, the `fork_turns` equivalent.

## Examples

`examples/` builds against the library:

| example | shows |
|---|---|
| `codex-lite-min` | the smallest complete run |
| `codex-lite` | a fuller manager with tools |
| `swarm-real` | a real endpoint, real tools |
| `swarmwatch` | consuming the notification stream |

The terminal renderer is not an example: it is `internal/tui`, reachable as
`zwai tui`.

```bash
go test -race ./...
```
