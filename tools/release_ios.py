"""Xcode/altool and the existing private App Store Connect client."""
import importlib.util
import os
from pathlib import Path
import plistlib
import re
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
            and (review is None or review.get('attributes', {}).get('betaReviewState') == 'APPROVED')
            and set(expected) <= set(groups))


def verify_public_receipt(version, source_sha, state, ledger):
    """Validate retained first-public evidence without Apple tools or credentials."""
    proof = state.get('first_publication', {})
    ios = state.get('ios', {})
    if not isinstance(proof, dict):
        raise ValueError('First-public TestFlight receipt must be an object')
    number = version_code(version)
    known = [r for r in ledger.get('releases', []) if r.get('app_id') == proof.get('app_id')
             and str(r.get('build_number')) == number]
    identity = {'version': version, 'source_sha': source_sha, 'build_number': number,
                'marketing_version': ios.get('marketing_version'), 'build_id': ios.get('build_id'),
                'ipa_sha256': ios.get('ipa_sha256')}
    if (proof.get('schema_version') != 1 or proof.get('channel') != 'ios_testflight'
            or not proof.get('app_id') or state.get('version') != version
            or state.get('source_sha') != source_sha or ios.get('source_sha') != source_sha
            or ios.get('status') != 'published' or ios.get('build_number') != number
            or not identity['build_id'] or not isinstance(identity['marketing_version'], str)
            or not re.fullmatch(r'[0-9a-f]{64}', identity['ipa_sha256'] or '')
            or any(proof.get(key) != value for key, value in identity.items()) or len(known) != 1):
        raise ValueError('First-public TestFlight receipt has conflicting source/build identity')
    row = known[0]
    if (not row.get('successfully_published') or any(row.get(key) != identity[key]
            for key in ('version', 'source_sha', 'marketing_version', 'ipa_sha256'))):
        raise ValueError('First-public TestFlight receipt conflicts with the successful ledger row')
    try:
        moment = datetime.fromisoformat(proof['available_at'])
        observed = proof['availability']
        expected = ios['group_ids']
        valid = (moment.tzinfo is not None and expected and len(expected) == len(set(expected))
                 and observed['processing_state'] == 'VALID' and observed['expired'] is False
                 and available(observed['detail'], observed['review'], observed['groups_forward'], expected)
                 and set(expected) <= set(observed['groups_reverse']))
    except (KeyError, TypeError, ValueError):
        valid = False
    if not valid:
        raise ValueError('First-public TestFlight receipt lacks strict availability evidence')
    return proof


