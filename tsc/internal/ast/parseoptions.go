package ast

import (
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

type SourceFileParseOptions struct {
	FileName                       string
	Path                           tspath.Path
	ExternalModuleIndicatorOptions ExternalModuleIndicatorOptions
}

type ExternalModuleIndicatorOptions struct {
	JSX   bool
	Force bool
}

func GetExternalModuleIndicatorOptions(fileName string, options *core.CompilerOptions, metadata SourceFileMetaData) ExternalModuleIndicatorOptions {
	if tspath.IsDeclarationFileName(fileName) {
		return ExternalModuleIndicatorOptions{}
	}

	switch options.GetEmitModuleDetectionKind() {
	case core.ModuleDetectionKindForce:
		return ExternalModuleIndicatorOptions{Force: true}
	case core.ModuleDetectionKindLegacy:
		return ExternalModuleIndicatorOptions{}
	case core.ModuleDetectionKindAuto:
		return ExternalModuleIndicatorOptions{
			JSX:   options.Jsx == core.JsxEmitReactJSX || options.Jsx == core.JsxEmitReactJSXDev,
			Force: isFileForcedToBeModuleByFormat(fileName, options, metadata),
		}
	default:
		return ExternalModuleIndicatorOptions{}
	}
}

var isFileForcedToBeModuleByFormatExtensions = []string{tspath.ExtensionCjs, tspath.ExtensionCts, tspath.ExtensionMjs, tspath.ExtensionMts}

func isFileForcedToBeModuleByFormat(fileName string, options *core.CompilerOptions, metadata SourceFileMetaData) bool {
	if GetImpliedNodeFormatForEmitWorker(fileName, options.GetEmitModuleKind(), metadata) == core.ModuleKindESNext || tspath.FileExtensionIsOneOf(fileName, isFileForcedToBeModuleByFormatExtensions) {
		return true
	}
	return false
}

func SetExternalModuleIndicator(file *SourceFile, opts ExternalModuleIndicatorOptions) {
	file.ExternalModuleIndicator = getExternalModuleIndicator(file, opts)
}

func getExternalModuleIndicator(file *SourceFile, opts ExternalModuleIndicatorOptions) Handle {
	if file.ScriptKind == core.ScriptKindJSON {
		return Handle{}
	}

	if node := isFileProbablyExternalModule(file); !node.IsNil() {
		return node
	}

	if file.IsDeclarationFile {
		return Handle{}
	}

	if opts.JSX {
		if node := isFileModuleFromUsingJSXTag(file); !node.IsNil() {
			return node
		}
	}

	if opts.Force {
		return file.ParseRoot()
	}

	return Handle{}
}

func isFileProbablyExternalModule(sourceFile *SourceFile) Handle {
	for _, statement := range sourceFile.ParseRoot().Statements() {
		if isAnExternalModuleIndicatorNode(statement) {
			return statement
		}
	}
	return getImportMetaIfNecessary(sourceFile)
}

func isAnExternalModuleIndicatorNode(node Handle) bool {
	if node.IsNil() {
		return false
	}
	if node.ModifierFlags()&ModifierFlagsExport != 0 {
		return true
	}
	switch node.Kind {
	case KindImportEqualsDeclaration:
		ref := node.ImportEqualsDeclarationModuleReference()
		return !ref.IsNil() && ref.Kind == KindExternalModuleReference
	case KindImportDeclaration, KindExportAssignment, KindExportDeclaration:
		return true
	default:
		return false
	}
}

func getImportMetaIfNecessary(sourceFile *SourceFile) Handle {
	root := sourceFile.ParseRoot()
	if root.Flags()&NodeFlagsPossiblyContainsImportMeta == 0 {
		return Handle{}
	}
	return findChildHandle(root, func(n Handle) bool {
		return !n.IsNil() && n.Kind == KindMetaProperty &&
			n.MetaPropertyKeywordToken() == KindImportKeyword &&
			!n.Name().IsNil() && n.Name().Text() == "meta"
	})
}

func findChildHandle(root Handle, check func(Handle) bool) Handle {
	var result Handle
	var visit StoreVisitor
	visit = func(node Handle) bool {
		if check(node) {
			result = node
			return true
		}
		return node.ForEachChild(visit)
	}
	visit(root)
	return result
}

func isFileModuleFromUsingJSXTag(file *SourceFile) Handle {
	return walkTreeForJSXTags(file.ParseRoot())
}

func walkTreeForJSXTags(node Handle) Handle {
	var found Handle
	var visitor StoreVisitor
	visitor = func(n Handle) bool {
		if !found.IsNil() {
			return true
		}
		switch n.Kind {
		case KindJsxOpeningElement, KindJsxSelfClosingElement, KindJsxFragment:
			found = n
			return true
		}
		return n.ForEachChild(visitor)
	}
	visitor(node)
	return found
}
