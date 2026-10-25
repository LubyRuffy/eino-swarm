# Working on this repository

Rules for anyone changing this code, human or AI. Read [ARCHITECTURE.md](ARCHITECTURE.md)
first for what the modules are; this file is about how to change them.

## Commands

```bash
go run ./cmd/zwai desktop          # the app, native window (builds frontend/dist if sources changed)
go run ./cmd/zwai web --mock       # the app in a browser, no model needed
make test                          # go test -race -cover -timeout 25m ./...  + front-end unit tests + phone tests
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
make native-smoke APP=/absolute/candidate/zwai.app EVIDENCE=/absolute/private/new-round # ci-macOS only, 60s including cleanup
make native-smoke-prepare          # build/identify the native AX helper once, before timed rounds
make mobile-ios-release VERSION=x.y.z # resume only this TestFlight batch
go run ./mobile/scripts/genicons.go  # paint appicon.png into iOS/Android launcher slots
```

Before finishing any change: **it compiles, the tests pass, and the docs match.**
Touched Go code → `go test -race -timeout 25m ./...`. Touched `frontend/src` → `npm run lint`
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
   Publishing an accepted installer must preserve its exact bytes: Android's
   `release_android_validation` binds version/source/name/SHA-256 and skips the
   rebuild. Regress the actual publisher with mismatched/missing receipts and
   files; a repeated build alone cannot inherit native acceptance.

## Testing rules

- Memory reclassification deletes a source only after preserving its full text
  (whitespace normalization is allowed); token overlap cannot prove that negation,
  numbers or conditions survived. Regress partial filesystem deletion and report
  actual writes even when the move fails. Skill usage is separate from content
  identity and never enters the prompt index. E2E language fixtures must restore
  server locale as well as browser storage.

- Concurrent Playwright runs must each own a separate server port, data
  directory and `--output` directory. A Vite preview also owns its built `dist`:
  never rebuild that directory while it is serving a case; concurrent builds
  need separate source/build copies. Preserve the first failure and identify
  whether it came from a business assertion or artifact/teardown handling;
  shared trace/network/ZIP `ENOENT` errors require an isolated rerun with its
  own evidence. A retry turning green alone does not establish the cause or
  accept the original run. See `docs/TESTING.md` for existing CLI options.
- Native desktop acceptance runs only inside the UTM `ci-macOS` guest. The
  host is for building, transferring and collecting evidence; never open or
  focus a test app, Finder, a browser, UTM console or simulator on the user's
  desktop as an acceptance fallback. Leave the user's desktop app, engine,
  cache bundle and data untouched. If guest control is unavailable, retain a
  pending native result instead of switching to the host.
- Guest login recovery is separate from application acceptance. A failed guest
  AX secure-field lookup does not establish that every input path is unavailable.
  When the current user explicitly authorizes direct control of that guest,
  inspect its existing UTM window and try its bound guest keyboard before asking
  the user to unlock it. Keep that authorization and login observation in the
  recovery receipt; it does not authorize a host application acceptance fallback.
- Run native cases one at a time through `tools/native_macos_case.py` in the
  guest (see `docs/TESTING.md`). Each case owns a fresh app copy, home/cache,
  data and workspace; scenario children inherit that private environment.
  On success, failure, timeout or cancellation, collect evidence,
  close that case's app, stop its engine and children, wait for exit, then
  remove its private data. A closed window is not an exited engine. Confirm
  cleanup before the next case; cleanup failure blocks it. Recovery steps
  within one case may retain state, independent cases may not. Never use
  process-name-wide kills or delete another case/user's data. Preserve logs,
  assertions, screenshots and cleanup receipts outside disposable state.
  Renderer fault admission needs the complete fresh process inventory, including
  WebKit name variants and unreadable/departed candidates. A pure selector test
  cannot accept a collector that silently drops them. Persist exact target
  identity before a stall; regress scenario SIGKILL through the runner's actual
  resume/exit cleanup. Corrupt or broken-link receipts must fail closed.
  System WebContent/GPU fault admission verifies only the already-selected
  allowlisted executable: strict code validation, all architectures and inline
  `=anchor apple and identifier "<exact service identifier>"`. Resource omission
  is explicit, never a fallback or App bundle acceptance. Keep raw signature
  stdout/stderr, command, target and status durable on success, failure and
  timeout; unknown results or failed evidence cannot append a target or signal.
  Regress real CLI syntax, wrong identity and a privately corrupted code page.
  Distribution App verification remains deep and strict with resources.
  Regress changed lifecycle adapters through the actual main harness module.
  A fake namespace that invents its missing methods cannot prove module wiring;
  exercise every real caller, including setup and cleanup, before freezing it.
  A long soak needs durable successful UI/identity observations across its whole
  duration with bounded gaps in both wall and monotonic clocks. Elapsed time,
  an overnight VM pause or a final screenshot alone cannot accept that duration.

