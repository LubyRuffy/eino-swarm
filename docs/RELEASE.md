# Unified release (F-430 / C-010)

`make release VERSION=x.y.z` publishes only the allocated batch's outstanding
required platforms in order:

| Platform | Build/sign | Destination | Completion |
|---|---|---|---|
| macOS | `make desktop-release` | GitHub `vX.Y.Z`, host architecture zip | remote SHA-256 matches |
| Android | APK-only `make mobile-android-release` | same GitHub Release | remote SHA-256 matches |
| iOS | Capacitor sync, `xcodebuild archive/exportArchive`, distribution signature/profile, `xcrun altool` | existing App Store Connect app | all configured groups attached, VALID, internal READY_FOR_BETA_TESTING and external IN_BETA_TESTING; any review submission must be APPROVED |

Installer upload does not replace the native installation/window/device acceptance
required for each candidate. Run project checks and native acceptance before release.
Platform errors are printed immediately; independent platforms continue. The command
exits non-zero if a required platform fails or is waiting. No version is incremented
inside publication. Allocate/commit one version after more than three main
commits since the last published source SHA **or** after the oldest unallocated,
accepted and integrated change has waited more than 24 hours. Then validate and
push the complete main and record its version/SHA in the delivery ledger.
Only mobile batches require `mobile/package.json` to match the new version.
Desktop-only releases leave the mobile version/build number unchanged.

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

Before a new archive, record the authorized **current build** classification:

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

The value shown is schema illustration, not this app's classification. Never copy
an old build's declaration. An existing signed IPA's boolean
`ITSAppUsesNonExemptEncryption` can supply the statement; otherwise source/build
must match the authorized record. A confirmation can also be retained in
`~/.zwai-swarm/ios-signing/testflight-releases.json` under
`compliance_confirmations` with `version`, `source_sha`, `build_number`,
`confirmed_by`, and boolean `uses_non_exempt`. The release adapter reads that
exact source/build record even if the active signing config later targets a
different build; conflicting exact records stop the release. Record a confirmed
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
new orchestration code, run the script from current main:

```bash
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
`ios-signing/testflight-releases.json`. API `builds/{id}/betaGroups` is not used.
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
