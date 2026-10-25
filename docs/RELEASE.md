# Unified release (F-430 / C-010)

`make release VERSION=x.y.z` publishes only the allocated batch's outstanding
required platforms in order:

| Platform | Build/sign | Destination | Completion |
|---|---|---|---|
| macOS | `make desktop-release` | GitHub `vX.Y.Z`, host architecture zip | ZIP 有 Finder 图标资源与 plist 引用，远端 SHA-256 匹配 |
| Android | APK-only `make mobile-android-release` | same GitHub Release | remote SHA-256 matches |
| iOS | Capacitor sync, `xcodebuild archive/exportArchive`, distribution signature/profile, `xcrun altool` | existing App Store Connect app | all configured groups attached, VALID, internal READY_FOR_BETA_TESTING and external IN_BETA_TESTING; any review submission must be APPROVED |

Installer upload does not replace the native installation/window/device acceptance
required for each candidate. Run project checks and native acceptance before release.
For Mac, inspect the extracted `zwai.app` in the guest's Finder: the visible mark comes
from `Contents/Resources/AppIcon.icns` selected by `CFBundleIconFile` in
`Info.plist`. The Dock icon supplied to Wails is a separate runtime path.
`tools/release.py` rejects a new ZIP without those installer resources before upload.
If the allocated batch records a passed macOS native-window receipt for the exact
version, source SHA, asset name and SHA-256, keep that accepted ZIP in `bin/`.
The release entry verifies and uploads that file without rebuilding it; a missing
or changed ZIP stops macOS publication. An allocated batch marked
`ready_for_publication` also requires this exact receipt.
For Android, record the exact device-accepted APK in the allocated batch's
`release_android_validation` object: `status: "passed"`, `version`, full
`source_sha`, `asset_name` and `asset_sha256`. Keep that file at
`bin/zwai-x.y.z-android.apk`. Publication verifies all five fields and the batch source SHA, then reuses
those bytes without running Make. A public tag does not hide a conflicting
same-version batch receipt. A present malformed/conflicting receipt, a
missing/changed file, or batch `android: "ready_for_publication"` without a
matching receipt refuses Android upload. The receipt records prior installation,
upgrade/signature and behavior acceptance; the hash check does not perform or
replace those checks. A still-pending batch without this receipt retains the
existing build entry. Existing public APKs retain their remote-digest retry rule.

Platform errors are printed immediately; independent platforms continue. The command
exits non-zero if a required platform fails or is waiting. No version is incremented
inside publication. Allocate/commit one version after more than three main
commits since the last published source SHA **or** after the oldest unallocated,
accepted and integrated change has waited more than 24 hours. Then validate and
push the complete main and record its version/SHA in the delivery ledger.
Only mobile batches require `mobile/package.json` to match the new version.
Desktop-only releases leave the mobile version/build number unchanged.

### Recover native Mac acceptance in a locked session

