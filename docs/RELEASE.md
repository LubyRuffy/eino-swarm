# Unified release (F-430 / C-010)

`make release VERSION=x.y.z` publishes the allocated main batch in order:

| Platform | Build/sign | Destination | Completion |
|---|---|---|---|
| macOS | `make desktop-release` | GitHub `vX.Y.Z`, host architecture zip | remote SHA-256 matches |
| Android | APK-only `make mobile-android-release` | same GitHub Release | remote SHA-256 matches |
| iOS | Capacitor sync, `xcodebuild archive/exportArchive`, distribution signature/profile, `xcrun altool` | existing App Store Connect app | all configured groups attached, VALID, APPROVED, internal READY_FOR_BETA_TESTING and external IN_BETA_TESTING |

Installer upload does not replace the native installation/window/device acceptance
required for each candidate. Run project checks and native acceptance before release.
Platform errors are printed immediately; independent platforms continue. The command
exits non-zero if a required platform fails or is waiting. No version is incremented
inside publication. First allocate/commit one version for a qualified batch of at
least three distinct accepted Issues, integrate/push complete main, then update the
existing delivery ledger's batch version/SHA. The source package version must match.

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
must match the authorized record. If non-exempt encryption needs additional Apple
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
fetched `origin/main`; GitHub repository must match `go.mod`. A tag already published
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
