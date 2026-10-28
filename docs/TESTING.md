# Testing

## Host verification admission (`F-430`, `C-010`)

`tools/release_host_lease.py` serializes cooperating local release verification
across independent copies using one persistent-inode host/UID/module flock.
Admission additionally samples actual same-user validation PID/birth, working
directory and module under the mutex. Saved owner JSON alone cannot accept an
idle state; active or unknown observations block with bounded queue evidence,
and the observer never terminates another task. This cannot serialize future
manual commands that bypass the protocol; `make test/check` remain unwrapped.
`tools/test_release_host_lease.py` uses unique temporary modules and lock roots,
real competing lock holders and one small owned unittest process. It checks
cross-copy exclusion, stale receipt versus live process, cancellation, PID reuse,
foreign-project exclusion and private evidence.
The waiting-CLI fixture holds the actual inode while a second real Python release
controller waits on it, then samples the current processes and confirms the
holder admits and releases normally; OS inventory selection is limited to the
owned fixture PIDs while identity/cwd/classification reads stay real. A controller
without a heavy worker is not an active-test conflict; manual workers remain
subject to the admission guard. `tools/test_release_verify.py` checks the actual
entry refuses before preflight/heavy commands on host conflict.
These lightweight fixtures do not require a VM, GUI or a full Go/Node suite.

## Release verification tooling (`F-430`, `C-010`, `C-031`)

`test_release_stages` uses a deterministic barrier inside the real pending-latch
publication path to prove a normal owned-stage setup does not falsely reject a
parallel ordinary stage. The actual StageRunner, subprocess, cache and durable
latch execute; only this no-detached-child fixture's OS inventory leaf is empty.
Independent detached-process tests retain their real inventory/cleanup checks.
Additional barriers cover a newly failed setup during input/cache reads;
write failures and `KeyboardInterrupt`/`Cancelled` after pending publication
retain sticky refusal and the original exception, including across a new runner.

