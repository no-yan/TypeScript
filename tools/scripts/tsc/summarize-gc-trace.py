#!/usr/bin/env python3
"""Parse GODEBUG gctrace=1,gcpacertrace=1 stderr. Prints last-cycle scan vs live."""
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

ASSIST = re.compile(
    r"pacer: assist ratio=.*\(scan ([0-9]+) MB in ([0-9]+)->([0-9]+) MB\)"
)
WORK = re.compile(
    r"pacer: .* for ([0-9]+)\+([0-9]+)\+([0-9]+) B work \(([0-9]+) B exp\.\)"
)
GCTRACE = re.compile(
    r"^gc (\d+) @([0-9.]+)s\s+([0-9.]+)%: .* ([0-9]+)->([0-9]+)->([0-9]+) MB,",
    re.M,
)


def parse(text: str) -> dict:
    assists = ASSIST.findall(text)
    works = WORK.findall(text)
    gcs = GCTRACE.findall(text)
    out: dict[str, object] = {
        "gc_count": len(gcs),
        "assist_lines": len(assists),
    }
    if assists:
        scan, live, goal = map(int, assists[-1])
        out["last_heapScan_MB"] = scan
        out["last_initialHeapLive_MB"] = live
        out["last_heapGoal_MB"] = goal
        out["last_scannable_fraction"] = (scan / live) if live else None
        out["last_noscanish_fraction"] = (1 - scan / live) if live else None
    if works:
        heap, stack, glob, exp = map(int, works[-1])
        out["last_heapScanWork_B"] = heap
        out["last_stackScanWork_B"] = stack
        out["last_globalsScanWork_B"] = glob
        out["last_expected_scan_B"] = exp
    if gcs:
        n, t, pct, a, b, c = gcs[-1]
        out["last_gc_n"] = int(n)
        out["last_gc_at_s"] = float(t)
        out["last_gc_cpu_pct"] = float(pct)
        out["last_gctrace_start_MB"] = int(a)
        out["last_gctrace_end_MB"] = int(b)
        out["last_gctrace_live_MB"] = int(c)
    return out


def fmt(v: object) -> str:
    if v is None:
        return "n/a"
    if isinstance(v, float):
        return f"{v:.4f}"
    return str(v)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("stderr", nargs="+", type=Path)
    args = ap.parse_args()
    rows = []
    for p in args.stderr:
        t = p.read_text(errors="replace")
        d = parse(t)
        d["file"] = str(p)
        rows.append(d)
        print(f"# {p}")
        for k in (
            "gc_count",
            "last_heapScan_MB",
            "last_initialHeapLive_MB",
            "last_scannable_fraction",
            "last_noscanish_fraction",
            "last_heapScanWork_B",
            "last_gc_cpu_pct",
        ):
            print(f"{k}={fmt(d.get(k))}")
        print()
    scans = [r["last_scannable_fraction"] for r in rows if r.get("last_scannable_fraction") is not None]
    if scans:
        scans.sort()
        mid = scans[len(scans) // 2]
        print(f"median_last_scannable_fraction={mid:.4f} n={len(scans)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
