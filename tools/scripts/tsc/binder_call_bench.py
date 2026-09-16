#!/usr/bin/env python3
"""Fixed, reproducible normal-GC Binder/micro benchmark runner.

The runner intentionally has a small surface: it prepares two test binaries,
runs an eight-round four-label rotation in fresh processes, and writes raw
samples plus benchstat and round-paired intervals.  It does not run KPC.
"""
from __future__ import annotations

import argparse
import contextlib
import datetime as dt
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path
import random
import re
import shutil
import subprocess
import sys
from typing import Iterator


LABELS = ("before_a", "after_a", "before_b", "after_b")
REQUIRED_METRICS = ("ns/op", "B/op", "allocs/op")
SEED = 20260915


def memory_limit() -> str:
    """Preserve an explicit operator cap; default to the normal-GC protocol."""
    return os.environ.get("GOMEMLIMIT", "off")


def stamp() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat()


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def write_json(path: Path, value: object) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def fail(message: str) -> None:
    raise RuntimeError(message)


def rotation() -> list[list[str]]:
    # Every label occupies every ordinal once; latter half reverses direction.
    forward = [list(LABELS[n:] + LABELS[:n]) for n in range(4)]
    return forward + [list(reversed(row)) for row in forward]


def parse_fixture(value: str) -> dict[str, str]:
    try:
        name, raw_path = value.split("=", 1)
    except ValueError:
        raise argparse.ArgumentTypeError("fixture must be NAME=ABSOLUTE_PATH") from None
    path = Path(raw_path).resolve()
    if not name or not path.is_file():
        raise argparse.ArgumentTypeError("fixture name must be nonempty and path must be a file")
    return {"name": name, "path": str(path), "sha256": digest(path)}


def parse_benchmark(line: str, benchmark: str, fixture: str, benchtime: str) -> dict[str, float]:
    words = line.split()
    if len(words) < 8 or not re.fullmatch(re.escape(benchmark) + r"/" + re.escape(fixture) + r"-\d+", words[0]):
        fail("unexpected benchmark name: " + line)
    if not words[1].isdigit() or int(words[1]) < 1 or (benchtime.endswith("x") and words[1] != benchtime.removesuffix("x")) or (len(words) - 2) % 2:
        fail("unexpected benchmark iteration count or metric layout: " + line)
    metrics: dict[str, float] = {}
    for n in range(2, len(words), 2):
        try:
            value = float(words[n])
        except ValueError:
            fail("non-numeric benchmark metric: " + line)
        if not math.isfinite(value):
            fail("non-finite benchmark metric: " + line)
        metrics[words[n + 1]] = value
    if not all(metric in metrics for metric in REQUIRED_METRICS):
        fail("required benchmark metric missing: " + line)
    return metrics


def sources(repo: Path) -> dict[str, str]:
    files = sorted((repo / "tsc/internal").rglob("*.go"))
    files += [repo / x for x in ("go.work", "go.work.sum", "tsc/go.mod", "tsc/go.sum")]
    return {str(p.relative_to(repo)): digest(p) for p in files if p.is_file()}


def git_value(repo: Path, *args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=repo, text=True).strip()


@contextlib.contextmanager
def host_lock() -> Iterator[None]:
    path = Path("/private/tmp/binder-call-bench.lock")
    with path.open("a+") as lock:
        try:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            fail("another Binder build or measurement holds " + str(path))
        try:
            yield
        finally:
            fcntl.flock(lock.fileno(), fcntl.LOCK_UN)


def command(args: list[str], cwd: Path, env: dict[str, str], log: Path) -> tuple[int, str]:
    with log.open("x") as out:
        code = subprocess.run(args, cwd=cwd, env=env, stdout=out, stderr=subprocess.STDOUT).returncode
    return code, log.read_text()


