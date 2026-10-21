# Working on this repository

Rules for anyone changing this code, human or AI. Read [ARCHITECTURE.md](ARCHITECTURE.md)
first for what the modules are; this file is about how to change them.

## Commands

```bash
go run ./cmd/zwai desktop          # the app, native window (builds frontend/dist if sources changed)
go run ./cmd/zwai web --mock       # the app in a browser, no model needed
make test                          # go test -race -cover -timeout 20m ./...  + front-end unit tests + phone tests
make e2e                           # Playwright, on the offline provider
make frontend                      # rebuild frontend/dist if the TypeScript sources changed
make build                         # ./bin/zwai (rebuilds the bundle first)
make check                         # formatting + vet + test + e2e
make fmt                           # gofmt the tree; `make fmt-check` only reports
cd mobile && npm test              # Capacitor phone unit tests (includes native iOS/Android trees)
cd mobile && npm run e2e           # phone scan/paste Playwright
make mobile-sync                   # rebuild the phone web bundle and copy into iOS/Android
make mobile-ios                    # open Xcode
make mobile-android                # open Android Studio
make mobile-android-release        # signed (or ANDROID_UNSIGNED=1) APK/AAB into bin/
make release VERSION=x.y.z         # only pending required Mac/Android/iOS platforms; independent gates
make release-check VERSION=x.y.z   # read-only source/signing/remote preflight
python3 tools/audit_macos_acceptance.py --observation <private.json> # offline native recovery decision
make mobile-ios-release VERSION=x.y.z # resume only this TestFlight batch
go run ./mobile/scripts/genicons.go  # paint appicon.png into iOS/Android launcher slots
```

Before finishing any change: **it compiles, the tests pass, and the docs match.**
Touched Go code → `go test -race -timeout 20m ./...`. Touched `frontend/src` → `npm run lint`
(a `tsc` type check), `npm test`. `go run ./cmd/zwai desktop` (and `web`)
rebuild `frontend/dist` when the sources changed, so a local run does not
need a separate `make frontend`. `make build` still generates first so the
embed inside the binary is current. Touched `mobile/` → `cd mobile && npm test`. Native iOS/Android trees under
`mobile/ios` and `mobile/android` stay in git (Pods/.gradle/build stay out).
Do not `cap add` those platforms again. Touched anything user-visible → the
E2E suite.

## Non-negotiables

1. **Nothing hardcoded.** No endpoint, model name, port, path or provider label in
   Go or TypeScript. It goes in `internal/config` with a default, and it is
   editable in Settings. `OPENAI_*` variables may only *seed blank fields* on
   first run.
2. **A user's example is an input, never a rule.** When someone illustrates a
   request with "e.g. read notes.md and make a table", that phrasing must not end
   up in a prompt, a config default, a business rule or a test expectation. Prompts
   stay task-agnostic; tests assert on structure, not on a sample's words. Add a
   negative assertion when a leak is plausible — `TestManagerPromptIsGenericAndGrounded`
   is the pattern.
3. **The workspace is an anchor, not a sandbox.** Agents run with full access, by
   design. Do not describe the workspace as a sandbox in code, comments, docs or
   UI copy, and do not add a half-measure that implies confinement it does not
   provide. HTTP-facing path handling is different: those *must* reject traversal,
   because that is an untrusted input rather than an agent's own decision.
4. **Same-origin only.** No CORS headers, no auth-free non-loopback default. This
   server holds every conversation and can run commands.
5. **No file over 1000 lines**, Go or TypeScript. Split by responsibility when a
   file approaches it — the existing split of `internal/server` and
   `internal/engine` shows the intended grain.
6. **Docs are part of the change.** Code without documentation does not ship. See
   below.

## Testing rules

- Fork-context role isolation must be tested at actual worker model input through
  both Registry and manager-tool paths. `spawned.Text` omits the separately
  injected conversation seed, so it cannot prove manager System/personality did
  not leak. Preserve historical task evidence and same-role resume behavior.

