package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
)

var jsxTagWordPattern = new("[a-zA-Z0-9:\\-\\._$]*")

func (l *LanguageService) ProvideLinkedEditingRange(ctx context.Context, params *lsproto.LinkedEditingRangeParams) (lsproto.LinkedEditingRangeResponse, error) {
	_, sourceFile := l.getProgramAndFile(params.TextDocument.Uri)
	positions := lsconv.FromLSPPositionForSourceFile(l.converters, sourceFile, params.Position, spanmap.FeatureLinkedEditing)
	if len(positions) != 1 || !positions[0].Fidelity.IsExact() {
		return lsproto.LinkedEditingRangeResponse{}, nil
	}
	sourceFile = positions[0].Script
	position := positions[0].Position
	token := astnav.FindPrecedingToken(sourceFile, int(position))
	if token.IsNil() || token.Parent().Kind() == ast.KindSourceFile {
		return lsproto.LinkedEditingRangeResponse{}, nil
	}
	if ast.IsJsxFragment(token.Parent().Parent()) {
		fragment := token.Parent().Parent()
		openFragment := fragment.OpeningFragment()
		closeFragment := fragment.JsxFragmentClosingFragment()
		if openFragment.Flags()&ast.NodeFlagsThisNodeOrAnySubNodesHasError != 0 || closeFragment.Flags()&ast.NodeFlagsThisNodeOrAnySubNodesHasError != 0 {
			return lsproto.LinkedEditingRangeResponse{}, nil
		}
		openPos := core.TextPos(astnav.GetStartOfNode(openFragment, sourceFile, false) + len("<"))
		closePos := core.TextPos(astnav.GetStartOfNode(closeFragment, sourceFile, false) + len("</"))
		if (position != openPos) && (position != closePos) {
			return lsproto.LinkedEditingRangeResponse{}, nil
		}
		openLineChar, openFidelity := l.converters.ToLSPPositionForFeature(sourceFile, openPos, spanmap.FeatureLinkedEditing)
		closeLineChar, closeFidelity := l.converters.ToLSPPositionForFeature(sourceFile, closePos, spanmap.FeatureLinkedEditing)
		if !openFidelity.IsExact() || !closeFidelity.IsExact() {
			return lsproto.LinkedEditingRangeResponse{}, nil
		}
		return lsproto.LinkedEditingRangeResponse{LinkedEditingRanges: &lsproto.LinkedEditingRanges{Ranges: []lsproto.Range{{Start: openLineChar, End: openLineChar}, {Start: closeLineChar, End: closeLineChar}}, WordPattern: jsxTagWordPattern}}, nil
	} else {
		tag := ast.FindAncestor(token.Parent(), func(n ast.Handle) bool {
			if ast.IsJsxOpeningElement(n) || ast.IsJsxClosingElement(n) {
				return true
			}
			return false
		})
		if tag.IsNil() {
			return lsproto.LinkedEditingRangeResponse{}, nil
		}
		debug.Assert(ast.IsJsxOpeningElement(tag) || ast.IsJsxClosingElement(tag), "tag should be opening or closing element")
		jsxElement := tag.Parent()
		openTag := jsxElement.OpeningElement()
		closeTag := jsxElement.ClosingElement()
		openTagNameStart := astnav.GetStartOfNode(openTag.TagName(), sourceFile, false)
		openTagNameEnd := openTag.TagName().End()
		closeTagNameStart := astnav.GetStartOfNode(closeTag.TagName(), sourceFile, false)
		closeTagNameEnd := closeTag.TagName().End()
		if openTagNameStart == astnav.GetStartOfNode(openTag, sourceFile, false) || closeTagNameStart == astnav.GetStartOfNode(closeTag, sourceFile, false) || openTagNameEnd == openTag.End() || closeTagNameEnd == closeTag.End() {
			return lsproto.LinkedEditingRangeResponse{}, nil
		}
		positionInt := int(position)
		if !(openTagNameStart <= positionInt && positionInt <= openTagNameEnd || closeTagNameStart <= positionInt && positionInt <= closeTagNameEnd) {
			return lsproto.LinkedEditingRangeResponse{}, nil
		}
		openingTagText := scanner.GetTextOfNode(openTag.TagName())
		if openingTagText != scanner.GetTextOfNode(closeTag.TagName()) {
			return lsproto.LinkedEditingRangeResponse{}, nil
		}
		openRange, openFidelity := l.converters.ToLSPRangeForFeature(sourceFile, core.NewTextRange(openTagNameStart, openTagNameEnd), spanmap.FeatureLinkedEditing)
		closeRange, closeFidelity := l.converters.ToLSPRangeForFeature(sourceFile, core.NewTextRange(closeTagNameStart, closeTagNameEnd), spanmap.FeatureLinkedEditing)
		if !openFidelity.IsExact() || !closeFidelity.IsExact() {
			return lsproto.LinkedEditingRangeResponse{}, nil
		}
		return lsproto.LinkedEditingRangeResponse{LinkedEditingRanges: &lsproto.LinkedEditingRanges{Ranges: []lsproto.Range{openRange, closeRange}, WordPattern: jsxTagWordPattern}}, nil
	}
}
