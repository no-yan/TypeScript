package storechecker_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
)

// A program with no file (compiler/fileloader.go adds no lib without a root):
// NewChecker ends normally, in the Pointer and in the Store, with the region
// made from the program's IDSpace, not from a first file
// (internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 6.5). The stubs embed the Program interface, so a method that NewChecker
// starts to call and the stub lacks panics on the nil interface.

type emptyStoreProgram struct {
	storechecker.Program
	space *store.IDSpace
}

func (p *emptyStoreProgram) Options() *core.CompilerOptions { return &core.CompilerOptions{} }
func (p *emptyStoreProgram) SourceFiles() []*store.File     { return nil }
func (p *emptyStoreProgram) BindSourceFiles()               {}
func (p *emptyStoreProgram) IDSpace() *store.IDSpace        { return p.space }

type emptyPointerProgram struct {
	checker.Program
}

func (p *emptyPointerProgram) Options() *core.CompilerOptions { return &core.CompilerOptions{} }
func (p *emptyPointerProgram) SourceFiles() []*ast.SourceFile { return nil }
func (p *emptyPointerProgram) BindSourceFiles()               {}

func TestNewCheckerEmptyProgram(t *testing.T) {
	pc, _ := checker.NewChecker(&emptyPointerProgram{}, nil)
	if pc == nil {
		t.Fatal("the Pointer checker is nil")
	}
	sc, _ := storechecker.NewChecker(&emptyStoreProgram{space: store.NewIDSpace(0)}, nil)
	if sc == nil {
		t.Fatal("the Store checker is nil")
	}
}
