package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

func NewUseStrictTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &useStrictTransformer{compilerOptions: opts.CompilerOptions, getEmitModuleFormatOfFile: opts.GetEmitModuleFormatOfFile}
	return tx.NewTransformer(tx.visit, opts.Context)
}

type useStrictTransformer struct {
	transformers.Transformer
	compilerOptions           *core.CompilerOptions
	getEmitModuleFormatOfFile func(file ast.HasFileName) core.ModuleKind
}

func (tx *useStrictTransformer) visit(node ast.Handle) ast.Handle {
	if node.Kind != ast.KindSourceFile {
		return node
	}
	return tx.visitSourceFile(node)
}
func (tx *useStrictTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	file := ast.GetSourceFileOfNode(node)
	if file != nil && file.ScriptKind == core.ScriptKindJSON {
		return node
	}
	isExternalModule := ast.IsExternalModule(file)
	moduleKind := tx.compilerOptions.GetEmitModuleKind()
	format := tx.getEmitModuleFormatOfFile(file)
	if isExternalModule && moduleKind >= core.ModuleKindES2015 && (moduleKind == core.ModuleKindPreserve || format >= core.ModuleKindES2015) {
		return node
	}
	statements := tx.Factory().EnsureUseStrict(node.Statements())
	statementList := tx.Factory().List(node.Store().ListLoc(node.StatementList()), statements...)
	return tx.Factory().UpdateSourceFile(node, statementList, node.EndOfFileToken())
}
