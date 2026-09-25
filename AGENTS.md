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
make release                       # Mac zip + Android APK onto one GitHub Release
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

- New or changed Go functions need unit tests in the same package, covering more
  than 90% of the new statements. Current per-package coverage is listed in
  [docs/TESTING.md](docs/TESTING.md); do not regress it.
- Run with `-race`. The engine, the event bus and the HTTP server run
  concurrently, and every serious bug found here so far has been an ordering bug.
- If something cannot be tested, split it until the untestable part is a two-line
  wrapper: `startDesktopServer` vs `runDesktop`, `serveWeb` vs `runWeb`,
  `buildTUISwarm` vs `runTUI`. Do not leave logic inside a function that no test
  can reach.
- Name tests after what would break for the user
  (`TestSlowSubscriberDoesNotBlockTheRun`), and comment *why the behaviour
  matters* so a later reader can tell a regression from an obsolete expectation.
- Never test against a network or a real model. Use the scripted offline
  provider, and keep its script generic.
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
- `frontend/dist` is a Vite artefact and is gitignored. `go run ./cmd/zwai
  desktop` (and `web`) rebuild it when the TypeScript sources changed
  (`frontend.Ensure`, `//go:generate` in `frontend/embed.go`). A shipped
  binary without a checkout serves the embed. A sentinel `dist/.gitkeep`
  stays in git so `go:embed` still compiles before the first build.

## Backend rules

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