The existing release entry exposes `--verify` / `make release-verify` for
candidate-bound, resumable local checks without publication. See
[RELEASE.md](RELEASE.md#local-verification-before-publication) for the commands,
exit statuses, dependency preparation and final-package/native boundaries.
Regression fixtures cover dependency self-links and outside module resolution,
bad core entries, source/lock/toolchain/config changes, changed output trees,
failed receipts, queue/cancellation cleanup and overlapping interval accounting:

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p 'test_release_*.py'
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH=tools python3 -m unittest test_native_macos_prepare_login.FrameTargetTests test_native_macos_prepare_login.PrepareLoginTests
```

Ordinary native readiness uses `native_macos_smoke.wait_initial_ready` and the
existing shared deadline for engine identity, real AX window and nonblank pixels.
It does not read a recovery journal. Recovery successor readiness and cold-start
source/launch assertions remain separate scenario contracts. Only explicit
registration/window/content pending observations may be polled; unknown helper
outcomes, identity/permission refusal, failed pixels and business assertions stop
the round. Every native helper attempt retains complete original streams and
timing before parsing, including timeout output. These headless regressions do
not accept an actual guest window or recovery.

The one-shot login preparation accepts `login_target=None`: publish the fresh
before-frame first, then approve its exact SHA and `login_target` in the single
RAM envelope. The target contains `width`, `height`, `password_x`, `password_y`
and `known_us_empty_ci_field: true`; malformed/out-of-frame points refuse input.
An explicit frozen target is only a hint and must be confirmed by that envelope.
Older frame-only login senders now refuse. After-desktop approval carries neither
target nor credential. Credentials remain RAM-only, absent from argv and receipts.

The existing 1500ms offline worker hold is intentional: Steer/wait fixtures need
an observable running worker. This change does not shorten it, add E2E shards,
weaken coverage/race/GUI gates, or create a VM pool. Parallel-stage timing alone
cannot establish a speed gain or VM contention; measure comparable candidates
before changing those policies.

Create private evidence files with explicit mode `0600` while preserving and
recording the normal compiler/test subprocess umask; a restrictive logging-only
mask must not silently change executable-mode fixtures or be reported as a
product regression without a controlled comparison.

## Paused goal replies and runtime provenance (`F-150`, `F-220`, `C-008`, `C-009`)

`internal/engine/goal_paused_schedule_claim_test.go` crosses the real Registry/HTTP boundary for capped, blocked and idle human replies rejected by schedule verification, with and without a durable terminal error receipt. The original pause, reason and auto-turn count must survive with no new goal turn. `internal/store/goal_cap_test.go` also makes an idle pause win inside the cap transaction, after an older snapshot was read. Active pursuit and explicit resume retain the separate three-failure schedule-claim cap.

`goal_test.go` changes the old cap-reset expectation: cap 1 admits a human turn
and one automatic turn; another ordinary message adds only its answer turn.
`goal_paused_message_test.go` and `goal_idle_test.go` cover blocked/idle, explicit
resume, active-pursuit reset, failed replies, Stop and independent wake ownership.
The independent scheduling-claim failure cap follows the same paused-send rule:
ordinary replies retain its cap, automatic count and original failure diagnostic;
Start / HTTP `goal_resume` / phone `resume_goal` explicitly restores pursuit and
resets the budgets.
`prompt_goal_pause_test.go` inspects actual manager HTTP input for the source-bound
human deliverable and checks generic completion-criteria guidance. No incident
request, objective, title or conversation ID is a positive prompt expectation.

`goal_pause_integration_test.go` crosses both PC HTTP send/PATCH and phone RPC
send/resume_goal for capped, blocked and idle. Desktop Playwright keeps the
paused banner and Start after sending/reloading, then observes pursuit after
Start. Phone tests retain the same projection through send until resume_goal.
`ordinary_worker_completion_test.go` uses the actual worker/manager boundary:
the same-turn correction is consumed and collection completes, then the real
next StartTurn excludes it and older GoalContinueText. `runtime_cues_test.go`
also captures compact input, retains human quotations and filters session
briefing input; legacy compatibility matches complete known cues only.

```bash
go test -race ./internal/engine ./internal/store ./internal/server ./internal/remote
ZWAI_E2E_GOAL_PAUSE=1 ZWAI_E2E_PORT=18964 ZWAI_E2E_DATA_DIR=/absolute/unique-pause-data \
  npm --prefix frontend run e2e -- goal-pause.spec.ts --output /absolute/unique-pause-output
(cd mobile && ZWAI_MOBILE_E2E_PORT=14196 npm run e2e -- goal-pause.spec.ts --output /absolute/unique-phone-pause-output)
```

Each concurrent run needs its own port, data/output directories and built dist.
The fixtures use loopback HTTP without provider credentials and disposable test
workspaces. They do not read or modify an active user's data directory.

## Phone nested reasoning disclosure (Issue #118, `F-220` / `F-222` / `F-223` / `F-230`, `C-006`)

`mobile/src/components/thread-blocks-reasoning.test.tsx` first reproduces the missing
second disclosure through the real log renderer. It checks live/replay and single
or multiple thoughts, independent block identities, full original text after an
explicit click, retained open/closed choices through deltas and completion, and
answers and tool controls remaining independent. Direct-model and Clients tests
keep their original text assertions after explicitly opening the thought.

`mobile/e2e/reasoning-fold.spec.ts` exercises the packaged phone flow in Chromium
and the iPhone WebKit project. The existing direct-chat wire/reload and generation
interruption cases also open the second disclosure and retain their original
reasoning, request-count and recovery assertions. A live outer ticker may contain
the last reasoning line: inspect the `phone-thought` body and the inner button
state, rather than asserting that no matching text exists anywhere on screen.

Native entry points are `ReasoningFoldTest` in the Android instrumentation tree
and `mobile/e2e/ios-reasoning-fold.swift` with `scripts/ios-layout-fixture.py`.
Use one source-identified public-resource bundle for both private native builds,
real taps to open the process row and then the thought, and actual images plus
read-only DOM/AX evidence for closed/open/closed states. Preserve package hashes,
command exit status and cleanup of only the owned device. Derived Simulator UI
acceptance is not installation of a signed TestFlight IPA or future release-package
acceptance; no new version/build/upload is implied by this source QA.

Run Android with `-e reasoningLocale en` and `-e reasoningLocale zh` separately,
clearing only the owned test app's data between cases. Its screenshots and JSON
are mode `0600` in the target app's internal files; collect them with debug
`run-as` and verify the archive and image bytes rather than relying on `adb`'s
exit status. The bounded passive frame samples retain original images and state:
pixel stability alone can still select an old frame, so review each phase.
For iOS, preserve the common web resources with `IOS_LAYOUT_PRESERVE_PUBLIC=1`,
set `IOS_LAYOUT_LOCALE=en|zh`, and select only the matching
`testEnglishReasoningRequiresItsOwnToggle` or
`testChineseReasoningRequiresItsOwnToggle` method. A fixed-locale fixture cannot
run both methods. Record any native warmup separately; it is not cold-start QA.

## Chat Completions reasoning fields (`F-170`, `F-230`, `C-009`)

`internal/provider/reasoning_fields_test.go` runs both Generate and Stream through
the configured Eino/SDK HTTP adapter against a keyless loopback fixture. It covers
`reasoning_content`, newer vLLM `reasoning`, absent/empty/null fields and legacy
precedence without duplication. A stream gate withholds the answer until the
consumer receives the reasoning-only delta. Tool arguments, finish reason,
reasoning usage and one original call record must survive unchanged.
These cases pass on the existing adapter; they guard dependency upgrades rather
than introduce another parser. PC E2E checks live thought, original-turn Trace
and reload; phone E2E checks direct chat thought and reload for both fields.
The PC fixture also serves the existing separate `schedule-answer-policy`
classification: only its exact tool-free, non-streaming protocol and the known
scripted assistant candidate receive an empty claim list. Unknown or malformed
requests fail closed. Manager and policy requests each occur once in the
original-turn Trace; full fixture requests and policy results are retained,
and reloading must not add either request.
No real model, provider credentials or native desktop launch is involved.

```bash
go test -race ./internal/provider -run TestChatReasoningFieldsSurviveGenerateAndLiveStream -count=1
ZWAI_E2E_REASONING_FIELDS=1 ZWAI_E2E_PORT=14879 ZWAI_E2E_DATA_DIR=.e2e-data-reasoning \
  npm --prefix frontend run e2e -- reasoning-fields.spec.ts --output /absolute/unique-pc-reasoning-output
(cd mobile && ZWAI_MOBILE_E2E_PORT=14874 npm run e2e -- direct-chat-reasoning.spec.ts --output /absolute/unique-phone-reasoning-output)
```

The PC Playwright suite holds one acknowledged, suite-owned TUI presence through
`frontend/e2e/engine-presence.ts`, across browser and failed-worker replacement.
Its handshake is bounded and teardown releases the stream; this does not alter
production idle shutdown or replace its dedicated lifecycle tests. Startup
schedule fixtures declare `kind: "standalone"` explicitly. A refused fixture
request and later idle process exit are separate failures with separate evidence.

## Queued human work after errors (`F-130`, `C-002`, `C-009`)

`internal/engine/followup_after_error_test.go` exercises the actual Engine,
Registry and offline HTTP model boundary. Queue while the first request is
running, exhaust the real first-byte reconnect for a scheduled check, and assert
independent human turns consume FIFO once while the original error, schedule run,
unread result and future wake stay intact. Cover ordinary errors, an empty queue,
Stop, restart and competing manual delivery. Do not broaden automatic goal retry
to make an explicit queue pass, or initialize a fixture as already failed.
`frontend/e2e/api-error.spec.ts` exercises the real composer, loopback model,
queue and reload path with `ZWAI_E2E_MODEL_API_ERROR=1`; each execution owns a
separate port, data directory and output directory.

The encrypted loopback relay fixture in `internal/remote/followup_relay_test.go`
waits for the relay's actual host presence before consuming its one-use offer.
A successful WebSocket upgrade alone does not establish that presence; the wait
is bounded by the fixture context. Repeated real RPC cases cover this setup.

`internal/engine/restore_failures_test.go` drives `Engine.Restore` and the
compatibility `ResumeOrphanedTurns` through a real missing-provider failure
before a healthy offline conversation. Both must return typed failure causes,
retain the original turn error, and continue healthy recovery; the startup
entry also records `engine_restore.phase=failed` rather than a success receipt.

## Long-conversation opening (`F-120`, `F-130`, `C-003`, `C-008`)

Phone opening has its own adapter and must be checked separately from the PC
HTTP path (`F-210`, `F-220`, `C-006`). `snapshot_reads_test.go` checks actual SQL
projection, row counts, current-turn selection and paging past hidden records;
`phone_reads_test.go` checks legacy NULL values, index migration and query plans.
`watch_open_test.go` checks one encrypted relay request carrying detail and
tail, legacy response shape, combined frame budget and refusal cleanup.
`mobile/src/lib/open-thread.test.ts` checks the single RPC, legacy-host fallback,
queued live events and Back during either response. The Chromium/WebKit
`mobile/e2e/conversation-open.spec.ts` exercises reopening and Earlier history.

Reconnect regression must use production Host admission with a real encrypted
relay, then count outbound frames for a subsequent real engine event. Closing
a fixture's own pumps cannot prove production cleanup. `host_links_test.go`
reconnects one device twice and requires one sender, keeps a second device
independent, verifies old connection callbacks cannot replace new owners and
joins actual watch/catalog work through Stop/Reload. A network write already
entered into pairlink still uses that dependency's existing write deadline;
the test does not claim a total shutdown deadline.

Replay regressions use the real Store with tool bodies much larger than retained
user/assistant text. Assert the query results exclude tool roles and unrelated
event kinds, compare output and `context_chars` with the previous replay, and
measure allocation/time while increasing only tool-body volume. Cover the active
compact watermark, seq-based and legacy-caption steer retraction, already-stored
answers below the watermark, tool-only/empty-message turns, missing manager
answers and mixed ASCII/CJK text. A second replay must not insert another answer.
Keep `ContextUsage` and the next model turn on the same replay entry point.
The differential and decoded-row checks are in
`internal/engine/replay_history_test.go`; Store migration, query-plan, Unicode
whitespace and repair-search regressions are in `internal/store/replay_test.go`.
`internal/engine/replay_history_benchmark_test.go` keeps row counts and retained
text fixed while increasing only tool-body bytes, and compares the filtered
replay with the frozen legacy-semantics reference, including the former two
whole-message and two whole-event reads. The benchmark's answers are already
stored, so missing-answer repair does not change either side during measurement.

Replay indexes are installed by the existing `store.Open` migration for both new
and existing SQLite databases; the migration test must reopen an existing store
with the indexes absent and inspect the resulting query plans. No columns or
manual conversation repair are needed. Frontend regressions hold detail usage,
turns and follow-ups while releasing the tail, then exercise late responses,
navigation and rewind. The UI E2E must show visible tail content with the usage
response still pending, then the unchanged compact meter after it arrives.
The independently delayed open responses are covered by
`frontend/src/store/app-open.test.ts`; `frontend/e2e/thread-open-tail.spec.ts`
checks visible tail content while the detail response remains held.
Opening and terminal turn-list reads share the request generation in
`frontend/src/store/turns-sync.ts`. Regress both response orders: an older
opening response must neither replace nor prevent the newer terminal snapshot.

```bash
go test -race -timeout 25m ./internal/store ./internal/engine
go test ./internal/engine -run '^$' -bench '^BenchmarkReplayHistoryToolBodies$' -benchmem
go test -race -timeout 25m ./...
go vet ./...
(cd frontend && npm run lint && npm test && npm run build)
ZWAI_E2E_PORT=18996 ZWAI_E2E_DATA_DIR=.e2e-data-open-tail \
  npm --prefix frontend run e2e -- thread-open-tail.spec.ts --output /absolute/unique-open-tail-output
make docs-check
```

## Ordinary completion collects delegated work (F-130 / C-019 / C-012)

`internal/engine/ordinary_worker_completion_test.go` 使用真实 Registry／ADK／
loopback HTTP：原 worker 请求保持运行，主管提前给出 final，候选正文必须在
live、保存回答和下次模型输入中缺席；纠正后真实 `wait_agents` 收到同一 worker
当前 generation 的结果，才允许正常完成，不能靠取消 worker 或新建替代者通过。
`settlement_http_test.go` 验证查询／Stats 归档之后仍能真实收取已完成结果。
`internal/engine/worker_completion_replay_test.go` 验证配对调用、worker ID 与 generation，
旧 generation、wait 超时、unknown、错误调用者及孤立回执均不能消耗当前结果。

`internal/engine/worker_completion_budget_test.go` 验证原 turn 的持久化纠正额度及存储失败边界。
首次 `model_retry` reason 为 `workers_uncollected`，携带 `worker_ids`；再次
提前 final 进入人工 Continue／Stop 暂停，保持同一 running turn 和 worker 请求。
Continue 只增加一次尝试；Stop／拒绝取消，重启保持原收取状态、额度和人工暂停。
门禁针对主管自然完成；`goal_session_test.go` 的外部 Complete／Block 中途终止与
ReAct 切片耗尽沿原边界收尾，不能因为这类结束新增收取确认卡。
有当前有效 goal／wake 时保留原停泊行为，policy 请求期间撤销最后 owner 后必须
重新执行收取门禁。显式关闭已结束的 worker 可以放弃结果，但回执仍须是
`already_finished:true,cancelled:false`；未知 ID 不得结清任何 worker。
`settlement_test.go` 覆盖旧 finished 无 generation 身份的兼容、旧 running 的
收取义务，以及新 generation 的精确状态、resume、显式 close 与归档淘汰。

```bash
go test -race -timeout 5m . -run 'TestActualWaitCollectsACompletedWorkerAfterStatsPrunesIt'
go test -race -timeout 5m ./internal/engine -run 'TestOrdinaryTurn|TestWorkerCollection'
# 独立端口、数据及输出目录；真实 named SSE、worker HTTP、Trace 和刷新历史。
cd frontend
ZWAI_E2E_WORKER_COMPLETION=1 ZWAI_E2E_PORT=8990 ZWAI_E2E_DATA_DIR=/tmp/zwai-collection-e2e npx playwright test --output=/tmp/zwai-collection-output
```

真实桌面验收继续使用 `ci-macOS` 独立 case 和当前源码／包哈希；普通请求须在
没有 goal／wake 的会话完成真实 spawn／wait／最终答复，保留原 worker 身份、
Trace、窗口截图及完整 cleanup。正常离线脚本的窗口验收不能替代提前 final 的
故障分支回归；生产源码变化后须重新绑定候选，不能复用旧包证据。

## Trailing goals and verified scheduling reports (F-150 / C-009 / C-012)

Issue #103 starts from a completed objective and no active thread wake.
Shared fixtures in `internal/slash/testdata/trailing-goal.json` cover ASCII,
CJK/IME slash, whitespace, multiline objectives and literal paths, quotations
and code, including CJK paths ending in `/goal`. Go, PC and phone command
admission use the same cases. `frontend/src/components/app/composer-trailing-goal.test.tsx` checks
Send/Enter, idle/running admission, menu completion and literal drafts. Leading
commands retain their whole argument even when it ends with `/goal`; UI/HTTP
regressions check that a second interpretation cannot rewrite the objective.
`frontend/src/store/app-goal.test.ts` and `app-plan.test.ts` check one complete
command send, acknowledged failure and fixed conversation ownership, including
a running turn or pending ask arriving after the initial idle observation.
Explicit command intent bypasses ordinary question answers and queues; plan
start-turn admission must refuse a newly busy target while a goal update may
retain its pending ask. Bare plan and busy
metadata operations keep their existing behavior.
`frontend/e2e/command-admission.spec.ts` controls actual durable draft and HTTP
admission across idle-to-ask/busy transitions, preserves the original question
and turn, and checks that a rejected plan survives reload. Late send ACK tests in
`frontend/src/store/app-send.test.ts` use only the target conversation's event
sequence across navigation and live/history terminal delivery.
`frontend/e2e/plan.spec.ts`
checks that a literal goal suffix remains the plan argument at the real UI/API
boundary. `internal/engine/slash_command_once_http_test.go` checks the real
Engine/Registry/loopback model input for leading command ownership, internal
continuations and a busy raw steer without resetting its original goal.
Engine and paired-phone tests
exercise actual sends and steering so a successful parse alone cannot accept
goal activation, and program-generated goal updates are not parsed recursively.

`internal/engine/schedule_answer_http_test.go` uses the real Registry and
loopback model adapter to check missing, denied, failed, historical and
cancelled scheduling evidence. It checks both live events and persisted
replay: unsupported scheduling prose must not become an answer. Correction
is bounded to the original turn, and its policy calls remain attributed there.
The classifier extracts claims only; deterministic schedule checks must reject
unsupported scope, current state, cadence or new-arrangement attribution.
`internal/engine/schedule_answer_meaning_test.go` exercises nonclaim publication
through Registry/HTTP and decoder filtering/invalid fields. No external model
is used in regression tests. Structured classification
requires stance, time and attribution; nonaffirmative, noncurrent and reported
items cannot require a matching armed plan. These offline fixtures prove the
protocol and runtime boundary, not an external model's semantic accuracy.
`internal/engine/goal_schedule_claim_test.go` checks the real Registry/HTTP
path for failed and corrected scheduling claims, automatic goal continuation
and a durable three-failure cap. It keeps `goal_blocked` false, the original
terminal error and empty final intact, and the existing automatic budget.
Before a cap, successful/cancelled/different-error turns and human input during
unpaused pursuit break the streak. Once capped, ordinary human replies retain
the cap, automatic-turn count and original failure diagnostic without granting
more pursuit budget. Explicit Start / HTTP `goal_resume` / phone `resume_goal`
restores pursuit and resets the budgets; goal changes reset the failure budget,
manager reopen does not. Scheduled errors retain future-wake ownership without immediate retry.
`internal/store/goal_cap_test.go` checks atomic cap/reason rollback on failed
reads and writes, deleted owners, changed goals, budget resets and newer turns;
a stale decision cannot pause new human work. Unreadable retry evidence has a
distinct recovery-state cap reason, and failed persistence never grants another
automatic request.
`internal/engine/schedule_answer_timeout_test.go` holds a real loopback
classification response beyond 30 seconds while the configured provider budget
allows it, then checks the original turn completes without a repeated manager
request. It also checks parent cancellation and the configured first-byte
and body-idle timeouts, retaining each failure in the original turn's Trace;
candidate prose stays withheld until classification succeeds.
`internal/engine/schedule_answer_recovery_http_test.go` exercises classifier
service failures through the real loopback adapter, preserving one manager
request, the original candidate/turn, recovery backoff and a single published
answer. A legal HTTP 200 with invalid classification JSON retains
`model_backoff.code:classification_invalid` and the original `candidate_id`,
even though its LLM transport call succeeded. Request failures retain
`classification_unavailable`; local storage/configuration failures have distinct
codes. A retry does not repeat the manager/tools or consume the single
same-turn correction for a confirmed unsupported claim.

`internal/provider/isolated_recovery_test.go` crosses the real HTTP adapter:
classification header timeouts use one full request budget with no implicit
reconnect; another model on the same origin remains responsive with no origin
backoff. Classification failure cannot start a cooldown, and success cannot
clear an existing ordinary-request cooldown. Stop cancels the real header wait.
Ordinary request failures retain the existing single-probe gate tests in
`internal/provider/backoff_http_test.go`, `backoff_integration_test.go` and
`backoff_responses_test.go`.

`internal/engine/schedule_answer_deferred_http_test.go` holds an actual worker
HTTP stream while the classifier repeatedly withholds response headers. After
three consecutive failures, the same turn/Registry continues manager work with
that worker and its original tool receipt; the private candidate stays withheld.
A second conversation using another model at the same origin remains usable
before, between and after failures with no model-backoff event. The continued
manager makes a new tool call, and a real handoff preserves that newer checkpoint.
Cold database reopen keeps the durable deferral and resumes user work instead
of classifying the old candidate or replacing new work with its transcript. These are
in-process engine/HTTP regressions, not native-window acceptance.
`internal/store/schedule_answer_deferred_test.go` checks atomic failure-count
and deferred-state persistence, stale/invalid/unowned writes, legacy consumed
budget restoration, private prose retention with only one full transcript,
compaction rollback, and terminal/deletion cleanup. The request-failure cap is
separate from malformed-output retries, claim correction and worker watchdogs.
`internal/engine/schedule_answer_deferred_test.go` checks legacy exhausted and
broken-streak budgets, storage-read/write refusal, and ordinary turns without
an executable owner: Continue, an already approved pause, queued human steer,
decline and Stop retain their existing semantics without duplicate continuation
or losing an input arriving before pause installation.
`internal/engine/schedule_answer_deferred_restore_test.go` crosses real engine
and HTTP boundaries: a restored ordinary pause needs only one human Continue,
and a deferred report retains its actual tool receipt while rejecting an
unexamined manager answer before further authorized work. Old prose stays out
of the next model input, and exhausted candidates do not regain execution.
`internal/engine/schedule_answer_restart_test.go` holds the real corrective
model request across PrepareRestart and cold resume of the same turn. The
persisted correction allowance stays consumed; event writes and budget reads
that fail prevent another model attempt instead of resetting the allowance.

`internal/store/schedule_answer_pending_test.go` checks immutable private
candidate identity, retry-state preservation, stable ordering across all
decisions, running-owner/Stop guards and single admission under concurrent
calls. Event insertion, replay insertion or candidate-decision failure must
roll back all public rows and leave the candidate pending. Empty public prose
still commits acceptance, and a repeated accepted identity returns the explicit
already-admitted error. Thread/project deletion and rewind remove the correct
private candidates transactionally; injected deletion failure rolls back the
owner and private rows, and late writes cannot recreate deleted state.
The same suite injects failures into both terminal turn writes and private
payload clearing: only a successful transaction may discard the full recovery
payload, preserving lightweight accepted/rejected receipts and other turns.
Long-turn storage tests retain only the newest answer transcript, leaving
distinct report receipts intact. New insertion and previous-answer compaction
must roll back together on refusal or an ignored write. Per-turn ordinals
preserve latest-candidate recovery through clock rollback, equal timestamps,
caller-supplied ordinal values and re-saving an existing identity.
The report-recovery query includes the pending/deferred/accepted/rejected states,
another thread and terminal empty-payload receipts; it must not load the
current answer's full transcript while discovering original report calls.

`internal/engine/schedule_answer_restore_test.go` checks latest-candidate
selection, malformed private state, original report-call receipts and worker
restoration without a manager request. Restoring a report result cannot publish
unverified findings or manufacture a result for an unrelated call.
`internal/engine/schedule_answer_recovery_restart_test.go` crosses the actual
PrepareRestart, shutdown, database-close/reopen and orphaned-turn recovery
paths. It checks original turn/candidate/output IDs and retry allowance, a live
worker restored under its original identity, and accepted publication followed
by a failed terminal write. Recovery cannot generate another manager request
or duplicate a saved answer. The actual handoff checkpoint must not contain
the withheld candidate before the replacement resumes.
`internal/engine/schedule_answer_checkpoint_test.go` checks the stored and live
checkpoint omit normal and raw Responses prose while preserving accepted
history, tools and reasoning without mutating the private original.
These cases rebuild engines inside the Go test
process; they do not by themselves establish executable-process or native UI
acceptance.
`cmd/zwai/schedule_answer_process_test.go` adds the OS-process boundary: the
real headless engine entry point records a private candidate and classifier
failure, the test verifies its owned lease/PID and SIGKILLs that child, then a
different engine PID reopens the same private data. It checks actual SSE and
Trace retain the original turn/candidate/output identity, with no repeated
manager call or duplicate answer after the local classifier recovers. This is
headless engine acceptance, separate from a native desktop-window case.
`internal/engine/report_admission_test.go` checks actual manager tools retain
their original receipt/candidate IDs, defer findings verification until the
manager boundary and avoid repeating committed rearms. A blocked model request
does not hold the global goal/wake lock.
Its report-save fault cases distinguish an already committed rearm/cancel from
findings-only or unchanged actions: storage refusal cannot publish findings or
invite repeating a change that already committed. This receipt does not accept
the unobserved crash window before the private save.
`internal/engine/schedule_answer_steer_test.go` injects human steering after
classification at the final ownership read. The superseded answer stays out
of subsequent model input, and steering does not consume a previously used
claim-correction allowance. Its HTTP case also steers during the actual pending
classifier request: that request is cancelled, identical later answer text has
a distinct output/candidate identity, and only the new verified output publishes.
`internal/engine/schedule_answer_steer_ownership_test.go` blocks real attachment
hydration across a turn/Registry replacement and verifies the stale steer is
refused without delivering to either owner; empty busy-turn input is also refused.

`frontend/e2e/schedule-answer.spec.ts` uses its isolated loopback fixture to
exercise unsupported claims through the three-failure goal cap, separate
correction-request and terminal-error rows, reload, and 503/429 classification
failures followed by a response delayed beyond 30 seconds, named live backoff
events, the original manager/tool request
counts, actual scheduling facts and replay after reload. Candidate waits carry
`scope:schedule_answer` and appear with answer-verification copy, without an
origin cooldown notice; the normal API-error suite checks `scope:origin` and
opaque cooldown IDs through actual HTTP and Trace. Its Stop case keeps
the fixture alive past the pending retry time and checks no delayed request
escapes cancellation. The case retains screenshots and original-turn Trace;
its offline HTTP model does not call an external provider.

`frontend/src/components/app/transcript-schedule-claim.test.tsx`,
`frontend/src/lib/schedule-claim-notice.test.ts` and
`mobile/src/lib/transcript-schedule-claim.test.ts` check the correction notice
and Chinese display without exposing internal JSON or claiming a wait exists.
`frontend/src/lib/transcript-backoff.test.ts` and
`mobile/src/lib/transcript-backoff.test.tsx` check one origin notice across roles
and turns per cooldown ID, separate candidates/deferral, zero-delay probe
suppression and replay without inventing classifier workers. The phone's
`mobile/e2e/api-error.spec.ts` drives Chinese/English history and reload on
Chrome and iPhone WebKit using `mobile/src/lib/mock-backoff.ts`; it checks the
actual rendered notices without candidate IDs/JSON. This MockLink case verifies
phone projection, not an engine/database outage or a packaged native app.
Packaged native projection uses Android `BackoffNoticeTest` in
`mobile/android/app/src/androidTest/java/com/lubyruffy/zwai/BackoffNoticeTest.java`
and `mobile/e2e/ios-backoff-notice.swift` through the isolated iOS layout
fixture. Both inspect the four distinct notices, one row per cooldown, hidden
zero-duration probe waits, absence of classifier workers, and Back/reopen.
Retain screenshots and accessibility observations. Android records the installed
APK and public asset hashes; iOS release acceptance compares the simulator's
public assets with the final signed IPA and records the architecture and test
bootstrap differences separately. These fixtures do not replace real engine
HTTP, cancellation or process-replacement tests.
The desktop component checks distinct notice/error rows in live and history
projections; the terminal error says the final was withheld and does not imply
verification is still running.
`internal/remote/schedule_answer_paging_test.go` exercises actual phone
Earlier requests for all visible retry reasons and invisible/invalid envelopes,
so a correction-only page is retained without trapping paging on an empty row.
`internal/engine/schedule_answer_responses_test.go` checks the real Responses
adapter: rejected prose is absent from raw `responses_output` replay while
function calls and encrypted reasoning survive. Stop admission tests hold the
actual lock ordering after `user_stop_requested` commits; the answer cannot
publish during that gap. Store tests check the same-turn stop receipt, exact
schedule snapshots and rollback of answer events plus replay messages.

Goal commands are text-only. `internal/remote/slash_goal_attachment_test.go`
and `internal/engine/slash_attachment_test.go` reject mixed image/file inputs
before goal mutation, including busy sends and steering.
`mobile/src/components/composer-goal-refusal.test.tsx` checks that a refused
send preserves the original draft and decodable PNG/file bytes for correction
and resubmission through the actual phone client.

Issue #111 checks quotations before command wrapping. The phone imports the
pure `frontend/src/lib/slash-core.ts` parser; `frontend/src/lib/slash.ts` keeps
the existing desktop exports and localization. The actual Composer tests in
`mobile/src/components/composer-quoted-command.test.tsx` cover Send/Steer/form
submission, prefix/trailing and CJK commands, the shared Go corpus, local alert,
unchanged text/quotes/File selection and no submit/steer callback. They also
cover ordinary quoted requests, unknown names, paths, URLs and literal markers
so a quotation does not become a blanket command ban.
`mobile/e2e/quoted-goal.spec.ts` selects real transcript text, attempts a quoted
goal, verifies the visible refusal and preserved draft, then removes the quote
and explicitly submits the original command. It runs in both the phone and
`iphone-layout` WebKit projects. This offline host E2E accepts that UI boundary,
not a newly configured engine goal or packaged native iOS/Android behavior;
existing real Engine/Pairlink command tests remain the goal-semantic evidence.
Never unwrap `<user_request>` or drop selected context to turn the refusal into
an apparent success. Attachment-only goal refusal retains its existing path.

Packaged native entry points for the same quoted-command boundary are
`mobile/e2e/ios-quoted-command.swift` and
`mobile/android/app/src/androidTest/java/com/lubyruffy/zwai/QuotedCommandTest.java`.
For iOS, first build/copy the current phone assets through the existing offline
fixture flow. From `mobile`, select an explicitly owned simulator UDID in
`IOS_LAYOUT_SIMULATOR`, create the disposable layout fixture, and replace only
that fixture's test file:

```bash
ios_quoted_fixture="$(IOS_LAYOUT_QUERY='/?mock=1&tick=0' python3 scripts/ios-layout-fixture.py)"
cp e2e/ios-quoted-command.swift "$ios_quoted_fixture/mobile/ios/App/AppUITests/BindFlowTests.swift"
xcodebuild -project "$ios_quoted_fixture/mobile/ios/App/App.xcodeproj" \
  -scheme App -configuration Debug \
  -destination "platform=iOS Simulator,id=$IOS_LAYOUT_SIMULATOR" \
  -derivedDataPath "$ios_quoted_fixture/DerivedData" CODE_SIGNING_ALLOWED=NO \
  -only-testing:AppUITests/BindFlowTests -parallel-testing-enabled NO \
  -resultBundlePath "$ios_quoted_fixture/quoted-command.xcresult" test
```

Do not replace the checkout's `AppUITests/BindFlowTests.swift`. Android reuses
the disposable offline APK/test-APK flow below, with an owned headless emulator
and explicit `android_test_device`. Run each method separately, stopping only
the owned fixture app between methods so WebView/AX state cannot leak:

```bash
for quoted_method in quotedGoalKeepsDraftUntilQuoteIsRemoved ordinaryQuotedMessageStillSends; do
  adb -s "$android_test_device" shell am force-stop com.lubyruffy.zwai
  adb -s "$android_test_device" shell am instrument -w \
    -e class "com.lubyruffy.zwai.QuotedCommandTest#$quoted_method" \
    com.lubyruffy.zwai.test/androidx.test.runner.AndroidJUnitRunner
done
```

Both cases must reach the actual packaged screen, use the system long-press
selection and Add to chat action, and verify the quoted-goal refusal with the
original text/quotation retained and no new message bubble. After removing the
quotation, submit the complete original command once; also verify an ordinary
quoted message still sends with its quotation. Count only actual transcript
messages: a textarea's nested accessibility StaticText is not a sent bubble.
Retain exact source/target and test-package hashes, action/DOM/accessibility
evidence, screenshots, result/exit status and owned-device cleanup. MockLink
does not prove an engine goal was stored in SQLite.

A DOM alert or native action acknowledgement does not prove visible rendering.
Synchronize the WebView's presented frame with a bounded wait inside the same
case deadline before capturing the refusal; inspect the actual screenshot for
the visible message. Preserve the original stale-frame screenshot and failure
separately. Do not repeat Send or other product input to repair capture evidence,
and do not claim native visual acceptance from DOM/AX state alone.

The PC and phone trailing-goal E2E specs exercise a completed objective followed
by a new suffix command. The scheduling admission browser regression runs with:

```bash
cd frontend
ZWAI_E2E_SCHEDULE_ANSWER=1 ZWAI_E2E_PORT=19306 ZWAI_E2E_DATA_DIR=.e2e-data-answer npm run e2e -- --output=/tmp/zwai-answer-e2e
```

`frontend/e2e/schedule-answer.spec.ts` blocks history reads until the actual
named `model_retry` SSE subscription paints the notice, then releases a real
successful `schedule_wake` call. It verifies the saved schedule, paired tool
receipt, Trace and reload without the rejected prose. Each concurrent run
must choose its own port, data directory and output directory. The normal
origin-wait and phone-projection cases can run separately with:

```bash
ZWAI_E2E_MODEL_API_ERROR=1 ZWAI_E2E_PORT=19307 ZWAI_E2E_DATA_DIR=/tmp/zwai-api-isolated \
  npm --prefix frontend run e2e -- --output=/tmp/zwai-api-evidence
ZWAI_MOBILE_E2E_PORT=14307 npm --prefix mobile run e2e -- api-error.spec.ts \
  --output=/tmp/zwai-phone-api-evidence
```

Each Vite preview owns its built dist until exit; do not rebuild that directory
while serving a case. Native acceptance uses an identified candidate in an isolated `ci-macOS`
case; Web E2E or successful SSH access does not accept the native window.

## Generation interruption and recovery (Issue #95 / C-035)

`TestInterruptCancelsButKeepsWork` uses the real loopback HTTP adapter with
a first chunk delayed beyond the former 120ms test assumption. It waits for
this turn's nonempty live reasoning delta while that generation remains open,
checks that the same thought is not yet stored, then interrupts it. The stored
thought must retain the live `message_id` and exact text, and a later turn must
still complete. A fixed startup sleep or an already-persisted event cannot
establish this partial-output retention boundary.

The generation and worker lifetimes are independent. `generation_test.go`
checks per-output identity for cancellation, deadline, transport failure and
output limits, with normal empty completion as a negative control.
`internal/engine/generation_test.go` checks original-turn attribution across
manager checks, cold recovery and already-completed output exclusion.
Deletion regressions hold a real worker callback across conversation/project
deletion, then release it: no cache or orphaned lifecycle event may reappear.
Test both callback/delete orders and preserve cache state when deletion fails.
Inject an events-query failure while the owner read still succeeds: the old
output must not be recorded in the current manager turn. Once reads recover,
the original-turn attribution must be reconstructed from durable events.
`internal/engine/generation_http_test.go` crosses the real keyless loopback
HTTP adapter, including shutdown versus planned handoff and a late Stop that
must retain the original failure cause. Partial-tool coverage must assert the
actual executor is never called with truncated invalid arguments; observing
only UI notifications misses ADK's independently consumed stream.
`internal/remote/generation_test.go` checks the real paired push/watch path,
including tiny text budgets that retain structured cause/recovery.
Inspect the original turn with `zwai trace <turn>` or
`GET /api/trace/:turn`; lifecycle rows must not become worker completion,
spawn receipts, human input or completion attention.

PC and phone generation reducer/component tests cover closure of old thinking,
reason/recovery retention, an independent new stream, late deltas, reversed
history/live arrival and unknown legacy causes. Replaying a stored event must
not reapply its state or notification side effects. Closing a partial
`tool_call_delta` must not complete a tool already executing.

```bash
go test -race -timeout 25m ./...
go vet ./...
(cd frontend && npm run lint && npm test && npm run build)
(cd mobile && npm run lint && npm test && npm run build)
ZWAI_E2E_GENERATION_INTERRUPTION=1 ZWAI_E2E_PORT=18995 ZWAI_E2E_DATA_DIR=.e2e-data-generation \
  npm --prefix frontend run e2e -- generation-interruption.spec.ts --output /absolute/unique-generation-output
(cd mobile && ZWAI_MOBILE_E2E_PORT=14195 npm run e2e -- generation-interruption.spec.ts --output /absolute/unique-phone-output)
make docs-check
```

On a busy machine the complete frontend unit suite can use
`npm test -- --maxWorkers=4 --minWorkers=1`. Sidebar drag fixtures use one fixed
timestamp for every item; creating each timestamp from the wall clock can
change the initial sort before the drag assertion. Keep the first failed
output when diagnosing scheduling-sensitive loader tests; a later green run
alone does not establish the original cause.

The desktop opt-in fixture controls real HTTP streams: partial thought,
output-limit interruption, same-worker resume, a distinct new thought and
reload. Assert the visible cause/recovery, closed old thought and absence of a
false worker failure. Keep the business-red trace separately from build or
artifact failures. Each browser run owns its server port, data and output.

Phone native acceptance uses the disposable iOS fixture below with
`IOS_LAYOUT_QUERY='/?mock=1&tick=0&history=generation-interruption'`, copies
`mobile/e2e/ios-generation-interruption.swift` into its private test target and
selects `AppUITests/BindFlowTests/testInterruptedGenerationRetainsReasonAndRunningWorkerAfterReopen`.
Android packages the same offline query into a disposable native tree and runs
`com.lubyruffy.zwai.GenerationInterruptionTest#interruptedGenerationRetainsReasonAndRunningWorkerAfterReopen`.
Both navigate the actual Agents controls, inspect the retained cause and
recovery, check the worker remains active and reopen its detail. Android also
reads the reached WebView DOM to assert the old thought has no animation.
Retain screenshots/accessibility, build/test exit status, source/asset hashes
and exact simulator/emulator cleanup. Desktop native checks still run only
inside `ci-macOS` through the isolated case lifecycle below.
The generation scenario is `tools/native_generation_interruption.py`, using
the keyless HTTP fixture and the VM-only accessibility helper in
`tools/native_generation_interruption_fixture.py` and
`tools/native_generation_interruption_ax.m`. Compile the helper in the guest
and pass it as `--ax` within a fresh `native_macos_case.py` case. Assertions
cover the actual window, same-worker recovery and absence of a false failure;
the runner's process success by itself is insufficient.

## PC timeout settings from a phone (`F-170`, `F-220`, `C-006`, `C-009`)

`internal/config/timeouts_test.go` checks the one-hour new worker default,
retained positive legacy values, detached snapshots, concurrent latest-state
patches and failed-save isolation. `internal/remote/timeouts_test.go` checks
narrow RPC serialization, secret exclusion, strict validation, atomic rejection,
frame budgeting and a reply bound to the saved candidate. The desktop settings
round trips in `internal/server/timeout_settings_test.go` must preserve omitted
worker/first-byte/idle fields and unrelated provider options.
`internal/config/timeout_contract_test.go` regresses a 120-second first-byte
value alongside a two-second idle value, including the phone compatibility
field. It also mutates nested cloud-tool maps and typed lists in snapshots and
aborted updates, asserting unchanged live values and preserved integer precision
under the existing `C-030` tool-options contract.

`internal/remote/timeout_adapter_test.go` crosses the real phone RPC handler,
atomic save and provider-cache invalidation into both loopback Chat Completions
and Responses adapters. With idle set to one second and first-byte raised from
one to three seconds, a response delayed 1.1 seconds must succeed in exactly one
HTTP request. The Responses case must retain its configured cloud tool and
protocol; checking the DTO alone cannot accept runtime application.

`internal/engine/timeout_settings_test.go` checks real loopback first-byte
failure through stored turn/Trace text, runtime application only after a saved
candidate, current context budgets during saves, and reviewer-anchor fallbacks.
`timeout_settings_test.go` checks real context deadlines for current and future
worker lifetimes. Keep the three-failure watchdog and restart regressions;
saving a timeout cannot reset those counts or resume a paused task. Provider
adapter tests must include a first-byte wait longer than stream idle, since
matching values cannot prove the two budgets remain independent.
`internal/engine/timeout_worker_ownership_test.go` reads the actual worker owner
while timeout saves replace the swarm settings, asserting the same goal owner
under `-race`. Run it with the existing ownership, parked-worker and handoff
regressions after changing snapshot reads in `worker_ownership.go` or
`restart_parked.go`; a timeout-only setter test cannot cover those readers.

`frontend/src/lib/settings-persist-delta.test.ts` covers provider-ID matching,
stale local drafts after phone edits, explicit timeout edits, failed writes and
queued reverts. `frontend/e2e/settings-timeout-concurrency.spec.ts` changes PC
timeouts through the real API while Settings stays open, edits an unrelated
field and verifies both saved values survive. Catalog refresh must also omit
unchanged timeouts after awaited discovery.

Phone hook/sheet tests cover loading, edited-field patches, invalid seconds,
retry, old-host feedback and stale link/generation callbacks.
`mobile/e2e/pc-timeouts.spec.ts` exercises save/reopen, read/write failures,
Back during a pending save and empty provider lists in Chromium/WebKit. Closing
a pending save cannot claim cancellation; reopening must reload the PC. The
phone-direct provider configuration stays separate. Run from the repository root:

```bash
go test -race ./internal/config ./internal/provider ./internal/remote ./internal/server ./internal/engine
go test -race -timeout 25m ./...
(cd frontend && npm test -- settings-persist.test.ts settings-persist-delta.test.ts settings-dialog.test.tsx)
(cd frontend && npm run lint && npm run build)
(cd frontend && npm run e2e -- settings-timeout-concurrency.spec.ts --output /absolute/unique-frontend-output)
(cd mobile && npm test)
(cd mobile && npm run lint && npm run build)
(cd mobile && npm run e2e -- pc-timeouts.spec.ts --output /absolute/unique-mobile-output)
```

Each concurrent browser run also needs its own server port and private data
where applicable; use the existing runner options below. These are validation
commands and expected assertions, not a statement that this candidate passed.
Native phone acceptance must focus numeric inputs in the packaged app, save,
dismiss the keyboard, reopen and inspect the displayed PC values and local
old-host feedback. Retain screenshots/accessibility evidence bound to source
and web asset hashes. Android Back must close the sheet before root navigation,
and its affected native bundle must compile. Desktop native behavior uses only
the isolated `ci-macOS` lifecycle below, with process/data cleanup before the
next case; browser E2E does not accept that native result.

## Native desktop isolation (F-430 / C-031)

The headless process fixtures wait for their readiness marker within a bounded
five-second interval, inside a ten-second test scenario budget. They do not
assume a separately launched process is ready in one second. The success fixture
deliberately delays initialization to regress that race; native case deadlines
and cleanup assertions remain separate.

Recorded guest acceptance: [2026-10-21 native isolation evidence](diagnostics/native-isolation-20261021.md). It binds a frozen candidate, precise legacy cleanup, serial real-window cases and host foreground observations; it is not latest-main or release acceptance.

The first-byte budget regression counts attempts at the real client transport
boundary, not the server handler's scheduling. Its handler is deliberately held
until the client finishes both 30 ms waits; the original transport error and
configured budget must survive. This prevents a delayed handler from reporting
fewer attempts than the client actually made. See
`internal/provider/first_byte_test.go`; no provider runtime budget is changed.

A native case reports `scenario timeout` only for its actual scenario deadline.
A process-inventory command timeout retains its own command, limit and original
streams before cleanup. Inventory failure still cancels and waits for directly held
child processes; it cannot declare unknown descendants gone.
The incomplete-cleanup latch and private data remain until a fresh complete
inventory and fault cleanup prove exit. A failed runner's surviving scenario
may write later evidence; those files do not make the failed round pass.

The SIGKILL fault-cleanup regression owns a private process namespace. Its
listing seam queries the real held PIDs with `ps -p`; the actual parser, process
identities, durable fault receipt, STOP/KILL/CONT and exit checks still run.
An unrelated full-machine inventory failure cannot prevent this cleanup test
from reaching its injection. Assertion failures retain the runner's complete
receipt and evidence before temporary state is removed. This test accepts
cleanup behavior, not the production collector's completeness: production keeps
its full inventory, five-second command deadline and unknown-process refusal.
See `tools/test_native_macos_fault_cleanup.py` and the separate collector tests
in `tools/test_native_macos_resilience_fault.py`.

Saved-input acceptance reads the persisted draft twice, including unchanged
attachment bytes and the current native composer value. Healthy-save copy is
optional; changed text/state/files/native values on the later read still refuse.
A binary-stream timeout fixture first witnesses its child's completed writes,
then starts the short communicate deadline. Slow process startup cannot stand
in for lost output; the original failing run remains separate evidence.

The actual parent launcher sends the private handoff before coordinating focus
for its owned successor within the same deadline. Registration observations do
not activate; the final native main-thread action rechecks the parent window,
foreground, child kernel birth/executable/native identity and cancellation. On
macOS 14 and later it yields permission then requests cooperative activation;
older systems make one targeted request from the parent. App switches, hidden or
minimized windows, a separate native modal window and late queued work refuse
activation. Genuine pixel readiness, child exit or caller cancellation cancels
and joins the observation owner without blocking the original Ready receipt.
`internal/desktop/recovery_focus_test.go` tests the actual coordinator and
`tools/test_native_macos_recovery_focus.py` compiles its Objective-C entry with
SDK seams. `cmd/zwai/desktop_recovery_focus_test.go` preserves spec-before-focus ordering,
cancellation and the real ready/commit/committed protocol. Neither opens a host
window. Bounded native diagnostics retain refusal/request results and sampled
identities; unsampled facts remain unknown. A request result cannot accept
recovery: native fault acceptance still requires fresh product nonce-ready and
commit evidence before QA may activate the successor. No focus phase is added
to the recovery journal/API or used to extend the deadline. Focus observation
errors retain a safe warning and leave the original Ready/exit/context outcome
in control. The exit regression reaps a real child, delays publication of its
actual Wait/EOF results, then exercises the real kernel identity refusal before
releasing those results; observation failure cannot turn user close into retry.

The render-probe cover has two independent assertions: a native screen capture
must omit the nonce, and a WebView snapshot must still match a fresh challenge.
`go test -race ./internal/desktop` exercises actual injected JavaScript, trusted
RGB messages, complete/stale/missing nonce pixels and recovery transitions.
`python3 -m unittest tools/test_native_macos_render_cover.py` compiles the actual
Objective-C entry with view/JavaScript seams, without opening a host window;
it checks cover-before-paint, opaque geometry, flipped/offset layout, resize,
single-cover reuse, input/AX exclusion and failed colour preparation. This is
also a pixel regression: an oversized native dirty rectangle must not paint
outside the cover's own bounds or erase the rest of the window. It is
also included in `make docs-check`. Native acceptance separately checks light
and dark themes, both palettes, modal/settings changes and recovery in the
isolated guest; a successful JS callback alone never accepts WebView painting.
`frontend/e2e/render-probe-cover.spec.ts` runs the actual injected script in
Chromium against a real Radix modal and theme/settings changes, including
background compositing while body pointer input is disabled and exact restoration
of that pointer state after synchronous sampling.

Native macOS acceptance runs in the UTM **ci-macOS** guest. The user's host
desktop may already be running zwai; do not launch or focus another desktop
app there, replace its cache bundle, change its engine/data, or bring UTM,
Finder, a browser or a simulator to the foreground for a test. Builds and
headless unit/browser tests may run on the host. Use background guest control
(SSH when available) and guest-side screenshots/accessibility. An unavailable
guest is a pending native check, never permission to test on the host.

### Case-owned preparation and finite power handoff (Issue #94)

The Python API `main(argv=None, *, prepare=None)` supports an internal callback,
not an extra CLI mode. The runner first holds its real case lock, saves an
incomplete cleanup receipt, creates private directories and a `ProcessScope`,
then calls `prepare(lock, receipt, environment, processes, scope, work_deadline)`.
Return `None` or `CasePreparation(before_launch, handoff)`. `before_launch()`
runs immediately before App Popen; `handoff(scenario, deadline)` runs after the
actual scenario starts. Neither callback may allocate a fresh work budget.
A process-local monotonic value is not a portable deadline: the guest Python
3.9 producer and child were measured with different monotonic origins. Shared
work expiry is a single conservative projection of the original local budget
onto system `CLOCK_MONOTONIC_RAW`; frames also retain their wall expiry. A
receiver maps the original remaining time once to its local monotonic clock. Later checks read the same expiry, never allocate another TTL.

`native_macos_case_power.make_prepare` registers its finite setup bridge in the
original process list before invoking the login/admission callback. The scenario
calls `start_setup_hold` at entry and publishes its actual birth and assertion
owner. The driver independently checks that owner's PID, birth, executable,
direct parent and three IOPM assertions, reaps its bridge, then publishes the
handoff commit. No ACK, malformed/partial ACK, a changed identity or cancellation
fails the case and follows the original cleanup. For an idle observation, start
the measured `-d -i` owner, prove its two assertions with no `UserIsActive`, and
call `release_setup_hold` before recording t0. Power is never login evidence.

`native_macos_prepare_login.make_before_app` creates a callback which returns
a final admission closure after Root approves the desktop. Power retains that
closure for `CasePreparation.before_launch`; preliminary approval cannot replace
this final check. The callback binds the original runner/case,
source/package, exact helper/client bytes and current session. The caller must
also verify the complete candidate inputs. The actor publishes current PNG and
ready receipts under `prepare-login/`. Root must inspect each current image:
only a strict locked result plus a ci/US/empty-password frame permits the one
input; a missing SDK flag stays unknown and does not load a credential. The
actor checks the sender's exact kernel/argv, case marker and birth. The pinned
client checks the case-root inode, actor/server identity, source and relative
socket, prints READY, then accepts one memory stdin payload within the original
eight seconds. A new desktop frame and approval are mandatory after input;
raw locked=true always refuses. Both approvals expire at the original capture
plus 60 seconds, checked again at final App publication. No password is written,
hashed, logged or put in argv/environment. Login source is read by explicit
interpreters; a LaunchServices executable entry still needs its own mode check.

HOST regressions exercise the actual runner main, real flock, real headless
children and sockets, cancellation/partial input/ACK and preparation cleanup.
The preparation fixture publishes complete readiness JSON by atomically renaming
a temporary file from the same directory.
OS leaves are replaced only for headless tests. Run:

```bash
python3 -B -m unittest discover -s tools -p 'test_native_macos_case*.py'
python3 -B -m unittest discover -s tools -p 'test_native_macos_prepare_login.py'
swiftc -typecheck tools/native_macos_prepare_login.swift
```

These checks do not demonstrate a guest login, window, fault recovery or release.

### Access evidence and maintenance exclusion (Issue #97)

Run the host preflight with the provisioned private profile and a new evidence
directory; it needs no repeated address or credential entry:

```bash
python3 tools/native_macos_access.py --profile /absolute/private/access.json --evidence /absolute/private/new-access-check
python3 tools/native_macos_access.py --profile /absolute/private/access.json --evidence /absolute/private/new-dhcp-check --repair-network
```

The profile must belong to the current user and exclude group/other permissions
(normally mode `0600`). It contains references, not passwords:

| Fields | Meaning |
|---|---|
| `vm_id`, `vm_name`, `mac`, `user`, `uid`, `interface` | Selected VM/account and exact Ethernet identity |
| `identity_file`, `known_hosts`, `host_key_alias` | Existing SSH key path and pinned host-key identity |
| `shared_host_root`, `shared_guest_root` | Absolute host/guest views of the same shared directory |

The default preflight freezes the guest probe source and writes diagnostics,
without changing network/service/VM state. It discovers candidate addresses
from DHCP leases for the selected MAC, checks their actual routes, and avoids
SSH to an old address routed through a proxy. SSH uses batch mode, the explicit
key and strict pinned host checking. A failed SSH probe falls back to newly
identified shared-queue requests, including nested queue roots. Replies must
match request/session, frozen source and guest identity; local monotonic waits
bound the attempt without treating guest/host clock differences as expiry.

Exit `0` means a target control channel was verified; shared control can be
available while guest networking remains down. Exit `3` means control is
unverified or requested repair did not converge; exit `2` is a profile, protocol
or probe failure. `--repair-network` requires a fresh queue and verified current
peer inventory, then requests one guarded guest DHCP renewal. SSH-only access
does not fabricate a queue claim for maintenance. Repair succeeds only after a
new pinned SSH/identity probe also succeeds; `guest_renewed_host_unverified`
retains exit `3`. The CLI never reboots the VM. A suggested rebuild still needs
actual exclusive maintenance ownership covering that separately authorized action.

Host evidence includes `source.json`, `host.json`, `queue.json`, `guest.json`,
`observation.json` and `decision.json`. Before publishing each request, a
`request-<id>.json` stores its queue/ID/timeout without the nonce. `repair.json`
retains the real request identity and outcome; a successful guest renewal also
creates an `after/` preflight. Deadline expiry leaves `outcome_unknown` and
`cancellation=unconfirmed`; removing this caller's pending request cannot prove
it never ran and does not authorize an automatic retry.

`tools/native_macos_access_state.py` classifies a strict current-probe observation;
it does not run commands. Host VM-interface presence and an old IP's route are
separate from the selected guest's identity, carrier, IPv4, default route and
sshd loaded/disabled state. A loaded on-demand sshd without a current listener
is not a disabled service. An unsupported `utmctl exec` result is retained as a
reason rather than retried as progress. A shared queue needs a newly identified
read-only request and matching response; old `ready.json` and cross-host wall
clock subtraction cannot prove that channel current or expired. Classification
always retains `native_acceptance=pending`.

An already enabled guest Screen Sharing service can provide a separate operator
recovery path through a pinned SSH tunnel when host desktop automation is
unavailable. Use normal existing-account authentication, recheck the expected
console user, VM identity and current unlocked state, and reap only the owned
tunnel afterward. This does not authorize enabling a service or changing TCC
or lock policy. It is not implemented by the access CLI; SSH or authentication
success alone still cannot accept guest unlock or native-window behavior.

`tools/native_macos_access_guest.py` exposes `inspect`, `renew-dhcp` and
`hold-maintenance`. Each checks Darwin, the ci-macOS hostname, expected UID,
hypervisor and the explicitly selected Ethernet MAC before use. `inspect` reads
the selected service, carrier/IP/default route, SSH socket and service state;
its `host_connectivity=unverified` is deliberate.
Its fresh process inventory supplies `current_peer_roots` and controller/child
PIDs without argv or nonces. Historical roots remain diagnostic candidates;
they leave the current maintenance inventory only after controller death and
their recorded effects/children are verified. Missing or ambiguous inventory is
`null` and blocks repair. A normal historical stop acknowledgement does not
weaken the current guard's strict completed-command check.

```text
python3 tools/native_macos_access_guest.py inspect --expected-uid <uid> --expected-mac <mac> --interface <interface> --evidence <new-directory>
```

Maintenance additionally requires `--own-queue-root`, `--request-id` and each
current `--peer-queue-root` (repeatable). The exact own claim must exist and its
root must be among those checked. Only that claim is exempt; another claim needs
a matching id/nonce, `status=exited` and integer exit code before it is considered
finished. Both maintenance modes take the existing native-case lock inode with
nonblocking exclusive flock and require a complete previous cleanup with no
remaining owned PIDs; they leave its receipt unchanged.

`renew-dhcp` only operates on one matching enabled service already using DHCP.
It runs `sudo -n ipconfig set <interface> DHCP` once under that lock, then rechecks
bounded network snapshots. It does not cycle interfaces or reboot.
`needs_host_recovery` exits 3 and preserves that the guest network remains down;
a successful renewal still leaves host connectivity to a separate SSH probe.
Evidence includes `identity.json`, `network-before.json`, `dhcp-command.json`,
`network-after.json` and `result.json`; refusals use `failure.json`. An existing
evidence directory is refused without writing into it.

`hold-maintenance` additionally needs an own `--token` and a positive `--ttl` up
to 180 seconds. It keeps the same flock, checks peer claims while holding it,
and writes `maintenance-ready.json` with actual ownership, identity, heartbeat
and guest monotonic timing. Only a matching private release token or bounded
expiry ends the hold; `maintenance-finished.json` records lock release. A host
performing an already authorized VM action must keep actual ownership through
that action. A classifier's `restart_admitted=true`, or a lock acquired and
released during a probe, cannot substitute for this hold. Neither helper
reboots the VM or accepts application-window behavior.

The state tests cover stale probes, unsupported backend fallback, separate
network/service/authentication failures, strict input and privacy boundaries.
The guest tests include actual two-process contention between maintenance and
the native runner's `case_lock`, wrong and correct release tokens, TTL release,
peer refusal and a single targeted DHCP attempt.

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p 'test_native_macos_access*.py'
```

#### Host NetworkSharing/PF stall

Issue #97 observed the correct guest MAC with inactive Ethernet, no IPv4 or
default route, working local SSH service, and no host `bridge100`/`vmenet`
interface. VM and UTM restarts did not repair it. Repeated samples of the
identified `/usr/libexec/InternetSharing` process showed `PFUserDeleteRules`
waiting on synchronous XPC. These combined observations located the current
failure in host sharing; an old address routed through a VPN alone cannot do
so. Do not blindly disable the VPN, repeat restarts or reinstall the guest.

For the same evidenced failure, an authorized operator/Agent can use this
bounded maintenance procedure while preserving the peer exclusion above:

1. Retain fresh interface/route/guest identity evidence, the exact sharing
   daemon's process identity and repeated bounded samples. Inspect PF status,
   complete filter/NAT/anchor policy and reference owners before any change;
   an empty state table is insufficient. A pfd exit status of 3 is not proof
   of a signal crash or a documented Apple exit-code contract.
2. If that daemon needs replacement, recheck its exact PID and executable,
   attempt graceful termination with a bounded wait, and escalate only for
   that still-identified process. Never kill by process name, change SIP or
   alter another VM's ownership. In this recovery, replacing the stalled
   daemon alone still left the new process waiting.
3. Only after checking the policy impact, `sudo pfctl -E` can acquire a private
   temporary enable reference. It enables existing PF policy even without
   changing rules. Retain the returned reference privately; recheck sharing
   interfaces, the target MAC/address/route and strict pinned SSH.
4. Inspect `sudo pfctl -s References` for independent system ownership, then
   release only this maintenance reference with `sudo pfctl -X <own-reference>`.
   Recheck owners, network and pinned SSH after release; releasing the last
   reference can stop PF. Never use `-d`, `-F` or `-f` for this cleanup, release
   another owner's reference, or manually start pfd.

This recovery produced `bridge100`/`vmenet` and verified pinned SSH after the
temporary reference was acquired; pfd retained its own reference after the
maintenance reference was released. It needed no further VM cold restart,
rule changes or persistent host service. The actual access CLI returned
`access_available`, `control_channel=ssh`, and `native_acceptance=pending`.
That result proves this access recovery, not future zero-reference startup
or native-window behavior. The CLI collects evidence and supports only the
guarded guest DHCP operation; it does not automate this host procedure.

[Apple container #2275](https://github.com/apple/container/issues/2275) and
[UTM #7951](https://github.com/utmapp/UTM/issues/7951) contain related user
reproductions in the official repositories, not an Apple-confirmed fix.
Check the installed `pfctl(8)`/`pfd(8)` manuals for the current host; `-E`,
`-X` and `-s References` respectively acquire, release and inspect enable
references. Preserve local stacks, maintenance and release receipts, then
run the access preflight again before resuming the separate native case.

Inside the guest, run one scenario at a time:

```bash
python3 tools/native_macos_case.py \
  --app /absolute/candidate/zwai.app \
  --evidence /absolute/private/new-case-evidence \
  --timeout 50 --budget 60 -- /absolute/scenarios/check-native-window.sh
```

### Daily native round: 60 seconds including cleanup

Measured acceptance: [2026-10-22 three serial native rounds](diagnostics/native-minute-20261022.md),
14.50 / 12.30 / 12.06 seconds including cleanup and independent checks, bound to
the recorded application and harness inputs.

With the identified candidate, prepare the direct Accessibility helper once on
macOS (host builds are safe; this command opens no window):

```bash
make native-smoke-prepare
```

Transfer the helper and its adjacent JSON receipt with the tracked harness
sources, retaining the same `tools/` and `bin/` layout. With `ci-macOS` already
running and those prepared inputs present, run the offline smoke scenario in
the guest:

```bash
make native-smoke APP=/absolute/candidate/zwai.app EVIDENCE=/absolute/private/new-round
```

The Make entry uses `native_macos_smoke.py --case` and checks the prepared
helper's identity and the current invocation's control prerequisites under the
existing case lock, before allocating state, copying or launching the app.
The noninteractive probe reads Accessibility/screen-capture permission and the
current console user/login/lock observations; it never requests permission or
performs input. `native-preflight.json` retains the helper identity, case,
command, deadline, raw streams and classified blockers. Its maximum five seconds
come from the same round budget. A timeout retains its original streams in the
runner timeout receipt and cannot become a scenario timeout. A blocked probe
leaves the previous cleanup latch unchanged and launches nothing.
An absent lock flag remains unknown; `ready_for_attempt` still requires the
existing real window, pixels and behavior checks. This result describes only
the actual invocation: an SSH denial does not reject an authorized Terminal
route. Each invocation probes afresh; a failure should first resolve its
recorded prerequisite or use the existing authorized route rather than repeat
the same expensive case. `tools/test_native_macos_smoke_preflight.py` exercises
the actual CLI/runner wiring, refusal before copy/launch, budgets, failure
evidence and the compiled helper preflight with headless OS seams. These offline
tests do not accept a real guest or installer.

The typed smoke fixture is one complete sentence containing the current case
nonce. An unfinished trailing label can create macOS's
`NSAutocorrectionSuggestion` windows and leave the exact draft behind the
existing multiple-window refusal. Keep the strict window and draft checks;
do not dismiss unknown windows, change input settings or relax the round budget
to accept that state. The run-level regression preserves one fill, exact draft
readback and one Send, and refuses both honest and incorrectly ready AX-one /
CG-three snapshots. A corrected fixture still needs a fresh native round.

This single command replaces separate manual preparation, API polling, window
inspection and cleanup steps. It uses fresh case state, waits for actual
readiness and completion rather than fixed startup sleeps, verifies a real
offline worker/tool run and Trace, inspects the native window through direct
Accessibility calls, and saves its PID-owned window screenshot. The helper
binds the WebView to this case's origin, desktop shell and PID; input must read
back exactly and the unique Send control must be enabled before it is pressed.
This avoids one System Events request per AX attribute. Missing accessibility/screenshot access
is a failed native check. HTTP success alone does not accept the window.
The scenario verifies the helper's source, build arguments and binary hash from
its preparation receipt; missing or stale inputs fail without cold compilation.
Bundle identity compares existing paths with `Path.resolve(strict=True)`: Darwin
aliases such as `/var` and `/private/var` are accepted only when they resolve to
the same directory. Missing or different bundles fail; origin, case and PID
checks remain exact. Before checking a helper's exit code, JSON or identity,
`smoke-native-last.json` saves its raw stdout, stderr and return code alongside
the requested bundle, origin, PID and action, including failed responses.
AX children are traversed as a graph using `CFEqual` identity: revisiting one
element does not create another composer or Send control. The helper counts all
visits, including empty nodes and repeated edges, toward its 5000-visit bound;
new nodes beyond depth 45 still fail. A genuine limit stops traversal and fails
the case rather than accepting an incomplete set of controls.

Smoke snapshot/fill/Send and resilience snapshot/fill/press/select-thread
validate a nonempty candidate's actual AX element type, `AXWindow` role and
owner PID before raising or traversing that window or requesting input. Its
strong lifetime covers the entire action. A proxy,
unknown read/type or wrong PID is a refusal, including during snapshot; no
window mutation is permitted before this boundary. The smoke helper's empty
window snapshot still reports not ready and cannot permit fill or Send.
Engine/API presence can precede `NSRunningApplication` registration. After the
kernel case guard, only `snapshot` may return `registration_pending:true` and
`ready:false` for a missing native application or unavailable same-PID bundle
URL/path. This returns before activation or AX access. `bundle` and
`requested_bundle` then echo the requested target; `case_verified:false` and
`bundle_verified:false` explicitly withhold native identity acceptance.
The existing `wait_window` polls within its original shared deadline and runs
the strict window assertions only when ready. Fill/Send, a wrong NS PID or a
nonempty wrong bundle remain immediate nonzero refusals. Regress these branches
through the actual C main, plus pending-to-window, deadline exhaustion and
identity-error/cancellation propagation through the actual Python caller.
Registration pending is distinct from a registered app whose AX window list is
still empty. The formal smoke snapshot requests one guarded application
activation even for that empty list, while keeping `ready:false` and performing
no raise, fill or Send. Reuse `tools/native_macos_smoke.py`'s `wait_window`;
do not prepend a read-only AX-nonempty polling gate that prevents the formal
activation path from running. The existing shared deadline includes each helper
call and poll; a successful fresh window observation remains mandatory.
`tools/test_native_macos_activation.py` checks the actual C zero-window caller's
single activation and absence of window input. `tools/test_native_macos_smoke.py`
checks registered empty-window responses followed by a valid ready response through
the actual `wait_window`/`native_snapshot` boundary without resetting that deadline.
These protect existing formal behavior; they do not establish why a particular
guest returned an empty AX list or accept its native window.

Every resilience helper invocation saves a unique
`native-actions/<id>/receipt.json` before JSON parsing or raising a shortened
terminal error. The receipt retains the original command, case/PID, execution
timing, requested deadline and actual exit/exception, with independent base64
streams and explicit missing/empty types. Failed receipt storage cannot replace
an existing command error; a successful command without its required receipt
refuses acceptance. `tools/test_native_macos_native_evidence.py` runs the actual
caller across success, long refusal output, malformed JSON, timeout and storage
failure; legacy choose-file receipt paths remain compatible.

Automatic replacement acceptance reads the successor's current session,
generation, engine and fresh pixel-ready event before QA activation. Activating
an unready successor could unpause its health controller and manufacture the
evidence being tested. `tools/test_native_macos_resilience_phases.py` regresses a
helper that would create ready only when the test activates it; that helper must
never be called until the product has supplied its own ready event.

An extra same-PID window must be identified before changing a QA setup or window
selection. Size, empty title and floating layer alone cannot identify an input
accessory. Preserve its actual AX role/subrole, identifier, children and native
pixels. A confirmed Caps Lock indicator can be corrected in the isolated guest
under the case exclusion lock; verify actual modifier readback and fresh pixels.
Per-event CG flags and a successful asynchronous setter alone do not establish
that Caps Lock is off. Navigation also needs page-specific arrival controls:
main-page tabs can themselves be radio buttons. Use the actual Settings/Search
and Back/Close controls when keyboard delivery is not independently established.
Window resizing requires stable AX/CG size readback and matching screenshot
dimensions at the screen's backing scale; a successful AX setter only confirms
the request. An unreachable requested size remains a failed assertion until the
screen constraints and a new attainable target are explicitly established.

The resilience helper's application-only `activate` command uses only
`NSRunningApplication`, without AX window lookup or raise. Its activation-only
result is not a window receipt: subsequent snapshot/input still requires a
fresh positively identified window and actual pixels. The file-panel path has
its own independent scan and acceptance rules below.

Recovery activation keeps the original `ZWAI_NATIVE_APP` case anchor rather
than replacing it with a caller-supplied successor path. The helper derives the
bundle from the kernel executable, accepts the original or the same case's
home/Library/Caches/zwai/desktop-recovery-*/zwai.app with its 16-hex session
file, and hashes both actual core files before requesting activation. With no
proof, their full SHA-256 values must match. Product `WriteMacApp` rewrites
Info/icon and re-signs its copy, so equal raw core bytes are not a general
recovery invariant. Never replace the original anchor or learn an expected
digest from the target being accepted. Prepare a proof through the actual
writer and signing system before the timed round:

```bash
python3 tools/native_macos_recovery_identity.py build --checkout "$CLEAN_SOURCE" --source "$SOURCE_SHA" --output "$NEW_BUILD_DIR"
python3 tools/native_macos_recovery_identity.py prepare --writer "$NEW_BUILD_DIR/recovery-identity-writer.test" --binding "$NEW_BUILD_DIR/writer-binding.json" --original "$ORIGINAL_CORE" --original-sha256 "$ORIGINAL_SHA256" --source "$SOURCE_SHA" --version "$VERSION" --output "$NEW_PROOF_DIR"
```

The build checks tracked and untracked desktop-package/module inputs before
and after compilation, disables external Go overlays/workspaces and requires a
new output outside the checkout. Its Go overlay calls actual `WriteMacApp`
with the real embedded icon; it does not copy the writer implementation or
launch an App. Prepare on the target signing system: strict/all-architecture
verify the frozen original, retain the writer's actual codesign argv/exit/raw
output (its production best-effort error is insufficient), verify two derived
copies and require identical full bytes. Compare every pre-signature code/data
and load-command byte, normalizing only the measured signature datasize and
linkedit virtual/file size fields, with strict tail/segment constraints.

Proof preparation attempts each available raw stream and exit/timeout receipt
even if another receipt write fails. A failed command retains its real exit
and raw streams as a `CalledProcessError` cause; a timeout propagates the
original `TimeoutExpired` object, partial streams and any existing cause. Secondary storage errors
are retained in an `evidence_errors` exception attribute and stderr JSON with
only artifact filename, operation, error type and errno. This works on the
guest's Python 3.9; a broken stderr cannot replace the primary failure.
Missing timeout streams remain missing. A successful
command with unavailable mandatory evidence still refuses preparation. The
recovery-identity tests exercise real child exit 7 and timeout paths while
controlling only receipt I/O, including independent stdout/stderr/status
failures and partial retention. A real CLI subprocess checks that safe
secondary details reach parent-captured stderr without losing the actual Git
failure; system-Python 3.9 and current-Python regressions remain separate from
App or guest UI acceptance.

The prepare command prints a binding object. Freeze its absolute `path`, full
`sha256`, `source_sha` and `version` as the JSON environment value
`ZWAI_NATIVE_RECOVERY_IDENTITY` in the isolated case capsule. Schema 1 proof
binds the original and derived full core hashes, derived Info/icon hashes and
the preparation receipt hash. The helper reads and hashes the actual proof;
unknown/extra schema fields, missing bytes, wrong pin/origin/source/version,
metadata changes and a different full target hash refuse. A present bad proof
also refuses when core hashes happen to match; no fallback or subprocess is
added to activation. Only proof path/hash/stage and sampled identity are logged.
The inherited case marker, NS PID/resolved bundle,
and kernel birth/executable before and after the request must agree; unknown
registration and nonempty wrong identities refuse. Layout/hash checks share
the original two-second deadline, with no new loop or fallback. Structured
`activation_identity` diagnostics retain actual/expected paths, hashes and the
sampled reason without reading the recovery spec or its handshake token.
`tools/test_native_macos_successor_identity.py` runs the entire real helper main
with kernel/NS/clock seams and real temporary files. Its actual product-writer
regression uses real signed Mach-O page-size and Info-version variants, broken
original signatures, and code/data/signature/metadata mutations. The companion
`tools/test_native_macos_recovery_identity.py` checks full-payload normalization,
dirty/untracked build inputs, raw failures and timeout streams, deterministic
writer/signature validation and CLI binding. Original/same-core success,
cross-case/layout/core/NS refusal, malformed process arguments and PID reuse
remain covered. The existing activation tests
still exercise one-request/runloop/deadline and AX-free behavior. These tests
accept QA targeting only: an earlier uncaptured NS refusal stays unknown, and
a separately observed 30-second product replacement failure remains pending
until its own real window and timing evidence passes (`C-034`, Issue #108).
The earlier raw-equality review missed the actual writer's signing side effect;
retain its software-green/native-red history rather than describing it as a
product failure or accepted native recovery.

When an action requests activation, it does so at most once; the guarded window
actions raise only the validated case window. Activation waits refresh
`NSRunningApplication.active` through short main-runloop slices. A single
two-second monotonic deadline starts before activation; each slice is at most
10 ms and no longer than the remaining budget. At least one refresh precedes
the `active` check, including cached true; a refresh crossing the deadline fails.
There is no fixed activation sleep, repeated activation within the invocation or automatic Send retry.
The resilience caller still verifies its actual frontmost PID independently.
Its existing polling and shared 30-second replacement deadline are unchanged;
the helper's activation wait cannot reset that deadline.

The smoke helper retains the same collected window/draft/Send predicates on
activation or fill/Send guard failure, with `activation_requested`,
`activation_ready`, `activation_elapsed_seconds` and `press_attempted`.
An attempted AXPress that returns an error stays `press_attempted=true`; it is
not retried. The resilience activate action retains requested/active/frontmost
PID/activation-only status/elapsed in stdout and, on failure, stderr because its
caller uses stderr for nonzero exits. Those fields reuse one sample; no AX raise
success is inferred from application activation.

On macOS, run the headless helper regression with:

```bash
python3 -m unittest discover -s tools -p 'test_native_macos_activation.py'
```

It compiles actual Objective-C wait and caller/action logic with OS/clock seams,
covering delayed cached false, stale true, deadline expiry, one activation and
zero/one Press. It also compiles both complete helpers against the real SDK and
runs their no-target refusal before window/input calls. These checks do not
exercise the real collector or accept a guest window. The separate native
diagnostic observed cached false becoming true after a main-runloop iteration;
the reverse transition is a seam regression inferred from the API's caching
semantics, not a claimed native reproduction. Neither result establishes the
cause of an earlier failed Send. Record QA helper source/build hashes separately
from the unchanged application ZIP/source; fresh native acceptance remains
required for the changed harness.

The runner's default `--budget 60` counts guest checks, isolated copy/preparation,
launch, scenario, cleanup and receipt writing. `--timeout` remains the scenario's
upper limit, further capped by the remaining round budget after reserving cleanup
time. Preparation that exhausts that allowance cannot start the next process.
`cleanup.json` records each phase, total time and whether the budget was exceeded.
A successful scenario whose complete round exceeds the budget returns `124`.
Cancellation and cleanup failures retain their own nonzero results. Cleanup
always runs to completion or reports failure, even after the deadline; the
budget is a success gate, not permission to leave running processes behind.

App/helper build and first transfer are preparation costs outside this daily-round target;
retain their source SHA, candidate executable/ZIP SHA-256 and elapsed time
separately, then bind the smoke receipt's executed binary hash to that candidate.
An app path or a version label alone is not build provenance. Reuse the same
prepared candidate for repeat rounds, never the previous case's app copy or data.
Run cold checkout/build acceptance and deliberately long recovery scenarios
separately, with an explicit larger `--budget` and corresponding `--timeout`.
They do not establish the daily round's latency. For a performance receipt,
retain at least three serial guest rounds, each with real window/behavior evidence,
`cleanup_complete: true`, no owned PIDs, data removed and total time at most 60s.
Offline runner tests cannot establish that measurement.

The runner refuses physical macOS hosts before launching anything; its VM guard
does not verify the UTM inventory name, so the operator must select `ci-macOS`.
It holds a guest-user lock, makes a disposable app copy and fresh home/cache,
data and workspace, starts
the copied executable with `desktop --mock --data-dir`, and removes inherited
OpenAI seed variables. Child processes receive a private `HOME` and `ZWAI_HOME`;
macOS checkout re-exec bundles under `HOME/Library/Caches` are removed with the
case rather than replacing the guest user's normal cache. The runner's own
environment is unchanged. The scenario receives the paths in environment variables
documented by `python3 tools/native_macos_case.py --help`. Readiness and native
window/behavior assertions belong to that scenario; a process launch or a zero
exit status is not by itself UI acceptance. Use the scripted offline provider
or a keyless loopback fixture, never a production model/account.

For an HTTP-adapter scenario, stop the initial mock client and engine inside
that same case, write only the case's isolated fixture configuration, and
relaunch the copied app without `--mock` against a keyless loopback fixture.
Keep that transition and all helper processes inside the scenario's owned
lifecycle; the final process/data cleanup still belongs to the runner. This
does not enable a production-provider test or change the candidate bundle.

Each case must finish before another starts. This includes **success, failure,
timeout and cancellation**: retain assertions, stdout/stderr, screenshots, source
and package identity outside the disposable directory; stop the app, its engine
and case-owned children; wait for exit; then remove the app copy, home/cache, database,
configuration, uploads and managed workspace. Merely closing a window leaves
the engine alive. `cleanup.json` records the case ID, remaining owned PIDs and
data removal; incomplete cleanup fails the case and blocks the next case.
Only case-owned paths/processes may be removed. Never run `killall zwai`,
`pkill zwai`, or clear the normal `~/.zwai-swarm` directory. A deliberate
restart/reopen recovery scenario keeps its state between its own steps; a
different scenario always starts with new data.

Scenario helpers must inherit the runner's `ZWAI_NATIVE_CASE_ID` marker, even
when they create a new session or outlive a short-lived parent. The marker
lets cleanup find those detached helpers without killing unrelated processes.
Do not deliberately clear it and create an untracked daemon: this runner owns
cooperating test processes, not an OS process container. System services shared
with other applications are not test-owned processes.

Darwin process-argument inspection retries the complete size/read sequence only
for `EIO` or `ENOMEM`, at most three attempts with 10 ms between attempts.
Persistent errors and unknown error categories still fail the case; diagnostics
include the PID and numeric errno without recording process arguments or
environment values. `tools/test_native_macos_case.py` injects transient,
persistent and unknown sysctl failures to verify those boundaries. A failed
headless regression preserves its evidence directories and prints their retained
location, including `cleanup.json` and app/scenario logs. These checks do not
establish the cause of an earlier failure whose evidence was not retained.

For a reported checkout defect, preserve that checkout and record HEAD plus
dirty-file hashes before/after. Transfer the exact identified source snapshot
or candidate to the guest. Host-side provenance/asset checks remain read-only;
report guest native behavior and actual host delivery separately. Never use an
exact-checkout requirement as permission to open the user's host UI.

When the defect concerns `go run ./cmd/zwai desktop` or checkout frontend
preparation, the scenario stops the initial copied app and engine, places the
identified source snapshot in the case's private workspace, then runs the
reported command there with the inherited isolated `ZWAI_HOME` and case marker.
Use a case-local keyless loopback fixture, or the reported command's mock option,
for that launch; do not inherit a production provider configuration.
Retain its build/startup output, served asset identity, native window and completed
offline turn. Starting the runner's prebuilt `.app` alone cannot accept this
checkout command. The scenario's command and descendants remain case-owned.

`make docs-check` runs the runner's offline regressions for physical-host
refusal, serial ownership, success/error/timeout cleanup and unrelated process
preservation. These use headless child fixtures; they do not accept a native
window. A real guest window and cleanup receipt are separate evidence.
`tools/test_native_macos_smoke.py` checks helper preparation/identity, the shared
deadline, case/loopback identity, worker-specific tool receipts, unchanged
workspace bytes, draft/Send checks, visible-result requirements and PNG pixel
completeness. Offline checks and helper compilation do not prove guest
Accessibility access or native rendering.

A native helper's `submitted=true` or successful `AXPress` confirms only that
its action was posted; its returned AX tree may precede that action. For queued
messages, send once, await the exact durable follow-up ID/thread/text prefix
and an empty composer, then send the next message. Keep those acknowledgement
observations inside the existing case deadline; reject duplicate or foreign
rows and never resend an unacknowledged message. Preserve the initial failure.
The real HTTP queue checks in `frontend/e2e/api-error.spec.ts` show the durable
boundary; a helper return or immediate single GET cannot replace it.

## Desktop renderer resilience (F-115 / C-034)

Incident evidence and the upstream-cause uncertainty are recorded in
[the white-screen investigation](diagnostics/desktop-white-screen-20261022.md).
`internal/desktop/recovery*_test.go` exercises the actual controller with injected
clock/probe/snapshot effects, including stale challenges, blank pixels, source
origin, inactivity, sleep-sized tick gaps, retry exhaustion and cancellation.
`recovery_diagnostic_runtime_test.go` requires the observed probe boundary before
reload, preserving its policy reason and lineage without leaking native error
content. `internal/lease/discover_diagnostics_test.go` uses real offline HTTP for
transport/status/read/decode failures and preserves the unreachable record/error
identity. `cmd/zwai/desktop_recovery_phase_diagnostics_test.go` exercises the real
parent, attach and pipe-handshake validation phases and child cleanup.

A reported unattended stall also needs an explicitly input-free observation.
Periodic composer edits, restore navigation or send actions cannot accept that
scope. Preserve images and diagnostics before automatic recovery where available;
a white image after reload does not prove that the document was white beforehand.
`cmd/zwai/desktop_recovery_test.go` uses real child processes and private pipes
to regress readiness/commit ownership and exact-child cleanup. Cache tests keep
an executable alive while preparing its immutable bundle again.
`desktop_recovery_entry_test.go` also exercises real inherited descriptors and
the OS-path preparation errors without opening a host window. Reporter tests
hold an actual HTTP request, close a full bounded queue with concurrent late
producers, and require cancellation plus local draining before return. The
original shutdown-delay failure and corrected repeated race runs are retained.

Issue #110 separates selected-location attribution from paint health.
`frontend/src/lib/composer-recovery.test.ts` checks synchronous publication before
the next paint tick, no content payload, tuple-only notifications, real
`openThread` transitions and subscription cleanup. The emitted native script is
executed by `internal/desktop/recovery_location_script_test.go` in Node against
the Wails/WebKit/WebView message bridges: initial attribution, event delivery,
same-session listener reuse and changed-session/generation listener replacement
are asserted without running animation frames to manufacture health.
`recovery_location_test.go` rejects wrong origin metadata, frame,
session/generation, type and size, and verifies that Reload preserves the original
URL's non-recovery fields. `recovery_location_runtime_test.go` drives the actual
controller through selection before the next paint, bootstrap Home, stale
challenge/capture, background selection and unchanged outer deadlines. A restored
thread's project is derived metadata; thread ID and draft key determine its route,
so a project field cannot block restoration of that same thread.

The document-epoch regressions also cover the same session/generation across
Reload: native increments the epoch before navigation, and the emitted probe
pins the first same-origin document stamp even before root mount. A later probe
cannot relabel the old JS world with the new epoch. Exercise a new global for
the new document, reject an old queued location after Reload, and verify that
neither a changed URL nor a matching query releases the expected-scope guard.
Fresh matching-document pixels remain necessary; this internal stamp introduces
no public diagnostic phase or additional recovery deadline.

`frontend/e2e/desktop-recovery.spec.ts` switches real conversations and opens an
explicit recovery URL, checking the selected conversation and retained draft
without selecting it manually after navigation. This covers the browser boot
route, not native `SetURL`, WebContent fault injection or real WKWebView pixels.
Software results do not accept or publish the frozen formal installer. Native
acceptance must fault after a recently acknowledged selection, then verify
automatic original-scope restoration, native draft/attachment values, original
HTTP input bytes and task identities, with no manual selection or Send replay.
Keep the original failed case and its exact package identity (Issue #110).

`internal/store/composer_draft_test.go` and the HTTP tests in
`internal/server/composer_draft*_test.go` verify original bytes and decoded image
dimensions after reload, CAS conflicts/tombstones, owner deletion, upload failure
and transactional rollback. Missing, truncated or extended stored blobs reject
later metadata saves without a new revision or partial new files; zero-byte
attachments remain valid. `internal/engine/desktop_recovery_test.go` checks
durable/live Trace ordering without model calls or changing task state. Run:

```bash
go test -race -count=1 -timeout 25m ./...
cd frontend && npm run lint && npm test && npm run build
```

`frontend/e2e/composer-draft.spec.ts` drives text, quotes, real File/JPEG bytes,
page reload, per-conversation drafts, first-send migration, navigation while
uploading, rejected/lost send acknowledgment and failed queue edits. Each browser
run needs separate port/data/output paths. Keep the first failed run and its
cause; corrected selectors or teardown do not erase that evidence.

`frontend/src/components/app/composer-draft-status.test.tsx` covers the draft
guidance boundary: normal loading, saving, saved and sending states render no
status region or Delete draft control. Save/restore failures, conflicts and
uncertain sends retain their guidance and recovery controls, then hide when
resolved. `frontend/src/components/app/composer-draft.test.tsx` checks that a
disabled input does not disable draft recovery actions through ancestor ARIA,
while read-only views retain that boundary. The composer-draft E2E must check the same visible behavior while
automatic saving and reload restoration continue. Wait for acknowledged
persistence when a case needs a saved draft; an absent status region alone
cannot prove that the latest input reached SQLite.

`frontend/src/lib/transcript-recovery.ts` retains recovery diagnostics separately
from conversation blocks. Reducer and Trace UI tests require original event
identity, deduplication/rewind, no invented worker or state transition, and no raw
diagnostic JSON in the main transcript. The Trace Full log renders that evidence.
Conversation-creation tests also hold the real create request: the old composer
is disabled until the new scope opens, preserving the old scope's draft.

`tools/native_macos_resilience_worker.py` is a keyless loopback fixture for the
native campaign. Explicit model identities select its scenarios; user wording
does not. A real worker calls exec, the fixture checks the actual file side
effect, and its next HTTP stream stays open with heartbeats until a driver
handshake. Per-kind requests, the actual spawn receipt, cancellation and release
are retained separately from engine Trace. A fixture request is not proof of a
worker identity or task completion: the campaign must independently compare the
engine's turn/worker events before and after recovery. Worker-fixture tests use real HTTP.
The native future wake has an explicit purpose/case title: the background-start
regression checks the actual API request and frozen row against controlled I/O;
the validator requires the API row's `title_auto` to be boolean `false` before
freezing it, rejecting missing or coerced values. Those test rows are validator
inputs; the server's explicit-title → `TitleAuto=false` semantics are covered by
`internal/engine/schedule_title_test.go`, not inferred from a fake response.
Final native acceptance must retain the actual schedule receipt as well.
The identified development baseline retained both workers through native
Quit/reopen, as recorded in the investigation; formal-installer integration
remains pending until observed for its exact package.
The pinned compact provider uses the checkpoint fixture; rolling session memory
still calls the thread's model, so the fixture separately recognizes that fixed
tool-free System/User protocol without consuming another worker request. A real
headless engine test forced a briefing and persisted automatic compaction while
the original worker kept its second HTTP stream; release completed the same
worker with one spawn. This is adapter evidence, not native acceptance.
Multimodal manager messages read the fixed scheduled-check wrapper from text
parts only; image parts cannot select that protocol. Real HTTP regressions cover
both ordinary and scheduled multimodal input without a second spawn.

Schedule-answer admission calls the same thread model separately from the
manager and worker streams. The fixture recognizes the exact System protocol
from `internal/engine/schedule_answer.go`, with two System/User string messages,
non-streaming Generate, no tools and `tool_choice: "none"`. Its User JSON must
contain only nonempty `messages` and RFC3339 `now`. Only the fixture's three
fixed manager answers (checkpoint complete, independently owned operation
active, existing operation complete) receive `{"claims":[]}`. Unknown or mixed
candidates and malformed protocol requests return HTTP 400. Human text and
quoted protocol text do not select this branch; it neither consumes a worker
request nor changes a spawn receipt or held stream.
`tools/test_native_macos_schedule_answer.py` constructs requests from the actual
Go prompt and tests this boundary over real loopback HTTP, including a held
worker. Run it with
`PYTHONPATH=tools python3 -m unittest tools/test_native_macos_schedule_answer.py tools/test_native_macos_resilience_worker.py`.
These protocol tests do not accept the native application or erase a prior
case's classification failure (Issue #106).

The final Issue #94 combined-source browser run passed 164 default cases (15
existing opt-in skips), plus ten related opt-in cases: interrupted generations,
live/parked worker ownership, roster replay, watchdog limits, model-API error/Trace
and goal/schedule completion. Frontend unit tests passed 2,127 cases; phone
compatibility tests passed 700 cases without changing phone production code. The five
unrelated opt-in cases were not run and are not counted as passed. The extra
goal fixture initially treated the tool-free wake-policy classifier request as
a manager call, returning a tool call instead of policy JSON; its fixed system
protocol handling was regressed separately. Earlier create-scope business
failures and synthetic-paste readiness errors retain their original evidence in
`/tmp/issue94-frontend-evidence/`; the final helper waits for an enabled composer.
These browser checks are separate from native installer acceptance.
The first final unit run encountered worker-RPC timeouts while other worktrees
also ran Vitest; it had no failed business assertion. Its missing file passed
all 24 tests in isolation, and a fresh full run with four workers passed all
210 files without unhandled errors. Keep the original failure and limit this
run's concurrency rather than changing assertions or timeouts.

`composer-draft-cache.test.ts` covers idle saved-session eviction, preview URL
release, durable reload and explicit ownership across pending save, conflict,
migration and send acknowledgement. The browser regression verifies actual URL
revocation and renewed byte downloads with a decodable image. Recovery streaming
also needs the actual named EventSource listener: the live E2E posts a persisted
diagnostic while blocking history/Trace reads, so a cold reload cannot hide a
missing subscription. `internal/remote/desktop_recovery_test.go` and
`mobile/src/lib/session-desktop-recovery.test.ts` keep those metadata frames from
changing phone work state or trapping Earlier on an invisible history page.

Formal native acceptance uses `tools/native_macos_resilience.py` inside ci-macOS
and the unchanged versioned ZIP from the clean pushed source:

```bash
python3 tools/native_macos_resilience.py launch \
  --zip /absolute/zwai-X.Y.Z-darwin-arm64.zip --source FULL_40_CHARACTER_SHA \
  --evidence /absolute/new-campaign --sleep-cycles 3 \
  --interval 60 --stage-timeout 180 \
  --phases baseline renderer gpu exhaustion sleepwake
python3 tools/native_macos_resilience.py status --evidence /absolute/new-campaign
```

The driver supports baseline, renderer stall, GPU stall, exhaustion, sleep/wake
and an optional long-run profile. The fixed-duration soak agreement for Issue #94
has been withdrawn, and that task is stopped. The example selects only the five
listed stages; it does not authorize resuming that task. The legacy optional
soak profile retains its tool-defined input limits. `selection_complete` and an
exit status of zero mean only the selected stages passed; the driver's full-profile
`passed` still requires real evidence for all six.
Selection and the full plan are immutable within one evidence directory;
removing pending stages cannot produce acceptance. Exhaustion's case budget is
at least 360 seconds. Each stage runs via
`native_macos_case.py` and cannot advance without complete cleanup. The driver
checks ZIP/plist/API version and clean embedded Git revision; a claimed or unknown
stage is not passed and cannot be blindly repeated. Use the driver's detached
launch/cancel commands for a campaign longer than its control-channel lifetime;
retain campaign PID/birth identity and wait for cancellation cleanup.
The internal `control.execute(args, action, *, admission=None)` API can run an
explicit admission check while holding that campaign’s actual `driver.lock`,
before replacing owner/exit receipts. The callback must raise an exception to
refuse; its return value is ignored. Only callers supplying the callback gain
its refusal policy; default full6 launch/resume behavior is unchanged.
`tools/test_native_macos_driver_admission.py` synchronizes two real owned
processes at the victim’s execute call frame: an older driver completes before
lock acquisition, and the callback must preserve its exact owner/terminal or
broken links without publishing a case. Independent file descriptors prove
lock ownership during admission and release after refusal; default returns,
interrupts, busy locks and real owner/terminal EISDIR failures are also checked.
These are HOST metadata/ownership tests, not native behavior acceptance.
`control.launch(args, script, *, admission=None)` also offers a single raising
check after both driver log files open and immediately before `Popen`.
`main(*, launch_admission=None)` passes it only to launch; the default CLI and
existing run/resume paths gain no new gate. Rejection retains the exclusive
claim/logs, launches no driver and creates no fake owner/terminal. Tests use the
actual main/parser/control, real log I/O and bounded owned inert Python children;
late log opens must refuse before publication and all test children are reaped.
The caller keeps its original absolute visual/local deadline; return values do
not authorize a launch and earlier outer checks do not cover the final I/O gap.
These hooks add no CLI flags or automatic retry.

`sleepwake(..., *, after_wake=None)` offers a separate raising hook after the
original wake log is saved. It receives `(context, cycle, deadline)`, with a
zero-based cycle and the same Deadline used by that wake wait and subsequent
draft verification. Log/progress I/O and the callback consume that original
budget; slow I/O cannot start a new timeout. The snapshot adapter still has its
fixed 8-second leaf limit, with the cycle deadline checked around it. Default
pmset commands, three-cycle policy, whole-case budgets and the owned wake-event
cleanup stay unchanged. `test_native_macos_sleepwake.py` exercises the actual
scenario/sleepwake path through explicit OS and business-I/O seams. This hook
does not establish that an actor is connected or accept native sleep/wake.

Snapshots without an evidence label preserve the original request/size/hash
checks and primary-error order; only an explicit label enables the added MIME
and raw-body capture. Both before/after revisions must be exact integers,
not JSON booleans or floats.

Campaign input bodies are recorded by `native_macos_input_evidence.py` beneath
`http-inputs/{before,after,sent-<turn>}/{index}.body`, with separate `receipt.json`
containing the original HTTP Content-Type, parsed MIME, metadata and body
hash/size/path. Ordinary file metadata MIME need not equal ServeContent's HTTP
sniff result. Image bodies must exactly match the decodable fixture and be
decompressed to verify dimensions. `accepted_stage` reopens these original
bodies, checks before/after/sent equality, scope/revision and HTTP/path identity,
and refuses missing, altered, cross-case or redirected evidence. Draft image
IDs are not required to equal permanent turn-image IDs. The module is part of
the runtime fingerprint; old manifests lacking it refuse rather than silently
upgrading old evidence. `test_native_macos_input_evidence.py` uses actual
loopback HTTP and offline revalidation; this is not a native package acceptance.
The frozen manifest includes the package, source, executable, runner, native
helper and all scenario/input-evidence module hashes. Stages recheck these inputs and their
whole-case budgets, including cleanup; a zero scenario exit with an exceeded
budget or missing cleanup receipt is not accepted. The long soak explicitly
receives a longer budget rather than extending the runner's one-minute default.

`test_native_macos_fault_cleanup.py` also kills a real private scenario child
after it records and stops its private target. The runner must resume that exact
PID/birth/executable, stop the case, verify the target exited and retain the
scenario's failed exit status. Recovery runs before and after owned-process
shutdown, including permission-only receipts. Missing confirmation or a corrupt
receipt retains private data and the refusal latch. This host-side child test
does not signal WebKit or accept a native fault stage. Read-only private Go module
cache directories are made removable without following links into other caches.
Lifecycle adapter regressions pass the actual main harness into both
`background.prepare` and `background.stop_awake`; a fake adapter had hidden the
missing identity-checked termination function. Private app/engine teardown and
the display assertion have different ownership boundaries: discovered app/engine
processes require the private case marker, path and fresh birth identity; the
display assertion is an unreaped `Popen` child and requires that held child plus
a fresh public birth/executable match before `terminate`/`wait`. Both use only
`SIGTERM`. A real guest observation returned no readable case marker for
`caffeinate`; that does not establish whether its environment was inherited.
Attachment setup waits for the same case/PID's actual AX sheet/dialog before
issuing one file-selection action.
The eight-second `choose-file` adapter saves completed argv, exit status and
captured text streams to `native-result.json` before parsing. A timeout saves
the original binary stdout/stderr as base64, including absent-versus-empty
stream flags, in `native-timeout.json`, then rethrows the same exception.
Final cleanup snapshots cannot overwrite this choose-only receipt. Evidence
I/O preserves an existing action failure and rejects an otherwise successful
result; it never retries the native input. Run
`python3 -m unittest discover -s tools -p 'test_native_macos_choose_evidence.py'`
for actual adapter and final-retain boundary regressions, including a real
owned child producing binary streams before timeout. These headless tests do
not accept an attachment panel or explain a historical unobserved AX stall.
The final panel scan requests a fixed Role/Title/Description/Value/Children array
with options zero in one `AXUIElementCopyMultipleAttributeValues` call per
distinct scanned node. Outer root enumeration keeps its independent complete
reads. A top-level error, non-array or array length other than five refuses;
there is no individual-read fallback. Successful batches can still contain
attribute errors: only typed AXValue AXError wrappers that decode a nonzero error
are interpreted as errors. Negative NSNumber values remain ordinary values;
malformed error wrappers and a wrapped success code refuse, including in unused
prefetched auxiliary slots. CFNull is a separate
`batch_missing`, never a fabricated NoValue/Unsupported error. Missing Role
refuses; missing Children is a leaf; missing Button Title creates no Open
candidate, preserving the existing explicit Unsupported/NoValue boundaries.

The scanner consumes auxiliary title/description/value text until it obtains a
successful string containing the generated fixture basename. After that it
still requests the fixed batch but stops consuming unrelated text. Role,
children and every button title remain required to exclude a second distinct
Open; the first match alone cannot authorize Press. Roots and traversal share
5000 visits, count repeated edges and use `CFEqual` graph identity; a new node
beyond depth 45, an incomplete required read or a malformed required value fails.
Consumed auxiliary attribute errors and non-string values remain unknowns with
their original state, never a filename witness or an absence conclusion. Another
successful witness may satisfy filename presence while the complete graph still
proves one distinct Open. Missing witnesses, later second Open and required
failures forbid Press. The witness does not bind the selected row: actual
original bytes, MIME/dimensions and saved draft ACK remain separate acceptance
checks. No filename-with-newline matching contract is claimed for this fixture.
Fixed auxiliary prefetch and CFNull representation are Changed QA semantics,
not the same query sequence or an atomic snapshot.
`test_native_macos_attachment_scan.py` and `test_native_macos_panel_batch.py`
compile actual decoder/scanner functions and their actual caller with OS seams.
Run the batch suite with
`python3 -m unittest discover -s tools -p 'test_native_macos_panel_batch.py'`.
They cover top-level and per-slot batch errors,
CFNull and malformed wrappers, unknowns before/after a successful witness,
non-string-only failure, button-title/children failures, late duplicates, cycles
and the unchanged visit/depth limits. The first64 auxiliary unknown records
retain cached role/path/depth/visits and witness/Open count; one summary retains
the total, and a later required failure is always recorded. No attribute-name
discovery query, error reread, input retry or eight-second extension is added.
Client API call counts and instrumented actual-function coverage are headless
evidence, not provider-internal IPC counts or guest elapsed time. The graph stays
O(N), and no arbitrary 5000-node eight-second performance guarantee is implied.
Private 91f5 retained 7733 short completed reads approaching its eight-second
deadline; this supports cumulative traversal cost for that case, not permanent
deadlock or historical timeout causes. Private 016618 returned NoValue for both
panel and host DefaultButton, so that observation did not authorize a shortcut.
Private b2e17a diagnosed an AXOutline Value Failure; b837's unrecorded node and
historical5008 timeout remain unknown. Original failure receipts remain unchanged.
Run it with the same unittest command pattern. The complete helper still needs
SDK compilation and a fresh guest attachment path. At most 64 choose-only
stderr JSON stages retain public helper identity and monotonic boundaries,
including the region containing both sheet lookup and recursive inspection;
that region timing does not identify an individual blocked AX call.
Draft collection keeps its last existing API/native observations in memory and
writes a fixed diagnostic file on success, failure or handled cancellation.
It retains individual predicates, attachment byte hashes/metadata and text/value
lengths/hashes, without copying draft prose or the native transcript. Diagnostic
retention itself adds no reads. Save acknowledgement independently re-reads the
persisted snapshot, attachment bytes and native value because healthy-save copy
is intentionally hidden. There are no added input retries, polling file writes
or sleeps; the deadline and exact text/attachment-byte assertions stay unchanged. Evidence
write failure cannot replace an existing exception and prevents an otherwise
successful check from passing. SIGKILL or VM loss may prevent the final write.
A later successful case cannot explain an earlier timeout whose final observation
was not captured; retain both results and mark the earlier cause unknown. These
diagnostics belong to the isolated case's generated fixture, outside disposable
state; they never inspect a user's real draft.

Verify native control in its actual guest responsibility chain. In the Issue 94
recovery, pinned SSH worked but the helper launched through SSH lacked guest
Accessibility access; the same helper launched through the existing guest
Terminal session could observe and operate the private window. The isolated
Terminal bootstrap used the shared native-case lock and changed no TCC policy.
SSH success alone does not establish native UI control, and host lock state does
not determine guest availability. For already authorized acceptance, try that
evidenced existing Terminal responsibility chain before asking the user to
change settings; retain the case lock, exact guest/candidate and current helper
result. Guest administrator membership does not prove native privacy trust.

All stages use the unchanged installer. `native_macos_resilience_fault.py` admits
a renderer/GPU stall only after fresh before-Quit, after-Quit, before-reopen and
after-reopen observations: the complete WebKit cohort must disappear, then one
new signed system cohort must appear with this private shell. Additional name
variants and incomplete process observations refuse admission. Public PID/birth,
executable and signature checks bind each target; the bounded stall records its
identity durably before stopping it and resumes only that identity. These
lifecycle observations support case attribution, not proof of every possible
IPC relationship. They do not reproduce the host's exact vendor defect.

Selected system services use `codesign --verify --strict --ignore-resources
--all-architectures -R '=anchor apple and identifier "<exact service identifier>"'
<selected-executable>`, still within the original two-second timeout. The leading
`=` selects requirement source; its absence means a requirement filename.
`anchor apple` is Apple's own code, not `anchor apple generic`. Only the existing
system path/name allowlist and unchanged complete-cohort proof can select this
command. It verifies executable code and the exact Apple identity; resource
contents and the whole bundle are explicitly **not** validated. This is the
defined system-code gate, not a retry that ignores an earlier verification
failure. The selected guest's obsolete resource envelope error, also seen
without strict mode, is retained separately and does not prove system damage.
Formal App packages retain their existing `codesign --verify --deep --strict`.
See the local `codesign(1)` requirements and `--ignore-resources` sections and
[Apple's requirement language](https://developer.apple.com/library/archive/documentation/Security/Conceptual/CodeSigningGuide/RequirementLang/RequirementLang.html).

`renderer-fault-signatures.jsonl` preserves each attempted command, case, exact
PID/birth/executable, shell, return code or timeout, elapsed time and raw stdout /
stderr as base64. JSON null distinguishes an absent stream from empty bytes.
It records `resources_validated:false` and `whole_bundle_validated:false`.
No command failure, timeout or missing receipt can append an admitted target or
reach `_pause` / SIGSTOP. A write failure preserves the original command error
as the raised exception, with the write error as its cause; a successful command
without durable evidence fails closed. Fresh cohort and kernel identity checks
still follow verification, and the target receipt must be durable before STOP.
`tools/test_native_macos_signature_admission.py` crosses the actual stall caller
for these refusals and runs the real read-only macOS CLI on `/usr/bin/true`, a
private code-page-corrupted copy and own linker-signed headless code. None of
those binaries is executed and no system file is changed. Existing actual
owned-child resume and scenario-SIGKILL cleanup tests remain required. These
tests prove QA admission boundaries, not native renderer recovery (Issue #107).

Each `verify_draft` invocation writes its own `draft-restore-<sequence>-<id>.json`
at normal, error or handled-cancellation exit. It keeps only the first unmet
observation and the last observation: existing engine/lease/candidate/native
and API predicates distinguish false, error and not-read. A first-false poll
does not itself fail a case. Types, lengths, hashes and equality flags retain
the observed boundary without copying draft text, quotes or filenames; native
text-mode stream summaries are explicitly decoded text, not original bytes.
No extra API/AX call, navigation, retry or deadline is added. Malformed summary
fields cannot pre-empt the original business checks. Receipt or WARN failures
preserve an existing primary exception/cause; success without its mandatory
receipt refuses. Hard kill or guest loss may prevent the final in-memory
observation from being written. `tools/test_native_macos_restore_observation.py`
crosses the real module wrappers, loopback API and native adapter seams,
including malformed API fields, unchanged call order and cancellation/write
failures. These are QA evidence tests, not native recovery acceptance.

The exhaustion stage removes execute permission only from this ready session's
unused private recovery copy, then stalls its attributed renderer. It requires
two actual permission-denied launch failures, the native exhausted dialog and
no third attempt during a further fifteen-second observation. It restores the
copy's mode and never modifies/re-signs the installer or a live executable.
This accepts bounded failure handling; it is not three successful native
replacement generations followed by a fourth stall. Cross-generation retry
accounting has separate controller/real-child tests.

`native_macos_resilience_phases.py` verifies the actual new ready event, parent
commit receipt and old-shell exit within the replacement deadline. Offline
acceptance rechecks the raw journal's session, engine, generation and attempt
order against those receipts. The unexecuted experimental private-API/dynamic
probe has been removed. A safety/signature/control failure leaves the stage
pending; do not bypass that check, re-sign the release or weaken runtime protections. Verify the
actual restored native pixels/actions, text/quotes and original file/image bytes,
the same engine PID and task/worker identities, no duplicate send, finite retry
behavior and the visible exhausted error. Real sleep/wake requires actual power
transition evidence, not only a posted application event or a successful `pmset`
command. A successful process launch, helper compile or headless suite cannot
substitute for these observations. Current formal native result: **pending**.
The baseline uses a real native Quit, verifies no shell revival, then reopens the
same package and selects the original conversation through native controls.
Running and parked workers retain their original engine/turn/worker identities
and must produce their actual completion/collection receipts. A binary file uses
the native file picker; a PNG uses one native **Edit → Paste** menu action.
The guest helper checks the clipboard write result, original PNG readback bytes
and unchanged ownership counter, focuses the exact composer, then requires
unique enabled Edit/Paste menu items with the expected roles, same PID and
foreground app before each public AXPress. It rechecks the clipboard before
Paste and retains `paste-transport-observation.json` on success or refusal.
No keyboard fallback, fixed paste delay or second Paste is attempted. A helper
success acknowledges the menu request, not DOM delivery or durable storage:
the existing full API snapshot, original bytes, native values and saved-label
conditions still decide whether draft collection passed. The earlier keyboard
transport timeouts remain failures of unknown cause; a later menu success does
not establish a product keyboard or WebKit fix. Reloaded original
bytes and the decoded 3-by-2 image dimensions are checked independently of the
visible attachment chips.

`python3 -m unittest tools/test_native_macos_paste.py -v` compiles a temporary
Objective-C translation unit that renames `main` and includes the real helper.
Its headless entry calls only the actual pure clipboard/menu predicates using
in-memory Foundation values (27 positive/negative checks); it never calls the
helper entry, NSApplication, NSWorkspace, AX or a pasteboard. Linking AppKit is
not native acceptance. The thin AX/clipboard wrappers require fresh isolated
ci-macOS evidence bound to the changed helper hash; no host GUI/clipboard test
or earlier private helper result substitutes for that run.
The guest may lock independently of the host. During this incident, guest AX
reported no secure input field, while the user's subsequently authorized direct
UTM control could wake the existing guest window and complete its normal login.
Record the current guest state and actual input attempt; a failed individual
channel does not prove all guest input unavailable. This login recovery is not
application acceptance. Baseline and soak hold a finite, case-owned display/idle
assertion; sleep/wake releases it before actual sleep and restores it afterward.
Cleanup must also reap that exact assertion process without changing the guest's
lock-screen security policy.

A ready observation is a point in time. If an already authorized guest relocks
while control returns to the model, keep the guest input actor's exit/cleanup,
a fresh session check and guarded launch in one bounded invocation. Distinguish
that actor from the finite power owner: waiting for the short wake assertion to
expire before the scenario owns its assertions leaves a handoff gap. Retained
WindowServer timer UUIDs were armed before the new case's power child existed
and later fired during that case despite its valid assertions. A nearby event
or an old unbound child PID alone does not establish that event's owner.

A recovery bridge must be registered in the actual runner's original process
list under its held case lock and incomplete-cleanup latch before recovery.
Keep it alive until a fresh same-birth, direct scenario child acknowledges all
three power assertions; a file's existence, partial JSON or one assertion is
insufficient. Terminate and reap the bridge on takeover; original cleanup owns
all failure, timeout and cancellation paths. A surviving bridge retains the
refusal latch. Regress this through actual `run_case`, `ProcessScope`, its
three-argument preparation-failure fallback, `stop_owned` and lock publication,
including malformed/wrong-owner ACKs and interruption. These are recovery
controls, not a new standalone runner mode or an application acceptance result.
They must share the existing whole-round/work/cleanup deadlines and may not add
a pulse loop or change lock-screen settings.

A direct `Popen` child can retain its PID/birth while its executable changes
from the spawning interpreter to the requested image. Wait for that owned
transition within the existing deadline before freezing full identity; unknown,
wrong birth, unexpected image, early exit and expiry remain refusals. Exercise
the actual kernel identity reader through a headless exec boundary. Keep helper
executable modes through the whole launch chain, including the actual
LaunchServices/Terminal `.command` entry, and check them before triggering it.
A source file passed to an explicit interpreter need not be executable. Regress
the same entry bytes at 0644 refusing before any trigger/claim and at the
intended executable mode passing preflight. Helper-only checks and matching
hashes cannot rule out a transfer-created PermissionError or a never-started
entry; inspect that boundary before waiting for a native-case timeout.

Preserve the new observation frame without another approval delay for the same
identified target and action. A wake image or a power handoff does not establish
lasting unlock. Credential input still requires the actual guest login state;
a refused preflight that never launched is distinct from a failed native case.

An owned `caffeinate` process or even current `UserIsActive`, display-idle and
system-idle assertions do not establish continued guest unlock. A real case
recorded those three assertions with 49 seconds remaining while WindowServer's
dim timer led to a loginwindow DisplayDim lock. Retain the case's PID/birth/poll,
exact owned assertion block and bounded session-event timeline; do not infer a
five-second default when `-t` is present, or attribute the lock to a nearby
capture without causal evidence. Missing lock flags remain unknown. Programmatic
events remain synthetic input; neither posting them nor an activity counter
replaces the original window/action and cleanup checks.

`native_macos_resilience_soak.py` persists an initial identity, fsynced per-tick
results and a final summary. Each tick checks the original engine and running/
parked workers, edits the native composer, waits for the complete saved snapshot,
then restores and independently reads the original text/quotes/file hashes.
Editing, saving and restoration share one action deadline; immediate UI input
does not imply the debounced durable save has finished. Both wall and monotonic
clocks must remain ordered and within `interval + stage-timeout` between ticks;
a tick itself must finish within the action timeout. The interval cannot exceed
that timeout. A separately selected long-run observation retains its actual
duration, checks every gap and hashes the tick ledger. It is not a fixed-duration
obligation for Issue #94. A long wall-clock gap, missing ticks or an elapsed
summary alone cannot establish successful observations.

## Local startup readiness (F-110 / C-001 / C-027)

`cmd/zwai/engine_frontend_test.go` injects preparation failure and actual
startup cancellation before a real serving holder can receive any handoff or
new launch. Its delayed successful preparation checks that the subsequent
replacement still receives its 30-second budget. These are preparation-order
regressions, not proof that every later build/startup must succeed.

`cmd/zwai/engine_boot_test.go` launches an actual offline child that acquires
the data-directory lease and publishes its HTTP metadata after 16 seconds.
It reproduces startup beyond the former 15-second cutoff and verifies the
ready URL belongs to that child. Additional cases check current-attempt log
isolation, the actual exit status, and cancellation that reaps the launched
child. `cmd/zwai/engine_restart_budget_test.go` retains the independent caller
deadline and competing-holder checks before irreversible handoff. After ACK
and stopping the old engine, `engine_startup_forward_test.go` verifies that the
launcher preserves its sole replacement beyond the handoff deadline, attaches
after an actual 31-second delayed lease publication, and retains a live child
with its PID/log ownership-unconfirmed pending result after the cold wait.
`TestForwardReplacementExpiryPreservesChildWhoseOwnershipIsNotYetKnown`
delays the actual child before Acquire until after that wait, then requires
that same PID to acquire and publish a discoverable URL. The competing-holder
case delays that same acquisition, keeps the competitor intact, and requires
the losing child to exit naturally and its Wait goroutine to finish. Neither
busy nor free lock state establishes this child's ownership or authorizes a
forward-only deadline kill. Explicit cancellation still reaps only its child
and retains the cancellation cause.

`TestSpawnEngineRetriesItsOwnFirstReadinessProbeTimeout` delays the real
child's first metadata response by 750ms, beyond the individual 500ms probe,
then requires a later successful probe and a surviving validated child.
`TestSpawnEngineStopsItsOwnUnreachableChildAtTheStartupDeadline` keeps every
probe slow and checks the caller's deadline and child cleanup. The lease
regression retains the unverified recorded identity on connection, status,
body-read and JSON failures; it does not treat that record as readiness.
Competing-holder regressions keep unreachable/mock-mismatch errors immediate
and preserve the independent process.
`engine_startup_cancel_test.go` cancels during real synchronous metadata probes,
including healthy, unavailable and mode-mismatched responses. The launcher's
actual SIGINT path must return a recognizable cancellation, never a late ready
URL or a discovery error, and must preserve the independent lease holder.

```bash
go test -race -count=1 ./cmd/zwai -run 'TestSpawnEngine|TestSpawnStartup|TestInProcessStartup'
go test -race -count=1 ./cmd/zwai -run 'TestForwardReplacement|TestAcknowledgedReplacement'
go test -race -count=1 ./internal/lease -run 'TestDiscover'
```

### Reachable recovery and cancellation

`frontend/e2e/engine-startup.spec.ts` reloads the actual page during restoration,
reopens the original conversation under the existing home-on-reload contract,
and checks its identity, saved draft, disabled Send, continuing progress and
automatic banner removal at readiness. `frontend/e2e/goal-restoring.spec.ts`
starts with a focused goal edit: Enter/blur during restoration cannot submit;
an actual intercepted `409 restoring` keeps its text and original conversation,
with no automatic retry before an explicitly acknowledged save. Component and
store tests cover a pending/rejected ACK, empty clear, duplicate blur/Enter and
navigation while an old acknowledgement is pending. Run those browser cases
with an independent port, data directory and output directory. These headless
UI cases do not replace real-process recovery or ci-macOS native acceptance.

`internal/app/startup_restore_test.go` verifies that a deferred-restoration app
serves metadata before recovery and becomes ready afterwards. Engine, HTTP and
phone startup tests check progress, mutation refusal and durable Stop before
runtime occupation. `cmd/zwai/engine_restore_process_test.go` exercises two real
engine processes and the offline loopback model adapter: the replacement serves
`restoring` immediately while its owned restoration exceeds 30 seconds; reads,
presence and `409 restoring` remain observable, then the original task/worker
continues without another spawn or side effect. This is headless process
acceptance; native window behavior uses the ci-macOS case runner separately.

On 2026-10-24, the isolated ci-macOS runner passed the actual
`go run ./cmd/zwai desktop --mock` command from an exact source copy of
`75ccd839a303b66f796e3c25d9f377b07fbef030` plus all 33 original dirty entries.
Native text entry and one Send produced an offline two-worker turn with matching
tool-output bytes and Trace; all 65 served assets matched the freshly built
bundle, and a PID-bound screenshot showed the final result. Source/index/status
remained unchanged. The cold-source round took **159.069 seconds**, including
**11.398 seconds** of complete process/data cleanup; first source transfer and
admission brought the measured total to **161.316 seconds** within its separate
900-second budget. This accepts the normal checkout launch path. Recovery beyond
30 seconds remains headless evidence above; this is neither a formal ZIP release
nor renderer-fault acceptance. Earlier CG multi-window refusals remain retained
without an established product cause.

```bash
go test -race -count=1 ./internal/app ./internal/server ./internal/remote ./internal/engine -run 'TestDeferredRestore|TestRestoringEngine|TestStopBeforeRestore|TestRecoveryServes|TestPhoneRecovery'
go test -race -count=1 ./cmd/zwai -run 'TestReplacementListensWhileRecoveryExceedsThirtySeconds|TestAcknowledgedReplacement|TestForwardReplacement'
```

### Mixed worker roster and human-pause replacement (Issue #102)

`cmd/zwai/engine_large_roster_process_test.go` and its fixture exercise actual
old and replacement processes, SQLite, the Registry and a keyless loopback model.
349 historical finished workers are explicitly seeded as historical events;
two live/parked workers and the human question are created through real tools.
The old engine's actual `PrepareRestart` must checkpoint all 351 identities.
The tests retain task/call identities, historical results and the future wake,
verify an uncertain write by a real read before completion, and reject duplicate
answers. The second case explicitly stops its identified restoring child after
model work is observed, then cold-recovers the same data without reseeding.
This is explicit cancellation, not a post-ACK deadline kill or a product crash.

The test-only helper records real indexed-checkpoint, filtered and full-body
query counts and stage timings. Counters remain active through the resumed
bootstrap; the parent requires fresh live/parked worker requests beyond that
attempt's baseline and waits for the actual ask waiter via the read-only thread
list, then receives the counter-freeze acknowledgement before requesting Trace.
`engine_large_roster_probe_checks_test.go` challenges the observer with real
full-thread/turn queries and failed generation writes. Restored ask lookup and
Stop separately regress zero repeated body reads in
`internal/engine/restore_ask_reads_test.go`. Early accepted answers and successive
questions retain the existing restart-answer replay regressions.

Handoff/listener readiness keeps its shared 30-second target; restoration and
total work are measured separately. This mixed-roster cardinality case does not
reproduce the large payload benchmark below and does not accept a native window
or installer. Keep each failed attempt, model inputs, original/new process logs,
Trace, probe and cleanup receipts separately from a later successful run.
`ZWAI_LARGE_ROSTER_EVIDENCE` optionally retains those private files; omit it for
ordinary disposable test data. No real model or host GUI is used.

```bash
go test -race -count=1 -timeout 8m ./cmd/zwai -run 'TestReplacementRestoresLargeRosterWithLiveParkedAndHumanPause|TestCancelledLargeRestoreReapsOwnedChildAndColdRecoversSameWork|TestLargeRestoreProbeCounts'
go test -race -count=1 ./internal/engine -run 'TestRestoreBindsTheOriginalQuestion|TestRestartReplaysAnAnswerAccepted|TestRestartReplaysAnAcceptedAnswer|TestRestartDoesNotApplyAnEarlierAcceptedAnswer|TestLateAskAnswer'
```

### Large-history restoration measurement

`BenchmarkRestoreSnapshotLargeHistory` uses 100,001 historical tool-result rows
(each 1,080 bytes) and two checkpoint bodies of 2,240,246 bytes each. On the
2026-10-24 Apple M3 Max run, three repetitions measured **125.695 ms/op**,
**9,018,429 B/op**, **325 allocs/op** (fixture creation excluded). This measures
snapshot construction.

`BenchmarkRestoreToGenerationLargeHistory` in
`internal/engine/restore_bench_test.go` reuses that fixture and times actual
`Engine.Restore` through both its return and receipt of the restored worker's
unchanged 1,120,000-byte history by the offline loopback HTTP model adapter.
On the 2026-10-24 integrated-source run with `GOMAXPROCS=4`, one repetition
measured **203.468 ms/op**, **51,397,968 B/op**, **9,706 allocs/op**; the
corresponding race run measured **3.075371 s/op**, **55,968,792 B/op**,
**10,337 allocs/op**. Both met the unchanged 10-second model-input budget.
The earlier 485.068 ms observation used the separate startup branch, before
integration exposed two full-turn budget reads; it does not describe this source.
The assertions retain thread `large`, turn `original-turn`, worker
`original-worker` and role `inspector`, observe its durable `generation_started`
and actual HTTP input, and reject another worker identity or tool-call replay.
Both recovery query counters must be one (latest checkpoint and filtered event
scan), with zero unfiltered full-event reads through the model-input boundary.
Count unfiltered `turn_id` reads as well as `seq` scans: ignoring the former
hid two reads of roughly 108 MB of tool history each.
`restore_budget_snapshot_test.go` and `internal/store/recovery_test.go` preserve
both correction allowances, checkpoint ordering and turn/manager identity; an
actual HTTP model request follows the new durable human Continue receipt. The
startup snapshot cannot substitute for that later receipt.

The actual-recovery fixture disables auto-title and memory, never starts the
scheduler, and blocks model responses. Setup and cleanup are outside the timer:
registry `Cleanup` followed by `Wait` confirms worker exit before engine shutdown
joins the manager. These are local observations, not native startup, whole-task
latency or a guarantee for arbitrary user histories.

```bash
go test ./internal/engine -run '^$' -bench BenchmarkRestoreSnapshotLargeHistory -benchtime=3x -benchmem
go test ./internal/engine -run '^$' -bench '^BenchmarkRestoreToGenerationLargeHistory$' -benchtime=3x -benchmem
go test -race ./internal/engine -run '^$' -bench '^BenchmarkRestoreToGenerationLargeHistory$' -benchtime=1x -benchmem
```

## Phone hub startup independence (F-210 / C-006)

`internal/remote/host_start_test.go` holds a loopback registration request
open while exercising local lifecycle operations. `internal/app/startup_remote_test.go`
starts the real app against that blocked hub and checks a successful local
`/api/meta` response before registration completes. Remote lifecycle tests
must also preserve cancellation and reject late results after Stop/Reload.
These offline checks do not replace opening the actual native window.

```bash
go test -race -count=1 ./internal/remote -run 'TestHost'
go test -race -count=1 ./internal/app -run 'TestNewDoesNotWaitForRemoteRegistration'
```

## Scheduled-check failure and worker ownership (F-130 / F-150 / C-009 / C-027)

`internal/engine/schedule_worker_ownership_test.go` runs the real Engine,
Registry, ADK graph and OpenAI HTTP adapter against a keyless loopback SSE
fixture. A first successful check spawns and parks a worker whose real model
request remains open. A later check exhausts its single no-activity
continuation; the turn/run must remain error/unread while the worker remains
running. The next wake must reuse its registry, ID and original HTTP request,
and the retained worker must finish naturally without a cancellation error.
The goal-only case consumes a one-shot and checks that its valid standing goal
is neither blocked nor immediately retried. HTTP 400 and exhausted 429 tests
prove isolation does not depend on an error-text match or bypass the existing
bounded retry/shared-backoff path. Other cases require actual HTTP cancellation
after Stop, final-wake cancellation/pause, consumed one-shot without an owner,
or an inactive goal. Receipt assertions bind exact owner/worker IDs to a
persisted event before the manager's terminal error.

Lifecycle fixtures must return valid `{"claims":[]}` for the exact tool-free
schedule-answer classifier protocol and disable unrelated memory review.
Terminal-failure cases use the real scheduled no-activity limit or an explicit
manager HTTP refusal. A plain-text fallback is invalid classification output;
it exercises pending service recovery rather than terminal ownership. The first
2026-10-25 full race run exposed this fixture defect: three ownership tests
stalled in their initial successful-check setup, before their intended failed
check. Retain that first failure when validating the repaired fixture.
Direct manager fixtures must execute the exact Registry and turn registered in
the runtime. `TestIndependentManagersDiscoverAndNegotiate` queues peer notices
into its actual receiver Registry before that manager starts; a dummy owner
would retain an undrainable inbox and repeatedly supersede accepted answers.
The later full-race timeout exposed this separate fixture defect, with valid
classifier JSON throughout. `TestManagerFixtureRejectsAnExistingDifferentOwnerBeforeModel`
requires the test helper to return an identifiable ownership error before any
model call and preserve the existing owner for both Registry and turn mismatch.
`TestScheduledClassifierOutageKeepsTheOriginalWorkerUntilRecovery` separately
returns a real classifier HTTP 503, verifies the original turn, registry, worker
ID and worker HTTP request remain active without terminal/cleanup events, then
restores the service. Only classification repeats; the manager and worker are
not restarted, and a later human Stop still cancels the worker request.

`internal/engine/worker_ownership_test.go` covers executable-goal and wake
selection (including plan mode with an active but non-executable wake),
receipt write failure, resource/watchdog exclusions, owner/Stop
revocation around finalization, and restart from failed scheduled turns. A
valid recovery needs a checkpoint, current ownership and a valid explicit
receipt; absent/malformed/other-agent receipts and revoked owners do not revive
workers. Recovery executes the original worker without rerunning or rewriting
the failed manager turn.
The actual `PrepareRestart` → new engine → `ResumeParkedWorkers` boundary also
runs through the loopback HTTP fixture, preserving the error/unread run and
future wake. A receipt arriving after the manager's terminal event cannot
authorize recovery; a later Stop still revokes an earlier valid receipt.

`internal/engine/worker_ownership_order_test.go` blocks the real cleanup
event's storage callback while another goroutine tries to take/occupy the
next registry and start its worker. Stop, final-wake cancellation and
finalization must all keep that new claim behind the old global cleanup's
durable publication. After release the old worker stops and the new worker
remains running. This prevents live/replayed cleanup from stopping an unrelated
replacement worker even though cancellation only targeted the old registry.

`frontend/src/lib/transcript-worker-ownership.test.ts` covers live/full/roster
replay, current output and pending tools, actual completion, human interruption,
cleanup, unrelated turns, malformed receipts and rewind. Ordinary parent
errors retain their prior behavior. `frontend/e2e/schedule-worker-ownership.spec.ts`
uses the actual engine and model HTTP adapter: the failed check remains visible
and unread, Agents stays running before and after reload, and the next check
collects that same worker's real completion with no replacement model request.
The JSON receipt belongs in Trace, never the visible conversation. This browser
test does not substitute for a native application-window walkthrough.
`internal/tui/client_worker_ownership_test.go` keeps the same receipt out of
terminal notices while preserving ordinary notice compatibility.

```bash
go test -race ./internal/engine -run 'WorkerOwner|ScheduledFailure|FailedCheckOwnership|FinalizationRechecks|RetainedWorkerRecovery|FailedScheduledCheck|FailedOneShotCheck|ScheduledManagerModelError|ParkedWorkerCleanup'
cd frontend && npm test -- src/lib/transcript-worker-ownership.test.ts
cd frontend && ZWAI_E2E_SCHEDULE_WORKER_OWNERSHIP=1 npm run e2e -- e2e/schedule-worker-ownership.spec.ts
```

## Worker output identity and history overlap (F-130 / C-003)

`signal_identity_test.go` and `internal/engine/accumulator_identity_test.go`
exercise a controlled Registry/ADK worker stream across manager returns and
notification-sink changes. Reasoning, answers and checkpoints retain one
`message_id`; a new model round, resumed stream and another worker do not share
it. Tool calls and results retain their original pairing. Store reopen and
legacy-schema migration, remote projection and event-to-notification adapters
must preserve the optional field without fabricating it for old events.

`frontend/src/lib/transcript-message.test.ts` covers live/replay checkpoint
replacement, distinct outputs with identical text, stale snapshots and
per-output burst coalescing. `frontend/src/store/app-stream-history.test.ts`
controls both historical/live arrival orders, exact stored identities below
the current cursor, side effects, paging, rewind and conversation changes.
History requests freeze a local generation so a response begun before rewind
cannot restore deleted events beyond the client's cursor. A delayed history
snapshot preserves newer explicitly identified live output, including before
its first durable checkpoint, without reviving stopped workers or an older
model output.
`frontend/e2e/agent-history-overlap.spec.ts` verifies the visible Agents flow,
tool details and reload through controlled offline event responses.

```bash
go test -race -timeout 25m ./...
cd frontend && npm test -- src/lib/transcript-message.test.ts src/store/app-stream-history.test.ts
cd frontend && npm run e2e -- e2e/agent-history-overlap.spec.ts
```

The [cancellation evidence](diagnostics/issue88-cancellation-evidence.md)
records what was verified from existing Trace and read-only storage. A model
call's `context canceled` string alone cannot distinguish handoff, parent-turn
cleanup, human interruption or an upstream failure.

## UI message catalogs

Worker failure translations live in `frontend/src/lib/messages-agent-failure-en.ts`
and `messages-agent-failure-zh.ts`; the main `messages-en.ts` and `messages-zh.ts`
catalogs compose them without changing message keys or values. `i18n.test.ts`
checks catalog key parity, while `agent-failure.test.tsx` checks translated
failure details and their recovery-settings links. Run them with
`cd frontend && npm test -- src/lib/i18n.test.ts src/components/app/agent-failure.test.tsx`.

## Automatic restart handoff (F-110 / C-027)

Issue #101 regressions must use the real manager Registry middleware: directly
calling `AskUserTool` does not run the handoff unknown-result conversion.
`internal/server/restart_ask_http_test.go` drives a loopback provider, HTTP
restart, a new engine on the same persisted data, named SSE recovery, the
original human answer and duplicate refusal. The replacement cannot call its
manager before that answer. `cmd/zwai/engine_restart_ask_acceptance_test.go`
also replaces actual executable processes. It records and checks the 30-second
handoff/listen target as soon as the replacement lease appears, then separately
waits at most 10 seconds for metadata `startup=ready` and the same running turn's
human pause before answering. Original turn/call identity, manager call counts
and the single confirmed result remain required. Legacy tests retain
both result/checkpoint orderings and accepted-answer precedence; ordinary
external-tool unknown outcomes remain unknown. PC/phone reducers and their
actual history/live adapters must retain confirmed answers and only re-open
an uncertain ask on the explicit recovery event. Checkpoint content stays on
the host; `ask_recovered` carries only the original public questions.

```bash
go test -race -count=1 -timeout 8m ./cmd/zwai -run '^TestRestartRealProcessPreservesTheOriginalUnansweredAsk$'
```

`handoff_test.go` and `internal/engine/restart_test.go` cover shared deadlines,
worker/tool admission, structured histories and unread inboxes, real completion
versus handoff, watchdog budgets, active/parked workers, human pauses and
checkpoint failures. `internal/engine/restart_parked_test.go` proves actual
worker execution after restart while the manager remains completed and its
future wake stays unchanged. Lifecycle helpers `mustCreateWake` and `armDueWake`,
and the parked-restart fault fixture, create explicitly titled waits. Fixtures
that replace the provider pool, register/remove database callbacks or close
storage must not also start unrelated asynchronous title work. Helper assertions
keep `TitleAuto=false`; automatic schedule-title tests use `CreateSchedule`
directly. This isolates fault injection without changing runtime title generation
or weakening race checks.
`internal/remote/restart_test.go` prevents private
checkpoint bodies leaving the PC. `internal/server/restart_http_test.go` covers idempotent
HTTP acknowledgement, readable status, same-origin refusal and admission errors.
`TestRestartStorageFailureKeepsDiagnosticsAndCanRetry` registers its private
checkpoint-write failure callback before starting any runtime or HTTP goroutine.
The actual offline worker reaches checkpointing; failed storage returns a
non-success HTTP response, leaves `failed` readable with the original turn's
Trace and rejects new work with `409 restarting`. Clearing only the atomic
failure switch allows the same handoff to return `ready` with one original-turn
checkpoint and no additional turn. This verifies retained diagnostics/retry,
not continued worker execution after a failed handoff. The launcher test
`TestReplacementStopsOnlyAfterTheCheckpointAcknowledgement` separately proves
that a failed acknowledgement does not authorize killing the old lease holder.
`TestNewRejectsFatalRecoveryAndClosesItsDatabase` retains a valid SQLite schema
with an invalid integer in a thread row: migration succeeds, actual recovery
fails decoding `ListThreads`, and `App.New` returns the fatal error and no app.
Changing the database out of WAL mode then proves its failed app closed the SQL
connection. Isolated source-overlay negative controls omit that cleanup and
hide the failed restart stage; both assertions fail before testing the unchanged
production behavior.

```bash
go test -race ./internal/app ./internal/server -run 'TestNewRejectsFatalRecoveryAndClosesItsDatabase|TestRestartStorageFailureKeepsDiagnosticsAndCanRetry' -count=1
```

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
and model input. `TestRestartCheckpointRetainsAnAcceptedAnswerBeforeTheWaiterReadsIt`
also holds the real loopback model response after receiving the recovered
call/result pair. While that turn remains running, it checks the accepted
answer's checkpoint, matching durable tool result and exact adapter input;
waiting for the whole turn to finish would test a different boundary. The
barrier in `internal/engine/restart_answer_barrier_test.go` releases on cleanup
or request cancellation. Run this target with
`go test -race -count=1 ./internal/engine -run '^TestRestartCheckpointRetainsAnAcceptedAnswerBeforeTheWaiterReadsIt$'`.
`internal/engine/restart_inbox_replay_test.go` checks unread
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
The ordinary process tests copy the same build into old/new executable paths;
they do not establish historical-version compatibility.
`cmd/zwai/engine_upgrade_acceptance_test.go` is a separate opt-in old→new gate.
It skips unless both `ZWAI_UPGRADE_OLD_BINARY` and `ZWAI_UPGRADE_NEW_BINARY` are
explicit absolute executable paths, rejects equal bytes, and records both hashes.
All engines use fresh private data/workspaces and a keyless loopback model.
`-race` instruments the test harness; supplied executable bytes retain their
original build modes and are not silently rebuilt with instrumentation.
It verifies the old database lacks `pending_schedule_answers`, the replacement
creates it, the same turn/worker/generation is restored and collected, and one
real persisted shell effect is inspected without repeating its write. Separate
private cases preserve the original unanswered ask and iteration-cap permission
wait; only the actual answer/Continue permits the next manager request. Manager
continuation is counted separately from tool execution. These cases cover an
active worker; they do not claim historical-version finished-worker or native
window acceptance. Private replacement leases are stopped before the next case.
The historical gate requests the old engine's real restart API, requires its
durable `ready` acknowledgement, joins that owned child, then starts and joins
the candidate as a directly owned engine child. It does not claim coverage of
the complete historical automatic launcher; the ordinary same-build process
gate exercises that separate shell path. A historical development binary may
retain its original frontend source path even with private data. Record that
checkout's tracked state and built-asset hashes before/after the opt-in run.

The 2026-10-25 private cross-version run passed all three cases under `-race`
(30.602s): old SHA-256
`357f79ed3f27219faffbfddcdcc9dc1f01ee1491a8ed319d11350b1c1b78c224`, candidate
`28ea3bc0da239611f6f9ac6e483c9c4bcf7c2830071597047a67944cf85cdeb4`.
The active worker retained ID `verifier-1` and generation 1, its persisted write
and verification each occurred once, and the original turn resumed within
18.835s. All six owned engine children exited normally. The original checkout
remained at `224864e` with no tracked changes; all 66 existing built assets
retained their hashes. The old child rebuilt the same asset bytes once and
added the missing ignored `frontend/dist/.source-stamp`; this is recorded as an
observed preparation effect, not an unchanged-directory claim.

```bash
ZWAI_UPGRADE_OLD_BINARY=/absolute/historical-zwai \
ZWAI_UPGRADE_NEW_BINARY=/absolute/candidate-zwai \
go test -race -count=1 -timeout 3m -v ./cmd/zwai \
  -run '^TestUpgradeOlderEnginePreservesPrivateWorkAndHumanPause$'
```

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

Concurrent runs follow the [E2E isolation rules](#end-to-end-tests).

## Memory review provenance and commit (F-420 / C-025 / C-012)

`internal/memory/reclassify_test.go` checks source-preserving moves, negation /
numeric / condition changes, complete-text demotion, destination conflicts,
capacity, failure after saving the target, partial deletion receipts and
idempotent/concurrent retry. `internal/engine/review_reclassify_test.go` runs
the actual offline reviewer/tool graph and checks post-turn tool isolation;
`internal/server/memory_reclassify_test.go` checks HTTP report and original-turn
Trace. `cmd/zwai/cli_trace_memory_test.go` runs the real CLI over stored
review outcomes, preserving partial writes and candidates alongside errors.
The offline script uses explicit step markers, not a semantic classifier.
`frontend/e2e/memory-boundary.spec.ts` checks both languages, usage / thresholds
and the requested tidy UI. Each test pins and restores server-side locale as
well as its private browser storage; localStorage alone cannot override config.
`frontend/src/lib/review-diagnostics.test.ts` distinguishes committed note and
completed move receipts from a skill-only partial promotion while retaining
post-turn note summaries. `frontend/e2e/memory-tidy-partial.spec.ts` makes only
its owned skill parent read-only under an ordinary POSIX user, then runs the
real offline model/tool/SSE path: the note survives source cleanup failure,
Memory lists the saved write without a completed demotion, and the original
turn's English/Chinese Trace does not claim that note was unsaved. The fixture
restores permissions before deleting its project; Windows/root runs skip it.

`internal/memory/skill_usage_test.go` checks concurrent successful views under
`-race`, worker tool usage, immutable content identity, corrupt sidecars, merge
counts, deletion/copy and exact stale boundaries.
`internal/memory/skill_copy_collision_test.go` preserves the distinction between
a filesystem file collision and an occupied skill directory.
`internal/server/skill_usage_copy_test.go` exercises real copy HTTP routes;
`internal/engine/review_stale_test.go` checks advisory candidates in model input,
streamed scan, report and original-turn Trace.

Boundary regressions in `internal/memory/prompt_test.go` cover manager, worker,
reviewer, tidy and library instructions, including generic wording and negative
assertions. Store tests distinguish an existing-name pointer from copied steps
and ensure an unknown name cannot bypass overlap checks. Frontend i18n tests
check the extracted memory catalogs and both panel descriptions. Run
`go test -race ./internal/memory` and `cd frontend && npm run lint && npm test`.

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
go test -race -count=1 -timeout 25m ./internal/memory ./internal/engine
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
go test -race -count=1 -cover -timeout 25m ./internal/desktop ./internal/engine
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

## Goal resume notices (F-150 / C-003 / C-012)

`frontend/src/lib/goal-notice.test.ts` checks the display projection for an
idle human resume followed by its new continuation turn, including hidden
metadata, typed memory-review feedback between them and a resume still waiting
for that turn. It preserves
later automatic continuations, independent resumes, manager reopen actions and
the original blocks; equal wording is not event identity. Input, substantive
manager activity, ordinary notices and ordinary errors prevent unrelated notices
from being paired. Typed review feedback, including a review failure, keeps its
original visibility and ordering without splitting the resume pair.
`frontend/src/components/app/transcript-goal-notice.test.tsx` renders the real
transcript and retains both original events for Trace.
`frontend/src/store/goal-notice-history.test.ts` covers both history/live arrival
orders, repeated replay, page and agent-log overlap, rewind and reconstruction,
and actual worker/quiet-metadata or typed review events between the two manager
notices.
Do not assume the persisted resume and continuation events are contiguous:
`internal/engine/goal.go` records the human resume before starting the turn,
`internal/engine/turn.go` records continuation in the asynchronous runtime, and
the previous turn's review goroutine in `internal/engine/review.go` can publish
its result between them. Regress changed/error and `notify=on` payloads through
the real reducer, component display, history/SSE overlap and pagination; assert
one restore notice while the feedback, raw blocks and both Trace receipts remain.
An ordinary notice with similar text is still a pairing boundary. The fix does
not reorder stored events or add wire correlation fields.
The primary case in `frontend/e2e/goal-resume-notice.spec.ts` exercises the explicit goal-resume API with
the offline provider after completion hides the goal strip, checks one new turn
and one visible notice per restore,
and reads both original turns' Trace events. Reload and a history-page split
between a stored resume/continuation pair retain the same notice count and
unchanged stored events; the visible Full log retains its continuation notice.
Additional changed/failed-review cases use controlled `/log` responses to check
browser replay, reload and pagination with the feedback still visible. These
are history-response fixtures, not claims of server execution or newly persisted
Trace; the primary case separately verifies the real restore and stored events.

```bash
cd frontend && npm test -- src/lib/goal-notice.test.ts src/components/app/transcript-goal-notice.test.tsx src/store/goal-notice-history.test.ts
cd frontend && ZWAI_E2E_PORT=18996 ZWAI_E2E_DATA_DIR=.e2e-data-goal-resume npm run e2e -- e2e/goal-resume-notice.spec.ts --output /absolute/unique-goal-resume-output
```

These browser checks do not replace acceptance in the actual desktop window.
`tools/native_goal_resume_notice.py` runs through `tools/native_macos_case.py`
with an identified candidate and prepared AX helpers in `ci-macOS`. It checks
two real goal turns, default-visible answers, one visible continuation notice,
collapsed process details, inline briefing and both original turns' Trace events
through reload and close/reopen without another turn, retaining window evidence and cleanup
receipts outside the isolated case data. Its assertion regressions are in
`tools/test_native_goal_resume_notice.py`.
The actual Objective-C entry-point regressions live in
`tools/test_native_goal_resume_notice_ax.py`. A refused complete traversal
retains its first predicate, path, attribute status and type without recording
text content; it does not reveal a target or post input.
Text extraction follows the actual AX role: `AXStaticText` requires a string
`AXValue` with the existing title fallback. A WebKit `AXHeading` exposes its
numeric heading level in `AXValue`, so the helper reads only its string title
and continues through its actual `AXChildren` for rendered static text. A
heading level never becomes a text witness. Real-entry regressions retain
numeric-heading child text and textual titles while rejecting unknown/malformed
consumed titles or Children and non-string StaticText values without input.
The unique root remains a real `AXWindow` owned by the private app PID.
Its complete bounded `AXChildren` graph may include remote-process WebKit
elements; each must have a real AX type and a successfully sampled positive
owner PID. Every reveal/input target must be identity-reachable in that graph
and have a fresh, bounded, cycle-free `AXParent` chain ending at that exact root.
Canonical parents may use another branch, including a parent absent from the
Children graph; individual Children edges need not be reciprocal. The helper
records actual target/ancestor owner PIDs and explicit root-binding metadata.
Regressions exercise remote bridges, duplicate DAG children and canonical
parents through the real entry point, plus zero-action refusal for unknown
child owners, detached/cyclic/foreign-root parents and changed root identity.
Reveals and the native refresh require fresh binding readback before wheel
input. The hit must equal the exact notice with its actual sampled owner;
a wrong hit receives no wheel, and the one wheel still targets the original
private app PID.
Before the one reveal, known finite offscreen target/ancestor frames may have
an empty clipped viewport. The helper still validates every ancestor's geometry
and exact root binding. After reveal, inspection and wheel admission require
the nonempty native scroller viewport and a target contained by it; unknown frames
or a detached root never qualify as offscreen content. Real-entry regressions
start both the target and its local section outside the window, move both into
view only on the one reveal, and retain refusals for unknown outer geometry,
detached parents and real scrollers that remain outside after reveal.
`viewport_basis: native_window_scrollarea_webarea` limits that geometry to the
root `AXWindow` intersected with actual `AXScrollArea` and `AXWebArea` ancestors.
Every ancestor still needs a real type, known positive owner and finite geometry
and remains in the diagnostic ancestry. Ordinary `AXGroup` frames are layout
bounds and do not establish clipping: text may extend beyond them. Ancestor
`used_for_native_clip` flags record which frames participate. These native bounds do
not prove CSS clipping or final rendered visibility; acceptance also requires
reviewing the actual target-window PNG. Real-entry regressions allow a text
target extending 7px past a layout Group while rejecting the same geometry when
that ancestor is an actual ScrollArea, with unknown viewport geometry refused.
The initial completed objective is fixture setup through normal local
`POST /api/threads` and `POST /api/threads/:id/turns` with `/goal` text, inside
that case's private conversation workspace. It waits for the accepted first
turn's real offline model calls, paired `complete_goal` result, goal completion
and manager finish; `goal-notice-fixture.json` and
`goal-notice-fixture-trace.json` retain those identities and receipts. After
reload, the native window opens Conversations and selects that conversation.
A normal `PATCH /api/threads/:id` with `goal_resume: true` supplies the second
real turn as fixture setup; a completed goal intentionally has no native Start
button. After reopen it selects the title from the restored conversation list.
Each control is observed as uniquely available and enabled before one press;
missing controls wait within the same deadline, ambiguous controls or failed
presses fail without retry. `goal-notice-availability-last.json` retains the
requested label, match count and raw snapshot. Fixture restoration uses the normal goal-resume API and never inserts
lifecycle events or fabricates model output.
Native acceptance covers default-visible answers and
continuation notice, collapsed process details, inline briefing controls,
Trace retention, reload and close/reopen. Initial slash typing is covered by the browser
E2E above, so this case does not depend on unrelated system input candidates.
Prepare its narrow `tools/native_goal_resume_notice_ax.m` helper before the case
with `python3 tools/native_goal_resume_notice.py --prepare-session-ax /absolute/private/native-goal-session-ax`.
The helper reads actual process-disclosure `AXExpanded` state and checks answer
and briefing text against the private fixture's real Trace and compact receipts.
Answers require no session-disclosure click. An older visible resume notice
cannot stand in for the matching continuation check. The helper's
source and binary hashes must match its preparation receipt.
Each live, reloaded and reopened answer capture first reveals the unique
continuation notice once, verifies its actual AX type, PID, native scroller
viewport and hit target, then posts one upward wheel in the guest to leave
automatic following. It does not change product follow state through script.
For each text witness, the narrow `reveal-text` action completes enumeration
before selecting one recorded AX child-index path and requesting one reveal.
Opening witnesses must be unique. Repeated closing prose is common tail
evidence only; it does not identify either turn. `inspect-text` checks the
same path, complete witness and match count before and after the screenshot,
without another reveal or input. The generic snapshot receives no expected
text and performs no target scrolling.
Every native and screenshot command retains independent
`goal-notice-helper-NNN.json` evidence with complete stdout/stderr, command,
return status and monotonic start/end times; image receipts retain their
capture interval and command-receipt identity. This case writes its own JSON
and precreated PNG evidence with mode `0600`. Only the screenshot child uses
umask `077` to keep replacement files private; the parent's normal umask and
helper/compiler subprocess permissions are unchanged. Evidence-write failures
retain the original action failure and streams through the final scenario
receipt, including stdout fallback when that receipt cannot be saved.
On initial launch and reopen, a read-only `startup` observation distinguishes
an unregistered application from a registered wrong bundle. Only a null bundle
with `registered=false` may wait within the original case deadline; each attempt
rechecks the actual executable/case PID and keeps `goal-notice-startup-NNN.json`.
A wrong nonempty bundle or helper failure is fatal. This wait neither opens a
window nor relaxes the later Accessibility, session and screenshot assertions.
The generic helper's single-window, exact-draft and Send safety checks are
unchanged; this dedicated case has no composer/Escape workaround. Its
feature scenario has an explicit 150-second action deadline and 180-second
whole-case success budget, including cleanup. Its measured scope is two real
offline goal turns, default-visible answer observations, collapsed process
details, a normal compact request, native inline briefing expansion/collapse,
reload and close/reopen. The daily smoke round retains its separate 60-second
whole-round budget.
Before each continuation-notice screenshot, `reveal-notice` selects the unique localized UI receipt,
uses `AXScrollToVisible` when needed, and checks its whole frame against the
window and ancestor scroll viewports. The scenario independently rejects
missing, nonfinite, empty or offscreen geometry before taking a screenshot;
`01-resumed.viewport.json`, `02-reloaded.viewport.json` and
`03-reopened.viewport.json` retain that evidence. Answer and briefing captures
use their expected text, actual native scroll-to-visible geometry and decoded
PNG evidence; inspect those images before accepting visible content.
An AX node count alone cannot accept a notice outside the captured viewport.

## Late review turn boundaries (F-120 / F-130 / C-003)

`frontend/src/lib/turn-boundary.test.ts` covers consecutive slices without
reordering events, the single footer owned by the last visible slice before
the actual terminal sequence (with legacy end-time fallback), hidden
title/session-memory metadata, quiet turns,
and late notices for a turn that has already ended.
`frontend/src/components/app/transcript-boundary.test.tsx` renders the real
transcript to retain the original answers while preventing
duplicate or metadata-only footers; late review notices remain visible outside
completed goal-session process disclosures. A finished tool followed by an answer establishes
the current turn's live planning status before an older review arrives; that
status must remain visible without adding a footer for the running turn.
`frontend/e2e/late-review-footer.spec.ts` exercises
the browser transcript with delayed review events, paged history and reload,
including default-visible original goal-session answers and opening their stored tool result. These are
regression scopes; a browser check does not replace native-window acceptance.

```bash
cd frontend && npm test -- src/lib/turn-boundary.test.ts src/components/app/transcript-boundary.test.tsx
cd frontend && npm run e2e -- e2e/late-review-footer.spec.ts
```

## Desktop reopen at engine idle exit (F-110 / C-001)

`cmd/zwai/desktop_attach_test.go` starts the real offline engine with a controlled
idle clock, returns native options before any frontend loads, then advances the
clock past the idle boundary. The pre-fix failure was the engine exiting before
frontend presence. Closing the native hold twice must release it and permit idle
exit. `desktop_attach_errors_test.go` exercises real HTTP POST/GET stopping and
disconnect paths against a real lease holder, then verifies a fresh engine's
identity only after that holder departs. A still-held unhealthy, forbidden or
mock-mismatched owner must remain untouched; handshake timeouts differ from the
unbounded lifetime of an acknowledged stream.
`desktop_attach_identity_test.go` replaces a real lease-holding child before the
failed reservation returns and republishes a different PID at the same loopback
URL. It first failed with one POST and `engine_stopping`; the repaired launcher
must validate that replacement and obtain a second acknowledged handshake.
The preserved record includes PID, start time and build identity, rather than
using the address as the owner.

`desktop_attach_stream_recovery_test.go` drops an acknowledged real HTTP stream
while its native process and the real offline engine remain alive. The old
implementation lost the counted slot and allowed idle shutdown; the fixed
owner must reserve and acknowledge another slot on the same engine and PID
before the grace expires. `desktop_presence_test.go` checks consecutive short
streams exhaust three attempts, a stable hold renews the next recovery episode,
typed stopping causes survive, and release interrupts HTTP and backoff.
`internal/desktop/lifecycle_test.go` tests terminal failure before and during
startup, Quit after ApplicationStarted, normal shutdown and racing Run exit.
It also ends Run while Quit is in flight, ensuring the cause returns without
waiting for a native-loop callback. `lifecycle_terminal_test.go` runs a real
subprocess whose fake native Quit calls `os.Exit`: the configured structured
logger must write the terminal cause once before that non-returning boundary.
The observed pre-fix RED reached Quit with only its marker and no cause. The
production callback synchronously logs before queueing Wails Quit. Early, nil,
closed and normal release paths remain silent; returning platforms retain the
typed cause. These tests do not open Wails; changed native code still needs the
same candidate's real window and exhaustion acceptance.

The 2026-10-22 full five-package race profile measures `holdDesktopPresence`
at 100%, `maintainDesktopPresence` at 96.7%, `openDesktopPresence` at 93.5%,
and `reserveDesktopPresence` / `checkDesktopPresenceResponse` /
`nativeFatalQuit` / `runWithFatal` at 100%. Unit coverage of these coordinators
does not accept the new package's native Wails event-loop behavior.

`internal/server/presence_shutdown_test.go` races attachment and final idle
commit under the real registry lock. Exactly one wins. The HTTP counterpart
checks typed `503 engine_stopping` for new and reserved ids, and rechecks an
actual running turn. `TestIdleCommitRetainsRestartOwnership` prepares a real
engine handoff to ready with no running manager, then requires the final idle
check to retain `ActiveWork` and keep presence admission open. It first failed
by committing shutdown with active work still owned. The final check uses the
same count as the watcher, including parked worker and handoff ownership.
The desktop frontend recognizes only `pc_` plus sixteen
lowercase hex digits as native-owned presence. It does not take over that
socket: reload and frontend closure leave the native slot counted. Web and
legacy desktop URLs keep their own reservation path.

`internal/server/presence_stream_error_test.go` lets the real heartbeat timer
reach a failed socket write, then asserts that the handler returns and releases
the lost native client's slot. It does not shorten the timer or inject a
production callback. The native Wails wrapper still requires real window
acceptance; unit coverage of its server/options is not GUI acceptance.

The same profile measures `ensureDesktopEngine` at 95.5%,
`ensureEngine` / `ensureEngineRecord` / `waitDesktopEngineDeparture` at 100%,
`startDesktopServer` at 92.3%, `serveEngine` at 90.2%, `reserve` at 90%,
`attach` / `isStopping` / `TryIdleShutdown` / `Watcher.Run` / `fail` at 100%,
and presence POST/GET handlers at 94.1%/96.3%. The reserve's original random-ID
failure branch remains outside this profile; the new stopping-admission path
is exercised. The actual Wails `Run` wrapper and `runDesktop` remain outside
offline unit execution. The same candidate's real native window, immediate
reopen and terminal recovery-exhaustion path remain separate acceptance gates.

```bash
go test -race -count=1 -coverprofile=/tmp/presence.cover -timeout 20m ./cmd/zwai ./internal/desktop ./internal/server ./internal/lease ./internal/remote
go tool cover -func=/tmp/presence.cover
(cd frontend && npm run lint && npm test && npm run build)
(cd frontend && npm run e2e -- e2e/presence.spec.ts)
```

The Playwright fixture holds the launcher's real HTTP socket separately from
the app page and checks reload, closure and the single client id. This proves
frontend lifecycle behavior; actual Mac window close/reopen, the candidate
binary and the engine lease still require native acceptance. Waiting until an
old engine is gone before a successful manual reopen does not accept the
immediate-reopen race.

The mainline Pairlink follow-up fixture retains its explicit close/join
regression and cancelled dispatcher/watch cleanup before engine/store cleanup.
Closing a Pairlink connection alone does not close `Link.Recv`; the leaked
watch previously raced a later test's scoped `watchLagged` replacement. The
reconnect-status fixture drops only its first
real WebSocket and holds the replacement handshake, rather than repeatedly
expiring it during the subsequent pairing/QR request. Both preserve actual
relay, offline and reconnection assertions without production test hooks.

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

## Worker Stop receipts and replay (Issue #85, F-130 / F-222, C-002 / C-003)

Cleanup cancels workers without joining their goroutines. A cancellation
`finished` receipt may precede cleanup, follow cleanup, or follow the manager's
`interrupted` terminal. The earlier Stop fixtures omitted these real finish
receipts, so their green cleanup/replay checks did not cover the later failure.
This is a derived-state classification defect, not evidence of a stored versus
streamed sequence violation.

`frontend/src/lib/transcript-cancelled-finish.test.ts` exercises all three
orders through live reduction, cold replay and the paged roster sidecar.
`mobile/src/lib/transcript-stop.test.ts` checks the phone's independent reducer
through live events, rewatch and earlier-history paging; cleanup alone remains
stopped. `mobile/src/components/thread-stop-roster.test.tsx` checks English and
Chinese Stopped groups and worker detail styling.

Both reducers recognize only the existing engine envelope
`swarm: agent ${agentID} cancelled: ` with the current event's exact agent ID
and a nonempty cause. Regress ordinary `context canceled`, cancellation words
inside provider errors, another ID, an ID-prefix collision, an empty cause and
watchdog failure as genuine failures. Already successful or failed workers
retain their results through cleanup; an explicit same-ID resume starts a new
running lifetime and its later result remains authoritative. Retain diagnostic
causes in Trace while settling pending tools/streams without worker-failure
styling for a stop; the PC's derived worker error field is not its Trace record.

`frontend/e2e/restart.spec.ts` includes late cancellation finish receipts and a
genuine provider failure in the persisted replay/reload path.
`mobile/e2e/worker-stop-replay.spec.ts` clicks Stop in the offline phone fixture,
then inspects both locales, all three finish orders and worker details, returns
Home and reopens the same host conversation through a new watch. It does not
reset the mock host by reloading the browser.
These fixtures use generic worker identities and the engine's receipt envelope,
not words from a user's task.

```bash
(cd frontend && npm test -- src/lib/transcript-cancelled-finish.test.ts src/lib/transcript-stop.test.ts)
(cd mobile && npm test -- src/lib/transcript-stop.test.ts src/components/thread-stop-roster.test.tsx)
(cd frontend && npm run e2e -- e2e/restart.spec.ts)
(cd mobile && npm run e2e -- e2e/worker-stop-replay.spec.ts)
```

For native acceptance, use the exact new candidate's real window: observe
workers running, press Stop before they finish, retain the same worker IDs and
turn Trace, then Reload/reopen and inspect Stopped separately from genuine
success/failure. Bind screenshots and runtime/package identity to that run.
Synthetic reducer histories, an offline Web fixture or an old package's failure
screenshots do not accept the new native package. A filename saying “live” or
“Stop” is not proof that the captured turn was still running or stopped.

Continue a pending native check by executing the current audit's `next_action`.
If one control channel fails, try another available isolated native control
path in the same recovery and retain its real outcome. An SSH failure or a
rejected UI action is evidence about that channel, not a completed acceptance
attempt or a need for renewed authorization. Keep each passed source/package
receipt when main advances; compare the changed inputs and repeat the affected
checks on the combined candidate. Development-window evidence remains separate
from the final versioned ZIP's installation and native behavior acceptance.
`tools/test_audit_macos_acceptance.py` exercises the recovery decisions and exact
package matching; its green result cannot execute the returned action for the
operator.

Packaged phone drivers are `mobile/e2e/ios-worker-stop-replay.swift` method
`BindFlowTests/testNativeWorkerStopReplayRetainsStoppedAndFinishedOutcomes` and
`mobile/android/app/src/androidTest/java/com/lubyruffy/zwai/WorkerStopReplayTest.java`
method `nativeWorkerStopReplayRetainsStoppedAndFinishedOutcomes`. Use disposable
offline native fixtures with `history=stopped-workers&stop_order=after_terminal`.
Both tap Stop, check stopped versus genuine done/failed workers, inspect details,
then return Home and reopen the same host conversation for a new watch. Android
may read the actual WebView DOM to locate visible, unoccluded targets; input
remains native touch. Keep each attempt's screenshots/results in a new directory
or xcresult bundle so retries cannot overwrite a failure. These are execution
entry points, not a claim that the current package has passed native acceptance.

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
result clears a stale manual-check error without hiding an offered version. The UI
tests also retain the confirmed version after install failure so Retry installs
it directly, keep a progress-query failure separate from the pending install
result, ignore stale progress, and render actual byte counts with a percentage
only when the total is known. Component and E2E checks assert red alert styling,
the distinct check/install retry actions, and download/install/restart stages.
The UI E2E uses an offline update feed; it does not install a public Release. ZIP
replacement, launch arguments and old-app recovery run against a local launcher
stub in `internal/update/apply_recovery_test.go`. That stub does not open a
signed Mac window. Release acceptance still follows [RELEASE.md](RELEASE.md).

`internal/update/progress_test.go` reads a pipe-backed response while Apply is
still pending. It verifies received body bytes, known and unknown lengths,
retained failure, busy-request isolation and clean same-version retry.
It also checks the separate 30-minute download total / 30-second header deadlines,
the metadata request's 30-second total deadline, caller cancellation and redirect
host restrictions. `internal/server/update_test.go` reads progress through real
loopback HTTP while the installation POST remains blocked; the default response
without a progress reporter is idle with zero bytes.
`frontend/src/lib/desktop-update-progress.test.ts` covers progress polling,
direct retry and disposal without using a public update feed. A stalled poll is
aborted after five seconds and the next observation continues while the
installation result remains pending.

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
cd frontend && npm test -- src/lib/desktop-update.test.ts src/lib/desktop-update-progress.test.ts src/components/app/desktop-update.test.tsx src/lib/desktop-presence.test.ts
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
make test        # go test -race -cover -timeout 25m ./...  +  front-end unit tests + mobile unit tests
make e2e         # Playwright, on the scripted offline provider
make check       # go vet + both of the above
```

No test needs a model endpoint, an API key or a network. Anything that would
otherwise call a model runs on the **scripted offline provider**, so the suite is
deterministic and fast enough to run on every change.

## Layers

| layer | what it covers | command |
|---|---|---|
| Go unit tests | config, store, memory, provider, tools, engine, search, server, CLI, TUI, and the swarm library | `go test -race -cover -timeout 25m ./...` |
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
Pursuing / Blocked / Paused with Start only while unfinished, a completed goal
hides its strip without clearing stored state. `mobile/src/lib/transcript-goal.test.ts`
retains a localized completion notice from the JSON `goal_complete` payload.
A parked wait is Waiting
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
`frontend/ensure_dependencies_test.go` regresses empty/partial installs newer
than the manifests, missing production/development packages, missing compiler
entries and broken executable links. Install must include development packages
and pass revalidation before build/stamping; a current bundle does not require
Node. `frontend/ensure_build_lock_test.go` uses separate processes to prove
one shared checkout builds once and that a waiting preparation can be cancelled.
`frontend/ensure_process_test.go` runs the real offline npm → shell → Node
chain and cancels after the descendant starts writing. It requires the original
context cancellation error, then lets another builder acquire the lock and
asserts the old writer cannot change the output. Separate cases let npm exit
successfully or with an error while a background child remains. The Unix helper
and Windows Job Object fixtures exercise their own process ownership paths;
Windows cross-compilation is build evidence and does not certify those runtime
tests or native Windows behavior.
These fixtures do not replace the original-checkout native command check below.
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

## Agent task evals (opt-in, real model)

The layers above prove the code is correct; they cannot say whether a prompt,
delegation or harness change makes a real model finish tasks better, faster or
cheaper. `make eval` answers that, and only that. It calls a real model, so it
is never part of `make test`, `make check`, CI or the before-finishing checks,
and its results are measurements rather than pass/fail gates.

```bash
make eval EVAL_PROVIDER=default                 # every task, 2 runs each
make eval EVAL_PROVIDER=default EVAL_RUNS=3 EVAL_TASKS=direct-lookup,noisy-log
python3 tools/agent_eval.py --mock --runs 1     # harness smoke test, no model
```

- **Tasks** live in `evals/tasks/<id>/`: `prompt.md` (the human message),
  `task.json` (checks, `expect_delegation`, timeouts, optional seeded skills
  and wait permission), a static `fixture/` and/or a seeded `generate.py`
  that writes large inputs into the workspace. Fixtures are synthetic; task
  wording never enters a product prompt, default or test expectation.
- **Isolation**: every run gets a new temporary data directory and its own
  `zwai engine` child (`bin/zwai-eval`, built by the target). The harness reads
  one provider from `~/.zwai-swarm/config.yaml` (or `--config`) without writing
  it, scrubs inherited `ZWAI_*` / `OPENAI_*` variables, writes a one-provider
  `config.yaml` (mode `0600`) into the temporary directory, holds a presence
  connection, stops only its own child and deletes that directory, key
  included. The user's engine and data directory are not touched.
- **Grading** is deterministic: files, JSON values, unchanged test files,
  commands run in the workspace, answer patterns, chart values, tool and event
  counts. A conversation that asks a question (`ask_user`), hits the tool-round
  cap or times out is interrupted and recorded as that outcome. A task with
  `allow_wait` follows one parked `schedule_wake` until it fires.
- **Metrics** come from `GET /api/trace/:turn` for every turn: duration, calls
  and tokens split into manager, workers and each host-side caller
  (`title-namer`, `memory-reviewer`, `schedule-answer-policy`,
  `thread-wake-policy`, `session-memory`, `compact-summarizer`), the first
  manager request's `input_chars` (system prompt size), spawns, `fork_context`,
  tool counts, `skill_view`, `model_retry` reasons and errors. The harness waits
  up to `--review-wait` seconds (default 240) for the post-turn memory review so
  its cost is attributed; a review the engine reports as still running at
  shutdown is counted as cut off, and its tokens are only a lower bound.
- **Evidence** goes to `output/agent-eval/<timestamp>/` (ignored by git):
  `report.md`, `report.json` and, per run, `result.json`, `answer.md`,
  `traces.json`, `engine.log` and a workspace copy. Every file is redacted for
  the provider key before it is written. A harness failure keeps the first
  error; nothing is retried to turn a run green.

Compare architectures by task, not only by the total: look at which runs
flipped, and run each task more than once. `tools/test_agent_eval.py` (part of
`make docs-check`) covers the harness offline: provider extraction, key
redaction through the actual run path, every check type, the poll loop's
question/cap/wait/timeout handling, and each generated task's answer key
recomputed from its data.

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
not an ask; answering clears the list). Steering acceptance waits within the
existing two-second readiness budget for this turn's real waiter/channel and
matching durable call ID, then checks that Steer retains that same question,
Registry and pending inbox. `internal/engine/ask_steer_order_test.go` holds the
actual AskUserTool callback just before entering runtime.waitAsk: after call
publication there is no waiter until release, then the real runtime waiter arms.
This distinguishes notification readiness from question readiness without
extending the budget or changing production behavior. `internal/server/ask_test.go` is why
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
`internal/engine/schedule_completion_test.go` covers text-only scheduled results
without forced tools or retries, two genuinely empty responses failing visibly,
ordinary chat remaining unchanged, and explicit reports/actual tool results.
Synthetic model-retry cleanup is not tool work.
`internal/engine/schedule_completion_http_test.go` crosses the real Registry,
ADK and loopback HTTP adapter: Run now and automatic fires accept text-only
results in one request, blank recovery can end with text in the same run,
requests keep automatic tool selection, explicit quiet reports keep precedence,
and wholly empty output and Stop retain their bounded failure/cancellation.
No external model is called.
The desktop notice reducer and localization tests distinguish this continuation
from a genuine model error while preserving older retry envelopes.
`frontend/src/lib/schedule-view.test.ts` and
`frontend/src/components/app/conversation-schedules.test.tsx` verify that
completed/cancelled bound wakes leave the right rail, including active-to-done
updates, while creating jobs keep their source link.
`frontend/src/components/app/schedule-inbox-row.test.tsx` verifies status labels
for paused/completed/cancelled rows instead of stale next-run times.
`frontend/e2e/schedules.spec.ts` runs a one-shot through the real offline server,
checks its paused label and removal after completion, reloads the conversation,
and opens its retained Completed history (`F-150` / `C-009`).

