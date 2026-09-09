#!/usr/bin/env bash
# Compare this worktree against pointer AST at 8ac035a394.
# Procedure: tsc/internal/ast/docs/parse-bind-vs-8ac035a.md
set -euo pipefail

PARENT_SHA=8ac035a394c79e693a3a7d74cb170448503ee894
PARENT_ROOT=${PARENT_ROOT:-/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript}
STORE_ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
KPERF_TESTDATA=${KPERF_TESTDATA:-/tmp/kperf-testdata}
VSCODE=${VERIFY_TSC_VSCODE_ROOT:-/Users/noyan/ghq/github.com/microsoft/vscode}
OUT=${OUT:-/tmp/eval-parse-bind-8ac035a}
GO=${GO:-/opt/homebrew/opt/go/bin/go}
FORBIDDEN_PARENT=/Volumes/SanDisk1TB/ghq/github.com/microsoft/typescript

die() { echo "eval-8ac035a: $*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage: eval-parse-bind-vs-8ac035a.sh [--identity] [--overlay] [--build] [--kpc] [--wall] [--monaco] [--vscode] [--summarize] [--restore] [--all]
Default is --identity --overlay --build --summarize.
EOF
}

want_identity=0
want_overlay=0
want_build=0
want_kpc=0
want_wall=0
want_monaco=0
want_vscode=0
want_summarize=0
want_restore=0
if [[ $# -eq 0 ]]; then
  want_identity=1
  want_overlay=1
  want_build=1
  want_summarize=1
fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    --identity) want_identity=1 ;;
    --overlay) want_overlay=1 ;;
    --build) want_build=1 ;;
    --kpc) want_kpc=1 ;;
    --wall) want_wall=1 ;;
    --monaco) want_monaco=1 ;;
    --vscode) want_vscode=1 ;;
    --summarize) want_summarize=1 ;;
    --restore) want_restore=1 ;;
    --all)
      want_identity=1
      want_overlay=1
      want_build=1
      want_kpc=1
      want_wall=1
      want_monaco=1
      want_vscode=1
      want_summarize=1
      want_restore=1
      ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown flag $1" ;;
  esac
  shift
done

export PATH="$(dirname "$GO"):$PATH"
export GOTOOLCHAIN=local
mkdir -p "$OUT" "$KPERF_TESTDATA/fixtures/compiler" "$KPERF_TESTDATA/fixtures/lib"

cmd_identity() {
  [[ "$PARENT_ROOT" == "$FORBIDDEN_PARENT" ]] && die "parent must not be $FORBIDDEN_PARENT"
  local got
  got=$(git -C "$PARENT_ROOT" rev-parse HEAD)
  [[ "$got" == "$PARENT_SHA" ]] || die "parent HEAD is $got, want $PARENT_SHA"
  git -C "$STORE_ROOT" status --short >"$OUT/store.status.txt"
  git -C "$PARENT_ROOT" status --short >"$OUT/parent.status.txt"
  "$GO" version | tee "$OUT/go.version.txt"
  local src_checker="$STORE_ROOT/tsc/testdata/fixtures/compiler/checker.ts"
  local src_dom="$STORE_ROOT/tsc/testdata/fixtures/lib/dom.generated.d.ts"
  [[ -f "$src_checker" ]] || die "missing $src_checker"
  cp -f "$src_checker" "$KPERF_TESTDATA/fixtures/compiler/checker.ts"
  cp -f "$src_dom" "$KPERF_TESTDATA/fixtures/lib/dom.generated.d.ts"
  shasum -a 256 "$KPERF_TESTDATA/fixtures/compiler/checker.ts" | tee "$OUT/checker.sha256"
  [[ -f "$VSCODE/src/tsconfig.monaco.json" ]] || die "missing monaco tsconfig under $VSCODE"
  [[ -f "$VSCODE/src/tsconfig.json" ]] || die "missing vscode src/tsconfig.json under $VSCODE"
  {
    echo "parent_root=$PARENT_ROOT"
    echo "parent_sha=$got"
    echo "store_root=$STORE_ROOT"
    echo "store_sha=$(git -C "$STORE_ROOT" rev-parse HEAD)"
    echo "go=$("$GO" version)"
    echo "kperf_testdata=$KPERF_TESTDATA"
    echo "vscode=$VSCODE"
    echo "out=$OUT"
  } | tee "$OUT/identity.txt"
}

