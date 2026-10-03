"""TestFlight trains keep their marketing version across release batches."""
import unittest
from unittest.mock import patch

import release
import release_ios
import test_release as fixtures


class TestFlightVersionTests(unittest.TestCase):
    setUp = fixtures.IosTests.setUp
    save_config = fixtures.IosTests.save_config
    ios = fixtures.IosTests.ios
    build = fixtures.IosTests.build
    ipa = fixtures.IosTests.ipa
    receipt = fixtures.IosTests.receipt

    def published(self, rows):
        release.write_json(self.root / 'ios-signing/testflight-releases.json', {'releases': rows})

    def test_new_batch_reuses_last_successful_train_and_increases_build(self):
        self.published([
            {'app_id': 'app', 'version': '0.1.18', 'build_number': '118', 'successfully_published': True},
            {'app_id': 'app', 'version': '0.1.19', 'marketing_version': '0.1.18',
             'build_number': '119', 'successfully_published': True},
            {'app_id': 'other', 'version': '9.9.9', 'build_number': '999', 'successfully_published': True},
            {'app_id': 'app', 'version': '0.2.0', 'build_number': '200', 'successfully_published': False},
        ])
        ios = self.ios()
        ios.build_ipa()
        args = next(c.args[0] for c in self.run.call_args_list if 'archive' in c.args[0])
        self.assertIn('MARKETING_VERSION=0.1.18', args)
        self.assertIn('CURRENT_PROJECT_VERSION=120', args)
        self.assertEqual(ios.state['ios']['marketing_version'], '0.1.18')

    def test_explicit_train_only_applies_to_new_candidate(self):
        self.config['marketing_version'] = '1.0.0'; self.save_config()
        ios = self.ios()
        self.assertEqual(ios.marketing_version, '1.0.0')
        self.config['marketing_version'] = '2.0.0'; self.save_config()
        self.assertEqual(self.ios().marketing_version, '1.0.0')
        self.assertNotIn('source_sha', self.state['ios'])

    def test_legacy_source_receipt_keeps_original_version_after_config_change(self):
        self.receipt()
        self.config['marketing_version'] = '1.0.0'; self.save_config()
        self.assertEqual(self.ios().marketing_version, '0.1.20')

    def test_legacy_ledger_receipt_keeps_original_train(self):
        self.published([{'app_id': 'app', 'version': '0.1.20', 'source_sha': 'sha',
                         'build_number': '120', 'marketing_version': '0.1.18'}])
        self.assertEqual(self.ios().marketing_version, '0.1.18')

    def test_retained_ipa_keeps_actual_marketing_version(self):
        ipa = self.ipa(version='0.1.18', flag=False)
        self.state['ios'] = {'ipa': str(ipa), 'ipa_sha256': release.sha256(ipa), 'source_sha': 'sha'}
        self.config['marketing_version'] = '1.0.0'; self.save_config()
        self.assertEqual(self.ios().marketing_version, '0.1.18')

    def test_invalid_train_is_rejected_before_archiving(self):
        for value in ['bad', '', '1.2.3-dirty', None, 123]:
            with self.subTest(value=value):
                self.config['marketing_version'] = value; self.save_config()
                self.state.pop('ios', None)
                with self.assertRaisesRegex(ValueError, 'marketing version'):
                    self.ios()

    def test_available_build_without_review_submission_is_delivery(self):
        detail = {'attributes': {'internalBuildState': 'READY_FOR_BETA_TESTING',
                                 'externalBuildState': 'IN_BETA_TESTING'}}
        self.assertTrue(release_ios.available(detail, None, ['internal', 'external'], ['internal', 'external']))
        for status in ['WAITING_FOR_REVIEW', 'IN_REVIEW', 'REJECTED']:
            self.assertFalse(release_ios.available(detail, {'attributes': {'betaReviewState': status}},
                                                  ['internal', 'external'], ['internal', 'external']))

    def test_group_attachment_can_make_build_available_without_review_post(self):
        self.receipt()
        original = self.apple.request
        def request(method, path, body=None):
            if path.endswith('/betaAppReviewSubmission'):
                return {'data': None}
            return original(method, path, body)
        self.apple.request = request
        def add_group(api, build_id, group):
            self.apple.members[group] = self.apple.builds
            self.apple.ready = True
        self.module.add_group = add_group
        result = self.ios().publish()
        self.assertEqual(result['status'], 'published')
        self.assertNotIn('review', self.actions)
        self.assertEqual(result['marketing_version'], '0.1.20')
        self.assertEqual(result['build_number'], '120')

    def test_wrong_ipa_train_is_rejected_even_when_batch_version_matches(self):
        self.config['marketing_version'] = '0.1.18'; self.save_config()
        with self.assertRaisesRegex(ValueError, 'version/build'):
            self.ios().verify_ipa(self.ipa(flag=False))

    def test_same_train_is_saved_in_upload_receipt(self):
        self.config['marketing_version'] = '0.1.18'; self.save_config()
        ios = self.ios()
        ipa = self.ipa(version='0.1.18', flag=False)
        with patch.object(ios, 'build_ipa', return_value=ipa), patch.object(ios, 'verify_ipa', return_value=False):
            result = ios.publish()
        self.assertEqual(result['marketing_version'], '0.1.18')
        row = release.read_json(ios.ledger_path)['releases'][0]
        self.assertEqual(row['version'], '0.1.20')
        self.assertEqual(row['marketing_version'], '0.1.18')
        self.assertEqual(row['build_number'], '120')

    def test_remote_build_is_checked_against_train_and_keeps_batch_identity(self):
        self.config['marketing_version'] = '0.1.18'; self.save_config()
        self.state['ios'] = {'source_sha': 'sha', 'marketing_version': '0.1.18'}
        self.apple.builds = [self.build()]
        ios = self.ios()
        with self.assertRaisesRegex(ValueError, 'marketing version mismatch'):
            ios.publish_once()
        original = self.apple.request
        def request(method, path, body=None):
            if path.endswith('/preReleaseVersion'):
                return {'data': {'attributes': {'version': '0.1.18'}}}
            return original(method, path, body)
        self.apple.request = request
        self.apple.ready = True
        for group in self.apple.members:
            self.apple.members[group] = self.apple.builds
        ios.ledger['pending_changes'] = [{'batch_version': '0.1.20', 'batch_source_sha': 'sha'}]
        self.assertEqual(ios.publish()['status'], 'published')
        ledger = release.read_json(ios.ledger_path)
        row = ledger['releases'][0]
        self.assertEqual((row['version'], row['marketing_version'], row['source_sha']), ('0.1.20', '0.1.18', 'sha'))
        self.assertEqual(ledger['pending_changes'][0]['delivered_version'], '0.1.20')
        self.assertEqual(ledger['pending_changes'][0]['delivered_marketing_version'], '0.1.18')

    def test_review_in_progress_is_observed_without_resubmitting(self):
        self.receipt()
        original = self.apple.request
        def request(method, path, body=None):
            result = original(method, path, body)
            if path.endswith('/buildBetaDetail'):
                result['data']['attributes']['externalBuildState'] = 'WAITING_FOR_BETA_REVIEW'
            return result
        self.apple.request = request
        self.assertEqual(self.ios().publish()['status'], 'waiting_beta_review')
        self.assertNotIn('review', self.actions)

    def test_new_train_ipa_passes_native_verification_with_original_batch_compliance(self):
        self.config['marketing_version'] = '0.1.18'; self.save_config()
        # Reuse the distribution/profile checks with an IPA on the independent train.
        original = self.ipa
        self.ipa = lambda version='0.1.18', flag=None: original(version=version, flag=flag)
        fixtures.IosTests.test_ipa_validates_version_distribution_identity_profile_and_declaration(self)

    def test_observed_public_build_retains_its_actual_train_for_future_candidates(self):
        self.apple.builds = [self.build('119')]
        self.apple.ready = True
        for group in self.apple.members:
            self.apple.members[group] = self.apple.builds
        original = self.apple.request
        def request(method, path, body=None):
            if path.endswith('/preReleaseVersion'):
                return {'data': {'attributes': {'version': '0.1.18'}}}
            return original(method, path, body)
        self.apple.request = request
        ios = self.ios()
        ios.snapshot()
        self.assertTrue(ios.observe(self.apple.builds[0]))
        row = release.read_json(ios.ledger_path)['releases'][0]
        self.assertEqual(row['marketing_version'], '0.1.18')
        self.state.pop('ios')
        self.assertEqual(self.ios().marketing_version, '0.1.18')