Run native desktop cases only inside UTM **ci-macOS**, using the
[isolated native case lifecycle](TESTING.md#native-desktop-isolation-f-430--c-031).
Never fall back to launching/focusing a test app or UTM console on the user's
host desktop. Guest control unavailable means native acceptance remains pending.
Cases are serial: retain window/behavior evidence, close the case-owned app and
engine, wait for exit, then remove private home/cache and data before the next case. A window
receipt does not prove cleanup; retain the runner's `cleanup.json` separately.

When native acceptance is pending, inspect the existing UTM guest session.
After a user unlock, session change or VM/control-channel change, begin the
resumed observation with `guest.state=unknown` and recheck the actual guest.
Keep earlier transport/login failures as history, then replace the active
state and login result with this recovery's observations. Do not reuse an old
UTM-unreachable claim when the host locks again.
A host lock says nothing about that guest's readiness. If the guest is locked,
use its existing authorized account from the UTM skill/private materials through
background guest control, then inspect the retained package in the isolated
case. Do not focus the host's UTM console, change login/security settings or print credentials.
Record a real failed login before calling guest unlock blocked; a locked screen,
SSH connection or started process alone does not prove failure or acceptance.

Audit a credential-free private observation before choosing the next action:

```bash
python3 tools/audit_macos_acceptance.py --observation /absolute/private/observation.json
```

| Observation field | Meaning |
|---|---|
| `candidate` | Object with `version` (numeric `x.y.z`), full lowercase `source_sha`, ZIP basename `asset_name` and lowercase SHA-256 `asset_sha256` from the frozen candidate |
| `guest.target`, `guest.state` | Existing guest identity; current state is `locked`, `ready` or `unknown`. `unknown` requires fresh guest observation before using any login result |
| `guest.existing_credentials_available` | Boolean availability of an already authorized account; never the account or secret itself |
| `guest.unlock` (optional) | Object with that guest's `target`, `result` (`not_attempted`, `succeeded`, `failed`) and `evidence` list of private observation references |
| `native_window` (optional) | Object with `guest_target`, the same four-field `candidate`, boolean `observed` and `behavior_verified`, and an `evidence` list of current native-control or real window/behavior receipt references |
| `native_window.ssh_helper_permission_denied`, `native_window.trusted_guest_terminal_available` (optional) | Booleans supported by this recovery's native-control evidence: the SSH-launched helper was denied, and an existing authorized guest Terminal responsibility chain is usable. Administrator membership alone proves neither |
| `host_locked` (optional) | Boolean observation only; may be omitted and does not affect the decision |
| `guest.ssh_ok`, `guest.process_started` (optional) | Boolean transport/process observations only; never native acceptance |

The tool only reads JSON and reports `status`, `next_action`, `reason`, the
candidate/guest identity and evidence references. It does not operate UI, SSH,
credentials or releases, and does not independently inspect referenced evidence.
Unknown fields and invalid input are rejected without echoing input values.
Exit 0 means a valid decision, including `pending`/`blocked`; exit 2 means invalid
or unreadable input. Read the status, not the exit code, to decide acceptance.

Except for already verified exact-package window evidence, `unknown` always
yields `observe_guest_session`; older successful or failed unlock observations
cannot override it. `locked` with an available authorized account and no attempt yields
`unlock_guest`; successful evidenced unlock or a ready guest yields
`inspect_native_window`. Missing attempt evidence requires observation, and only
an evidenced unlock failure for the target guest yields `blocked`. `passed`
requires observed and verified native behavior, nonempty real evidence references,
the same guest, and all four candidate identity fields exactly matching. A later
lock does not invalidate an already accepted exact package. Continue publication
using the same accepted ZIP and existing batch/version under the rules above.
This development audit tool does not itself change app behavior or require a new
installer release.

For a ready guest, both optional native-control flags being true with nonempty
evidence and the same guest/four-field candidate yields pending /
`inspect_native_window_via_guest_terminal`. Execute that existing authorized
isolated path under the shared native-case lock and retain the actual helper
result before asking the user to change settings. Missing/false flags, missing
evidence or mismatched identity keep the ordinary window-inspection action;
older observations without these flags remain valid. Unknown guest state still
requires re-observation, and completed exact-package acceptance still wins.
The action grants no privacy permission and does not itself accept a window.

### Finalize one processing batch before publication

At the start of a run, identify the Issues and worktrees this automation will
process. Integrate and push each actionable accepted fix in order, then audit
the final complete main. Defer an Issue only for a recorded real blocker or
requirements ambiguity; other-owned and unrelated open Issues do not hold a
qualified batch indefinitely. The desktop and phone changes processed in this
batch share one source version, tag and SHA even if their fixes came from
different worktrees. Evaluate the existing `>3` first-parent commits or `>24h`
acceptance wait rule only after this workset is settled.

Before the first new-platform upload, record
`pending_new_batch.finalized_workset` in the private delivery ledger with
`source_sha`, `issues` and `platforms_to_release` from the fresh
`tools/audit_pending_batch.py` report. `tools/release.py` refuses new
publication while `active_issue` is set or if that receipt differs from the
live audit. A platform-specific failure after version allocation leaves the
other verified platforms runnable against the same frozen source. An already
public tag/SHA stays immutable; a later-integrated fix belongs to the next
qualified batch, never to the old tag.

### TestFlight version and build number

The batch/tag/package version identifies the complete source release. TestFlight's
`MARKETING_VERSION` is independent: a new candidate defaults to the marketing version
of this app's highest-numbered successfully published build in the private ledger.
Legacy ledger rows use their `version` field. With no published history, the first
candidate uses the batch version. Routine updates therefore stay on the same
TestFlight version and change only `CURRENT_PROJECT_VERSION`, still derived from
the mobile package version by `tools/release.py:version_code`. All remote builds and
uploads must have lower build numbers before a new upload is attempted.

Set optional `marketing_version` to a numeric `x.y.z` in `IOS_RELEASE_CONFIG` only
when deliberately starting a new TestFlight version. It applies to new candidates;
existing release state, signed IPA and source receipts retain their original version.
Candidate state and upload/publication receipts store `marketing_version` and
`build_number` separately from the batch `version` and `source_sha`. Removing the
override after successful publication continues that version from its ledger receipt.
Local `npm run version:sync-ios` still derives development Xcode settings from the
package; the release archive overrides the marketing version without rewriting source.

For example, after `0.1.23 (123)` has been delivered, batch `0.1.24` uploads
`0.1.23 (124)` unless that candidate already has a frozen receipt or an explicit
new-version override. The GitHub tag and Android version remain `0.1.24`.

Apple requires review of the first external build of a version; later builds
may not need a full review, so reusing a version does **not** guarantee no review.
See [Apple's TestFlight App Review definition](https://developer.apple.com/help/glossary/testflight-app-review/).
After group attachment the adapter reads Apple's external build state and only calls
`submit_review` for `READY_FOR_BETA_SUBMISSION`. An `IN_BETA_TESTING` build needs no
new submission; a missing review resource is accepted only with that actual testing
state and all required groups. A review that exists must still be `APPROVED`.

`python3 tools/audit_pending_batch.py` reconciles open and closed Issues against
all accepted ledger rows, including macOS-only deliveries without iOS pending rows.
`issues` lists the complete pending set; `allocated_pending_issues` retains frozen
batch recovery. `unallocated_issues`, `pending_commit_count`,
`oldest_pending_at`, `pending_age_hours`, and `platforms_to_release` explain
`allocate_new_version`. Once a version is assigned but no platform has been
published, the audit reports `resume_allocated_version` instead of allocating
another number; the candidate must still contain the complete unallocated set.
The ledger records `last_published_source_sha` and each
accepted change's `integrated_at` or `accepted_at` in an offset-aware ISO 8601
form. The audit counts first-parent main commits after that SHA (so a merge
operation is one mainline submission) and checks the oldest pending
timestamp against 24 elapsed hours; it does not reset at midnight or per run.
The first verified public platform updates `last_published_source_sha` automatically.
An already allocated, public batch is excluded from the next trigger even if
one of its platforms is still pending. The platform list is the union of
unallocated Issues' actual required delivery statuses and verified extra
behavior changes on the complete main. Record such a change in
`next_unallocated_batch.additional_platforms` with a required platform,
`source_shas` between the previous public SHA and current main, and its native
acceptance/release status. The batch needs that installer without changing an
unrelated Issue row's `not_required_by_behavior_change` status. Desktop selects
macOS, mobile selects Android and iOS. Server deployment is tracked separately.
Before assigning a newer batch, move a still-pending public batch into
`completed_batches[version].batch` with its frozen source and platform statuses.
The release entry can resume that old version from its frozen checkout; its
completion updates the old Issue rows and never rewinds the newest public SHA.

TestFlight may become public before a GitHub tag exists. In that case the
release entry recognizes a frozen current or historical batch by its exact
version/source and finalized workset, rather than treating it as unallocated.
It cross-checks the private release state's `first_publication` evidence and
successful TestFlight ledger row for the same source, build, marketing version,
build ID and IPA hash. This immutable proof includes the observed time and
strict processing, testing, review and bidirectional group state. A later iOS
expiry, group change or unavailable Apple client does not revoke this source
proof or block Mac/Android. The iOS publisher saves it on successful delivery.
Legacy state without this proof needs one current read-only Apple verification;
`--check` never saves it, while actual recovery saves it under the release lock
before attempting an installer upload. Missing/conflicting evidence stops
recovery; a conflicting GitHub tag is always refused. None of these proof
checks archives, uploads or submits review. Actual iOS delivery still checks
current installability. The frozen source may precede today's main. Each
remaining platform still passes its own acceptance gate, and unpublished new
batches still need the live qualification audit and complete finalized workset.

## Private iOS setup

Use the documented existing signing and App Manager CLI; browser login is unnecessary.
Set `IOS_RELEASE_CONFIG` or create `~/.zwai-swarm/ios-signing/release-config.json`
(0600). The configuration references private files; it contains no key contents:

```json
{
  "signing_dir": "/absolute/private/signing-directory",
  "testflight_cli": "/absolute/private/signing-directory/testflight-cli.py",
  "group_ids": ["internal-alpha-id", "internal-beta-id", "external-alpha-id", "external-beta-id"],
  "keychain": "/absolute/private/zwai.keychain-db",
  "keychain_password_file": "/absolute/private/keychain.pass"
}
```

Read the signing directory's README first. Group IDs must be explicitly taken from
this app's API and cover its intended internal and external groups. The adapter uses
that private CLI's `APP_ID` and existing authenticated `Client`, `set_compliance`,
`add_group`, `submit_review`. It does not create apps, keys or groups. Default export
options are `signing_dir/exportOptions.plist` (override `export_options`); default
upload credentials are `AppStoreConnectAPI.credentials` (override `upload_credentials`
with a filename in the same directory). API private keys are used from that directory,
not copied into Git or a VM. Keychain fields are optional if the correct distribution
identity is already usable. Command argv/output never appears in publishing errors.

The authorized application declaration is retained as the boolean
`ITSAppUsesNonExemptEncryption` in `mobile/ios/App/App/Info.plist`. The user
confirmed the existing `false` classification for unchanged encryption usage
on 2026-10-08 (Issue #62). Preflight and the pre-archive gate read this source
field, and the signed IPA must retain the same boolean. Routine version/build
increments do not require another question. Review relevant encryption use,
implementation and dependency changes before retaining or changing the field;
the script does not infer a classification from an old Apple build.
[Apple documents the Info.plist declaration](https://developer.apple.com/help/app-store-connect/test-a-beta-version/provide-export-compliance-information-for-beta-builds)
as the way to avoid repeated submission questions when documentation is not required.

For frozen sources without that field, record the authorized **current build** classification:

```json
{
  "compliance": {
    "source_sha": "the-full-frozen-batch-sha",
    "build_number": "the-mapped-mobile-build-number",
    "confirmed_by": "the-authorized-classification-record",
    "uses_non_exempt": false
  }
}
```

The value shown is schema illustration. Never copy an old per-build answer to
another source/build. An existing signed IPA's boolean
`ITSAppUsesNonExemptEncryption` can supply the statement; otherwise source/build
must match the authorized record when the source field is absent. A confirmation can also be retained in
`~/.zwai-swarm/ios-signing/testflight-releases.json` under
`compliance_confirmations` with `version`, `source_sha`, `build_number`,
`confirmed_by`, and boolean `uses_non_exempt`. The release adapter reads that
exact source/build record even if the active signing config later targets a
different build; conflicts between source, IPA or exact records stop the release. Record a confirmed
answer once and reuse its receipt on recovery, without asking the user again.
If non-exempt encryption needs additional Apple
documents, prepare them before completing TestFlight. See
[Apple export compliance](https://developer.apple.com/help/app-store-connect/test-a-beta-version/provide-export-compliance-information-for-beta-builds)
and [beta review API](https://developer.apple.com/documentation/appstoreconnectapi/post-v1-betaappreviewsubmissions).

## Preflight and recovery

```bash
make release-check VERSION=x.y.z       # no archive, upload or Apple mutations
make mobile-ios-release VERSION=x.y.z  # only iOS, same candidate
```

`VERSION` is a release triple, not a dirty `git describe`; pass it explicitly.
`ZWAI_HOME` defaults to `~/.zwai-swarm`. `DELIVERY_STATE` optionally selects the
existing `issue-automation/delivery-state.json`. Source must be clean and in freshly
fetched `origin/main`; GitHub repository must match `go.mod`. The default entry
reads required, still-unpublished platforms from the allocated batch; a
single-platform recovery can use `--platform macos|android|ios`. A tag already published
must resolve to exactly this SHA. To resume a frozen historical source with the
new orchestration code, run the script from current main with the accepted installer
in the frozen checkout's `bin/`. The public tag must point at the frozen source SHA:

```bash
python3 tools/release.py --version x.y.z --platform macos --source-dir /absolute/isolated/frozen-checkout
python3 tools/release.py --version x.y.z --platform ios --source-dir /absolute/isolated/frozen-checkout
```

This does not change current main or mix its new content into the historical batch.
Retained IPA path/hash from matching `testflight-releases.json` pending rows is reused.
Fresh candidates archive under `ZWAI_HOME/ios-signing`; exported IPA version, bundle,
distribution identity/team, profile and signature are verified before upload.
An existing archive without a verified receipt is not overwritten.

`ZWAI_HOME/releases/x.y.z.json` stores source, IPA hash and each platform's state;
a host file lock prevents overlapping runs. GitHub assets are checked before builds:
existing assets with equal digests are skipped; mismatched or missing digests require
investigation. No `--clobber`, tag replacement or new version on retry. The Release body retains
its existing description and appends a managed block with each platform status. Remote tags
and numeric versions are checked before first publication.
For a mixed-platform batch, a successful platform updates only the Issue rows that
require that platform. Other rows keep their `not_required_by_behavior_change`
status; each row becomes delivery-complete when all of its required platforms
are published. The first verified public platform locks the version and source
SHA onto all batch rows, so a partial delivery cannot qualify another version.
The batch-level platform state still records the shared Release.
An extra platform with `pending_native_*` status blocks only that platform
until native acceptance is recorded. Its success is tracked at batch level;
the batch is not fully delivered while it remains pending.

iOS first checks app `builds`, app `buildUploads`, group `builds` and the existing
`ios-signing/testflight-releases.json`. Configured groups are validated against
`apps/{app}/betaGroups`. Strict publication evidence cross-checks all pages of
`betaGroups/{group}/builds` with the public
`betaGroups?filter[builds]={build}&limit=200` collection
([Apple's beta-group list](https://developer.apple.com/documentation/appstoreconnectapi/get-v1-betagroups)). Only one relationship
filter is allowed: do not combine `filter[app]` with `filter[builds]`.
Neither `GET builds/{id}/betaGroups` nor its relationship GET is supported or used.
This retains the existing schema-1 first-public proof and its two actual query
directions; old frozen proof is validated locally without rewriting it. Read-only
checks do not persist that proof or delivery success; actual recording still
requires strict availability and both group directions before writing success.
An accepted build/upload is resumed, not uploaded again. Uncertain upload attempts
stay `waiting_upload_confirmation` for investigation instead of blind retry. A
failed Apple processing result stops this platform. Processing is polled every
30 seconds, up to 600 seconds (`processing_timeout_seconds` in private config).
Then the same command resumes compliance, associations and review. Apple review
waits remain `waiting_beta_review`; rerun the iOS-only entry to observe completion.

The Beijing natural day permits one **newly available** build, counted by first
verified group availability, not upload/VALID date. Ready builds are reconciled
before any availability mutation; another accepted pending candidate must be
resolved first. On actual completion the original pending Issue rows and delivery
aliases are updated, retaining the existing ledger and unrelated history. Closed
Issues are never reopened merely for publication waits. `--check` does not write
platform/daily success records or mutate Apple; it can fetch Git and create a lock.
