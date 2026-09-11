#!/usr/bin/env python3
"""Commit-pinned conformance snapshots. Python standard library + Git + Go."""
import argparse
import collections
import contextlib
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import tarfile

SCHEMA = 1
RUNNER = 'tsc/internal/testrunner/compiler_runner_test.go'
BASELINE = 'tsc/internal/testutil/baseline/baseline.go'
PROTOCOL = 'conformance-capture-v1'


def sha(data):
    return hashlib.sha256(data).hexdigest()


def dump(path, value):
    Path(path).write_text(json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + '\n')


def read(path):
    return json.loads(Path(path).read_text())


def command(args, cwd=None):
    return subprocess.check_output(args, cwd=cwd, text=True).strip()


def exclusive(path):
    @contextlib.contextmanager
    def lock():
        with Path(path).open('a') as handle:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
            yield
    return lock()


def run_owned(args, cwd, env, output):
    """Only terminate the process group launched here, even on Ctrl-C."""
    process = None
    try:
        with Path(output).open('wb') as log:
            process = subprocess.Popen(args, cwd=cwd, env=env, stdout=log,
                                       stderr=subprocess.STDOUT, start_new_session=True)
            return process.wait()
    finally:
        if process is not None and process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()


def manifest(root, paths):
    result = {}
    for path in sorted(paths):
        if path.is_file():
            result[str(path.relative_to(root))] = sha(path.read_bytes())
    return result


def contract(root):
    data = root / 'tsc/testdata'
    inputs = manifest(root, (data / 'tests').rglob('*'))
    refs = manifest(root, (data / 'baselines/reference').rglob('*'))
    harness = {}
    for name in ['testrunner', 'testutil', 'repo']:
        harness.update(manifest(root, (root / 'tsc/internal' / name).rglob('*.go')))
    return {'inputs': inputs, 'references': refs, 'harness': harness}


def digest(value):
    return sha(json.dumps(value, sort_keys=True, separators=(',', ':')).encode())


def conformance_categories(info):
    """Map the runner's unique basename to its top-level conformance feature."""
    prefix = 'tsc/testdata/tests/cases/conformance/'
    categories = {}
    for path in info['conformance_files']:
        if not path.startswith(prefix):
            raise ValueError('Unexpected conformance input: ' + path)
        relative = path[len(prefix):]
        basename = Path(relative).name
        category = relative.split('/', 1)[0] if '/' in relative else '(root)'
        if basename in categories:
            raise ValueError('Duplicate conformance basename: ' + basename)
        categories[basename] = category
    return categories


def summarize_categories(cases, info):
    categories = conformance_categories(info)
    counts = collections.defaultdict(collections.Counter)
    # Configuration suffixes are appended to the unique input basename (the
    # current runner uses both spaces and underscores), so prefer longer
    # basenames to distinguish e.g. sample.ts from sample.tsx.
    basenames = sorted(categories, key=len, reverse=True)
    for name, item in cases.items():
        case = name.removeprefix('TestLocal/')
        basename = next((value for value in basenames if case.startswith(value)), None)
        if basename is None:
            raise ValueError('Cannot map conformance case to an input: ' + name)
        counts[categories[basename]][item['status']] += 1
    return {category: dict(statuses) for category, statuses in sorted(counts.items())}