`frontend/e2e/schedules.spec.ts` drives Run now and automatic fires through the
real UI and Trace with text-only offline responses. Both
`ZWAI_MOCK_SCHEDULE_ACK=once` (the E2E server default) and `always` now finish
without tools or retry; these knobs only affect the offline scheduled provider.
Use separate ports, data and output directories for concurrent runs. The
worker-ownership E2E failure fixture returns a genuinely empty response so it
continues testing failed-check retention without rejecting valid text.
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

The full Go package deadline is 25 minutes, including real HTTP timeouts,
recovery and cancellation regressions. In the measured expanded engine suite,
the prefix before task coordination took about 19m10s and its corrected
112-test tail took another 90.6s. This package deadline does not extend any
individual test, provider timeout, worker budget or native acceptance budget.
An indefinitely waiting fixture must still be diagnosed and fixed; extending
the package deadline cannot accept it.

The scripted provider keeps each asynchronous reasoning, text, tool and usage
chunk, but Go test fixtures pace chat chunks at 1ms instead of the executable's
18ms demonstration speed. `go run`, packaged applications and Playwright retain
18ms pacing. A test whose assertion depends on visible streaming across the
engine's coalescing interval uses `provider.NewMockWithStreamDelay` to retain
the original 18ms interval; `TestFullTurnRunsTheWholeSwarm` keeps its live-delta
assertions this way. Explicit worker fixture delays, real HTTP timeout tests,
provider retry budgets and race detection remain unchanged.
`internal/provider/mock_pacing_test.go` checks both modes, unchanged ordered
chunks, real pacing/cancellation and independent per-pool overrides. Run the
same test selection with `-race -count=1 -json` before and after a change;
compare package/test elapsed times and retain failures as well as successes.
Historical full-suite measurements above are not a current before/after pair.

