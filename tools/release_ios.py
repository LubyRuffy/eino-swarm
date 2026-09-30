"""Xcode/altool and the existing private App Store Connect client."""
import importlib.util
import os
from pathlib import Path
import plistlib
import tempfile
import time
import zipfile
from datetime import datetime, timezone
from zoneinfo import ZoneInfo

from release import NOT_REQUIRED, batch_delivery_complete, delivery_batch, delivery_complete, source_preflight, home, read_json, run, sha256, version_code, write_json


def load_client(path):
    spec = importlib.util.spec_from_file_location('zwai_private_testflight', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def credentials(path):
    values = {}
    for line in Path(path).read_text().splitlines():
        if line and not line.startswith('#') and '=' in line:
            key, value = line.split('=', 1)
            values[key.strip()] = value.strip().strip('"\'')
    return values


def ipa_info(path):
    with zipfile.ZipFile(path) as archive:
        names = [n for n in archive.namelist() if n.startswith('Payload/')
                 and n.count('/') == 2 and n.endswith('.app/Info.plist')]
        if len(names) != 1:
            raise ValueError('Expected exactly one iOS app in the IPA')
        return plistlib.loads(archive.read(names[0]))


def available(detail, review, groups, expected):
    attrs = (detail or {}).get('attributes', {})
    return (attrs.get('internalBuildState') == 'READY_FOR_BETA_TESTING'
            and attrs.get('externalBuildState') == 'IN_BETA_TESTING'
            and (review or {}).get('attributes', {}).get('betaReviewState') == 'APPROVED'
            and set(expected) <= set(groups))


class IOSRelease:
    def __init__(self, root, version, source_sha, state):
        self.root, self.version, self.source_sha, self.state = root, version, source_sha, state
        self.number = version_code(version)
        self.config_path = Path(os.environ.get('IOS_RELEASE_CONFIG', home() / 'ios-signing/release-config.json'))
        self.config = read_json(self.config_path, {})
        required = ['signing_dir', 'testflight_cli', 'group_ids']
        if any(not self.config.get(key) for key in required):
            raise ValueError('IOS_RELEASE_CONFIG needs signing_dir, testflight_cli and explicit group_ids')
        self.signing = Path(self.config['signing_dir']).expanduser()
        self.module = load_client(Path(self.config['testflight_cli']).expanduser())
        self.api = self.module.Client()
        self.ledger_path = home() / 'ios-signing/testflight-releases.json'
        self.ledger = read_json(self.ledger_path, {'releases': []})
        self.day = datetime.now(ZoneInfo('Asia/Shanghai')).date().isoformat()
        retained = [r for r in self.ledger.get('pending_changes', []) if r.get('batch_source_sha') == source_sha
                    and r.get('batch_version') == version and r.get('ipa') and r.get('ipa_sha256')]
        if not self.state.get('ios', {}).get('ipa') and retained:
            identities = {(r['ipa'], r['ipa_sha256']) for r in retained}
            if len(identities) != 1:
                raise ValueError('Conflicting retained IPA receipts')
            ipa, digest = identities.pop()
            self.state['ios'] = {'ipa': ipa, 'ipa_sha256': digest, 'source_sha': source_sha, 'status': 'signed'}

    def snapshot(self):
        self.api = self.module.Client()
        app_id = self.module.APP_ID
        self.builds = self.api.all(f'apps/{app_id}/builds?limit=200')
        self.uploads = self.api.all(f'apps/{app_id}/buildUploads?limit=200')
        self.groups = {g['id']: g for g in self.api.all(f'apps/{app_id}/betaGroups?limit=200')}
        expected = self.config['group_ids']
        if len(expected) != len(set(expected)) or any(g not in self.groups for g in expected):
            raise ValueError('Configured TestFlight groups must exist on this app and be unique')
        if not any(self.groups[g]['attributes'].get('isInternalGroup') for g in expected) or not any(
                not self.groups[g]['attributes'].get('isInternalGroup') for g in expected):
            raise ValueError('Both internal and external testing groups are required')
        self.members = {g: {b['id'] for b in self.api.all(f'betaGroups/{g}/builds?limit=200')}
                        for g in expected}
        builds = [b for b in self.builds if b['attributes']['version'] == self.number]
        if len(builds) > 1:
            raise ValueError('Ambiguous remote build number')
        return builds[0] if builds else None

    def observe(self, build, record=True):
        build_id = build['id']
        detail = self.api.request('GET', f'builds/{build_id}/buildBetaDetail').get('data')
        review = self.api.request('GET', f'builds/{build_id}/betaAppReviewSubmission').get('data')
        attached = [g for g, ids in self.members.items() if build_id in ids]
        ready = not build['attributes'].get('expired') and build['attributes'].get('processingState') == 'VALID' and available(detail, review, attached, self.config['group_ids'])
        if ready and record:
            releases = self.ledger.setdefault('releases', [])
            row = next((r for r in releases if str(r.get('build_number')) == build['attributes']['version'] and r.get('app_id') == self.module.APP_ID), None)
            if row is None:
                row = {'app_id': self.module.APP_ID, 'build_number': build['attributes']['version']}
                releases.append(row)
            if not row.get('successfully_published'):
                if build['attributes']['version'] == self.number:
                    row.update(version=self.version, source_sha=self.source_sha)
                row.update(successfully_published=True, success_beijing_date=self.day,
                           first_observed_available_at=datetime.now(ZoneInfo('Asia/Shanghai')).isoformat())
                write_json(self.ledger_path, self.ledger)
        return ready

    def quota(self, record=True):
        # Observe every ready build before changing availability, including pending reviews.
        for build in self.builds:
            self.observe(build, record)
        return any(r.get('successfully_published') and r.get('success_beijing_date') == self.day
                   and str(r.get('build_number')) != self.number
                   for r in self.ledger.get('releases', []) if r.get('app_id') == self.module.APP_ID)

    def declaration(self, info=None):
        value = (info or {}).get('ITSAppUsesNonExemptEncryption')
        if isinstance(value, bool):
            return value
        statement = self.config.get('compliance', {})
        if (statement.get('source_sha') == self.source_sha and str(statement.get('build_number')) == self.number
                and statement.get('confirmed_by') and isinstance(statement.get('uses_non_exempt'), bool)):
            return statement['uses_non_exempt']
        raise ValueError('Current IPA lacks an authorized export-compliance declaration; no upload attempted')

    def build_ipa(self):
        output = home() / 'ios-signing' / f'zwai-{self.version}-{self.number}-{self.source_sha[:7]}'
        archive, exported = Path(str(output) + '.xcarchive'), Path(str(output) + '-export')
        if archive.exists() or exported.exists():
            raise ValueError('Existing archive/export needs verified recovery metadata; refusing rebuild/overwrite')
        options = Path(self.config.get('export_options', self.signing / 'exportOptions.plist'))
        settings = plistlib.loads(options.read_bytes())
        if settings.get('method') != 'app-store-connect':
            raise ValueError('iOS export must use app-store-connect')
        _, profile = next(iter(settings['provisioningProfiles'].items()))
        run(['make', 'mobile-sync'], cwd=self.root)
        args = ['xcodebuild', '-project', 'mobile/ios/App/App.xcodeproj', '-scheme', 'App',
                '-configuration', 'Release', '-destination', 'generic/platform=iOS',
                '-archivePath', str(archive), 'archive', f'MARKETING_VERSION={self.version}',
                f'CURRENT_PROJECT_VERSION={self.number}', 'CODE_SIGN_STYLE=Manual',
                f'DEVELOPMENT_TEAM={settings["teamID"]}',
                f'PROVISIONING_PROFILE_SPECIFIER={profile}',
                f'CODE_SIGN_IDENTITY={settings["signingCertificate"]}']
        keychain = self.config.get('keychain')
        if keychain:
            run(['security', 'unlock-keychain', '-p', Path(self.config['keychain_password_file']).read_text().strip(), keychain])
            args.append(f'OTHER_CODE_SIGN_FLAGS=--keychain {keychain}')
        run(args, cwd=self.root)
        run(['xcodebuild', '-exportArchive', '-archivePath', str(archive), '-exportPath', str(exported),
             '-exportOptionsPlist', str(options)], cwd=self.root,
            env=dict(os.environ, PATH='/usr/bin:/bin:/usr/sbin:/sbin:' + os.environ.get('PATH', '')))
        return exported / 'App.ipa'

    def verify_ipa(self, ipa):
        info = ipa_info(ipa)
        if info.get('CFBundleShortVersionString') != self.version or str(info.get('CFBundleVersion')) != self.number:
            raise ValueError('IPA version/build does not match this batch')
        app = self.api.request('GET', f'apps/{self.module.APP_ID}')['data']
        if info.get('CFBundleIdentifier') != app['attributes']['bundleId']:
            raise ValueError('IPA bundle does not match configured App Store Connect app')
        with tempfile.TemporaryDirectory(prefix='zwai-ipa-verify-') as directory:
            with zipfile.ZipFile(ipa) as archive:
                archive.extractall(directory)
            apps = list((Path(directory) / 'Payload').glob('*.app'))
            run(['codesign', '--verify', '--deep', '--strict', str(apps[0])])
            identity = run(['codesign', '-dvv', str(apps[0])])
            settings = plistlib.loads(Path(self.config.get('export_options', self.signing / 'exportOptions.plist')).read_bytes())
            if 'Authority=Apple Distribution:' not in identity or f'TeamIdentifier={settings["teamID"]}' not in identity:
                raise ValueError('IPA signing identity/team does not match distribution configuration')
            profile = plistlib.loads(run(['security', 'cms', '-D', '-i', str(apps[0] / 'embedded.mobileprovision')]).encode())
            if profile.get('ProvisionedDevices') or profile.get('ProvisionsAllDevices') or profile['Entitlements'].get('get-task-allow'):
                raise ValueError('IPA is not signed for App Store distribution')
            if profile['Entitlements'].get('application-identifier') != settings['teamID'] + '.' + info['CFBundleIdentifier']:
                raise ValueError('IPA profile application identity mismatch')
            if profile['ExpirationDate'].replace(tzinfo=timezone.utc).timestamp() <= datetime.now().timestamp():
                raise ValueError('Distribution profile expired')
        return self.declaration(info)

    def complete_delivery(self):
        for row in self.ledger.get('pending_changes', []):
            if row.get('batch_source_sha') == self.source_sha and row.get('batch_version') == self.version:
                row.update(status='published', delivered_version=self.version, delivered_build_number=self.number)
        for row in self.ledger.get('delivery_aliases', {}).values():
            if row.get('source_sha') == self.source_sha:
                row['status'] = 'published'
        write_json(self.ledger_path, self.ledger)
        path = Path(os.environ.get('DELIVERY_STATE', home() / 'issue-automation/delivery-state.json'))
        delivery = read_json(path, {})
        batch, current = delivery_batch(delivery, self.version, self.source_sha)
        if batch:
            batch['ios'] = 'published'
            batch['version_locked'] = True
            if 'ios' in batch.get('additional_platforms', {}):
                batch['additional_platforms']['ios']['status'] = 'published'
            if current:
                delivery['last_published_source_sha'] = self.source_sha
            issues = set(map(str, batch['issues']))
            issues.update(str(n) for n, alias in delivery.get('delivery_aliases', {}).items()
                          if alias.get('source_sha') == self.source_sha)
            for number in issues:
                row = delivery.get('issues', {}).get(number)
                if row:
                    row['version'] = row['batch_version'] = self.version
                    row['release_source_sha'] = self.source_sha
                    if row.get('ios') not in NOT_REQUIRED:
                        row['ios'] = 'published'
                    row['delivery_complete'] = delivery_complete(row)
            batch['publication'] = ('platform_delivery_verified' if batch_delivery_complete(delivery, batch)
                                    else 'platform_delivery_in_progress')
            write_json(path, delivery)

    def publish(self, check=False):
        deadline = time.monotonic() + self.config.get('processing_timeout_seconds', 600)
        while True:
            result = self.publish_once(check)
            if not check:
                self.state['ios'] = result
                write_json(home() / 'releases' / f'{self.version}.json', self.state)
            if result['status'] == 'published' and not check:
                self.complete_delivery()
            if check or result['status'] != 'waiting_processing' or time.monotonic() >= deadline:
                return result
            print('ios: Apple processing pending; same build will be resumed', flush=True)
            time.sleep(30)

    def publish_once(self, check=False):
        self.day = datetime.now(ZoneInfo('Asia/Shanghai')).date().isoformat()
        run(['xcodebuild', '-version'])
        build = self.snapshot()
        existing = self.state.get('ios', {})
        known = next((r for r in self.ledger.get('releases', []) if str(r.get('build_number')) == self.number and r.get('app_id') == self.module.APP_ID), {})
        if build:
            if existing.get('source_sha', known.get('source_sha')) != self.source_sha:
                raise ValueError('Remote build has no matching source receipt; verify provenance before recovery')
            marketing = self.api.request('GET', f'builds/{build["id"]}/preReleaseVersion')['data']['attributes']['version']
            if marketing != self.version:
                raise ValueError('Remote build marketing version mismatch')
        if build and self.observe(build, not check):
            return {**existing, 'status': 'published', 'source_sha': self.source_sha, 'build_id': build['id'], 'group_ids': self.config['group_ids']}
        if self.quota(not check):
            return {**existing, 'status': 'waiting_daily_limit', 'earliest_beijing_date': 'next natural day'}
        # Other accepted candidates might become available after asynchronous review.
        pending = [r for r in self.ledger.get('releases', []) if r.get('app_id') == self.module.APP_ID
                   and not r.get('successfully_published') and str(r.get('build_number')) != self.number]
        # Another publisher may have submitted a candidate without our local receipt.
        for candidate in self.builds:
            if candidate['attributes']['version'] != self.number and not candidate['attributes'].get('expired'):
                review = self.api.request('GET', f'builds/{candidate["id"]}/betaAppReviewSubmission').get('data')
                if (review or {}).get('attributes', {}).get('betaReviewState') in ('WAITING_FOR_REVIEW', 'IN_REVIEW'):
                    pending.append(candidate)
        if pending:
            raise ValueError('Another accepted TestFlight candidate is pending; resolve it before changing availability')
        if check:
            options = Path(self.config.get('export_options', self.signing / 'exportOptions.plist'))
            if plistlib.loads(options.read_bytes()).get('method') != 'app-store-connect':
                raise ValueError('iOS export must use app-store-connect')
            self.declaration(ipa_info(existing['ipa']) if existing.get('ipa') else None)
            return {'status': 'preflight_passed'}
        if not build:
            uploads = [u for u in self.uploads if str(u['attributes'].get('cfBundleVersion')) == self.number]
            if uploads:
                if any(u['attributes'].get('state', {}).get('state') == 'FAILED' for u in uploads):
                    raise ValueError('Apple upload processing failed; inspect its errors before retry')
                return {**existing, 'status': 'waiting_processing', 'uploads': [u['id'] for u in uploads]}
            if existing.get('status') in ('upload_attempted', 'waiting_upload_confirmation', 'waiting_processing'):
                return {**existing, 'status': 'waiting_upload_confirmation'}
            occupied = [int(b['attributes']['version']) for b in self.builds if b['attributes']['version'].isdigit()]
            occupied += [int(u['attributes']['cfBundleVersion']) for u in self.uploads if str(u['attributes'].get('cfBundleVersion', '')).isdigit()]
            if occupied and int(self.number) <= max(occupied):
                raise ValueError('iOS build number must increase beyond all remote builds/uploads')
            ipa = Path(existing['ipa']) if existing.get('ipa') else None
            if ipa:
                if existing.get('source_sha') != self.source_sha or existing.get('ipa_sha256') != sha256(ipa):
                    raise ValueError('Retained IPA source/hash mismatch')
            else:
                self.declaration()  # refuse expensive archive until this build's classification exists
                ipa = self.build_ipa()
            if source_preflight(self.root, self.version, ('ios',)) != self.source_sha:
                raise ValueError('Source changed during iOS archive; refusing upload')
            self.api = self.module.Client()
            value = self.verify_ipa(ipa)
            existing = {'status': 'signed', 'ipa': str(ipa), 'ipa_sha256': sha256(ipa),
                        'source_sha': self.source_sha, 'uses_non_exempt': value}
            self.state['ios'] = existing
            # Durable checkpoint before upload: the outer driver will persist on failure too.
            write_json(home() / 'releases' / f'{self.version}.json', self.state)
            accepted = self.snapshot()  # recheck immediately before the first upload
            if self.quota():
                return {**existing, 'status': 'waiting_daily_limit'}
            if accepted or any(str(u['attributes'].get('cfBundleVersion')) == self.number for u in self.uploads):
                return {**existing, 'status': 'waiting_processing'}
            upload_credentials = credentials(self.signing / self.config.get('upload_credentials', 'AppStoreConnectAPI.credentials'))
            existing['status'] = 'upload_attempted'
            write_json(home() / 'releases' / f'{self.version}.json', self.state)
            run(['xcrun', 'altool', '--upload-app', '--type', 'ios', '-f', str(ipa),
                 '--apiKey', upload_credentials['APP_STORE_CONNECT_KEY_ID'],
                 '--apiIssuer', upload_credentials['APP_STORE_CONNECT_ISSUER_ID']],
                env=dict(os.environ, API_PRIVATE_KEYS_DIR=str(self.signing)))
            self.ledger.setdefault('releases', []).append({'app_id': self.module.APP_ID,
                'build_number': self.number, 'version': self.version, 'source_sha': self.source_sha,
                'ipa_path': str(ipa), 'ipa_sha256': existing['ipa_sha256'], 'state': 'uploaded_waiting_processing'})
            write_json(self.ledger_path, self.ledger)
            self.snapshot()
            confirmed = any(str(u['attributes'].get('cfBundleVersion')) == self.number for u in self.uploads)
            return {**existing, 'status': 'waiting_processing' if confirmed else 'waiting_upload_confirmation'}
        if build['attributes'].get('expired'):
            raise ValueError('Apple build expired; it cannot be delivered by retry')
        if build['attributes'].get('processingState') != 'VALID':
            if build['attributes'].get('processingState') == 'FAILED':
                raise ValueError('Apple build processing failed')
            return {**existing, 'status': 'waiting_processing', 'build_id': build['id']}
        value = self.declaration(ipa_info(existing['ipa']) if existing.get('ipa') else None)
        self.module.set_compliance(self.api, build['id'], str(value).lower(), True)
        for group in self.config['group_ids']:
            self.module.add_group(self.api, build['id'], group)
        self.module.submit_review(self.api, build['id'])
        build = self.snapshot()
        return {**existing, 'status': 'published' if self.observe(build) else 'waiting_beta_review',
                'build_id': build['id'], 'group_ids': self.config['group_ids']}