- Fork-context role isolation must be tested at actual worker model input through
  both Registry and manager-tool paths. `spawned.Text` omits the separately
  injected conversation seed, so it cannot prove manager System/personality did
  not leak. Preserve historical task evidence and same-role resume behavior.

- Daily native regression has a 60-second whole-round success budget once the
  guest and identified candidate are ready. Use `make native-smoke`; retain phase
  and total timings including cleanup. Build/first transfer and deliberately
  long recovery tests have separate measured scope and explicit budgets. Do not
  replace behavior checks with shorter timeouts or abandon cleanup at expiry.
- Native activation is requested once per helper action invocation. Refresh `NSRunningApplication` on the
  main runloop before trusting `active`, even when its cached value is true.
  A nonempty window candidate must have the actual AX element type, `AXWindow`
  role and expected owner PID before raise, traversal or window input; retain
  its strong lifetime through the action. Regress proxy, unknown and wrong-PID
  candidates through both actual helpers, with no window action on refusal.
  Engine presence can precede native application registration. After the kernel
  case guard, only a snapshot may report registration pending for an absent
  `NSRunningApplication` or unavailable bundle path. It performs no activation
  or AX access and does not verify that requested bundle. Keep the existing
  shared window deadline; fill/Send, wrong native PID and a nonempty wrong
  bundle remain immediate refusals. Regress the real main and window caller.
  The resilience helper's application-only activate command does no AX lookup
  or raise; callers still require a fresh real-window and pixel observation.
  Keep `ZWAI_NATIVE_APP` as the original case anchor when a recovery shell
  replaces its PID. Derive the activation bundle from the actual kernel
  executable: accept only that original bundle or this case's exact private
  home/Library/Caches/zwai/desktop-recovery-*/zwai.app layout, with its session
  marker and verified original/derived core identities. Actual `WriteMacApp`
  rewrites metadata and re-signs the copy; equal raw hashes are not a general
  successor contract. For different bytes, prepare a pinned proof independently
  through the real writer on the target signing system, never from the runtime
  target. Strictly verify the original and both derived signatures, compare two
  outputs and every non-signature byte, normalizing only the three measured
  signature/linkedit length fields. Freeze source/version, original/derived
  full hashes and metadata; a present malformed proof never falls back to raw
  equality. Regress the actual writer with real signed page/Info variants,
  damaged code, dirty/untracked writer inputs and the entire real helper main.
  Verify the inherited case marker,
  NS PID/resolved bundle and unchanged kernel birth/executable around the one
  request. Hashing shares the existing two-second deadline. Regress the actual
  helper main against real private files; wrong layout/core/NS identity or PID
  reuse must refuse without AX/input. Record sampled identities and reason,
  never credentials. A QA identity fix does not accept product recovery timing.
  One monotonic deadline covers activation, raise and short runloop slices;
  expiration cannot allocate another wait or retry Send. Regress actual
  Objective-C callers with OS/clock seams, including stale true/false and an
  activation that consumes the deadline. Keep real SDK/main-entry refusal checks
  separate from those seams and from guest window/collector acceptance. Failed
  activation and fill/Send guards retain their sampled predicates and Press
  attempt state; a caller that drops stdout must still retain key failure fields.
  Existing caller polling keeps its original shared deadline; this helper wait
  cannot reset the replacement budget.