cmd_overlay() {
  mkdir -p "$PARENT_ROOT/tsc/internal/testutil/kperf"
  cp -f "$STORE_ROOT/tsc/internal/testutil/kperf/kperf_darwin.go" "$STORE_ROOT/tsc/internal/testutil/kperf/kperf_stub.go" \
    "$PARENT_ROOT/tsc/internal/testutil/kperf/"
  cp -f "$STORE_ROOT/tsc/internal/binder/bind_kperf_bench_test.go" "$PARENT_ROOT/tsc/internal/binder/"
  cp -f "$STORE_ROOT/tsc/internal/binder/bind_hot_bench_test.go" "$PARENT_ROOT/tsc/internal/binder/"
  cp -f "$KPERF_TESTDATA/fixtures/compiler/checker.ts" \
    "$PARENT_ROOT/tsc/testdata/fixtures/compiler/checker.ts"
  echo overlay_ok >"$OUT/overlay.txt"
}

cmd_restore() {
  git -C "$PARENT_ROOT" checkout -- tsc/testdata/fixtures/compiler/checker.ts || true
  rm -rf "$PARENT_ROOT/tsc/internal/testutil/kperf"
  rm -f "$PARENT_ROOT/tsc/internal/binder/bind_kperf_bench_test.go" \
    "$PARENT_ROOT/tsc/internal/binder/bind_hot_bench_test.go"
  git -C "$PARENT_ROOT" status --short -- tsc/internal/testutil/kperf \
    tsc/internal/binder/bind_kperf_bench_test.go \
    tsc/internal/binder/bind_hot_bench_test.go \
    tsc/testdata/fixtures/compiler/checker.ts | tee "$OUT/parent.status.after.txt"
}

cmd_build() {
  local lock
  lock=$(printf '%s' "$STORE_ROOT" | shasum -a 256 | awk '{ print substr($1, 1, 16) }')
  echo "building parent then store under sequential compiles (lock id $lock)"
  (
    cd "$PARENT_ROOT/tsc"
    CGO_ENABLED=1 "$GO" test -tags kperf -c -o /tmp/orig.binder.kperf.test ./internal/binder
    CGO_ENABLED=0 "$GO" test -c -o /tmp/orig.parser.bench.test ./internal/parser
    CGO_ENABLED=0 "$GO" test -c -o /tmp/orig.binder.bench.test ./internal/binder
    CGO_ENABLED=0 "$GO" build -trimpath -o /tmp/tsc-orig ./cmd/tsc
  )
  /tmp/tsc-orig --version | tee "$OUT/tsc-orig.version.txt"
  if "$GO" version -m /tmp/tsc-orig | grep -q noembed; then
    die "parent tsc has noembed, monaco libs will miss under xctrace"
  fi
  cd "$STORE_ROOT"
  CGO_ENABLED=1 ./.cursor/skills/verify-tsc/scripts/control-tsc go -- -C ./tsc test -tags kperf -c -o /tmp/store.binder.kperf.test ./internal/binder
  CGO_ENABLED=0 ./.cursor/skills/verify-tsc/scripts/control-tsc go -- -C ./tsc test -c -o /tmp/store.parser.bench.test ./internal/parser
  CGO_ENABLED=0 ./.cursor/skills/verify-tsc/scripts/control-tsc go -- -C ./tsc test -c -o /tmp/store.binder.bench.test ./internal/binder
  VERIFY_TSC_RUN_ID=${VERIFY_TSC_RUN_ID:-eval-8ac035a} ./.cursor/skills/verify-tsc/scripts/control-tsc launch --embed
  cp -f "$STORE_ROOT/built/local/tsc" /tmp/tsc-store
  /tmp/tsc-store --version | tee "$OUT/tsc-store.version.txt"
  ls -lh /tmp/orig.binder.kperf.test /tmp/store.binder.kperf.test /tmp/tsc-orig /tmp/tsc-store | tee "$OUT/binaries.txt"
}

