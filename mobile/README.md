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
webview. A `chart` fence paints as a plot with a table of the same rows;
a truncated fence waits, and a finished invalid body stays code. Adjacent thoughts and tool calls fold into one row (the same
compact rule as desktop user view, and the phone's only view). An
answer stays on screen and splits that row. The live tail is **Thinking**
/ **Planning next moves** / **Editing** / **Reading** / **Exec**; open
the row for the pieces. A `progress` pulse is not a chat
row. `wait_agents` is a status count, not the `elapsed_ms` roster.
A conversation with sub-agents offers **Agents / 子 Agent** in its header. The
list shows each role, ID, status, and latest permitted activity; open a row
for that agent's separate thought, tool, and answer log. **Earlier** loads
older events into the same list and log. Worker answers do not appear as
manager answers. Pairlink strips worker launch instructions and clips event
bodies; the phone does not offer the desktop-only full instruction view.
The `schedule` event is still **A wait is armed.** in the transcript; the live
wait is the banner (next check, **Run now**, **Cancel wait**).
`schedule_report` stays on the `report_schedule` chip. A standing
`/goal` is Pursuing / Done / Blocked / Paused, not a muted strip that
vanishes on complete. The objective is one truncated line (the full text
stays on the PC); a novel cannot cover Run now or the composer. Send,
follow-up, steer, stop, ask, `/goal` and `/plan` land on the PC engine; the
other window sees them live. Settings, Files, PTY and Trace stay on the PC.

A conversation sits on the composer rather than under a screen of blank:
a short thread is bottom-aligned, and scrolling away from the tail raises
**Jump to latest**. Answering inside a conversation is one rounded box
(`components/composer.tsx`) that grows with the text up to a cap, Enter
sends (Shift+Enter is a newline, and an IME candidate list is never a
send), and the round button stays off until there is something to send.
While a turn is live the box also offers steer, and says a plain send is
queued after this turn.
The waiting-message tray offers **Insert** on each row. **Interrupt and insert**
promotes the oldest waiting message into the current turn, then interrupts
the current manager step so it can read that message sooner; later messages
remain queued.
Select text in the PC conversation and tap **Add to chat** to attach it to
the next message. The quote appears above the box and can be read, edited, or removed before
sending. A quote can be sent by itself or with a typed request. The phone
sends the same `<selected_text>` and `<user_request>` blocks as the desktop;
the quote is not part of the editable message text. Direct model chat does
not have a PC transcript to quote.

The inbox does not compose. Search and **New chat** sit under the list,
and the header corner is the same **New chat** — Add a PC stays on the
menu, not a second control beside the chips. Each project row folds.
The icon on its right starts a conversation already in that project,
including a project that has no threads yet. A conversation shown under
In progress stays listed under its project too. **New chat** is its own
screen: which PC, which project, then that same box. Android's system back
leaves the screen
before it leaves a conversation.

Inbox rows are cards, not a settings list. State is a badge — Running /
Waiting / Waiting for an answer — and an idle row carries its age
(`lib/when.ts`), so a stale thread is not mistaken for today's. A parked
wait is an In progress row with the thread's own summary, because the host
sends that as the row's line when there is no live turn to preview. The
header is one row: which PC this is, not the app's own name. Pulling the
list down past the top reloads the roster; an empty inbox says so. Search
filters the roster this phone already holds.

## A model on the phone

Menu → Models saves an OpenAI-compatible endpoint: base URL, optional key,
chat completions or responses, discover, timeout. After one is saved, a
Chat tab sits on the top row, including when no PC is bound. That chat uses
the same composer as a PC thread. The reply streams. The thought is the
same live row as a PC thread: the latest line while it is arriving, the
full text when the row is opened. Discover uses the platform HTTP stack so
the webview origin is not the caller. The completion is read as it arrives
(`StreamBody` on the device) because that stack returns a POST body in one
piece. An image is vision; a text file is sent as text. A responses front
that rejects the image part is asked once more as chat completions. A
rejected reasoning summary is omitted and the responses call is tried
again. The key stays on
the phone.

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
[v0.1.12](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.12) attaches
[`zwai-0.1.12-android.apk`](https://github.com/LubyRuffy/eino-swarm/releases/download/v0.1.12/zwai-0.1.12-android.apk)
(versionName `0.1.12`, versionCode `112`). iOS is `0.1.12` (112).
[v0.1.11](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.11),
[v0.1.10](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.10),
[v0.1.9](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.9),
[v0.1.8](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.8),
[v0.1.7](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.7),
[v0.1.6](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.6),
[v0.1.5](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.5),
[v0.1.4](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.4),
[v0.1.3](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.3),
[v0.1.2](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.2),
[v0.1.1](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.1) and
[v0.1.0](https://github.com/LubyRuffy/eino-swarm/releases/tag/v0.1.0) are the
previous sideloads; they share the debug key, so each updates the last in
place. A later APK signed with a different key cannot.

`make mobile-android-release` syncs the web bundle, builds the Gradle
`release` variant, and copies the APK/AAB to `bin/`. Play/store signing
needs `ANDROID_KEYSTORE*` or a gitignored `mobile/android/keystore.properties`
— missing those fails on purpose so a debug-signed APK cannot ship as a
store build. `ANDROID_UNSIGNED=1` is the sideload path: debug-signed APK,
no AAB (`adb install` works; Play will not).

For a new mobile release, `npm version patch --no-git-tag-version` updates
`package.json` and its lockfile; then `npm run version:sync-ios` derives every
Xcode marketing/build setting from that package version. Verify the candidate
against the latest Android Release and TestFlight build before tagging. This
keeps the two phone platform versions together without editing each Xcode
configuration by hand.

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
in `Info.plist` and `AndroidManifest.xml`. The iOS camera component also
references the photo library, so those purpose strings are declared too;
the app itself only asks for the camera. Nothing compiles a hub hostname.

## Updates

The installed app checks [GitHub Releases](https://github.com/LubyRuffy/eino-swarm/releases)
itself (`https://api.github.com/repos/LubyRuffy/eino-swarm/releases/latest`).
It does not ask the PC. The browser walkthrough (`?mock=1`) does not check,
so a desk session never phones GitHub. A newer `zwai-*-android.apk` on
Android is downloaded and handed to the system installer. While the bytes
are moving, the banner shows the percent when the response has a length,
otherwise how much has arrived. The sheet still
needs a tap, and the first time Android may ask to allow installs from this
app. iOS has no package on that feed, so Update opens the release page.
**Not now** hides that version until a later one is published. A failed
automatic check stays quiet. **Menu → Check for updates** always requests:
it shows that it is fetching, says when this build is current, asks before
an install, and shows the response's own error. The download only follows `github.com/LubyRuffy/eino-swarm`
and GitHub's release-asset hosts.

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
then asserts `path=` plus the seed conversation, opens New chat, and Starts a new thread. It is
not part of `make check`. English accessibility names (`Pairing URI`,
`Paste and bind`, `New message`, `Start`, `Back`, `Stop` / `Follow-up` /
`Send`) are the unit-test locale (`en`); Playwright `e2e/scan.spec.ts` uses
`zh-CN`.