CAPTURE = r'''
// Verification overlay only: record the actual user-visible baseline content.
var conformanceCaptureMu sync.Mutex

func conformanceCapture(t *testing.T, name, actual string, opts Options) {
    out := os.Getenv("CONFORMANCE_VERIFY_OUT")
    if out == "" { t.Fatal("CONFORMANCE_VERIFY_OUT is required") }
    conformanceCaptureMu.Lock()
    defer conformanceCaptureMu.Unlock()
    normalize := func(s string) string {
        return strings.ReplaceAll(s, filepath.Dir(repo.RootPath()), "<REPO>")
    }
    relative := filepath.ToSlash(filepath.Join(opts.Subfolder, name))
    expected, err := os.ReadFile(filepath.Join(referenceRoot, opts.Subfolder, name))
    expectedText := NoContent
    if err == nil { expectedText = string(expected) } else if !os.IsNotExist(err) { t.Fatal(err) }
    save := func(content string) string {
        content = normalize(content)
        sum := sha256.Sum256([]byte(content))
        key := hex.EncodeToString(sum[:])
        dir := filepath.Join(out, "contents")
        if err := os.MkdirAll(dir, 0755); err != nil { t.Fatal(err) }
        if err := os.WriteFile(filepath.Join(dir, key+".txt"), []byte(content), 0644); err != nil { t.Fatal(err) }
        return key
    }
    row := map[string]string{"test": t.Name(), "path": relative, "actual": save(actual), "reference": save(expectedText)}
    f, err := os.OpenFile(filepath.Join(out, "captures.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil { t.Fatal(err) }
    err = json.NewEncoder(f).Encode(row)
    closeErr := f.Close()
    if err != nil { t.Fatal(err) }
    if closeErr != nil { t.Fatal(closeErr) }
}
'''


def replace_once(text, old, new):
    if text.count(old) != 1:
        raise ValueError('Harness changed; review overlay anchor: ' + repr(old))
    return text.replace(old, new, 1)


def prepare(args):
    repo = Path(command(['git', 'rev-parse', '--show-toplevel'], args.repo)).resolve()
    rev = command(['git', 'rev-parse', '--verify', args.revision + '^{commit}'], repo)
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=False)
    runtime = out / '.runtime'
    runtime.mkdir()
    root = runtime / 'source'
    root.mkdir()
    info = {'schema': SCHEMA, 'protocol': PROTOCOL, 'repo_root': str(repo),
            'typescript_go_git_rev': rev, 'tsgolint_git_rev': None,
            'source_tree': command(['git', 'rev-parse', rev + '^{tree}'], repo),
            'helper_sha256': sha(Path(__file__).read_bytes()),
            'created_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'working_tree_changes_excluded': command(['git', 'status', '--porcelain', '-uno'], repo),
            'ready': False}
    dump(out / 'identity.json', info)
    try:
        archive = runtime / 'source.tar'
        with archive.open('wb') as f:
            subprocess.run(['git', 'archive', '--format=tar', rev], cwd=repo, stdout=f, check=True)
        with tarfile.open(archive) as f:
            f.extractall(root, filter='data')
        archive.unlink()
        manifests = contract(root)
        dump(out / 'contract.json', manifests)
        info['contract'] = {name: digest(value) for name, value in manifests.items()}
        info['conformance_files'] = sorted(p for p in manifests['inputs']
            if p.startswith('tsc/testdata/tests/cases/conformance/') and p.endswith(('.ts', '.tsx')))
        if not info['conformance_files']:
            raise ValueError('No conformance inputs')
        # Build and run use the same explicit environment. No inherited Go build flags.
        env = os.environ.copy()
        env.update(GOFLAGS='', GOWORK=str(root / 'go.work'))
        details = json.loads(subprocess.check_output(
            ['go', 'env', '-json', 'GOVERSION', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'GOEXPERIMENT'],
            cwd=root / 'tsc', env=env, text=True))
        info['go'] = details
        info['environment'] = {key: env.get(key, '') for key in
            ['GODEBUG', 'GOMAXPROCS', 'GOGC', 'GOMEMLIMIT', 'TS_TEST_PROGRAM_SINGLE_THREADED']}
        patches = {}
        text = (root / RUNNER).read_text()
        text = replace_once(text, '\t\tNewCompilerBaselineRunner(TestTypeRegression),\n', '')
        patches[RUNNER] = text
        file = 'tsc/internal/testrunner/compiler_runner.go'
        text = (root / file).read_text()
        text = replace_once(text,
            'var localBasePath = filepath.Join(repo.TestDataPath(), "baselines", "local")',
            'var localBasePath = filepath.Join(os.Getenv("CONFORMANCE_VERIFY_OUT"), "baseline-diffs")')
        patches[file] = text
        text = (root / BASELINE).read_text()
        text = replace_once(text, 'import (', 'import (\n"crypto/sha256"\n"encoding/hex"\n"encoding/json"\n"strings"\n"sync"')
        text = replace_once(text, '\tsubfolder := opts.Subfolder', '\tconformanceCapture(t, fileName, actual, opts)\n\tsubfolder := opts.Subfolder')
        text = replace_once(text, 'localRoot     = filepath.Join(repo.TestDataPath(), "baselines", "local")',
                            'localRoot     = filepath.Join(os.Getenv("CONFORMANCE_VERIFY_OUT"), "baseline-diffs")')
        patches[BASELINE] = text + CAPTURE
        replace = {}
        overlaydir = out / 'overlay-sources'
        overlaydir.mkdir()
        for index, (name, text) in enumerate(patches.items()):
            path = overlaydir / (str(index) + '.go')
            path.write_text(text)
            subprocess.run(['gofmt', '-w', str(path)], check=True)
            replace[str(root / name)] = str(path)
        dump(out / 'overlay.json', {'Replace': replace})
        info['overlay_sha256'] = {p.name: sha(p.read_bytes()) for p in overlaydir.iterdir()}
        binary = runtime / 'conformance.test'
        cmd = ['go', 'test', '-c', '-overlay', str(out / 'overlay.json'), '-o', str(binary), './internal/testrunner']
        info['build_command'] = cmd
        code = run_owned(cmd, root / 'tsc', env, out / 'build.log')
        info['build_exit_code'] = code
        if code:
            raise RuntimeError('Build failed; inspect ' + str(out / 'build.log'))
        info['binary_sha256'] = sha(binary.read_bytes())
        info['ready'] = True
        dump(out / 'identity.json', info)
        print(json.dumps({'prepared': str(out), 'revision': rev, 'inputs': len(info['conformance_files'])}))
        return 0
    except BaseException:
        info['ready'] = False
        dump(out / 'identity.json', info)
        shutil.rmtree(runtime)
        raise