class IOSRelease:
    def __init__(self, root, version, source_sha, state, *, urgent_reason=None):
        self.root, self.version, self.source_sha, self.state = root, version, source_sha, state
        if urgent_reason is not None and (not isinstance(urgent_reason, str) or not urgent_reason.strip()):
            raise ValueError('An urgent reason must be nonblank text')
        self.urgent_reason = urgent_reason.strip() if urgent_reason is not None else None
        if self.urgent_reason:
            self.state['urgent_reason'] = self.urgent_reason
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
        self.marketing_version = self.select_marketing_version()
        if self.state.get('ios', {}).get('build_number', self.number) != self.number:
            raise ValueError('Retained iOS build number does not match this frozen batch')
        self.state.setdefault('ios', {}).update(marketing_version=self.marketing_version, build_number=self.number)

    def select_marketing_version(self):
        existing = self.state.get('ios', {})
        rows = [r for r in self.ledger.get('releases', []) if r.get('app_id') == self.module.APP_ID]
        known = next((r for r in rows if str(r.get('build_number')) == self.number
                      and r.get('source_sha') == self.source_sha), {})
        if 'marketing_version' in existing:
            value = existing['marketing_version']
        elif existing.get('ipa'):
            value = ipa_info(existing['ipa']).get('CFBundleShortVersionString')
        elif existing.get('source_sha') or known:
            # Old receipts predate separate train metadata; never reclassify an uploaded build.
            value = known.get('marketing_version', self.version)
        elif 'marketing_version' in self.config:
            value = self.config['marketing_version']
        else:
            published = [r for r in rows if r.get('successfully_published')
                         and str(r.get('build_number', '')).isdigit()]
            last = max(published, key=lambda r: int(r['build_number']), default={})
            value = last.get('marketing_version', last.get('version', self.version))
        if not isinstance(value, str) or not re.fullmatch(r'\d+\.\d+\.\d+', value):
            raise ValueError('TestFlight marketing version must be a numeric release triple')
        return value

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

    def verify_publication(self):
        """Prove an iOS-first frozen batch without archiving or mutating Apple."""
        if 'first_publication' in self.state:
            return verify_public_receipt(self.version, self.source_sha, self.state, self.ledger)
        existing = self.state.get('ios', {})
        known = [r for r in self.ledger.get('releases', [])
                 if r.get('app_id') == self.module.APP_ID and str(r.get('build_number')) == self.number]
        if (self.state.get('version') != self.version or self.state.get('source_sha') != self.source_sha
                or existing.get('status') != 'published' or existing.get('source_sha') != self.source_sha
                or existing.get('build_number') != self.number or not existing.get('build_id')
                or not re.fullmatch(r'[0-9a-f]{64}', existing.get('ipa_sha256', ''))
                or len(known) != 1):
            raise ValueError('TestFlight public recovery needs the exact source/build publication receipt')
        known = known[0]
        if (not known.get('successfully_published') or known.get('version') != self.version
                or known.get('source_sha') != self.source_sha
                or known.get('marketing_version') != self.marketing_version
                or known.get('ipa_sha256') != existing['ipa_sha256']):
            raise ValueError('TestFlight public recovery has conflicting source/build publication receipts')
        build = self.snapshot()
        if not build or build['id'] != existing['build_id']:
            raise ValueError('TestFlight public recovery build is missing or changed')
        marketing = self.api.request('GET', f'builds/{build["id"]}/preReleaseVersion')['data']['attributes']['version']
        reverse = {g['id'] for g in self.api.all(f'betaGroups?filter%5Bbuilds%5D={build["id"]}&limit=200')}
        detail = self.api.request('GET', f'builds/{build["id"]}/buildBetaDetail').get('data')
        review = self.api.request('GET', f'builds/{build["id"]}/betaAppReviewSubmission').get('data')
        forward = [g for g, ids in self.members.items() if build['id'] in ids]
        if (marketing != self.marketing_version or not set(self.config['group_ids']) <= reverse
                or build['attributes'].get('expired') or build['attributes'].get('processingState') != 'VALID'
                or not available(detail, review, forward, self.config['group_ids'])):
            raise ValueError('TestFlight public recovery requires current configured-group installability and review')
        return self.publication_evidence(build, detail, review, forward, reverse)

    def publication_evidence(self, build, detail, review, forward, reverse):
        return {'schema_version': 1, 'channel': 'ios_testflight', 'app_id': self.module.APP_ID,
                'version': self.version, 'source_sha': self.source_sha, 'build_number': self.number,
                'marketing_version': self.marketing_version, 'build_id': build['id'],
                'ipa_sha256': self.state['ios']['ipa_sha256'], 'available_at': datetime.now(timezone.utc).isoformat(),
                'availability': {'processing_state': 'VALID', 'expired': False, 'detail': detail,
                    'review': review, 'groups_forward': sorted(forward), 'groups_reverse': sorted(reverse)}}

    def observe(self, build, record=True):
        build_id = build['id']
        detail = self.api.request('GET', f'builds/{build_id}/buildBetaDetail').get('data')
        review = self.api.request('GET', f'builds/{build_id}/betaAppReviewSubmission').get('data')
        attached = [g for g, ids in self.members.items() if build_id in ids]
        ready = not build['attributes'].get('expired') and build['attributes'].get('processingState') == 'VALID' and available(detail, review, attached, self.config['group_ids'])
        proof = None
        if ready and record and build['attributes']['version'] == self.number and self.state.get('ios', {}).get('ipa_sha256'):
            reverse = {g['id'] for g in self.api.all(f'betaGroups?filter%5Bbuilds%5D={build_id}&limit=200')}
            marketing = self.api.request('GET', f'builds/{build_id}/preReleaseVersion')['data']['attributes']['version']
            ready = set(self.config['group_ids']) <= reverse and marketing == self.marketing_version
            if ready:
                proof = self.publication_evidence(build, detail, review, attached, reverse)
        if ready and record:
            releases = self.ledger.setdefault('releases', [])
            row = next((r for r in releases if str(r.get('build_number')) == build['attributes']['version'] and r.get('app_id') == self.module.APP_ID), None)
            if row is None:
                row = {'app_id': self.module.APP_ID, 'build_number': build['attributes']['version']}
                releases.append(row)
            if not row.get('successfully_published'):
                if build['attributes']['version'] == self.number:
                    row.update(version=self.version, marketing_version=self.marketing_version, source_sha=self.source_sha)
                    if proof:
                        row['ipa_sha256'] = proof['ipa_sha256']
                elif not row.get('marketing_version') and not row.get('version'):
                    row['marketing_version'] = self.api.request(
                        'GET', f'builds/{build_id}/preReleaseVersion')['data']['attributes']['version']
                row.update(successfully_published=True, success_beijing_date=self.day,
                           first_observed_available_at=datetime.now(ZoneInfo('Asia/Shanghai')).isoformat())
                write_json(self.ledger_path, self.ledger)
            if proof:
                self.state.setdefault('first_publication', proof)
        return ready

    def quota(self, record=True):
        # Observe every ready build before changing availability, including pending reviews.
        for build in self.builds:
            self.observe(build, record)
        return any(r.get('successfully_published') and r.get('success_beijing_date') == self.day
                   and str(r.get('build_number')) != self.number
                   for r in self.ledger.get('releases', []) if r.get('app_id') == self.module.APP_ID)

    def declaration(self, info=None):
        key = 'ITSAppUsesNonExemptEncryption'
        path = self.root / 'mobile/ios/App/App/Info.plist'
        source = plistlib.loads(path.read_bytes()) if path.exists() else {}
        if key in source and not isinstance(source[key], bool):
            raise ValueError('Source export-compliance declaration must be boolean')
        value = source.get(key) if info is None else info.get(key)
        if info is not None:
            if key in info and not isinstance(value, bool):
                raise ValueError('IPA export-compliance declaration must be boolean')
            if key in source and value is not source[key]:
                raise ValueError('IPA export-compliance declaration differs from source')
        statements = [self.config.get('compliance', {})]
        statements.extend(self.ledger.get('compliance_confirmations', []))
        confirmed = [statement['uses_non_exempt'] for statement in statements
                     if statement.get('source_sha') == self.source_sha
                     and str(statement.get('build_number')) == self.number
                     and statement.get('version', self.version) == self.version
                     and statement.get('confirmed_by')
                     and isinstance(statement.get('uses_non_exempt'), bool)]
        if isinstance(value, bool):
            confirmed.append(value)
        if len(set(confirmed)) > 1:
            raise ValueError('Conflicting current-build export-compliance declarations')
        if confirmed:
            return confirmed[0]
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
                '-archivePath', str(archive), 'archive', f'MARKETING_VERSION={self.marketing_version}',
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
        if info.get('CFBundleShortVersionString') != self.marketing_version or str(info.get('CFBundleVersion')) != self.number:
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
                row.update(status='published', delivered_version=self.version,
                           delivered_marketing_version=self.marketing_version, delivered_build_number=self.number)
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
            result.update(marketing_version=self.marketing_version, build_number=self.number)
            if not check:
                self.state['ios'] = result
                if result['status'] == 'published' and result.get('ipa_sha256'):
                    verify_public_receipt(self.version, self.source_sha, self.state, self.ledger)
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
            if marketing != self.marketing_version:
                raise ValueError('Remote build marketing version mismatch')
        if build and self.observe(build, not check):
            return {**existing, 'status': 'published', 'source_sha': self.source_sha, 'build_id': build['id'], 'group_ids': self.config['group_ids']}
        if self.quota(not check) and not self.urgent_reason:
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
            source_options = {'urgent_reason': self.urgent_reason} if self.urgent_reason else {}
            if source_preflight(self.root, self.version, ('ios',), **source_options) != self.source_sha:
                raise ValueError('Source changed during iOS archive; refusing upload')
            self.api = self.module.Client()
            value = self.verify_ipa(ipa)
            existing = {'status': 'signed', 'ipa': str(ipa), 'ipa_sha256': sha256(ipa),
                        'source_sha': self.source_sha, 'uses_non_exempt': value,
                        'marketing_version': self.marketing_version, 'build_number': self.number}
            self.state['ios'] = existing
            # Durable checkpoint before upload: the outer driver will persist on failure too.
            write_json(home() / 'releases' / f'{self.version}.json', self.state)
            accepted = self.snapshot()  # recheck immediately before the first upload
            if self.quota() and not self.urgent_reason:
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
                'build_number': self.number, 'version': self.version,
                'marketing_version': self.marketing_version, 'source_sha': self.source_sha,
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
        build = self.snapshot()
        detail = self.api.request('GET', f'builds/{build["id"]}/buildBetaDetail').get('data')
        if (detail or {}).get('attributes', {}).get('externalBuildState') == 'READY_FOR_BETA_SUBMISSION':
            self.module.submit_review(self.api, build['id'])
            build = self.snapshot()
        return {**existing, 'status': 'published' if self.observe(build) else 'waiting_beta_review',
                'build_id': build['id'], 'group_ids': self.config['group_ids']}