- New or changed Go functions need unit tests in the same package, covering more
  than 90% of the new statements. Current per-package coverage is listed in
  [docs/TESTING.md](docs/TESTING.md); do not regress it.
- A pre-model validation test must run through the actual Registry/ADK/HTTP
  adapter boundary and thread each middleware's returned state. A manual hook
  invocation cannot prove that an upstream request was prevented.
- Selected filesystem paths have identity. Regress save and runtime resolution
  with trailing whitespace, a trimmed sibling, and deletion after selection;
  never silently redirect a missing selection to another existing directory.
- Image acceptance must use a decodable fixture and verify dimensions, MIME
  and unchanged bytes after save/reload. Magic bytes and an img src alone
  cannot prove that the selected image reached the visual-input path.
- Run with `-race`. The engine, the event bus and the HTTP server run
  concurrently, and every serious bug found here so far has been an ordering bug.
- Engine replacement follows `C-027`: freeze new work, durably checkpoint every
  active or parked registry, then stop and restore within one launch deadline.
  Regress real process replacement, original turn/worker identities, human
  pauses and Stop, storage failure, and tool results arriving after shutdown.
  Unknown external effects require verification before retry; a model summary
  or process launch is not proof of handoff or recovery.
- A cleanup event is not a worker completion receipt. Regress Stop and replay
  with unfinished workers shown as stopped, actual finished workers retained,
  and no model-failure retry banner for human interruption. Cleanup does not
  join worker goroutines: include their actual cancellation `finished` receipts
  before cleanup, after cleanup and after the manager terminal, through live,
  cold and paged replay on both PC and phone. Bind cancellation to the current
  worker ID and a nonempty engine cancellation cause; ordinary errors containing
  cancellation words, wrong IDs and watchdog failures remain failures. A fixture
  without these finish receipts cannot accept Stop/reload behavior. Serialize restart
  ownership against the whole normal finalization, including worker parking
  and durable terminal writes. Regress a deadline while waiting for that
  ownership: no partial abandonment, premature ready or lost parked worker.
  Handoff cancellation cannot become a human-cancelled terminal event.
- If something cannot be tested, split it until the untestable part is a two-line
  wrapper: `startDesktopServer` vs `runDesktop`, `serveWeb` vs `runWeb`,
  `buildTUISwarm` vs `runTUI`. Do not leave logic inside a function that no test
  can reach.
- Name tests after what would break for the user
  (`TestSlowSubscriberDoesNotBlockTheRun`), and comment *why the behaviour
  matters* so a later reader can tell a regression from an obsolete expectation.
- A simulator build or app launch does not accept a changed native UI flow.
  Run its packaged-app XCTest and inspect the reached screen; iOS menu changes
  use `BindFlowTests/testNativeIOSMenuHidesAndroidUpdateCheck`. XCTest can drive
  the simulator while the host desktop session is locked.
- A Mac Dock icon does not prove the extracted installer has a Finder icon.
  When changing desktop packaging, check the ZIP's `Info.plist` icon reference
  and `Contents/Resources/AppIcon.icns`, then inspect the extracted `.app` in
  Finder and launch that same package. A running process alone is insufficient.
- For native Mac acceptance, a locked host does not block a ready UTM guest.
  On resuming pending acceptance after a user unlock, a session change, or a
  VM/control-channel change, start the guest observation as `unknown` and
  recheck the actual guest before choosing the next action. Keep earlier
  unreachable/login results as history; they cannot describe this recovery's
  state or justify a blocker when the host locks again. After observing the
  guest, record only the current recovery's login attempt as the active result.
  A locked guest with an existing authorized account from the UTM skill/private
  materials needs an actual login attempt before declaring a blocker. Record
  the target and result without credentials; only a failed guest unlock with
  evidence is an unlock blocker. `tools/audit_macos_acceptance.py` only decides
  the next action from observations, never logs in. A passed observation needs
  real behavior/window evidence bound to the same version, source SHA, ZIP name
  and SHA-256. SSH/process success and the audit's exit code are not acceptance.