- Native file-selection diagnostics retain the first choose-file timeout's raw
  streams and original exception before final cleanup. Later snapshot failures
  cannot overwrite them. Preserve completed helper streams before parsing;
  missing success evidence fails the case, while evidence I/O cannot replace an
  existing action failure. Keep the eight-second adapter deadline and never
  repeat an input to repair a missing acknowledgement.
  File-panel inspection requests Role/Title/Description/Value/Children in one
  fixed-order, options-zero batch per distinct scanned node. Validate the top
  status and array type/exact length. Decode every returned slot's error-wrapper
  structure, including unused prefetched auxiliary slots; decode only correctly
  typed AXValue AXError wrappers, never negative numbers. CFNull is a separate
  batch-missing result, not an invented NoValue error: Role missing refuses,
  Children missing is a leaf and Button Title missing creates no Open candidate,
  preserving the existing explicit Unsupported/NoValue boundaries. Malformed
  error wrappers and incomplete required reads refuse; never fall back to
  individual reads. Roots keep their complete independent enumeration.
  A filename witness stops auxiliary text consumption, although fixed batches
  still request those attributes. Continue proving one distinct Open across the
  complete supplied AX graph. Roots, role/children and every button title keep
  their required read and type checks; unreadable, malformed or over-budget
  graphs cannot prove uniqueness. Consumed auxiliary text errors and non-string
  values are unknown,
  never absence or a filename witness. Retain their original status and cached
  node context; only another successful string witness plus the complete unique
  Open proof permits Press. Regress partial batch errors, CFNull and malformed
  wrappers, unknowns before/after a witness, a hidden second Open, missing
  witnesses and required failures through the actual caller.
  Bound auxiliary error records without dropping a later required failure;
  Query-count savings do not prove provider-internal IPC count, atomic snapshots,
  native elapsed time or a past timeout cause. The full graph remains O(N).
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
- Lifecycle-only schedule fixtures use explicit titles. Before fault injection
  replaces provider state, registers/removes database callbacks or closes storage,
  unrelated background title tasks must be absent or joined. Keep automatic-title
  tests on the real untitled schedule API and assert lifecycle helper waits have
  `TitleAuto=false`; a repeated green race run alone does not prove isolation.
  A one-shot GORM callback on a private test Store stays until Store.Close unless
  every querying actor is joined. A terminal row or idle notification alone does
  not join turn finalization and cannot authorize removing its callback.
  Direct manager fixtures must register the same turn and Registry they execute;
  an existing different owner must fail before model input, never remain a no-op.
- Run with `-race`. The engine, the event bus and the HTTP server run
  concurrently, and every serious bug found here so far has been an ordering bug.
- Local startup must not wait for optional phone-hub registration. Keep remote
  startup cancellable and serialize Stop/Reload; a late registration must not
  revive a stopped host. Keep orphaned-turn/parked-worker restoration ordered
  before scheduling rather than moving it into an unowned goroutine.
  Regress a real child taking longer than 15s to publish its lease, blocked
  registration with a responsive local API, current-attempt logs and exit
  status, and cancellation that reaps only the launcher's own child. The `C-027` 30s handoff/listen target excludes restoration. After irreversible
  ACK and stopping the old engine, a launch deadline cannot kill the sole
  replacement; explicit human cancellation still reaps only its owned child.
  Cancel during successful and failed metadata probes; neither a late ready URL
  nor a discovery error may override the caller's cancellation.
- A reported checkout-launch failure needs the reported command exercised in
  `ci-macOS` from an identified copy of that checkout's exact source and dirty
  content, with current build provenance and unrelated edits preserved. Keep
  the original host checkout's delivery checks read-only. Regress empty and
  partial `node_modules` newer than the manifests, including missing development
  build tools. Observe the guest launch's native window and an offline turn.
  A different source snapshot, prebuilt embed or separately launched `.app`
  cannot accept the original checkout command. Use the isolated case lifecycle
  above; this requirement never authorizes launching the host GUI.
  Check declared direct/development packages and executable entries before a
  rebuild, revalidate after installation, and do not stamp an incomplete build.
  Serialize install/build/stamping for one checkout across processes; regress
  cancellation while waiting and staleness checked again after locking.
  Regress real npm-to-Node descendants: cancellation or script exit must stop
  old writers before another build acquires the lock; retain the context cause.

