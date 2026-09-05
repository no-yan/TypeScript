package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
)

const maxSelectionRangeDepth = 1000

type selectionRangeBuilder struct {
	ranges []lsproto.Range
	oldestIndex int
}

func newSelectionRangeBuilder(capacity int) *selectionRangeBuilder {
	return &selectionRangeBuilder{ranges: make([]lsproto.Range, 0, capacity)}
}
func (b *selectionRangeBuilder) push(selectionRange lsproto.Range) {
	if len(b.ranges) < cap(b.ranges) {
		b.ranges = append(b.ranges, selectionRange)
		return
	}
	b.ranges[b.oldestIndex] = selectionRange
	b.oldestIndex = (b.oldestIndex + 1) % len(b.ranges)
}
func (b *selectionRangeBuilder) build(result *lsproto.SelectionRange) *lsproto.SelectionRange {
	for i := range b.ranges {
		index := (b.oldestIndex + i) % len(b.ranges)
		result = &lsproto.SelectionRange{Range: b.ranges[index], Parent: result}
	}
	return result
}
func (l *LanguageService) ProvideSelectionRanges(ctx context.Context, params *lsproto.SelectionRangeParams) (lsproto.SelectionRangeResponse, error) {
	_, sourceFile := l.getProgramAndFile(params.TextDocument.Uri)
	if sourceFile == nil {
		return lsproto.SelectionRangesOrNull{}, nil
	}
	results := make([]*lsproto.SelectionRange, 0, len(params.Positions))
	for _, position := range params.Positions {
		positions := lsconv.FromLSPPositionForSourceFile(l.converters, sourceFile, position, spanmap.FeatureSelectionRanges)
		if len(positions) != 1 || !positions[0].Fidelity.IsSingleSegment() {
			return lsproto.SelectionRangesOrNull{}, nil
		}
		selectionRange := getSmartSelectionRange(l, positions[0].Script, int(positions[0].Position))
		if selectionRange != nil {
			results = append(results, selectionRange)
		}
	}
	return lsproto.SelectionRangesOrNull{SelectionRanges: &results}, nil
}
func getSelectionChildren(factory ast.HandleFactory, node ast.Handle, sourceFile *ast.SourceFile) []ast.Handle {
	if !ast.IsMappedTypeNode(node) {
		return getChildrenFromNonJSDocNode(node, sourceFile)
	}
	children := getChildrenFromNonJSDocNode(node, sourceFile)
	if len(children) < 2 {
		return children
	}
	openBraceToken := children[0]
	closeBraceToken := children[len(children)-1]
	if openBraceToken.Kind() != ast.KindOpenBraceToken || closeBraceToken.Kind() != ast.KindCloseBraceToken {
		return children
	}
	mappedType := node
	children = children[1 : len(children)-1]
	groupedWithPlusMinusTokens := groupChildren(factory, children, func(child ast.Handle) bool {
		return child == mappedType.MappedTypeNodeReadonlyToken() || child.Kind() == ast.KindReadonlyKeyword || child == mappedType.QuestionToken() || child.Kind() == ast.KindQuestionToken
	})
	groupedWithBrackets := groupChildren(factory, groupedWithPlusMinusTokens, func(child ast.Handle) bool {
		return child.Kind() == ast.KindOpenBracketToken || child.Kind() == ast.KindTypeParameter || child.Kind() == ast.KindCloseBracketToken
	})
	return []ast.Handle{openBraceToken, createSyntaxList(factory, splitChildren(factory, groupedWithBrackets, func(child ast.Handle) bool {
		return child.Kind() == ast.KindColonToken
	}, false)), closeBraceToken}
}
func groupChildren(factory ast.HandleFactory, children []ast.Handle, groupOn func(ast.Handle) bool) []ast.Handle {
	var result []ast.Handle
	var group []ast.Handle
	for _, child := range children {
		if groupOn(child) {
			group = append(group, child)
		} else {
			if len(group) > 0 {
				result = append(result, createSyntaxList(factory, group))
				group = nil
			}
			result = append(result, child)
		}
	}
	if len(group) > 0 {
		result = append(result, createSyntaxList(factory, group))
	}
	return result
}
func splitChildren(factory ast.HandleFactory, children []ast.Handle, pivotOn func(ast.Handle) bool, separateTrailingSemicolon bool) []ast.Handle {
	if len(children) < 2 {
		return children
	}
	splitTokenIndex := -1
	for i, child := range children {
		if pivotOn(child) {
			splitTokenIndex = i
			break
		}
	}
	if splitTokenIndex == -1 {
		return children
	}
	leftChildren := children[:splitTokenIndex]
	splitToken := children[splitTokenIndex]
	lastToken := children[len(children)-1]
	separateLastToken := separateTrailingSemicolon && lastToken.Kind() == ast.KindSemicolonToken
	rightEnd := len(children)
	if separateLastToken {
		rightEnd--
	}
	rightChildren := children[splitTokenIndex+1 : rightEnd]
	result := make([]ast.Handle, 0, 4)
	if len(leftChildren) > 0 {
		result = append(result, createSyntaxList(factory, leftChildren))
	}
	result = append(result, splitToken)
	if len(rightChildren) > 0 {
		result = append(result, createSyntaxList(factory, rightChildren))
	}
	if separateLastToken {
		result = append(result, lastToken)
	}
	return result
}
func createSyntaxList(factory ast.HandleFactory, children []ast.Handle) ast.Handle {
	list := factory.NewSyntaxList(factory.NewList(children))
	list.SetLoc(core.NewTextRange(children[0].Pos(), children[len(children)-1].End()))
	return list
}
func getSmartSelectionRange(l *LanguageService, sourceFile *ast.SourceFile, pos int) *lsproto.SelectionRange {
	factory := ast.NewFactory(ast.FactoryHooks{})
	ranges := newSelectionRangeBuilder(maxSelectionRangeDepth - 1)
	var root *lsproto.SelectionRange
	var lastRange lsproto.Range
	if sourceFile.ContentMapper() == "" {
		fullRange, _ := l.converters.ToLSPRange(sourceFile, core.NewTextRange(sourceFile.Pos(), sourceFile.End()))
		root = &lsproto.SelectionRange{Range: fullRange}
		lastRange = fullRange
	}
	nodeContainsPosition := func(node ast.Handle) bool {
		if node.IsNil() {
			return false
		}
		start := scanner.GetTokenPosOfNode(node, sourceFile, true)
		end := node.End()
		return start <= pos && pos < end
	}
	positionShouldSnapToNode := func(node ast.Handle) bool {
		if pos < node.End() {
			return true
		}
		if node.End() == pos {
			touchingPropertyName := astnav.GetTouchingPropertyName(sourceFile, pos)
			return !touchingPropertyName.IsNil() && touchingPropertyName.Pos() < node.End()
		}
		return false
	}
	pushSelectionRange := func(start, end int) {
		if start == end {
			return
		}
		if !(start <= pos && pos <= end) {
			return
		}
		lspRange, fidelity := l.converters.ToLSPRangeForFeature(sourceFile, core.NewTextRange(start, end), spanmap.FeatureSelectionRanges)
		if fidelity.IsNone() {
			return
		}
		if lastRange == lspRange {
			return
		}
		lastRange = lspRange
		ranges.push(lspRange)
	}
	pushSelectionCommentRange := func(start, end int) {
		pushSelectionRange(start, end)
		commentPos := start
		text := sourceFile.Text()
		for commentPos < end && commentPos < len(text) && text[commentPos] == '/' {
			commentPos++
		}
		pushSelectionRange(commentPos, end)
	}
	positionsAreOnSameLine := func(pos1, pos2 int) bool {
		if pos1 == pos2 {
			return true
		}
		lineStarts := sourceFile.ECMALineMap()
		return scanner.ComputeLineOfPosition(lineStarts, pos1) == scanner.ComputeLineOfPosition(lineStarts, pos2)
	}
	shouldSkipNode := func(node ast.Handle, parent ast.Handle) bool {
		if ast.IsBlock(node) {
			return true
		}
		if ast.IsTemplateSpan(node) || ast.IsTemplateHead(node) || ast.IsTemplateTail(node) {
			return true
		}
		if !parent.IsNil() && ast.IsVariableDeclarationList(node) && ast.IsVariableStatement(parent) {
			return true
		}
		if !parent.IsNil() && ast.IsVariableDeclaration(node) && ast.IsVariableDeclarationList(parent) {
			decl := parent
			if !decl.IsNil() && len(decl.Declarations()) == 1 {
				return true
			}
		}
		if ast.IsJSDocTypeExpression(node) || ast.IsJSDocSignature(node) || ast.IsJSDocTypeLiteral(node) {
			return true
		}
		return false
	}
	var current ast.Handle
	for current = sourceFile.ParseRoot(); !current.IsNil(); {
		var next ast.Handle
		parent := current
		visit := func(node ast.Handle) ast.Handle {
			if !node.IsNil() && next.IsNil() {
				var foundComment *ast.CommentRange
				for comment := range scanner.GetTrailingCommentRanges(sourceFile.Text(), node.End()) {
					foundComment = &comment
					break
				}
				if foundComment != nil && foundComment.Kind == ast.KindSingleLineCommentTrivia {
					pushSelectionCommentRange(foundComment.Pos(), foundComment.End())
				}
				if nodeContainsPosition(node) {
					if ast.IsBlock(node) && ast.IsFunctionLikeDeclaration(parent) {
						if !positionsAreOnSameLine(astnav.GetStartOfNode(node, sourceFile, false), node.End()) {
							start := astnav.GetStartOfNode(node, sourceFile, false)
							end := node.End()
							pushSelectionRange(start, end)
						}
					}
					if ast.IsTemplateSpan(parent) {
						templateSpan := parent
						if !templateSpan.Literal().IsNil() {
							spanStart := node.Pos() - 2
							spanEnd := astnav.GetStartOfNode(templateSpan.Literal(), sourceFile, false) + 1
							text := sourceFile.Text()
							if spanStart >= 0 && spanEnd <= len(text) && spanStart < spanEnd {
								pushSelectionRange(spanStart, spanEnd)
							}
						}
					}
					if !shouldSkipNode(node, parent) {
						start := astnav.GetStartOfNode(node, sourceFile, false)
						end := node.End()
						pushSelectionRange(start, end)
						if ast.IsMappedTypeNode(node) {
							for selectionParent := node; ; {
								var selectionChild ast.Handle
								for _, child := range getSelectionChildren(factory, selectionParent, sourceFile) {
									childStart := scanner.GetTokenPosOfNode(child, sourceFile, true)
									if childStart > pos {
										break
									}
									if positionShouldSnapToNode(child) {
										pushSelectionRange(childStart, child.End())
										selectionChild = child
										break
									}
								}
								if selectionChild.IsNil() || !ast.IsSyntaxList(selectionChild) {
									break
								}
								selectionParent = selectionChild
							}
						}
						if ast.IsStringLiteral(node) || node.Kind() == ast.KindTemplateExpression || node.Kind() == ast.KindNoSubstitutionTemplateLiteral {
							if start+1 < end-1 {
								pushSelectionRange(start+1, end-1)
							}
						}
					}
					next = node
				}
			}
			return node
		}
		visitNodes := func(nodes ast.ListRef, v *ast.HandleVisitor) ast.ListRef {
			if nodes != 0 && parent.Store().ListLen(nodes) > 0 {
				shouldSkipList := !parent.IsNil() && (ast.IsVariableDeclarationList(parent) || ast.IsTemplateExpression(parent))
				if !shouldSkipList {
					start := astnav.GetStartOfNode(parent.Store().ListAt(nodes, 0), sourceFile, false)
					end := parent.Store().ListAt(nodes, parent.Store().ListLen(nodes)-1).End()
					if start <= pos && pos < end {
						pushSelectionRange(start, end)
					}
				}
			}
			return v.DefaultVisitNodes(nodes)
		}
		for _, jsdoc := range current.JSDoc(sourceFile) {
			visit(jsdoc)
		}
		tempVisitor := ast.NewHandleVisitor(visit, ast.NewFactoryOn(sourceFile.ParseStore(), ast.FactoryHooks{}), ast.HandleVisitorHooks{VisitNodes: visitNodes})
		tempVisitor.VisitEachChild(current)
		current = next
	}
	return ranges.build(root)
}