- Desktop install exclusion belongs to the application bundle, not an engine or
  data directory. Regress two Service instances and two real processes
  updating the same bundle, launch failure/rollback, independent bundles and
  process exit. Keep the lock identity stable while the app is replaced. A
  delayed cleanup must own a unique old-bundle path, never a later install's
  shared `.previous` path. Single-Service concurrency alone is insufficient.
- A context-budget fix must test replayed messages without billed usage, with
  mixed ASCII/CJK tool results, message framing, and the configured output cap. A synthetic
  billed-token boundary alone cannot prove a long scheduled conversation will
  compact before the provider rejects it. Recheck the actual scheduled turn
  and desktop error path before closing a context-overflow Issue.
- Model error classification must retain model-call versus tool-decoder origin. Regress empty/HTML API bodies and malformed successful-stream field types through the actual offline HTTP adapter, not only error strings. Observe transient reconnect transitions by controlling the fixture handshake; a polling interval may miss the whole transition.
- Restored worker identities are runtime state, never fabricated assistant tool
  calls or successful spawn receipts. Preserve each worker's ID, role and current
  status across goals, schedules, compaction, model retries and restart. Regress a
  large finished roster through those paths and a real loopback model adapter;
  assert that the next turn has no new spawn when none occurred. Session
  briefings and memory review must use turn-attributed lifecycle evidence over
  assistant prose, including a false creation claim with zero lifecycle events.
  Keep generated evidence in model state, storage and diagnostics; a compact
  briefing shown to the user contains prose, not the internal evidence prefix.
  Regress both manual and automatic compact payloads without discarding stored facts.
  Do not repair this feedback loop by matching a user's words or business data.
- Never test against a network or a real model. Use the scripted offline
  provider, and keep its script generic.
- Memory quality tests must not treat prior notes, old reviewer output, or model
  agreement as truth. Freeze source evidence and follow-up checks before comparing
  final facts, missed explicit corrections, conflicts and task behavior. Optional
  knowledge need not be stored. Distinguish source/number checks from semantic
  truth, and paired/shared outputs from independent runs.
- Review note callbacks on a NotesDraft are proposals until commit. Regress
  failure/conflict without partial notes, and actual skill changes surviving a
  later batch refusal; skills are not part of a cross-file notes transaction.
- Post-turn memory review must bind an immutable snapshot to the finishing
  path's terminal status. A database finish does not update the caller's running
  turn object. Regress that real boundary, notes-only failed-review tools and no
  skill folding; a fixture already initialized as failed is insufficient.
