package storecompiler

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
)

// IDSpace is the program's StoreID namespace (store-cli-design-20261004.md
// 4.3): the checker's region is made in it.
func (p *Program) IDSpace() *store.IDSpace { return p.opts.idSpace }

var _ storechecker.Program = (*Program)(nil)

// ModeForUsageLocation is the resolution mode of the module specifier usage of
// a file whose metadata is meta: the loader's, for a program that keeps the
// metadata elsewhere (the H1 harness, storeharness/mode.go).
func ModeForUsageLocation(fileName string, meta ast.SourceFileMetaData, usage store.Node, options *core.CompilerOptions) core.ResolutionMode {
	return getModeForUsageLocation(fileName, meta, usage, options)
}

// EmitSyntaxForUsageLocation is GetEmitSyntaxForUsageLocation's worker, as
// ModeForUsageLocation.
func EmitSyntaxForUsageLocation(fileName string, meta ast.SourceFileMetaData, usage store.Node, options *core.CompilerOptions) core.ResolutionMode {
	return getEmitSyntaxForUsageLocationWorker(fileName, meta, usage, options)
}