```bash
go test -race -cover -timeout 25m ./...       # everything
go test -race ./internal/engine/              # one package
go test -race -run TestSteer ./internal/engine/
go test ./internal/server/ -coverprofile=/tmp/c.out && go tool cover -html=/tmp/c.out
```

`internal/terminal/pty_unix_test.go` keeps its working-directory fixture's shell
waiting on stdin after `/bin/pwd`. The PTY slave therefore stays open through
the directory-output and resize assertions; a short-lived command can instead
leave an empty EOF on Darwin after exiting successfully. This fixture retains
the existing three-second output assertion. Its timer closes its own session
at that budget so a blocking Darwin PTY read cannot wait indefinitely for
missing output; cleanup stops or joins that timer, then closes and waits for
only its own child. No terminal runtime behavior changes.

```bash
go test -race -count=100 -run '^TestStartRunsInTheGivenDirectory$' -timeout 2m ./internal/terminal
go test -race -count=1 -timeout 2m ./internal/terminal
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
a complete at the cap does not immediately recap) and later through the explicit
resume API (`ResumeThreadGoal` accepts a completed objective). Completed goal
strips no longer offer Start; this engine recovery contract remains available;
`internal/engine/goal_schedule_completion_test.go` covers the narrower scheduled
tool conflict: a manager-owned current wake cannot both continue and complete
the goal, either tool order must agree, the last check can finish, and independent
waits remain valid. Database failure checks must leave completion state and its
Trace event together. The keyless loopback HTTP fixture in
`frontend/e2e/goal-schedule.spec.ts` exercises Run now and automatic fires through
the real tool graph, checks the recoverable `goal_schedule_conflict`, then ends
the wait and completes the goal. It verifies Waiting/Open, then Idle with the
completed goal strip hidden and the completion notice retained after reload,
preserving screenshots and Trace JSON. Run separately with
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
`TestAHumanMessagePreservesAHeldGoal` / `TestResumeClearsAHeldGoal` are why
the hold survives ordinary messages and only explicit Start resumes pursuit;
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
  Finished `goal_session` / `goal_continued` turns keep every answer visible
  in `transcript-session.test.tsx`, including the live-to-finished transition
  and multiple answer/process groups. Reasoning and tool contents remain
  collapsed by default in the main user view; developer/Agents inspection and
  Find expansion retain their behavior. Armed waits, goal completion, budget
  holds and errors remain visible, with one completion footer and the hover
  clock on only the last finished answer. `MemoMarkdown` reuses unchanged
  completed answer text when another turn streams. Paged history and late
  review feedback keep their original ordering in `e2e/late-review-footer.spec.ts`.
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
  `block_goal` (edit and resume actions, including explicit API resume after a
  mistaken complete; completed banner visibility is tested separately); setting a standing
  objective while idle starts that turn; setting one during a run does not
  queue a second turn; `/compact` landing on the
  open conversation, with the hint hidden at 0% (and a compact with nothing
  open being a no-op); a live compressing pulse (`seq` 0) not marking the
  conversation compacted. A `done` then `goal_continued` (or `goal_resumed`)
  keeps `status.started_at` so the title-bar Working clock is the current
  turn, not a frozen 1s. An `error` then `goal_blocked` idles the composer
  and marks the objective blocked, so a crashed `/goal` turn is not still
  Working — a real refusal, not truncated tool JSON / `429` / a dropped
  stream (those `model_retry` then auto-continue). A `goal_idle` event holds the banner across ordinary sends until explicit Start.
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
  Successful compact notices use secondary text status; `compact-notice.test.tsx`
  covers labelled inline briefing expansion/collapse, complete whitespace-preserved
  content, token counts, Chinese/English labels and unchanged error/other notice
  cards. `transcript-goal.test.ts` verifies compaction identity survives replay;
  `e2e/conversation.spec.ts` covers the real compact API and inline disclosure.
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
- **`src/components/app/transcript-session.test.tsx`**: a completed goal notice
  stays outside the collapsed goal session whether it arrives before or after
  the final answer, so hiding the composer strip leaves completion visible.
- **`src/components/app/goal-banner.tsx`**: Pursuing / Blocked / Paused,
  no strip once complete, including a live transition from an open goal, Start
  when Paused or Blocked (not while Pursuing between sessions),
  a Paused/idle reason line that this is not an error and Start resumes,
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
  Only changed sections are submitted; a changed `ui` section carries normalized
  locale, font, size, column width and palette. Swarm/provider edits omit
  unchanged timeout fields. Each serialized write computes its delta when it
  runs, so queued edits and reverts survive. Failure keeps the previous baseline;
  success records the submitted local draft rather than unrelated phone values
  returned by the server.
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
  `PUT /api/settings` and **Back to app** flushes. Reopening waits for previous
  writes before loading and seeding a fresh draft; a stale earlier-open response
  cannot replace that draft.
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
- **Terminal quoting (`F-180` / `C-011`)**: `src/lib/terminal.test.ts`
  checks source-context formatting without inventing commands, exit status or
  a later working directory. `src/components/app/terminal-session.test.tsx`
  exercises xterm selections and ready metadata through the rendered component;
  `selection-menu.test.tsx` covers the shared Add to chat menu. Terminal quotes
  retain their client tab/source target and initial-directory/shell context
  inside the existing quote text, including across draft save and refresh.
  Browser E2E evidence does not establish native WebView acceptance.
- **Terminal appearance (`F-110` / `F-170`)**:
  `src/components/app/terminal-session.test.tsx` checks live light/dark and
  named-palette updates, hidden tabs, observer disposal and unchanged terminal
  and socket identities. `src/lib/system-theme.test.tsx` covers OS theme
  subscriptions, fixed preferences and cleanup. `e2e/terminal-theme.spec.ts`
  checks actual xterm colours and retained shell output through theme changes,
  including runtime OS colour-scheme changes in System mode.
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

Concurrent Playwright commands must isolate all three resources: server port
(`ZWAI_E2E_PORT`), application data (`ZWAI_E2E_DATA_DIR`) and artifacts
(`--output`). Separate ports and databases still leave shared output cleanup
able to delete another run's trace, network or ZIP files. For two independent
runs, use distinct paths and ports, for example in separate terminals:

```bash
cd frontend
ZWAI_E2E_PORT=18860 ZWAI_E2E_DATA_DIR=.e2e-data-default npm run e2e -- --output=test-results/default --retries=0
ZWAI_E2E_PORT=18861 ZWAI_E2E_DATA_DIR=.e2e-data-ownership ZWAI_E2E_SCHEDULE_WORKER_OWNERSHIP=1 npm run e2e -- --output=test-results/ownership --retries=0
```

Vite preview serves its build directory directly. Do not run `npm run build`
against that directory during a phone E2E run: Vite removes `dist/index.html`
before writing the new bundle, so navigation can reach an empty document before
the application mounts. Separate concurrent source/build copies as well, or run
the build and browser suite serially. Preserve startup evidence before blaming
an absent UI element on its business logic.

Keep the first failure log and its available artifacts. Inspect whether a
failure is a business assertion or an artifact/teardown error such as `ENOENT`
for a trace, network file or ZIP; do not relabel an assertion failure as an
environment problem. After correcting shared resources, rerun the complete
affected suite with isolated output and retain that separate result. A retry
turning green alone neither explains the first failure nor accepts that run.

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
| `e2e/conversation.spec.ts` | a full swarm turn, user-mode work folded behind one live ticker (`work-fold` + `swap-line` + `MarqueeText` shimmer or left-to-right scroll), a live status line marked as sweeping while the turn runs, opening a sub-agent (back control beside the scroller, not sticky on it; system prompt from the chrome; log at the live edge; a collapsed `write` opens to the file body, not `Updated file`), a generated sidebar title from the opening message (not the raw request, not a transcript row), a chart in the scripted answer with Chart/Table tabs (reload returns to the home page with nothing selected; reopening the row replays the chart), scrolling up mid-stream leaving the viewport put and a jump-to-latest control returning to the live edge (developer view, so spawn rows exist to overflow), switching conversations landing at the latest turn rather than the top of the history (latest jump-rail tick current), context carried across turns, jumping to an earlier user message from the left rail (latest tick current while idle at the live edge), Enter while `wait_agents` is pending queuing a follow-up until the turn finishes, the corner button swapping from Stop to Send once that draft is typed and the click queueing without leaving Working, **Steer** on that queued row injecting and emptying the tray, **Steer** after Stop starting that leftover row as the next user turn, pulling a queued row into the composer and Enter queueing that edit at the back of the FIFO, a pencil on unread steering doing the same, **Steer** (⌘Enter while `wait_agents` is pending) pinning unread steering under the working line with Interrupt and Delete, retracting an unread steer so the turn stays Working, Interrupt aborting the current tool without cancelling the turn, **Stop** while a tool is in flight leaving no spinner next to the interrupted banner, hovering a user request or the last finished answer fading in the reserved event clock without a layout jump, copying or editing a sent message in place so Send restarts from that bubble and clears everything below, file upload appearing in the Files panel with the user bubble naming `uploads/brief.txt`, collapsing a workspace directory in Files and filtering to a nested file, dropping a file and an image onto the composer (overlay, then a workspace chip vs a vision thumb), the turn id on the Trace summary with the event log folded until Full log, an IME-confirming Enter leaving the draft in the box, the manager tool-round cap pausing for Continue/Stop instead of dumping eino's iteration error, and switching the catalog model from a grouped searchable picker (Refresh models / Edit providers) so a reload still sends that name, and the composer context ring plus Trace usage after a turn (reload keeps the ring; the snapshot never lands as a transcript row), `/` listing goal, plan and compact without a 0% hint on an empty chat, pinning a standing objective, starting it from the banner without a human message, editing it in place, compacting without rewriting user bubbles (a labelled disclosure expands the briefing inline), auto-compacting at a low token budget with a secondary compressed status and the same briefing control, a scripted run hiding its completed goal strip while preserving the completion notice, and a one-round ReAct slice leaving a standing objective running until completion instead of pausing it as two Worked-for sessions, `/plan` (`e2e/plan.spec.ts`) showing a Planning banner and an `ask_user` dialog that spans the conversation column (**Your answer needed**, still `border-ask`, no ring or ping on the card; **Your turn** on the title bar with a pinging `ask-mark`; a numbered choice then Submit continues the same turn), then Implement remounting work and leaving planning |
| `e2e/quotes.spec.ts` | quoting selected transcript text into the next send as an editable composer annotation (count chip at rest, hover for the snippet, not `<selected_text>` tags in the bubble), and Add to chat still landing while a turn is streaming (user-mode work fold, not the inner thought box) |
| `e2e/terminal-quotes.spec.ts` | selecting real integrated-terminal text, using the shared Add to chat action, preserving its source context in the current composer draft across refresh, and submitting that reference separately from the user request |
| `e2e/skill-copy.spec.ts` | a skill copied from one project into another stays on its own file: the copy names where it came from, **Update** brings a later source change across, **Cancel** keeps a local edit when both sides changed, **Replace with source** then takes the source text, and the source file does not gain the local edit |
| `e2e/library-skills.spec.ts` | a conversation outside a project adds a skill under Shared skills (no Notes); the test identifies that skill from this turn's `memory_review` trace even when other reviews write at the same time, then Copy puts it into a project whose origin says it came from the shared skill library |
| `e2e/projects.spec.ts` | a project created from the sidebar, a conversation started from the project row that says so with the project name prefixing the title on one line, the review named in the transcript without opening a tab, **View skills** on the project menu opening the Memory tab with that skill expanded and in view (body inside its card, not over Files), the notes in the panel without a reload, the review in the same Full log as the turn, a second conversation starting with the first one's memory, a hand-edited note surviving a reload (Save notes absent until the draft changes), a deleted project taking its conversations with it after a confirm, the Memory tab not leaving a blank Agents pane above the notes or clipping Skills off the window or painting inactive Files beside Memory, Review now saying when there is nothing to review, Tidy skills saying when the catalog is empty (counts, no fake model review) and asking the model when a skill is already there (scan/review, then a reviewed report with a non-zero scanned count), hovering a project row revealing a new-conversation control that starts one in that project rather than Conversations (the folder is not pressed; the open topic is `aria-current`; the folder glyph is open when expanded and closed when collapsed; a running conversation's progress sits in that same icon column; topic names sit under the project name; there is no drag-grip glyph), pinning a project topic to the top across reload, dragging a project pinning that order across reload, a sixth topic in the folder sitting behind **Show more** until it is opened, and a running conversation keeping its sidebar progress after switching to a new conversation (an explicit folder collapse keeps a clipped breathe-dot on the glyph, not a smear) |
| `e2e/markdown.spec.ts` | the scripted answer paints a tagged `go` fence (Copy code + syntax colour), `$n$` as KaTeX, a GFM table that stays inside the conversation column (not under the side panel), and a heading rendered as a heading while the turn is still Working |
| `e2e/goal-resume.spec.ts` | `/goal` on the mock provider completes, hides its composer strip and retains the history completion notice without clearing persisted goal/completion fields; reload and reopening keep that state, and setting a new goal displays the strip again |
| `e2e/schedules.spec.ts` | a standalone wait created from the Scheduled page in the main column (New task opens the right drawer, Task only — the inbox name is generated — Repeat → On an interval, Every (seconds) 60, Add wait; Agents/Files/Trace rail hidden; Expand fills the page (`data-expanded`) then Collapse restores the list), click the row to open the editor, Run now, Escape back to the conversation, unread / Open findings landing on the minted conversation whose title matches that generated name, with a `Scheduled check.` chip and no user bubble of the protocol wrapper; REST create then click-to-edit title/prompt/cadence and Save; a REST `kind=thread` wake on the open conversation showing the composer banner with Run now and Cancel wait, a breathing wait clock on the sidebar row and **Waiting** on the title bar, Cancel wait removing the chip and returning Idle, Run now starting Working, hiding the wait banner, a `Scheduled check.` chip, then Waiting again with the clock once the check finishes. Mock provider, no `ZWAI_MOCK_SCHEDULE_WAKE` |
| `e2e/remote.spec.ts` | Settings → Phone: Hub URL, no Host Token field, Event text on the phone, Events on the phone, Keep this computer awake, Bound phones empty copy, Show pairing QR, no QR pixels while the hub is unset; the failure toasts over the sheet in viewport (× dismisses it). A stubbed binding paints the reported model and last-connected, not a bare fingerprint. A binding that appears on a later list read while the QR stays up is painted without leaving Phone. |
| `mobile/e2e/goal-completion.spec.ts` | English/Chinese phone path starts with a visible unfinished goal, sends a turn that completes it, hides the composer goal strip and shows one localized completion notice; leaving and reopening the conversation preserves both |
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

- **The desktop window**: run the isolated native case above in `ci-macOS`.
  Check that the window
  opens, the UI loads, a turn runs, an upload opens a native file dialog, and
  "Show in Finder" works. A link in the transcript must open the system
  browser, not replace the window. The Dock / taskbar should show the rounded
  app mark at the same size as a bundled `.app`, not a square canvas, not a
  full-bleed tile, and not the generic Unix-exec glyph.
  For a reported checkout startup failure, run the reported command from the
  exact source/dirty-content-identified guest copy under the isolated lifecycle
  above. Retain its startup/build output and observe the window plus a completed
  offline turn from that launch; a different snapshot or separately launched
  `.app` is not acceptance of the reported command. Keep the original host
  checkout's source and served-asset checks read-only.
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
  tree and previews use read-only Pairlink RPC, while full Settings, PTY and Trace
  stay on the PC. Paired phones also expose separate PC timeout controls. The hub hostname is typed on the PC, never shipped in the binary.
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
`mobile/e2e/ask-answer.spec.ts` also checks the actual Other textarea font size
before typing and retaining a rejected answer. The Issue #104 regression first
observed 14 CSS px against the existing 16px focus boundary. Use
`IOS_LAYOUT_QUERY='/?mock=1&ask=1&askfail=1&tick=0'` and
`mobile/e2e/ios-ask-viewport.swift` in an isolated English native fixture for
`testAskChoicesRetainAnswerAndNativeViewportAcrossFocusAndRetry`. It records
actual App/main/Back bounds and PNG/AX evidence while focused, after keyboard
Done, after rejection and after successful retry; functional assertions alone
cannot accept layout. Keep the original cropped screenshots. Main-composer
acceptance does not cover a separate Ask editor, and browser CSS checks do not
replace this packaged native case. User pinch zoom remains enabled.
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

`python3 -m unittest discover -s tools -p 'test_release_android_retained.py' -v`
runs the actual `github_platform` with temporary installer/ledger files and
substituted external commands. It first reproduces an accepted Android APK
being rebuilt, then verifies exact-byte reuse, every receipt identity/status,
missing/changed/non-file installers, malformed or missing ready receipts,
historical batch recovery, source drift and unchanged public-asset digest
retries. `tools/test_release_public_recovery.py` also runs actual `execute`
and `source_preflight` against a temporary bare Git remote and public tag:
a conflicting same-version batch must not fall back to a new APK build even
when the tag matches. The ordinary unaccepted pending build path remains covered. No test
builds an APK, installs it, contacts GitHub or uploads; actual device acceptance
and signature/upgrade checks remain separate.

`python3 -m unittest discover -s tools -p 'test_release_public_recovery.py'`
uses real temporary local Git remotes and an offline Apple adapter to reproduce
TestFlight-first publication without a GitHub tag. It checks frozen recovery
after main advances and from historical batches, exact source/build/train/IPA
receipts, both directions of group membership, review/expiry states, conflicting
tags, unchanged new-batch and native gates, and a second real release process
refused by the held publication lock. Retained first-public evidence permits
other platforms after iOS expiry or loss of the Apple client; malformed proof
never falls back to fresh queries. Legacy preflight is read-only, actual recovery
saves proof before even a failing upload, and iOS itself still checks current
installability. No test contacts Apple or publishes.

`python3 -m unittest discover -s tools -p 'test_release_ios_groups.py' -v`
uses a strict offline adapter that rejects both unsupported build-to-group GET
routes and combined app/build relationship filters. It checks the supported
single-build-filter collection, its later pages, all four configured groups in
both query directions, read-only checks versus actual success recording, and a
failed group read without a partial success/proof write. The first-public proof
stays schema 1; the existing recovery suite continues to reject incomplete old
proof and accepts retained evidence without an Apple client. A successful
preflight is not evidence of current installability or publication.

`tools/test_release_ios_compliance.py` verifies the authorized app declaration is retained in source, a new source/build passes read-only preflight without borrowing an old answer, both boolean classifications are accepted, and invalid values, missing/mismatched IPA declarations or conflicting exact receipts prevent publication. The preflight regression asserts no archive, upload or private receipt write occurs.

`make docs-check` includes `python3 -m unittest discover -s tools -p "test_*.py"`. `tools/test_release.py` uses temporary source/ledger/IPA fixtures and injected Apple/command adapters to verify independent platform failures, source/version/tag gates, daily availability counting and retained signed candidates, remote reviews absent from local ledgers, import caches not dirtying release source, no duplicate upload, compliance refusal and retained exact-build confirmations after config switches (including wrong-source and conflicting-record rejection), signature/profile/version validation, archive/export settings, review waits, extra batch platforms, and old iOS completion after a newer batch is public. `tools/test_audit_pending_batch.py` verifies that an extra platform needs an integrated main commit and does not rewrite phone Issue scope. `internal/release/safety_test.go` verifies immutable attachment digests and independent platform assets; run `go test -race -cover ./internal/release/...`. `tools/test_release_ios_versions.py` checks marketing-version reuse across batches, explicit new-version overrides, frozen legacy/source/IPA recovery, separate batch/train receipts, IPA and remote train validation, review-free external availability, and no resubmission while review is pending. No test contacts Apple or publishes. Actual `release-check` checks the real source and private configuration without uploading; actual publication remains subject to native acceptance and batch gates.

`TestStatusGoesOfflineWhenHubCloses` holds the replacement WebSocket handshake with a channel after the real relay idle disconnect, then observes offline and releases the handshake to test Offer reconnect. A 10ms status poll can miss an entire fast reconnect; this fixture controls the handshake instead of changing production retry behavior or increasing timeouts. The prior fixture failed 1/10 isolated race runs; the synchronized fixture passed 20/20 and the remote package race suite (Issue #74).

The packaged `testNativeIOSMenuHidesAndroidUpdateCheck` scrolls the provider form
before tapping Save: provider visibility fields may move Save below a small
iPhone viewport. Native acceptance must reach the menu after configuring the
endpoint, rather than tapping an offscreen control.

## Phone fenced source highlighting (Issue #91, F-220 / F-230 / C-032)

`cd mobile && npm test -- --run src/components/markdown-highlight.test.tsx src/components/markdown.test.tsx`
checks C++ aliases, untagged/unknown plain text, exact whitespace/HTML literal
copying, open-to-closed streamed fences and stable block/copy state. Existing
Markdown tests keep chart, formula and safe-link behaviour covered. The phone
imports only the shared pure source lexer; token class literals stay in mobile
sources so the mobile Tailwind build retains both themes' syntax colours.

`cd mobile && npm run e2e -- markdown-highlight.spec.ts` reaches paired and
direct conversations on Chromium and iPhone WebKit. It checks computed token
colours in light/dark themes, narrow-screen bounds, source preservation and
copying through the actual message renderer. Each concurrent run needs its own
server port and output directory, as described above.

Native acceptance uses the packaged app in a private iOS simulator and Android
headless emulator with offline input. Keep the source/dirty manifest, production
web-asset hashes, reached-screen screenshots, assertions, actual exit status and
cleanup receipt. App launch, DOM token classes or a browser pass alone do not
accept either native platform. Local QA does not increment release versions or
build numbers and does not upload anything.

The iOS fixture substitutes `mobile/e2e/ios-code-highlight.swift` as its private
`BindFlowTests.swift` and selects
`AppUITests/BindFlowTests/testNativeCppFenceRetainsSourceAndCopyAcrossThemes`.
Android selects
`com.lubyruffy.zwai.CodeHighlightTest#nativeCodeHighlightPreservesSourceCopyAndSystemThemes`
through `AndroidJUnitRunner`. Both enter and send a generic fence using native
controls and retain light/dark screenshots. Android compares the actual native
clipboard with the source inside the selected test. The iOS XCTest taps Copy;
its outer collector must then run `xcrun simctl pbpaste <owned-UDID>` with a
five-second deadline before deleting that simulator, and compare the exact code
bytes with the expected source, distinct from the initially pasted fence.
Both a passing XCTest and this clipboard postcondition are required; a
cross-app `UIPasteboard.string` read in the UI test runner can block and is not
a reliable collector. Android additionally reads the reached WebView's
computed colours and panel/viewport bounds without changing its DOM.

