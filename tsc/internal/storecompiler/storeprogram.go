package storecompiler

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
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

// configFile is the Store file of a Pointer config file (tsconfig.json or a
// file it extends) for a diagnostic that points into it: the same name and
// text, parsed outside the program, so that the diagnostic sorts and prints
// as the Pointer's. A nil file stays nil.
func (p *Program) configFile(f *ast.SourceFile) *store.File {
	if f == nil {
		return nil
	}
	p.configFilesMu.Lock()
	defer p.configFilesMu.Unlock()
	if sf := p.configFiles[f]; sf != nil {
		return sf
	}
	if p.configFiles == nil {
		p.configFiles = map[*ast.SourceFile]*store.File{}
	}
	sf := storeparser.ParseSourceFile(f.ParseOptions(), f.Text(), f.ScriptKind)
	p.configFiles[f] = sf
	return sf
}

// isolatedEntityNameIsNil is parser.ParseIsolatedEntityName(text) == nil with
// the Store parser, in an IDSpace of its own: the name is not kept.
func isolatedEntityNameIsNil(text string) bool {
	return storeparser.ParseIsolatedEntityName(store.NewIDSpace(0), text).IsNil()
}