interleave_bench() {
  local label="$1" orig="$2" store="$3" bench="$4" orig_out="$5" store_out="$6" n="$7"
  local extra="${8:-}"
  : >"$orig_out"
  : >"$store_out"
  local i
  for i in $(seq 1 "$n"); do
    echo "-- $label $i orig --"
    # shellcheck disable=SC2086
    "$orig" -test.run '^$' -test.bench "$bench" -test.benchmem -test.count 1 -test.benchtime 30x $extra | tee -a "$orig_out"
    echo "-- $label $i store --"
    # shellcheck disable=SC2086
    "$store" -test.run '^$' -test.bench "$bench" -test.benchmem -test.count 1 -test.benchtime 30x $extra | tee -a "$store_out"
  done
}

kpc_one() {
  local bin="$1" bench="$2"
  sudo -E env GOGC=off KPERF_TESTDATA="$KPERF_TESTDATA" "$bin" \
    -test.run '^$' -test.bench "$bench" -test.benchtime=30x -test.count 1
}

cmd_kpc() {
  export KPERF_TESTDATA
  export GOGC=off
  if ! sudo -n true 2>/dev/null; then
    echo kpc=unreachable | tee "$OUT/kpc.status.txt"
    return 0
  fi
  echo kpc=ok | tee "$OUT/kpc.status.txt"
  sudo -v
  kpc_one /tmp/store.binder.kperf.test '^BenchmarkParseKPC$/^checker.ts$' | tee "$OUT/kpc.sanity.a.txt"
  kpc_one /tmp/store.binder.kperf.test '^BenchmarkParseKPC$/^checker.ts$' | tee "$OUT/kpc.sanity.b.txt"
  local names benches i
  names=(ParseKPC.checker.ts ParseKPC.empty.ts ParseKPC.dom.generated.d.ts BindKPC.checker.ts BindKPC.empty.ts BindKPC.dom.generated.d.ts)
  benches=(
    '^BenchmarkParseKPC$/^checker.ts$'
    '^BenchmarkParseKPC$/^empty.ts$'
    '^BenchmarkParseKPC$/^dom.generated.d.ts$'
    '^BenchmarkBindKPC$/^checker.ts$'
    '^BenchmarkBindKPC$/^empty.ts$'
    '^BenchmarkBindKPC$/^dom.generated.d.ts$'
  )
  local idx
  for idx in 0 1 2 3 4 5; do
    : >"$OUT/kpc.orig.${names[$idx]}.txt"
    : >"$OUT/kpc.store.${names[$idx]}.txt"
    for i in 1 2 3; do
      echo "-- kpc ${names[$idx]} $i orig --"
      kpc_one /tmp/orig.binder.kperf.test "${benches[$idx]}" | tee -a "$OUT/kpc.orig.${names[$idx]}.txt"
      echo "-- kpc ${names[$idx]} $i store --"
      kpc_one /tmp/store.binder.kperf.test "${benches[$idx]}" | tee -a "$OUT/kpc.store.${names[$idx]}.txt"
    done
  done
}