- A worker model output can outlive several manager checks. Preserve its
  `message_id` through live deltas and durable checkpoints; regress one real
  worker stream across those boundaries, replay and a later distinct output
  with identical text. History pages, agent logs and SSE must apply each
  stored `(thread_id, seq)` once before state changes or side effects. Do not
  use text similarity or a highest-seq cutoff as identity. Regress both arrival
  orders and rewind across multiple checkpoints of one output.
- A model generation and its worker have separate lifetimes. Persist each interrupted
  output identity, recorded cause and actual recovery state; close its old thinking
  stream through live, cold and paged replay. Restart/handoff recovery must not
  fabricate `finished` or mark a recovering worker failed. Unknown process-exit
  causes stay unknown; a manager check ending is not a worker interruption.
  Regress partial thoughts/tools, Stop, output limits, transport errors, both
  history/live arrival orders and same-ID recovery at the real model boundary.
  Preserve an observed model error or finish reason if Stop arrives later;
  Stop changes recovery availability, not the earlier failure's cause. Runtime
  abandonment alone is not proof of a planned restart. A cold-start checkpoint
  must follow this generation's start before it can establish handoff intent.
  Regress malformed partial tool arguments against the actual executor, not
  only tool notifications: ADK may consume its stream before the UI does.
  Generation caches derive only from durable events. Conversation/project
  deletion and callback writes must share ownership ordering; a late callback
  cannot recreate deleted conversation state or append an orphaned receipt.
- A long-running worker belongs to its executable goal or active same-thread
  wake, not to one manager check's terminal status. A failed scheduled check
  must retain that ownership durably before its terminal error; the failed
  turn/run and unread result stay failed. Never infer retention from error
  wording or invent success. Regress real worker HTTP requests across failed
  checks, live/history/roster replay, restart, same-ID collection, Stop and
  revocation of the last owner. Keep resource-budget and watchdog termination
  separate, and do not turn a failed check into an immediate goal retry.
- An ordinary turn without executable goal/wake ownership cannot finish with
  running workers or finished results it has not collected. Bind actual
  `wait_agents` results and explicit `close_agent` decisions to the current
  worker generation; timeout, unknown IDs and old-generation receipts do not
  settle it. Closing an already-finished worker may discard its result but
  must retain `already_finished`, never fabricate cancellation. Withhold early
  completion prose, allow one durably counted correction, then keep the same
  turn/workers paused for human Continue or Stop. Continue grants one further
  attempt; restart preserves both collection state and the consumed allowance.
  Regress actual HTTP workers, completed-uncollected results, resume, archive,
  restart, owner revocation, storage failure and live/history/Trace output.
- A parked registry's conversation-wide cleanup recording/publication attempt
  must finish before another registry can be taken or occupied. Serialize that
  attempt with the runtime claim; cancel/close the detached registry outside
  the runtime lock. Existing best-effort event writes WARN on storage failure;
  do not claim a durable cleanup receipt then. Regress Stop, last-wake removal
  and finalization with storage blocked before cleanup so an old receipt
  cannot mark a new worker stopped.
- Engine replacement follows `C-027`: freeze new work, durably checkpoint every
  active or parked registry, then stop and publish the replacement listener within
  the handoff target. Serve `restoring` before owned, cancellable recovery; count
  recovery in ActiveWork, reject new mutations on HTTP and phone RPC, and start
  scheduling only afterwards. Do not terminate the sole replacement on the old
  launch deadline after acknowledged handoff. Regress restoration exceeding 30s,
  durable Stop before runtime occupation, a shared per-conversation snapshot and
  one indexed checkpoint read; Trace diagnostics cannot claim restored on error.
  Regress real process replacement, original turn/worker identities, human
  pauses and Stop, storage failure, and tool results arriving after shutdown.
  Unknown external effects require verification before retry; a model summary
  or process launch is not proof of handoff or recovery.
