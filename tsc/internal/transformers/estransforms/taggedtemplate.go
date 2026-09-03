package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
	"strings"
)

var newlineNormalizer = /*modifiers*/ strings.NewReplacer("\r\n", "\n", "\r", "\n")

type taggedTemplateTransformer struct {
	transformers.Transformer
	currentSourceFile                *ast.SourceFile
	taggedTemplateStringDeclarations []ast.Handle
}

func newTaggedTemplateLiftRestrictionTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &taggedTemplateTransformer{}
	return tx.NewTransformer(tx.visit, opts.Context)
}
func (tx *taggedTemplateTransformer) visit(node ast.Handle) ast.Handle {
	if node.SubtreeFacts()&ast.SubtreeContainsInvalidTemplateEscape == 0 {
		return node
	}
	switch node.Kind {
	case ast.KindSourceFile:
		return tx.visitSourceFile(node)
	case ast.KindTaggedTemplateExpression:
		return tx.visitTaggedTemplateExpression(node)
	default:
		return tx.Visitor().VisitEachChild(node)
	}
}
func (tx *taggedTemplateTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	tx.currentSourceFile = ast.GetSourceFileOfNode(node)
	tx.taggedTemplateStringDeclarations = nil
	visited := tx.Visitor().VisitEachChild(node)
	if len(tx.taggedTemplateStringDeclarations) > 0 {
		visitedSourceFile := visited
		statements := append(visitedSourceFile.Statements()[:len(visitedSourceFile.Statements()):len(visitedSourceFile.Statements())], tx.Factory().NewVariableStatement(0, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList(tx.taggedTemplateStringDeclarations), ast.NodeFlagsNone)))
		stmtList := tx.Factory().List(node.Store().ListLoc(node.StatementList()), statements...)
		visited = tx.Factory().UpdateSourceFile(visitedSourceFile, stmtList, visitedSourceFile.EndOfFileToken())
	}
	tx.EmitContext().AddEmitHelper(visited, tx.EmitContext().ReadEmitHelpers()...)
	return visited
}
func (tx *taggedTemplateTransformer) visitTaggedTemplateExpression(node ast.Handle) ast.Handle {
	return tx.processTaggedTemplateExpression(node)
}
func (tx *taggedTemplateTransformer) processTaggedTemplateExpression(node ast.Handle) ast.Handle {
	tag := tx.Visitor().VisitNode(node.Tag())
	template := node.Template()
	if !hasInvalidEscape(template) {
		return tx.Visitor().VisitEachChild(node)
	}
	f := tx.Factory()
	templateArguments := []ast.Handle{ast.Handle{}}
	var cookedStrings []ast.Handle
	var rawStrings []ast.Handle
	if ast.IsNoSubstitutionTemplateLiteral(template) {
		cookedStrings = append(cookedStrings, createTemplateCooked(f, template))
		rawStrings = append(rawStrings, getRawLiteral(f, template))
	} else {
		te := template
		cookedStrings = append(cookedStrings, createTemplateCooked(f, te.Head()))
		rawStrings = append(rawStrings, getRawLiteral(f, te.Head()))
		for _, span := range te.TemplateSpans() {
			ts := span
			cookedStrings = append(cookedStrings, createTemplateCooked(f, ts.Literal()))
			rawStrings = append(rawStrings, getRawLiteral(f, ts.Literal()))
			templateArguments = append(templateArguments, tx.Visitor().VisitNode(ts.Expression()))
		}
	}
	helperCall := f.NewTemplateObjectHelper(f.NewArrayLiteralExpression(f.NewList(cookedStrings), false), f.NewArrayLiteralExpression(f.NewList(rawStrings), false))
	if ast.IsExternalModule(tx.currentSourceFile) {
		tempVar := f.NewUniqueName("templateObject")
		tx.taggedTemplateStringDeclarations = append(tx.taggedTemplateStringDeclarations, f.NewVariableDeclaration(tempVar, ast.Handle{}, ast.Handle{}, ast.Handle{}))
		templateArguments[0] = f.NewLogicalORExpression(tempVar, f.NewAssignmentExpression(tempVar, helperCall))
	} else {
		templateArguments[0] = helperCall
	}
	call := f.NewCallExpression(tag, ast.Handle{}, 0, f.NewList(templateArguments), ast.NodeFlagsNone)
	call.SetLoc(node.Loc())
	return call
}
func createTemplateCooked(f *printer.NodeFactory, template ast.Handle) ast.Handle {
	if template.TokenFlags()&ast.TokenFlagsIsInvalid != 0 {
		return f.NewVoidZeroExpression()
	}
	return f.NewStringLiteral(template.Text(), ast.TokenFlagsNone)
}
func getRawLiteral(f *printer.NodeFactory, node ast.Handle) ast.Handle {
	text := node.RawText()
	if text == "" {
		text = scanner.GetSourceTextOfNodeFromSourceFile(ast.GetSourceFileOfNode(node), node, false)
		isLast := node.Kind == ast.KindNoSubstitutionTemplateLiteral || node.Kind == ast.KindTemplateTail
		endLen := 2
		if isLast {
			endLen = 1
		}
		text = text[1 : len(text)-endLen]
	}
	text = newlineNormalizer.Replace(text)
	result := f.NewStringLiteral(text, ast.TokenFlagsNone)
	result.SetLoc(node.Loc())
	return result
}
func hasInvalidEscape(template ast.Handle) bool {
	if ast.IsNoSubstitutionTemplateLiteral(template) {
		return template.TokenFlags()&ast.TokenFlagsContainsInvalidEscape != 0
	}
	te := template
	if te.Head().TokenFlags()&ast.TokenFlagsContainsInvalidEscape != 0 {
		return true
	}
	for _, span := range te.TemplateSpans() {
		if span.TemplateSpanLiteral().TokenFlags()&ast.TokenFlagsContainsInvalidEscape != 0 {
			return true
		}
	}
	return false
}
