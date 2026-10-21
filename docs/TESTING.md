# Testing

## UI message catalogs

Worker failure translations live in `frontend/src/lib/messages-agent-failure-en.ts`
and `messages-agent-failure-zh.ts`; the main `messages-en.ts` and `messages-zh.ts`
catalogs compose them without changing message keys or values. `i18n.test.ts`
checks catalog key parity, while `agent-failure.test.tsx` checks translated
failure details and their recovery-settings links. Run them with
`cd frontend && npm test -- src/lib/i18n.test.ts src/components/app/agent-failure.test.tsx`.

## Automatic restart handoff (F-110 / C-027)

`handoff_test.go` and `internal/engine/restart_test.go` cover shared deadlines,
worker/tool admission, structured histories and unread inboxes, real completion
versus handoff, watchdog budgets, active/parked workers, human pauses and
checkpoint failures. `internal/engine/restart_parked_test.go` proves actual
worker execution after restart while the manager remains completed and its
future wake stays unchanged. `internal/remote/restart_test.go` prevents private
checkpoint bodies leaving the PC. `internal/server/restart_http_test.go` covers idempotent
HTTP acknowledgement, readable status, same-origin refusal and admission errors.
`TestRestartLateWorkerToolResultStaysUnknownAfterRegistryClose` keeps late
cancelled tool results unknown even after the registry has closed;
`TestRestartLateUnknownReceiptKeepsItsCallIdentity` replays both unknown and
confirmed late receipts for managers and workers without losing call pairing.
`TestRestartCannotCancelATurnBetweenItsFinishChecks` gates the real manager's
outcome read while `PrepareRestart` takes ownership; it prevents restart from
turning completed work into a human cancellation.
`internal/engine/restart_finish_ownership_test.go` exercises restart at the
normal completion/worker parking boundary and while terminal persistence is
blocked after idle closes. It also checks deadline failure and retry without
partial runtime abandonment; readiness cannot precede durable finalization.
`TestRestartRejectsAnUnstoredNormalCompletion` injects terminal-event and turn
status write failures into a real completed run. Replacement must report the
missing durable completion instead of accepting an idle runtime as ready.
`internal/engine/restart_answer_replay_test.go` sends real answers before the
waiter arms and across a new question, then checks cold-replayed tool results
and model input. `internal/engine/restart_inbox_replay_test.go` checks unread
messages, distinct steering sequences and peer roles survive exactly once.
`internal/app/restart_parked_failure_test.go` opens real saved checkpoints with
a missing provider or exhausted watchdog; startup must report failure without
inventing work, changing the completed manager or advancing its future wake.
`frontend/src/lib/transcript-stop.test.ts`,
`frontend/src/components/app/panel-stop.test.tsx` and the restart UI E2E cover
cleanup/interruption, stopped grouping, terminal ordering and persisted replay.
The stop reducer regressions deliver cancellation and successful `finished`
events after `cleanup` or `interrupted`, including late `agent_message`,
`delta`, `reasoning_delta`, `tool_call` and `tool_call_delta` immediately before
completion. Stopped status, activity and stop time remain unchanged in live,
full-history and roster replay. Explicit `spawned` resume then allows new output
and `finished` to complete normally; existing parked-worker live-output recovery
is retained. `transcript-goal.test.ts` models that legacy derived `done` state
directly: a real `cleanup` cancels execution and is never a stale roster tick.
The replay UI fixture also places late messages and tool calls
between Stop and completion. The real Stop E2E in
`frontend/e2e/restart.spec.ts` waits for both worker `finished` events to be
persisted before checking the stopped roster and reloading, so a fast UI check
cannot miss the late-completion race. Those original events remain in Trace;
these checks only constrain the frontend's derived worker state.

`cmd/zwai/engine_restart_acceptance_test.go` builds real race-enabled binaries,
starts an old engine and a new shell on one temporary data directory, and
measures process replacement and original task/worker restoration against 30s.
Its keyless loopback model drives a real shell tool with a single filesystem
side effect; recovery must retain the unknown call identity, inspect the effect
and finish without repeating the write. It rejects replacement user turns,
worker twins, false completion and terminal confirmation prompts. Binary build
time precedes the measured restart. `go test -short` skips this expensive real
process gate; a short run cannot substitute for the full acceptance.
`cmd/zwai/engine_restart_budget_test.go` checks caller deadlines against real
blocked startup and lease competition, including cleanup of owned resources.
Native acceptance must use private data even when LaunchServices reopens the
bundle without arguments. A default-directory launch invalidates that scenario;
application startup or a backend script alone is not native-window acceptance.

```bash
go test -race -timeout 5m ./cmd/zwai -run TestRestartReplacesTheRealProcess -count=1 -v
go test -race ./internal/engine ./internal/server -run Restart -count=1
cd frontend && npm test -- --run src/lib/transcript-restart.test.ts
```


## Phone model catalog refresh (F-220 / C-006)

`internal/remote/catalog_refresh_test.go` uses `provider.New` and loopback
HTTP model endpoints so the test exercises the real discovery adapter, not
the provider mock's saved catalog. It distinguishes the network-free `catalog`
from explicit `refresh_catalog`, including newly available and removed default
models, all-or-error results, bounded discovery, frame limits and secret/config
isolation. `internal/remote/pump_catalog_test.go` checks that refresh cannot
stall conversation RPCs or outlive its link.

`mobile/src/lib/use-model-catalog.test.ts` covers current-link/generation
ownership, loading, retry and old-host errors; picker/composer tests retain an
explicit selection when refreshed choices change. `mobile/e2e/model-refresh.spec.ts`
drives the offline phone UI from both a PC conversation and New chat. It proves
the web shell interaction, not packaged native-device acceptance.

The disposable iOS layout fixture also runs
`BindFlowTests/testModelRefreshReplacesNamesAndKeepsExplicitSelection`,
`testModelRefreshFailureKeepsChoicesAndCanRetryLocally` and
`testModelRefreshLegacyHostFeedbackDoesNotBlockConversation` from
`mobile/e2e/ios-wait-layout.swift`. Use the matching `IOS_LAYOUT_QUERY` in
`mobile/README.md`, retain the packaged app's screenshots and accessibility
attachments, and inspect the reached model picker. A private development
package retains its source fingerprint separately from any public batch SHA;
these checks neither upload a build nor accept another release's installer.

```bash
go test -race -count=1 -run 'TestRefreshCatalog|TestCatalogRefresh' ./internal/remote
cd mobile && npm test
npm run lint && npm run build
npx playwright test e2e/model-refresh.spec.ts
```

## Historical worker state (F-130 / F-150 / C-019)

`internal/workerstate/snapshot_test.go` keeps runtime snapshots separate from
human messages and real tool pairs. `internal/engine/worker_roster_test.go`
regresses a large finished roster without fabricating new spawn calls.
Goal/schedule continuation, auto-compaction, retries and restart must retain
IDs, roles and current states through the same System JSON snapshot; an old
snapshot is replaced rather than accumulated.

`frontend/e2e/worker-roster-history.spec.ts` drives the real engine, model HTTP
adapter and desktop Web UI against `frontend/e2e/worker-roster-fixture.ts`, a
keyless loopback-only model fixture. It
finishes a worker, sends a later message, checks the existing worker with
`wait_agents`, and verifies zero creation events/calls on that later turn.
It also reloads the visible result and opens its Trace. This proves the
offline path and recorded evidence; it does not promise a real model will
never choose to delegate.
The same fixture exercises Run now and an automatic scheduled fire: each
queries the original finished worker and records no new spawn.

`internal/engine/worker_lifecycle_facts_test.go` supplies a false creation
claim with zero lifecycle events, then checks summary input, stored summary,
goal wrap-up and memory review against the real turn-attributed zero. It also
distinguishes first creation from repeated/restored IDs and paired refused
calls, and retains evidence before long prose under the input cap. The
deterministic rows survive even if the summarizer repeats a false count;
existing prose is labelled as claims rather than cleaned by business keywords.

`internal/engine/compact_evidence_test.go` and
`internal/server/compact_evidence_test.go` keep generated facts in persisted
briefings and model input while manual/automatic `compacted.summary` contains
only the prose shown in the compressed-context preview. Thread API views do
not expose the raw internal summary fields. The offline reviewer tests in
`internal/provider/mock_briefing_test.go` derive a task from framed human/user
source text after `Conversation to review` and submit the batch protocol.
A lifecycle preamble, even one containing source-looking seq text, cannot become
a quoted write source or make distinct tasks create the same scripted memory.
The shared source parser is `internal/provider/mock_review_source.go`.
The mock helper is not an authorization boundary: the engine still checks its
proposed seq and exact quote against the admitted effective source events.

```bash
go test -race ./internal/workerstate ./internal/engine -run 'WorkerRoster|FinishedRoster|Snapshot|WorkerFacts|WorkerClaims'
go test -race -count=1 ./internal/provider -run 'TestMock(Reviewer|ReviewSource|LibraryReviewer|SessionBriefing)'
cd frontend && ZWAI_E2E_WORKER_ROSTER=1 npm run e2e
```

Concurrent E2E runs need separate `ZWAI_E2E_PORT`, `ZWAI_E2E_DATA_DIR` and
Playwright `--output` directories; shared output cleanup can delete another
run's screenshots or traces.

## Memory review provenance and commit (F-420 / C-025 / C-012)

The scripted offline reviewer submits one complete batch. Tests assert real
memory files and stored outcomes, not agreement with the old reviewer's answer:

- `internal/engine/review_quality_test.go`: current notes in the review input,
  partial/refused manager writes do not skip the rest, failed turns retain only
  human requirements, and a successful batch needs no billed closing call.
- `internal/engine/review_history_test.go`: automatic/manual paths use full
  thread history to distinguish restored IDs from new workers, reserve lifecycle
  facts inside the source budget without losing late human input, and keep
  generated/history text outside writable event references. Failed-turn review
  does not read or include lifecycle history; compatibility wrappers accept only
  the canonical admitted transcript as source authority.
- `internal/engine/review_terminal_test.go`: reproduce the real storage boundary
  by creating a `running` turn and calling `FinishTurn(error)` on the database
  while the caller's pointer still says `running`. Review captures a terminal
  snapshot before waiting for the project gate, without mutating shared state.
  A failed snapshot permits human notes only, refuses skills and does not fold
  skill families; cancellation starts neither review nor fold. While a successful
  review waits at the gate, changing the caller's turn ID/status cannot change
  its captured permissions, deterministic fold or original Trace attribution.
- `internal/engine/review_source_select_test.go`: late human input, revision and
  retraction, recent interleaved tool pairs, chronological rendering, CJK/rune
  budgets including all framing, visible omission markers, and whole-group
  omission when identity metadata alone exceeds the per-source cap. Other
  visible source bytes and evidence authority remain unchanged.
- `internal/memory/prompt_test.go`: task-agnostic delta instructions and separate,
  self-contained normal/recovery paths with supported entry and return/completion
  conditions. Failed probes remain context; these structure assertions do not
  prove a model will produce a correct procedure.
- `internal/engine/review_batch_test.go` and `review_evidence_test.go`: exact
  source quotes, refused/unpaired receipts, failed-turn boundaries, unsupported
  new numeric details, whole JSON parsing, repair within the existing cap,
  no partial notes on failure, actual skills surviving later refusal, and
  duplicate completion. Numeric checks explicitly do not prove entailment.
- `internal/engine/review_diagnostics_test.go`: full-capacity review input and
  structured refusal receipts (`usage`, `over_by`, `current_entries`, `matched`),
  supported repair within the existing cap, iteration exhaustion without pending
  notes, missing proposal versus successful empty batch, model/runtime errors,
  terminal conflict/storage categories and bounded safe attempt/refusal metadata.
  Raw proposed note/skill text, old
  entries, exact quotes and arbitrary target/action values must not leak into the
  diagnostic outcome. Capacity and provenance checks retain their existing gates.
- `internal/engine/review_finalize_test.go`: an actual accepted batch followed
  by a real skill-fold write failure retains committed notes/skills and records
  `stop_reason: error` without the storage path. A separate real `NotesDraft`
  CAS conflict checks refusal metadata and the next actual batch's fixed-revision
  termination; its manually supplied refusal receipt does not claim to reproduce
  the entire final-commit race inside `InvokableRun` deterministically.
- `internal/server/review_diagnostics_test.go`: the real settings → project
  memory → turn → automatic offline review → `GET /api/trace/:turn` path.
  Full-capacity failure retains usage, excess, attempt/refusal metadata and the
  original turn id; the main turn stays done, official notes remain unchanged,
  and repair-only entries/quotes do not appear in the stored review outcome.
- `frontend/src/lib/review-diagnostics.test.ts` and
  `frontend/src/lib/transcript-review.test.ts`: optional-field/legacy replay,
  malformed diagnostic fields, distinct stopping/refusal categories, capacity
  usage/excess, submitted proposal count, and truthful unsaved-notes versus
  already-saved-skills wording. UI display must not reflect raw legacy refusal
  text into the chat.
- `frontend/src/components/app/review-status.test.tsx`,
  `frontend/src/components/app/trace-tab.test.tsx` and
  `frontend/src/store/projects.test.ts`: both languages, original-turn Trace
  navigation while newer work exists, loading/failed source reads, safe expanded
  attempt/source metadata, and historical focus that hides the latest live usage.
  Project/conversation ownership and late acknowledgements/errors cannot replace
  another review's result. A pending result outside the initial transcript tail
  is recovered by one known-turn Trace read; generation/thread/turn and starting
  seq guards reject stale responses, and missing/failed reads do not start polling.
- `frontend/e2e/memory-review.spec.ts` with `memory-review-fixture.ts`: a
  keyless loopback model and the real UI/engine adapters produce evidence,
  numeric and full-capacity refusals over the original eight-round budget.
  The browser checks unchanged notes, both transcript and Memory failure
  details, late-result ownership after conversation navigation, original-turn
  Trace details while newer work exists, opening Trace with the right panel
  closed, reload replay and a repaired commit.
  This suite uses the opt-in loopback mode, not a real provider or model.
- `internal/engine/review_cas_test.go`: real offline HTTP requests paused during model work; human/manager edits survive both the first proposal and a repair, conflicts stop without another request, queued reviews read notes after the project gate, and unreadable notes stop before a model call.
- `internal/memory/notes_draft_test.go`: existing write validation, real skill
  overlap, no intermediate notes file writes, net-zero drafts, CAS conflicts,
  legacy notes, storage failure and concurrent drafts against a real store.

```bash
go test -race -count=1 -timeout 20m ./internal/memory ./internal/engine
go test -race -run '^(TestReviewBatch|TestReviewEvidence|TestValidateReview|TestReviewNoteDiff|TestInvokeReviewOperation)' ./internal/engine
go test -race -count=1 ./internal/engine -run 'ReviewHistory|FullThreadWorkerHistory|LifecycleHistory|CompatibilityWrapper|TerminalStatus'
go test -race -count=1 ./internal/engine -run 'Review.*(Capacity|Proposal|Diagnostics|ReceiptFeedback|Bounded|Refusal|ModelError|TerminalFailure)'
go test -race -count=1 ./internal/server -run TestReviewCapacityFailureIsExplainedByTheOriginalTurnTrace
cd frontend && npm test -- src/lib/review-diagnostics.test.ts src/lib/transcript-review.test.ts src/components/app/review-status.test.tsx src/components/app/trace-tab.test.tsx src/store/projects.test.ts
ZWAI_E2E_MEMORY_REVIEW=1 ZWAI_E2E_PORT=18849 ZWAI_E2E_DATA_DIR=.e2e-data-memory-review npm run e2e
```

The merged lifecycle/provenance path must also keep full-history worker facts
before model-written claims without admitting the generated preamble to the
source-event quote map. Reserve its complete bounded block inside the 24,000-rune
source total before selecting human/tool/claim events; the 2,000-rune item cap
still applies to event sources and includes identity framing. Regress
historical/restored IDs, zero-creation claims, refused calls and a failed
full-history read together with source selection and
batch evidence checks; a history failure skips the model while retaining the
successful turn's deterministic fold. Failed-turn human-only review must receive
neither the lifecycle preamble nor derived/task-tool context. The lifecycle regressions in
`internal/engine/worker_lifecycle_facts_test.go` and the source/evidence tests
above cover these distinct boundaries; a valid exact quote is not a proof that
its surrounding model conclusion is true.

The 2026-10-20 Issue #84 targeted offline race run
(`go test -race ./internal/engine -run 'TestReview|TestInvokeReview' -count=1 -coverprofile=/tmp/memory-review-engine-final.cover`)
covered all new diagnostic functions at 100%. Diff-intersected executable blocks
measured 146/155 statements (94.19%); `runReviewInput` was 100%,
`reviewUserMessageWithNotes` 95.7%, batch `InvokableRun` 95.5%,
`invokeReviewOperation` 92.9%, `completed` and `finishReview` 100%, and
`applySkillFold` 92.9%. The shared `driveReviewer`
function measured 89.8% overall: its remaining blocks are existing ADK
constructor rejection and a nil iterator event, neither produced by the current
fixed constructor/iterator path. No production test hook was added to fabricate
those states. This is targeted change coverage, not a repository-wide baseline
or a measurement of model memory quality.

Real-file notes microbenchmarks (macOS/Linux) apply the same three edits and
compare immediate writes with one draft commit. File identities count actual
atomic replacements; process user+system CPU time, allocations and elapsed time
are measured, with fixture reset outside the measured section. Both modes
include the same file-stat probes. This measures storage work, not end-to-end
model latency. Source-render benchmarks use offline synthetic events and verify
that the old late-input loss is actually present in the comparison fixture.

```bash
go test -run '^$' -bench '^BenchmarkNotesReviewWrites$' -benchtime=100x -count=5 -benchmem ./internal/memory
go test -run '^$' -bench '^BenchmarkReviewSourceRender$' -count=5 -benchmem ./internal/engine
```

Online gateway studies are separate experiments, not CI tests or proof of
production benefit. Freeze source evidence and follow-up checks before scoring;
compare final supported facts, explicit corrections, conflicts and later task
behavior. Neither prior memory, old reviewer outputs nor model consensus is a
truth label. Keep paired/shared-output samples distinct from independent runs.

## Project working-directory selection (F-120 / C-026)

`frontend/src/lib/workdir.test.ts` verifies the native bridge boundary and
request/result pairing. `frontend/src/components/app/workdir-input.test.tsx`
covers selection, cancellation, inline errors, preserved manual entry, and late
results after the form closes. `project-dialog.test.tsx` covers filling the form
without saving it, waiting for the picker before save, and clearing a refused
workdir after another selection.
`frontend/e2e/workdir.spec.ts` drives project create/edit and saved paths with a
controlled native bridge, and checks that the browser does not offer a folder
picker. `internal/desktop/directory_test.go` tests origin and existing absolute
directory validation; `internal/engine/project_test.go` retains the save-time
workdir contract.

```bash
go test -race -count=1 -cover -timeout 20m ./internal/desktop ./internal/engine
cd frontend && npm test -- src/lib/workdir.test.ts src/components/app/workdir-input.test.tsx src/components/app/project-dialog.test.tsx
cd frontend && npm run e2e -- e2e/workdir.spec.ts
```

The controlled browser bridge proves the form flow, not the system picker.
Native acceptance opens New project and Edit project in the actual desktop
window, chooses a folder, cancels without losing the path, then saves and
reopens the project. Closing the form while the picker is open must leave a
subsequently opened form unchanged.
With no extension filters, Go regressions require `AllowsOtherFileTypes=true`
and `CanChooseFiles=false`; native acceptance must also confirm that the folder
confirmation button is enabled and fills the path. Option assertions alone do
not replace that native check.

## Late review turn boundaries (F-120 / F-130 / C-003)

`frontend/src/lib/turn-boundary.test.ts` covers consecutive slices without
reordering events, the single footer owned by the last visible slice before
the actual terminal sequence (with legacy end-time fallback), hidden
title/session-memory metadata, quiet turns,
and late notices for a turn that has already ended.
`frontend/src/components/app/transcript-boundary.test.tsx` renders the real
transcript to retain the original answers while preventing
duplicate or metadata-only footers; late review notices remain visible outside
completed goal-session folds. A finished tool followed by an answer establishes
the current turn's live planning status before an older review arrives; that
status must remain visible without adding a footer for the running turn.
`frontend/e2e/late-review-footer.spec.ts` exercises
the browser transcript with delayed review events, paged history and reload,
including opening the original goal session and its stored tool result. These are
regression scopes; a browser check does not replace native-window acceptance.

```bash
cd frontend && npm test -- src/lib/turn-boundary.test.ts src/components/app/transcript-boundary.test.tsx
cd frontend && npm run e2e -- e2e/late-review-footer.spec.ts
```

## Single PC work disclosures (F-130 / C-022)

`frontend/src/components/app/transcript-fold.test.tsx` verifies that one finished
thought or tool result opens with one click, a lone live thought can fold,
failed tools retain their error state, Find reveals a lone thought, and work
still aggregates when another row arrives. `work-fold.test.tsx` keeps pending
hidden registry activity visible. `frontend/e2e/work-fold.spec.ts` walks the
single-thought and single-exec controls through replayed log events and the
multi-row fold through replayed events. The offline-provider conversation
suite also verifies the live chrome on directly rendered work rows.
`frontend/e2e/schedules.spec.ts` opens the lone `report_schedule` row directly
when checking completion evidence, without expecting a one-tool aggregate.

```bash
cd frontend && npm test -- src/components/app/transcript-fold.test.tsx src/components/app/work-fold.test.tsx
cd frontend && npm run e2e -- e2e/work-fold.spec.ts
```

## Phone queue steering after Stop (F-220 / C-003)

The phone does not decide whether a queued row is a live steer or a new turn;
the host does. `internal/remote/followup_relay_test.go` sends real encrypted
Pairlink requests through the production LinkPump and engine, verifies the
new user event against the persisted Trace, and retains the unselected row
after a second Stop. `TestFollowupRelayCleanupJoinsItsActiveWatch` verifies
that fixture cleanup joins the LinkPump and its watch before another test
can replace watch hooks; closing the Pairlink socket alone does not do this.
`internal/engine/followup_failure_test.go` uses SQLite
triggers to prove deletion, event-persistence and restoration error behavior;
`internal/server/followup_failure_test.go` covers the HTTP missing/storage paths.

`internal/store/followups_claim_test.go` pauses a real GORM selection to let
another consumer claim or edit the row, then verifies unique delivery, FIFO
reselection, conversation isolation and storage/busy failures. The old
implementation returned the already claimed row in controlled interleavings.
`internal/server/missing_record_test.go` uses the actual HTTP handlers to check
a repeated queued Steer creates one turn, distinguishes missing conversations,
and binds response/header/default-level log to one request ID without bodies,
query strings or authorization values.
`frontend/src/store/app-followup.test.ts` covers pending-click coalescing,
stale-row refresh outcomes and cross-conversation late responses;
`frontend/e2e/followup-error.spec.ts` drives those controls through the browser.
Queue actions live in `frontend/src/store/app-followup.ts`.

For a native queue check, the app bundle must retain a private data-directory
fallback before an app-selection tool can launch it: such tools may reopen a
bundle without the CLI arguments of an earlier process. Keep the explicit
`desktop --mock --data-dir <temporary-directory>` process alive, and verify
that the native window URL matches that directory's `engine.json` with
`mock: true` before any fixture action. A launch icon or a running process does
not establish this identity. Never use the main data directory for queue fixtures.
The accepted native path removes only a private waiting row, clicks its stale
Steer button, verifies the explanatory banner and refreshed queue while the
turn stays running, then opens a new conversation and verifies the banner clears.


The offline phone fixture now matches the existing host's idle-Steer rule. Its
RED was a missing fixture `user_message`, not a new Go product failure.
`mobile/e2e/composer-layout.spec.ts`,
`BindFlowTests/testQueueSteerAfterStopStartsNewTurn`, and
`ComposerStopTest#steeringQueuedMessageAfterStopStartsANewTurn` exercise
Stop, idle waiting rows, explicit Steer, the restarted live composer and a
second Stop. They assert the other row while the selected turn is live or
cancelled; they do not invent a rule that clean completion leaves the FIFO
waiting forever. Native screenshots and accessibility must reach that state
in the packaged app; an offline Web screenshot is separate evidence.

## Desktop update lifecycle (F-190 / C-024)

The update controller tests use fake time and delayed responses. They prove that
six hours is measured after a successful check, foreground/network events are
throttled, background failures stay quiet, an older response cannot restore an
install button while installation is running, and a later automatic unsupported
result clears a stale manual error without hiding an offered version. The UI
E2E uses an offline update feed; it does not install a public Release. ZIP
replacement, launch arguments and old-app recovery run against a local launcher
stub in `internal/update/apply_recovery_test.go`. That stub does not open a
signed Mac window. Release acceptance still follows [RELEASE.md](RELEASE.md).