def prepare(args: argparse.Namespace) -> None:
    run = Path(args.out).resolve()
    before, after = Path(args.before).resolve(), Path(args.after).resolve()
    if run.exists() or not (before / "tsc/go.mod").is_file() or not (after / "tsc/go.mod").is_file():
        fail("new --out and two repository roots containing tsc/go.mod are required")
    if args.benchtime != "10x" and args.stage == "binder":
        fail("Binder stage requires -benchtime=10x to preserve distinct-AST lifetime contract")
    if args.stage == "micro" and not (re.fullmatch(r"[1-9][0-9]*x", args.benchtime) or re.fullmatch(r"[1-9][0-9]*(?:ms|s)", args.benchtime)):
        fail("micro stage requires fixed Nx (mutable) or a duration such as 300ms (read-only)")
    run.mkdir(parents=True)
    for name in ("bin", "fixtures", "raw", "analysis", "correctness", "codegen"):
        (run / name).mkdir()
    fixture_manifest = []
    for fixture in args.fixture:
        target = run / "fixtures" / fixture["name"]
        if target.exists():
            fail("duplicate fixture name: " + fixture["name"])
        shutil.copyfile(fixture["path"], target)
        fixture_manifest.append({"name": fixture["name"], "path": str(target), "sha256": digest(target)})
    write_json(run / "input-manifest.json", fixture_manifest)
    variants = {"before": before, "after": after}
    identity = {"repo_root": str(Path.cwd()), "tsgolint_git_rev": None,
                "typescript_go_git_rev": git_value(before, "rev-parse", "HEAD"),
                "runner_sha256": digest(Path(__file__).resolve()), "go_version": subprocess.check_output(["go", "version"], text=True).strip(),
                "variants": {name: {"repo_root": str(repo), "git_rev": git_value(repo, "rev-parse", "HEAD"), "dirty": bool(git_value(repo, "status", "--porcelain")), "sources": sources(repo)} for name, repo in variants.items()},
                "created_at": stamp(), "artifact_status": "current"}
    write_json(run / "identity.json", identity)
    (run / "source-diff.patch").write_text(git_value(before, "diff", "--binary"))
    plan = {"card": args.card, "stage": args.stage, "package": args.package, "benchmark": args.benchmark,
            "benchtime": args.benchtime, "rounds": 8, "labels": list(LABELS), "order": rotation(),
            "gc": {"GOGC": "100", "GOMEMLIMIT": memory_limit(), "BINDER_BENCH_GC_OFF": "1" if args.gc_off else "0"}, "gomaxprocs": 8,
            "kpc": {"status": "missing", "not_required_for_card": True}}
    write_json(run / "run-plan.json", plan)
    with (run / "order.jsonl").open("x") as out:
        for number, labels in enumerate(rotation()):
            out.write(json.dumps({"round": number, "labels": labels}) + "\n")
    env = os.environ.copy()
    env.update({"GOMAXPROCS": "8", "GOGC": "100", "GOMEMLIMIT": memory_limit(), "BINDER_BENCH_GC_OFF": "1" if args.gc_off else "0",
                "BINDER_INVESTIGATION_MANIFEST": str(run / "input-manifest.json")})
    with host_lock():
        for name, repo in variants.items():
            binary = run / "bin" / (name + ".test")
            log = run / "bin" / (name + ".build.log")
            code, _ = command(["go", "test", "-c", "-tags=binderinvestigation", "-o", str(binary), args.package], repo / "tsc", env, log)
            if code:
                fail("build failed; see " + str(log))
            identity["variants"][name]["binary_sha256"] = digest(binary)
            if sources(repo) != identity["variants"][name]["sources"]:
                fail("production source changed while building " + name)
            lifetime_log = run / "correctness" / (name + "-lifetime.log")
            code, _ = command([str(binary), "-test.run=^TestInvestigationBatchLifetime$", "-test.count=1"], repo / "tsc", env, lifetime_log)
            if code:
                fail("lifetime test failed; see " + str(lifetime_log))
        semantic_log = run / "correctness" / "semantic-selftest.log"
        code, _ = command([sys.executable, str(before / "tools/scripts/tsc/binder_semantic_audit.py"), "selftest"], before, env, semantic_log)
        if code:
            fail("semantic comparator selftest failed; see " + str(semantic_log))
    write_json(run / "identity.json", identity)
    print(run)


