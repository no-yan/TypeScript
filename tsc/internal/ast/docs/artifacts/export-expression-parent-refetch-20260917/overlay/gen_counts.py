#!/usr/bin/env python3
"""Generate a go -overlay that counts Handle.Parent calls and
isExportOrExportExpression calls/ancestor visits, printed at exit when
TSGO_EXPORT_EXPR_COUNTS is set. Production sources are untouched."""
import json, sys
from pathlib import Path
repo = Path('/Volumes/SanDisk1TB/worktree/binder-rewrite/tsc')
checker_src, out = Path(sys.argv[1]), Path(sys.argv[2])
out.mkdir(parents=True, exist_ok=True)
def inject(s, needle, line):
    assert s.count(needle) == 1, needle
    return s.replace(needle, needle + "\n" + line, 1)
store = (repo/'internal/ast/store.go').read_text()
store = inject(store, 'func (h Handle) Parent() Handle {', '\tExportExprParentCalls.Add(1)')
(out/'store.go').write_text(store)
(out/'zz_counts.go').write_text('''package ast

import "sync/atomic"

var (
	ExportExprParentCalls atomic.Int64
	ExportExprCalls       atomic.Int64
	ExportExprVisits      atomic.Int64
)
''')
ck = checker_src.read_text()
ck = inject(ck, 'func isExportOrExportExpression(location ast.Handle) bool {', '\tast.ExportExprCalls.Add(1)')
ck = inject(ck, '\treturn !ast.FindAncestor(location, func(n ast.Handle) bool {', '\t\tast.ExportExprVisits.Add(1)')
(out/'checker.go').write_text(ck)
mn = (repo/'cmd/tsc/main.go').read_text()
mn = mn.replace('import (\n\t"context"\n', 'import (\n\t"context"\n\t"fmt"\n\t"github.com/microsoft/TypeScript/tsc/internal/ast"\n', 1)
mn = inject(mn, '\tresult := execute.CommandLine(ctx, newSystem(), args, nil)',
  '\tif os.Getenv("TSGO_EXPORT_EXPR_COUNTS") != "" {\n\t\tfmt.Fprintf(os.Stderr, "{\\"parent_calls\\":%d,\\"export_expr_calls\\":%d,\\"export_expr_visits\\":%d}\\n", ast.ExportExprParentCalls.Load(), ast.ExportExprCalls.Load(), ast.ExportExprVisits.Load())\n\t}')
(out/'main.go').write_text(mn)
ov = {"Replace": {
  str(repo/'internal/ast/store.go'): str(out/'store.go'),
  str(repo/'internal/ast/zz_export_expr_counts.go'): str(out/'zz_counts.go'),
  str(repo/'internal/checker/checker.go'): str(out/'checker.go'),
  str(repo/'cmd/tsc/main.go'): str(out/'main.go'),
}}
(out/'overlay.json').write_text(json.dumps(ov, indent=2)+"\n")
print(out/'overlay.json')
