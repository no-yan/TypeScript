#!/bin/zsh
# VS Code parse+bind real workload: base/head CLI alternated, parallel and single-threaded.
S=/private/tmp/claude-501/-Volumes-SanDisk1TB-worktree-binder-rewrite-tsc/a24c25c3-22c1-445d-9836-a6e6a71f89a3/scratchpad
cd /Volumes/SanDisk1TB/ghq/github.com/microsoft/vscode
export GOMAXPROCS=8 GOGC=100 GOMEMLIMIT=off
for mode in parallel single; do
  extra=""; [ $mode = single ] && extra="--singleThreaded"
  for r in 1 2 3 4 5 6; do
    if [ $((r % 2)) -eq 1 ]; then order="base head"; else order="head base"; fi
    for v in ${=order}; do
      /usr/bin/time -l $S/bin/tsc_$v --project src/tsconfig.json --noEmit --noCheck ${=extra} > /dev/null 2> $S/raw/wl-$v-$mode-$r.time
      echo "exit $?" >> $S/raw/wl-$v-$mode-$r.time
    done
  done
done
echo WL_DONE > $S/raw/workload.done
