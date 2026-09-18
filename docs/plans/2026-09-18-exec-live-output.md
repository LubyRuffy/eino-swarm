# Live exec output Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Stream exec stdout/stderr into the pending tool row in real time.

**Architecture:** eino-tools tees process output through a context listener. Swarm emits broadcast-only `tool_delta` events (full accumulated JSON, coalesced, keyed by `tool_call_id`). The model still gets one JSON `tool_result`. Web/TUI paint the pending row and treat `\r` as overwrite.

**Tech Stack:** Go, eino ADK tool middleware, EventSource, React/vitest, bubbletea TUI.

---

### Task 1: eino-tools OutputListener

**Files:**
- Create: `exec/output.go` (in eino-tools worktree)
- Modify: `exec/tool.go` (`runCommandOnce` stdout/stderr writers)
- Test: `exec/output_test.go`
- Docs: eino-tools `CHANGELOG.md`, `docs/TESTING.md`, `ARCHITECTURE.md`

**Step 1:** Write `TestExecuteReportsStdoutChunksBeforeTheCommandFinishes` in eino-tools. It must fail because output is still held until `Wait`.

**Step 2:** Run `go test ./exec -count=1 -run TestExecuteReportsStdoutChunksBeforeTheCommandFinishes`.

**Step 3:** Add `WithOutputListener` + tee writers. Copy chunks before handing them to the listener (the pipe buffer is reused).

**Step 4:** Re-run the test. Add stderr + "no listener still works" cases.

**Step 5:** Commit in eino-tools.

---

### Task 2: NotifyToolDelta + binder hook

**Files:**
- Modify: `signal.go`, `run_test.go`, `swarm.go`, `worker.go`
- Create: `internal/tools/execstream.go`, `internal/tools/execstream_test.go`
- Modify: `internal/engine/turn.go` (`newTurnRegistry`)

**Step 1:** Failing tests: kind round-trip includes `tool_delta`; `WrapInvokableToolCall` emits `NotifyToolDelta` when `ToolOutputBinder` fires; `BindExecOutput` forwards real exec chunks.

**Step 2:** Implement kind, wrap on manager + Injector, binder.

**Step 3:** Commit.

---

### Task 3: Accumulator coalesce by tool_call_id

**Files:**
- Modify: `internal/engine/coalesce.go`, `internal/engine/accumulator.go`
- Test: `internal/engine/coalesce_test.go`

**Step 1:** Failing tests: two parallel execs do not overwrite each other's live snapshots; `tool_result` drops the held `tool_delta` rather than persisting it.

**Step 2:** `liveKey(agent, kind, toolCallID)`; `NotifyToolDelta` → `pushLive`; `NotifyToolResult` → drop that key.

**Step 3:** Commit.

---

### Task 4: Frontend + TUI

**Files:**
- Create: `frontend/src/lib/carriage.ts`, `frontend/src/lib/carriage.test.ts`
- Modify: `frontend/src/lib/types.ts`, `stream.ts`, `transcript.ts`, `transcript.test.ts`, `tool-result.tsx`, `tool-result.test.tsx`, `transcript.tsx`
- Modify: `internal/tui/tui.go`, `internal/tui/toolview.go`, tests

**Step 1:** Failing tests: `tool_delta` fills pending result without clearing pending; two calls keyed separately in `collapseLiveEvents`; `\r` overwrites; pending exec opens; TUI updates `toolRes` while open.

**Step 2:** Implement. Split `transcript.ts` if it would exceed 1000 lines.

**Step 3:** Commit.

---

### Task 5: Docs + wire stability

**Files:** `docs/API.md`, `docs/LIBRARY.md`, `docs/DATA_MODEL.md`, `docs/TESTING.md`, `ARCHITECTURE.md`, `CHANGELOG.md`, `internal/server/server_test.go`

**Step 1:** `TestNotifyKindsAreStableAcrossTheWire` includes `tool_delta`.

**Step 2:** Document broadcast-only semantics. Bump `eino-tools` in `go.mod` after tools is on origin.

**Step 3:** `go test -race ./...`, frontend lint/test, `make frontend`.

**Step 4:** Review gate, integrate to main, push, close #1.
