"""TestFlight availability cross-checks only Apple's supported group reads."""
import unittest
from unittest.mock import patch
from urllib.parse import unquote

import release
import release_ios
import test_release_public_recovery as fixtures


class SupportedGroupReadTests(unittest.TestCase):
    setUp = fixtures.PublicRecoveryTests.setUp
    git = fixtures.PublicRecoveryTests.git
    persist = fixtures.PublicRecoveryTests.persist

    def all(self, route):
        self.reject_unsupported_group_read('GET', route)
        return fixtures.PublicRecoveryTests.all(self, route)

    def request(self, method, route):
        self.reject_unsupported_group_read(method, route)
        return fixtures.PublicRecoveryTests.request(self, method, route)

    def reject_unsupported_group_read(self, method, route):
        decoded = unquote(route)
        if method == 'GET' and ((decoded.startswith('builds/') and '/betaGroups' in decoded)
                or ('filter[app]' in decoded and 'filter[builds]' in decoded)):
            raise AssertionError('Unsupported Apple GET: ' + route)

    def ios(self):
        return release_ios.IOSRelease(self.root, '1.2.3', self.sha, self.state)

    def test_fake_refuses_both_undocumented_group_gets(self):
        for route in ('builds/build-one/betaGroups?limit=200',
                      'builds/build-one/relationships/betaGroups?limit=200',
                      'betaGroups?filter[app]=app&filter[builds]=build-one&limit=200',
                      'betaGroups?filter%5Bapp%5D=app&filter%5Bbuilds%5D=build-one&limit=200'):
            with self.subTest(route=route), self.assertRaisesRegex(AssertionError, 'Unsupported Apple GET'):
                self.all(route)
            with self.subTest(route=route), self.assertRaisesRegex(AssertionError, 'Unsupported Apple GET'):
                self.request('GET', route)

    def test_legacy_bootstrap_uses_supported_positive_group_membership(self):
        before = {p: p.read_bytes() for p in (self.state_path, self.ledger_path)}
        proof = self.ios().verify_publication()
        self.assertEqual(proof['schema_version'], 1)
        self.assertEqual(proof['availability']['groups_forward'], sorted(self.groups))
        self.assertEqual(proof['availability']['groups_reverse'], sorted(self.groups))
        self.assertEqual(before, {p: p.read_bytes() for p in before})
        queried = {call[1] for call in self.calls if call[0] == 'GET' and call[1].startswith('betaGroups/')}
        self.assertEqual(queried, {f'betaGroups/{group}/builds?limit=200' for group in self.groups})
        self.assertIn(['GET', f'betaGroups?filter%5Bbuilds%5D={self.build["id"]}&limit=200'], self.calls)

    def test_read_only_check_and_recorded_success_use_the_same_supported_groups(self):
        self.state['ios']['status'] = 'signed'
        self.ledger['releases'][0]['successfully_published'] = False
        self.persist()
        ios = self.ios()
        before = {p: p.read_bytes() for p in (self.state_path, self.ledger_path)}
        with patch.object(release_ios, 'run', return_value=''):
            self.assertEqual(ios.publish_once(check=True)['status'], 'published')
        self.assertEqual(before, {p: p.read_bytes() for p in before})
        self.assertNotIn('first_publication', ios.state)
        with patch.object(release_ios, 'run', return_value=''), patch.object(ios, 'complete_delivery'):
            self.assertEqual(ios.publish()['status'], 'published')
        saved = release.read_json(self.state_path)
        proof = saved['first_publication']
        self.assertEqual(proof['schema_version'], 1)
        self.assertEqual(release_ios.verify_public_receipt('1.2.3', self.sha, saved,
            release.read_json(self.ledger_path)), proof)
        self.assertTrue(release.read_json(self.ledger_path)['releases'][0]['successfully_published'])

    def test_missing_any_configured_group_refuses_bootstrap_and_success_record(self):
        for group in self.groups:
            with self.subTest(missing=group):
                self.members = set(self.groups) - {group}
                ios = self.ios()
                with self.assertRaisesRegex(ValueError, 'installability'):
                    ios.verify_publication()
                ios.snapshot()
                self.assertFalse(ios.observe(self.build, record=True))
                self.assertNotIn('first_publication', self.state)

    def test_missing_reverse_group_refuses_record_but_check_stays_read_only(self):
        for group in self.groups:
            with self.subTest(missing=group):
                self.reverse_members = set(self.groups) - {group}
                ios = self.ios()
                with self.assertRaisesRegex(ValueError, 'installability'):
                    ios.verify_publication()
                ios.snapshot()
                before = self.ledger_path.read_bytes()
                self.calls.clear()
                self.assertTrue(ios.observe(self.build, record=False))
                self.assertFalse(any('filter%5Bbuilds%5D' in call[1] for call in self.calls))
                self.assertFalse(ios.observe(self.build, record=True))
                self.assertEqual(self.ledger_path.read_bytes(), before)
                self.assertNotIn('first_publication', self.state)

    def test_filtered_groups_are_read_as_a_complete_paginated_collection(self):
        query = f'betaGroups?filter%5Bbuilds%5D={self.build["id"]}&limit=200'
        pages = {query: {'data': [{'id': group} for group in self.groups[:3]],
                         'links': {'next': query + '&cursor=second'}},
                 query + '&cursor=second': {'data': [{'id': self.groups[3]}], 'links': {}}}
        original = self.all
        visited = []

        def all_pages(route):
            if route != query:
                return original(route)
            result = []
            while route:
                visited.append(route)
                page = pages[route]
                result.extend(page['data'])
                route = page['links'].get('next')
            return result

        with patch.object(self, 'all', side_effect=all_pages):
            proof = self.ios().verify_publication()
        self.assertEqual(visited, list(pages))
        self.assertEqual(proof['availability']['groups_reverse'], sorted(self.groups))

    def test_failed_supported_reverse_read_never_records_success_or_replaces_proof(self):
        self.state['ios']['status'] = 'signed'
        self.ledger['releases'][0]['successfully_published'] = False
        self.persist()
        ios = self.ios()
        ios.snapshot()
        before = self.ledger_path.read_bytes()
        original = self.all

        def unavailable(route):
            if route.startswith('betaGroups?filter%5Bbuilds%5D='):
                raise RuntimeError('offline group read unavailable')
            return original(route)

        with patch.object(self, 'all', side_effect=unavailable), \
             self.assertRaisesRegex(RuntimeError, 'group read unavailable'):
            ios.observe(self.build, record=True)
        self.assertEqual(self.ledger_path.read_bytes(), before)
        self.assertNotIn('first_publication', self.state)


if __name__ == '__main__':
    unittest.main()
