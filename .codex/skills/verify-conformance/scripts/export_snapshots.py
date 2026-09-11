#!/usr/bin/env python3
"""Export captured conformance evidence to deterministic, Git-reviewable snapshots."""
import argparse
from collections import defaultdict
import difflib
import json
from pathlib import Path
import re
import sys
from conformance import read, sha, normalize_output, digest, summarize_categories

HEADER = '---\nsource: tsc/internal/testrunner/compiler_runner_test.go\nformat: conformance-text-v1\n---\n'


def percentage(count, total):
    return f'{(count * 100 / total):.2f}%'


def render(source):
    data = read(source / 'snapshot.json')
    if data['schema'] != 1 or not data['complete']:
        raise ValueError('Only complete schema-1 captures can be exported')
    invocation = read(source / 'invocation.json')
    root = Path(invocation['prepared']) / '.runtime/source'
    messages = defaultdict(list)
    for line in (source / 'test.jsonl').read_text().splitlines():
        row = json.loads(line)
        if row.get('Test') and row.get('Output'):
            text = normalize_output(row['Output'], root, source)
            if text:
                messages[row['Test']].append(text)
    files = {}
    info = data['identity']
    lines = [HEADER, 'commit: ' + info['typescript_go_git_rev'],
             'filter: ' + data['filter'], 'suite exit code: ' + str(data['exit_code']),
             'capture: complete', 'input/reference/harness contract: ' + digest(info['contract']),
             '', 'File/configuration summary:']
    for status, count in sorted(data['case_counts'].items()):
        lines.append(f'{status}: {count}')
    cases = {key: value for key, value in data['tests'].items()
             if key.startswith('TestLocal/') and key.count('/') == 1}
    derived_category_counts = summarize_categories(cases, info)
    category_counts = data.get('category_counts', derived_category_counts)
    if category_counts != derived_category_counts:
        raise ValueError('Category summary does not match captured cases')
    lines += ['', 'Category summary:']
    for category, counts in sorted(category_counts.items()):
        total = sum(counts.values())
        lines += ['', f'{category} Summary:']
        for label, status in [('Passed', 'pass'), ('Failed', 'fail'), ('Skipped', 'skip'),
                              ('Unfinished', 'unfinished')]:
            count = counts.get(status, 0)
            lines.append(f'{label:<10}: {count}/{total} ({percentage(count, total)})')
    files['summary.snap'] = '\n'.join(lines) + '\n'
    groups = defaultdict(list)
    for name, item in sorted(data['tests'].items()):
        parts = name.split('/')
        stage = parts[2] if len(parts) > 2 else 'cases'
        if not re.fullmatch(r'[a-z_]+', stage):
            raise ValueError('Unexpected stage: ' + stage)
        lines = [f"{item['status'].upper()}: {name}"]
        if item['status'] != 'pass':
            if digest(messages[name]) != item['message_sha256']:
                raise ValueError('Message integrity mismatch: ' + name)
            lines.extend(messages[name])
        for capture in item['captures']:
            contents = []
            for key in ('reference', 'actual'):
                value = capture[key]
                if not re.fullmatch('[0-9a-f]{64}', value):
                    raise ValueError('Invalid content digest')
                raw = (source / 'contents' / (value + '.txt')).read_bytes()
                if sha(raw) != value:
                    raise ValueError('Content integrity mismatch: ' + name)
                contents.append(raw.decode('utf-8'))
            # Fingerprint every output, including successful comparisons: changed
            # successful output must remain visible even if references also change.
            lines.append('baseline: ' + capture['path'])
            lines.append('reference: ' + capture['reference'])
            lines.append('actual: ' + capture['actual'])
            if contents[0] != contents[1]:
                lines.extend(line.rstrip('\r\n') for line in difflib.unified_diff(
                    contents[0].splitlines(keepends=True), contents[1].splitlines(keepends=True),
                    fromfile='expected/' + capture['path'], tofile='actual/' + capture['path'], n=3))
        groups[stage].append('\n'.join(lines) + '\n')
    # Bucket by case name's stable digest. No moving size-based boundaries, and
    # large type/symbol stages stay reviewable without thousands of tiny files.
    for stage, blocks in sorted(groups.items()):
        buckets = defaultdict(list)
        for block in blocks:
            name = block.split('\n', 1)[0].split(': ', 1)[1]
            buckets[sha(name.encode())[:1]].append(block)
        for bucket, entries in sorted(buckets.items()):
            files[f'{stage}-{bucket}.snap'] = HEADER + '\n' + '\n'.join(entries)
    return files


def export(source, out, check=False):
    files = render(source)
    existing = {p.name: p.read_bytes().decode('utf-8') for p in out.glob('*.snap')}
    # Refuse to overwrite unrelated snapshots.
    if any(not value.startswith(HEADER) for value in existing.values()):
        raise ValueError('Destination contains unmanaged .snap files')
    changed = sorted(k for k in existing.keys() | files.keys() if existing.get(k) != files.get(k))
    if check:
        for name in changed:
            print('changed: ' + name)
        return 1 if changed else 0
    out.mkdir(parents=True, exist_ok=True)
    for name in existing.keys() - files.keys():
        (out / name).unlink()
    for name, content in files.items():
        (out / name).write_text(content)
    print(f'Exported {len(files)} snapshots; {len(changed)} changed')
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True, help='Captured artifact directory')
    parser.add_argument('--out', type=Path, required=True, help='Git-tracked snapshot directory')
    parser.add_argument('--check', action='store_true', help='Compare without writing; exit 1 on differences')
    args = parser.parse_args()
    try:
        return export(args.source.resolve(), args.out.resolve(), args.check)
    except (OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
