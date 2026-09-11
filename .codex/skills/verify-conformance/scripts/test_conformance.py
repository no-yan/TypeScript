#!/usr/bin/env python3
"""Regression checks for snapshot comparisons, not compiler correctness tests."""
import copy
import unittest
from conformance import comparison, normalize_output, summarize_categories
from pathlib import Path


def snap(status='pass', actual='a'):
    return {'schema': 1, 'filter': '^TestLocal$', 'complete': True,
            'identity': {'protocol': 'v1', 'helper_sha256': 'h', 'contract': {}, 'go': {},
                         'environment': {}, 'typescript_go_git_rev': 'abc'},
            'tests': {'TestLocal/sample.ts/error': {'status': status, 'message_sha256': None,
                      'captures': [{'path': 'a.errors.txt', 'actual': actual, 'reference': 'r'}]}}}


class ComparisonTests(unittest.TestCase):
    def test_revision_change_alone_is_not_regression(self):
        a, b = snap(), snap()
        b['identity']['typescript_go_git_rev'] = 'def'
        self.assertEqual(comparison(a, b)['verdict'], 'no_regression_observed')

    def test_same_failed_count_changed_content_requires_review(self):
        self.assertEqual(comparison(snap('fail'), snap('fail', 'changed'))['verdict'], 'review_required')

    def test_new_failure(self):
        self.assertEqual(comparison(snap(), snap('fail'))['verdict'], 'regression')

    def test_skip_changes_are_not_silent_success(self):
        for old, new in [('fail', 'skip'), ('skip', 'fail'), ('pass', 'skip')]:
            self.assertEqual(comparison(snap(old), snap(new))['verdict'], 'review_required')

    def test_disappearing_test_and_crash_are_incomparable(self):
        a, b = snap(), snap()
        b['tests'] = {}
        self.assertEqual(comparison(a, b)['verdict'], 'incomparable')
        b = snap(); b['complete'] = False
        self.assertEqual(comparison(a, b)['verdict'], 'incomparable')

    def test_changed_expectations_invalidate_comparison(self):
        b = snap(); b['identity']['contract'] = {'references': 'changed'}
        self.assertEqual(comparison(snap(), b)['verdict'], 'incomparable')

    def test_run_paths_and_goroutine_ids_do_not_change_failure(self):
        self.assertEqual(normalize_output('created by testing.(*T).Run in goroutine 345\n', Path('/tmp/repo')), '')
        self.assertEqual(normalize_output('new baseline at /tmp/run-a/baseline-diffs/x', Path('/tmp/repo'), Path('/tmp/run-a')),
                         normalize_output('new baseline at /tmp/run-b/baseline-diffs/x', Path('/tmp/repo'), Path('/tmp/run-b')))

    def test_assertion_hex_values_are_preserved(self):
        self.assertEqual(normalize_output('    sample_test.go:12: want 0x10 got 0x20\n', Path('/tmp/root')),
                         '    sample_test.go:<line>: want 0x10 got 0x20')

    def test_category_counts_include_configurations(self):
        info = {'conformance_files': [
            'tsc/testdata/tests/cases/conformance/types/sample.ts',
            'tsc/testdata/tests/cases/conformance/root.ts',
        ]}
        cases = {
            'TestLocal/sample.ts': {'status': 'pass'},
            'TestLocal/sample.ts_target=es5': {'status': 'fail'},
            'TestLocal/root.ts': {'status': 'skip'},
        }
        self.assertEqual(summarize_categories(cases, info), {
            '(root)': {'skip': 1},
            'types': {'pass': 1, 'fail': 1},
        })


if __name__ == '__main__':
    unittest.main()