The iOS screenshot guard runs on the actual captured frame after the C++ panel
and Copy control are visible. It requires a mostly opaque dark background and
visible foreground contrast, rejecting light, empty black and transparent
frames. `AppUITests/DarkScreenshotTests` checks synthetic light/dark text and
blank images without launching the target app. This guard catches an
appearance getter reporting dark while the captured app remains light; it does
not identify the UIKit mechanism or replace syntax-colour inspection, real
Send/Copy, the exact clipboard postcondition or actual image review.

For a focused cold-dark check, establish dark appearance on a new owned
simulator before launching the app and select
`AppUITests/BindFlowTests/testNativeCppFenceRetainsSourceAndCopyInEstablishedDark`.
Keep earlier light evidence separate, and retain the original two-theme test
and its failures. Build only a private Runner when reusing an accepted target:
use a Runner-only test target, then a standard V1 `xctestrun` with `TestHostPath`
pointing to that Runner, `TestBundlePath` to its test plug-in and
`UITargetAppPath` to the preserved original app. Execute `test-without-building`
and compare the installed target's full file/resource hashes before and after;
reinstalling exactly the same app is permitted. `UseDestinationArtifacts` is
not supported for an iOS Simulator destination by the current Xcode; its
exit-70/zero-test rejection is a test-start failure, not a UI assertion or a
permission problem. Never substitute another app or rebuild a frozen target to
make the test pass.

