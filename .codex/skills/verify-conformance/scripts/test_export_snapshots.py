#!/usr/bin/env python3
import json
from pathlib import Path
import tempfile
import unittest
from conformance import sha, digest
from export_snapshots import render, export


class ExportTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.source = self.root / 'artifact'
        self.source.mkdir()
        (self.source / 'contents').mkdir()
        self.data = {'schema': 1, 'complete': True, 'filter': '^TestLocal$', 'exit_code': 1,
                     'identity': {'typescript_go_git_rev': 'abc', 'contract': {},
                                  'conformance_files': [
                                      'tsc/testdata/tests/cases/conformance/types/sample.ts']},
                     'case_counts': {'fail': 1}, 'tests': {}}
        (self.source / 'invocation.json').write_text(json.dumps({'prepared': str(self.root / 'build')}))
        (self.source / 'test.jsonl').write_text('')
        self.capture('old actual\n')

    def capture(self, actual):
        expected = 'expected\n'
        for value in (expected, actual):
            (self.source / 'contents' / (sha(value.encode()) + '.txt')).write_text(value)
        self.data['tests']['TestLocal/sample.ts/error'] = {
            'status': 'fail', 'message_sha256': digest([]), 'captures': [{
                'path': 'conformance/sample.errors.txt',
                'reference': sha(expected.encode()), 'actual': sha(actual.encode())}]}
        self.data['tests']['TestLocal/sample.ts'] = {
            'status': 'fail', 'message_sha256': digest([]), 'captures': []}
        (self.source / 'snapshot.json').write_text(json.dumps(self.data))

    def test_same_count_changed_output_is_readable(self):
        a = render(self.source)
        self.capture('new actual\n')
        b = render(self.source)
        self.assertNotEqual(a, b)
        self.assertIn('+new actual', '\n'.join(b.values()))
        self.assertIn('-expected', '\n'.join(b.values()))

    def test_summary_reports_category_counts_and_rates(self):
        summary = render(self.source)['summary.snap']
        self.assertIn('types Summary:', summary)
        self.assertIn('Passed    : 0/1 (0.00%)', summary)
        self.assertIn('Failed    : 1/1 (100.00%)', summary)
        self.assertIn('Skipped   : 0/1 (0.00%)', summary)

    def test_inconsistent_category_summary_is_rejected(self):
        self.data['category_counts'] = {'types': {'pass': 1}}
        (self.source / 'snapshot.json').write_text(json.dumps(self.data))
        with self.assertRaisesRegex(ValueError, 'does not match'):
            render(self.source)

    def test_crlf_output_exports_lf_text_and_checks_equal(self):
        self.capture('changed\r\n')
        out = self.root / 'snapshots'
        export(self.source, out)
        self.assertTrue(all(b'\r' not in p.read_bytes() for p in out.iterdir()))
        self.assertEqual(export(self.source, out, True), 0)

    def test_check_does_not_write(self):
        out = self.root / 'snapshots'
        self.assertEqual(export(self.source, out), 0)
        before = {p.name: p.read_bytes() for p in out.iterdir()}
        self.assertEqual(export(self.source, out, True), 0)
        self.capture('changed\n')
        self.assertEqual(export(self.source, out, True), 1)
        self.assertEqual(before, {p.name: p.read_bytes() for p in out.iterdir()})

    def test_incomplete_capture_cannot_replace_snapshot(self):
        self.data['complete'] = False
        (self.source / 'snapshot.json').write_text(json.dumps(self.data))
        with self.assertRaises(ValueError):
            render(self.source)

    def test_missing_content_is_rejected(self):
        next((self.source / 'contents').iterdir()).unlink()
        with self.assertRaises(OSError):
            render(self.source)

    def test_unmanaged_file_is_preserved(self):
        out = self.root / 'snapshots'
        out.mkdir()
        path = out / 'other.snap'
        path.write_text('unrelated')
        with self.assertRaises(ValueError):
            export(self.source, out)
        self.assertEqual(path.read_text(), 'unrelated')


if __name__ == '__main__':
    unittest.main()
