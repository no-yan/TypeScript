#!/usr/bin/env python3
"""Summarize xctrace CPU Counters XML exports. Does not use pprof."""
from __future__ import annotations

import argparse
import re
import xml.etree.ElementTree as ET
from collections import defaultdict
from pathlib import Path


def text(path: Path) -> str:
    return path.read_text(errors="replace") if path.is_file() else ""


def launched_process(toc: str) -> tuple[str, str]:
    m = re.search(r'<process\b[^>]*\btype="launched"[^>]*>', toc)
    if not m:
        return "", ""
    tag = m.group(0)
    name = re.search(r'\bname="([^"]*)"', tag)
    status = re.search(r'\breturn-exit-status="([^"]*)"', tag)
    return (name.group(1) if name else ""), (status.group(1) if status else "")


def template_and_duration(toc: str) -> tuple[str, str, str]:
    tmpl = ""
    m = re.search(r"<template-name>([^<]*)</template-name>", toc)
    if m:
        tmpl = m.group(1)
    dur = ""
    m = re.search(r"<duration>([^<]*)</duration>", toc)
    if m:
        dur = m.group(1)
    guided = "unknown"
    if "CPU Bottlenecks" in toc:
        guided = "guided-cpu-bottlenecks"
    if '"configurationType":{"guided":{}}' in toc or "Guided" in toc:
        if guided == "unknown":
            guided = "guided"
    return tmpl, dur, guided


def last_metrics(path: Path) -> dict[str, str]:
    if not path.is_file() or path.stat().st_size == 0:
        return {}
    last: dict[str, str] = {}
    try:
        root = ET.parse(path).getroot()
    except ET.ParseError:
        return {}
    for row in root.iter("row"):
        name = None
        val = None
        for s in row.findall("string"):
            name = s.get("fmt") or s.text or name
        for fd in row.findall("fixed-decimal"):
            val = fd.get("fmt") or fd.text or val
        if name and val is not None:
            last[name] = val
    return last


def sum_metric_table(path: Path) -> dict[str, float]:
    totals: dict[str, float] = defaultdict(float)
    if not path.is_file() or path.stat().st_size == 0:
        return {}
    try:
        root = ET.parse(path).getroot()
    except ET.ParseError:
        return {}
    for row in root.iter("row"):
        names = [e.get("fmt") or (e.text or "") for e in row.findall("string")]
        metric = names[1] if len(names) > 1 else (names[0] if names else "")
        val = None
        for fd in row.findall("fixed-decimal"):
            raw = fd.text
            if raw:
                try:
                    val = float(raw)
                except ValueError:
                    pass
        if metric and val is not None:
            totals[metric] += val
    return dict(totals)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--toc", required=True)
    ap.add_argument("--agg", default="")
    ap.add_argument("--metrics", default="")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()
    toc = text(Path(args.toc))
    name, exit_status = launched_process(toc)
    tmpl, dur, mode = template_and_duration(toc)
    last = last_metrics(Path(args.agg)) if args.agg else {}
    sums = sum_metric_table(Path(args.metrics)) if args.metrics else {}

    lines = [
        f"launched_process={name}",
        f"target_exit={exit_status}",
        f"template_name={tmpl}",
        f"trace_duration_s={dur}",
        f"counters_mode={mode}",
        "pprof=forbidden",
    ]
    if mode == "guided-cpu-bottlenecks":
        lines.append(
            "warning=this template is Instruments Guided CPU Bottlenecks "
            "(time/PMI samples and bottleneck ratios), not a process-lifetime "
            "INST_RETIRED / instruction total. Save a Custom CPU Counters "
            "template that counts instruction events, then set "
            "VERIFY_TSC_XCTRACE_TEMPLATE to that .tracetemplate."
        )
    if last:
        lines.append("last_metric_aggregation:")
        for k in sorted(last):
            lines.append(f"  {k}={last[k]}")
    if sums:
        lines.append("metric_table_sums:")
        for k in sorted(sums):
            lines.append(f"  {k}={sums[k]:.6f}")
    Path(args.out).write_text("\n".join(lines) + "\n")
    print(Path(args.out).read_text(), end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