def doctor_path(prepared):
    p = Path(prepared).resolve()
    info = read(p / 'identity.json')
    binary = p / '.runtime/conformance.test'
    if not info.get('ready') or not binary.exists():
        raise ValueError('Prepared runtime is not available; prepare a new directory')
    if info['helper_sha256'] != sha(Path(__file__).read_bytes()):
        raise ValueError('Helper changed since prepare')
    if sha(binary.read_bytes()) != info['binary_sha256']:
        raise ValueError('Prepared binary was modified')
    root = p / '.runtime/source'
    current = contract(root)
    if {k: digest(v) for k, v in current.items()} != info['contract']:
        raise ValueError('Prepared inputs/references/harness were modified')
    return p, info


def doctor(args):
    p, info = doctor_path(args.prepared)
    print(json.dumps({'ready': True, 'revision': info['typescript_go_git_rev'], 'prepared': str(p)}))
    return 0


def normalize_output(line, root, evidence=None):
    line = line.replace(str(root), '<REPO>')
    if evidence is not None:
        line = line.replace(str(evidence), '<EVIDENCE>')
    if re.match(r'\s*(=== (RUN|PAUSE|CONT|NAME)|--- (PASS|FAIL|SKIP):)', line):
        return ''
    if re.match(r'\s*(goroutine \d+ .*|created by .* in goroutine \d+)', line):
        return ''
    # Stack frames are volatile. Keep assertions and panic messages, including hex values.
    if re.match(r'\s*(/|<REPO>/).*\.go:\d+(?:\s|$)', line):
        return ''
    if re.match(r'\s*\S+\(.*\)\s*$', line) and ('0x' in line or line.lstrip().startswith(('runtime.', 'testing.'))):
        return line.strip().split('(')[0] + '(...)'
    line = re.sub(r'(\.go):\d+:', r'\1:<line>:', line)
    return line.rstrip()