- Unanswered ask recovery must cross the real Registry middleware and HTTP/
  process replacement boundary. A direct `AskUserTool` call misses handoff's
  synthetic unknown result. Recover a legacy placeholder only from the matching
  unaccepted human-pause checkpoint; real answers win in either arrival order.
  Preserve the original turn/call identity, raw unknown receipt and private
  checkpoint. Test named `ask_recovered` delivery, PC/phone replay and duplicate
  answer refusal; a restored form is not a new call or an accepted answer.
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
- A fix for a reported checkout launch must reach that exact checkout. Record
  its HEAD and dirty-file hashes, preserve unrelated edits, and verify the fix
  commit is in that checkout's HEAD. Verify its source and served assets
  read-only. Native behavior runs from a source- and dirty-content-identified
  copy in `ci-macOS`, never the user's host GUI. Record that scope explicitly:
  a guest copy cannot prove that the host's already-running binary changed.
  Another worktree or a remote commit is not delivery.
  `tools/check_checkout_delivery.py` checks source ancestry and optional
  loopback asset hashes; it cannot accept native behavior.

- Desktop launch must hold acknowledged native-owned presence before opening
  the WebView. Regress reopening at idle shutdown, page reload, failed launch
  and idempotent release; a meta probe or waiting for the old process to exit
  cannot prove this boundary. Serialize final idle shutdown with new presence
  admission instead of increasing the grace or creating a second writer.
  Regress a replacement lease with a new PID at the same URL; keep the validated
  process/start/build identity instead of treating a port as its owner.
  Final idle admission must use the same ActiveWork count as the watcher,
  including parked workers and restart handoff, not only running managers.
- Regress loss of an already acknowledged desktop presence stream over real
  HTTP. Recovery only reserves and acknowledges on the same engine URL, has a
  bounded consecutive budget, and is interrupted by native shutdown. Short
  acknowledgements cannot renew that budget; a stable hold can. The handshake
  deadline must not later cancel a healthy stream. After startup, exhaustion
  synchronously records its cause through the configured logger before native
  Quit, which may terminate without returning to Go. Preserve the typed cause
  when Run does return; an early failure returns without opening a window.
  Regress a non-returning Quit in a subprocess and keep normal release silent.
  Never start another engine or hand ownership back to the frontend.
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
- Native appearance tests must check the captured target image against light,
  dark and blank-image regressions; an appearance getter or delay never replaces
  visible content, actual user actions and clipboard evidence.
- A Mac Dock icon does not prove the extracted installer has a Finder icon.
  When changing desktop packaging, check the ZIP's `Info.plist` icon reference
  and `Contents/Resources/AppIcon.icns`, then inspect the extracted `.app` in
  Finder and launch that same package. A running process alone is insufficient.
- Native QA launchers must enforce their private data directory themselves,
  including a parameter-free LaunchServices launch. Attaching through app
  automation can launch another instance without the original command args;
  an already-running isolated PID does not make that safe. Validate the actual
  launch entry with no args and an inherited default-data environment before
  attaching. Never overwrite or re-sign a bundle used by a live engine.
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
  Execute the reported next action during the current recovery. A failed
  control channel alone is not a reason to leave authorized acceptance waiting:
  try another available isolated native control path and record its actual
  result. If an SSH-launched helper is denied native permissions while an
  existing authorized guest Terminal control path is evidenced as usable,
  execute that path before asking the user to change settings. Keep the
  native-case lock, exact target/candidate and fresh helper receipts; guest
  administrator status does not establish Accessibility or Screen Recording
  trust. Routine recovery already authorized by the user needs no second
  approval. If the guest relocks during model/tool handoffs, chain the guest
  input actor's exit/cleanup, a fresh session check and guarded case launch in
  one bounded invocation. Keep finite power ownership continuous through that
  handoff: register any bridge in the original case's process list under its
  held exclusion lock and incomplete-cleanup latch, before recovery or launch.
  Release/reap it only after the actual scenario child's fresh same-birth three
  assertions acknowledge takeover, or through original failure cleanup. Regress
  the actual runner's preparation-failure fallback, malformed/partial/wrong-owner
  acknowledgements, cancellation and false cleanup latch; fixture-only methods
  cannot prove this wiring. Keep the existing whole-round/work/cleanup budgets.
  A direct child's PID/birth can precede its expected executable after spawn;
  bind that transition within the same deadline before freezing its full
  identity. Regress a real headless exec, not only a mocked identity reader.
  Preserve executable modes through the entire launch chain, including a
  LaunchServices/Terminal `.command` entry, and check each actual executable
  before triggering that entry. Source read by an explicit interpreter does
  not need an executable bit. Regress unchanged bytes at mode 0644 refusing
  before any trigger and the intended executable mode passing preflight.
  Checking helper binaries alone or matching byte hashes misses this boundary;
  a missing entry claim must not become an unexamined wait for native timeout.
  For the already identified, authorized target, retain the new observation
  frame without adding an approval delay. A wake frame or power handoff is not
  continuing login proof; input still needs the actual guest login state, and
  lock-screen settings stay unchanged.
  A live owned `caffeinate` process, active power assertions or an active App
  do not prove that the guest remains unlocked. Preserve missing lock flags as
  unknown. If a case locks, retain its exact child birth/poll and assertion
  observations plus the bounded session-event timeline before choosing another
  action; a timed assertion is not the default five-second untimed assertion.
  Synthetic input is not physical keyboard/mouse evidence.
  Preserve passed evidence for its exact source and package; a later
  main change requires checking the affected inputs, not erasing that evidence.