cmd_wall() {
  export KPERF_TESTDATA
  unset GOGC || true
  interleave_bench parse-checker /tmp/orig.parser.bench.test /tmp/store.parser.bench.test \
    '^BenchmarkParse$/^checker.ts$' "$OUT/orig.parse.checker.txt" "$OUT/store.parse.checker.txt" 10
  interleave_bench parse-empty /tmp/orig.parser.bench.test /tmp/store.parser.bench.test \
    '^BenchmarkParse$/^empty.ts$' "$OUT/orig.parse.empty.txt" "$OUT/store.parse.empty.txt" 10
  interleave_bench parse-dom /tmp/orig.parser.bench.test /tmp/store.parser.bench.test \
    '^BenchmarkParse$/^dom.generated.d.ts$' "$OUT/orig.parse.dom.txt" "$OUT/store.parse.dom.txt" 10
  interleave_bench bindhot-checker /tmp/orig.binder.bench.test /tmp/store.binder.bench.test \
    '^BenchmarkBindHot$/^checker.ts$' "$OUT/orig.bindhot.checker.txt" "$OUT/store.bindhot.checker.txt" 20
  interleave_bench bindhot-empty /tmp/orig.binder.bench.test /tmp/store.binder.bench.test \
    '^BenchmarkBindHot$/^empty.ts$' "$OUT/orig.bindhot.empty.txt" "$OUT/store.bindhot.empty.txt" 10
  interleave_bench bindhot-dom /tmp/orig.binder.bench.test /tmp/store.binder.bench.test \
    '^BenchmarkBindHot$/^dom.generated.d.ts$' "$OUT/orig.bindhot.dom.txt" "$OUT/store.bindhot.dom.txt" 10
  export GOGC=off
  interleave_bench parse-checker-gogcoff /tmp/orig.parser.bench.test /tmp/store.parser.bench.test \
    '^BenchmarkParse$/^checker.ts$' "$OUT/orig.parse.checker.gogcoff.txt" "$OUT/store.parse.checker.gogcoff.txt" 6
  interleave_bench bindhot-checker-gogcoff /tmp/orig.binder.bench.test /tmp/store.binder.bench.test \
    '^BenchmarkBindHot$/^checker.ts$' "$OUT/orig.bindhot.checker.gogcoff.txt" "$OUT/store.bindhot.checker.gogcoff.txt" 6
  unset GOGC
  if command -v benchstat >/dev/null; then
    {
      echo '======== parse.checker ========'
      benchstat "$OUT/orig.parse.checker.txt" "$OUT/store.parse.checker.txt"
      echo '======== bindhot.checker n=20 ========'
      benchstat "$OUT/orig.bindhot.checker.txt" "$OUT/store.bindhot.checker.txt"
    } | tee "$OUT/benchstat.txt"
  fi
}

cli_once() {
  local bin="$1" slug="$2" project="$3"
  shift 3
  echo "===== $slug =====" | tee -a "$OUT/cli.log"
  GODEBUG=gctrace=1,gcpacertrace=1 /usr/bin/time -l "$bin" --project "$project" "$@" \
    >"$OUT/${slug}.stdout.txt" 2>"$OUT/${slug}.time.txt" || true
  cat "$OUT/${slug}.stdout.txt" | tee -a "$OUT/cli.log"
  cat "$OUT/${slug}.time.txt" | tee -a "$OUT/cli.log"
}

cmd_config_audit() {
  cd "$VSCODE"
  /tmp/tsc-store --showConfig --project src/tsconfig.json --noEmit --declaration false --incremental false \
    >"$OUT/vscode.showConfig.json" || true
  python3 - "$OUT/vscode.showConfig.json" "$OUT/vscode.audit.txt" <<'PY'
import json, sys
from pathlib import Path
p = Path(sys.argv[1])
out = Path(sys.argv[2])
text = p.read_text(errors="replace")
try:
    cfg = json.loads(text)
except json.JSONDecodeError:
    out.write_text("showConfig_not_json\n" + text[:2000])
    raise SystemExit(0)
opts = cfg.get("compilerOptions") or {}
lines = [
    f"incremental={opts.get('incremental')}",
    f"composite={opts.get('composite')}",
    f"tsBuildInfoFile={opts.get('tsBuildInfoFile')}",
    f"noEmit={opts.get('noEmit')}",
    f"declaration={opts.get('declaration')}",
    f"skipLibCheck={opts.get('skipLibCheck')}",
]
out.write_text("\n".join(lines) + "\n")
print("\n".join(lines))
PY
}

