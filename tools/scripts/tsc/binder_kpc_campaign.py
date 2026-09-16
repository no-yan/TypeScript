#!/usr/bin/env python3
"""Run a fixed, root-authorized Binder KPC A/B campaign."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
from datetime import datetime, timezone


LABELS = ("before_a", "after_a", "before_b", "after_b")


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def stamp() -> str:
    return datetime.now(timezone.utc).isoformat()


def rotation() -> list[list[str]]:
    forward = [list(LABELS[n:] + LABELS[:n]) for n in range(4)]
    return forward + [list(reversed(row)) for row in forward]


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--before-bin", required=True, type=Path)
    parser.add_argument("--after-bin", required=True, type=Path)
    parser.add_argument("--before-repo", required=True, type=Path)
    parser.add_argument("--after-repo", required=True, type=Path)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--config", required=True, type=Path)
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise SystemExit("KPC campaign must be launched with macOS administrator authentication")
    out = args.out.resolve()
    if out.exists():
        raise SystemExit("--out must not already exist")
    fixtures = json.loads(args.manifest.read_text())
    if {x["name"] for x in fixtures} != {"checker.ts", "dom.generated.d.ts"}:
        raise SystemExit("expected exactly checker.ts and dom.generated.d.ts fixtures")
    out.mkdir(parents=True)
    raw = out / "raw"
    raw.mkdir()
    plan = {
        "stage": "kpc-gc-off", "rounds": 8, "labels": LABELS,
        "order": rotation(), "benchmark": "BenchmarkBindInvestigationKPC",
        "benchtime": "10x", "GOMEMLIMIT": os.environ.get("GOMEMLIMIT", "off"),
        "GOGC": "100", "GOMAXPROCS": "8",
    }
    (out / "run-plan.json").write_text(json.dumps(plan, indent=2) + "\n")
    binaries = {"before": args.before_bin.resolve(), "after": args.after_bin.resolve()}
    repos = {"before": args.before_repo.resolve(), "after": args.after_repo.resolve()}
    identity = {
        "created_at": stamp(), "before_binary_sha256": digest(binaries["before"]),
        "after_binary_sha256": digest(binaries["after"]),
        "manifest_sha256": digest(args.manifest), "config_sha256": digest(args.config),
    }
    (out / "identity.json").write_text(json.dumps(identity, indent=2) + "\n")
    base_env = os.environ.copy()
    base_env.update({"GOMEMLIMIT": os.environ.get("GOMEMLIMIT", "off"), "GOMAXPROCS": "8", "GOGC": "100",
                     "BINDER_BENCH_GC_OFF": "1", "BINDER_KPC_CONFIG": str(args.config.resolve()),
                     "BINDER_INVESTIGATION_MANIFEST": str(args.manifest.resolve())})
    records = []
    for variant in ("before", "after"):
        for number in range(2):
            env = base_env | {"BINDER_INVESTIGATION_REPO_ROOT": str(repos[variant] / "tsc")}
            proc = subprocess.run([str(binaries[variant]), "-test.run=^TestInvestigationKPC$", "-test.v"], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            path = out / f"selftest-{variant}-{number}.log"
            path.write_text(proc.stdout)
            if proc.returncode or "3 fresh pthreads x 100 short/long pairs" not in proc.stdout:
                raise SystemExit(f"KPC self-test failed: {path}")
    for round_number, labels in enumerate(rotation()):
        for label in labels:
            variant = label.split("_", 1)[0]
            env = base_env | {"BINDER_INVESTIGATION_REPO_ROOT": str(repos[variant] / "tsc")}
            for fixture in fixtures:
                argv = [str(binaries[variant]), "-test.run=^$", "-test.bench=^BenchmarkBindInvestigationKPC$/^" + fixture["name"] + "$", "-test.benchtime=10x", "-test.count=1", "-test.benchmem"]
                started = stamp()
                proc = subprocess.run(argv, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
                lines = [line for line in proc.stdout.splitlines() if line.startswith("Benchmark")]
                path = raw / f"{round_number:02d}-{label}-{fixture['name']}.log"
                path.write_text(proc.stdout)
                if proc.returncode or len(lines) != 1:
                    raise SystemExit(f"sample failed: {path}")
                with (raw / f"{label}.txt").open("a") as aggregate:
                    aggregate.write(lines[0] + "\n")
                records.append({"round": round_number, "label": label, "fixture": fixture["name"], "argv": argv, "started_at": started, "finished_at": stamp(), "log": str(path), "line": lines[0]})
        print(f"round {round_number + 1}/8 complete", flush=True)
    if len(records) != 64:
        raise SystemExit("missing KPC samples")
    with (raw / "records.jsonl").open("w") as result:
        for record in records:
            result.write(json.dumps(record) + "\n")
    (out / "complete.json").write_text(json.dumps({"complete": True, "samples": len(records), "finished_at": stamp()}, indent=2) + "\n")


if __name__ == "__main__":
    main()
