#!/usr/bin/env bash
# Recount Handle-ish Foo() vs FooSeq() in tsc/internal/checker.
# Excludes common *Type / InterfaceType TypeParameters and Type.Types noise.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
CHK="$ROOT/tsc/internal/checker"
cd "$ROOT"

exclude='InterfaceType|TupleType|TypeReference|SubstitutionType|containingType\.Types|contextualType\.Types|unionTarget|overlap\.Types|source\.Types|target\.Types|alias\.TypeArguments|Signature|sig\.Parameters|Mapper|globalGeneratorType|globalIteratorType|TypeFlags'

count_lines() {
  # stdin may be empty
  wc -l | tr -d ' '
}

printf '%-16s %8s %8s\n' accessor Foo FooSeq
for name in Arguments Parameters Statements Elements Members Properties Tags TypeArguments TypeParameters; do
  foo=$(rg -n --glob '*.go' "\\.${name}\\(\\)" "$CHK" 2>/dev/null | rg -v "$exclude" | count_lines || true)
  seq=$(rg -n --glob '*.go' "\\.${name}Seq\\(\\)" "$CHK" 2>/dev/null | count_lines || true)
  foo=${foo:-0}
  seq=${seq:-0}
  printf '%-16s %8s %8s\n' "${name}()" "$foo" "$seq"
done

echo
echo 'Seq().Len / First / Slice (checker):'
hits=$(rg -n --glob '*.go' 'Seq\(\)\.(Len|First|At|Last|Slice)\(|DeclarationNodes\([^)]+\)\.(Len|First|Slice)\(' "$CHK" 2>/dev/null | count_lines || true)
echo "hits ${hits:-0}"

echo
echo 'DeclarationNodes(...).Slice:'
rg -n --glob '*.go' 'DeclarationNodes\([^)]+\)\.Slice\(\)' "$CHK" 2>/dev/null || true
