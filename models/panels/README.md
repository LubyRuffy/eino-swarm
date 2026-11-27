# Task-panel protocol checks

`make model-check` runs these TLA+ specifications with the pinned TLC version.
Every current case checks its complete safety list. Fault switches restore a
specific suspect behavior; their cases must violate the named invariant.
`Never*` cases must fail to establish that the relevant path is reachable.
`python3 -m unittest tools/test_model_check.py` checks that registration cannot
silently lose a guard, invariant or witness.

| Specification | Finite scope | Code boundary and checked rule |
| --- | --- | --- |
| `PanelInstance.tla` | One binding, one observation and one source | Host visibility depends on committed observations or a running source. The initial snapshot stays hidden. Closing the instance leaves the turn running. |
| `PanelCloseOrdering.tla` | One target and one sibling source, one captured publication, one failed close followed by a retry, one process restart | Store close commits its marker, interrupted status and deletion together. The engine cancels only the target after a successful write and acknowledges after dispatching cancellation. Late new writes cannot commit. Restart retains the marker. |
| `PanelCloseDelivery.tla` | One instance, two visits and one in-flight reply | The PC snapshot and phone manifest/body paths both fence a reply captured before an acknowledged close. A previous visit's acknowledgement cannot change the new visit. |
| `TaskPanel.tla` | One panel, two operation IDs, two digests, one captured request competing with another writer, two browser windows | `PublishTaskPanel`: accepted retries preserve identity, changed bodies conflict, new writes compare their captured base, commit precedes delivery. Its revision counts accepted data publications, abstracting lifecycle-only increments. The connection branch re-finds the observed extra-SSE/two-window draft starvation. |
| `PanelOwnership.tla` | Two publisher identities; one live source; one malformed and one healthy recovery thread | `PublishTaskPanel` refuses a different writer while a source is running. `RecoverTaskPanelSourcesForThread` and `Engine.Restore` isolate failure to its owning conversation. |
| `PanelMCPAdmission.tla` | One frozen role and source invocation, two destination identities, manager or six worker tool scopes | `resolveMCPSurface`, `bindPanelMCP`, `runPanelSource`, `runEASMSource` and the MCP caller: captured bindings do not expand with later settings, restored destinations match, worker execution scope is required, and session/live-switch checks retain their actual sampling points. |
| `EASMCollection.tla` | One panel, three steps (`clouds` shares the `components` abstraction), two queries and at most two serial attempts; separate query/retry and interleaving variants | `easm.Run`: same-query retries keep completed steps; new queries clear old metrics; one complete reply proves a summary; apply, publication, join and failed writes remain separate. The view branch checks terminal source observations with an inactive task. |
| `PanelProjection.tla` | Two panel identities, up to four publications/deletion, one outstanding snapshot and one selection change | `frontend/src/store/task-panels.ts`: per-panel revisions and deletion cursors merge an older GET across newer SSE updates; a new panel must not disappear because another panel advanced; authoritative absence settles a covered observation rather than causing endless GETs; an old visit cannot alter the new visit. |
| `PanelFrameDelivery.tla` | One iframe, two desired revisions, one outstanding callback and a bounded automatic resynchronization | `task-panel-frame.tsx` and `sdk.ts`: sent, applied and acknowledged versions have distinct meanings; hidden failed updates retain a snapshot retry; incompatible code does not run; timeout is visible. |
| `PanelProducer.tla` | Two HTTP targets plus equivalent/duplicate input identities, separate observation/submission/reduction/join stages | `fofaworld.Run` and `startWorkersUsing`: deduplication prevents duplicate requests; cancellation cannot drop an already observed response before the reducer; returning requires the owned collector/workers and preceding receipts to join. |
| `MobileTaskPanel.tla` | Two chunks, two transfers/revisions, three visits including A→B→A | `internal/remote/task_panels.go`, `remote-task-panels.ts` and `use-task-panels.ts`: a transfer stays immutable across manifest refresh, complete validated bodies belong to the current visit, old cleanup cannot close a successor, and chat seq remains independent. |
| `InspectorRegistry.tla` | Two projects, two conversations and a standalone conversation for render eligibility; two instance names, one captured callback and up to six actions for automatic focus | `AppPanel`, `RightPanel`, the work-records tab (`GraphPanel`) and `useRegisteredInspectorAutoOpen`: project and conversation scope, available-tab fallback, retained hidden iframe, and rejection of stale, superseded, deleted or replayed automatic-open signals. The model's `work` tab and its project `mode` / `statusOwner` inputs date from the removed project mode; the registered tab is now bound by the open conversation or, with none, the selected project. |

`models/lifecycle/EngineLifecycle.tla` also includes a panel-recovery refusal
before runtime occupation under the existing restoration/Stop ordering.
`models/stream/EventStream.tla` checks that named panel frames cannot overwrite
the independent chat SSE cursor.