Keep the actual `xcodebuild` exit status as well as the selected XCTest result.
If finalization stalls, capture its stack before acting: optional
`collectSimulatorDiagnostics` / `simctl diagnose` can wait after the case ends.
Cancel only a diagnostic child whose parent, owned simulator UDID and private
result path have all been verified, then observe the parent's real exit.
For subsequent isolated runs, the supported `-collect-test-diagnostics never`
option avoids verbose system-diagnostic collection; test logs, screenshots, AX,
clipboard assertions and cleanup receipts remain mandatory. A passing case
line alone does not accept a hung or cancelled parent command.

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

## Native Mac acceptance recovery (`F-430`, `C-010`, `C-031`)

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

The optional `native_window.ssh_helper_permission_denied` and
`native_window.trusted_guest_terminal_available` booleans describe this
recovery's observed control paths, not requested permissions. A ready guest,
nonempty evidence and matching guest/four-field candidate are all required
before both true flags yield pending /
`inspect_native_window_via_guest_terminal`. The regression first observed the
old CLI reject these flags, then covers the action through the CLI and pure
decision function, all false/missing combinations, every mismatched identity
field, absent evidence, strict boolean types, unchanged input and precedence
of unknown state and already completed exact-package acceptance. Existing
observations without these optional fields retain their behavior.

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

