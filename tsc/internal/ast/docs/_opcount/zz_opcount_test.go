package compiler_test

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// TestOpCount runs the AST benchmark project once, single threaded, and dumps
// the accessor counters per phase. Emit uses the JS emit config of the Emit bench.
func TestOpCount(t *testing.T) {
	path := os.Getenv("OPCOUNT_OUT")
	if path == "" {
		t.Skip("OPCOUNT_OUT not set")
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	project := loadASTBenchmarkProject(t)
	ast.OpCountReset()
	program := project.newProgram(project.config, core.TSTrue)
	ast.OpCountDump(w, "parse")
	program.BindSourceFiles()
	ast.OpCountDump(w, "bind")
	if d := program.GetSemanticDiagnostics(context.Background(), nil); len(d) != 0 {
		t.Fatalf("diagnostics: %s", formatDiagnostics(d))
	}
	ast.OpCountDump(w, "check")

	// node census per file, outside the counted windows
	var visit ast.Visitor
	var counts [ast.KindCount]uint64
	var walk func(n *ast.Node)
	visit = func(n *ast.Node) bool { walk(n); return false }
	walk = func(n *ast.Node) {
		if n == nil {
			return
		}
		counts[n.Kind]++
		n.ForEachChild(visit)
	}
	for _, file := range program.SourceFiles() {
		counts = [ast.KindCount]uint64{}
		walk(file.AsNode())
		for k, c := range counts {
			if c != 0 {
				fmt.Fprintf(w, "census\t%s\t%s\t%d\n", file.FileName(), ast.Kind(k).String(), c)
			}
		}
	}
	ast.OpCountReset()

	emitConfig := newASTBenchmarkJSEmitConfig(project.config)
	program = project.newProgram(emitConfig, core.TSTrue)
	program.BindSourceFiles()
	if d := program.GetSemanticDiagnostics(context.Background(), nil); len(d) != 0 {
		t.Fatalf("diagnostics: %s", formatDiagnostics(d))
	}
	ast.OpCountReset()
	result := program.Emit(context.Background(), compiler.EmitOptions{WriteFile: func(string, string, *compiler.WriteFileData) error { return nil }})
	ast.OpCountDump(w, "emit")
	if result == nil || result.EmitSkipped {
		t.Fatal("emit skipped")
	}
}
