#!/usr/bin/env bash
set -euo pipefail

# Compare the always-map lookup against the atomic-guard lookup with benchstat.
# Also capture BenchmarkGetMergedSymbol for old-vs-new production commits.
#
# Usage from repo root:
#   ./tsc/internal/checker/compare_merged_symbol_lookup.sh
# After changing getMergedSymbol:
#   ./tsc/internal/checker/compare_merged_symbol_lookup.sh --production-after /tmp/merged-symbol-old.txt

tsc_mod=$(cd "$(dirname "$0")/../.." && pwd)
out=${TMPDIR:-/tmp}/merged-symbol-bench
mkdir -p "$out"

count=${BENCH_COUNT:-10}
benchtime=${BENCHTIME:-}

extra=()
if [[ -n "$benchtime" ]]; then
	extra+=(-benchtime="$benchtime")
fi

run_benches() {
	local dest=$1
	go -C "$tsc_mod" test ./internal/checker \
		-run='^$' \
		-bench='BenchmarkMergedSymbolLookup|BenchmarkGetMergedSymbol$' \
		-benchmem \
		-count="$count" \
		"${extra[@]}" \
		| tee "$dest"
}

split_strategy() {
	local raw=$1
	python3 - "$raw" "$out" <<'PY'
import sys
from pathlib import Path
raw_path, out_dir = sys.argv[1], sys.argv[2]
lines = Path(raw_path).read_text().splitlines()
header = [l for l in lines if l.startswith(("goos:", "goarch:", "pkg:", "cpu:"))]
always, guard, prod = [], [], []
for line in lines:
    if line.startswith("BenchmarkMergedSymbolLookup/always_map"):
        always.append(line.replace("BenchmarkMergedSymbolLookup/always_map", "BenchmarkMergedSymbolLookup"))
    elif line.startswith("BenchmarkMergedSymbolLookup/atomic_guard"):
        guard.append(line.replace("BenchmarkMergedSymbolLookup/atomic_guard", "BenchmarkMergedSymbolLookup"))
    elif line.startswith("BenchmarkGetMergedSymbol"):
        prod.append(line)
Path(out_dir, "always.txt").write_text("\n".join(header + always) + "\n")
Path(out_dir, "guard.txt").write_text("\n".join(header + guard) + "\n")
Path(out_dir, "getmerged.txt").write_text("\n".join(header + prod) + "\n")
PY
}

if [[ "${1:-}" == "--production-after" ]]; then
	old=${2:?path to previous BenchmarkGetMergedSymbol benchstat file}
	run_benches "$out/raw-after.txt"
	split_strategy "$out/raw-after.txt"
	echo "=== strategy always_map vs atomic_guard ==="
	benchstat "$out/always.txt" "$out/guard.txt"
	echo "=== production BenchmarkGetMergedSymbol before vs after ==="
	benchstat "$old" "$out/getmerged.txt"
	exit 0
fi

run_benches "$out/raw.txt"
split_strategy "$out/raw.txt"
echo "=== strategy always_map vs atomic_guard ==="
benchstat "$out/always.txt" "$out/guard.txt"
echo "Production snapshot written to $out/getmerged.txt"
echo "Pass that file to --production-after after changing getMergedSymbol."