Mac native verification runs the current packaged candidate in `ci-macOS`
through the isolated case lifecycle above. Its HTTP-adapter scenario replaces
the initial mock client/engine within that case and uses a local keyless
empty-502 model fixture without `--mock`: verify terminal error, Idle state,
same-turn Trace status and Full log, three LLM calls and two retry events, and
save window/Trace evidence before final cleanup.


## First-byte timeout configuration and guidance

### Actual checkout delivery

For a reported `go run ./cmd/zwai desktop` failure, record the absolute checkout,
HEAD, index and dirty-file hashes before integration. Preserve unrelated edits
and verify the fix reached that checkout. Do not launch the command on the
user's host desktop. Transfer the exact source/dirty-content-identified snapshot
to `ci-macOS` for the corresponding native scenario. A different source snapshot
does not accept the defect; a guest run does not prove the host's already-running
binary changed. Verify host source and currently served assets read-only with:

```bash
python3 tools/check_checkout_delivery.py --checkout /absolute/checkout --required-commit <fix-sha> --url http://127.0.0.1:<actual-port>
python3 -m unittest discover -s tools -p 'test_check_checkout_delivery.py'
```

The checker allows dirty trees, rejects missing fix ancestry, compares every
module script and refuses redirects, external origins and escaping paths. Its
`native_behavior_verified=false` requires a separate real-window observation
inside `ci-macOS`: bind the guest launch PID, source snapshot and origin,
trigger the loopback timeout, open its field, save a longer wait and verify a
successful new turn in that same window. Finish the case's process/data cleanup
before starting another scenario.

