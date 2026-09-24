#!/bin/sh
# Alternates the two modes of the _e2e command in separate processes and
# prints the medians of every run of each mode, and store/pointer.
#
#   ./run.sh <tsconfig.json> [processes] [runs per process] [extra flags]
#
# GOGC and GOMAXPROCS are passed through the environment.
set -eu
project=$1
procs=${2:-3}
runs=${3:-3}
shift $(( $# < 3 ? $# : 3 ))

dir=$(cd "$(dirname "$0")" && pwd)
out=$(mktemp -d)
(cd "$dir/../../.." && go build -o "$out/e2e" ./internal/storebinder/_e2e)

i=0
while [ $i -lt "$procs" ]; do
	for mode in pointer store; do
		"$out/e2e" -mode $mode -project "$project" -runs "$runs" "$@" | tee -a "$out/all.tsv" | grep '^# median'
	done
	i=$((i + 1))
done
head -1 "$out/all.tsv" | sed 's/ mode=[a-z]*//'

# Columns: 3 parse, 4 bind, 5 total, 6 user, 8 alloc, 11 retained.
awk -F'\t' '
function med(a, n,    i, j, t) {
	for (i = 2; i <= n; i++) for (j = i; j > 1 && a[j-1] > a[j]; j--) { t = a[j]; a[j] = a[j-1]; a[j-1] = t }
	return n % 2 ? a[(n+1)/2] : (a[n/2] + a[n/2+1]) / 2
}
$1 == "pointer" || $1 == "store" {
	n[$1]++
	for (c = 3; c <= 11; c++) v[$1, c, n[$1]] = $c
}
END {
	split("3 parse_ms 4 bind_ms 5 total_ms 6 user_ms 8 alloc_MB 11 retained_MB", spec, " ")
	printf "%-12s %10s %10s %8s  (n=%d/%d)\n", "metric", "pointer", "store", "ratio", n["pointer"], n["store"]
	for (k = 1; k <= 12; k += 2) {
		c = spec[k]
		for (m = 1; m <= 2; m++) {
			mode = m == 1 ? "pointer" : "store"
			delete a
			for (i = 1; i <= n[mode]; i++) a[i] = v[mode, c, i]
			r[mode] = med(a, n[mode])
		}
		printf "%-12s %10.1f %10.1f %8.3f\n", spec[k+1], r["pointer"], r["store"], r["store"] / r["pointer"]
	}
}' "$out/all.tsv"
echo "# runs: $out/all.tsv"
