#!/bin/zsh
S=/private/tmp/claude-501/-Volumes-SanDisk1TB-worktree-binder-rewrite-tsc/a24c25c3-22c1-445d-9836-a6e6a71f89a3/scratchpad
export GOMAXPROCS=8 GOGC=100 GOMEMLIMIT=off BINDER_INVESTIGATION_MANIFEST=$S/manifest.json
rm -f $S/raw/micro-*.txt
for r in 1 2 3 4 5 6 7 8 9 10; do
  if [ $((r % 2)) -eq 1 ]; then order="base head"; else order="head base"; fi
  for v in ${=order}; do
    $S/bin/$v.test -test.run '^$' -test.bench '^BenchmarkScratchContextualIdentifier$' -test.benchtime=200x -test.count=1 >> $S/raw/micro-$v.txt 2>&1
  done
done
echo MICRO_DONE > $S/raw/micro.done
$S/workload.sh