def summarize(out, info, root, code, run_filter):
    results = {}
    fatal = []
    malformed = 0
    with (out / 'test.jsonl').open(errors='replace') as f:
        for line in f:
            try:
                row = json.loads(line)
            except ValueError:
                malformed += 1
                continue
            name = row.get('Test')
            output = row.get('Output', '')
            if 'fatal error:' in output or 'panic: test timed out' in output:
                fatal.append(output.strip())
            if not name:
                continue
            item = results.setdefault(name, {'status': 'unfinished', 'messages': [], 'captures': []})
            action = row.get('Action')
            if action in ('pass', 'fail', 'skip'):
                item['status'] = action
            if output:
                text = normalize_output(output, root, out)
                if text:
                    item['messages'].append(text)
    capture_path = out / 'captures.jsonl'
    if capture_path.exists():
        for line in capture_path.open():
            row = json.loads(line)
            item = results.setdefault(row['test'], {'status': 'unfinished', 'messages': [], 'captures': []})
            item['captures'].append({k: row[k] for k in ['path', 'actual', 'reference']})
    for item in results.values():
        item['captures'].sort(key=lambda v: (v['path'], v['actual'], v['reference']))
        # Stable failure/skip reasons; raw logs retain complete stack traces.
        item['message_sha256'] = digest(item['messages']) if item['status'] != 'pass' else None
        del item['messages']
    cases = {k: v for k, v in results.items() if k.startswith('TestLocal/') and k.count('/') == 1}
    counts = dict(collections.Counter(v['status'] for v in cases.values()))
    category_counts = summarize_categories(cases, info)
    complete = bool(cases) and not fatal and not malformed and code in (0, 1) and all(v['status'] != 'unfinished' for v in results.values())
    if code == 1 and not any(v['status'] == 'fail' for v in results.values()):
        complete = False
    snapshot = {'schema': SCHEMA, 'identity': info, 'filter': run_filter, 'exit_code': code,
                'complete': complete, 'case_counts': counts, 'category_counts': category_counts,
                'fatal_errors': fatal,
                'malformed_json_lines': malformed, 'tests': results}
    dump(out / 'snapshot.json', snapshot)
    (out / 'report.md').write_text('# Conformance snapshot\n\n' +
        f"Commit: `{info['typescript_go_git_rev']}`\n\nComplete: {complete}; exit code: {code}\n\n" +
        f'File/configuration results: `{json.dumps(counts)}`\n\n' +
        f'Category results: `{json.dumps(category_counts)}`\n\n' +
        'Raw: test.jsonl; actual/expected contents: contents/; structured result: snapshot.json.\n')
    return snapshot


def snapshot(args):
    prepared, info = doctor_path(args.prepared)
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=False)
    root = prepared / '.runtime/source'
    env = os.environ.copy()
    for key, value in info['environment'].items():
        if value:
            env[key] = value
        else:
            env.pop(key, None)
    env['CONFORMANCE_VERIFY_OUT'] = str(out)
    cmd = ['go', 'tool', 'test2json', '-t', '-p', 'conformance',
           str(prepared / '.runtime/conformance.test'), '-test.v=test2json',
           '-test.run=' + args.filter, '-test.count=1', '-test.timeout=' + args.timeout]
    dump(out / 'invocation.json', {'command': cmd, 'prepared': str(prepared), 'filter': args.filter,
                                  'timeout': args.timeout, 'environment': info['environment']})
    with exclusive(prepared / '.runtime/drive.lock'):
        code = 130
        try:
            code = run_owned(cmd, root / 'tsc', env, out / 'test.jsonl')
        finally:
            if (out / 'test.jsonl').exists():
                result = summarize(out, info, root, code, args.filter)
    print(json.dumps({'snapshot': str(out / 'snapshot.json'), 'complete': result['complete'],
                      'counts': result['case_counts'], 'exit_code': code}))
    return 0 if result['complete'] else 2  # Existing failures are snapshot data, not capture failure.


