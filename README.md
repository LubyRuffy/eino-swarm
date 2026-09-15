# eino-swarm

Codex-style multi-agent orchestration for [Eino](https://github.com/cloudwego/eino) ADK:
asynchronous sub-agent spawning, mid-flight steering, wait/collect, and cancellation.

`adk.NewAgentTool` runs sub-agents in parallel (when the host model issues multiple
tool calls in one message), but it is **synchronous** and gives sub-agents **no
communication channel**. This package fills the gap with four lifecycle tools backed
by one shared, concurrency-safe registry:

| tool | semantics |
|---|---|
| `spawn_agent(role, task)` | start a sub-agent in the background; returns `{"agent_id": ...}` immediately |
| `send_message(agent_id, text)` | steer a running agent; delivered at its **next turn boundary** via a `ChatModelAgentMiddleware` |
| `wait_agents(agent_ids, timeout_s)` | block until all listed agents finish; returns JSON results |
| `close_agent(agent_id)` | cancel a running agent |

Give sub-agents the `SendTool()` too and any agent can message any other (mesh).

## Usage

Minimal (the whole product surface — Registry + one Run + one callback):

```go
shared, _ := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{...})

reg := swarm.NewRegistry()
reg.ModelBuilder = func(role, agentID string) model.BaseChatModel { return shared }

final, err := reg.Run(ctx, task, func(n swarm.Notification) {
    switch n.Kind {
    case swarm.NotifyAgentMessage: // manager or worker spoke
    case swarm.NotifyDone:         // run finished (n.Text = final answer)
    case swarm.NotifyError:        // fatal (n.Err)
    }
})
```

`Run` wires the manager agent, the four lifecycle tools, fork_context
middleware, and Ctrl+C (SIGINT/SIGTERM -> context cancel -> every agent
cancelled) internally. Callbacks only ever see product-grade events:
`NotifyAgentMessage / NotifySpawned(规划中) / NotifyDone / NotifyError`.

For custom control, drop to the primitives:

```go
reg := &swarm.Registry{
    MaxConcurrent: 8,
    ModelBuilder: func(role, agentID string) model.BaseChatModel {
        return sharedOpenAIModel
    },
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

`Spawn` can also be called directly from Go code (no LLM in the loop):

```go
h, _ := reg.Spawn(ctx, "researcher", "find foobar", modelBuilder, extraTools...)
<-h.Done()
result, err, _ := h.Result()
```

## Guarantees / notes

- Steering never interrupts an in-flight model call or tool execution; messages
  land at the next turn boundary (same semantics as Codex steering).
- `send_message` to a finished agent returns `{"delivered": false}` instead of erroring.

## Resource release (caller failure paths)

Sub-agents never outlive their lineage, even when nobody calls `close_agent`:

1. **Context lineage** — an agent's run context derives from the `Spawn`
   caller's context. If the host dies (model call failed, SIGINT, client
   disconnect), all agents it spawned are canceled by Go's context
   propagation. No orphan goroutines.
2. **Watchdog** — `Registry.AgentTimeout` (default 10m) hard-caps every
   agent's lifetime; a hung model/endpoint is force-terminated and the
   handle's error records `exceeded timeout`.
3. **MaxTurns** — caps ReAct iterations (default 20); a looping/broken model
   ends with eino's `ErrExceedMaxIterations` instead of spinning forever.
4. **Registry.Close** (`defer reg.Close()`) — cancels everything and rejects
   further spawns; safe even against agents whose cancel was not yet wired
   (contexts are created synchronously inside `Spawn`).
5. **Bounded registry** — finished handles are pruned by `Stats()`; long-lived
   sessions cannot grow the map without bound.

These paths are all covered by tests: `TestCallerContextCancelReleasesAgents`,
`TestWatchdogTimeoutReleases`, `TestMaxTurnsEndsBrokenModel`,
`TestRegistryClose`, `TestForkContextInheritsHistory` — run with `-race`.

## Dynamic creation (no pre-registration)

`spawn_agent(role, task, fork_context)` takes the role/task the model
invents at runtime; `ModelBuilder` constructs the agent on the fly. No
pre-registration, matching Codex `spawn_agent(task_name, message)`.
`fork_context: true` replays the manager's conversation (kept fresh by
`Registry.ManagerMiddleware()` on the manager's `Handlers`) into the spawned
agent — the `fork_turns` equivalent.

## Test

```
go test -race ./...
```
