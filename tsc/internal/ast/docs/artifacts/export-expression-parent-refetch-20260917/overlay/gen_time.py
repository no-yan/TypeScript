import json, sys
from pathlib import Path
repo = Path('/Volumes/SanDisk1TB/worktree/binder-rewrite/tsc')
checker_src, out = Path(sys.argv[1]), Path(sys.argv[2])
out.mkdir(parents=True, exist_ok=True)
def inject(s, needle, line):
    assert s.count(needle) == 1, needle
    return s.replace(needle, needle + "\n" + line, 1)
(out/'zz_counts.go').write_text('''package ast

import (
	"sync/atomic"
	"time"
)

var (
	ExportExprCalls atomic.Int64
	ExportExprNanos atomic.Int64
	ExportExprNull  atomic.Int64
)

func ExportExprStart() int64 { return time.Now().UnixNano() }

func ExportExprEnd(start int64) {
	end := time.Now().UnixNano()
	ExportExprNanos.Add(end - start)
	n0 := time.Now().UnixNano()
	n1 := time.Now().UnixNano()
	ExportExprNull.Add(n1 - n0)
}
''')
ck = checker_src.read_text()
ck = inject(ck, 'func isExportOrExportExpression(location ast.Handle) bool {', '\tast.ExportExprCalls.Add(1)\n\tdefer ast.ExportExprEnd(ast.ExportExprStart())')
(out/'checker.go').write_text(ck)
mn = (repo/'cmd/tsc/main.go').read_text()
mn = mn.replace('import (\n\t"context"\n', 'import (\n\t"context"\n\t"fmt"\n\t"github.com/microsoft/TypeScript/tsc/internal/ast"\n', 1)
mn = inject(mn, '\tresult := execute.CommandLine(ctx, newSystem(), args, nil)',
  '\tif os.Getenv("TSGO_EXPORT_EXPR_COUNTS") != "" {\n\t\tfmt.Fprintf(os.Stderr, "{\\"export_expr_calls\\":%d,\\"export_expr_nanos\\":%d,\\"null_pair_nanos\\":%d}\\n", ast.ExportExprCalls.Load(), ast.ExportExprNanos.Load(), ast.ExportExprNull.Load())\n\t}')
(out/'main.go').write_text(mn)
ov = {"Replace": {
  str(repo/'internal/ast/zz_export_expr_counts.go'): str(out/'zz_counts.go'),
  str(repo/'internal/checker/checker.go'): str(out/'checker.go'),
  str(repo/'cmd/tsc/main.go'): str(out/'main.go'),
}}
(out/'overlay.json').write_text(json.dumps(ov, indent=2)+"\n")
