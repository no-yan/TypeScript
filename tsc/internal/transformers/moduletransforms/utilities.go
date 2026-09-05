package moduletransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/outputpaths"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

func isDeclarationNameOfEnumOrNamespace(emitContext *printer.EmitContext, node ast.Handle) bool {
	if original := emitContext.MostOriginal(node); !original.IsNil() && !original.Parent().IsNil() {
		switch original.Parent().Kind {
		case ast.KindEnumDeclaration, ast.KindModuleDeclaration:
			return original == original.Parent().Name()
		}
	}
	return false
}
func rewriteModuleSpecifier(emitContext *printer.EmitContext, node ast.Handle, compilerOptions *core.CompilerOptions) ast.Handle {
	if node.IsNil() || !ast.IsStringLiteral(node) || !core.ShouldRewriteModuleSpecifier(node.Text(), compilerOptions) {
		return node
	}
	updatedText := tspath.ChangeExtension(node.Text(), outputpaths.GetOutputExtension(node.Text(), compilerOptions.Jsx))
	if updatedText != node.Text() {
		updated := emitContext.Factory.NewStringLiteral(updatedText, node.StringLiteralTokenFlags())
		emitContext.SetOriginal(updated, node)
		emitContext.AssignCommentAndSourceMapRanges(updated, node)
		return updated
	}
	return node
}
func createEmptyImports(factory *printer.NodeFactory) ast.Handle {
	return factory.NewExportDeclaration(0, false, factory.NewNamedExports(factory.NewList(nil)), ast.Handle{}, ast.Handle{})
}

func getExternalModuleNameLiteral(factory *printer.NodeFactory, importNode ast.Handle, sourceFile *ast.SourceFile, host any, resolver printer.EmitResolver, compilerOptions *core.CompilerOptions) ast.Handle {
	moduleName := ast.GetExternalModuleName(importNode)
	if !moduleName.IsNil() && ast.IsStringLiteral(moduleName) {
		name := tryGetModuleNameFromDeclaration(importNode, host, factory, resolver, compilerOptions)
		if name.IsNil() {
			name = tryRenameExternalModule(factory, moduleName, sourceFile)
		}
		if name.IsNil() {
			name = factory.NewStringLiteral(moduleName.Text(), ast.TokenFlagsNone)
		}
		return name
	}
	return ast.Handle{}
}

func tryGetModuleNameFromFile(factory *printer.NodeFactory, file *ast.SourceFile, host any, options *core.CompilerOptions) ast.Handle {
	if file == nil {
		return ast.Handle{}
	}
	return ast.Handle{}
}
func tryGetModuleNameFromDeclaration(declaration ast.Handle, host any, factory *printer.NodeFactory, resolver printer.EmitResolver, compilerOptions *core.CompilerOptions) ast.Handle {
	if resolver == nil {
		return ast.Handle{}
	}
	return tryGetModuleNameFromFile(factory, resolver.GetExternalModuleFileFromDeclaration(declaration), host, compilerOptions)
}

func getExternalModuleNameFromPath(host any, fileName string, referencePath string) string {
	return ""
}

func tryRenameExternalModule(factory *printer.NodeFactory, moduleName ast.Handle, sourceFile *ast.SourceFile) ast.Handle {
	return ast.Handle{}
}
func isFileLevelReservedGeneratedIdentifier(emitContext *printer.EmitContext, name ast.Handle) bool {
	info := emitContext.GetAutoGenerateInfo(name)
	return info != nil && info.Flags.IsFileLevel() && info.Flags.IsOptimistic() && info.Flags.IsReservedInNestedScopes()
}

func isSimpleInlineableExpression(expression ast.Handle) bool {
	return !ast.IsIdentifier(expression) && transformers.IsSimpleCopiableExpression(expression)
}
