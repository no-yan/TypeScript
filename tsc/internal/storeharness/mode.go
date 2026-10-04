package storeharness

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
)

// C (5.2): the methods that read a module specifier. The mode is the native
// Store loader's (storecompiler, translated from compiler/fileloader.go) on the
// Pointer program's metadata.

// GetModeForUsageLocation is compiler.Program.GetModeForUsageLocation
// (program.go) and getModeForUsageLocation (fileloader.go).
func (p *Program) GetModeForUsageLocation(file ast.HasFileName, usage store.Node) core.ResolutionMode {
	return storecompiler.ModeForUsageLocation(file.FileName(), p.pointer.GetSourceFileMetaData(file.Path()), usage, p.optionsForFile(file))
}

// GetEmitSyntaxForUsageLocation is compiler.Program.GetEmitSyntaxForUsageLocation
// (program.go) and getEmitSyntaxForUsageLocationWorker (fileloader.go).
func (p *Program) GetEmitSyntaxForUsageLocation(file ast.HasFileName, usage store.Node) core.ResolutionMode {
	return storecompiler.EmitSyntaxForUsageLocation(file.FileName(), p.pointer.GetSourceFileMetaData(file.Path()), usage, p.optionsForFile(file))
}

// GetResolvedModuleFromModuleSpecifier is
// compiler.Program.GetResolvedModuleFromModuleSpecifier (program.go).
func (p *Program) GetResolvedModuleFromModuleSpecifier(file ast.HasFileName, moduleSpecifier store.Node) *module.ResolvedModule {
	if !store.IsStringLiteralLike(moduleSpecifier) {
		panic("moduleSpecifier must be a StringLiteralLike")
	}
	mode := p.GetModeForUsageLocation(file, moduleSpecifier)
	return p.pointer.GetResolvedModule(file, moduleSpecifier.Text(), mode)
}

// optionsForFile is the Pointer's projectReferenceFileMapper.getCompilerOptionsForFile.
func (p *Program) optionsForFile(file ast.HasFileName) *core.CompilerOptions {
	return module.GetCompilerOptionsWithRedirect(p.pointer.Options(), p.pointer.GetRedirectForResolution(file))
}
