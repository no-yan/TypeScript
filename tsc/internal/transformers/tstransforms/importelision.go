package tstransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type ImportElisionTransformer struct {
	transformers.Transformer
	compilerOptions   *core.CompilerOptions
	currentSourceFile *ast.SourceFile
	emitResolver      printer.EmitResolver
}

func NewImportElisionTransformer(opt *transformers.TransformOptions) *transformers.Transformer {
	compilerOptions := opt.CompilerOptions
	emitContext := opt.Context
	if compilerOptions.VerbatimModuleSyntax.IsTrue() {
		panic("ImportElisionTransformer should not be used with VerbatimModuleSyntax")
	}
	tx := &ImportElisionTransformer{compilerOptions: compilerOptions, emitResolver: opt.EmitResolver}
	return tx.NewTransformer(tx.visit, emitContext)
}
func (tx *ImportElisionTransformer) visit(node ast.Handle) ast.Handle {
	if ast.IsSourceFile(node) && tx.emitResolver != nil {
		tx.emitResolver.MarkLinkedReferencesRecursively(ast.GetSourceFileOfNode(tx.EmitContext().MostOriginal(node)))
	}
	switch node.Kind {
	case ast.KindImportEqualsDeclaration:
		if ast.IsExternalModuleImportEqualsDeclaration(node) {
			if !tx.shouldEmitAliasDeclaration(node) {
				return ast.Handle{}
			}
		} else {
			if !tx.shouldEmitImportEqualsDeclaration(node) {
				return ast.Handle{}
			}
		}
		return tx.Visitor().VisitEachChild(node)
	case ast.KindImportDeclaration:
		n := node
		if !n.ImportClause().IsNil() {
			importClause := tx.Visitor().VisitNode(n.ImportClause())
			if importClause.IsNil() {
				return ast.Handle{}
			}
			return tx.Factory().UpdateImportDeclaration(n, n.Modifiers(), importClause, n.ModuleSpecifier(), tx.Visitor().VisitNode(n.Attributes()))
		}
		return tx.Visitor().VisitEachChild(node)
	case ast.KindImportClause:
		n := node
		name := core.IfElse(tx.shouldEmitAliasDeclaration(node), n.Name(), ast.Handle{})
		namedBindings := tx.Visitor().VisitNode(n.NamedBindings())
		if name.IsNil() && namedBindings.IsNil() {
			return ast.Handle{}
		}
		return tx.Factory().UpdateImportClause(n, n.ImportClausePhaseModifier(), name, namedBindings)
	case ast.KindNamespaceImport:
		if !tx.shouldEmitAliasDeclaration(node) {
			return ast.Handle{}
		}
		return node
	case ast.KindNamedImports:
		n := node
		elements := tx.Visitor().VisitNodes(n.ElementList())
		if node.Store().ListLen(elements) == 0 {
			return ast.Handle{}
		}
		return tx.Factory().UpdateNamedImports(n, elements)
	case ast.KindImportSpecifier:
		if !tx.shouldEmitAliasDeclaration(node) {
			return ast.Handle{}
		}
		return node
	case ast.KindExportAssignment:
		if !tx.compilerOptions.VerbatimModuleSyntax.IsTrue() && !tx.isValueAliasDeclaration(node) {
			return ast.Handle{}
		}
		return tx.Visitor().VisitEachChild(node)
	case ast.KindExportDeclaration:
		n := node
		var exportClause ast.Handle
		if !n.ExportClause().IsNil() {
			exportClause = tx.Visitor().VisitNode(n.ExportClause())
			if exportClause.IsNil() {
				return ast.Handle{}
			}
		}
		return tx.Factory().UpdateExportDeclaration(n, 0, false, exportClause, tx.Visitor().VisitNode(n.ModuleSpecifier()), tx.Visitor().VisitNode(n.Attributes()))
	case ast.KindNamedExports:
		n := node
		elements := tx.Visitor().VisitNodes(n.ElementList())
		if node.Store().ListLen(elements) == 0 {
			return ast.Handle{}
		}
		return tx.Factory().UpdateNamedExports(n, elements)
	case ast.KindExportSpecifier:
		if !tx.isValueAliasDeclaration(node) {
			return ast.Handle{}
		}
		return node
	case ast.KindSourceFile:
		savedCurrentSourceFile := tx.currentSourceFile
		tx.currentSourceFile = ast.GetSourceFileOfNode(node)
		node = tx.Visitor().VisitEachChild(node)
		tx.currentSourceFile = savedCurrentSourceFile
		return node
	case ast.KindModuleDeclaration, ast.KindModuleBlock:
		return tx.Visitor().VisitEachChild(node)
	default:
		return node
	}
}
func (tx *ImportElisionTransformer) shouldEmitAliasDeclaration(node ast.Handle) bool {
	return ast.IsInJSFile(node) || tx.isReferencedAliasDeclaration(node)
}
func (tx *ImportElisionTransformer) shouldEmitImportEqualsDeclaration(node ast.Handle) bool {
	return tx.shouldEmitAliasDeclaration(node) || (!ast.IsExternalModule(tx.currentSourceFile) && tx.isTopLevelValueImportEqualsWithEntityName(node))
}
func (tx *ImportElisionTransformer) isReferencedAliasDeclaration(node ast.Handle) bool {
	node = tx.EmitContext().ParseNode(node)
	return node.IsNil() || tx.emitResolver.IsReferencedAliasDeclaration(node)
}
func (tx *ImportElisionTransformer) isValueAliasDeclaration(node ast.Handle) bool {
	node = tx.EmitContext().ParseNode(node)
	return node.IsNil() || tx.emitResolver.IsValueAliasDeclaration(node)
}
func (tx *ImportElisionTransformer) isTopLevelValueImportEqualsWithEntityName(node ast.Handle) bool {
	node = tx.EmitContext().ParseNode(node)
	return !node.IsNil() && tx.emitResolver.IsTopLevelValueImportEqualsWithEntityName(node)
}
