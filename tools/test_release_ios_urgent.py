"""An explicit urgent release skips only the daily availability limit."""
import unittest
from unittest.mock import patch

import release
import release_ios
import test_release as fixtures


class IOSUrgentReleaseTests(unittest.TestCase):
    setUp = fixtures.IosTests.setUp
    save_config = fixtures.IosTests.save_config
    build = fixtures.IosTests.build
    ipa = fixtures.IosTests.ipa
    receipt = fixtures.IosTests.receipt

    def ios(self, reason=None):
        return release_ios.IOSRelease(self.root, '0.1.20', 'sha', self.state,
                                      urgent_reason=reason)

    def delivered_today(self):
        ios = self.ios()
        release.write_json(ios.ledger_path, {'releases': [{'app_id': 'app', 'build_number': '115',
            'successfully_published': True, 'success_beijing_date': ios.day}]})

    def test_urgent_reason_must_be_explicit_nonblank_text(self):
        for value in ['', ' \n ', False, 1]:
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, 'urgent reason'):
                self.ios(value)
        self.assertNotIn('urgent_reason', self.state)

    def test_saved_reason_does_not_authorize_a_later_invocation(self):
        self.delivered_today()
        self.state['urgent_reason'] = 'previous authorized release'
        self.assertEqual(self.ios().publish()['status'], 'waiting_daily_limit')
        self.assertFalse(self.actions)
        self.assertFalse(any('altool' in c.args[0] for c in self.run.call_args_list))

    def test_urgent_preflight_preserves_remote_and_ledger(self):
        self.delivered_today()
        path = self.root / 'ios-signing/testflight-releases.json'
        before = path.read_bytes()
        ios = self.ios('  mobile loading regression  ')
        self.assertEqual(ios.publish(check=True)['status'], 'preflight_passed')
        self.assertEqual(self.state['urgent_reason'], 'mobile loading regression')
        self.assertEqual(path.read_bytes(), before)
        self.assertFalse((self.root / 'releases/0.1.20.json').exists())
        self.assertFalse(self.actions)

    def test_urgent_upload_rechecks_quota_and_checkpoints_reason_before_upload(self):
        self.delivered_today()
        ios = self.ios('mobile loading regression')
        ipa = self.ipa(flag=False)
        original_quota = ios.quota
        def command(args, **kwargs):
            if 'altool' in args:
                checkpoint = release.read_json(self.root / 'releases/0.1.20.json')
                self.assertEqual(checkpoint['urgent_reason'], 'mobile loading regression')
                self.assertEqual(checkpoint['source_sha'], 'sha')
                self.assertEqual(checkpoint['ios']['status'], 'upload_attempted')
            return ''
        self.run.side_effect = command
        with patch.object(ios, 'build_ipa', return_value=ipa), \
                patch.object(ios, 'verify_ipa', return_value=False), \
                patch.object(ios, 'quota', wraps=original_quota) as quota, \
                patch.object(release_ios, 'source_preflight', return_value='sha') as source:
            self.assertEqual(ios.publish()['status'], 'waiting_upload_confirmation')
        self.assertEqual(quota.call_count, 2, 'urgent releases must still reconcile both snapshots')
        source.assert_called_once_with(self.root, '0.1.20', ('ios',),
                                       urgent_reason='mobile loading regression')
        self.assertEqual(len([c for c in self.run.call_args_list if 'altool' in c.args[0]]), 1)
        saved = release.read_json(self.root / 'releases/0.1.20.json')
        self.assertEqual(saved['urgent_reason'], 'mobile loading regression')

    def test_urgent_reason_does_not_bypass_pending_candidate(self):
        self.delivered_today()
        self.apple.builds = [self.build('119')]
        ios = self.ios('mobile loading regression')
        with patch.object(ios, 'build_ipa') as archive, self.assertRaisesRegex(ValueError, 'pending'):
            ios.publish_once()
        archive.assert_not_called()
        self.assertFalse(self.actions)

    def test_urgent_reason_does_not_bypass_build_number_or_compliance(self):
        self.delivered_today()
        self.apple.builds = [self.build('121')]
        self.apple.review_state = 'REJECTED'
        with self.assertRaisesRegex(ValueError, 'increase'):
            self.ios('mobile loading regression').publish_once()
        self.apple.builds = []
        self.config.pop('compliance'); self.save_config()
        ios = self.ios('mobile loading regression')
        with patch.object(ios, 'build_ipa') as archive, self.assertRaisesRegex(ValueError, 'declaration'):
            ios.publish_once()
        archive.assert_not_called()
        self.assertFalse(self.actions)

    def test_urgent_reason_does_not_bypass_candidate_source_or_group_checks(self):
        self.delivered_today()
        self.apple.builds = [self.build()]
        with self.assertRaisesRegex(ValueError, 'source receipt'):
            self.ios('mobile loading regression').publish_once()
        self.apple.builds = []
        self.config['group_ids'] = ['missing']; self.save_config()
        with self.assertRaisesRegex(ValueError, 'groups'):
            self.ios('mobile loading regression').publish_once()
        self.assertFalse(self.actions)

    def test_urgent_reason_does_not_bypass_signature_or_source_recheck(self):
        self.delivered_today()
        ios = self.ios('mobile loading regression')
        ipa = self.ipa(flag=False)
        with patch.object(ios, 'build_ipa', return_value=ipa), \
                patch.object(ios, 'verify_ipa', side_effect=ValueError('invalid signature')):
            with self.assertRaisesRegex(ValueError, 'signature'):
                ios.publish_once()
        with patch.object(ios, 'build_ipa', return_value=ipa), \
                patch.object(release_ios, 'source_preflight', return_value='changed'):
            with self.assertRaisesRegex(ValueError, 'Source changed'):
                ios.publish_once()
        self.assertFalse(any('altool' in c.args[0] for c in self.run.call_args_list))
        self.assertFalse(self.actions)