## Counterexamples reproduced on the implementation

The source lease and recovery failures are reproduced by
`internal/store/task_panel_ownership_audit_test.go` and
`internal/engine/task_panel_restore_audit_test.go`. The two frontend ordering
failures are reproduced by `frontend/src/store/task-panel-ordering-audit.test.ts`
and `frontend/src/components/app/task-panel-ordering-audit.test.tsx`.
`result_receipt_order_test.go` gates an actual loopback HTTP probe after the
client observes a 403 and cancels before result delivery. The original worker
select loses that receipt; the fixed terminal send preserves it.

The ordinary package tests also exercise real SQLite transactions, failed
writes, competing revisions, rewind and the model/HTTP tool boundary. These
tests are necessary: a TLC action standing for a transaction does not prove
that the Go implementation or its SQLite adapter is atomic.

## EASM protocol refinements

The admission model re-finds the worker-scope bypass fixed by `b71b592b`.
`TestEASMWorkerSourceUsesItsRegisteredMCPScope` drives the actual worker model,
Registry and counted MCP HTTP server: empty and directory-only tool scopes
must not call the producer's aggregation tools. Other cases remove the captured
configuration, source-policy, destination, session and remote-recheck guards.
Configuration publication, Manager reload, source admission, session acquire,
the second live-switch read and dispatch are distinct actions. A later disable
may occur after the last check; it does not retroactively invalidate that call.

The collection model has two configurations of one specification. Each current
configuration checks the complete safety list; fault and witness cases are
shared across them. `ClaimCompleteOnSettled` and `TrustStoredRunning` re-find the
receipt and reload defects fixed by `49377286`. `MergePartialTotals` is a seeded
cumulative-count mutation, not an exact reconstruction of a historical parser.
The immutable snapshot captured under the producer mutex is separate from the
last committed state and the snapshot observed by the UI. A running step may
already have valid data: another step can publish after its apply but before
its outcome. Collection `done` means settled; a complete receipt additionally
requires every step to have succeeded.

| Model rule | Implementation regression |
| --- | --- |
| Frozen bindings and recovery destination | `TestPanelMCPKeepsInitialSettingsAndDoesNotRestoreToAReplacement`, `TestEASMServerEnabledMidTurnWaitsForTheNextTurn` |
| Calling worker's exact direct tool or same-server call proxy | `TestEASMWorkerSourceUsesItsRegisteredMCPScope` |
| Source policy changed after mount; live disable during discovery | `TestEASMSourceRechecksAccountRestrictionAfterTheTurnMountedMCP`, `TestProducerDisableDuringDiscoveryPreventsToolExecution` |
| Same-query retry, replacement query and one-response totals | `TestRepeatingQueryRetriesOnlyUnfinishedAggregations`, `TestQueryChangesClearAndRerunTheMetrics`, `TestSummaryNeedsBothTotalsInOneResponse` |
| Truthful completion and concurrent progress | `TestReceiptNamesEveryAggregationThatDidNotFinish`, `TestQueryStepsRunConcurrentlyAndPublishEachCompletion` |
| Failed publication and late results after Stop | `TestEASMCollectionStopAndFailedPublicationKeepDurableEvidence` |
| Cancellation waits for a caller that has not returned | `TestCancellationWaitsForStartedAggregationToReturn` |
| Terminal source observation settles loading | `frontend/src/lib/easm/data.test.ts`, `frontend/e2e/easm.spec.ts` |

`FuzzPortraitPublicationKeepsSavedSources` generates bounded portraits and
sequences of edits through the catalog and actual snapshot/patch application.
It checks saved source/target and confirmation retention, legal enrichment and
subject changes, including retained historical detached records. Its seeds
re-find the deletion gap fixed by `0cf63642` and the confirmation downgrade gap,
including an upgrade followed by a refused downgrade.
`TestEASMDirectPublishPreservesSavedFirstSources` checks refusal through real
model tools and SQLite state, revision, mutation and delivery readback.
These data-space properties remain implementation tests rather than another
protocol model.

For implementation calibration, run the unchanged regression first, then use
an isolated Go `-overlay` to remove its named guard or restore the historical
file. The mutant must fail its business assertion; build failures, races and
timeouts are not accepted counterexamples. Retain the exact command, source
hashes, overlay, complete streams and exit status outside the checkout. No
production fault switch or mutable test mode is required.

Mobile's mixed-revision and A→B→A guard mutations run against isolated copies
of the real TypeScript reader/hook. The unchanged control passes; removing each
guard makes its corresponding assertion fail. These calibrate the finite fault
classes without claiming that the final guarded implementation contains those
defects. Real sealed-RPC tests cover immutable large bodies, patch fallback,
scope/token binding and cleanup. Browser tests separately cover the shipped map
with phone touch/rotation and version/error presentation.