def run_stage(args: argparse.Namespace) -> None:
    run = Path(args.out).resolve()
    plan = json.loads((run / "run-plan.json").read_text())
    identity = json.loads((run / "identity.json").read_text())
    fixtures = json.loads((run / "input-manifest.json").read_text())
    if plan["stage"] != args.stage or plan["benchmark"] != args.benchmark:
        fail("stage/benchmark differs from immutable run plan")
    raw = run / "raw" / args.stage
    raw.mkdir()
    records: list[dict[str, object]] = []
    env = os.environ.copy(); env.update({"GOMAXPROCS": "8", "GOGC": "100", "GOMEMLIMIT": memory_limit(), "BINDER_BENCH_GC_OFF": plan["gc"]["BINDER_BENCH_GC_OFF"], "BINDER_INVESTIGATION_MANIFEST": str(run / "input-manifest.json")})
    with host_lock():
        for round_number, labels in enumerate(rotation()):
            for label in labels:
                variant = label.split("_", 1)[0]
                binary = run / "bin" / (variant + ".test")
                if digest(binary) != identity["variants"][variant]["binary_sha256"]:
                    fail("binary changed after prepare: " + str(binary))
                for fixture in fixtures:
                    regex = "^" + re.escape(args.benchmark) + "$/^" + re.escape(fixture["name"]) + "$"
                    log = raw / f"{round_number:02d}-{label}-{fixture['name']}.log"
                    argv = [str(binary), "-test.run=^$", "-test.bench=" + regex, "-test.benchtime=" + plan["benchtime"], "-test.count=1", "-test.benchmem"]
                    started = stamp(); code, text = command(argv, Path(identity["variants"][variant]["repo_root"]) / "tsc", env, log); finished = stamp()
                    lines = [line for line in text.splitlines() if line.startswith("Benchmark")]
                    rec: dict[str, object] = {"round": round_number, "label": label, "fixture": fixture["name"], "argv": argv, "started_at": started, "finished_at": finished, "exit_code": code, "log": str(log), "binary_sha256": digest(binary)}
                    if code or len(lines) != 1:
                        rec["failure"] = "nonzero exit or benchmark line count is not one"
                    else:
                        try:
                            rec["metrics"] = parse_benchmark(lines[0], args.benchmark, fixture["name"], plan["benchtime"])
                            rec["benchmark_line"] = lines[0]
                        except RuntimeError as error:
                            rec["failure"] = str(error)
                    records.append(rec)
                    if "failure" in rec:
                        write_json(raw / "failure.json", rec); fail(str(rec["failure"]) + "; see " + str(log))
    expected = 8 * 4 * len(fixtures)
    if len(records) != expected or len({(r["round"], r["label"], r["fixture"]) for r in records}) != expected:
        fail("missing or duplicate sample")
    for label in LABELS:
        (raw / (label + ".txt")).write_text("\n".join(str(r["benchmark_line"]) for r in records if r["label"] == label) + "\n")
    (raw / "bench.txt").write_text("\n".join(str(r["benchmark_line"]) for r in records) + "\n")
    with (raw / "samples.jsonl").open("x") as out:
        for record in records: out.write(json.dumps(record) + "\n")
    write_json(raw / "stage-state.json", {"complete": True, "samples": expected, "finished_at": stamp()})


def bootstrap(left: list[float], right: list[float]) -> list[float]:
    values = [math.log(y / x) for x, y in zip(left, right)]
    rng = random.Random(SEED); samples = []
    for _ in range(20000): samples.append((math.exp(sum(rng.choice(values) for _ in values) / len(values)) - 1) * 100)
    samples.sort(); return [samples[500], samples[19499]]