- Every feature must be reachable through the one-id troubleshooting path: a turn
  id in, the whole run out (`zwai trace <id>`, `GET /api/trace/:turn`, the Trace
  tab's Full log). A new event kind that is not recorded there is not finished.

## Front-end rules

- Colours and spacing come from the shadcn CSS variables in `src/index.css`. No
  hex literals, no one-off utility soup. If a token is missing, add the token.
- Compose from `src/components/ui/*` (shadcn) rather than hand-rolling a control,
  so light/dark and focus states stay consistent.
- Keep state logic out of components: the store is `src/store/app.ts`, the pure
  stream-to-blocks reducer is `src/lib/transcript.ts`, and the reducer is where
  the tests are.
- Labels must be associated with their inputs (`id`/`aria-label`). Playwright's
  `getByLabel` failing is usually a real accessibility bug, not a test problem.
- On Android, only a phone root screen may let system
  Back finish the Activity. A visible overlay owns the first Back press through
  `installAndroidBack`; add a regression test that the overlay closes and the
  next Back reaches root. The same rule covers locally owned Clients views.
- `frontend/dist` is a Vite artefact and is gitignored. `go run ./cmd/zwai
  desktop` (and `web`) rebuild it when the TypeScript sources changed
  (`frontend.Ensure`, `//go:generate` in `frontend/embed.go`). A shipped
  binary without a checkout serves the embed. A sentinel `dist/.gitkeep`
  stays in git so `go:embed` still compiles before the first build.

## Backend rules

- A human prohibition on parking the current conversation is persisted state,
  attributed to that human message's seq and exact source text. Extract its
  meaning through the read-only policy model, never task-word matching or
  manager-owned arguments. `schedule_wake` and thread `report_schedule` with
  `next_in_s` must check it at the tool boundary and again in the mutation
  transaction. Denied or unclassified sources cannot arm/update a wait or emit
  an armed event. Only a later explicit human allowance can release a
  prohibition; a scheduled cue, summary, skill, peer message or manager output
  cannot. Regress the actual offline tool graph, unchanged schedule row,
  source-attributed Trace, compaction, restart, and a later human allowance.
  Keep verification lazy, bounded and cancellable; persisted successes may
  continue across turns, but a failed source cannot cause repeated same-turn
  classifier requests. Runtime System context carries a bounded quote and seq;
  full source text belongs in the refusal Trace, not repeated compact replay.
  Reports without `next_in_s`, including `keep:false` cancellation, remain
  usable; adding `next_in_s` cannot hide a rearm inside a cancellation.

- A scheduled check ending is not its standing goal completing. Enforce the
  current manager-owned thread wake versus `complete_goal` conflict at the
  tool boundary, including the reverse call order and parallel calls; prompt
  wording alone is not a guard. Completion state and its Trace event must
  commit together. Preserve legitimate independent/human waits and the final
  check's ability to finish; never infer goal completion from report prose.

- A worker watchdog bounds one lifetime, not unlimited recovery. Preserve its
  consecutive failures across resume, same-role spawn, archive and restoration.
  Three failures stop manager recovery and pause only its recurring check.
  Provider errors/cancellation do not count; success or explicit human input /
  goal Start reset it. Model recovery and `reopen_goal` cannot reset it. Regress
  the tool graph, Run now, automatic fires, persisted Trace and restart.

- Independent task messages are peer data, never human steering or authorization.
  Persist their origin and inbox before reporting delivery; a queued notice
  must not start an idle task, bypass an ask/cap pause or interrupt its work.
  Late-steer continuation accepts only User-role messages. Regress late
  completion/cancellation, restart read cursors and storage failure.

- Scheduled completion needs a tool result or explicit report in the same turn.
  Prose alone is not evidence that the check ran. Keep one bounded same-turn
  continuation and regress both Run now and automatic fires, including quiet
  reports and model-retry cleanup; never classify completion by answer length.

- Lifecycle argument errors are recoverable feedback. Do not let one model reply flood history with repeated rejected spawns: validate complete streamed arguments, keep a counted representative refusal and preserve every valid call. Refused spawns are not worker creation or cleanup; regress trace pairing and same-turn repair.

- An HTTP send acknowledgement can arrive after the same turn finished. Keep a received terminal event authoritative, and never let a late acknowledgement mark another selected conversation running. Regress both fast success/error and navigation while awaiting the acknowledgement.
- One turn per conversation. Concurrency is `ErrBusy` (`409`), and the UI turns a
  second Enter into a follow-up instead of a second turn. ⌘Enter steers the
  live turn.
- Persist completed events, broadcast streamed deltas. Deltas carry the full text
  so far, so a lost one costs nothing; storing them would store the answer once
  per token.
- Assign event sequence numbers and broadcast under the same lock. Stored order
  and streamed order diverging is unrecoverable for a replaying client.
- Never drop a stored event because a subscriber is slow: mark it lagged and let
  the connection catch up from the database. The dropped event is often the one
  that says the turn finished.
- Errors get mapped by `server.fail`: `store.ErrNotFound` → `404`, `ErrBusy` /
  `ErrIdle` → `409` with a `code`. Add to that map rather than writing statuses
  inline in a handler. A tool result must not be that sentinel's text.
  `schedule_wake` does not take an id: an optional id is a name the model
  invents when no wait is listed. A leftover id that matches no row follows
  the omit-id path.
- The event kind strings travel over the wire and sit in the database. Renaming
  one breaks replay of existing conversations; `TestNotifyKindsAreStableAcrossTheWire`
  is there to make that a deliberate act.
- A phone inbox poll is another first page. Rows already loaded with `more`
  stay; a row that left page one does not. Replacing the whole list from
  page one puts More back and hides what was just expanded.

## Comments and naming

Comment what the code cannot say: a constraint, a race, a decision that looks
wrong until you know why. Do not narrate the next line, do not explain that a
change is correct, do not leave a note addressed to a reviewer. Name things after
what they do for the user, not after the pattern they implement.

## Documentation duties

Any change that affects behaviour, an interface, configuration or usage must
update, in the same change:

| you changed | update |
|---|---|
| an endpoint or its payload | [docs/API.md](docs/API.md) |
| a CLI flag or subcommand | [docs/CLI.md](docs/CLI.md) and the `usage()` text |
| a config key or default | [docs/CONFIG.md](docs/CONFIG.md) |
| the schema | [docs/DATA_MODEL.md](docs/DATA_MODEL.md) |
| how tests run, or coverage | [docs/TESTING.md](docs/TESTING.md) |
| a module boundary or a data flow | [ARCHITECTURE.md](ARCHITECTURE.md) |
| the swarm library's API | [docs/LIBRARY.md](docs/LIBRARY.md) |
| how the app is used | [README.md](README.md), including the examples |
| anything | [CHANGELOG.md](CHANGELOG.md) — Added / Changed / Fixed |

If documentation and code disagree, fixing the disagreement comes before any new
feature.

## Repository conventions

- Before editing, identify the task branch and worktree. Keep independent tasks
  in separate worktrees and record their owned files. Finish accepted work as
  coherent commits; list any remaining work and its owner explicitly. Never
  bundle another task's unfinished changes merely to clear a dirty checkout.

- Commit in coherent slices with a scope: `feat(engine): …`, `fix(server): …`,
  `docs: …`. A commit should build and pass tests on its own.
- `frontend/dist` is gitignored (except `dist/.gitkeep`). Build artefacts
  (`bin/`, `*.tsbuildinfo`, `test-results/`) and anything under a data
  directory are not tracked. `go run` rebuilds dist when sources changed;
  `make build` generates first so the embed matches.
- Frontend and mobile lockfiles resolve against `registry.npmjs.org`. A
  mirror in `~/.npmrc` used to write those hosts into `package-lock.json`;
  npm 12 then refuses the install on a machine whose registry does not
  match (`EALLOWREMOTE`). Project `.npmrc` pins the public registry so the
  local mirror cannot leak into the lockfile. Do not commit mirror
  `resolved` URLs.
- The data directory is `~/.zwai-swarm` (`ZWAI_HOME` overrides). Never write to
  `~/.zwai`; that belongs to another project. Tests always use `t.TempDir()`.
- `examples/` must keep compiling: it is the library's public surface.

## Serial Issue integration and release

- Finish and push each Issue as its own coherent commits, then integrate its
  complete branch into local `main`, validate that candidate, and push `main`.
  Before the next Issue, fetch and synchronize this `main`; create or update
  the next task worktree from it. Do not keep independent tasks on stale bases.
- Unrelated edits in another checkout do not block task commits or packaging.
  Preserve that checkout's files, index and branch. If it cannot safely host
  `main` integration, use a dedicated clean checkout of the same remote with
  its own local `main`, and record its path and SHA in the Issue. Never stash,
  reset, overwrite or include another task's unfinished files.
- Package only the complete, clean, verified and pushed main commit, with tags
  and platform versions tied to it. A real merge conflict or failed gate must
  be resolved or reported; a dirty unrelated checkout alone is not a failure.
- Track Android, macOS, iOS and server delivery separately. Report a failed
  platform immediately and continue other verified, independent platforms.
  Close the Issue once its behavior acceptance, required tests, documentation
  and review pass and the complete fix is integrated and pushed to main.
  Record published and pending platforms accurately in the closing comment.
  Release thresholds, missing new versions, TestFlight limits/review and
  isolated publication failures do not keep an accepted fix open. Continue
  pending delivery in the release ledger after closure; closure never means
  that an unshipped platform has shipped.
- Issues have separate commits, but releases are batched: integrate each
  completed Issue into main in order, then allocate one version and publish
  once from the final batch commit. Ten Issues still mean one version/tag;
  each platform shares that source batch identity. TestFlight routine archives
  retain their last successfully published marketing version and advance only
  the package-derived build number; `IOS_RELEASE_CONFIG.marketing_version` may
  deliberately start a new version for new candidates. Existing IPA/source/state
  receipts freeze the original iOS version on recovery. Same-version builds may
  still need Apple review. Do not upload intermediate snapshots.
- Before allocating a new version, enumerate this run's owned Issue/worktree
  workset. Finish each actionable fix, review and integrate it into pushed main
  serially. Explicitly defer genuinely blocked or ambiguous work; do not wait
  for unrelated or other-owned open Issues. Only then re-audit the final main
  and allocate one version. Before publication, record
  `pending_new_batch.finalized_workset` with that source SHA, the full
  accepted Issue set and required platforms. An
  active Issue or mismatched/missing workset receipt blocks new publication.
  Different worktrees cannot split one run's desktop and phone changes into
  separate versions. The receipt is a release gate, not permission to change
  the existing commit-count/time threshold or an already public version/SHA.
- For unallocated accepted changes, allocate one new version when more than
  three first-parent commits have landed on main since the last published source SHA, or
  the oldest pending accepted change has waited more than 24 hours. Before
  either trigger, keep the changes pending without a new version/tag. Count
  across runs and calendar days. An already public version/SHA is frozen;
  resume only its unfinished platforms without another version.
- Publish only platforms whose behavior changed in that batch: desktop needs
  the Mac installer; mobile needs Android and iOS. A desktop-only version does
  not change the mobile package/build number or upload phone installers.
  Platform-specific gates remain independent. A Release may contain only one
  installer type, so each updater must find the newest Release containing its
  own safe asset rather than assuming `/releases/latest` has every platform.
- Count pending releases from the delivery ledgers, including closed Issues;
  never intersect that set with open Issues or erase pending iOS work on closure.
  `python3 tools/audit_pending_batch.py --state <delivery-state.json>` checks
  integrated fix SHAs and batch qualification without publishing anything.
  `make docs-check` includes regressions for this accounting rule.
- Retain an authorized application-wide iOS export-compliance declaration in
  `mobile/ios/App/App/Info.plist`. The user confirmed the existing `false`
  classification for unchanged encryption usage on 2026-10-08; routine version
  changes must not prompt again. Review changes to encryption use, implementation
  and dependencies before retaining or changing that declaration. The release
  preflight reads the source boolean and verifies the signed IPA retains it.
  Old frozen sources without the field still use an exact source SHA/build
  confirmation in the private TestFlight ledger. Read that receipt on recovery;
  do not ask again after a signing-config switch or copy a per-build answer to
  another source/build. Conflicting source, IPA or exact records stop publication.

- Reconcile closed and open Issues with both delivery and TestFlight ledgers.
  Keep partially delivered, version-locked Issues for recovery, but exclude
  their changes from the next version's commit/time trigger. An unallocated
  Issue with a missing platform stays pending after closure. The audit requires
  the previous public source SHA and an acceptance timestamp (or verified Git
  commit time) to make the 24-hour rule repeatable.
- Before replacing the current batch with a newer version, retain any older
  public batch with pending platforms in `completed_batches`. Its frozen source
  must remain resumable without rolling back the newest public source SHA.
  Record verified desktop/mobile changes on the complete main even when those
  commits have no Issue row; batch-level platform scope must not change an
  unrelated Issue's required-platform fields.
