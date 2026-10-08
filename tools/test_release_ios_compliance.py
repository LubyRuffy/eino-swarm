"""An authorized app declaration survives routine version changes."""
from pathlib import Path
import plistlib
import unittest
from unittest.mock import patch

import release
import release_ios
import test_release as fixtures


class IOSComplianceTests(unittest.TestCase):
    setUp = fixtures.IosTests.setUp
    save_config = fixtures.IosTests.save_config
    ios = fixtures.IosTests.ios

    def source_declaration(self, value):
        path = self.root / 'mobile/ios/App/App/Info.plist'
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(plistlib.dumps({'ITSAppUsesNonExemptEncryption': value}))

    def test_app_keeps_the_user_authorized_declaration_in_source(self):
        path = Path(__file__).resolve().parents[1] / 'mobile/ios/App/App/Info.plist'
        self.assertIs(plistlib.loads(path.read_bytes()).get('ITSAppUsesNonExemptEncryption'), False)

    def test_new_source_and_build_preflight_use_app_declaration_without_asking_again(self):
        self.source_declaration(False)
        # The old per-build record remains present but is not authority for the new build.
        ios = release_ios.IOSRelease(self.root, '0.1.21', 'new-source', {})
        with patch.object(ios, 'build_ipa') as archive:
            self.assertEqual(ios.publish_once(check=True)['status'], 'preflight_passed')
        archive.assert_not_called()
        self.assertFalse(ios.ledger_path.exists(), 'read-only preflight must not create a receipt')
        self.assertFalse(self.actions)

    def test_authorized_source_boolean_does_not_depend_on_old_false_records(self):
        self.config.pop('compliance'); self.save_config()
        for value in [False, True]:
            with self.subTest(value=value):
                self.source_declaration(value)
                self.assertIs(self.ios().declaration(), value)

    def test_invalid_source_declaration_refuses_archive(self):
        for value in ['false', 0]:
            with self.subTest(value=value):
                self.source_declaration(value)
                ios = self.ios()
                with patch.object(ios, 'build_ipa') as archive, self.assertRaisesRegex(ValueError, 'boolean'):
                    ios.publish_once()
                archive.assert_not_called()
        self.assertFalse(any('altool' in c.args[0] for c in self.run.call_args_list))

    def test_source_and_exact_record_conflict_refuses_archive(self):
        self.source_declaration(True)
        ios = self.ios()
        with patch.object(ios, 'build_ipa') as archive, self.assertRaisesRegex(ValueError, 'Conflicting'):
            ios.publish_once()
        archive.assert_not_called()
        self.assertFalse(self.actions)

    def test_signed_ipa_must_retain_source_declaration(self):
        self.source_declaration(False)
        for info in [{}, {'ITSAppUsesNonExemptEncryption': True}, {'ITSAppUsesNonExemptEncryption': 'false'}]:
            with self.subTest(info=info), self.assertRaisesRegex(ValueError, 'declaration'):
                self.ios().declaration(info)
        self.assertIs(self.ios().declaration({'ITSAppUsesNonExemptEncryption': False}), False)

    def test_ipa_boolean_cannot_hide_conflicting_exact_confirmation(self):
        release.write_json(self.root / 'ios-signing/testflight-releases.json', {
            'compliance_confirmations': [{'version': '0.1.20', 'build_number': '120',
                'source_sha': 'sha', 'confirmed_by': 'authorized reviewer', 'uses_non_exempt': True}]})
        with self.assertRaisesRegex(ValueError, 'Conflicting'):
            self.ios().declaration({'ITSAppUsesNonExemptEncryption': False})