The inspector model re-finds the observed unavailable-tab fallback defect and
the cross-project status defect. The real-component regressions that carried
those two findings (a first-DOM-commit ownership test and a missing-tab
fallback test on the actual right panel) belonged to the removed project-mode
panel and were deleted with it; no implementation test replaces them yet.
`inspector-panels.test.tsx` checks the automatic-open guards and that the
work-records tab stays out of the embedded agent plugin catalog, and
`graph-panel.test.tsx` covers the panel's rings, to-do highlighting, subject
filter, credential masking and retry. The render branch enumerates one committed view from independent
inputs; the focus branch models store changes between signal capture and
delivery, not React scheduling.

## Limits

These are finite safety and reachability checks, not an unbounded proof or a
wall-clock termination guarantee. They assume the modeled atomic database and
channel steps and do not establish a remote service's behavior, an OS child's
exit deadline, a browser's scheduling or native WebView presentation.

Producer `applied` means the in-memory reducer accepted an observation. It does
not mean the final observation reached SQLite: Stop cancels publication too,
and a final flush can be refused. The durable source interruption marker then
describes incomplete work without inventing per-asset observations. The
complete rewind reconstruction and JSON Patch value space remain covered by
implementation regressions rather than these finite models. The projection
model does not reopen a deleted ID; implementation tests separately cover an
older GET crossing deletion and same-ID reopening. No liveness
property or native acceptance is claimed by these cases.

The admission model abstracts successful connection/schema validation and
credential/session renewal. The collection model assumes the publisher's
transaction boundary and existing owner/revision guards; it does not duplicate
the general panel protocol. Stop may fail both its producer snapshot and host
terminal write, so it cannot guarantee a durable interruption or immediate
loading change. Manager cancellation is not a source or remote-service join.
The collection model's cancellation action refers to the producer context,
not to acknowledgement of the manager's Stop request.
The HTTP cleanup-failure case observes the host marker failing; it does not
claim that both modeled interruption write paths executed in that scenario.
Successful producer collection also does not guarantee the host's finish write:
the model and HTTP regression reject a successful receipt if that write fails.
The models are checked independently; real HTTP regressions bind their shared
role/source interface to the implementation rather than proving an arbitrary
composition of the two specifications.

## Instance close audit

`PanelCloseOrdering` separates SQLite commit, cancellation dispatch,
acknowledgement and producer exit. Its restart witness interrupts a close after
the durable commit and before cancellation. Its publication witnesses cover
both successful publication before close and refusal of an earlier captured
publication after close. A separate mutation attempts a write using the closed
revision, so the closed-state guard is checked independently of the revision
guard. Foreign scope, failed writes, lost deletion, sibling cancellation,
premature acknowledgement and lost persistence each have their own switch.

| Boundary | Implementation evidence |
| --- | --- |
| Storage transaction, receipt and retry | `TestTaskPanelCloseDeliveryFailureLeavesSourceRunning`, `TestTaskPanelCloseRejectsCapturedPublicationAndPreservesHistoricalReceipt` |
| Persistent marker, restart and late writes | `TestTaskPanelCloseSurvivesReopeningDatabaseAndRejectsLateWrites` |
| Actual running source and conversation | `TestTaskPanelSourceRunsAndStopsThroughActualManagerHTTP/close` observes cancellation of the real loopback request and a normal turn completion without repeating the request. |
| Scope and frozen transfer | `TestPhoneTaskPanelManifestUsesExistingSealedLink` rejects a foreign close and refuses continuation of the closed instance's transfer. |
| Acknowledgement crossing a delayed reply | `frontend/src/store/task-panel-ordering-audit.test.ts` holds a Web GET without delivering SSE deletion. `mobile/src/lib/use-task-panels.test.tsx` holds the old body and the following manifest separately. Both also exercise acknowledgement from a previous scope. |

Implementation mutations run in isolated copies. Removing the Store's
closed-row guard admits a new write at the closed revision. Removing the Web
snapshot filter reintroduces the closed instance. On the phone, removing the
manifest filter restores the entry, while removing the post-read guard leaves
patch metadata for the closed instance. The unchanged regression passes in
each case.

The close ordering model begins with two registered sources and assumes
atomic Store operations. The delivery model abstracts message bytes and
decoding, and checks one reply and two visits. A fresh post-close read samples
durable state. Delivery is asynchronous before a client observes the close.
The models do not cover replacing the database with a historical backup,
encryption, native UI behavior, or when an already-issued external request
finishes unwinding. A successful close can be returned while the cancelled
producer is still running its cleanup.
