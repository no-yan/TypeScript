#!/bin/zsh
set -euo pipefail

artifact_dir=${0:a:h}
benchmark='^BenchmarkTraversalSizeScaling$/(expression|full-tree)/subtrees-(32|256|4096)/store$'
orders=(baseline candidate candidate baseline baseline candidate candidate baseline baseline candidate candidate baseline baseline candidate candidate baseline baseline candidate candidate baseline baseline candidate candidate baseline)

mkdir -p "$artifact_dir/balanced"
: > "$artifact_dir/order.tsv"
print -r -- $'attempt\tvariant\tstarted_at' >> "$artifact_dir/order.tsv"

attempt=0
for variant in "${orders[@]}"; do
	(( attempt += 1 ))
	stamp=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
	printf '%02d\t%s\t%s\n' "$attempt" "$variant" "$stamp" >> "$artifact_dir/order.tsv"
	"$artifact_dir/$variant.test" \
		-test.run '^$' \
		-test.bench "$benchmark" \
		-test.benchmem \
		-test.benchtime 500ms \
		-test.count 1 \
		-test.cpu 1 \
		> "$artifact_dir/balanced/$(printf '%02d' "$attempt")-$variant.txt"
done
