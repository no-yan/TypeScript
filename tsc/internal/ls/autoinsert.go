package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
)

func (l *LanguageService) ProvideOnAutoInsert(ctx context.Context, params *lsproto.VSOnAutoInsertParams) (lsproto.VSOnAutoInsertResponse, error) {
	if l.UserPreferences().EnableAutoClosingTags.IsFalse() {
		return lsproto.VSOnAutoInsertResponse{}, nil
	}
	if params.VSCh != ">" {
		return lsproto.VSOnAutoInsertResponse{}, nil
	}
	_, sourceFile := l.getProgramAndFile(params.VSTextDocument.Uri)
	positions := lsconv.FromLSPPositionForSourceFile(l.converters, sourceFile, params.VSPosition, spanmap.FeatureAutoInsert)
	if len(positions) != 1 || !positions[0].Fidelity.IsExact() {
		return lsproto.VSOnAutoInsertResponse{}, nil
	}
	sourceFile = positions[0].Script
	position := positions[0].Position
	token := astnav.FindPrecedingToken(sourceFile, int(position))
	if token.IsNil() {
		return lsproto.VSOnAutoInsertResponse{}, nil
	}
	var closingText string
	var element ast.Handle
	if token.Kind == ast.KindGreaterThanToken && ast.IsJsxOpeningElement(token.Parent()) {
		element = token.Parent().Parent()
	} else if ast.IsJsxText(token) && ast.IsJsxElement(token.Parent()) {
		element = token.Parent()
	}
	if !element.IsNil() && isUnclosedTag(element) {
		tagNameNode := element.JsxElementOpeningElement().TagName()
		closingText = "</" + ast.EntityNameToString(tagNameNode, scanner.GetTextOfNode) + ">"
	} else {
		var fragment ast.Handle
		if token.Kind == ast.KindGreaterThanToken && ast.IsJsxOpeningFragment(token.Parent()) {
			fragment = token.Parent().Parent()
		} else if ast.IsJsxText(token) && ast.IsJsxFragment(token.Parent()) {
			fragment = token.Parent()
		}
		if !fragment.IsNil() && isUnclosedFragment(fragment) {
			closingText = "</>"
		}
	}
	if closingText == "" {
		return lsproto.VSOnAutoInsertResponse{}, nil
	}
	return lsproto.VSOnAutoInsertResponse{VSOnAutoInsertResponseItem: &lsproto.VSOnAutoInsertResponseItem{VSTextEditFormat: lsproto.InsertTextFormatSnippet, VSTextEdit: &lsproto.TextEdit{Range: lsproto.Range{Start: params.VSPosition, End: params.VSPosition}, NewText: "$0" + escapeSnippetText(closingText)}}}, nil
}
func isUnclosedTag(node ast.Handle) bool {
	openingElement := node.JsxElementOpeningElement()
	closingElement := node.JsxElementClosingElement()
	if !ast.TagNamesAreEquivalent(openingElement.TagName(), closingElement.TagName()) {
		return true
	}
	parent := node.Parent()
	if ast.IsJsxElement(parent) {
		return ast.TagNamesAreEquivalent(openingElement.TagName(), parent.JsxElementOpeningElement().TagName()) && isUnclosedTag(parent)
	}
	return false
}
func isUnclosedFragment(node ast.Handle) bool {
	closingFragment := node.JsxFragmentClosingFragment()
	if closingFragment.Flags()&ast.NodeFlagsThisNodeHasError != 0 {
		return true
	}
	parent := node.Parent()
	if ast.IsJsxFragment(parent) && isUnclosedFragment(parent) {
		return true
	}
	return false
}
