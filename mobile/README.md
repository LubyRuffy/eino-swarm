# zwai phone

iOS and Android apps. The camera scan of a `pairlink:v1:…` QR is the product
path; paste is the same URI, not a second protocol. After bind, a live
turn (or the last thread this phone opened) opens immediately; otherwise
the inbox lists projects and threads. The phone talks to the PC over
pairlink (WebSocket relay). After the ticket socket is up it sends `hello`
with a one-line model (OS + version + device) so Settings → Phone can
name the binding. It keepalives that socket (the hub idle-drops
a quiet connection) and reconnects a drop without unlinking. It never
calls zwai `/api`.

The phone is a compact screen on the same conversation as the desktop: same
event kinds and seq (`watch` / `unwatch` / `log`), clipped bodies so a frame
stays under 64KiB. First paint is a live-edge snapshot on the `watch` RPC
`ready`, already at the tail; a tap shows the chrome before that returns.
**Earlier** sits above the log; pulling up also pages older events. User and
assistant rows render markdown; a path link does not navigate the
webview. Tool rows stay collapsed until tapped, with a one-line field
preview instead of the packed JSON. A `progress` pulse is not a chat
row. `wait_agents` is a status count, not the `elapsed_ms` roster. A
`schedule` event is still **A wait is armed.** in the transcript; the live
wait is the banner (next check, **Run now**, **Cancel wait**).
`schedule_report` stays on the `report_schedule` chip. A standing
`/goal` is Pursuing / Done / Blocked / Paused, not a muted strip that
vanishes on complete. The objective is one truncated line (the full text
stays on the PC); a novel cannot cover Run now or the composer. Parked
waits are In progress on the inbox. Send,
follow-up, steer, stop, ask, `/goal` and `/plan` land on the PC engine; the
other window sees them live. Settings, Files, PTY and Trace stay on the PC.

The native trees `ios/` and `android/` live in git. `npx cap add` is already
done. A re-add restores Capacitor's cyan launcher; the zwai mark is
`internal/desktop/appicon.png` painted into those slots by
`go run ./mobile/scripts/genicons.go`. iOS 15+ (Xcode 27 dropped 14). Capacitor CLI 7.6 lowercases
`--packagemanager` and then compares it to `SPM`, so a re-add on a machine
without CocoaPods must use `node scripts/add-ios-spm.cjs` instead of
`npx cap add ios`. `npx cap sync` rewrites `CapApp-SPM/Package.swift`; keep
`.iOS(.v15)` if it snaps back to v14.

```bash
npm install              # uses registry.npmjs.org (project .npmrc)
npm test
npm run e2e
npm run cap:sync          # rebuild dist and copy into both apps
npx cap open ios          # Xcode
npx cap open android      # Android Studio
```

From the repo root: `make mobile-ios` / `make mobile-android`.
Android debug builds need JDK 21 (`JAVA_HOME` pointing at it). iOS needs Xcode
and a 15.0+ deployment target.

Hub URL and pairing live on the PC (Settings → Phone). This app
only stores the device identity and the redeem ticket on the phone. Camera
and cleartext (user-typed hub URLs, including `http` on a LAN) are declared
in `Info.plist` and `AndroidManifest.xml`; nothing compiles a hub hostname.

## Simulators

Camera is still the product path. Simulators paste the same `pairlink:v1`
URI minted by `POST /api/remote/offer`. Do not log that URI.

iOS Simulator shares the Mac loopback, so a hub on `127.0.0.1` is reachable.
Android emulator loopback is not the host; keep the hub URL as the PC's
address and publish the port:

```bash
adb reverse tcp:7780 tcp:7780   # when the hub listens on 7780
```

A hub redeem `host offline` means this PC's WebSocket is not in the hub's
live map. Settings → Phone must show online **when you mint a new QR**; a
code already spent on that error will not redeem again.

Capacitor Android WebView origin is `https://localhost`; the hub must answer
CORS on HTTP redeem. After minting an offer:

```bash
xcrun simctl pbcopy booted < offer.txt
cd ios/App
xcodebuild test -project App.xcodeproj -scheme App \
  -destination 'platform=iOS Simulator,name=iPhone 17' \
  -only-testing:AppUITests/BindFlowTests/testPasteBindListsTheSeedConversation \
  CODE_SIGNING_ALLOWED=NO
```

`BindFlowTests` reuses a saved bind when the scan screen is gone. A live
or last thread may already be open; the test taps Back to reach the inbox,
then asserts `path=` plus the seed conversation and Starts a new thread. It is
not part of `make check`. English accessibility names (`Pairing URI`,
`Paste and bind`, `New message`, `Start`, `Back`, `Stop` / `Follow-up` /
`Send`) are the unit-test locale (`en`); Playwright `e2e/scan.spec.ts` uses
`zh-CN`.
