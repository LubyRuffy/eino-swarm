# Live exec output

> Product decisions are locked. This is the implementation design.

**Goal:** While `exec` is running, Web and TUI show stdout/stderr as it arrives. The model still receives one complete JSON `tool_result`.

## Why

`exec` buffers until `Wait`. The pending tool row only says "running…". A long command looks dead. Issue #1.

## Contract

| Surface | Behaviour |
|---|---|
| Wire | `tool_delta` — accumulated `{stdout,stderr}` JSON, `tool_call_id`, seq 0, **not stored** |
| Cap | same 64k runes as `tool_result` |
| Coalesce | existing `delta_coalesce_ms`; `liveKey` includes `tool_call_id` |
| Model | unchanged: one JSON result after exit |
| Timeout | unchanged (10s default, 300s cap) |
| Scope | `exec` only |

## Layers

1. **eino-tools:** `WithOutputListener(ctx, fn)` tees stdout/stderr chunks while the process runs.
2. **swarm:** `Registry.ToolOutputBinder` runs in manager *and* worker `WrapInvokableToolCall`. Emits `NotifyToolDelta` via the existing sink.
3. **engine:** binder = `tools.BindExecOutput`. Accumulator `pushLive`s `tool_delta`, drops it on `tool_result`.
4. **UI:** pending exec auto-opens; live JSON renders as stdout/stderr; `\r` overwrites the current line.

## Out of scope

`python_runner`, timeout changes, PTY / terminal panel, persisting deltas.
