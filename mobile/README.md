# zwai phone

iOS and Android apps. Scan QR opens a live viewfinder on a `pairlink:v1:…`
QR: a frame, a beam that sweeps up and down, and a short chime when it reads.
Paste is the same URI, not a second protocol. After bind, a live
turn (or the last thread this phone opened) opens immediately; otherwise
the inbox lists projects and threads. A later launch with saved tickets
paints host chips (the name this PC sent on `hello`/`list`) and a connecting
skeleton; the scan form is only when this
phone has never bound (or after Unlink of the last PC). Add a PC is a
side control that opens a sheet. Android's system back returns from a
conversation, or from that sheet, to the screen under it. The activity
finishes only from the inbox, or from the scan screen when this phone has
never bound (the header Back control does the same pop). The phone talks to the PC over
pairlink (WebSocket relay). After the ticket socket is up it sends `hello`
with a one-line model (OS + version + device) so Settings → Phone can
name the binding. It keepalives that socket (the hub idle-drops
a quiet connection) and reconnects a drop without unlinking. The chip dot
is green while that socket is up and gray while it is not; relay versus
direct stays on the dot's title, not its color. A failure says whether
nothing answered (server off, network cut, or a timeout, with the machine
reason) or the server refused (its own text). `host offline` is still
"this PC is not on the hub". It never
calls zwai `/api`.

The phone is a compact screen on the same conversation as the desktop: same
event kinds and seq (`watch` / `unwatch` / `log`), clipped bodies so a frame
stays under 64KiB. First paint is a live-edge snapshot on the `watch` RPC
`ready`, already at the tail; a tap shows the chrome before that returns.
**Earlier** sits above the log; pulling up also pages older events. User and
assistant rows render markdown; a path link does not navigate the
webview. Adjacent thoughts and tool calls fold into one row (the same
compact rule as desktop user view, and the phone's only view). An
answer stays on screen and splits that row. The live tail is **Thinking**
/ **Planning next moves** / **Editing** / **Reading** / **Exec**; open
the row for the pieces. A `progress` pulse is not a chat
row. `wait_agents` is a status count, not the `elapsed_ms` roster. A
`schedule` event is still **A wait is armed.** in the transcript; the live
wait is the banner (next check, **Run now**, **Cancel wait**).
`schedule_report` stays on the `report_schedule` chip. A standing
`/goal` is Pursuing / Done / Blocked / Paused, not a muted strip that
vanishes on complete. The objective is one truncated line (the full text
stays on the PC); a novel cannot cover Run now or the composer. Send,
follow-up, steer, stop, ask, `/goal` and `/plan` land on the PC engine; the
other window sees them live. Settings, Files, PTY and Trace stay on the PC.

A conversation sits on the composer rather than under a screen of blank:
a short thread is bottom-aligned, and scrolling away from the tail raises
**Jump to latest**. Starting a conversation and answering inside one are
the same control (`components/composer.tsx`): one rounded box that grows
with the text up to a cap, Enter sends (Shift+Enter is a newline, and an
IME candidate list is never a send), and the round button stays off until
there is something to send. While a turn is live the box also offers
steer, and says a plain send is queued after this turn.

Inbox rows are cards, not a settings list. State is a badge — Running /
Waiting / Waiting for an answer — and an idle row carries its age
(`lib/when.ts`), so a stale thread is not mistaken for today's. A parked
wait is an In progress row with the thread's own summary, because the host
sends that as the row's line when there is no live turn to preview. The
header is one row: which PC this is, not the app's own name. Pulling the
list down past the top reloads the roster; an empty inbox says so.

## Walking the screens without a PC

`?mock=1` boots onto the inbox against a scripted host (`lib/mock-link.ts`):
projects, a live turn, a parked wait, and idle recents. It answers the same
ops with the same shapes over no network and no model, so the inbox and a
conversation can be driven in a browser and in Playwright — the screens
behind the scan form used to have no end-to-end coverage at all. Add
`&tick=0` to play a turn as fast as the browser paints; the default paces it
so a human can watch. Conversations live on the host rather than on a link,
because one PC seen down two sockets is still one PC (React strict mode
opens two).

```bash
npm run dev     # then open http://127.0.0.1:5173/?mock=1
```

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

## Android release

Sideload APKs ship on [GitHub Releases](https://github.com/LubyRuffy/eino-swarm/releases).
[v0.1.1](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.1) attaches
[`zwai-0.1.1-android.apk`](https://github.com/LubyRuffy/eino-swarm/releases/download/v0.1.1/zwai-0.1.1-android.apk)
(versionName `0.1.1`, versionCode `101`).
[v0.1.0](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.0) is the
previous sideload. A later APK signed with a different key cannot update an
install in place.

`make mobile-android-release` syncs the web bundle, builds the Gradle
`release` variant, and copies the APK/AAB to `bin/`. Play/store signing
needs `ANDROID_KEYSTORE*` or a gitignored `mobile/android/keystore.properties`
— missing those fails on purpose so a debug-signed APK cannot ship as a
store build. `ANDROID_UNSIGNED=1` is the sideload path: debug-signed APK,
no AAB (`adb install` works; Play will not).

```bash
# Sideload (emulator / adb). No tags on this checkout is fine.
ANDROID_UNSIGNED=1 make mobile-android-release

# Signed APK + AAB (Play / sideload with your upload key)
export ANDROID_KEYSTORE="$HOME/.zwai-android-upload.jks"
export ANDROID_KEYSTORE_PASSWORD
export ANDROID_KEY_ALIAS=upload
make mobile-android-release
```

`keystore.properties` next to `mobile/android/build.gradle` is the same
four keys (`storeFile`, `storePassword`, `keyAlias`, `keyPassword`) if you
do not want them in the shell. Do not commit that file or `*.jks`.

Version: a `vX.Y.Z` git tag becomes `versionName` X.Y.Z and `versionCode`
`major*10000+minor*100+patch`. This checkout has no tags, so the script
falls back to `mobile/package.json` `version`. Override with
`ANDROID_VERSION` / `ANDROID_VERSION_CODE`. `ANDROID_ARTIFACT=apk|aab|both`
picks the Gradle task (default both). JDK 21 is required (Capacitor
`compileOptions`); JAVA_HOME 17 is skipped.

Hub URL and pairing live on the PC (Settings → Phone). This app
only stores the device identity and the redeem ticket on the phone. Camera
and cleartext (user-typed hub URLs, including `http` on a LAN) are declared
in `Info.plist` and `AndroidManifest.xml`; nothing compiles a hub hostname.

## Simulators

The viewfinder is the product path on a device with a camera. Simulators paste the same `pairlink:v1`
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