- Repeated ci-macOS access failures need fresh route/interface, guest MAC/link/IP/
  default-route and SSH-service evidence. A saved IP, UTM started state or old
  queue ready file cannot establish current access. Send a newly identified
  read-only shared probe before rejecting that channel; use a local bounded wait,
  not host/guest wall-clock subtraction. Do not retry an unsupported UTM backend
  operation as progress. On-demand sshd without a listener is not evidence that
  the service is disabled. Before disruptive VM recovery, verify peer claims and
  the previous native cleanup, then hold the actual native-case exclusion lock
  through the action; acquiring and releasing it during a probe is insufficient.
  Only the exact current queue/request may be exempted from peer checks. Include
  nested queue roots in the inventory; exclude historical controllers and claims
  only after current process-death evidence, while retaining their receipts. Test
  queue requests against the actual protocol, including absolute executables and
  integer timeouts, rather than a permissive subprocess fixture. Keep
  DHCP renewal bounded to one attempt on the identified, already-DHCP interface,
  recheck the result, and retain `needs_host_recovery` when it remains down.
  Access classification and a restart-admission flag do not execute or authorize
  a reboot, and transport success never accepts native application behavior.
  If the identified guest has no carrier/IP/route and the host lacks sharing
  interfaces, inspect NetworkSharing/PF before another VM restart, VPN change or
  reinstall. Bind a persistent `PFUserDeleteRules` synchronous-XPC wait to the
  actual daemon PID; one sample or a pfd exit status is not a crash diagnosis.
  Authorized host maintenance must preserve peer exclusion, recheck any exact
  PID before signalling, and retain the PF filter/NAT/anchor/reference baseline.
  A temporary `pfctl -E` enables existing policy: release only its own reference
  with `-X`, verify other owners and pinned SSH afterward, and never flush/load/
  disable PF or change SIP as cleanup. This is manual evidence-led maintenance,
  not part of the access CLI or permission to add a host watchdog. See the
  NetworkSharing/PF procedure in `docs/TESTING.md`.
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
- Native worker fixtures must distinguish the engine's exact System policy
  protocol from manager and worker requests. Schedule-answer classification is
  non-streaming, tool-free and two-message; only the fixture's own fixed known
  candidates may receive empty claims. Unknown candidates or malformed policy
  requests fail closed. Regress actual loopback HTTP, quoted/user protocol
  spoofing and an already-held worker without changing its spawn or request
  counters; never approve arbitrary prose to make a native case pass.