def comparison(old, new):
    incompatible = []
    for key in ['schema', 'filter']:
        if old[key] != new[key]: incompatible.append(key)
    for key in ['protocol', 'helper_sha256', 'contract', 'go', 'environment']:
        if old['identity'][key] != new['identity'][key]: incompatible.append(key)
    if not old['complete'] or not new['complete']: incompatible.append('incomplete snapshot')
    a, b = old['tests'], new['tests']
    groups = collections.defaultdict(list)
    for key in sorted(a.keys() | b.keys()):
        if key not in a: groups['added'].append(key); continue
        if key not in b: groups['removed'].append(key); continue
        x, y = a[key], b[key]
        if x['status'] != y['status']:
            groups[x['status'] + '_to_' + y['status']].append(key)
        if x['captures'] != y['captures']:
            groups['content_changed'].append(key)
        if x['status'] == y['status'] and x['message_sha256'] != y['message_sha256']:
            groups['failure_or_skip_detail_changed'].append(key)
    if groups.get('added') or groups.get('removed'): incompatible.append('test coverage changed')
    regressions = groups.get('pass_to_fail', [])
    coverage = [key for name, keys in groups.items() if '_to_' in name and name not in ('pass_to_fail', 'fail_to_pass') for key in keys]
    review = groups.get('content_changed', []) + groups.get('failure_or_skip_detail_changed', [])
    verdict = 'incomparable' if incompatible else 'regression' if regressions else 'review_required' if coverage or review else 'no_regression_observed'
    return {'schema': SCHEMA, 'verdict': verdict, 'incompatible': incompatible, 'changes': dict(groups),
            'old_commit': old['identity']['typescript_go_git_rev'], 'new_commit': new['identity']['typescript_go_git_rev']}


def compare(args):
    old_path, new_path = Path(args.old).resolve(), Path(args.new).resolve()
    old, new = read(old_path / 'snapshot.json'), read(new_path / 'snapshot.json')
    result = comparison(old, new)
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=False)
    result['snapshots'] = {'old': str(old_path), 'new': str(new_path)}
    dump(out / 'comparison.json', result)
    lines = ['# Conformance comparison', '', 'Verdict: **' + result['verdict'] + '**', '',
             f"`{result['old_commit']}` → `{result['new_commit']}`", '',
             'Comparability issues: ' + ', '.join(result['incompatible']), '',
             '| Change | Test entries (parents/children may overlap) |', '| --- | ---: |']
    for name, values in result['changes'].items(): lines.append(f'| {name} | {len(values)} |')
    lines += ['', 'Exact keys are in comparison.json. Content hashes refer to each snapshot contents/<hash>.txt.',
              'No baseline is accepted and no snapshot is overwritten by comparison.']
    (out / 'report.md').write_text('\n'.join(lines) + '\n')
    print(json.dumps({'verdict': result['verdict'], 'report': str(out / 'report.md')}))
    return {'no_regression_observed': 0, 'regression': 1, 'incomparable': 2, 'review_required': 3}[result['verdict']]


def cleanup(args):
    p = Path(args.prepared).resolve()
    info = read(p / 'identity.json')
    runtime = p / '.runtime'
    if runtime.exists():
        with exclusive(runtime / 'drive.lock'):
            shutil.rmtree(runtime)
    info['ready'] = False
    info['cleaned_utc'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    dump(p / 'identity.json', info)
    print('Removed only prepared runtime; snapshots/build logs/overlays/identity retained.')
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subs = parser.add_subparsers(dest='action', required=True)
    p = subs.add_parser('prepare'); p.add_argument('--repo', default='.'); p.add_argument('--revision', default='HEAD'); p.add_argument('--out', required=True)
    p = subs.add_parser('doctor'); p.add_argument('--prepared', required=True)
    p = subs.add_parser('snapshot'); p.add_argument('--prepared', required=True); p.add_argument('--out', required=True); p.add_argument('--filter', default='^TestLocal$'); p.add_argument('--timeout', default='45m')
    p = subs.add_parser('compare'); p.add_argument('--old', required=True); p.add_argument('--new', required=True); p.add_argument('--out', required=True)
    p = subs.add_parser('cleanup'); p.add_argument('--prepared', required=True)
    args = parser.parse_args()
    try:
        return globals()[args.action](args)
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        return 2


if __name__ == '__main__':
    sys.exit(main())