def analyze(args: argparse.Namespace) -> None:
    run = Path(args.out).resolve(); raw = run / "raw" / args.stage; analysis = run / "analysis" / args.stage; analysis.mkdir()
    plan = json.loads((run / "run-plan.json").read_text()); identity = json.loads((run / "identity.json").read_text())
    records = [json.loads(line) for line in (raw / "samples.jsonl").read_text().splitlines()]
    fixtures = [x["name"] for x in json.loads((run / "input-manifest.json").read_text())]
    benchstat = shutil.which("benchstat") or str(Path("/Volumes/SanDisk1TB/Library/Caches/binder-85506e8-80d8b41-20260915-run1/tools/benchstat"))
    benchstat_path = Path(benchstat)
    if not benchstat_path.is_file() or not (raw / "stage-state.json").is_file(): fail("missing benchstat or complete stage state")
    state = json.loads((raw / "stage-state.json").read_text()); expected = 8 * 4 * len(fixtures)
    if not state.get("complete") or len(records) != expected or len({(r["round"], r["label"], r["fixture"]) for r in records}) != expected: fail("incomplete or duplicate sample set")
    for rec in records:
        if rec["exit_code"] != 0 or "failure" in rec: fail("failed sample in analysis")
        log = Path(rec["log"]); lines = [x for x in log.read_text().splitlines() if x.startswith("Benchmark")]
        if len(lines) != 1 or lines[0] != rec["benchmark_line"]: fail("raw log does not match recorded sample: " + str(log))
        parse_benchmark(lines[0], plan["benchmark"], rec["fixture"], plan["benchtime"])
        variant = rec["label"].split("_", 1)[0]
        if rec["binary_sha256"] != identity["variants"][variant]["binary_sha256"]: fail("sample binary identity mismatch")
    results = {"benchstat": {"path": str(benchstat_path), "sha256": digest(benchstat_path), "version": subprocess.run([benchstat, "-h"], text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT).stdout.splitlines()[0] if benchstat else "unknown"}, "bootstrap": {"seed": SEED, "resamples": 20000, "method": "round-paired mean log-ratio percentile"}, "fixtures": {}}
    for name, left, right in (("primary", "before_a", "after_a"), ("confirmation", "before_b", "after_b"), ("before-aa", "before_a", "before_b"), ("after-aa", "after_a", "after_b")):
        proc = subprocess.run([benchstat, str(raw / (left + ".txt")), str(raw / (right + ".txt"))], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode: fail("benchstat failed: " + proc.stderr)
        (analysis / (name + ".benchstat.txt")).write_text(proc.stdout)
    for fixture in fixtures:
        results["fixtures"][fixture] = {}
        for metric in REQUIRED_METRICS:
            groups = {label: [r["metrics"][metric] for r in records if r["label"] == label and r["fixture"] == fixture] for label in LABELS}
            results["fixtures"][fixture][metric] = {"primary_delta_pct": (sum(groups["after_a"]) / sum(groups["before_a"]) - 1) * 100,
                                        "confirmation_delta_pct": (sum(groups["after_b"]) / sum(groups["before_b"]) - 1) * 100,
                                        "primary_95_ci_pct": bootstrap(groups["before_a"], groups["after_a"]),
                                        "confirmation_95_ci_pct": bootstrap(groups["before_b"], groups["after_b"]),
                                        "before_aa_95_ci_pct": bootstrap(groups["before_a"], groups["before_b"]),
                                        "after_aa_95_ci_pct": bootstrap(groups["after_a"], groups["after_b"])}
    write_json(analysis / "summary.json", results)


def selftest(_: argparse.Namespace) -> None:
    rows = rotation()
    assert len(rows) == 8 and all(len(row) == 4 and set(row) == set(LABELS) for row in rows)
    assert all(sum(row.index(label) == pos for row in rows) == 2 for label in LABELS for pos in range(4))
    good = "BenchmarkX/checker.ts-8 10 123 ns/op 4 B/op 1 allocs/op"
    assert parse_benchmark(good, "BenchmarkX", "checker.ts", "10x")["ns/op"] == 123
    try: parse_benchmark("BenchmarkX/checker.ts-8 9 123 ns/op 4 B/op 1 allocs/op", "BenchmarkX", "checker.ts", "10x")
    except RuntimeError: pass
    else: raise AssertionError("bad benchmark count was accepted")
    timed = "BenchmarkX/checker.ts-8 17 123 ns/op 4 B/op 1 allocs/op"
    assert parse_benchmark(timed, "BenchmarkX", "checker.ts", "300ms")["B/op"] == 4
    print("binder_call_bench selftest: OK")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__); sub = parser.add_subparsers(dest="cmd", required=True)
    def common(p: argparse.ArgumentParser) -> None:
        p.add_argument("--out", required=True); p.add_argument("--stage", choices=("binder", "micro"), default="binder"); p.add_argument("--benchmark", default="BenchmarkBindInvestigation")
    p = sub.add_parser("prepare"); common(p); p.add_argument("--before", required=True); p.add_argument("--after", required=True); p.add_argument("--card", required=True); p.add_argument("--package", default="./internal/binder"); p.add_argument("--benchtime", default="10x"); p.add_argument("--gc-off", action="store_true", help="disable GC only during the timed Binder batch"); p.add_argument("--fixture", type=parse_fixture, action="append", required=True); p.set_defaults(fn=prepare)
    p = sub.add_parser("run"); common(p); p.set_defaults(fn=run_stage)
    p = sub.add_parser("analyze"); common(p); p.set_defaults(fn=analyze)
    p = sub.add_parser("selftest"); p.set_defaults(fn=selftest)
    args = parser.parse_args(); args.fn(args); return 0


if __name__ == "__main__":
    try: raise SystemExit(main())
    except (RuntimeError, OSError, subprocess.CalledProcessError) as error: print("error:", error, file=sys.stderr); raise SystemExit(2)
