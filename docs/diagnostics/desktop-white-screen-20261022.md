# Desktop white screen investigation — 2026-10-22

Issue: [#94](https://github.com/LubyRuffy/eino-swarm/issues/94). Scope: `F-115`,
`F-145`, `C-033`, `C-034`. Native release acceptance is **pending**; implementation
and headless regressions alone do not accept this incident.

## Field evidence

The original host desktop (PID 51451), engine (52217), WebContent (52485) and GPU
process (52483) were identified before recovery. The engine API remained responsive
(about 13 ms), and HTML/JavaScript/CSS returned HTTP 200. The desktop main thread
was in its normal run loop. WebContent used about 1.1 GiB and its sampled main
thread repeatedly followed:

```
RemoteLayerTreeDrawingArea::updateRendering
  LegacyRenderSVGRoot::paintReplaced
    RemoteGraphicsContextProxy::concatCTM / clip
      IPC::StreamClientConnectionBuffer::tryAcquire
        semaphore_timedwait
```

Unified logs first recorded repeated `FailedToAcquireBufferSpan` at 2026-10-22
01:34:24 Asia/Shanghai, with approximately 15-second waits; WebContent was reported
unresponsive later that morning. These observations locate the observed failure
in WebKit remote rendering IPC rather than engine readiness. They do not identify
which application SVG, if any, deterministically triggers the stall.

The authorized operational restart restored a real native window from checkout
`5c97ee6f17c7e95cb7d697029b9e8eb9667cd772`. Both original running turn identities
and the database record counts were retained. That restart was recovery of the
user's incident, not acceptance of a source fix. Private original evidence is in
`/tmp/zwai-white-recovery-20261022/` and `/tmp/zwai-white-webkit-sample.txt`.
A mistaken old acceptance-app launch during diagnosis was corrected; it is not
used as evidence for the fix.

## Upstream mechanism and uncertainty

[WebKit commit 3c5ef807](https://github.com/WebKit/WebKit/commit/3c5ef8070083d991e6ed9d233cedf8c525008534)
fixes bug 328269: a nearly full IPC stream buffer can make the client wait while
batched messages have not yet awakened the server. The patch flushes a pending
batch before waiting; its C++ regression fills the buffer with the server asleep.
The described WebContent main-thread hang, RemoteGraphicsContextProxy path and
timeout match this incident closely. This is a **strong candidate mechanism**,
not proof that Apple's installed private branch contains this exact defect.

The observed system was macOS 27.0.1 (26A434), WebKit 22625.1.29.11.28. Its installed
framework predates the public commit (2026-10-21 17:34:30 UTC), but equivalent
private fixes/backports cannot be excluded from public version metadata alone.
The upstream TestWebKitAPI test has not been built or run here. A prepared SVG
stress page is not a deterministic reproducer and is not counted as acceptance.
Product code does not patch or replace system WebKit.

## Confirmed application protection gaps

Before this change the native shell had no independent painted-frame health
check, so a live engine could coexist indefinitely with a frozen window. Composer
state also relied on volatile page state; clearing before an HTTP acknowledgment
could lose text and attachments on rejection, reload or an ambiguous send.
A fixed runtime cache path could overwrite/re-sign a still-running bundle.
These are application defects regardless of the vendor trigger.

The repair persists versioned snapshots and original attachment bytes in SQLite,
retains uncertain sends for human review, and checks a changing probe against
actual WKWebView snapshot pixels. Recovery reloads once, then replaces only the
shell through a bounded private-pipe ownership handshake. It keeps the same
engine, turns and workers, carries at most two replacement attempts across
shell generations, and displays a native error when exhausted. Immutable
executable-hash cache bundles prevent replacement of an executing binary.

## Validation and remaining gates

Observed business failures before the fixes include early composer clearing on
rejection/no acknowledgment, deleted-owner draft resurrection, and rewriting an
executing cache bundle. Their retained RED/GREEN evidence is under
`/tmp/issue94-frontend-evidence/`, `/tmp/issue94-native-recovery-evidence/` and
`/tmp/issue94-root-draft-tests.txt`. The focused race run covers new backend
functions at 91.7–100%; storage rollback, concurrent revisions, original image
bytes/dimensions, scope isolation and lost-storage errors are asserted.
Later review also reproduced a reporter shutdown that kept starting queued HTTP
requests and wrote after its caller returned. Closing the queue under the enqueue
lock and cancelling the active request fixes this failure; real blocked-HTTP and
late-producer race regressions passed twenty repetitions each. The subsequent
complete current-source CLI race run passed at 91.8% package coverage. Native OS entry logic and
inherited-descriptor rejection are covered separately from the one-line GUI wrapper.

The identified dirty checkout copy at `06c4674` also passed the actual
`go run ./cmd/zwai desktop` entry inside ci-macOS: a visible offline turn, matching
served assets, normal Quit and complete process/data cleanup. A separate packaged
development baseline retained draft text, original binary/PNG bytes, the running
and parked workers and the same engine through Quit/reopen. These are development
results, not a clean-source formal installer result, and do not establish that
the host's already-running binary changed. The first checkout case reached its
behavior assertions but failed read-only module-cache cleanup; its failed receipt
was preserved, the runner was fixed, and a fresh case passed complete cleanup.

Required before acceptance: complete current-source build/static/unit/E2E review;
a clean pushed source and its exact formal installer; serial ci-macOS baseline,
renderer/GPU stall, finite-budget exhaustion, three real sleep/wake cycles and
at least 18 hours of operation. Preserve package/source hashes, real native
screenshots/actions, unchanged draft bytes, engine/turn/worker identity and cleanup
receipts. See [testing procedure](../TESTING.md#desktop-renderer-resilience-f-115--c-034).
The host's already-running app is outside the native acceptance scope.