`internal/update/bundle_lock_test.go` covers two services and real helper
processes with different data directories targeting one bundle. The losing
install must be busy and the first launch failure must restore the original
bytes. It also covers separate app targets, stable lock identity through
replacement, failure/exit release, and cleanup ownership across later installs.
The original two-service failure was observed before the fix; a same-service
mutex test cannot establish this application-wide boundary.
Whole-bundle and parent-directory symlink aliases must resolve to that same
target even while replacement temporarily removes it. Cleanup tests execute
the generated script against temporary paths containing quotes, dollar command
substitution and backticks; they prove no marker command runs and a later
installation's backup remains. These alias and shell-path counterexamples were
also observed failing before the repair. Filesystem rename/write failures retain
the old bundle for recovery.

```bash
go test -race -count=1 -cover -timeout 20m ./internal/update ./internal/app ./internal/server ./cmd/zwai
cd frontend && npm test -- src/lib/desktop-update.test.ts src/components/app/desktop-update.test.tsx src/lib/desktop-presence.test.ts
cd frontend && npm run e2e -- e2e/desktop-update.spec.ts
```

`make test-mobile` runs the phone TypeScript check before Vitest. A successful
transpile-only unit suite does not prove the production bundle can compile.
The mobile production build and UI E2E remain separate required evidence.

## Issue closure and pending release accounting (C-010)

`make docs-check` also runs `tools/test_audit_pending_batch.py`. The regressions
cover closed fixes still waiting for a platform, three new commits before
24 hours not allocating a version, a fourth commit or more than 24 hours
qualifying even one accepted fix,
resuming a locked batch without another version, fully delivered fixes not
counting again, and missing/non-integrated fix evidence failing the audit. Accepted Mac-only
fixes absent from both the current batch and iOS ledger still count. The
output separates `allocated_pending_issues` from `unallocated_issues`; only
newly unallocated changes contribute to the next version's commit count and
oldest waiting timestamp. Alias reports
and unfinished code do not qualify.
`tools/test_release.py` also checks that a new public batch cannot start while
an Issue remains active or without a finalized workset matching the audited
source, full Issue set and affected platforms. Public-tag recovery keeps its
previously frozen source and does not require a new workset receipt.

Run a read-only real-ledger reconciliation with:

```bash
python3 tools/audit_pending_batch.py
```

Without `--state`, the tool uses `ZWAI_HOME` or the normal `~/.zwai-swarm`
directory. Its sibling `ios-signing/testflight-releases.json` records outstanding
iOS delivery. GitHub is queried with pagination for both open and closed Issues;
closure does not filter the pending ledger or mark a platform as published.
Fix SHAs must be ancestors of the integration checkout's main candidate. These
accounting checks do not replace application tests or platform acceptance.

```bash
make test        # go test -race -cover -timeout 20m ./...  +  front-end unit tests + mobile unit tests
make e2e         # Playwright, on the scripted offline provider
make check       # go vet + both of the above
```

No test needs a model endpoint, an API key or a network. Anything that would
otherwise call a model runs on the **scripted offline provider**, so the suite is
deterministic and fast enough to run on every change.

## Layers

| layer | what it covers | command |
|---|---|---|
| Go unit tests | config, store, memory, provider, tools, engine, search, server, CLI, TUI, and the swarm library | `go test -race -cover -timeout 20m ./...` |
| HTTP tests | every endpoint, SSE replay and resume, the tail log page (`GET /log`, including the live-edge roster sidecar), one worker's log (`GET /agents/:agent/log`), upload path traversal, restart recovery (leftover turns, in-flight sub-agents, and the follow-up queue continue; in-flight tools are closed), PTY terminals (`GET /terminal`, same-origin / loopback Origin, DNS-rebind Host refused, project cwd), phone pairing status/token/offer (`/api/remote/*`, token never echoed), conversation search (`GET /api/search` finds a body the title does not contain; embeddings stay off until a model is pinned), SPA freeze (a Vite rebuild of `dist/` cannot steal hashed JS from a live window; a missing `/assets/*` file is 404 text, not the HTML shell) | `go test ./internal/server/` |
| Front-end unit tests | the event reducer that turns the stream into blocks, the store's conversation targeting, quoting selected transcript text into the composer (count chip at rest, hover to read/edit/drop; tagged `<selected_text>` / `<user_request>` on send; Copy message without the wire tags; the Add to chat snapshot surviving a live stream), clipboard image paste, copying transcript text when the Clipboard API refuses (copy event plant, not a silent execCommand true), file drop onto the composer, find-in-conversation matching (count vs a paint window so a live turn does not freeze), http(s) links leaving the window, sidebar drag order (title drag after 8px, first click still opens), Scheduled inbox / wake banner / notice Run now and cancel / live waits sort first and show prompt / Swarm schedule caps, chrome i18n (`en`/`zh` key parity, locale persist through settings), appearance tokens (`font` / `ui_font_size` / `font_size` / `content_width` / `transcript_mode` / `palette`, directory density), tail-first history pages (`thread-log` / `thread-history` / `use-history-window` / `use-turn-jump`), dismissible settings toasts, ⌘K palette body search (`thread-search` / `palette.test`, stale hits cleared while the next query is in flight), semantic-search settings (off until a model is named) | `cd frontend && npm test` |
| End-to-end | a real browser against a real server: conversation, streaming, sub-agents, files, settings (including the per-note memory cap), theme, chrome language, font and conversation width, directory rows tracking UI size, scheduled inbox / wake banner, Phone settings QR control (pairing failure toasts over the sheet; Bound phones paints a reported model) | `cd frontend && npm run e2e` |
| Phone unit tests | Capacitor iOS/Android apps exist with camera permission and no compiled hub URL; offer URI parse, Noise session, scan/paste screen, live viewfinder (frame, sweeping beam, chime on a pairlink QR), saved-ticket host chips + connecting skeleton (not the scan form), multiple tickets, computer name from `hello`/`list` `host` (not the hub hostname), slim list, resume picker (live turn / last thread / parked wait), compact transcript / watch session (goal flags + waiting), ticket-socket keepalive and reconnect banner, goal/wait banners, device model line from platform+UA (`hello`), launcher is the zwai mark not Capacitor's default; Android system back pops Add a PC, then the new-conversation screen, then a conversation, and finishes only from the inbox or the unbound scan screen; inbox search filters the roster already on the phone; a project row starts a conversation in that project; Android release Gradle reads version/signing from env (no password in git); `android-release` refuses a store upload without a keystore, skips JDK 17, copies the APK to `bin/` | `cd mobile && npm test` |
| Phone E2E | scan screen opens a live viewfinder (frame, sweeping beam, fake-camera preview) and paste of the same `pairlink:v1` URI; a saved ticket shows host chips and Connecting, not Scan QR; Add a PC is a menu sheet; New chat and a project row open the start screen, and on a 320px-wide viewport the project row and the message box stay inside it; sub-agent header counts running workers only, finished history stays grouped and opens a separate activity log without painting worker answers as manager answers; Check for updates shows fetching, then current, an install question, or the stubbed feed error | `cd mobile && npm run e2e` |
| Phone simulators | packaged iOS/Android apps bind via paste of that URI, list the seed thread, New chat, Start; native iOS Menu shows the version without Android's update action | see `mobile/README.md` (not in `make check`) |

Phone output verification (`F-224`, `C-020`): `go test -race ./internal/remote`
checks bounded listing and content frames, traversal and symlink escape;
`cd mobile && npm test -- --run src/lib/remote-files.test.ts` checks
pagination, chunk assembly and safe HTML/SVG classification;
`cd mobile && npx playwright test e2e/walkthrough.spec.ts -g 'a phone lists PC outputs'`
opens the offline PC conversation, checks that Outputs starts at the first
workspace level, opens one folder on tap and then previews its file.
`mobile/src/components/file-browser.test.tsx`
also covers nested folders staying collapsed, search through collapsed paths
and refresh returning to the default fold. The disposable
native fixture `mobile/e2e/ios-wait-layout.swift` also opens the same file
in a packaged iOS simulator app; the fixture does not alter a release build.

Observed Go coverage on the 2026-09-27 integrated candidate, from
`go test -race -cover -timeout 20m ./...` (background paths can vary between runs):

| package | coverage |
|---|---|
| `frontend` | 92.9% |
| `internal/memory` | 96.9% |
| `internal/provider` | 91.2% |
| `.` (swarm library) | 95.0% |
| `internal/tools` | 98.3% |
| `internal/store` | 90.7% |
| `internal/search` | 90.6% |
| `internal/engine` | 90.1% |
| `internal/config` | 88.7% |
| `internal/terminal` | 97.8% |
| `internal/server` | 89.9% |
| `internal/slash` | 92.9% |
| `internal/lease` | 92.9% |
| `internal/tui` | 87.8% |
| `internal/app` | 88.8% |
| `cmd/zwai` | 85.9% |
| `internal/remote` | 92.7% |
| `internal/wakeup` | 95.7% |

`internal/wakeup` is the phone-host sleep assertion. `TestSetDoesNotBounceTheAssertion`
is why a settings reload cannot release and re-acquire (idle sleep wins that
gap). `TestDarwinCaffeinateUsesSystemSleepAndParentPID` freezes `caffeinate -s
-w <pid>`. `TestHostKeepAwakeFollowsPairingAndSurvivesReload` is the host
wiring: pairing on holds, the switch off releases, Reload does not drop.
`TestHelloStoresThePhoneLabelOnThisFingerprint` plus
`TestDecorateBindingsJoinsHubRowsOntoLocalLabels` are why Bound phones
can show a model: `hello` is keyed by the pairlink fingerprint, a quiet
reconnect does not wipe the label, and a phone that has not said hello
stays a fingerprint. `TestHelloAndListNameThisComputer` plus
`mobile/src/lib/store.test.ts` are why the phone chip is this PC's
`display_name`, not the hub hostname, and why a second scan of the same
fingerprint replaces the ticket.
`TestBlankRemoteDisplayNameSeedsTheMachineHostname` is why a blank Settings
name becomes the hostname instead of a compiled label. `TestHostOfferAndServeLinkOverRelay` sends `hello`
and asserts the bindings list carries that label. `mobile/src/lib/device.test.ts`
builds the one-line label from platform + UA.

