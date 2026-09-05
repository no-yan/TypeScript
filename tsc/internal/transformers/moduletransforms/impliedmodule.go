package moduletransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type ImpliedModuleTransformer struct {
	transformers.Transformer
	opts                      *transformers.TransformOptions
	resolver                  binder.ReferenceResolver
	getEmitModuleFormatOfFile func(file ast.HasFileName) core.ModuleKind
	cjsTransformer            *transformers.Transformer
	esmTransformer            *transformers.Transformer
}

func NewImpliedModuleTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &ImpliedModuleTransformer{opts: opts, resolver: opts.Resolver, getEmitModuleFormatOfFile: opts.GetEmitModuleFormatOfFile}
	return tx.NewTransformer(tx.visit, opts.Context)
}
func (tx *ImpliedModuleTransformer) visit(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindSourceFile:
		node = tx.visitSourceFile(node)
	}
	return node
}
func (tx *ImpliedModuleTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	if ast.GetSourceFileOfNode(node) != nil && ast.GetSourceFileOfNode(node).IsDeclarationFile {
		return node
	}
	format := tx.getEmitModuleFormatOfFile(ast.GetSourceFileOfNode(node))
	var transformer *transformers.Transformer
	if format >= core.ModuleKindES2015 {
		if tx.esmTransformer == nil {
			tx.esmTransformer = NewESModuleTransformer(tx.opts)
		}
		transformer = tx.esmTransformer
	} else {
		if tx.cjsTransformer == nil {
			tx.cjsTransformer = NewCommonJSModuleTransformer(tx.opts)
		}
		transformer = tx.cjsTransformer
	}
	return transformer.TransformSourceFile(ast.GetSourceFileOfNode(node)).ParseRoot()
}