- Schedule-answer classification must honor the provider's response-header and
  body-idle budgets and turn cancellation, without a shorter hidden total
  deadline. Regress a real response beyond 30 seconds without replaying the
  manager, plus configured header/body timeouts and Stop; a classifier timeout
  alone does not establish that the manager made an unsupported schedule claim.
  Persist the original candidate before classification. Unavailable or malformed
  classifications retry only that candidate with durable capped backoff; they
  cannot consume the manager's correction allowance, end its turn or cancel its
  workers. Stop and a superseding human steer remain effective. Accept public
  events, replay messages and the candidate decision in one transaction; an
  accepted identity must not broadcast twice after restart. Keep private candidate payloads
  out of events, Trace, compaction and ordinary history, and delete private rows
  with their owning thread/project or rewound turns. Clear candidate payloads
  in the terminal turn's transaction, retaining lightweight decisions; a failed
  terminal write must preserve the full recovery state. Regress real HTTP failure,
  recovery, storage refusal, worker ownership and process replacement.
  Running turns retain only their latest answer's full transcript; save and old
  answer compaction must commit together without clearing report receipts.
  Candidate order uses a transaction-owned monotonic ordinal, never wall time
  or random IDs; regress clock rollback, tied timestamps and stale-ID re-saves.
  A report's committed action and actual tool-call receipt must survive findings
  verification without repeating the action. Release the global goal/wake lock
  before requesting a model; a slow classifier cannot block unrelated schedules.
  The schedule action and private report save are separate commits. A failed
  private save must report whether the action committed, without claiming the
  findings were published. A crash between those commits remains unknown and
  requires checking current scheduling state before repeating the action.
- Regress a new explicit goal after a completed one through the real PC and
  paired-phone send paths, including a standalone trailing `/goal`. The
  original objective text must reach the model, and the completed flag must
  be cleared before work starts. A resumed worker or an assistant's promise
  of a future check is not a successful schedule receipt: verify the exact
  turn's persisted schedule and its current state before publishing that
  success claim. Failed, denied, cancelled and historical schedules must not
  become a new armed wait through prose or replay.
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

- An unattended rendering report needs an explicitly input-free observation after
  setup. Periodic composer edits, navigation or sends cannot accept that scope.
  Report the last probe boundary before automatic reload; asynchronous enqueueing
  is not a durable receipt. A white image captured afterward does not establish
  that the original document was already white.
- A renderer stall with a healthy engine is a product recovery failure until
  bounded native recovery is accepted. Preserve process-specific WebContent/GPU
  evidence before restarting; a successful reopen does not prove the trigger
  was fixed. A candidate upstream WebKit patch is not proof of an Apple private
  build's patch contents. Health acceptance needs current challenge pixels from
  the actual WKWebView, not only JavaScript callbacks or `requestAnimationFrame`.
- Unsent text, quotes and attachment bytes must be durably acknowledged before
  destructive queue editing or send-state transitions. A lost send acknowledgment
  preserves an uncertain draft and never authorizes automatic resubmission.
  Regress fixed conversation ownership during navigation, first-send scope
  migration, CAS tombstones and original image/file bytes after renderer recovery.
  Saved inactive composer scopes release attachment caches; retain dirty,
  conflicting and sending state, and pin asynchronous migration/send owners
  across navigation. Regress actual preview release and durable byte reload.
- A new stored event needs its actual named SSE subscription tested end to end.
  Prevent history/Trace reads from hiding a missing live listener; metadata must
  preserve worker/task state and must not strand phone paging on invisible rows.
- Renderer recovery replaces only its owned shell, retains the original engine
  lease, and carries its finite retry budget across generations. Regress sleep,
  inactive windows, cancellation at each handshake phase, budget exhaustion and
  normal Quit/update. Never overwrite or re-sign a live runtime cache bundle.
  Accept the unchanged formal ZIP in ci-macOS with recorded source/package
  identity, actual pixels/actions, retained task IDs and complete case cleanup.

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

- Scheduled checks retain automatic tool selection and may end with a nonempty
  text result without tools. Do not require a tool call to accept that result.
  Only an empty/whitespace final with no actual tool result or explicit report
  gets one bounded same-turn continuation. Regress Run now and automatic fires,
  text-only results, empty recovery, explicit quiet reports and synthetic retry
  cleanup through the real model adapter. Text is a delivered response, not
  proof of external work or standing-goal completion; do not classify its
  meaning by keywords, similarity, language or a length threshold.

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
