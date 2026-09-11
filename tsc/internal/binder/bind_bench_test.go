//go:build binderinvestigation

package binder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// Shared verbatim with the pointer checkout through a Go overlay. The manifest
// pins source bytes and paths; missing fixtures must fail, never silently skip.
type investigationFixture struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func investigationFixtures(tb testing.TB) []investigationFixture {
	tb.Helper()
	path := os.Getenv("BINDER_INVESTIGATION_MANIFEST")
	if path == "" {
		tb.Fatal("BINDER_INVESTIGATION_MANIFEST is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatal(err)
	}
	var fixtures []investigationFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		tb.Fatal(err)
	}
	if len(fixtures) == 0 {
		tb.Fatal("empty fixture manifest")
	}
	seen := make(map[string]bool)
	for _, f := range fixtures {
		if f.Name == "" || seen[f.Name] {
			tb.Fatalf("empty or duplicate fixture name: %q", f.Name)
		}
		seen[f.Name] = true
	}
	return fixtures
}

func (f investigationFixture) source(tb testing.TB) string {
	tb.Helper()
	data, err := os.ReadFile(f.Path)
	if err != nil {
		tb.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != f.SHA256 {
		tb.Fatalf("fixture changed: %s", f.Path)
	}
	return string(data)
}

func (f investigationFixture) parse(text string) *ast.SourceFile {
	return parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: f.Path,
		Path:     tspath.Path(f.Path),
	}, text, core.GetScriptKindFromFileName(f.Path))
}

// Bound preparation memory even when a caller forgets -benchtime=10x.
// Go's initial N=1 calibration is supported and releases its own batch.
const investigationMaxBatch = 10

func investigationBatch(tb testing.TB, f investigationFixture, text string, n int) ([]*ast.SourceFile, func()) {
	tb.Helper()
	if n < 1 || n > investigationMaxBatch {
		tb.Fatalf("bind benchmark requires -benchtime=10x (batch size must be 1..%d, got %d)", investigationMaxBatch, n)
	}
	baseline := investigationRegisteredStores()
	files := make([]*ast.SourceFile, n)
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		for i, file := range files {
			if file != nil {
				investigationReleaseFile(file)
			}
			files[i] = nil
		}
		if got := investigationRegisteredStores(); got != baseline {
			tb.Errorf("Store registration leak: before=%d after=%d", baseline, got)
		}
	}
	tb.Cleanup(release)
	seen := make(map[*ast.SourceFile]bool, n)
	for i := range files {
		files[i] = f.parse(text)
		if files[i].IsBound() || seen[files[i]] {
			tb.Fatal("each timed bind requires a distinct, unbound AST")
		}
		seen[files[i]] = true
	}
	return files, release
}

func BenchmarkBindInvestigation(b *testing.B) {
	for _, f := range investigationFixtures(b) {
		b.Run(f.Name, func(b *testing.B) {
			b.StopTimer()
			text := f.source(b)
			files, release := investigationBatch(b, f, text, b.N)
			// One explicit collection for the prepared batch, never between binds.
			runtime.GC()
			if os.Getenv("BINDER_BENCH_GC_OFF") == "1" {
				previous := debug.SetGCPercent(-1)
				defer debug.SetGCPercent(previous)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.StartTimer()
			for _, file := range files {
				BindSourceFile(file)
			}
			b.StopTimer()
			runtime.KeepAlive(files)
			for _, file := range files {
				if !file.IsBound() {
					b.Fatal("AST was not bound")
				}
			}
			release()
		})
	}
}