cmd_monaco() {
  cd "$VSCODE"
  /tmp/tsc-orig --project src/tsconfig.monaco.json --noEmit --noCheck --declaration false >/dev/null
  /tmp/tsc-store --project src/tsconfig.monaco.json --noEmit --noCheck --declaration false >/dev/null
  : >"$OUT/cli.log"
  local i
  for i in 1 2 3 4 5; do
    cli_once /tmp/tsc-orig "orig-par-$i" src/tsconfig.monaco.json --noEmit --noCheck --declaration false --extendedDiagnostics
    cli_once /tmp/tsc-store "store-par-$i" src/tsconfig.monaco.json --noEmit --noCheck --declaration false --extendedDiagnostics
  done
  for i in 1 2 3 4 5; do
    cli_once /tmp/tsc-orig "orig-st-$i" src/tsconfig.monaco.json --noEmit --noCheck --declaration false --extendedDiagnostics --singleThreaded
    cli_once /tmp/tsc-store "store-st-$i" src/tsconfig.monaco.json --noEmit --noCheck --declaration false --extendedDiagnostics --singleThreaded
  done
  for i in 1 2 3 4 5; do
    GOMAXPROCS=1 cli_once /tmp/tsc-orig "orig-g1-$i" src/tsconfig.monaco.json --noEmit --noCheck --declaration false --extendedDiagnostics --singleThreaded
    GOMAXPROCS=1 cli_once /tmp/tsc-store "store-g1-$i" src/tsconfig.monaco.json --noEmit --noCheck --declaration false --extendedDiagnostics --singleThreaded
  done
  for i in 1 2 3; do
    cli_once /tmp/tsc-orig "orig-check-$i" src/tsconfig.monaco.json --noEmit --declaration false --extendedDiagnostics
    cli_once /tmp/tsc-store "store-check-$i" src/tsconfig.monaco.json --noEmit --declaration false --extendedDiagnostics
  done
}

cmd_vscode() {
  cd "$VSCODE"
  cmd_config_audit
  /tmp/tsc-orig --project src/tsconfig.json --noEmit --declaration false --incremental false >/dev/null || true
  /tmp/tsc-store --project src/tsconfig.json --noEmit --declaration false --incremental false >/dev/null || true
  local i
  for i in 1 2 3; do
    cli_once /tmp/tsc-orig "orig-vs-$i" src/tsconfig.json --noEmit --declaration false --incremental false --extendedDiagnostics
    cli_once /tmp/tsc-store "store-vs-$i" src/tsconfig.json --noEmit --declaration false --incremental false --extendedDiagnostics
  done
}

cmd_summarize() {
  echo "identity=$OUT/identity.txt"
  [[ -f "$OUT/benchstat.txt" ]] && echo "benchstat=$OUT/benchstat.txt"
  [[ -f "$OUT/kpc.status.txt" ]] && echo "kpc=$(cat "$OUT/kpc.status.txt")"
  [[ -f "$OUT/vscode.audit.txt" ]] && echo "vscode_audit=$OUT/vscode.audit.txt"
  local gc
  gc=$(ls "$OUT"/*.time.txt 2>/dev/null || true)
  if [[ -n "$gc" ]]; then
    python3 "$STORE_ROOT/tools/scripts/tsc/summarize-gc-trace.py" $gc | tee "$OUT/gc-summary.txt"
  fi
  echo "fill $STORE_ROOT/tsc/internal/ast/docs/parse-bind-vs-8ac035a-report.md from $OUT"
}

[[ "$want_identity" == 1 ]] && cmd_identity
[[ "$want_overlay" == 1 ]] && cmd_overlay
[[ "$want_build" == 1 ]] && cmd_build
[[ "$want_kpc" == 1 ]] && cmd_kpc
[[ "$want_wall" == 1 ]] && cmd_wall
[[ "$want_monaco" == 1 ]] && cmd_monaco
[[ "$want_vscode" == 1 ]] && cmd_vscode
[[ "$want_summarize" == 1 ]] && cmd_summarize
[[ "$want_restore" == 1 ]] && cmd_restore