### Configuration and recovery

`internal/config/provider_timeout_test.go` covers the 30-second default,
missing/invalid legacy values, save/reload and a first-byte wait longer than the
independent stream idle timeout. `internal/server/settings_timeout_test.go`
checks actual PUT/GET settings round trips, including an older client's omitted
field preserving the saved value and an explicit nonpositive value resetting it.
`internal/provider/first_byte_test.go` exercises the real Eino HTTP adapter,
independent initial/idle waits, actual timeout duration, cancellation and the
single fresh-connection retry. `internal/provider/http_test.go` retains the
moving-stream idle and connection recovery regressions.
`internal/provider/responses_timeout_test.go` covers the same independent budgets
through both Generate and Stream on the real loopback Responses adapter.
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

Native macOS acceptance uses the same loopback endpoint in `ci-macOS` and the
isolated case lifecycle above: send in the actual desktop window, follow the timeout button, verify
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

The 2026-10-26 local content-admission candidate passed the complete provider
package with `-race -count=1` at 96.5% statement coverage. The gate, admission,
content predicate and Generate/Stream wrappers reached 100%; the Responses
event handler reached 97.7%. These are local checks, not a release or native
desktop acceptance. The previous completion-only probe test failed at the
new early-admission assertion before the implementation change. An overlay
of the old Responses completion handler also reproduced tool output escaping
before a contradictory error/completed event was rejected.

Before this integration, the local provider race audit reported 96.0% package
coverage and Pool.Get at 93.3%; the 2026-10-21 remote audit reported 95.3% package
coverage. Its shared gate, admission/classification, HTTP reconnect adapter,
Generate/Stream wrappers, stream-completion observer, engine wait recording
and call-recorder dispatch reached 100% target statement coverage in focused
race runs. These are separate pre-integration observations, not coverage of
the merged candidate, which requires a fresh run. The 2026-10-19 loopback 502
browser run observed HTTP arrival gaps of 5002ms and 10003ms for the unchanged
three-call budget; rerun that fixture for the merged candidate.

`internal/provider/backoff_test.go` uses `testing/synctest` to assert the actual
5/10/20/30-second sequence without wall-clock sleeps. It covers one recovery
probe, healthy concurrency, stale in-flight replies, cancellation, renewed
cooldown notifications and idempotent release. `model_backoff_test.go` exercises
classification and recorder/context wiring; authentication/parameter errors
cannot add failures or prove recovery. `backoff_progress_test.go` checks content
admission without settling probe health, parallel and stale results, cancellation
after progress, and the current reconnect ticket under `testing/synctest`.
`backoff_integration_test.go` uses the
real Pool/SDK boundary with keyless loopback HTTP fixtures to verify
cross-role/model/provider and cache-invalidated calls, independent origins,
malformed successful streams and delayed/cancelled header reconnects. After an
initial 503 and cooldown, a probe remains open: before its first decoded nonempty
body, reasoning or tool-call delta, a sibling role/model request must not reach
the server; after that delta, it must arrive before the probe finishes. Headers,
role-only/empty deltas, usage-only frames and error frames cannot release it.
Parallel successes and failures during the open probe cannot reset or advance
the failure streak, and late results cannot rewrite a newer generation.
`internal/provider/backoff_progress_http_test.go` also drives a real configured
body-idle timeout after content has opened admission: the failed probe renews
cooldown while an already admitted, heartbeat-fed sibling stream stays alive.
An actual Chat Completions HTTP probe ending after valid deltas but without
`[DONE]` or finish reason must fail with `io.ErrUnexpectedEOF`, retain received
usage and renew cooldown for the next request without cancelling already
admitted siblings. Complete `[DONE]`, `stop`, `length` and `tool_calls` responses
preserve usage and clear failures only at the final result; non-streaming
Generate also waits for its complete result. `stream_completion_test.go` verifies exact unchanged
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
Late waits after a quiet scheduled report remain hidden from Chat while their
raw Trace events are retained. `internal/provider/backoff_responses_test.go`
exercises the actual Pool/Responses boundary, including reasoning, output caps,
cloud-tool receipts and cancelled admission producing no invented model call.
`internal/provider/backoff_progress_responses_test.go` checks Responses probe
content and the non-streaming Generate boundary through both protocols.
Responses probe content may release concurrent admission, but only a legal
`response.completed` accepts its final success; a raw `[DONE]` does not. The
isolated classification HTTP fixtures retain timeout coverage with no origin
`model_backoff` observation or change to the origin failure streak.
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

## Provider Responses and cloud tools (`F-175` / `C-030`)

Use keyless loopback fixtures, never a real model or network service:

```bash
go test -race -timeout 25m ./internal/config ./internal/provider ./internal/server ./internal/engine
cd frontend && npm run lint && npm test && npm run build
ZWAI_E2E_BUILTIN_TOOLS=1 ZWAI_E2E_PORT=8819 npm run e2e -- builtin-tools.spec.ts
```

`internal/config/provider_tools_test.go` and
`internal/server/settings_provider_tools_test.go` verify old configuration,
protocol/type errors, nested parameter round trips, omission/clearing, API key
masking and refused writes. `internal/engine/responses_spawn_replay_test.go` verifies the actual Registry/ADK
boundary keeps merged refused-spawn calls paired without resurrecting discarded
calls from raw Responses metadata. `internal/engine/builtin_tools_test.go` drives the
actual Responses HTTP adapter and Eino manager: cloud and local declarations
coexist; hosted calls never execute locally; usage, sources and attributed
receipts persist once in the original turn. The opt-in browser test checks
clickable sources, Trace Full log and reload through the real server with a
keyless local Responses endpoint. Default `make e2e` remains offline scripted.

Responses streams retain ordinary text deltas, function argument pairing,
non-strict optional function parameters, encrypted reasoning, HTTP/stream error
origin and context cancellation. The framework's plain StreamReader.Close does
not expose a transport-close hook: it is observed at the next publication;
request context cancellation or the configured silence timeout bounds a quiet
upstream. User Stop uses context cancellation. Quiet scheduled checks hide cloud
notices in Chat while retaining their full metadata in Trace.

`internal/engine/responses_budget_test.go` regresses replay without billed usage:
ASCII/CJK/emoji hosted output and encrypted reasoning are included using the
larger of the visible estimate and actual filtered Responses replay JSON. Shared
text is not counted twice and removed calls do not inflate the request. The
Registry/ADK loopback test observes compaction before the HTTP request, preserving
the current human request and briefing. Existing billed-usage precedence remains
unchanged; this test does not establish its accuracy for arbitrary providers.

## Question options without model IDs (`F-130`, `F-220`, `C-002`)

`frontend/src/lib/transcript-ask.test.ts` and `mobile/src/lib/ask.test.ts`
regress label-only, blank/invalid and normalized explicit option IDs.
`mobile/src/components/ask-card.test.tsx` checks that a label-only choice submits
its original label. `frontend/e2e/plan.spec.ts` uses the real offline manager
and host tool with an omitted option ID, verifies the live form and pending
form after reload, submits that option into the same turn, then reloads the
settled answer. The trace retains the raw call and matching result.

```bash
(cd frontend && npm test -- src/lib/transcript-ask.test.ts)
(cd mobile && npm test -- src/lib/ask.test.ts src/components/ask-card.test.tsx)
(cd frontend && ZWAI_E2E_PORT=8936 ZWAI_E2E_DATA_DIR=.e2e-ask-options npm run e2e -- plan.spec.ts --output=test-results/ask-options)
```

Browser checks validate the shared Web UI and do not establish native-window
acceptance or update an already running engine.

### Urgent publication authorization (F-430 / C-010)

`tools/test_release_urgent.py` and `tools/test_release_ios_urgent.py` verify that
only a nonempty current `--urgent-reason` relaxes aggregation or the local
TestFlight daily limit. Source/workset/native/signing/remote gates remain in
force, saved reasons do not authorize later runs, and check-only runs do not
write publication receipts.

Phone conversation loading also has packaged native cases in
`mobile/e2e/ios-conversation-open.swift` and
`mobile/android/app/src/androidTest/java/com/lubyruffy/zwai/ConversationOpenTest.java`.
Use a disposable fixture with `/?mock=1&tick=0&history=hidden&pause=open`.
Both cases check empty live edge, Earlier history, Back and reopening the same
conversation before reading Earlier again, retaining PNG and accessibility
observations. Android API 33 text polling reads the actual rendered WebView
DOM because its cached accessibility tree can omit a painted paragraph;
all navigation and Earlier actions still use native accessibility clicks.
An unloaded body or null callback remains pending within the original poll.
Native fixture behavior supplements bounded-query/RPC and
Chromium/WebKit regression tests; it does not measure a real paired network.

### Engine fixture timing and Store ownership

Measure a complete engine run with `go test -race -cover -count=1 -json
./internal/engine`. Keep the package and command wall times, the complete test
set, coverage and host admission evidence. Per-test elapsed totals omit some
process work and must not replace package time. A timed subset is not a complete
engine result, and `-count=1` prevents a cached test result from becoming a speed
sample.

Engine fixtures use at most four concurrent preparation workers and a bounded
ready queue of independent disk Stores.
Every Store still runs the real constructor, migrations, search setup and WAL
configuration. It belongs to one test, is never recycled, and has its own
callbacks and directory. Explicit-directory fixtures keep their ordinary open
and reopen path. `TestMain` stops and joins preparation and closes unused Stores
before the process exits, so this work remains inside package timing. A failed
close retains its directory. Stop prevents new preparation admissions; an already
admitted `Store.Open` is joined rather than abandoned.

For a single-factor comparison, run the same selection and repetition count with
`-args -engine.store-prefetch=false` and then with
`-args -engine.store-prefetch=true`. This flag belongs only to the engine test
binary; do not pass it to `./...`. Include startup and final pool cleanup in both
samples, and retain all samples rather than selecting the fastest one.

`internal/timing` lets a fixture supply an instance clock to endpoint backoff or
durable answer recovery. Normal constructors retain real clocks. HTTP timeouts,
request duration telemetry and the scheduler clock remain separate. A manual
clock advances only registered timers; cancellation, probe generations, persisted
RetryAt and the original 5/10-second waits remain assertions. The real HTTP test
that waits beyond thirty seconds and checks its recorded duration stays in the
complete suite. No fixture changes the Go mock's default 1ms chunk pacing or the
explicit 18ms live-stream test.

A 2026-10-28 same-host sample with Go 1.26.1, `-race -cover -count=1`
measured 781.192s before these fixture changes and 348.830s afterwards for the
complete engine package. All 1,382 original top-level tests and 710 original
subtests ran with their original outcomes; the candidate added 21 top-level tests
and nine subtests. This is a measured 55.3% package-time reduction, not a stable
one-minute result. Command wall time was 784.78s and 359.20s respectively; host
queueing and source preparation are separate. Peak RSS rose from about 666MB to
1,016MB, so prefetch trades memory and extra preparation CPU for lower latency.
The largest three remaining serial test times alone totalled 68.80s, including
the unchanged real HTTP boundary. Further parallelization must first isolate
process environment and global state, rather than mechanically adding
`t.Parallel` or moving real-duration tests outside the measured suite.
The subsequent complete-repository race/coverage run passed with engine package
time 344.493s and 94.9% coverage. All changed production coverage blocks were
executed, and the new clock module was covered completely. The earlier baseline
printed 95.0% but did not retain a block profile; its 0.1 percentage-point
difference cannot be attributed to a particular pre-existing branch from those
rounded totals alone.
