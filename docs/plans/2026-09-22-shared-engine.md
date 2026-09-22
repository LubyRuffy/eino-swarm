# Shared engine implementation plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** One engine per data directory, shared by desktop, web, TUI, and the phone, outliving the shell that started it.

**Architecture:** `zwai engine` holds `engine.lock` and serves the existing `App`. Other shells discover `engine.json`, attach over loopback HTTP, and hold a presence connection. Idle exit is a pure function of clients, the phone socket, and running turns. Design: `docs/plans/2026-09-22-shared-engine-design.md`.

**Tech Stack:** Go, flock, gin, bubbletea, React.

---

### Task 1: Lease

**Files:**
- Create: `internal/lease/lease.go`
- Create: `internal/lease/lease_unix.go`
- Create: `internal/lease/lease_windows.go`
- Create: `internal/lease/idle.go`
- Test: `internal/lease/lease_test.go`
- Test: `internal/lease/idle_test.go`

**Step 1:** Failing tests — second process does not get the lock; dead pid is not a live engine; mock mismatch; idle table.

**Step 2:** Run `go test -race -count=1 ./internal/lease/` and see it fail to compile.

**Step 3:** Implement acquire / publish / discover / idle.

**Step 4:** Tests pass.

### Task 2: `zwai engine`

**Files:**
- Create: `cmd/zwai/engine.go`
- Modify: `cmd/zwai/main.go`
- Test: `cmd/zwai/engine_test.go`

Spawn or attach. In tests, start in-process instead of exec. Production spawn uses a new session. Write `engine.json` only after `Listen`. Remove it and the lock on shutdown.

### Task 3: Presence and meta

**Files:**
- Modify: `internal/server/server.go`
- Create: `internal/server/presence.go`
- Test: `internal/server/presence_test.go`
- Modify: `internal/app/app.go`

`POST /api/presence`, `GET /api/presence/:id` held until disconnect. `clients` on `GET /api/meta`. Engine `ServeUntilIdle` uses the idle function plus `Engine.Running` and phone `Status().Online`.

### Task 4: Desktop and web attach

**Files:**
- Modify: `cmd/zwai/serve.go`
- Modify: `internal/desktop/desktop.go` only if shutdown must be optional
- Test: `cmd/zwai/cli_test.go`

Desktop opens the existing URL and does not shut the engine down on window close. Web prints the existing URL. Ctrl-C stops the waiter only.

### Task 5: TUI HTTP client

**Files:**
- Create: `internal/tui/client.go`
- Modify: `cmd/zwai/tui.go`
- Test: `internal/tui/client_test.go`
- Modify: `cmd/zwai/cli_test.go` (`TestTUIUsesAScratchWorkspace` becomes a persisted thread)

List, create, log, SSE into the existing blocks, `POST /turns`, answers, presence. `--task` leaves a row. No scratch dir.

### Task 6: Title bar

**Files:**
- Modify: `frontend/src/lib/types.ts`
- Modify: `frontend/src/components/app/header.tsx`
- Modify: `frontend/src/lib/messages-en.ts`
- Modify: `frontend/src/lib/messages-zh.ts`
- Test: `frontend/src/components/app/header.test.tsx`

Two surfaces are named. The composer still submits.

### Task 7: Docs

Update `ARCHITECTURE.md`, `docs/CLI.md`, `docs/API.md`, `docs/DATA_MODEL.md`, `CHANGELOG.md`, and the `cmd/zwai` usage text. Fix the "every shell shares the database" comment so it describes the engine process, not every shell binary opening SQLite.
