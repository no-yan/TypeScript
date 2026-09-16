#!/bin/zsh
S=/private/tmp/claude-501/-Volumes-SanDisk1TB-worktree-binder-rewrite-tsc/a24c25c3-22c1-445d-9836-a6e6a71f89a3/scratchpad
export GOMAXPROCS=8 GOGC=100 GOMEMLIMIT=off BINDER_INVESTIGATION_MANIFEST=$S/manifest.json
$S/bin/head.test -test.run '^TestScratchIdentifierClasses$' -test.v > $S/raw/identclasses.log 2>&1
$S/bin/head.test -test.run 'Lifetime|Batch' -test.v > $S/raw/lifetime-head.log 2>&1; echo "exit $?" >> $S/raw/lifetime-head.log
$S/bin/base.test -test.run 'Lifetime|Batch' -test.v > $S/raw/lifetime-base.log 2>&1; echo "exit $?" >> $S/raw/lifetime-base.log
for gc in natural gc_off; do
  if [ $gc = gc_off ]; then export BINDER_BENCH_GC_OFF=1; else export BINDER_BENCH_GC_OFF=0; fi
  for r in 1 2 3 4 5 6 7 8 9 10; do
    if [ $((r % 2)) -eq 1 ]; then order="base head"; else order="head base"; fi
    for v in ${=order}; do
      $S/bin/$v.test -test.run '^$' -test.bench '^BenchmarkBindInvestigation$' -test.benchtime=10x -test.count=1 -test.benchmem >> $S/raw/$v-$gc.txt 2>&1
    done
  done
done
echo BENCH_DONE > $S/raw/bench.done