`internal/remote` is the phone RPC. `TestListDefaultsToFiveAndOmitsProjectSecrets`
is why the phone never sees a project prompt. `TestListRecentPageDoesNotCountLiveRows`
is why In progress does not eat the Recents quota: four parked waits plus
six idle conversations still return five project rows, and More stays on
that idle list. `TestRunningRosterCarriesTheProjectSoTheFolderStillListsIt`
is why a live row still names its project: the phone lists it under that
folder without putting it back on the idle page. `TestCatalogListsReadyModelsAndHidesEndpointSecrets` is why
the phone's model list has a second provider and no endpoint secret.
`TestCatalogOmitsAProviderThatWasSwitchedOff` is why that list also drops
an endpoint the desktop switched off.
`TestPngBytesNamedAsTextStayAFile` is why a png named like a text file is
still a file, and `TestABadSelectionDoesNotLeaveAnEmptyConversation` is
why a refused model does not leave an empty thread.
`TestSlimListPayloadStaysBounded`
caps the default list. `TestUDPBlockedListAndSendStayOnRelay` is the
UDP-blocked path: QR pixels round-trip to the same URI, then list and send
stay on `path=relay`. `TestWatchLiveSendAndUnwatch` plus the kinds freeze
in `watch_test.go` keep phone `watch` on the same seq/kind bus as desktop
SSE, clip `spawned` bodies, and stay under the 64KiB pairlink frame.
`TestWatchOpensAtTheLiveEdgeNotTheOldestEvent` is why a long conversation
on the phone does not start at seq 1 — and why that tail arrives on
the `watch` RPC `ready`, not a trickle of `event` frames.
`TestWatchFirstSnapshotFitsAPhoneScreen` is why first `watch` is a
live-edge page (24), not the whole last turn of a pursuing `/goal`.
`TestWatchOpensOnTheLastTurnNotEarlierOnes`
plus `TestLogPagesOlderEventsBeforeTheViewport` are why first paint is the
live edge of the last turn and pulling up loads earlier events. `TestWatchEmptyLastTurnReadyStillPages`
is why an empty live turn still carries a log cursor, and
`TestLogZeroBeforePagesOlderThanLastTurn` is why `log` with `before` 0
pages older than that window.
`TestLogFindsEarlierAnswerPastHiddenSubagentBookkeeping` covers a long run of
phone-hidden subagent calls and results: one `log` RPC must reach the prior
answer instead of returning another empty-looking page. The phone screen unit
test and `walkthrough.spec.ts` exercise the empty-state explanation and the
Earlier button through the rendered page.
`TestPhoneListAndOpenSurfaceGoalAndParkedWait` is why a parked `/goal` wait
is on the phone roster (`waiting`) even when it is not on the recent page,
and why `open` carries the objective flags plus the wake snapshot.
`TestHumanLineDropsScheduleAndMemoryJargon` plus
`TestListActionOmitsAWakeEnvelope` are why `list.running[].action` is
findings / a command / assistant prose, not `schedule_wake({…})` or
`memory({…})`, and why a quiet scheduled check does not become the Recents
subtitle.
`TestRunNowCancelWaitAndResumeGoalMapOntoTheEngine` is Run now / Cancel wait /
Start goal over pairlink. `TestWatchReadyAndArmPushCarryWaitingStatus` is why
`watch` `ready` and a live `schedule` frame carry `status.waiting` / `wake`.
`mobile/src/lib/session.test.ts` queues `event` until `ready` so an old
host that still streams catch-up still paints once, a later empty
`ready` must not walk the log cursor forward, and a listing stub stays
on screen until that `ready` (a tap is not a freeze).
`TestStatusGoesOfflineWhenHubCloses` is why a dead hub socket cannot keep
minting a QR the phone will redeem as `host offline`: status follows idle-drop
and reconnect of that WebSocket.
`mobile/src/lib/client.test.ts` is why a phone ticket socket keepalives
with a punch-ping data frame, rejects in-flight RPC on `onclose`, maps an
abnormal close to the can't-reach copy and a close reason to a server
refusal (not one reconnect sentence), and sends `hello` with the model
line once the session is up. `mobile/src/components/host-chrome.test.tsx`
is why that chip is green while the socket is up and gray while it is not.
The Capacitor shell in `mobile/` has its own unit tests
and a Playwright paste/scan screen; `mobile/native-project.test.ts` asserts the
iOS and Android trees ship with camera permission, no compiled hub URL, and
the zwai launcher (not Capacitor's default icon), and that Android release
Gradle takes version/signing from the environment (no password in git).
`mobile/src/app.test.tsx` and `mobile/src/components/client-groups.test.tsx`
cover Clients More pending, duplicate tap, success, and failure. A Clients
task's **Earlier** loads the page above the live tail and a later read keeps
that page (`client-groups.test.tsx`, `frontend/src/lib/client-log.test.ts`). The mobile
walkthrough holds a scripted Clients page briefly to verify the spinner.
`mobile/src/lib/android-back.test.ts`, `mobile/src/app.test.tsx`, and the phone
walkthrough prove that a Clients task consumes the first Android Back, root
receives the next, and a delayed read cannot reopen a closed task.
`mobile/src/components/thread-screen.test.tsx` and the phone walkthrough also
verify that Android system Back and the header button return from a sub-agent
log through its list and conversation, including a history refresh that omits
the selected agent before Back.
`mobile/scripts/android-release.test.ts` is why `make mobile-android-release`
refuses a store upload without a keystore, skips JAVA_HOME 17, and copies
the APK into `bin/`. `internal/update` is why a Mac desktop install only
accepts this repo's `zwai-<version>-darwin-<arch>.zip`, skips drafts, searches
past a phone-only latest Release for the newest Mac package, and
swaps `zwai.app` before reopening. `internal/update/apply_recovery_test.go` proves install exclusion, retry after failure, old-file recovery after launch rejection, and native launch arguments. `internal/app/update_test.go` verifies the absolute data directory is retained; `cmd/zwai/engine_cli_test.go` and `frontend/src/lib/desktop-presence.test.ts` verify the window PID reaches presence. `frontend/src/lib/desktop-update.test.ts` and `frontend/src/components/app/desktop-update.test.tsx` cover long-running checks, stale responses, quiet background failures and manual recovery. `frontend/e2e/desktop-update.spec.ts` drives those interactions with an offline update feed; its clock and API interception prove the UI flow, not an installed native upgrade. `internal/release` is why `make release` refuses a dirty version, uploads
only the required platform assets, and fails when a required asset is absent
after upload. `internal/desktop/pack` refuses a
non-release version so `make desktop-release` cannot stamp a dirty describe.
`TestMacReleaseZipContainsFinderIcon` checks the installable ZIP's icon
resource and plist reference; `TestFinderIconMatchesTheRuntimeDockMark` asks
macOS to decode the ICNS and compares it with the runtime mark, so changing
the source PNG without regenerating the Finder asset fails. `MacInstallerIconTests` makes the release driver
reject a new Mac ZIP lacking either before upload. Native acceptance must still
inspect the extracted `.app` in Finder and launch that same package because the
runtime Dock icon is independent. `TestPhoneLauncherSlotsMatchTheDesktopMark`
is why those PNGs are `internal/desktop/appicon.png` painted into the iOS/Android
slots (`go run ./mobile/scripts/genicons.go`).
`mobile/src/lib/resume.test.ts` is why bind opens a live turn (or the last
thread, including a parked wait) instead of parking on New conversation,
and why a tap paints a listing stub instead of freezing on the inbox.
`mobile/src/app.test.tsx` and `mobile/e2e/walkthrough.spec.ts` also assert that
explicitly selecting a saved PC stays on its inbox rather than applying this
startup resume rule to that PC's live or last conversation.
`mobile/src/lib/phone-shell.test.ts` and `mobile/src/app.test.tsx` are why a
saved ticket paints host chips and a connecting skeleton on the first frame
instead of the scan form, and why Add a PC is a sheet. A saved model does the
same: the scan form stays hidden and a Chat tab is the first screen
(`direct-chat-screen.test.tsx`, `provider-sheet.test.tsx`,
`openai-wire.test.ts`, `openai-client.test.ts`). `mobile/e2e/direct-chat.spec.ts`
drives discover and a reply against a routed endpoint, and opens the thought
row after the answer. A multi-event body paints one token per turn
(`openai-client.test.ts`), including a device chunk source that delivers
bytes before the call returns. A responses rejection of an image part is
sent once more as chat completions, and that retry uses `image_url`
(`openai-client.test.ts`). A rejected reasoning summary is omitted and the
responses call is tried again. A different responses 400 is not retried.
`rosterFingerprint` is why a 2s inbox poll that repeats the same roster
does not remount the row mid-tap. `mobile/src/components/markdown.test.tsx` renders `$n$` as KaTeX and copies a
fenced body (including when the Clipboard API refuses), paints `**bold**` and GFM tables, keeps a filesystem path
from becoming a webview navigation, paints a `chart` fence as a plot with a table of the same rows, holds a truncated chart, and leaves a `json` fence and a finished invalid chart as code. `mobile/src/lib/chart-spec.test.ts` is the same parser as the desktop, including an `x`/`y` caption whose row uses other keys. `mobile/src/components/phone-chart.test.tsx` covers line, area, bar, stacked bar, and pie marks. `mobile/src/lib/copy-text.test.ts` is why
that copy plants on the `copy` event instead of trusting WebKit's boolean. `mobile/src/components/thread-blocks.test.tsx`
is why a user bubble uses that same markdown, not the source markers.
`mobile/src/components/home-screen.test.tsx` is why a parked wait sits in
In progress as Waiting, not a quiet Recents row, why a live conversation
is also listed under its project, why that project folds (and stays folded
across a remount) while the new-conversation control is an icon, and why a live
`schedule_wake` / `report_schedule` envelope is not the subtitle.
`mobile/src/lib/inbox-groups.test.ts` is why a roster row with a project
lands in that folder even when `threads` omitted it, and why a live row
with no project stays out of Recents.
`mobile/src/lib/inbox-preview.test.ts` pulls findings and drops the tool name.
`mobile/src/components/host-chrome.test.tsx` is why the menu prints the package version.
`mobile/src/components/thread-screen.test.tsx` is why an empty earlier page does not immediately ask again, why Interrupt is only on a queued follow-up and names the queue head, and why a standing `/goal` is
Pursuing / Done / Blocked / Paused with Start, a parked wait is Waiting
with Run now / Cancel wait rather than a silent Send box, a long
objective stays one truncated line so that chrome cannot cover the composer,
and Earlier sits outside the scroller so a live-edge tail can still page.
A scroll that is already at the top does not ask for another page; that
request is the Earlier button or a pull. Interrupt is on a queued follow-up,
not on every running turn. `mobile/src/lib/phone-turn.test.ts` verifies that
this action promotes the selected waiting message before preempting the current
manager step, leaves later messages queued, and tolerates a steer already read
before the second RPC; `mobile/e2e/walkthrough.spec.ts` exercises the button
through the phone screen against a scripted PC.
The same walkthrough selects answer text and checks that Add to chat stays near
the highlight: below the native menu's reserved space when there is room, or
in its own row below a selection at the transcript bottom. On the old fixed-corner layout
the gap was 300.5 CSS px on a 375px phone; the position regression failed
before the fix. Android's isolated debug package was long-pressed in the real
WebView: Copy/Share/Select all and Add to chat were visible without overlap
for both a top and bottom selection, and tapping Add to chat created the quote
draft in both cases. The disposable iPhone 17
simulator fixture likewise showed the native Copy menu with a hittable Add to
chat button; tapping created the quote draft. WebKit's phone layout test also
checks the quote action and draft. Browser E2E alone cannot render either
operating system's selection toolbar.
`TestLogPageStillSealsAfterTheHostNameIsStamped` is why an earlier page
still fits after the display name and the seal, instead of vanishing and
dropping the link.
A long tool path must not stretch the column (`min-w-0` / `break-words`).
`mobile/src/lib/transcript.test.ts` is why a `schedule` payload is **A wait is armed.**, not the JSON, a `progress` pulse is not a notice, a findings `report_schedule` is that prose as a notice rather than a tool chip, adjacent thoughts and tools fold while an answer stays visible and splits the group, and Planning sits under a closed answer instead of on the fold above it.
`mobile/src/lib/tool-preview.test.ts` pulls `findings` (not `prompt`) for the collapsed chip, and a wait roster is counts rather than `elapsed_ms`.
`mobile/src/components/thread-blocks.test.tsx` keeps that roster off the user bubble and the expanded tool body.

`mobile/src/lib/app-update.test.ts` is why the phone asks GitHub Releases
itself, searches past a desktop-only latest Release for the newest Android
APK, ignores a draft, a prerelease, the version already installed, and
any artefact that is not this repo's `zwai-*-android.apk` on a GitHub host.
A menu check always requests: a fresh cache and Not now do not answer it,
and a failed response is that response's error rather than silence. The
quiet bar still stays silent on failure.
`mobile/src/components/update-notice.test.tsx` is why Update starts that
install and why Not now hides the bar. `mobile/src/components/version-check.test.tsx`
is why the menu shows fetching, the current build, the install question, and
the feed error. The browser walkthrough does not check on its own, so
`mobile/src/app.test.tsx` still sees no Update button.
`mobile/e2e/version-check.spec.ts` drives that menu check against a stubbed feed.

`mobile/src/lib/mock-link.test.ts` is why the walkthrough host answers the
same ops with the same shapes, streams a turn to whoever is watching, says
the turn is over on the frame that ends it, and is one PC seen down two
sockets rather than two PCs (React strict mode opens two). Without it the
inbox and a conversation had no end-to-end coverage, because Playwright
could not get past the scan form. `mobile/e2e/walkthrough.spec.ts` drives
those screens on it: live rows separate from recents with an age on each,
a turn that folds its work behind the answer, a short conversation resting
on the composer rather than under blank space, New chat (and a project row's
icon) opening what it started, a project folder folding while its live row
stays under In progress, inbox search narrowing the rows, a page loaded
with More still there after the inbox refreshes, a loading status while
open is held (`?pause=open`), and the composer's model, thinking level,
and attach control.
`mobile/src/lib/inbox-window.test.ts` is why a later first page does not
drop rows More already loaded, and why a page-one row that left that page
does not stay.

`mobile/src/components/composer.test.tsx` is why Enter sends but Shift+Enter
and an IME candidate list do not, why send stays off until there is
something to send, and why the box grows with the text but stops before the
transcript loses its last answer. `mobile/src/components/inbox-row.test.tsx`
is why a pending question is filled rather than tinted like a wait, and
`mobile/src/lib/when.test.ts` is why a PC whose clock runs ahead reads as
`just now` instead of a negative age.
`TestAParkedWaitCarriesItsSummarySoTheRowIsNotJustABadge` is why that row
has a line and a date at all: `list` keeps a roster thread out of
`threads`, so reading either off the Recents list would have read nothing.
`mobile/src/components/home-screen.test.tsx` is why Running has no date
(it is happening now) and a wait does, and `mobile/src/lib/resume.test.ts`
is why a host that flags the wait on the thread and sends an empty roster
still gets both, without a stale thread line overwriting a roster row the
host did send. `detailFromListing` is why that line does not then read as a
turn: the stub a tap paints would swap the Waiting header and the schedule
banner for Stop for the one frame before `open` answers, which no
Playwright assertion is fast enough to see.
`mobile/src/components/pull-to-refresh.test.tsx` is why a short tug scrolls
and a long one reloads, and why a drag that starts mid-list is never a
reload. Camera on a real device is the product path (`make mobile-ios` /
`make mobile-android`).

`frontend` is the embed plus the incremental Vite rebuild (`Ensure` /
`Load`). The fingerprint and `npm` runner are tested against a fake tree;
`Load` returns the embed under `go test` so `go test ./...` does not
require Node. `TestCommittedLockfilesMatchThePublicNpmRegistry` refuses a
lockfile whose `resolved` hosts are not `registry.npmjs.org` — npm 12
treats those tarballs as remote packages (`EALLOWREMOTE`) on a fresh clone.
`assembleApp` in `cmd/zwai` is the two-line call that
production `desktop` / `web` go through.

What is deliberately not unit-tested: `main`, `runDesktop`/`runWeb`/`runTUI` (thin
wrappers around functions that *are* tested), `desktop.Run` (opens a native
window) and `tui.RunClient` (drives a real terminal). The rest of `internal/desktop`
is tested: traffic-light geometry, hopping AppKit geometry onto the main
thread (Wails delivers window events off-thread), the quit-time
`Window #1 not found` filter, that the Dock icon is a real PNG with
transparent rounded corners on Apple's 824/1024 icon grid, wired into
`application.Options`, that `WritePhoneIcons` paints the same mark into the
iOS App Icon and Android mipmaps (not Capacitor's default lattice), and that
`prepareBundle` writes a Local Network
Info.plist under the cache dir (the `syscall.Exec` re-exec itself cannot
run in a unit test). The window itself still cannot be opened in a unit
test. Each untestable shell was
split so that everything which can fail is testable: `startDesktopServer`
returns the window options without opening a window, and closing that
window does not stop the engine. `serveWeb` takes an injected stop signal
and leaves the engine up. `runTUI` attaches with `ensureEngine` and
`tui.RunClient`. `TestEnsureEngineReplacesAnOlderBuild` checks why a newer
binary replaces the process it would otherwise attach to.
`TestReplacementStopsOnlyAfterTheCheckpointAcknowledgement` requires a durable
handoff acknowledgement before stopping a capable engine.
`TestEnsureEngineWaitsUntilTheTurnIsNotExecuting` covers the older engine's
quiet wait, which does not wait on a human question or a parked schedule.
`TestLegacyRestartUsesOneDeadlineWithoutAConfirmation` and
`TestEnsureEngineAutomaticallyReplacesAnOlderBusyEngine` bound that legacy
wait without terminal input. The launcher reports progress every 5s and shares
one 30s budget across handoff, process exit and replacement readiness.
`TestEnsureEngineStartsWhenTheOldProcessDiesWhileWaiting` is why a process
that exits during that wait does not fail the launch.
`TestKillEndsAProcessThatIgnoresStop` is why that kill follows a ignored
SIGTERM with SIGKILL. `TestSameEngineIsTheBinaryThatWasStarted`
is the match (version plus executable identity). `assembleTUI` / `buildTUISwarm` still build an in-process
swarm for renderer tests. The window itself is verified by hand — see below.

## The scripted offline provider

`--mock` (or `provider.NewMock`) replaces the model with a script that behaves
like a real swarm run: it thinks, spawns two sub-agents (one with
`fork_context`), has a worker call `write` to produce a file in the workspace,
waits for both, then streams a markdown answer that includes a `chart`
fence whose values are the report lengths (so the UI's plot renderer is
on the same path, without a sample domain baked in), a language-tagged
`go` fence, and inline `$n$` math so highlighting and KaTeX ride the
same offline path. Under `go test` those last two extras are omitted:
each extra word is another `mockChunkDelay`, and streaming them on every
manager answer blew the engine package's 10m budget. `zwai --mock` and
Playwright (`go run`) still emit them.

It answers as a **memory reviewer** too: the post-turn script submits a complete
`apply_memory_review` batch derived from effective source events, with exact
seq/quote citations. A normal project review can store a note and a skill;
failed-turn requirements-only input stores notes without skills. Shared-library
review submits skills only. The batch exits the agent and the program supplies
the result line, rather than billing a closing model call. A manager write does
not skip this path. Explicit Review now selects the latest successful turn.
`frontend/src/components/app/memory-settings.test.tsx` and the settings E2E
cover an older settings payload without a review effort, saving each supported
generic level, persistence of Low, and restoring empty inheritance without
changing other settings. `internal/engine/review_profile_test.go` captures real
loopback HTTP requests to verify the review-only override and unchanged output
cap; manager and catalog tidy retain their own effort. Low is optional and
must be accepted by the configured model.
The offline tests and E2E therefore exercise the actual tools, draft commit,
files, outcome event and panel without requiring a real model.

It also answers as a **conversation namer** (`title-namer`): a short label
derived from the request, shorter than the placeholder, so `--mock` and the
tests can tell a generated name from a quoted first message.
`internal/engine/title_test.go` is why that namer runs from the opening
message, a later turn does not get a second name, and an interrupted
opening turn still keeps the landed name.
`internal/engine/schedule_title_test.go` is why an untitled wait plants the
truncated prompt then the same namer replaces it, an explicit or PATCH
title wins, `AutoTitle` off leaves the placeholder, and naming a wait
does not emit a conversation `title` event.

It answers as a **compact summarizer** (`compact-summarizer`) and as a
**session-memory** writer (`session-memory`) with a short briefing derived
from the messages or events it was handed. A manager Generate whose
billed or estimated prompt tokens exceed the compact trigger (80% of a
confirmed window, else `swarm.auto_compact_tokens`, default 80 000) is rewritten
in `BeforeModelRewriteState` before the call: older
replayable tool results are cleared first, then older messages become that
briefing (preferring the rolling session memory), the recent tail stays, and
the human transcript is untouched. The briefing is streamed; silence uses the
provider idle timeout. `internal/config/compact_trigger_test.go` verifies that a confirmed 262144-token window
with 198145 input tokens and a 64000-token output cap must compact before the
request, even though input alone is under 80% of the window;
`internal/engine/autocompact_test.go` verifies that the model middleware
actually clears older replay at that boundary. A terminal context
overflow produces recovery advice (`internal/engine/runerr_test.go`) and pauses
only its failing recurring schedule (`internal/engine/schedule_context_overflow_test.go`).
The same middleware test covers mixed ASCII/CJK replay with no billed usage;
an estimator regression reserves framing for the 1848-message live replay,
and `internal/engine/session_memory_test.go` checks the event-log estimate.
For a live regression, restart the desktop from the fixed build, run the
affected scheduled check once, inspect its Trace and the reached desktop
screen, and confirm compaction precedes any provider context rejection.
Additional tests live in `internal/engine/compact_test.go`
(streamed briefing, no compact deadline, session-memory preference, a
transcript dump not persisted),
`internal/engine/compact_briefing_test.go` (refuse `Tool:`/`Human:`/`Assistant:`
and exec JSON, refuse a source-tail clip, keep dense prose, `/compact` and
auto-compact summarizer input newest-first under a rune cap, no task words in
the prompt),
`internal/engine/session_memory_test.go` (token/tool gate, event-log extract,
a force refresh that is already caught up, a short extract still storing a
briefing, cancel while another refresh holds
the gate),
`internal/engine/followup_test.go` (a queued follow-up starts as soon as the
turn is `done`, even while a session briefing is still blocked on the
summarizer; Steer on a leftover row after Stop starts that text, and a lost
slot puts the row back),
`internal/engine/followup_failure_test.go` (real SQLite failures during queued
row deletion, steering-event persistence and row restoration retain observable
errors and the unclaimed queue),
`internal/engine/session_memory_bound_test.go` (newest-first rune cap, a
multi-thousand-event log staying under the cap, tool results clipped harder than
answers, dump not stamped, a failed refresh stamping the token watermark
so force does not resend, refresh wait is the provider idle timeout),
`internal/engine/microcompact_test.go` (replayable results only),
`internal/engine/goal_session_test.go` (historical wrap leftover stays
generic and is dropped from leftover steers, heat vs
80% of a confirmed window rather than the fixed token budget, wrap-up copied into
session memory when the meter is still cold), and
`internal/engine/autocompact_test.go` (under-budget
skip, in-turn fire, summarizer errors swallowed, a canceled turn stopping compact, a rejected dump leaving ADK state and `compact_summary` alone, roster rehydrated from
the lifecycle events as a current runtime snapshot, default budget leaving a mock turn alone). `internal/engine/autocompact_vs_eino_test.go` is the keep-or-delete
gate against eino's `adk/middlewares/summarization`: same fixture, same stub
briefing, score runtime roster states / in-flight `wait_agents` / last human as a user
message / swallowed summarizer errors / a task-agnostic summarizer prompt. The
Generate is eino's; Finalize keeps the tail and restores one state snapshot from events. It
does not score briefing prose. `internal/engine/autocompact_invariants_test.go`
locks the wiring and the regressions that would silently come back: default
Finalize, fishing spawn pairs from folded text, dropping `wait_agents`,
losing the current user message, stale billed usage, missing finished-worker
states (including leftovers from an earlier `/goal` session). `TestLiveCompactBriefingQualityVsEino` does
that against the configured endpoint when `ZWAI_LIVE_COMPACT=1`; default
`go test` / `make test` skip it. If the custom Finalize does not beat eino's
`DefaultFinalize` on the structural checks, it should be deleted.
And when the manager prompt
still has an open standing objective, it calls `complete_goal` so a `--mock`
run does not auto-continue until the cap. Tests that need to observe
auto-continue call `provider.SetCompleteOpenGoal(false)`.
`internal/slash` and `internal/engine/slash_test.go` are why a `/goal`
send (glued CJK, fullwidth `／`, IME punctuation `、`) is the command at `StartTurn`, not a
user task; `/goals` still is. `/plan` is the same parse; a live turn is
`ErrBusy`. `internal/engine/ask_test.go` is why `ask_user` blocks this
turn until an answer, idle answers are `ErrIdle`, a wrong `call_id` is
`ErrAskMismatch`, interrupt cancels the wait, resume re-arms an orphaned
questionnaire instead of swallowing it, a worker call fails in JSON, and
`AwaitingAnswer()` lists only the blocked conversation (a working turn is
not an ask; answering clears the list). `internal/server/ask_test.go` is why
the thread listing marks `awaiting_answer` while the questionnaire is open.
`internal/engine/schedule_tool_test.go` is why `schedule_wake` upserts on
this conversation (including when the model omits `id`), and
`internal/engine/schedule_wake_id_test.go` is why that tool's schema has no
`id` — a leftover id that matches no stored wait still arms instead of
returning `store: not found`, it still replaces the open wake, while another
conversation's id or a finished id does not arm a new one. `schedule_task` refuses a `GoalContinue` /
`ScheduleContinue` / plan-implement turn, empty `report_schedule` findings
are quiet, `next_in_s` rearms a delay that claim already marked `done`,
garbage arguments come back as JSON `ok:false`, and workers get a deny stub.
`internal/engine/schedule_tool_info_test.go` is why the tool names stay
stable, Info text stays generic, a remaining-time estimate is biased to
about a third, and `report_schedule` must not stretch the interval.
`internal/engine/thread_wake_policy_test.go` sends the original human event to
a keyless loopback OpenAI-compatible classifier, then drives the actual
manager/ADK tool graph with a manager that attempts to wait and claims success.
Assertions use the refusal code, exact source event and persisted schedule
state; assistant prose cannot pass as an arm. Scripted classifier decisions
test program enforcement and provenance, not the accuracy of arbitrary model
interpretations.
`TestReportRecadenceCannotCancelOrChangeAHumanProhibitedWake` refuses the whole
rearm call before cancellation or findings are persisted, while reports
without `next_in_s` remain usable.
`TestHumanWakeAuthoritySurvivesRealCompactAndEngineRestart` removes the original
human sentence from actual compacted replay, reopens the database in a new
engine, rejects generated User-role/peer cues and then accepts a later human
allowance. `TestUnverifiedWakePermissionFailsClosedWithoutRetryingThisTurn`
keeps malformed or unsupported classifier answers pending without repeat
requests in that turn. `TestWakeClassificationCannotOutrunANewHumanInstruction`
and `TestParallelWakeAttemptsShareOneHumanClassification` cover late human input
and concurrent attempts. `TestHumanSteeringAtTheWriteBoundaryCannotBeBypassed`
injects new human input at the first schedule query after verification, proving
the write transaction must recheck authority. `TestWakeClassificationHasACancellableGateAndPerTurnBudget`
covers cancellation while waiting for the classification gate, the 16-input
turn budget, and continued backfill from persisted successes in a later turn.
`internal/store/wake_policy_test.go` covers effective human-only sources,
exact quotes, immutable classification, unread-steer edits/retractions,
compaction, restart and a later explicit human allowance.
`internal/store/wake_policy_guard_test.go` covers the actual schedule write
transactions: create/replace/recadence leave the entire row unchanged when
denied or pending; a newly arrived human input closes the classification/write
race; rewinds, deletion, stale cache rows and storage failures cannot grant
consent. Human inbox operations and independent jobs retain their boundaries.
`internal/provider/mock_wake_policy_test.go` keeps offline policy extraction
neutral and tool-free rather than routing it to the worker script.
`frontend/e2e/human-wake-policy.spec.ts` runs the keyless scripted fixture with
`cd frontend && ZWAI_E2E_HUMAN_WAKE_POLICY=1 ZWAI_E2E_PORT=8814 ZWAI_E2E_DATA_DIR=.e2e-data-human-wake-policy npm run e2e -- human-wake-policy.spec.ts --workers=1 --retries=0`.
In a fresh worktree, first run `go generate ./frontend` from the repository
root so the engine startup budget does not include the first Vite build.
It verifies no new armed card/banner/Waiting status despite a false manager
claim, source-attributed Trace, reload, later human permission producing one
real wait, and a scheduled report refusing recadence after a one-shot fire.
An existing wait is compared unchanged on rejection; prior arm/fire notices
remain valid history. The fixture's source text is generic and its decisions
are scripted; no production endpoint, credential or real model is used.
`internal/engine/schedule_rearm_test.go` is why that recadence refuses
cancelled and paused rows, respects `schedule_max_active`, and why
`schedule_wake` plus `next_in_s` in one turn record one armed chip
(a cancel in that turn still records the next one).
`internal/store/schedule_activate_test.go` is why `done` → `active` shares
the cap transaction with create/resume.
`internal/engine/schedule_goal_test.go` is why a pending thread wake
pauses `/goal` auto-continue without cancelling in-flight sub-agents
(`TestPendingWakeLeavesParkedWorkersRunning`: reaping them left a bare
`context canceled`, the next fire resumed those ids, and the following
wake killed them again), why cancelling it while idle starts the next
pursuing turn immediately (`goal_continued`), why a cancel without a
standing objective stays idle, and why paused, cancelled, and
standalone origin-only rows do not suppress. A still-due delay that
has already been claimed (`status=done` plus a `running` run, matching
`advanceAfterFire` before `StartTurn`) is `TestClaimedOneShotStillSuppressesGoalAutoContinue`
— the armed-row tests do not cover that race.
`internal/engine/schedule_report_test.go` is why a scheduled check with
nothing to surface archives (`quiet`, unread cleared, `turn.quiet`; a
standalone fire leaves Recents) while an omitted report after actual tool
execution with an answer is `findings` and stays in the sidebar, and why a crashed or cancelled
scheduled turn marks the run `error` so `HasRunningRun` cannot stick.
`internal/engine/schedule_completion_test.go` covers acknowledgement recovery
in the original turn, two empty/acknowledgement responses failing visibly,
ordinary chat remaining unchanged, and event-based evidence independent of
answer length or language. Synthetic model-retry cleanup is not tool work.
The desktop notice reducer and localization tests distinguish this continuation
from a genuine model error while preserving older retry envelopes.
`frontend/e2e/schedules.spec.ts` drives Run now through the real UI and Trace
with `ZWAI_MOCK_SCHEDULE_ACK=once` (the E2E server default); `always` reproduces
the visible error path with `ZWAI_MOCK_SCHEDULE_ACK=always npm run e2e -- schedules.spec.ts -g "Run now handles"` (use an isolated E2E port/data dir). These knobs only
affect the offline provider on scheduled checks.
`TestCreateScheduleUsesFrozenCapWhenTickerIsLive` is why inbox create
(and resume/patch, and a standalone claim's default provider) reads the
same snapshot the ticker froze, not a later `e.cfg.Swarm` write;
`ApplyLiveSwarmLimits` is what Settings uses to refresh it.
`TestResumeCannotRestartClosesTheScheduleRun` and
`TestResumeDropsASupersededScheduledRun` are why a leftover
`ScheduleContinue` that resume cannot continue — empty user text, or
superseded by a later unfinished row — closes that bound fire as
`error` instead of leaving `running`. The surviving run is polled until
`FinishRun` (turn close lands first).
Tests opt into `provider.SetMockScheduleQuiet` / `SetMockScheduleSilent` /
`SetMockScheduleFindings`. The default mock sniffs a scheduled check
(`This turn is a scheduled check.`) and calls `report_schedule` with
generic findings (`ZWAI_MOCK_SCHEDULE_QUIET=1` empties them).
`SetMockScheduleSpawn` restores the old fan-out so omitted-report-with-answer
stays testable. `ZWAI_MOCK_SCHEDULE_WAKE=1` is off unless a test arms a
min-interval wait on the first manager step.
`internal/server/schedules_test.go` is why the inbox HTTP API creates
human waits, lists `{schedules, unread}`, pauses/cancels, run-now on
idle starts a `ScheduleContinue` turn, run-now while the conversation is
busy is `409 skipped_busy`, and reading a findings run clears unread.
An omitted `title` plants a placeholder and the namer replaces it
(`TestUntitledScheduleCreateGetsAGeneratedTitle`).
`TestRunScheduleNowConsumesAFutureCronSlot` is why an early cron run-now
advances past the pending slot instead of leaving `next_run_at` on that
same due time.
`internal/store/schedule_test.go` is why `HasPendingThreadWake` treats
an active `kind=thread` row and a running thread fire as pending, and
why paused/cancelled/done-with-no-run, standalone origin-only, an empty
thread id, and a missing table do not (the last returns the error;
the engine fail-opens). `TestActiveThreadWakeReturnsTheSoonestArmedRow`
is the one parked row the phone banner and Run now / Cancel wait target
(soonest `next_run_at`, not a paused or alien conversation).
`PendingWakeThreadIDs` is the listing batch of
those same conversations so the sidebar does not query per row.
`internal/store/schedule_title_test.go` is why `ApplyAutoScheduleTitle`
is a compare-and-swap: a PATCH in between wins.
`internal/engine/schedule_test.go` is why `Status.Waiting` and `Waiting()`
follow an armed thread wake without starting a turn.
`internal/server/schedules_test.go` is why GET `/api/threads/:id` and the
listing carry `waiting` while a thread wake is parked.
`TestManagerPromptPrefersProactiveDelegation`,
`TestOpenGoalPrefersProactiveDelegation`, and `TestTUIManagerMatchesTheApp`
guard `C-019`: runtime prompts reassess delegation and allow a single worker
for independent review, including when the manager must wait. Negative
assertions reject the old large/noisy-only and blanket waiting restrictions.
`TestFullTurnRunsTheWholeSwarm` and `frontend/e2e/conversation.spec.ts` exercise
the offline spawn/result/Trace path. Scripted calls do not measure a real
model's delegation decisions or prove an increased invocation rate.

`internal/engine/prompt_test.go` is why the manager prompt has `## Waiting`
(`schedule_wake`, do not wait for the human to remind, `report_schedule`,
a parallel `exec` progress-poll sleep is allowed, do not sleep the full
remaining time, estimated waits about a third, extra checks, do not pad)
without CI / deploy / pull-request / cron-job samples or a sample duration,
why an open `/goal` names a pending wake as the next turn, and why extra
lists this conversation's active wakes from a real `CreateSchedule` row.
`internal/engine/plan_test.go` is why planning unmounts write/exec,
`propose_plan` writes the file, Implement remounts those tools,
deleting a conversation takes `PLAN.md` with it, and
`/goal` auto-continue does not fire while planning. The mock asks first
during planning (`explore → ask → propose`); default non-plan runs do
not, unless a test calls `provider.SetMockAskUser`.
`internal/tui/ask_test.go` is why a missing TTY fails `ask_user` instead of
hanging, and why digits pick an option. `internal/tui/plan_test.go` is why
planning drops write/exec and a paused `/goal` stays held after Implement.
`internal/engine/project_test.go` and `internal/memory/prompt_test.go`
are why a project worker gets `skill_view` and the notes/skills snapshot,
not `memory` / `skill_manage`, and why that worker prompt stays generic.
`internal/memory/families_test.go`, `internal/engine/review_test.go` and
`internal/engine/review_tidy_test.go` are why
a shared name stem is one skill, leftover families fold after a successful turn
without a reviewer call when auto-review is off, the fold is still on the
turn id as `memory_review`, `FoldProjectSkills` still folds stems then asks
the reviewer to curate the live catalog (and refuses after shutdown), a
tidy report names what was created, deleted, patched, and left unchanged, and
catalog-tidy model calls hang on the project's latest finished turn rather
than the first sidebar conversation that happens to have one. The reviewer's
model call streams (`TestReviewGenerateStreamsSoTheFirstByteIsNotTheWholeBody`):
a one-shot body would hold headers until it finished, and the default 30s
first-byte wait would fail the review. `TestACompleteReviewBatchDoesNotSpendAClosingIteration`
verifies that a successful post-turn batch finishes within its write iteration.
An incomplete batch at the cap now records an error rather than announcing
uncommitted notes; already-persisted skills remain reported. Catalog tidy keeps
its separate direct skill-tool cap behavior. The reviewer prompt does not tell
it to open every skill.
`internal/engine/library_test.go` and `internal/server/library_test.go` are
why a conversation in no project records one skill in the shared library,
lists that skill in the next such conversation's prompt, leaves the library
alone when auto-review or memory is off, can copy that skill into a
project whose origin is `library`, folds a stem family there without writing
notes, hangs that tidy on the loose conversation's turn, and refuses a
missing name or an empty copy.

```bash
go run ./cmd/zwai web --mock --no-open --data-dir /tmp/zwai-demo
```

Its output is derived from the input, so tests can assert on what they sent
without hardcoding a script. **It must stay generic**: no example task, filename
or domain word from a user request belongs in the script, the manager prompt, or
any test expectation. The host snapshot in the prompt is live (`runtime.GOOS`,
today's date); tests assert those values, never a sample OS or a sample command.

## Go tests

```bash
go test -race -cover -timeout 20m ./...       # everything
go test -race ./internal/engine/              # one package
go test -race -run TestSteer ./internal/engine/
go test ./internal/server/ -coverprofile=/tmp/c.out && go tool cover -html=/tmp/c.out
```

`-race` is not optional here. The engine runs a swarm, an event bus and an HTTP
server concurrently, and the interesting bugs in it have all been ordering bugs:
a subscriber that misses the "done" event, a runtime that is still marked busy
when the next turn starts, a listener read while another goroutine binds it.
`TestShutdownDoesNotWaitForTheEventStream` is why ⌘Q does not freeze the
window: the EventSource is cancelled instead of waited out for five seconds.
`TestTruncateDoesNotBusyAgainstAWriter` is why editing a sent message does
not fail with `SQLITE_BUSY` while the previous turn is still flushing events
(the database is one connection).
`TestListWorkspaceDoesNotHideSiblingsBehindAFatDirectory` is why the Files
panel still shows later siblings when an early directory would otherwise spend
the 2000-entry cap.
`TestMissedHandoffReachesTheWaitingManager` is why a worker that
`send_message`s an invented id still finishes as done and the manager's next
model call sees the handoff text: the miss notifies the host
(`notified: manager`) instead of a `NodeRunError` or a retry roster.
`TestSpawnWithoutATaskDoesNotKillTheManager` /
`TestSpawnAgentRequiresATask` are why a `spawn_agent` that omits `task`
returns `{"error"}` so the manager retries, instead of a `NodeRunError` that
blocks a pursuing `/goal`.
`TestInvalidSpawnBatchIsBoundedAndCanBeRepaired` verifies that a reply with many task-less spawns produces bounded counted refusals, no workers from refused calls, and a corrected spawn in the same run. `spawn_batch_test.go` also checks fragmented arguments, original-message immutability, valid/other calls, generated and streamed output, and cancellation/error propagation. This accepts host recovery, not a promise that any real model never emits invalid calls.
`TestPathShapedRolesBecomeDistinctJobNames` is why a slash-shaped role is
stored as the job word, and a second path of the same job is `-2` rather
than one resumed id or the path itself.
`TestKeepHumanSteersDropsTheSessionWrap` is why an unread `/goal` wrap-up
leftover cannot become a human turn that resets the auto-continue cap.
The front-end reducer also hides the same historical text on older sessions.
`TestRetractUnreadSteerDropsItFromReplay` /
`TestPreemptKeepsTheTurnRunning` are why Delete drops one unread `[steer]`
by event seq (the `steer` row stays; replay does not) and Interrupt does not
cancel the turn. The library side is
`TestPreemptAbortsInFlightToolAndDeliversSteer` /
`TestRetractPendingSteerDropsItFromTheInbox` /
`TestPreemptDoesNotCancelWorkers`.
`TestGoalSessionExtendsTheSameTurnAtTheIterationCap` is why a `/goal`
ReAct slice keeps the same turn instead of asking to extend or cutting a
session; `TestAGoalTurnFinishesAcrossOneRoundSlices` is why `complete_goal` still
lands when each ReAct slice is one round;
`TestStitchManagerToolResultsFillsDroppedWaitResults` /
`TestStitchManagerToolResultsRefreshesAStaleWait` /
`TestManagerToolResultsIgnoresWorkersAndEmptyTurns` are why a slice that
lost its ADK tool results still reads the event-log `wait_agents` report
(including a later wait that reused the same call id);
`TestMockWaitCallIDsStayUniqueAfterAContinuedRun` is why the scripted
manager does not reuse `mock-wait-1` on the next ReAct slice;
`TestSetHistoryKeepsDroppedToolResults` /
`TestRunWithMaxIterationsKeepsToolResultsInTranscript` are why that slice's
tool results are in the transcript the next slice reads;
`TestClosingAGoalAtTheReActSliceDoesNotAskToExtend` /
`TestBlockingAGoalAtTheReActSliceDoesNotAskToExtend` are why
`complete_goal` / `block_goal` mid-slice must not pop the non-goal
confirm card;
`internal/engine/goal_reopen_test.go` is why a mistaken `complete_goal`
can be undone in-turn (`reopen_goal`, which also resets `goal_auto_turns` so
a complete at the cap does not immediately recap) and later from **Start**
(`ResumeThreadGoal` no longer rejects a completed objective);
`internal/engine/goal_schedule_completion_test.go` covers the narrower scheduled
tool conflict: a manager-owned current wake cannot both continue and complete
the goal, either tool order must agree, the last check can finish, and independent
waits remain valid. Database failure checks must leave completion state and its
Trace event together. The keyless loopback HTTP fixture in
`frontend/e2e/goal-schedule.spec.ts` exercises Run now and automatic fires through
the real tool graph, checks the recoverable `goal_schedule_conflict`, then ends
the wait and completes the goal. It verifies Waiting/Open and Idle/Done after
reload, preserving screenshots and Trace JSON. Run separately with
`cd frontend && ZWAI_E2E_GOAL_SCHEDULE=1 npm run e2e`; this is Web behavior evidence,
not a substitute for required native-window acceptance.
`internal/engine/goal_schedule_start_test.go` controls the actual binding/write
boundary without sleeps: a runtime must not start before its claimed fire is
persisted, binding failure must not launch a model, and a second schedule lookup
failure must not report a successful mutation. Restored goal-bearing fires use
the same checked ownership boundary; independent legacy no-goal reports retain
their compatibility path.
The scheduled-start fixture leaves its update callback registered for its
private database's lifetime and only releases the gate during cleanup. A
terminal turn status or idle runtime does not join its final `TouchThread`
write; unregistering a Gorm callback then can race an active update. Repeat
the regression with `go test -race ./internal/engine -run '^TestScheduledRuntimeBindsItsClaimBeforeTheFirstModelTool$' -count=20`.
`TestCatchUpStopsWhenTheWatchIsCancelled` /
`TestWatchCancelSkipsReady` /
`TestWatchReplaysAfterALaggedSubscriber` are why a phone `unwatch` / second
`watch` must not keep pushing the old thread, and a slow phone replays
from the store instead of dropping stored events;
`TestGoalContinuesWhenTheManagerStopsCallingTools` is why the manager
stopping tools starts the next turn with no `goal_session`;
`TestAContinuationWithoutToolsStopsAutoContinue` /
`TestAContinuationWithToolsKeepsAutoContinue` are why an empty
continuation records `goal_idle` instead of looping, and a live
`wait_agents` does not;
`TestHoldGoalIdleIsIdempotentAndSkipsMissingThreads` /
`TestAHumanMessageClearsAHeldGoal` / `TestResumeClearsAHeldGoal` are why
the hold is sticky until a human message or Start;
`TestParkedWorkersStayVisibleBetweenGoalSessions` is why Agents still
sees in-flight sub-agents after the manager yields.
`TestHostNotifyKeepsWorkerEventsAfterRunReturns` /
`TestAttachHostNotifyIgnoresNil` are why a parked worker still records
`finished` after the manager `RunWith` returns, instead of freezing
Agents on starting with an empty pane.
`TestRaisingMaxConcurrentUnblocksQueuedWorkers` /
`TestLoweringMaxConcurrentKeepsWaitersQueuedUntilASlotFrees` /
`TestApplyLiveSwarmLimitsResizesParkedAndRunningRegistries` are why
Settings → Swarm → sub-agents at once takes effect on the live and
parked registry: a higher cap starts waiters immediately, a lower cap
does not kill in-flight workers.
`TestSpawnToolRefusesPastTheConcurrencyCap` /
`TestParallelSpawnsAdmitOnlyTheCap` /
`TestSameRoleSteerDoesNotNeedAFreeSlot` /
`TestResumeToolWaitsForAFreeSlot` /
`TestForkedSpawnRefusesPastTheCap` /
`TestSameRoleResumeRefusesWhileAnotherWorkerHoldsTheCap` are why
`spawn_agent` and `resume_agent` do not park a worker past the cap,
while `Registry.Spawn` still queues.
`TestNextGoalSessionAfterRestartResolvesLeftoverWorkers` /
`TestNextGoalSessionAfterRestartRestoresRunningWorkers` are why a process
death between `/goal` sessions does not turn `wait_agents` into
`unknown agent`: leftovers are planted or restored from the conversation
event log, and one runtime-state snapshot preserves IDs and terminal states
after later turns drop previous tool results.
`TestResumeConversationCompletesADanglingWaitOnTheLiveIds` /
`TestResumeConversationDoesNotPinFinishedLeftoversAsNakedSpawns` /
`TestCloseOrphanedWaitAgentsRecordsATimedOutSnapshot` are why a kill
mid-`wait_agents` completes that wait as a timed-out snapshot of the live
ids without replaying finished leftovers as new `spawn_agent` calls.
`TestOrphanedWorkersTreatsCleanupAsFinished` is why a
killed leftover is planted as stopped instead of restarted.
`TestStartupPullsAWakeWhenTheLatestDispatchWasStopped` is why a restart does
not leave a future wake polling sub-agents the previous process already
stopped; `TestScheduleWakeRefusesUntilAReplacementDispatchStarts` and
`TestReportScheduleRefusesToRearmWhileThoseSubAgentsStayStopped` are why
that wait cannot be re-armed until those exact ids are resumed or a newer
spawn has started.
`TestCloseAlreadyFinishedIsNotCancelled` is why `close_agent` on a planted
finished worker returns `already_finished` instead of `cancelled: true`
(that lie made a long `/goal` walk the leftover roster).
`TestManagerPromptDoesNotTreatLeftoverWorkersAsSomethingToClose` /
`TestLeftoverWorkerCuesDoNotInviteARosterClose` are why the manager is told
leftover ids stay for `resume_agent`.
`TestAutoCompactRehydratesFinishedWorkersFromEarlierTurn` is why a fold
on the next session still pins those ids.
`TestANormalMockTurnDoesNotAutoCompact` is why the default token budget
leaves a scripted turn alone; `TestAutoCompactFiresDuringATurnWhenOverBudget`
is the in-turn fold.
`TestBuildUserMessagePutsImagesOnUserInputMultiContent` is why a pasted
screenshot with a caption does not send `Content` and `UserInputMultiContent`
together (OpenAI's marshaler rejects that pair);
`TestExclusiveContentDropsCaptionAlreadyInParts` is the client-side strip
so a later concat cannot revive the same error.

`TestAFailedTurnBlocksAnOpenGoalAndDoesNotAutoContinue` is why a crashed
`/goal` turn that is a real refusal shows Blocked with the public turn error on the banner (not a
generic sentinel) and an idle composer instead of auto-continuing into the
same failure; `TestARetryableModelErrorRetriesInsteadOfBlockingTheGoal` is
why truncated tool JSON / a `429` / a dropped stream / a mid-stream TCP read timeout re-enters the same
turn (`model_retry`) instead; `TestModelFailureClassSeparatesABadToolCallFromTheModelService` is
why that row's `reason` says the model returned invalid tool JSON or the model service failed
(rate limit, unavailable, dropped connection, idle); `TestARetryableModelErrorContinuesTheGoalAfterRetriesAreExhausted`
is why exhausting those retries still auto-continues (or `goal_idle`) instead of pinning the banner;
`TestARetryableModelErrorWithoutAGoalDoesNotAutoStart` is why that auto-continue is `/goal` only;
`TestFailedTurnBlockReasonPrefersThePublicError` is the
fallback when the turn row has no error text;
`TestTurnOutcomePrefersAModelFailureOverInterruptNoise`
is why a `NodeRunError` is still an error;
`TestTurnOutcomeTreatsABareCancelAsAnError` is why a cancelled model
call without Interrupt is not a clean `done`.
`TestAnInterruptedTurnDoesNotBlockTheGoal` is why Stop pauses (`goal_capped`)
instead of blocking; `TestPauseOpenGoalOnInterruptMarksAPursuingGoal` is the
direct pause path.

The TUI package also covers the same tool-display rule as the desktop UI: an
`exec` notification whose args are JSON is shown as the command, a non-zero
exit becomes a failed block (not a JSON dump), and `web_search` lists hits.
`TestPanesShowTheConversationAndHideTheBookkeeping` is why TUI drops
`wait_agents` / `close_agent`. Desktop `transcript.test.tsx` and the phone
`thread-blocks.test.tsx` hide `close_agent` the same way (`spawn_agent`
already has a SpawnRow).
A missing `--task` opens the composer instead of failing; typed turns continue
the transcript, and a `--task` run still exits when that swarm finishes.
`--goal` without `--task` is the first user message and starts immediately
(`TestTUIGoalWithoutATaskStartsTheObjective`); leftover words still win as the
task. An empty auto-continue returns the composer
(`TestAGoalContinuationWithoutToolsReturnsTheComposer`) instead of looping
until `goal_max_auto_turns`. Typing `/` opens a command popup (`TestSlashMenuAppearsAboveTheComposer`) with
prefix filter, a `/model` / `/reason` picker, and `/help` / `/clear` / `/exit`.
`shift+tab` still cycles thinking level for the next turn, same choice the
desktop composer makes. The idle composer parks
the real terminal cursor after committed text (`TestComposerIMECursorSitsAfterCommittedText`)
and idle ticks must not rewrite the view (`TestIdleTicksDoNotRepaintTheComposer`),
or CJK IME preedit jumps to the start of the line.

Conventions in these tests:

- Every test gets its own data directory (`t.TempDir()`) and clears the
  `OPENAI_*` variables, so a developer's environment cannot change the outcome.
- Test names say what would break for the user, not which function they call:
  `TestStartTurnIsAcceptedNotAwaited`, `TestSlowSubscriberDoesNotBlockTheRun`.
- Comments in tests explain *why the behaviour matters*, because that is what a
  later reader needs to decide whether a failing assertion is a regression or an
  obsolete expectation.

`cmd/zwai/cli_trace_test.go` is split from `cli_test.go` so neither file
crosses 1000 lines: a memory review on the same turn id shows in `zwai trace`.
`internal/provider/provider_wait_test.go` is wait_agents progress parsing.
`internal/tools/pin_test.go` is why a UTF-8 `read` whose first 4096 bytes
cut a rune stays `encoding=utf-8`, why an empty `edit` `replace_block`
deletes instead of the old catch-all missing-payload error, and why
`write` refuses a missing/`contents` body instead of claiming `Updated file`
while emptying the disk.
`internal/tools/execwait_test.go` is why `exec` still runs a progress-poll
sleep and shortens one of five seconds or more to about a third of that
duration (a sub-second pause between commands is left as written).

## Front-end unit tests

```bash
cd frontend
npm test          # vitest, once
npm run test:watch
```

`src/test/setup.ts` replaces `localStorage` with a Map. Node 25's global
`localStorage` is not Web Storage and it shadows jsdom's; chrome preferences
(theme, conversation-list width) would otherwise silently no-op in unit
tests. The Map is cleared after every test.

Several things are tested here, some as pure logic and some in jsdom:

- **`src/lib/transcript.ts`**, where the stream becomes UI: streamed text
  replaces rather than appends, a completed block folds into the streamed one
  instead of duplicating it, a tool call pairs with its result by
  `tool_call_id`, a `tool_delta` fills the pending row without closing it,
  `collapseLiveEvents` keeps the latest snapshot per live tool call (two
  parallel `exec`s do not mix), a progress pulse updates the live summary without leaving
  a row in the timeline or reviving an agent that has already finished, a
  `usage` pulse is ignored by the reducer (the composer meter owns it) and
  `collapseLiveEvents` keeps only the latest usage snapshot in a burst, a
  `max_iterations` event becomes a confirm card (and `max_iterations_continued`
  resolves it) rather than an unknown notice. A `model_retry` notice follows
  `reason` (invalid tool JSON, rate limit, unavailable model service, dropped
  connection, idle). The `{attempt,cap}` JSON stays in Trace. An envelope
  with no known reason stays the generic retry line. An interrupt, a `cleanup`, a
  worker `finished`, or a manager `error` closes tools left pending so they
  stop spinning. A clean manager `done` does not: leftover workers are parked
  for the next `/goal` session or armed thread wake. `TestScheduledChecksReuseTheWorkerRegistryAcrossWakes`
  keeps real mock workers running across two scheduled turns and checks their final errors;
  `TestCancelLastWakeStopsParkedWorkers` and `TestPauseLastWakeStopsParkedWorkers`
  cover the terminal wait boundary. `goal_continued` must not paint them done
  or `wait_agents` sits next to a Done roster while the worker is still in
  `exec`. `cleanup` is the kill signal.
  `collapseLiveEvents` keeps only the latest snapshot per agent and kind from
  a burst of deltas — lossless, because a delta carries the accumulated string.
  Live tool output uses the same rule keyed by `tool_call_id`. Carriage return
  in an expanded tool body is overwrite (`src/lib/carriage.ts`). A pending `exec`
  row starts open and the output box follows the tail (`src/lib/output-scroll.ts`).
  `splitQueuedSteers` pulls unread steering out of the turn body while a turn
  is running (a later model round on the same turn consumes it; a previous
  turn's steer stays put). `steer_retracted` hides that bubble by seq (a
  duplicate caption stays); `steer_preempted` is silent. A retract that
  arrives before its `steer` (history paging) still hides the later row.
  `steer_revised` replaces that bubble's caption (the latest revision wins,
  including when it arrives before the `steer`). A rewind drops revisions at
  or after the cut. A `resumed` event keeps leftover sub-agents running
  — a second `spawned` for the same id is the roster coming back, not a twin —
  closes tools left pending so a killed `exec` does not keep spinning, and
  dismisses a leftover tool-round confirm so a crash does not look like
  the turn is still waiting for a click. Engine tests assert the matching
  `tool_result` (`err`: the previous process stopped) is stored before `resumed`.
  A `rewound` event (and `rewindTranscript`) drops
  that `user_message` seq and everything after it without lowering `lastSeq`.
  `placePendingEdit` keeps that cut. A later stored row inside the cut, and a
  seq-0 delta before the replacement `user_message`, are ignored so the old
  editor cannot reappear. The replacement message and deltas after it stay.
  Both are pure functions,
  Standing-objective notices (`goal`, `goal_continued`,
  `goal_idle`, `goal_session`, `goal_resumed`, compact) live in `transcript-goal.test.ts`: auto-continue
  starts the next turn's clock, a parked worker stays running across manager
  `done` and `goal_continued` so Agents cannot say Done while `wait_agents`
  is still blocked, a forced session end does not paste its JSON,
  a session wrap-up `steer` is omitted so it cannot look like human guidance,
  a `goal_capped` interrupt payload notices as paused without pasting `interrupted`
  and both cap and idle notices name Start so a budget pause is not a crash,
  the briefing JSON never becomes a chat row (`detail` holds `summary` so an
  icon can open it), and auto-compact shows compressing then the token counts
  instead of the payload. Scheduled-task kinds live in `transcript-schedule.test.ts`:
  `schedule` / `schedule_fired` / `schedule_cancelled` are manager notices (not
  `user`, not the JSON payload or raw id), `schedule_skipped` is a no-op,
  a quiet `schedule_report` drops that turn's chat bubbles and later events
  cannot grow them back, an omitted report whose `done` text is empty (or
  whitespace) after `schedule_fired` is the same quiet drop, a findings `done`
  or `schedule_report` keeps the fired chip then the answer (including when
  empty `done` follows a findings report), an armed `schedule` plus empty
  `done` keeps the wait notice, and an ordinary empty `done` stays
  visible. `compact-notice.test.tsx` clicks that icon.   `schedule-notice.test.tsx` clicks Run now and Cancel wait on an armed wait when `detail` is a `sch_` id (including a padded id matching the store row) and does not treat a
  cancelled notice as a briefing.   `schedule-view.test.ts` is why the inbox
  matches All / Active / Paused / Completed (`inboxFilteredSchedules`; Active
  is the default, shown as Upcoming; the menu does not offer All) and search matches title or prompt, empty titles fall back to `prompt`, an armed
  chip paints the store before GET, a replayed arm chip cannot rewind a
  later `next_run_at`, a fire marks that conversation's live
  wake done, and a stale empty GET cannot wipe the
  open conversation's live wait.   `schedule-inbox.test.tsx` /
  `schedule-dest.test.ts` cover Runs in (standalone vs a thread wake), a
  Create drawer with a Task field and no Title, click-to-edit (`schedule-edit-drawer`)
  that PATCHes title / prompt / cadence / destination without resending an unchanged interval,
  pause/run-now in that drawer, a finished wait read-only, and
  `schedule-banner.test.tsx` / `app-schedule.test.ts` cover the sidebar
  page control (`aria-current="page"` while open, the name stays Scheduled),
  pause/run-now/New task labels, no unread count on the rail icon, busy run-now `skipped_busy` as a
  labelled alert on the scheduled page, the composer wake banner's labeled
  Run now / Cancel wait (`schedule-wait-actions.test.tsx`), the banner hiding
  while the conversation is working, a parked wait painting a breathing clock
  on the sidebar row and Waiting on the title bar (`waitingThreadIds`,
  `wait-mark.tsx`), run-now painting the open conversation Working,
  a `schedule_skipped` stream event refreshing the list, and a silent
  schedule GET that must not toast. The inbox list is the nav column
  (`schedule-list-pane` inside `conversation-list`); the editor is the task
  column (`schedule-page` wraps both and is not an `absolute inset-0` overlay).
  It defaults to Upcoming (the Active filter). The status menu is
  Active / Paused / Completed, not a tab grid; Completed
  lists done and cancelled waits. The conversation side panel is hidden
  (`side-panel`). New task opens `schedule-create-drawer` so the form does not
  sit on the list; Expand hides the list (`data-expanded`, list pane `hidden`;
  Escape collapses first). The form is a Task field plus Details (Runs in:
  new conversation each run, or wake a listed chat; Project only for a minted
  conversation) and Frequency (Repeat + delay/every/cron). Click a row for
  `schedule-edit-drawer` (title in the header, Task + searchable Runs in +
  cadence; Save is dirty-only, including a destination-only change). A finished
  wait locks Runs in. The open conversation's side panel lists waits aimed at
  that chat and jobs that minted it (`conversation-schedules`,
  `GET /api/threads/:id/schedules`); a row opens that task's editor. Pause keeps a selected wait under Upcoming
  while the editor is open. Escape then closes the drawer, then the page. Several unread fires on one wait collapse
  to one Open findings control (a count when there is more than one) so
  finished reports stay off the row (`unreadFindings` newest-first; opening
  a conversation marks every unread fire on that thread; wait rows are
  `shrink-0`). A `session_memory` event is
  quiet on the manager like a generated title — no chat row, no extra worker.
  Finished `goal_session` /
  `goal_continued` turns fold behind a one-line Worked-for row in
  `transcript-session.test.tsx` (CJK preview truncates; duration does not wrap;
  an armed wait and a budget-cap notice stay visible outside the fold;
  the hover clock is the last finished answer, not every answer in the turn).
  A `goal_capped` / `goal_idle` / `goal_blocked` notice after that work stays
  outside the fold so a budget pause cannot hide behind Worked-for.
  A failed session stays expanded as Stopped-after with the error visible,
  and a collapsed preview prefers that error over the last answer.
  Folded successful work is not mounted until the row is opened, so a long `/goal`
  cannot re-parse every past answer on each streamed token.
- **`src/lib/turn-nav.ts`**, **`src/lib/use-turn-jump.ts`** and **`src/components/app/turn-nav.tsx`**: human
  `user_message` rows become jump targets (steering does not; `goal_continue`
  and `schedule_continue` share the last human tick even when `user_text` is
  the protocol prompt). Ticks pack into a compact
  cluster in the middle of the pane rather than stretching it, and a long
  conversation shares a fixed height instead of overflowing a second
  scrollbar. The hover list is a wider two-line preview of those sends, not a
  shadowed popover of fake bubbles. The active tick
  is the last human turn whose top has reached a 48px band under the top
  of the pane (a later input that is only visible lower down does not take
  the tick — at scrollTop 0 that used to light the second send while the
  scrollbar was still on the first), or the
  latest turn when the scroller is at the bottom or still following the live
  edge (opening a conversation used to measure at scrollTop 0 and keep the
  first tick current). A click sticks until the reader wheels again, and
  the pending jump is re-applied after a lazy prepend (clearing it on the
  first scrollIntoView used to swallow the click).   A click pins the user
  send at the top of the pane (`scrollTop` on the transcript scroller —
  `scrollIntoView` on the turn group left earlier work peeking above the
  first tick, and `scroll-margin` on the user row kept that gap). The
  earliest tick is the exception: it opens the top of the log
  (`scrollTop` 0) so a prefix above that bubble stays on screen. Pinning
  it left the thumb off zero and the prefix looked unloaded. The pin
  runs again after paint so a history prepend that skipped compensation
  near the top cannot leave the previous turn on screen; the earliest
  jump stays at the top instead, so that same prepend stays visible. Turns not yet mounted are skipped rather than treated
  as offset 0, and a
  missing id after a conversation switch is a no-op. The rail stays hidden until
  there are two user turns; hover opens the list; a click (or arrow keys) jumps.
  A click whose row is not mounted yet pages then scrolls after commit — not
  in the fetch callback.
- **`src/lib/thread-log.ts`**, **`src/lib/use-history-window.ts`**, **`src/store/thread-history.ts`**, **`src/lib/welcome.ts`**: opening a
  conversation paints one viewport of the live edge (`GET /log`) and resumes
  SSE after that seq; scrolling up prepends older pages without jumping the
  viewport. The top sentinel pages even when IntersectionObserver fires in
  its 80px lead (`scrollTop` still 80), and a later scroll to 0 pages even
  if that first intersection already happened — IO does not re-fire while
  the sentinel stays on screen. A scroll also pages once the reader has left
  the live edge and the oldest loaded turn-nav row is inside one viewport
  (or the scroller is already inside that top viewport). `scrollTop < 48`
  alone left that row on screen with older history still above it. An older
  page that does not move the seq cursor clears `has_more`. Prepend keeps the same row on screen unless
  the reader already reached the top, in which case they stay on the newly
  loaded rows. A jump to an unloaded turn keeps paging until that user row exists
  (`use-turn-jump`: the scroll waits for the row to mount — scrolling in the
  fetch callback missed; a click that already found its row stays pending
  across a lazy prepend, because a sentinel page started at the top used
  to land after the jump, skip restore, and swallow the click; `loadUntilTurn`
  joins an in-flight sentinel page
  instead of treating `historyLoading` as the end of the log, and fetches max
  pages rather than the 24-row viewport).
  A worker-only tail keeps paging until a non-quiet, non-spawn manager row exists so the
  empty-state idea cards cannot cover a running conversation. The live-edge
  page also folds a `roster` sidecar (`spawned` / `finished` / `cleanup`
  outside the viewport) so a manager-only tail does not empty the Agents tab.
  Roster sidecar rows do not become manager "Started" lines. Opening a
  worker off the live edge fetches `GET /api/threads/:id/agents/:agent/log`.
  `isWelcomePane`
  stays false while a turn is running, workers are on the roster, or older
  history is still above the tail.
- **`src/store/app.ts`**, against a fake API: boot loads the catalogs and leaves the center on the home page with no conversation selected (the rail stays on the last list); which conversation an action lands
  in when New conversation is clicked twice, when a send races a slow create,
  and when a file is dropped on the empty state; sending while a conversation is
  still being created must wait for it, or the turn runs in the conversation the
  user just left; Enter while a turn is running queues a follow-up instead of
  steering, except the live turn's own user text (a leftover Enter / IME echo
  must not enqueue the request that just started); ⌘Enter / `{steer:true}` injects now
  and drops a queued copy of that text; `{fromEventSeq}` starts a turn
  (never a follow-up) after clearing everything below that bubble and leaving
  the edited text in place, an edited follow-up is moved to the back of the
  queue, and an idle enqueue falls
  through to starting a turn; a title event renaming the open conversation (and a failed
  namer leaving the placeholder, and a `done` `refreshThreads` whose GET
  is still `title_auto` — or omits the field — not stomping that name — `src/lib/thread-title.ts`); `refreshCatalogs` rediscovering every
  endpoint with a URL without rebooting the open conversation; a live
  `usage` event filling the composer snapshot (reload reads it from GET);
  and opening a conversation whose live-edge page is only worker tools pages
  older events until a manager row exists before painting as loaded.
  Switching away from a live turn keeps `thread.running` on that row so the
  sidebar progress does not wait for you to click back in. `setThreadRunning`
  lives in `src/lib/thread-title.ts` and also stamps `awaiting_answer` so a
  blocked question is not a working pulse after you leave. A listing refresh
  is the source of truth for every other row: `mergeThreadList` takes
  `running` / `awaiting_answer` / `waiting` from `GET /api/threads`, and only the open
  conversation keeps a live overlay so an Enter/`done`/arm race cannot flicker.
  That is how a `/goal` auto-continue or a schedule fire on a conversation
  you are not looking at still lights the folder without a click — and how
  a finished background turn goes dark. `startSidebarSync` re-reads the
  listing every 2s while the window is visible (`src/lib/sidebar-sync.ts`);
  a hidden tab skips the tick, becoming visible is an immediate catch-up,
  and a dropped packet does not toast (`syncThreads`). A parked thread wake is not `running`:
  `waitingThreadIds` from the schedule list plus listing `waiting` paints the breathing clock,
  and `status.waiting` from GET/SSE (kept across `done`) is why the title bar says Waiting
  instead of Idle while `/goal` is parked on that wait. A live `schedule` event sets
  `waiting` even before GET returns; a stale GET cannot wipe a newer arm
  (`scheduleFetch` generation plus `keepArmedWakes` in `app-schedule.ts`). The goal banner repeats that this is
  parked, not an error. An armed-wait notice is pinned after the work of a finished
  turn so it does not sit above a long report.
  `askingThreadIds` unions listing `awaiting_answer` with the open
  conversation's live status.
  Live SSE folding lives in `src/store/app-stream.ts` so `app.ts` stays under
  1000 lines. `title` flushes immediately like `done`. A live `compacted`
  `phase: "start"` pulse sets `status.compressing` (title bar Compressing,
  composer banner) and does not mark the thread compacted; `done` clears it.
  `app-steer.test.ts` is why Interrupt-inject and per-bubble retract hit the
  open conversation and swallow `no_steer` / `404` races.
  `app-followup.test.ts` is why a duplicate send of the live turn does not sit
  in the tray, why Steer on a row / ⌘Enter of the same words empties that copy,
  and why a stale follow-up GET cannot resurrect a steered row.
  `followup-sync.ts` holds the generation token and the text match so
  `app.ts` stays under 1000 lines.
  A roster sidecar of `spawned` rows must not stop that paging or paint a
  wall of "Started" lines. Opening a worker off the live edge fetches
  `GET /agents/:agent/log` even when paging already claimed the log was
  complete but that worker's pane is still empty
  (`src/store/app-history.test.ts`). Opening paints the tail and resumes
  the stream after that seq (also here, split from `app.test.ts`). A jump
  to a turn still above the tail pages until that user row exists, and
  joins an in-flight sentinel page instead of bailing.
- **`src/store/app-goal.test.ts`**: `/goal` pursuing until `complete_goal` or
  `block_goal` (edit and resume from the banner, including Start after a
  mistaken complete); setting a standing
  objective while idle starts that turn; setting one during a run does not
  queue a second turn; `/compact` landing on the
  open conversation, with the hint hidden at 0% (and a compact with nothing
  open being a no-op); a live compressing pulse (`seq` 0) not marking the
  conversation compacted. A `done` then `goal_continued` (or `goal_resumed`)
  keeps `status.started_at` so the title-bar Working clock is the current
  turn, not a frozen 1s. An `error` then `goal_blocked` idles the composer
  and marks the objective blocked, so a crashed `/goal` turn is not still
  Working — a real refusal, not truncated tool JSON / `429` / a dropped
  stream (those `model_retry` then auto-continue). A `goal_idle` event holds the banner until a human message or Start.
  Split from `app.test.ts` so neither file crosses 1000 lines.
- **`src/store/app-plan.ts`**: `/plan` enters planning and starts a turn with
  an argument (not during a run, not when the argument is empty); a human
  edit PATCHes the body; Leave does not start a turn; Implement starts one;
  ask_user answers POST the open conversation; nothing open is a no-op. A
  live `plan_updated` folds into the banner thread. Split from `app.test.ts`
  so neither file crosses 1000 lines.
- **`src/store/plan-events.ts`**: thread flags for `/plan` events live here so
  `app.ts` is not the owner of that protocol.
- **`src/lib/transcript-review.test.ts`**: memory review notices, title and
  session-memory rows, rewind, `ask_user` cards, and plan notices. A merged
  skill is named in the transcript the same way a recorded one is. Split
  from `transcript.test.ts` so neither file crosses 1000 lines.
- **`src/lib/transcript-ask.ts`** and **`src/components/app/ask-card.tsx`**: an
  `ask_user` tool_call becomes a question block; the host injects Other; a
  result settles the card; a numbered choice plus Submit POSTs answers. The
  parser uses the host's normalized question ID (`test-window` → `test_window`);
  `internal/engine/ask_test.go` verifies that the PC rejects the raw key and
  accepts the normalized key;
  `src/lib/transcript-ask.test.ts` guards that wire compatibility. The paired
  phone's `src/lib/ask.test.ts`, `src/lib/phone-turn.test.ts` and
  `src/components/ask-card.test.tsx` guard the same ID plus explicit RPC
  rejection, retained Other draft and retry. `mobile/e2e/ask-answer.spec.ts`
  drives the visible rejection and retry against the scripted PC.
  The desktop
  card is `w-full` in the conversation column (no `max-w-*` dialog cap).
  A pending card says **Your answer needed**, carries a still `ask-mark`
  and `border-ask` (no ring or ping on the dialog); a settled card drops
  both. Sidebar and title-bar `ask-mark` still ping.
- **`src/components/app/plan-banner.tsx`**: Planning, editable markdown,
  Implement, Leave. Same one-line composer pin as the goal banner. Split
  from the goal banner so neither file grows past 1000 lines.
- **`src/store/goal-events.ts`**: thread flags for `/goal` events live here so
  `app.ts` stays under 1000 lines. `goal-events.test.ts` covers the idle hold.
- **`src/lib/usage.ts`** and **`src/components/app/context-meter.tsx`**: compact
  Cursor counts (`71.3K`), a percentage that is undefined without a window
  rather than a fake 0%, a saturating arc so an unknown window is not a dead
  empty circle, Discover merging per-name windows, selecting a listed name
  without stealing a sibling's window, and the ring's accessible name.
- **`src/lib/api.ts`**, against a stubbed `fetch`: the server's error message and
  code reach the UI instead of a bare status line, a tool list that arrives
  as `null` is read as an empty list rather than crashing the settings dialog,
  and `POST /compact` / `PATCH goal` hit the right paths.
- **`src/lib/marquee.ts`** and **`src/components/app/marquee.tsx`**: overflow is
  a width comparison (jsdom cannot layout a real line), a longer line gets a
  longer loop, and a live `MarqueeText` is marked `data-marquee="shimmer"` while
  idle text stays `"off"`.
- **`src/lib/work-fold.ts`**, **`src/components/app/work-fold.tsx`**, and
  **`src/components/app/swap-line.tsx`**: user mode groups consecutive
  thinking / tools / spawns; developer mode is a flat list.
  Answers stay visible and split the group. `ask_user` / errors / notices split the group. A live ticker is one
  current activity on the newest running turn's latest work row only
  (a pending tool beats a streaming thought; a live gap with neither is
  **Planning next moves**). An answer after that fold is the tail, so the
  fold keeps its count; Planning renders under a closed answer and stays
  off while the answer is still streaming. Older folds keep the thought / tool count.
  Copy is **Thinking** or the latest thought line, **Editing** / **Reading** /
  **Exec** `{name}` for file and shell tools. `SwapLine` always wraps `MarqueeText` so a
  long line still scrolls left-to-right; changing the activity key slides
  the previous line up.
- **`src/lib/chart-spec.ts`**: a `chart` fence body is JSON for bar / line /
  area / pie. Parallel `labels`+`values`, a values object, missing x/y,
  and x/y written as captions while the row uses other keys (including the
  literal keys `x` and `y`) still parse; a single row, an unknown type, and
  junk do not; a truncated
  object is incomplete so a live fence can wait; rows and series are capped.
  Two parses of the same fence compare equal so a live answer does not treat
  a new object as a new chart.
- **`src/components/app/transcript-chart.tsx`**: a valid spec is a figure with
  Chart / Table tabs (plot first; the table is the same rows, not a
  screen-reader-only dump); a live incomplete fence is a
  placeholder; fills and axes use `var(--chart-*)` / muted / border tokens
  so a theme switch restyles the same plot. Re-rendering with a new but equal
  spec object keeps the same SVG; the plot only redraws when the spec or the
  measured box size changes.
- **`src/lib/stream-markdown.ts`**: trailing `**` / `` ` `` / `~~` in the
  current prose region are closed so a streaming answer can render; an open
  fence is left alone (it is already a code block), a marker with no
  content yet is not turned into `****`, and an unclosed `$$` with a body
  is closed so KaTeX can paint live (`$HOME` is not treated as math).
- **`src/components/app/markdown.tsx`**, rendered in jsdom: a streaming
  `## heading` is a heading, trailing bold renders as `<strong>`, a
  finished answer is not rewritten, a `chart` fence paints, a `json` fence
  does not, a live truncated chart fence is a placeholder, a finished
  invalid chart fence stays code, and a closed chart keeps the same instance
  while later tokens append prose after it. A `go` fence paints keywords
  onto tokens and copies the original body; `cpp` maps onto the C
  highlighter; an unlabeled fence stays plain and still copies; `$n$` /
  `$$…$$` and a `math` fence render as KaTeX.
- **`src/components/app/transcript.tsx`**, rendered in jsdom: while `wait_agents`
  is pending the transcript shows the sub-agents it is waiting on, each one's
  live activity and the age the latest pulse gave it — the roll-up that keeps a
  running swarm from looking frozen — and clicking a row opens that agent. The
  heartbeat line is tested separately: it reports the turn's age and the number
  of sub-agents still working, keeps ticking on fake timers between pulses, and
  shows an animated pending line before the first pulse and renders nothing
  after the turn ends. The id parser
  behind the roll-up is also unit tested for half-streamed and malformed
  arguments. A streaming answer (manager transcript and a worker's panel) is a
  heading, not raw hashes. A live thought is a 10-line scrolling box that
  follows new tokens and fades at the top once earlier lines have left; the
  **Thinking** label sweeps (`MarqueeText`) until it collapses to **Thought**.
  Clicking the row hides it while tokens still arrive; later deltas do not
  force it back open.   User mode (`transcript.test.tsx`, `transcript-fold.test.tsx`, `work-fold.test.tsx`)
  folds adjacent thoughts and tools behind one `WorkFold`; answers stay
  visible and split the group. The live summary is a `SwapLine` of **Thinking**
  / the latest thought line, **Planning next moves**, or **Editing** /
  **Reading** / **Exec**, still on `MarqueeText`.
  Developer mode is the previous every-row log. Opening an `exec` row wraps the full command with shell
  highlighting instead of leaving it truncated (`transcript-exec.test.tsx`).
  A pending `exec` does not paint stdout until that click
  (`tool-row.test.tsx`); the latest line stays on the summary. A finished
  non-exec tool that was watched live folds unless the reader opened it.
  A refused `memory` write shows the refusal on the collapsed row, not only
  inside the disclosure. An exec still open when the turn is interrupted loses its spinner rather
  than running forever.   Hovering a user bubble or the last finished answer fades in
  the event clock without changing the row height (`transcript-message-time.test.tsx`); copy and a pencil sit
  under each user bubble (copy is hidden when there is no text); a denied
  Clipboard API still copies via `execCommand` planting `text/plain` on the
  `copy` event (`copy-text.test.ts`); a silent `true` is not Copied; edit opens
  the bubble in place and Send restarts from that `user_message` seq, clearing
  everything below (`transcript-user.test.tsx`).
  A compact notice stays a one-liner; `compact-notice.test.tsx` opens the
  briefing from the icon without pasting it into the row.
  Each user send carries `data-turn-nav` so the rail can jump (the thought
  fold is not inside that target); one turn
  keeps the rail hidden, two turns show it. A live thought's top fade is
  trapped in its own stacking context so it cannot cover the rail.   Unread steering is pinned below
  the heartbeat (`queued-steers`), not above the working line; a steer the
  manager has already read stays in the turn body. That pin has Interrupt
  (all unread), a pencil that hands one caption to the composer, and
  per-bubble Delete (`queued-steers.test.tsx`). An image-only steer has no
  pencil. Auto-follow unpins on
  wheel-up even inside the old 80px slack; new tokens then show a jump-to-latest
  control, and a skeleton→loaded remount still attaches the listener. Switching
  conversations re-pins even if the previous one was scrolled up; a layout
  scroll at 0 while the scroller is opening does not unpin.
- **`src/lib/follow-scroll.ts`**: whether the reader is at the live edge, whether
  a jump control should show (only after content arrived while unpinned), and
  whether a wheel started in a nested scroller (a thought, a tool payload)
  should leave the transcript pinned. `reset` restores the initial pin when the
  open conversation (or the open sub-agent) changes.
- **`src/lib/thought-scroll.ts`**: whether the top fade should show, whether
  new tokens should pin the box to the bottom — the same "do not yank a reader
  who scrolled up" contract as the transcript scroller — and whether a live
  thought stays open (`thoughtExpanded`: the reader's click wins over streaming).
- **`src/lib/ime.ts`**: Enter sends only when composition is settled. An IME
  Process key (`keyCode` 229), a live `isComposing` flag, or composition that
  ended in this frame (WebKit fires compositionend *before* the confirming
  Enter) must not send; leftover Latin staying as typed is the point.
- **`src/components/app/composer.tsx`**, rendered in jsdom: the thinking-level
  menu labels the empty default as "Default" and a set level by name, and it is
  absent when the server offers no levels, so an older server never draws a
  control that would send a meaningless choice.   The input is owned by the
  composer, not the app shell, so typing cannot re-render the transcript.
  A live `usage` pulse is read by the ring child (`ComposerContextMeter`),
  not by the textarea's parent, so an in-flight turn cannot rewrite the
  field while a CJK IME is composing. Auto-resize skips `height: auto`
  during composition (`resizeComposerArea`); the confirming
  `compositionend` measures once.
  Enter while composition is live, or on the key that just confirmed it, leaves
  the draft in the box; the next settled Enter sends. A CJK IME writing the
  committed string back after send is swallowed so a leftover Enter cannot
  queue the live turn; a different draft still sends.   The chrome is a fade over
  the transcript, not a top border, so a docked toolbar cannot regress in.
  Goal / plan / wait pins sit in the dock with the input; a slab behind
  that plate hides chip gaps, and the fade is only the join above it.
  The draft uses `--ui-font-size` (content type), not Textarea `text-sm`.
  The model control is always a switcher, even with one ready name.
  ⌘Enter marks the send as steer; Enter while running queues. The Queued tray
  (`queue-tray.tsx`) names the count, pulls a clicked row into the composer
  (the row leaves the tray; Enter while the turn runs queues it at the back),
  steers or drops a row after a confirm, and Clear queue asks first.
  A prefill also drops pasted images and pending files, so that Enter stays
  a follow-up.
  File drop onto the box (`composer-drop.ts`) arms a dashed overlay, then
  splits images into vision thumbs and other files into workspace chips
  (`composer-attachments.tsx`). A png/jpeg/gif/webp whose drop type is empty,
  a generic binary, or a UTI is still a thumb; the vision body claims
  `image/jpeg` (or the matching type), not `application/octet-stream`. An
  explicit non-image MIME stays a chip. A disabled composer ignores the drop.
  Send names the uploaded paths on the turn (`files` on `POST /turns`); a
  failed upload keeps the chip and does not start a turn.
  `/plan` waits for the work after picking the command (the box shows
  `/plan ` plus a hint); `/plan <task>` submits in one go. Picking **goal**
  writes `/goal ` the same way — wiping the token looked like a missed
  click. While a turn is running the corner button is Stop until the draft
  is submittable (`composerCornerActions`): text, a quote, a file, or an
  image replaces Stop with Send, and clearing the draft brings Stop back.
  Whitespace stays Stop. A `/goal` or `/plan` prompt that still needs its
  argument keeps Send disabled beside Stop; once the argument is there,
  Stop leaves. While `awaiting_answer` the placeholder is the Other prompt.
- **`src/components/app/goal-banner.tsx`**: Pursuing / Blocked / Paused / Done,
  Start when Paused, Blocked, or Done (not while Pursuing between sessions),
  a Paused/idle reason line that this is not an error and Start resumes,
  a Done reason line if the close was early,
  icon Start with an accessible name (Codex-density pin above the composer),
  inline edit that saves on blur and cancels on Escape, and
  elapsed time from `goal_started_at`. A failed-turn sentinel is localized
  and yields to the turn's public error when that is on the transcript.
  A parked wait does not reprint the wait essay — the schedule chip owns that.
- **`src/lib/models.ts`**: choice ids round-trip through a tab separator,
  `groupModels` keeps server order, and auxiliary choices skip blanks.
- **`src/components/app/model-settings.tsx`**, rendered in jsdom: providers
  start as a collapsed list (name, model, default badge) so a second
  endpoint is not a wall of fields; opening a row reveals Provider / URL /
  key / default; Add a provider appends a row and opens it; a settings
  search that matches a field opens the matching rows. Discover fills the
  default-model dropdown from the catalog. A catalog of two names gets two
  window fields — typing one must not write the provider fallback.
  Removing a provider asks first. A failed listing toasts over the sheet
  (× dismisses it) instead of a red line under the Models heading.
  The switch on a row takes that endpoint out of the composer; the last
  one cannot be switched off. The model button keeps a conversation's
  name when that provider has left the list.
- **`src/lib/composer-chrome.ts`**: the composer writes `--composer-pad` onto the
  conversation stage from its own height; a zero height (jsdom) leaves the CSS
  fallback so a unit test cannot collapse the transcript into the box.
  `composerPadPx` is dock height plus join air so goal / plan / wait pins
  stay in the dock (the slab behind it hides chip gaps) and the last answer
  is not under a % opaque hang. Fade overhang is px on the same stage so
  content font-size cannot grow a rem wash. `resizeComposerArea` skips the
  `height: auto` measure while `composing` is set, so an IME candidate
  window is not laid out on every preedit key. Overflow stays `auto`
  (`resizeComposerArea` returns whether the draft passed the cap). While it
  fits, the box uses `composer-scroll-fit` so the thumb stays hidden;
  `overflow: hidden` on an empty box is what made WKWebView drop paste until
  the field already had text. A paste of the draft just sent is not the IME
  echo that clears the box. A paste event that leaves the value unchanged is
  inserted on the next task (`recoverDroppedPaste`), not in the paste
  microtask — that ran before the browser inserted and wrote the text twice.
- **`src/lib/tool-view.ts`**: a built-in tool's JSON args collapse to the
  command / query / path the user needs to see, `execCommand` keeps newlines
  so an expanded row can show a heredoc, `exec` payloads become stdout
  plus a failed flag when the exit code is not 0, and `web_search` payloads
  become a list of hits. An `edit` or `write` summary is the path;
  `+N −M` lives on `ToolView.diff` (that hunk, parsed once) and the row
  paints it as a sibling of the path. An empty write is the path with
  no `+0`. A refused memory write puts the refusal on the collapsed
  row (`toolRowSummary`), not only inside the disclosure. Assertions
  check structure, not any particular query.
- **`src/lib/edit-diff.ts`**: an `edit` call's `search_block` / `replace_block`
  (or apply_patch `patch`) becomes line hunks. Shared prefix/suffix stay
  context; the middle is an LCS (capped so a huge replace cannot freeze
  the transcript). An empty `replace_block` is a deletion hunk (`−N`, no
  `+0`). A `write` call's `content` is all additions; empty
  content is still a hunk (zero lines). `clipDiff` keeps the first 400
  painted lines and reports the rest as hidden. The tool result is ignored
  — it is only a status sentence. CRLF matches LF. A path-only call has no
  hunk. `parseFileChange` dispatches edit/write and ignores other tools.
- **`src/lib/shell-highlight.ts`**: an `exec` command tokenizes into
  keywords / strings / flags / variables without dropping characters; a
  too-long line falls back to plain text.
- **`src/lib/math-fence.ts`**: `math` / `latex` / `tex` / `katex` fences are
  formulas; `go` / `chart` / empty are not.
- **`src/lib/source-highlight.ts`** / **`src/lib/source-lang.ts`**: a `read`
  body tokenizes from the path suffix into the same token kinds; concatenating
  tokens reproduces the file; markdown and unknown suffixes are left alone.
  A markdown fence info-string (`cpp`, `javascript`, `python`) maps onto
  the same dialects; `chart` / `math` / unknown tags stay undefined so the
  body is not guessed. A C `#include` line is a keyword, not an operator.
- **`src/lib/read-result.ts`**, a `read` tool payload becomes a file listing:
  newline-separated bodies, and the older flattened one-liners, both recover
  the path and the lines; other tool output is left alone.
- **`src/components/app/tool-row.tsx`**, rendered in jsdom: a pending `exec`
  does not paint `tool-output` until the row is opened (latest line on the
  summary); a finished exec stays collapsed unless the reader clicked; a
  pending `web_search` still opens and folds when the result lands; a
  finished `edit` or `write` stays collapsed (`+N −M` beside the path,
  muted until hover/focus) until the reader opens it, including while
  the call is still pending.
- **`src/components/app/tool-result.tsx`**, rendered in jsdom: a markdown `read`
  renders headings, a `*.go` `read` paints keywords onto tokens and keeps line
  numbers, an unknown suffix stays uncoloured, `exec` shows
  the full wrapped command plus stdout (and a role=alert error when it failed),
  `web_search` lists hits instead of dumping JSON, and an `edit` / `write`
  paints the reconstructed hunk (not `ok: replaced block` / `Updated file`)
  with add/delete rows. A failed call keeps the error above that hunk. An
  empty write shows the empty-file line and not `+0`. A 401-line write
  paints 400 rows plus the leftover count.
- **`src/components/app/source-code.tsx`**, rendered in jsdom: a numbered
  listing maps token kinds onto the syntax CSS variables.
- **`src/components/app/edit-diff.tsx`**, rendered in jsdom (via `tool-result`
  and `edit-diff.test.tsx`): add/delete rows carry `data-diff`, a `*.go`
  hunk still paints keywords, an empty write shows the empty-file copy,
  and `EditCountMarks` inherit the row until `group-hover` /
  `group-focus-within` split add from delete. Tokenizing is per painted
  hunk, not per table row.
- **`src/components/app/shell-command.tsx`**, rendered in jsdom: the expanded
  command wraps instead of truncating, and the collapsed preview stays one
  line.
- **`src/components/app/sidebar.tsx`**,
  **`src/components/app/sidebar-section.tsx`**,
  **`src/components/app/sidebar-slots.tsx`**,
  **`src/components/app/sidebar-thread-row.tsx`**, and
  **`src/components/app/sidebar-thread-group.tsx`**, rendered in jsdom
  (`sidebar-slots.test.ts` pins chrome-relative density, not rem): a leftmost
  rail (`dest-rail`) switches Projects (default), Conversations, Scheduled, and Clients so those lists are
  not stacked in one scroll or drawn over the task. The project list
  does not offer New conversation (that button is on Conversations) and does not show an unread count on the rail; there is no title-bar chrome row and no hide
  control — those live on the window title bar. Settings and the app menu
  (conversation width, developer view, theme, language, which other shells are open, build version)
  are icon buttons at the bottom of that rail, and they stay when the list column hides. Projects and Conversations are separate rail pages; each section still uses
  `sidebar-section-label`. Wrapping the project
  section in a second `px-2` is rejected. The list starts at
  360px, the arrow keys change that width (CSS variable, not a React `width`
  style), a remembered width is restored, and the resize strip sits on the
  right edge (`z-20`) with the aside stacked above the transcript (`z-10`) so
  the 4px overlap is not painted over by the main column. Dropping one Conversations
  row on another reports the new id order; a drag that starts on the row menu
  is ignored. A title click still opens when a dragstart races it; dragging
  the title past 8px reorders. There is no grip glyph. A pinned project
  topic sits in Pinned and still under its folder. The folder itself is
  not pressed; the open topic is `aria-current`. The folder icon is the
  fold control: open directory vs closed directory. A running
  conversation's progress sits in that same icon column, including after
  switching away and when a turn starts on a conversation you have not
  opened (`GET /api/threads` `running`, re-read while the window is
  visible), and an explicitly collapsed folder that still has a live
  turn keeps the progress on the directory glyph, clipped so the breathe
  animation cannot smear the icon. A blocked `ask_user` replaces that
  progress with a pinging question mark (`ask-mark`; the ping is allowed to
  overflow). Conversations,
  Pinned and nested titles keep a `sidebar-kind` spacer so they share a
  column with the project name. A section chevron is hover-only
  while that section is open. Folder and topic rows are `sidebar-row`
  (`--sidebar-row-height` from UI chrome size, not rem) with a 2px stack gap so the
  selected pill floats; row menus stay `icon-xs` so they cannot pad the
  list out. Content font size must not shrink that directory; UI size grows it
  (12px chrome → 26px row, 13 → 28, 16 → 34). Clicking Pinned, Projects,
  or Conversations folds that section (`aria-expanded`);
  a reload keeps the fold. A project folder and Conversations show at most
  five conversations from the last seven days; **Show more** reveals
  the rest and **Show less** folds them. The open or running
  conversation stays in the preview. Deleting a conversation asks first.
- **`src/lib/sortable.ts`**: the row is never HTML5-`draggable`. A title
  click still opens; a pointer move of 8px from the title reports the move;
  a twitch under that threshold is still a click. Synthetic row `dragstart`
  (jsdom / Playwright) still reorders. A dragstart with no `dataTransfer`
  is ignored. A disabled bind does not arm.
- **`src/lib/reorder.ts`**, **`src/lib/sidebar-groups.ts`**,
  **`src/lib/sidebar-preview.ts`**,
  **`src/lib/sidebar-collapse.ts`**,
  **`src/lib/thread-title.ts`**, **`src/lib/sidebar-sync.ts`**: moving a row is a splice; Conversations and a
  project sort their own ranks, then unranked rows interleave by last
  activity so a stale global cannot sit above a ranked topic that just ran.
  Pinned order is `pinned_at`. A folder without an override follows
  the open conversation (or a selected empty project, or a folder that
  has a running conversation); an explicit collapse still wins. Garbage storage is
  an empty map. Pinned / Projects / Conversations default open; a remembered
  fold is the three booleans in `zwai.sidebar.section-expanded`. A
  sidebar group preview keeps the first five conversations active in
  the last seven days; older or extra rows sit behind Show more unless
  they are the open or running conversation. `mergeThreadList` takes
  listing `running` for background rows and only overlays the open
  conversation; `startSidebarSync` re-reads the listing while visible.
- **`src/lib/sidebar-width.ts`**: missing or garbage storage is the default
  column, out-of-range values are clamped, a live drag paints
  `--zwai-sidebar-width` without writing storage, and a commit (pointer up
  or arrow key) is what remembers the width.
- **`src/components/app/resize-handle.tsx`**, rendered in jsdom: a right-edge
  strip widens on ArrowRight; a left-edge strip widens when the separator
  moves left (the right panel). A pointer drag does not start a text
  selection. Width stops at the min and max. Keyboard (and pointer up) fire
  an optional commit so the parent can skip React state until the gesture
  ends.
- **`src/components/app/header.tsx`**, rendered in jsdom: the title bar always
  has a Hide/Show conversations control, pads for traffic lights on desktop,
  sizes the leading cluster from `--zwai-sidebar-width` while the list is
  open so the title starts with the transcript, is marked as window chrome so
  a double-click can zoom, and a conversation in a project prefixes the title
  with the project name on the same line — which directory the tools are
  pointed at is otherwise invisible. The model name is not repeated here; it
  lives on the composer. A running turn counts from `status.started_at`
  (hours included); a missing start time is `Working` with no invented 1s.
  `awaiting_answer` is **Your turn** with `ask-mark`, not Waiting / Working;
  `awaiting_continue` stays Waiting. Conversation width, developer view, who
  else is connected, theme and language live in the sidebar app menu, not on
  this bar.
- **`src/lib/settings-persist.ts`**: edits coalesce into one `PUT` after
  400ms; `flush` writes immediately; a failed write does not block the next.
  The `ui` object always carries locale, font, size, column width and palette so a
  swarm edit cannot reset General.
- **`src/lib/appearance.ts`**: chrome tokens (`system`/`serif`/`mono`,
  `small`/`medium`/`large`, `comfortable`/`full`, `zwai`/`fofa`) map to CSS variables and
  `data-*` attributes. Junk becomes the current defaults. A `localStorage`
  cache paints the first frame; `GET /api/meta` is the source of truth.
  `toggleContentWidth` is the app-menu / ⌘K flip. `--content-gutter`
  shrinks in `full` so the column sits against the sidebars. UI size
  writes `--chrome-font-size` and the directory density tokens
  (`src/lib/chrome-density.ts`); content size writes `--ui-font-size`
  (`ui` follows chrome) and must not change row height. Code size writes `--code-font-size`.
  `palette` writes `data-palette` so `index.css` can swap the FOFA token sheet
  (DESIGN 7.2 / fofa.info: page matches the list, cyan `--primary`, blue
  `--brand-from`, composer `[data-composer-box]` gets the official search rim).
- **`src/components/app/theme-cards.tsx`**, rendered in jsdom: Settings →
  General paints System / Light / Dark preview cards and a Color theme
  menu (ZWAI / FOFA). The cards restyle with the named set.
- **`src/lib/chrome-type.ts`**: the shared chrome-size regular class for window
  chrome (sidebar, Settings, title bar, composer controls), plus the
  muted opaque chip (`composerPinClass`) for goal / plan / wait above
  the composer so transcript lines cannot show through. Content type includes `--ui-font-size` so it beats
  Textarea `text-sm`.
- **`src/components/app/settings-dialog.tsx`**, rendered in jsdom: Settings
  is a full-page sheet (`h-dvh`) with **Back to app**, a labelled search
  box, and a left rail of tabs. Opening the sheet lands on **General**
  unless the caller passed `initialSection` (the model picker uses Models).
  The dialog title is screen-reader only —
  the visible page heading is the section name (General, Models, …). There
  is no Save/Cancel: edits debounce into
  `PUT /api/settings` (locale rides along) and **Back to app** flushes.
  Searching a Swarm-only word jumps to that
  section and hides Models. The Personality tab writes personal preferences
  into `personality.instructions`. The Swarm tab exposes compact keep-messages,
  the auto-compact token budget. The selected page is a `tabpanel` switch
  (not Radix `TabsContent`, which hid every pane after settings loaded).
  Every page scrolls with `overflow-auto` and
  bottom padding, so an outline at the
  end of the tab (Add a provider) is not clipped. `overflow-y-auto` is
  rejected because it would leave the shared `overflow-hidden` in place.
  With `trafficInset`, an `h-12` `data-drag-region` strip sits above the
  rail so **Back to app** is not a descendant of that chrome (macOS
  traffic lights live there); a browser sheet has no strip. The sheet is
  `modal={false}` so Radix does not `aria-hide` the conversation underneath.
  A failed write toasts and keeps the sheet open.
- **`src/components/app/settings-remote.tsx`**, rendered in jsdom: a pairing
  offer paints a QR; a stored host token is never echoed; a failed offer
  toasts over the sheet instead of a red line under Phone; **Keep this
  computer awake** defaults on and toggles `remote.keep_awake`. A binding
  with a reported model paints that line, not a bare fingerprint. While the
  QR is up, the bound-phone list is re-read; a phone that appears on a later
  read is painted, and a failed read does not clear phones already shown.
  Before the QR, that watch does not run.
  `src/lib/remote-binding.ts` is the title / last-seen join.
- **`src/store/settings-sheet.ts`**: sidebar / ⌘, / ⌘K open General;
  a provider shortcut opens Models.
- **`src/store/toasts.ts`**: same-id replace, a cap of 4, `toastError` as
  the settings failure helper.
- **`src/components/app/settings-field.tsx`**, rendered in jsdom: a row
  puts the label left of the control; a query hides non-matching rows;
  a section with no remaining rows is omitted. Title and hint share
  chrome type (`--chrome-font-size` / `font-normal`; color is the hierarchy).
  Menus hug their value (`w-auto`, `bg-background`, same chrome type). The
  page column is centered (`max-w-3xl mx-auto`, `gap-8`).
- **`src/lib/file-tree.ts`**: a flat workspace listing becomes a nested tree
  (missing parents are synthesised), filtering keeps ancestors, collapse hides
  children unless a query is forcing matches open, and a unique directory
  chain starts expanded while a repository-shaped root stays collapsed.
  Keyboard actions are pure: arrows move, Right expands, Left collapses or
  walks to the parent, Enter opens a file.
- **`src/components/app/files-tab.tsx`**, rendered in jsdom: the filter is
  labelled, a repository listing starts collapsed, clicking a folder toggles
  it, a query surfaces a nested file without expanding first, download/delete
  stay off directory rows, deleting a file asks first, and the tree takes
  arrow keys.
- **`src/lib/agent-failure.test.ts`** checks exact worker watchdog identity and
  duration, known worker iteration and provider stream-idle limits, the exact
  output-budget error, and absence of speculative settings
  links for generic deadlines, cancellation, another worker's timeout or unknown
  errors. **`src/components/app/agent-failure.test.tsx`** checks the visible cause
  entry, the error card outside the log scroller, bounded scrolling for a long
  error, localized missing-reason text, and opening the exact Swarm input without
  arming a manager retry, plus the Models entry without a guessed provider.
  **`src/lib/transcript-failure.test.ts`** checks live and
  replayed parent-failure origin, preservation of an independent worker error,
  clearing stale causes on resume, and existing cleanup/human-stop semantics.
- **`src/components/app/panel.tsx`**, rendered in jsdom: opening a sub-agent
  puts the back row *beside* the scroller, not sticky on top of it — dragging
  the transcript must not paint through the back button. The worker log
  lands at the latest line, re-pins when switching workers, follows tokens
  at the live edge, and a wheel-up leaves the viewport put until Jump to
  latest. A prompt control opens the worker's recorded instruction and is
  absent when the event only stored the role name. The roster shows the
  `agent_id` `resume_agent` takes, not a `#n` badge. Two workers that share a
  role still show an id that is not that role plus a number. The list opens
  newest-activity first. Created, last update, and the name on the row each
  reverse; a worker with no timestamp stays last; running and finished stay
  in their sections (`src/lib/agent-roster-sort.ts`). The resize strip sits above that chrome (`z-20`) and the arrow
  keys still change the width. A pointer drag on the strip must not start a
  text selection. The Files pane is a flex column (`overflow-hidden`) so the
  filter stays put while the tree scrolls. Inactive Files/Agents panes are
  `hidden` when Memory is selected, and the active pane is an opaque
  `bg-card` so a leaked flex sibling cannot paint through the skill list.
- **`src/lib/selection.ts`**: while a gesture holds the pointer, existing
  ranges are cleared and `selectstart` is cancelled, then the previous
  `user-select` is restored. Dragging a panel over a transcript is a selection
  as far as the browser is concerned.
- **`src/lib/titlebar.ts`**: a double-click on the desktop title bar (not on a
  button in it) sends `wails:drag:doubleclick` so the native window zooms; a
  browser has no bridge and the click is left alone. Dragging is native and is
  not this module.
- **`src/lib/slash.ts`** and **`src/components/app/slash-menu.tsx`**: `/` as a
  token (start of the box, after whitespace, or after CJK — no space yet)
  is a menu; a space after the name makes it a submit. `foo/bar` and
  `https://` are not a menu. A known command name followed by a non-ASCII
  token is a submit too, so a CJK objective glued to `/goal` is not a user
  message, and a fullwidth `／` or IME punctuation `、` is still a slash
  (the composer rewrites those to `/`). `/plan` is in the same catalog.
  Picking a command that needs an argument completes `/name ` in the box
  and keeps the textarea focused (menu `mousedown` is cancelled so the
  click cannot steal the caret); wiping the token is not a pick.
  Filtering is
  prefix-or-contains, compact's hint is a percentage (hidden when used is 0),
  and unknown names are not commands. The menu is a listbox; the textarea
  owns the keys.
- **`src/lib/stream.ts`**, against a fake `EventSource`: every kind the server
  sends is subscribed to (including `goal_session`), a review that lands after
  `done` is delivered, the event name wins over a disagreeing payload, and a
  connection that dropped on its own is not reported as a failure. The first of
  those is a regression test: a kind missing from the list is stored, traceable,
  and invisible until the page is reloaded. Live session fold depends on this —
  a `goal_session` that only exists in the database looks like a TurnFooter
  until refresh.
- **`src/store/projects.ts`**, against a fake API: a project deleted elsewhere
  stops being selected, a slow memory response for a project that is
  no longer open is ignored (one project's notes under another's name is worse
  than none), a refused create or edit reaches the dialog so it can show the
  message against the field that caused it, and a memory reload copies the skill
  index onto the project so the Memory tab updates without a second listing. A
  tidy click folds overlapping skills, copies the new index onto the project,
  and keeps the tidy card when the same project's memory is re-read (a catalog
  tidy records `memory_review`, which would otherwise wipe the result). The
  card is per project: a slow tidy does not paint onto the project now open,
  opening the project that was tidied shows the in-flight progress or the
  finished report, a failed tidy stays on the project that asked, a memory
  read that started during the tidy does not put the pre-tidy list back, and
  dismissing the card or deleting the project does not bring it back.
- **`src/components/app/project-dialog.tsx`**, rendered in jsdom: a rejected
  working directory is shown under that field and the dialog stays open, a
  generic save failure toasts (× dismisses it), a
  nameless project cannot be created, the instruction textarea uses the same
  `border-input` chrome as a one-line field, and the memory switch is disabled
  when the install has memory off.
- **`src/components/app/project-list.tsx`** and
  **`src/components/app/delete-project-dialog.tsx`**: the folder is not
  pressed (the open topic is `aria-current`), each row's menu is named after
  its project, a hover control on the row starts a conversation in that
  project (named after it, so it cannot collide with the list's New
  conversation), conversations nest under an expanded folder whose glyph
  is an open directory (closed when collapsed), a running topic's progress
  sits in that icon column (and stays there after switching away; a
  collapsed folder that still has a live turn keeps the progress on the
  directory glyph, clipped so the breathe animation cannot smear the
  icon), skills are opened from the row menu rather than
  listed under the name, the section does not add a second horizontal gutter
  on top of the sidebar scrollport, a sixth topic (or one idle longer than
  a week) sits behind **Show more**, dropping a project row on another reports
  the new id order (title drag, no grip), a name click still selects when a
  dragstart races it, and the delete dialog says that the conversations and
  the memory go too while the user's own directory does not.
- **`src/components/app/confirm-delete-dialog.tsx`**, rendered in jsdom: Cancel
  leaves the thing in place; the destructive button is the only path that
  fires onConfirm; a closed dialog is not in the document.
- **`src/components/app/memory-panel.tsx`**, rendered in jsdom: notes and their
  budget are shown, **Save notes** is absent until the draft differs from what
  is stored and leaves again after a save or a revert, a failed save stays on
  screen, a skill's body is fetched only when it is opened, a skill named by
  the sidebar is opened and fetched on arrival, an opened body stays inside
  its card (overflow clipped, the next skill still follows it in the list)
  rather than overlaying Files, notes that arrive from a
  review are adopted unless the user is mid-edit — in which case a conflict
  banner keeps what they typed and offers Reload — and a write that landed
  while the tab was closed is a badge, not a silent panel. Deleting a skill
  asks first. **Copy** writes that skill into another project (a new name is
  optional); **Copy all skills** appears when there is more than one. With
  nowhere else to put it, the dialog says to create a project. A copy shows
  where it came from. **Update** pulls when the source moved and this copy
  did not; when both changed it asks before replacing the local text.
  **Tidy skills** sits on the Skills heading. While the model
  runs, the bar is an estimate that stays under full and the assistant prose
  scrolls in a short box; then a report of merged / deleted / created /
  patched names plus counts. A model that kept the catalog still shows the zeros.
- **`src/components/app/skill-tidy-card.tsx`** and **`src/lib/skill-tidy.ts`**:
  the bar is `tidyEstimateRatio` (capped until the POST returns) and the
  stream box is height-capped; the finished card lists what was merged,
  deleted, created and patched; dismiss clears it.

## End-to-end tests

```bash
cd frontend
npx playwright install chromium   # once
npm run e2e
npx playwright test --headed      # watch it happen
npx playwright test e2e/shell.spec.ts -g "settings"
```

Playwright starts the real server itself (`go run ./cmd/zwai web --mock --no-open`),
so a run exercises the whole stack: gin, the engine, SQLite, SSE and the built
front end.

**The data directory is deleted before the server starts**, so every run is a
first install. This matters: a config file written by an earlier run has already
been normalized, which once hid a crash that only happened on a fresh one. The
wipe is part of the `webServer.command` rather than a `globalSetup` hook, because
Playwright starts the web server first and a hook would delete the directory the
server had already opened. For the same reason the suite does not reuse a server
that is already listening on the port. `ZWAI_MOCK_WORKER_DELAY_MS=1500` holds
each mock worker before its first tool call so `wait_agents` stays pending
long enough for Steer; unit tests leave it unset.

| spec | covers |
|---|---|
| `e2e/agent-failure.spec.ts` | replayed failure details remain visible outside a long worker log; the exact watchdog setting receives focus, saves and survives reload; an ambiguous provider deadline has no guessed repair link, and a parent-turn failure keeps its origin and cause after replay |
| `e2e/agent-roster.spec.ts` | Agents opens on last update, newest first; Name flips between Z to A and A to Z and the rows follow the label; Created flips between oldest and newest |
| `e2e/worker-roster-history.spec.ts` | opt-in loopback model adapter: a later turn queries an existing finished worker, emits no new spawn call/event, survives reload and exposes its Trace |
| `e2e/work-fold.spec.ts` | user-mode work folded behind one live ticker (`work-fold` with the spinner, not every finished fold); click expands the thought box and click again collapses it |
| `e2e/conversation.spec.ts` | a full swarm turn, user-mode work folded behind one live ticker (`work-fold` + `swap-line` + `MarqueeText` shimmer or left-to-right scroll), a live status line marked as sweeping while the turn runs, opening a sub-agent (back control beside the scroller, not sticky on it; system prompt from the chrome; log at the live edge; a collapsed `write` opens to the file body, not `Updated file`), a generated sidebar title from the opening message (not the raw request, not a transcript row), a chart in the scripted answer with Chart/Table tabs (reload returns to the home page with nothing selected; reopening the row replays the chart), scrolling up mid-stream leaving the viewport put and a jump-to-latest control returning to the live edge (developer view, so spawn rows exist to overflow), switching conversations landing at the latest turn rather than the top of the history (latest jump-rail tick current), context carried across turns, jumping to an earlier user message from the left rail (latest tick current while idle at the live edge), Enter while `wait_agents` is pending queuing a follow-up until the turn finishes, the corner button swapping from Stop to Send once that draft is typed and the click queueing without leaving Working, **Steer** on that queued row injecting and emptying the tray, **Steer** after Stop starting that leftover row as the next user turn, pulling a queued row into the composer and Enter queueing that edit at the back of the FIFO, a pencil on unread steering doing the same, **Steer** (⌘Enter while `wait_agents` is pending) pinning unread steering under the working line with Interrupt and Delete, retracting an unread steer so the turn stays Working, Interrupt aborting the current tool without cancelling the turn, **Stop** while a tool is in flight leaving no spinner next to the interrupted banner, hovering a user request or the last finished answer fading in the reserved event clock without a layout jump, copying or editing a sent message in place so Send restarts from that bubble and clears everything below, file upload appearing in the Files panel with the user bubble naming `uploads/brief.txt`, collapsing a workspace directory in Files and filtering to a nested file, dropping a file and an image onto the composer (overlay, then a workspace chip vs a vision thumb), the turn id on the Trace summary with the event log folded until Full log, an IME-confirming Enter leaving the draft in the box, the manager tool-round cap pausing for Continue/Stop instead of dumping eino's iteration error, and switching the catalog model from a grouped searchable picker (Refresh models / Edit providers) so a reload still sends that name, and the composer context ring plus Trace usage after a turn (reload keeps the ring; the snapshot never lands as a transcript row), `/` listing goal, plan and compact without a 0% hint on an empty chat, pinning a standing objective, starting it from the banner without a human message, editing it in place, compacting without rewriting user bubbles (an icon opens the briefing), auto-compacting at a low token budget with a visible compressed notice and the same briefing icon, a scripted run with a goal finishing as Done, and a one-round ReAct slice leaving a standing objective running until Done instead of pausing it as two Worked-for sessions, `/plan` (`e2e/plan.spec.ts`) showing a Planning banner and an `ask_user` dialog that spans the conversation column (**Your answer needed**, still `border-ask`, no ring or ping on the card; **Your turn** on the title bar with a pinging `ask-mark`; a numbered choice then Submit continues the same turn), then Implement remounting work and leaving planning |
| `e2e/quotes.spec.ts` | quoting selected transcript text into the next send as an editable composer annotation (count chip at rest, hover for the snippet, not `<selected_text>` tags in the bubble), and Add to chat still landing while a turn is streaming (user-mode work fold, not the inner thought box) |
| `e2e/skill-copy.spec.ts` | a skill copied from one project into another stays on its own file: the copy names where it came from, **Update** brings a later source change across, **Cancel** keeps a local edit when both sides changed, **Replace with source** then takes the source text, and the source file does not gain the local edit |
| `e2e/library-skills.spec.ts` | a conversation outside a project adds a skill under Shared skills (no Notes); the test identifies that skill from this turn's `memory_review` trace even when other reviews write at the same time, then Copy puts it into a project whose origin says it came from the shared skill library |
| `e2e/projects.spec.ts` | a project created from the sidebar, a conversation started from the project row that says so with the project name prefixing the title on one line, the review named in the transcript without opening a tab, **View skills** on the project menu opening the Memory tab with that skill expanded and in view (body inside its card, not over Files), the notes in the panel without a reload, the review in the same Full log as the turn, a second conversation starting with the first one's memory, a hand-edited note surviving a reload (Save notes absent until the draft changes), a deleted project taking its conversations with it after a confirm, the Memory tab not leaving a blank Agents pane above the notes or clipping Skills off the window or painting inactive Files beside Memory, Review now saying when there is nothing to review, Tidy skills saying when the catalog is empty (counts, no fake model review) and asking the model when a skill is already there (scan/review, then a reviewed report with a non-zero scanned count), hovering a project row revealing a new-conversation control that starts one in that project rather than Conversations (the folder is not pressed; the open topic is `aria-current`; the folder glyph is open when expanded and closed when collapsed; a running conversation's progress sits in that same icon column; topic names sit under the project name; there is no drag-grip glyph), pinning a project topic to the top across reload, dragging a project pinning that order across reload, a sixth topic in the folder sitting behind **Show more** until it is opened, and a running conversation keeping its sidebar progress after switching to a new conversation (an explicit folder collapse keeps a clipped breathe-dot on the glyph, not a smear) |
| `e2e/markdown.spec.ts` | the scripted answer paints a tagged `go` fence (Copy code + syntax colour), `$n$` as KaTeX, a GFM table that stays inside the conversation column (not under the side panel), and a heading rendered as a heading while the turn is still Working |
| `e2e/goal-resume.spec.ts` | `/goal` on the mock provider reaches Done, then **Start** on the banner reopens pursuit (Working) |
| `e2e/schedules.spec.ts` | a standalone wait created from the Scheduled page in the main column (New task opens the right drawer, Task only — the inbox name is generated — Repeat → On an interval, Every (seconds) 60, Add wait; Agents/Files/Trace rail hidden; Expand fills the page (`data-expanded`) then Collapse restores the list), click the row to open the editor, Run now, Escape back to the conversation, unread / Open findings landing on the minted conversation whose title matches that generated name, with a `Scheduled check.` chip and no user bubble of the protocol wrapper; REST create then click-to-edit title/prompt/cadence and Save; a REST `kind=thread` wake on the open conversation showing the composer banner with Run now and Cancel wait, a breathing wait clock on the sidebar row and **Waiting** on the title bar, Cancel wait removing the chip and returning Idle, Run now starting Working, hiding the wait banner, a `Scheduled check.` chip, then Waiting again with the clock once the check finishes. Mock provider, no `ZWAI_MOCK_SCHEDULE_WAKE` |
| `e2e/remote.spec.ts` | Settings → Phone: Hub URL, no Host Token field, Event text on the phone, Events on the phone, Keep this computer awake, Bound phones empty copy, Show pairing QR, no QR pixels while the hub is unset; the failure toasts over the sheet in viewport (× dismisses it). A stubbed binding paints the reported model and last-connected, not a bare fingerprint. A binding that appears on a later list read while the QR stays up is painted without leaving Phone. |
| `mobile/e2e/scan.spec.ts` | Capacitor shell Scan QR opens a live viewfinder (four corners, a beam whose `scan-beam` animation is running, a fake-camera preview); junk paste errors; a syntactically valid URI uses the same bind path; a saved ticket shows host chips and Connecting, not the scan form; Add a PC is a sheet |
| `e2e/clients.spec.ts` | a long local agent session opens on the live reply, **Earlier** loads the lines above it, and that reply stays on screen |
| `e2e/output-budget.spec.ts` | an output-budget error in the transcript links to Settings → Swarm, focuses Max completion tokens, and shows Retry only after that sheet closes |
| `e2e/composer-paste.spec.ts` | a trusted paste into an empty composer lands once, and a second paste appends instead of doubling each insert |
| `e2e/settings.spec.ts` | Settings sheet: tool catalogue on a never-saved config, round-trip through the config file, per-note memory cap, personality, pinning a title-generation model, discovering models into the default dropdown, a failed listing toasting over the open provider, switching a provider off so it leaves the composer list while the open conversation keeps that name (switching it back lists it again), **Back to app** on a short window when Swarm is long, the Add-a-provider outline inside the Models scrollport, semantic search off by default, local agent tasks hidden until Clients is enabled (empty directories, three tool groups, no task rows), Color theme / font / conversation width, directory rows tracking UI size not conversation size, chrome language switching (restored to English) |
| `e2e/shell.spec.ts` | launch with an existing conversation stays on the home page (no row selected, no transcript), keyboard shortcuts (including hiding the conversation list, `⌘F` find in the conversation, and `⌘J` / the title-bar terminal opening a PTY in the conversation workspace — and in a project's working directory when the conversation belongs to one), dragging the conversation list and the side panel without selecting transcript text (the list width is remembered across reload and the title-bar leading cluster tracks it), the composer sitting on the transcript with a fade instead of a dock hairline (pins and the box share one slab; the join sits on that plate), Projects and Conversations sharing one left gutter (conversation titles in the icon column), collapsing Conversations so its rows stay hidden across reload, the Conversations header icon starting a conversation outside a project, an external link opening a new window instead of replacing the app, ⌘K finding a conversation by words in its body, theme switching persisted, the app-menu width control filling the pane in wide mode and restoring the reading column (also persisted), renaming a conversation and deleting it after a confirm, and dragging a Conversations row pinning that order across reload |

E2E tests run against `frontend/dist`. Playwright starts
`go run ./cmd/zwai web --mock --no-open`, which rebuilds that bundle when the
TypeScript sources changed; `make e2e` still runs `go generate ./frontend`
first so the server is not stuck on a cold Vite build inside the 180s
webServer timeout. If you invoke `npx playwright test` directly, a stale
`dist/` is rebuilt on server start.

The progress pulse has no positive E2E assertion on purpose: even with the
worker pause, a scripted turn usually finishes near the 5-second default
interval, so a test that waited for a pulse would pass or fail on timing. The
suite checks that a submitted turn immediately has a spinning response line and
that a finished turn leaves no pulse behind — the
regression that actually bites is an unhandled event kind landing in the
timeline as a raw row, or the heartbeat outliving the answer. That pulses reach
a client at all is verified in the engine and reducer tests.

## Manual checks that no automated test replaces

- **The desktop window**: `go run ./cmd/zwai desktop`. Check that the window
  opens, the UI loads, a turn runs, an upload opens a native file dialog, and
  "Show in Finder" works. A link in the transcript must open the system
  browser, not replace the window. The Dock / taskbar should show the rounded
  app mark at the same size as a bundled `.app`, not a square canvas, not a
  full-bleed tile, and not the generic Unix-exec glyph.
  Double-click the title bar: the window should zoom,
  then restore on the next double-click. On macOS the traffic lights and the
  sidebar toggle must share one baseline in that title bar; New conversation
  starts on the row below.   Opening Settings, **Back to app** must sit on the
  row under the lights, not in the drag strip. On macOS, the first desktop
  launch may prompt for Local Network access (LAN model endpoints); denying
  it looks like `no route to host` while Terminal curl still works. ⌘Q should return the shell promptly without
  `Window #1 not found` warnings. Wails cannot be driven by these tests.
- **CJK IME**: leftover Latin confirmed with Enter must stay in the composer;
  the next Enter sends. The desktop window is WebKit, which fires
  `compositionend` before that key — Chromium unit tests cannot replace a real
  input method.
- **A real model**: the offline provider proves the plumbing, not that a prompt
  works. Run one real conversation before shipping a change to the manager
  prompt or the toolset.
- **A UI change**: look at it, in both light and dark, before and while a turn
  runs. The panel, the transcript and the composer all change shape mid-turn.
- **Turn boundaries**: `frontend/src/components/app/transcript-session.test.tsx`
  and `frontend/e2e/schedules.spec.ts` check finished desktop sessions;
  `mobile/src/lib/transcript.test.ts`, `mobile/src/lib/session.test.ts`,
  `mobile/src/components/thread-blocks.test.tsx`, and
  `mobile/e2e/walkthrough.spec.ts` check host completion timestamps across
  hidden scheduled input and history paging (Issue #59).
- **The phone apps**: `make mobile-ios` / `make mobile-android`. Scan QR opens
  a live viewfinder (frame, sweeping beam, chime) on the pairing QR from
  Settings → Phone. Paste is the same URI when the camera is missing. After bind, a live turn (or the last thread this phone opened)
  opens; a later launch with saved tickets shows host chips and a connecting
  skeleton, not the scan form. Selecting another host chip stays on that PC's
  inbox even when it has a live or last conversation. Otherwise the inbox is the compact screen on the same conversation
  bus (send / follow-up / steer / stop / ask); the paired conversation's file
  tree and previews use read-only Pairlink RPC, while Settings, PTY and Trace
  stay on the PC. The hub hostname is typed on the PC, never shipped in the binary.
  Simulators: iOS can use a loopback hub; Android needs `adb reverse` onto
  that same hub port. `mobile/ios/App/AppUITests/BindFlowTests.swift` pastes
  the offer from `simctl pbcopy`, taps Back if a thread already opened, asserts
  `path=` plus the seed conversation, and Starts a thread. Its separate native
  menu test opens local chat through a saved or dummy loopback model, taps Menu,
  and verifies that iOS shows a version without Android's Check for updates.
  The command is in `mobile/README.md`; this closes the native acceptance gap
  that browser E2E and a successful simulator launch alone cannot cover.
  Android is the debug
  APK plus `adb reverse`, or `make mobile-android-release` for the release
  APK/AAB in `bin/`. Neither is in `make check`.

## When a turn misbehaves

Reproduce it, then read it:

```bash
zwai trace <turn-id>          # the timeline plus every model call
zwai trace <turn-id> --full    # untruncated text
zwai tui                       # interactive client of the same engine
zwai tui --goal "..."          # standing objective; starts immediately
zwai tui --task "..."          # one stored turn; this process can exit when it settles
```

Raise `log.level` to `debug` in the config for one line per HTTP request and per
event-stream decision. If a turn is slow, the model-call table at the end of
`zwai trace` usually shows why: `input_msgs` growing, billed tokens climbing,
or one call eating the wall clock.

## Adding tests

- New Go code needs unit tests with it, in the same package, above 90% of the new
  statements. If a function cannot be tested, split it until the untestable part
  is a two-line wrapper.
- A new endpoint needs a test for its success path **and** its refusals — a `404`
  for an unknown conversation, a `409` when busy, a rejected path traversal.
- A new event kind needs a reducer test, and usually an assertion in
  `TestNotifyKindsAreStableAcrossTheWire`: the names travel over the network and
  are stored in the database, so renaming one breaks replay of old conversations.
  Scheduled-task kinds (`schedule`, `schedule_fired`, `schedule_skipped`,
  `schedule_report`, `schedule_cancelled`) live in that table and in
  `frontend/src/lib/stream.test.ts` `wireKinds`.
- A new user-visible flow needs an E2E test. If it cannot be driven in the
  browser, it probably cannot be driven by a user either.
# Documentation structure

`make docs-check` checks required documents, feature and contract IDs,
cross-references, evidence paths, and links in the current snapshots. It is
part of `make check`; semantic correctness still requires review against
current entry points and tests.

### Phone composer bounds and iOS focus regression (C-017)

`mobile/e2e/walkthrough.spec.ts` checks the actual message/quote font size and
wait controls at 320, 375, and 402 CSS px. `mobile/e2e/composer-layout.spec.ts`
checks the actual submit/steer bounds and sends long input from PC follow-up,
New chat, and direct model chat with long model names. It runs on Chromium
and iPhone WebKit; install both with `cd mobile && npx playwright install chromium webkit`
before `npm run e2e`. JSDOM cannot prove WebKit focus zoom.
`mobile/e2e/ios-wait-layout.swift` drives the packaged Capacitor app: open a
parked wait, type, dismiss the keyboard, then check that Back is hittable at
its original vertical position and Run now remains inside the screen.
A second test checks the submission button after typing and actually submits
the draft without horizontal scrolling.
The Clients task-detail test opens a read-only session, drags down while its
underlying inbox is present, checks that the title/Back layer remains fixed
and hittable, then returns to the task list. The matching Chromium/WebKit
regression in `mobile/e2e/composer-layout.spec.ts` first failed with the detail
64 CSS px above the viewport when the inbox scroller was transformed; it now
checks full viewport bounds and Back navigation. This covers `C-018` and Issue
#45 without changing the user's installed app. A component test also verifies
that a touch inside the portaled detail does not bubble into inbox refresh.

Use an isolated iPhone 17 / iOS 26 simulator; set `IOS_LAYOUT_SIMULATOR` to its
UDID (`xcrun simctl list devices`). The fixture script copies the current
native app into a new temporary directory and enables the existing offline
walkthrough there. It does not alter shipping assets, sign, or upload anything.
The keyboard accessory fallback coordinate is specific to that simulator.
The formal `mobile/ios/App/AppUITests/BindFlowTests.swift`
`testNativeIOSMenuHidesAndroidUpdateCheck` taps the last keyboard accessory
button and asserts that the keyboard disappears before opening the menu.
iOS 26 exposes its checkmark as `selected`, so looking for a literal `Done`
fails before the menu assertions; those version/update-entry assertions remain.
For Issue #75, set `IOS_LAYOUT_QUERY='/?mock=1&tick=150&history=streamed-agents'`
when creating the fixture and select these three native XCTests from
`mobile/e2e/ios-wait-layout.swift`: `testLeftEdgeSwipeReturnsThroughAgentPages`,
`testLeftEdgeSwipeDismissesModelPickerBeforeThread` and
`testEdgeSwipeIgnoresShortVerticalAndNonEdgeMotion` (each with
`-only-testing:AppUITests/BindFlowTests/<test-name>`).
Start at the left screen edge rather than
using a centre-screen `swipeRight()`: the packaged app must return worker →
Agents → conversation → inbox, keep the root open, dismiss the current model
picker before its conversation and ignore short, vertical, centre and opposite
edge gestures. Browser tests of the same return hook prove page-layer behavior;
they do not prove recognition of the UIKit gesture.
For the hidden-history regression, set `IOS_LAYOUT_QUERY='/?mock=1&tick=0&history=hidden'`
when creating the disposable fixture and select only
`AppUITests/BindFlowTests/testHiddenHistoryShowsEarlierAnswer`. That XCTest
checks the packaged app's empty-state explanation, Earlier tap and recovered
answer; the default fixture leaves this one test skipped.
For Issue #59, use `IOS_LAYOUT_QUERY='/?mock=1&tick=0&history=turns'` and select
`AppUITests/BindFlowTests/testScheduledTurnBoundariesShowCompletionDates`.
It opens the seeded conversation inside the packaged app and checks both dated
results. The same fixture was installed in an isolated Android emulator as a
debug APK; its screen showed both dated dividers. These are native behavior
checks, not signed publication. On desktop, two turns against the offline mock
provider showed separate completion times in the native Wails window.

```bash
cd mobile
npm run build
npx cap copy ios
ios_layout_fixture="$(python3 scripts/ios-layout-fixture.py)"
xcodebuild -project "$ios_layout_fixture/mobile/ios/App/App.xcodeproj" \
  -scheme App -configuration Debug \
  -destination "platform=iOS Simulator,id=$IOS_LAYOUT_SIMULATOR" \
  -derivedDataPath "$ios_layout_fixture/DerivedData" CODE_SIGNING_ALLOWED=NO \
  -only-testing:AppUITests/BindFlowTests/testPhoneOutputPreview \
  -only-testing:AppUITests/BindFlowTests/testClientTaskHeaderStaysVisibleAfterPull \
  -only-testing:AppUITests/BindFlowTests/testQuoteActionBesideNativeSelectionMenu \
  -only-testing:AppUITests/BindFlowTests/testComposerStopsLiveTurn \
  -only-testing:AppUITests/BindFlowTests/testComposerSubmissionAfterTyping \
  -only-testing:AppUITests/BindFlowTests/testWaitLayoutAfterTyping \
  -parallel-testing-enabled NO -resultBundlePath "$ios_layout_fixture/layout.xcresult" test
```

Issue #40 was reproduced on the original 14px message editor after keyboard
dismissal: Run now ended at x=441 on a 402pt screen, and Back moved to y=-50.
The repair uses the existing `text-base` token for both message and quote editors;
manual pinch zoom is left available.

Issue #41's screenshot uses the older native model select. Replaying the
archived 0.1.15/build115 web assets in the disposable iOS fixture fails the
submission test: Send ends at x=434, and Follow-up at x=411, on a 402pt screen; tapping does
not clear the draft. This is the same focus zoom repaired by Issue #40, not a
second unreleased fix. Current source requires no additional layout changes;
the added tests preserve that evidence without changing the frozen v0.1.20 bundle.

## Unified release regression checks

`tools/test_release_ios_compliance.py` verifies the authorized app declaration is retained in source, a new source/build passes read-only preflight without borrowing an old answer, both boolean classifications are accepted, and invalid values, missing/mismatched IPA declarations or conflicting exact receipts prevent publication. The preflight regression asserts no archive, upload or private receipt write occurs.

`make docs-check` includes `python3 -m unittest discover -s tools -p "test_*.py"`. `tools/test_release.py` uses temporary source/ledger/IPA fixtures and injected Apple/command adapters to verify independent platform failures, source/version/tag gates, daily availability counting and retained signed candidates, remote reviews absent from local ledgers, import caches not dirtying release source, no duplicate upload, compliance refusal and retained exact-build confirmations after config switches (including wrong-source and conflicting-record rejection), signature/profile/version validation, archive/export settings, review waits, extra batch platforms, and old iOS completion after a newer batch is public. `tools/test_audit_pending_batch.py` verifies that an extra platform needs an integrated main commit and does not rewrite phone Issue scope. `internal/release/safety_test.go` verifies immutable attachment digests and independent platform assets; run `go test -race -cover ./internal/release/...`. `tools/test_release_ios_versions.py` checks marketing-version reuse across batches, explicit new-version overrides, frozen legacy/source/IPA recovery, separate batch/train receipts, IPA and remote train validation, review-free external availability, and no resubmission while review is pending. No test contacts Apple or publishes. Actual `release-check` checks the real source and private configuration without uploading; actual publication remains subject to native acceptance and batch gates.

`TestStatusGoesOfflineWhenHubCloses` holds the replacement WebSocket handshake with a channel after the real relay idle disconnect, then observes offline and releases the handshake to test Offer reconnect. A 10ms status poll can miss an entire fast reconnect; this fixture controls the handshake instead of changing production retry behavior or increasing timeouts. The prior fixture failed 1/10 isolated race runs; the synchronized fixture passed 20/20 and the remote package race suite (Issue #74).

The packaged `testNativeIOSMenuHidesAndroidUpdateCheck` scrolls the provider form
before tapping Save: provider visibility fields may move Save below a small
iPhone viewport. Native acceptance must reach the menu after configuring the
endpoint, rather than tapping an offscreen control.

## Phone output rendering (Issue #63)

`mobile/src/components/file-browser.test.tsx` exercises the real read/preview path; `text-file-preview.test.tsx` checks suffix selection, unknown/large/empty text, exact source preservation and inert HTML/SVG/MDX. `mobile/e2e/walkthrough.spec.ts` checks Markdown structure and syntax colors in light/dark; the packaged `mobile/e2e/ios-wait-layout.swift` `testPhoneOutputPreview` reaches both documents through Outputs. Reuse `scripts/ios-layout-fixture.py` and select only that XCTest. Android uses a disposable offline package and isolated emulator, inspecting the same reached screens; a browser pass or native launch alone does not accept the changed flow.

## Independent task coordination (`F-135`, `C-021`)

- `go test -race ./internal/engine ./internal/store ./internal/remote ./internal/provider -run 'Test(IndependentManagers|TaskMessage|TaskDiscovery|InboxCursor|PeerMessage|OfflineTaskCoordination)'` runs two managers through the actual Eino tool graph, bidirectional messages, provenance, cursors, idle/late delivery, no human-turn conversion and database failure.
- `cd frontend && npm test -- --run src/lib/task-message.test.ts src/lib/stream.test.ts src/lib/i18n.test.ts`; `cd mobile && npm test -- --run src/lib/task-message.test.ts src/lib/transcript.test.ts` check notices stay outside user steering and worker rosters.
- `cd frontend && ZWAI_MOCK_TASK_COORDINATION=1 ZWAI_E2E_PORT=8817 npm run e2e -- task-coordination.spec.ts` uses an opt-in offline model script, simultaneous real HTTP turns, live SSE and reload. Normal `--mock` behavior is unchanged and the opt-in spec skips without this flag.
- A send receipt proves storage only. Native packaged-window / paired-phone inspection remains required before recording UI acceptance; browser E2E is not native acceptance.
- `TestStandaloneManagerDoesNotAdvertiseHostInboxTools` keeps unmounted host tools out of the standalone manager prompt. `TestExistingConversationMigrationCreatesAWorkingInboxCursor` checks upgrade from the old schema. `TestRewindThenRestartDoesNotHideNewPeerMessages` proves the durable inbox remains readable after explicit history rewriting and restart; reading alone must not change conversation activity.
- Phone native provenance uses `IOS_LAYOUT_QUERY='/?mock=1&tick=0&history=peers'` with the disposable iOS fixture and `AppUITests/BindFlowTests/testIndependentTaskMessageHasPeerProvenance`. It checks the visible source notice and absence of Stop in the idle conversation. The matching phone E2E uses the same fixture; Android packages it into an isolated debug app and inspects the reached screen. These are behavior checks, separate from signing/upload and the desktop native window gate. The opt-in offline manager scenario is in `internal/provider/mock_task_coordination.go`; the existing manager control flow lives in `internal/provider/mock_manager.go` to keep production files below 1000 lines.

## Bounded worker watchdog recovery

Issue #65 first reproduced a fourth start after three real short watchdog
expirations, before and after `Stats` pruned the handle. `worker_timeout_test.go`
drives real manager resume/same-role spawn, streaming expiry, cancellation,
provider deadlines, concurrent resumes, archive/restore, success reset and human
retry. `internal/engine/worker_timeout_test.go` checks Run now, automatic fires,
real offline worker expiry, goal blocking, exact-plan pause, failed pause writes
and persisted legacy receipts across restart. `frontend/e2e/worker-timeout.spec.ts`
uses `ZWAI_MOCK_WORKER_TIMEOUT=1` and a one-second watchdog to observe three actual
starts of one ID, terminal error, no model retry, schedule pause, reload and Trace.
It also opens that worker's recorded failure, focuses and saves the watchdog
setting, and verifies the failed task remains idle with its original cause.
The separate `frontend/e2e/agent-failure.spec.ts` replay fixture tests long-log
visibility and failure-entry navigation without claiming to trigger a watchdog;
the real-watchdog script above provides that independent lifecycle evidence.
Default demo scripts are unchanged. Phone `history=timeout` checks existing error
rendering and absence of Stop; packaged iOS uses
`BindFlowTests/testWorkerTimeoutShowsPausedErrorInsteadOfRunning`, with the same
isolated fixture inspected on Android. Native desktop window acceptance and
public installer delivery remain separate gates; browser/launch is not proof.

The timeout desktop path also verifies that the authoritative paused schedule clears Waiting. `frontend/src/store/app-schedule.test.ts` retains the armed-wake/stale-GET guard and another active wake while rejecting a paused-only wait.

## Clients idle CPU and immutable snapshots (`F-223`, `C-014`)

`go test -race ./internal/clients ./internal/remote ./internal/server` covers
unchanged transcript read counts, changed/empty/deleted/replaced files, root
changes, status aging and Codex finish markers, concurrent readers and returned
row isolation. The Issue #66 targeted race run measured 92.7% package coverage
and 95.5–100% for the changed production functions. A 100,000-row chronological sort has a loose ten-second guard
against the former quadratic algorithm. `frontend/e2e/clients.spec.ts` reads a
600-session archive through the real HTTP/UI path, pages it, modifies one file
and deletes it without stale rows remaining.

`go test ./internal/clients -run '^$' -bench 'Benchmark(IdleTranscriptRefresh|CachedClientView|ChronologicalCatalogSort)$' -benchmem`
compares isolated long-transcript refreshes and cached page reads. It uses
temporary files and no model. The Issue #66 baseline on Apple M3 Max was
1.43 s and 1.32 GB allocated per unchanged 1,000-file refresh; the parsed-row
cache measured about 3 ms and 1.46 MB. Chronological 7,749-row sorting fell
from about 152 ms to 1.4 ms. These are development measurements, not a promise
for every filesystem or model-running CPU load. Inspect a real native Mac
window separately before recording native acceptance.

## Native Mac acceptance recovery (`F-430`, `C-010`)

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest tools.test_audit_macos_acceptance -v
make docs-check
```

The offline tests first observed the missing recovery CLI fail, then verify that
a ready guest remains actionable with the host locked, unlocked or unobserved.
A locked guest with an existing authorized account yields a login action;
successful evidenced unlock leads to window inspection. Only an actual failed
target-guest unlock with evidence yields a blocker. Without completed exact-package
acceptance, unknown session state requires fresh guest observation even when
historical successful or failed login evidence remains, regardless of the host lock.
The regression covers that decision through the CLI and a recovery sequence from
an old failure through re-observation to exact-package acceptance; a later unknown
session or host lock does not invalidate that completed acceptance.
Missing evidence, SSH/process success and mismatched candidate/window receipts stay
unaccepted. Passed observations require real window/behavior references and exact
version/source SHA/ZIP/hash/guest identity. Invalid JSON, unsupported credential
fields and non-boolean flags are rejected without printing private input values.
`make docs-check` discovers this regression with the existing Python tests.

These tests exercise a pure JSON decision function and CLI with temporary files;
they do not operate UTM, SSH, UI, login or an app. The caller must inspect the
actual window and behavior before writing the private observation. The audit
does not create that evidence, and its successful exit is not native acceptance.
When resuming pending acceptance after a user unlock, session change or
VM/control-channel change, the caller starts with `guest.state=unknown` and
rechecks the actual guest. Prior transport/login failures remain historical;
replace the active state and login result with observations from this recovery
before declaring a blocker. A host that locks again does not revive an old failure.

### Phone live-turn Stop action (Issue #69, C-002)

`mobile/src/components/thread-stop.test.tsx` checks that the composer itself can stop an empty live turn, whitespace still offers Stop, a typed follow-up and steer keep their send behavior, and asking/parked waits never become Stop. `composer.test.tsx` covers attachment-only and quote-only drafts and pending-request cancellation. The direct chat test holds a live stream, cancels it through the composer, and verifies the abort signal, retained partial text, no error, and restored Send.

`mobile/e2e/composer-layout.spec.ts` runs the same primary-action path on Chromium and iPhone WebKit, verifies the square icon, sends drafts through the existing layout flow, and stops the offline live turn. `mobile/e2e/ios-wait-layout.swift` adds `testComposerStopsLiveTurn`; the existing disposable packaged-app fixture checks the bottom native Stop is hittable, stops the turn and restores Send. `testComposerSubmissionAfterTyping` checks the typed follow-up separately. Use the fixture and isolated simulator commands above; app launch alone is insufficient.

Android uses the same existing offline walkthrough in a disposable packaged
app. `mobile/android/app/src/androidTest/java/com/lubyruffy/zwai/ComposerStopTest.java`
runs through AndroidJUnitRunner without added dependencies: launch the actual
Activity, find the bottom accessible Stop, perform its native accessibility
click, require disabled Send and no remaining Stop, and save screenshots plus
accessibility nodes under the target app's external files `composer-stop`.
Do not use the stock `ExampleInstrumentedTest` as this acceptance evidence.

Build and copy Android assets with `cd mobile && npm run build && npx cap copy android`.
Copy that native tree to a temporary `mobile/android`, with its sibling
`mobile/node_modules` linked to the same checkout's dependencies; exclude
`.gradle` and `build`. In this disposable copy only, inject the same
`/?mock=1&tick=0` and English locale initialization used by `ios-layout-fixture.py`
into `app/src/main/assets/public/index.html`. Shipping assets are unchanged.
On an isolated emulator, with JDK 21 and the existing Android SDK configured:

```bash
cd "$android_fixture/mobile/android"
./gradlew :app:assembleDebug :app:assembleDebugAndroidTest
adb -s "$android_test_device" install -r app/build/outputs/apk/debug/app-debug.apk
adb -s "$android_test_device" install -r app/build/outputs/apk/androidTest/debug/app-debug-androidTest.apk
adb -s "$android_test_device" shell am instrument -w \
  -e class com.lubyruffy.zwai.ComposerStopTest \
  com.lubyruffy.zwai.test/androidx.test.runner.AndroidJUnitRunner
```

Inspect the retained before/after screenshots as well as the test result. This
checks a local packaged-app behavior; it does not sign or publish a release.

### Live phone transcript identities and worker navigation

`mobile/src/components/agent-page.test.tsx` reproduces the live sequence-zero
answer → stored answer → tool boundary path repeatedly, then loads earlier
history through `prependOlder` and switches to the worker roster. It asserts
against the entire transcript, so detached manager nodes above the roster
cannot pass a check limited to the roster itself. `transcript.test.ts` checks
unique identities for manager/worker answers and thoughts, stable updates and
completion, consecutive replies without tools, and retained identities after
history rebuild. `session.test.ts` covers simultaneous live text during paging
and stored catch-up on reconnect.

The native and browser fixtures use generic streamed paragraphs, real
sequence-zero deltas, stored completion events, tool boundaries and an earlier
page; a static history fixture alone cannot reproduce this reconciliation bug.
Run `cd mobile && npm test` and `npm run e2e`. Native runs use the packaged app's
XCTest/AndroidJUnitRunner, load Earlier before entering Agents, inspect the
whole screen for manager text/work folds, and verify worker → roster → manager
return paths with screenshots.

Use `IOS_LAYOUT_QUERY='/?mock=1&tick=150&history=streamed-agents'` with the
disposable iOS fixture above and select only
`AppUITests/BindFlowTests/testAgentPagesExcludeStreamedManagerHistoryAfterEarlier`.
The native fixture spaces updates across frames. Start in its live thread and wait for `Agents (3)` before paging;
opening a completed static snapshot would miss the live identity regression.
For Android, inject the same query into the disposable native copy described
above, then select `com.lubyruffy.zwai.AgentRosterTest` through
AndroidJUnitRunner. Both tests inspect the reached roster and worker detail,
then return to the original manager transcript. Retain screenshots and the
accessibility tree alongside the test result and packaged asset hashes.
## Model API failure evidence (#71)

`go test -race ./internal/engine -run 'TestPublic|TestEmptyGatewayResponse|TestAPIAuthenticationFailure'` covers wrapped SDK/Eino errors, HTTP 502 empty-body decode failure, 400/401/403/429/5xx causes, bounded credential-masked public text and preserved context/first-byte advice. The integration test uses a local `httptest` HTTP server through the real SDK and engine: 502 yields three HTTP/model calls and two retries on the same failed turn; 401/403 yield one call and no retry. Stored turn/error text, original LLM errors, retry events and no-goal turn count are asserted independently. No network or real model is used.

`cd frontend && ZWAI_E2E_MODEL_API_ERROR=1 npm run e2e -- api-error.spec.ts` runs the desktop Web path against a local empty-502 HTTP endpoint through the real SDK. The flag selects this single test and starts the app without `--mock`; the regular offline-provider suite skips it. Check Chat, the same-turn Trace and Full log, return/reload persistence, three HTTP calls and two retries, and the retained screenshots/Trace JSON.

Phone `mobile/e2e/api-error.spec.ts` uses `history=api-error` to regress existing error-text passthrough on Chromium and iPhone WebKit, including return/reopen and reload. Native packaged XCTest entry is `AppUITests/BindFlowTests/testModelAPIErrorShowsHTTPStatusAndDecodeCause`, with `IOS_LAYOUT_QUERY='/?mock=1&tick=0&history=api-error'`; Android entry is `com.lubyruffy.zwai.APIErrorTest`. Check the reached screen and exact production-bundle hashes; fixture/replay tests supplement the real engine HTTP test, not replace it. A system ANR before the page is reached is an environment failure, never behavior acceptance.

Mac native verification runs the current packaged candidate against a local empty-502 HTTP model in a separate data directory, without `--mock`: verify terminal error, Idle state, same-turn Trace status and Full log, three LLM calls and two retry events, and save window/Trace evidence.


## First-byte timeout configuration and guidance

`internal/config/provider_timeout_test.go` covers the 30-second default,
missing/invalid legacy values, save/reload and a first-byte wait longer than the
independent stream idle timeout. `internal/server/settings_timeout_test.go`
checks actual PUT/GET settings round trips, including an older client's omitted
field preserving the saved value and an explicit nonpositive value resetting it.
`internal/provider/first_byte_test.go` exercises the real Eino HTTP adapter,
independent initial/idle waits, actual timeout duration, cancellation and the
single fresh-connection retry. `internal/provider/http_test.go` retains the
moving-stream idle and connection recovery regressions.
`internal/engine/runerr_timeout_test.go` checks the configured duration through
the public error wrapper without adding another model retry loop. Run
`go test -race ./internal/config ./internal/provider ./internal/server ./internal/engine`.

`frontend/src/components/app/transcript-error.test.tsx` checks that the error
opens the failed turn's provider, focuses the first-byte field and saves a longer
wait while leaving the other provider alone. It also checks localized durations,
an unavailable turn, and the absence of first-byte advice for an idle timeout.
`frontend/src/lib/first-byte-timeout.test.ts` checks Go duration parsing and
provider-specific settings anchors; `frontend/src/components/app/model-settings.test.tsx`
covers editing the independent field. Run
`cd frontend && npm test -- transcript-error.test.tsx model-settings.test.tsx first-byte-timeout.test.ts`.
`frontend/src/components/app/skill-tidy-card.test.tsx` checks that catalog-tidy
header failures also give configuration guidance without inventing a fixed wait.

`frontend/e2e/api-error.spec.ts` includes the real loopback adapter recovery:
one-second first-byte and idle values fail against a 1.2-second header delay
after exactly two HTTP attempts. The error survives reload and its button opens
the focused provider field. Changing only the first-byte wait to three seconds
persists through Settings reopen, and resending succeeds with the same idle
value. Failed and successful turns retain their own Trace, screenshots and
`first-byte-traces.json`. Run
`cd frontend && ZWAI_E2E_MODEL_API_ERROR=1 npm run e2e -- api-error.spec.ts`.

Native macOS acceptance uses the same loopback endpoint and a separate data
directory: send in the actual desktop window, follow the timeout button, verify
the provider field receives focus, increase one to three seconds, close Settings
and resend. Inspect the successful response and Idle state in that window, and
read Settings and both turn rows back to verify persistence and terminal status.

## Issue #74 complete-main release review regressions

`cmd/zwai/engine_startup_test.go` uses real holder subprocesses and delayed lease publication to distinguish same/different version and dev binary identity, wait for active work, and preserve holders on unreadable state or refused graceful stop. Modified `ensureEngine`/startup discovery statement coverage is 93.1%/100% in the target race run.

`internal/store/schedule_retarget_test.go` and `internal/engine/schedule_retarget_test.go` cover old empty/NULL kind in both directions, quiet archive, claimed wake Waiting identity, transactional failure rollback, combined target/status and resume cap, parked-worker and goal handoff. Run `go test -race ./internal/store ./internal/engine -run 'Retarget|PendingWake|PatchScheduleFields'`. Schedule contract regressions and repeated concurrent cap checks supplement these targets.

`internal/engine/context_overflow_flow_test.go` goes through the real keyless loopback HTTP adapter and scheduled manager graph: forced compaction below the normal trigger, two actual HTTP 400 rejections on one turn, persisted error/Trace, recurring-only pause, and successful recovery followed by three real report tool rounds without a second compaction. These are additional existing-behavior evidence, not an invented pre-fix failure.

`internal/engine/api_error_integration_test.go` reproduces empty/HTML HTTP 400, invalid JSON HTTP 200 chunks and a legal JSON chunk with the wrong field type. All retain one turn, two retries/three model requests, no invented tool call, original LLM Trace causes and the appropriate terminal public error. Modified classification, public error and provider marker functions have 100% target statement coverage; streamed provider forwarding has 94.6%.

The opt-in desktop `ZWAI_E2E_MODEL_API_ERROR=1 npm run e2e -- api-error.spec.ts` checks empty 502, empty 400 and a malformed successful stream through the real SDK, Chat and Trace, then replays after reload. Mobile `history=model-response-error` complements `history=api-error` with response-decoding notices/error, correct localization and no active Stop or tool blame. Packaged native acceptance must run `BindFlowTests/testModelResponseErrorIsLocalizedWithoutToolBlame` and `APIErrorTest#testModelResponseErrorRetriesRemainLocalizedWhenReopened` and inspect the reached screen; old published package receipts do not accept a new source.

`frontend/src/store/app-send.test.ts` defers the API fixture acknowledgement while event payloads pass through the production stream/store reducer and finish the turn, then resolves it. Fast success/error, live steering, new-turn identity, follow-up and navigation regressions keep terminal state authoritative; the real loopback HTTP E2E reproduces the same former Working-after-error race. The first full Go race run also exposed an old string-only mock of a decoded API rejection: the fixture now uses the SDK API error type, and `TestParsedToolAPIRejectionKeepsToolReasonThroughRealSDK` verifies actual HTTP JSON rejection and its distinct retry category. The original failure logs remain private evidence.

## Unread completion attention (Issue #76, F-125, C-023)

`internal/store/completion_test.go` checks persisted completion/read cursors,
old-data migration, quiet/cancel filtering, late reads, rewind and restart.
HTTP and paired RPC tests check exact ACK validation and an attention roster
independent of live work/ordinary pagination. Large attention collections are
serialized within the existing encrypted-message byte budget; keyset continuation
survives a read/deletion of its boundary, and phone polling keeps expanded pages.
An offline engine run checks real primary
success/error and cancellation/quiet paths.

Desktop `sidebar-unread.test.tsx` and `completion-reader.test.tsx` distinguish
static completion from live pulse, aggregate collapsed projects on the right,
and preserve newer results on late replies. Read confirmation requires the exact
primary terminal event in the rendered main history: worker/roster-only cursors,
rewound terminals and quiet turns retain attention. In-flight first-page responses
must not undo a newer local completion or read. Two delayed read responses returned
in reverse order must not relight the same already-read completion.
`frontend/e2e/unread-completion.spec.ts`
runs a real offline host task, reloads a folded project and opens its result.
Phone `mobile/src/lib/completion-read.test.tsx` and
`mobile/e2e/unread-completion.spec.ts` cover automatic recovery, explicit reads,
visibility, failed ACK retries and English/Chinese layouts.
`mobile/src/components/home-screen-unread.test.tsx` (Issue #83) covers live,
waiting and asking rosters, running/waiting listing and attention-only flags,
different IDs with one title, search matching only an old completion, idle
reappearance without watermark mutation, and More with every loaded result hidden.
`mobile/src/lib/resume.test.ts` covers attention-only running/waiting fingerprint
transitions in both directions and stable repeated input.
`mobile/src/active-unread-refresh.test.tsx` sends those transitions through the
real App inbox poll and quiet-poll guard, retaining the unread watermark and
checking that hiding/reappearance sends neither `open` nor `read`.
Run `cd mobile && npm run e2e -- active-unread.spec.ts` for Chromium phone and
iPhone WebKit. The `history=active-unread` fixture exercises running, waiting and
asking IDs alongside a different completed ID with the same title, expanded pages,
polls and an idle transition without opening a result or acknowledging it.
For the disposable native fixture described above, use
`IOS_LAYOUT_QUERY='/?mock=1&tick=0&history=active-unread'`, place the current bundle
in the private native project, and copy `mobile/e2e/ios-active-unread.swift` to its
`AppUITests/BindFlowTests.swift`. Select
`-only-testing:AppUITests/BindFlowTests/testNativeActiveUnreadHomeProjectionKeepsOldWatermarks`
in the existing `xcodebuild test` command. Android uses the same query and locale
in a disposable native copy; build with the app-scoped Gradle tasks
`./gradlew --offline --no-daemon :app:assembleDebug :app:assembleDebugAndroidTest`,
install as above and run AndroidJUnitRunner
with `-e class com.lubyruffy.zwai.ActiveUnreadTest#nativeActiveUnreadHomeProjectionKeepsOldWatermarks`, implemented in
`mobile/android/app/src/androidTest/java/com/lubyruffy/zwai/ActiveUnreadTest.java`.
Both drivers retain reached-screen screenshots and accessibility output; Android
also reads the installed APK's WebView DOM. More uses native accessibility clicks;
DOM inspection never invokes clicks, mutates state or injects RPCs. Confirm one
actually executed test and its exact method, runner and test APK identity; a script
exit of zero alone does not prove acceptance. These entries require actual
candidate runs and package/source receipts; their presence does not claim native
acceptance or publication.
Native iOS uses
`BindFlowTests/testUnreadCompletionPersistsUntilExplicitVisit` with the packaged
`history=unread-completion` offline fixture. Phone read tests distinguish the
request's authoritative completion baseline from the rendered terminal event:
late replies cannot revive a rewound result, while the first live result remains
readable when the initial server watermark is zero. Quiet/cancel events do not
manufacture an authoritative completion watermark.
`mobile/src/lib/completion-read-compatibility.test.tsx` regresses repeated
legacy `done`/`error` checks with missing completion fields, a modern host's
first zero-watermark result, switching between those hosts and real read
refusals without a false clear. Existing read tests retain delayed/reversed
ACK, visibility, rewind and newer-completion guards. The two host variants in
`mobile/e2e/unread-completion.spec.ts` stream separate scheduled completions,
then send in the same conversation and return to the inbox. They run in both
Chromium phone and WebKit iPhone layouts. The offline
`history=completion-compatibility&completion=legacy` fixture omits completion
fields and rejects `read` exactly as a pre-completion host does;
`completion=modern` retains the initial numeric zero. The fixture has unit
coverage in `mobile/src/lib/mock-link.test.ts`. These browser receipts do not
identify the exact RPC from an uninstrumented user screenshot or prove a
native package was accepted.
`mobile/src/lib/inbox-refresh.test.tsx` checks the actual loaded attention window:
a desktop read removes a phone tail row even when the head is unchanged; new
head rows do not shrink its old boundary; failed continuation preserves the
window. Tests also cover reads and More during a refresh, host switches,
non-overlapping polls and sub-millisecond timestamp ordering. A confirmed read
must not be undone by an older list response for the same completion. A newer
completion verified before or during refresh remains reachable whether it is in
attention, the ordinary head, or an expanded tail. A successful explicit read
retry also clears the previous read error. Whole cursor pages are retained,
including rows beyond the old boundary, so consuming a cursor never loses rows.
`BindFlowTests/testUnreadCompletionPagesContinueAfterReadingTheCursorAnchor`
uses `history=unread-completion-pages` to expand independent pages, retain them
through polling, read the cursor's boundary row and continue without duplicates.
These source tests do not themselves
claim native acceptance or Release/TestFlight delivery; current receipts must
include the tested package/source identity and reached screens.

Android native completion uses `UnreadCompletionTest` in the existing
`mobile/android/app/src/androidTest` harness: automatic recovery retains the
completed row, entering its result clears it, and Back returns to the inbox
without that attention row. Screenshots validate the static badge because an
InboxRow button groups its label in native accessibility output.
`UnreadCompletionTest#completionPagesContinueAfterReadingTheCursorAnchor`
exercises the same independent pagination and read-boundary continuation in the
packaged Android WebView.

## Truncated tool arguments before HTTP (F-130 / C-012)

`internal/engine/model_retry_http_test.go` exercises Registry, ADK and the actual OpenAI HTTP adapter on a keyless loopback server. Invalid trailing parameters and trailing parameters followed by steer make zero HTTP requests; valid parameters keep one. The compaction test uses the returned middleware state, and runtime recovery checks bounded attempts, error classification, steer and historical worker identity. Manual hook calls alone do not prove that HTTP was prevented.

```bash
go test -race ./internal/engine -run 'TestToolJSONGuard|TestInvalidTrailingToolCall'
```

Directory identity regressions in `internal/engine/project_workdir_identity_test.go`, `project-dialog.test.tsx` and `frontend/e2e/workdir.spec.ts` cover a selected trailing-space folder with and without its trimmed sibling, create/edit/readback/runtime resolution, and deletion after a real selection before Save. Manual non-empty paths are literal; all-whitespace remains managed. Native acceptance must repeat create/edit through the actual system picker against the same candidate binary; browser bridge fixtures alone are insufficient.

## Decodable image input and sent-image preview (F-140 / C-028)

`frontend/e2e/fixtures/vision-photo.jpg` is a valid JPEG (684 bytes, 16x12), not a three-byte magic marker. `frontend/e2e/image-input.spec.ts` checks MIME and unchanged base64 through submission, exact stored bytes, Trace references, natural dimensions and reload. `composer-image-drop.test.tsx` owns the corresponding component behavior; the large conversation/composer suites keep their other responsibilities. A visible img element or src alone does not prove decoding. The default offline provider does not prove image semantics; independent keyless loopback acceptance can inspect actual OpenAI image_url transport without using a real model.

`frontend/src/components/app/input-thumbs.test.tsx` covers selected-image identity, mouse/keyboard opening, loading/error states and closing with focus returned to the thumbnail. The sent-image browser path in `frontend/e2e/image-input.spec.ts` uses decoded stored images, checks proportional viewport fitting and verifies that the selected image opens again after reload. `frontend/src/App.test.tsx` exercises the actual window capture handler with a mounted Radix dialog: Escape closes the preview before a Find bar underneath it, and repeated Escape after either closes never calls interrupt. It also covers a focused composer, closed dialog remnants, the explicit Stop button and unchanged unrelated shortcuts. `frontend/e2e/shell.spec.ts` verifies Find dismissal during a running turn, repeated Escape keeping that turn active and the Stop button ending it (`F-130` / `C-002`). Desktop and Web share this component; a browser run does not by itself claim a native-window acceptance result.

`frontend/e2e/fixtures/preview-landscape.png` is a deterministic 1200x800 PNG used to prove enlargement beyond the thumbnail, proportional fitting at several viewport sizes and unchanged stored bytes. Geometry checks wait for the dialog's entry animation to finish after a resize. Backdrop dismissal also waits for a visible, entered dialog before clicking outside; an immediate raw coordinate click can race Radix's deferred outside-pointer listener.

```bash
cd frontend && npm test -- src/lib/paste-image.test.ts src/lib/composer-drop.test.ts src/components/app/composer-image-drop.test.tsx src/components/app/input-thumbs.test.tsx src/App.test.tsx
cd frontend && npm run e2e -- image-input.spec.ts
cd frontend && npm run e2e -- shell.spec.ts
```

## Forked worker role boundary

`go test -race -run 'TestForkedWorkerDoesNotReceiveManagerSystemInstructions|TestForkContextOmitsSystemOnlyAndEmptyConversations' .`

The regression observes actual model input through Registry and ADK for both
`SpawnForked` and the manager lifecycle tool. A `spawned` event only carries the
worker instruction and cannot prove that the separately injected conversation
seed excludes manager System/personality. Check retained user/assistant/tool
evidence, empty/System-only history, manager input immutability and unchanged
same-role resume formatting. Do not infer actual model behavior or memory
quality improvement from this deterministic payload test.
## Shared model recovery backoff (`C-029`)

The 2026-10-21 provider race audit reports 95.3% package coverage. The shared
gate, admission/classification, HTTP reconnect adapter, Generate/Stream wrappers
and stream-completion observer each have 100% target statement coverage. Engine
wait recording and the call-recorder dispatch each have 100% target statement
coverage in the focused race run. The
2026-10-19 loopback 502 browser run observed HTTP arrival gaps of 5002ms and
10003ms for the unchanged three-call budget; that historical run does not certify
a later merged candidate, which must rerun the browser fixture.

`internal/provider/backoff_test.go` uses `testing/synctest` to assert the actual
5/10/20/30-second sequence without wall-clock sleeps. It covers one recovery
probe, healthy concurrency, stale in-flight replies, cancellation, renewed
cooldown notifications and idempotent release. `model_backoff_test.go` exercises
classification and recorder/context wiring; authentication/parameter errors
cannot add failures or prove recovery. `backoff_integration_test.go` uses the
real SDK with keyless loopback HTTP fixtures to verify cross-role/model/provider
and cache-invalidated calls, independent origins, malformed successful streams,
complete-stream recovery and delayed/cancelled header reconnects. An actual HTTP
stream ending after valid deltas but without `[DONE]` or finish reason must fail
with `io.ErrUnexpectedEOF`, retain received usage and keep sibling requests
blocked. Complete `[DONE]`, `stop`, `length` and `tool_calls` responses preserve
usage and reopen admission. `stream_completion_test.go` verifies exact unchanged
bytes, fragmented/large frames and false terminator matches. Closing an Eino
reader alone retains a silent probe; canceling its request context releases it
without clearing failures. A header reconnect following a cooldown longer than
the header budget still gets a fresh response-header budget. These are service-
failure fixtures, not scripted success claimed as retry evidence.

`internal/provider/stream_terminal_order_test.go` blocks the real recording
wrapper's callback and uses `testing/synctest` to prove receive errors,
truncation, caller cancellation and normal EOF cannot reach the consumer before
that callback completes. It asserts one attributed record with all received
usage, and retains one record if the consumer closes while persistence waits.
The receive-error, cancellation and truncation cases failed before the ordering
fix; the actual HTTP truncation/completion fixtures above retain the adapter
and shared-gate coverage.

`internal/engine/model_backoff_test.go` asserts actual agent/turn provenance and
that waiting creates no LLM call or usage pulse, while actual and auxiliary calls
retain their accounting and storage failures cannot emit usage.
`frontend/src/lib/transcript-backoff.test.ts`
checks SSE subscription, replay, localized human text and no invented workers.
The opt-in `frontend/e2e/api-error.spec.ts` measures actual HTTP arrival gaps on
502 (at least 5s then 10s), and checks the original turn, Chat, Full log and reload.

```bash
go test -race ./internal/provider -count=1 -coverprofile=/tmp/provider-backoff.cover
go test -race ./internal/engine -run 'TestModelBackoff|TestEmptyGatewayResponse'
cd frontend && ZWAI_E2E_MODEL_API_ERROR=1 npm run e2e
```

This preserves the original manager retry count and worker watchdog budget;
no real model or external service is used. It does not certify another client's
retry behavior or impose a server-wide request limit.
