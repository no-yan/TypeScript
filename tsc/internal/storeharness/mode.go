package storeharness

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/module"
)

// C (5.2): the methods that read a module specifier, written by hand from the
// Pointer's, in the same order of tests with the Store's predicates. They are
// not translated: the adapter is the checker's counterpart, not its copy.

// GetModeForUsageLocation is compiler.Program.GetModeForUsageLocation
// (program.go) and getModeForUsageLocation (fileloader.go).
func (p *Program) GetModeForUsageLocation(file ast.HasFileName, usage store.Node) core.ResolutionMode {
	return getModeForUsageLocation(file.FileName(), p.pointer.GetSourceFileMetaData(file.Path()), usage, p.optionsForFile(file))
}

// GetEmitSyntaxForUsageLocation is compiler.Program.GetEmitSyntaxForUsageLocation
// (program.go) and getEmitSyntaxForUsageLocationWorker (fileloader.go).
func (p *Program) GetEmitSyntaxForUsageLocation(file ast.HasFileName, usage store.Node) core.ResolutionMode {
	return getEmitSyntaxForUsageLocationWorker(file.FileName(), p.pointer.GetSourceFileMetaData(file.Path()), usage, p.optionsForFile(file))
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

func getModeForUsageLocation(fileName string, meta ast.SourceFileMetaData, usage store.Node, options *core.CompilerOptions) core.ResolutionMode {
	parent := usage.Parent()
	if store.IsImportDeclaration(parent) || parent.Kind() == ast.KindJSImportDeclaration || store.IsExportDeclaration(parent) || parent.Kind() == ast.KindJSDocImportTag {
		isTypeOnly := store.IsExclusivelyTypeOnlyImportOrExport(parent)
		if isTypeOnly {
			var override core.ResolutionMode
			var ok bool
			switch parent.Kind() {
			case ast.KindImportDeclaration, ast.KindJSImportDeclaration:
				override, ok = parent.AsImportDeclaration().Attributes().GetResolutionModeOverride(nil)
			case ast.KindExportDeclaration:
				override, ok = parent.AsExportDeclaration().Attributes().GetResolutionModeOverride(nil)
			case ast.KindJSDocImportTag:
				override, ok = parent.AsJSDocImportTag().Attributes().GetResolutionModeOverride(nil)
			}
			if ok {
				return override
			}
		}
	}
	if store.IsLiteralTypeNode(parent) && store.IsImportTypeNode(parent.Parent()) {
		if override, ok := parent.Parent().AsImportTypeNode().Attributes().GetResolutionModeOverride(nil); ok {
			return override
		}
	}

	if options != nil && importSyntaxAffectsModuleResolution(options) {
		return getEmitSyntaxForUsageLocationWorker(fileName, meta, usage, options)
	}

	return core.ResolutionModeNone
}

// importSyntaxAffectsModuleResolution is fileloader.go's: it reads no node.
func importSyntaxAffectsModuleResolution(options *core.CompilerOptions) bool {
	moduleResolution := options.GetModuleResolutionKind()
	return core.ModuleResolutionKindNode16 <= moduleResolution && moduleResolution <= core.ModuleResolutionKindNodeNext ||
		options.GetResolvePackageJsonExports() || options.GetResolvePackageJsonImports()
}

func getEmitSyntaxForUsageLocationWorker(fileName string, meta ast.SourceFileMetaData, usage store.Node, options *core.CompilerOptions) core.ResolutionMode {
	parent := usage.Parent()
	if store.IsRequireCall(parent, false /*requireStringLiteralLikeArgument*/) || store.IsExternalModuleReference(parent) && store.IsImportEqualsDeclaration(parent.Parent()) {
		return core.ModuleKindCommonJS
	}
	fileEmitMode := ast.GetEmitModuleFormatOfFileWorker(fileName, options, meta)
	if store.IsImportCall(store.WalkUpParenthesizedExpressions(parent)) {
		if ast.ShouldTransformImportCall(fileName, options, fileEmitMode) {
			return core.ModuleKindCommonJS
		} else {
			return core.ModuleKindESNext
		}
	}
	// If we're in --module preserve on an input file, we know that an import
	// is an import. But if this is a declaration file, we'd prefer to use the
	// impliedNodeFormat. Since we want things to be consistent between the two,
	// we need to issue errors when the user writes ESM syntax in a definitely-CJS
	// file, until/unless declaration emit can indicate a true ESM import. On the
	// other hand, writing CJS syntax in a definitely-ESM file is fine, since declaration
	// emit preserves the CJS syntax.
	if fileEmitMode == core.ModuleKindCommonJS {
		return core.ModuleKindCommonJS
	} else {
		if fileEmitMode.IsNonNodeESM() || fileEmitMode == core.ModuleKindPreserve {
			return core.ModuleKindESNext
		}
	}
	return core.ModuleKindNone
}
