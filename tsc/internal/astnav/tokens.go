package astnav

import (
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

func shouldRescanLessThanLessThanToken(s *scanner.Scanner, containingNode ast.Handle, token ast.Kind) bool {
	return token == ast.KindLessThanLessThanToken && ast.IsJsxChild(containingNode)
}
func scanNavigationToken(s *scanner.Scanner, containingNode ast.Handle) ast.Kind {
	token := s.Token()
	if shouldRescanLessThanLessThanToken(s, containingNode, token) {
		return s.ReScanJsxToken(true)
	}
	return token
}
func GetTouchingPropertyName(sourceFile *ast.SourceFile, position int) ast.Handle {
	return getTokenAtPosition(sourceFile, position, false, func(node ast.Handle) bool {
		return ast.IsPropertyNameLiteral(node) || ast.IsKeywordKind(node.Kind) || ast.IsPrivateIdentifier(node)
	})
}
func GetTouchingToken(sourceFile *ast.SourceFile, position int) ast.Handle {
	return getTokenAtPosition(sourceFile, position, false, nil)
}
func GetTokenAtPosition(sourceFile *ast.SourceFile, position int) ast.Handle {
	return getTokenAtPosition(sourceFile, position, true, nil)
}
func getTokenAtPosition(sourceFile *ast.SourceFile, position int, allowPositionInLeadingTrivia bool, includePrecedingTokenAtEndPosition func(node ast.Handle) bool) ast.Handle {
	var next, prevSubtree ast.Handle
	current := sourceFile.ParseRoot()
	left := 0
	var nodeAfterLeft ast.Handle
	getIncludedPrecedingToken := func(subtree ast.Handle) ast.Handle {
		child := FindPrecedingTokenEx(sourceFile, position, subtree, false)
		if !child.IsNil() && child.End() == position && includePrecedingTokenAtEndPosition(child) {
			return child
		}
		return ast.Handle{}
	}
	testNode := func(node ast.Handle) int {
		if node.Kind != ast.KindEndOfFile && node.End() == position && includePrecedingTokenAtEndPosition != nil && node.Flags()&ast.NodeFlagsReparsed == 0 {
			if !prevSubtree.IsNil() && !getIncludedPrecedingToken(prevSubtree).IsNil() {
				return 0
			}
			prevSubtree = node
		}
		if node.End() < position || node.End() == position && node.Kind != ast.KindEndOfFile && (!ast.IsJSDocKind(node.Kind) || node.End() != sourceFile.ParseRoot().End()) {
			return -1
		}
		nodePos := getPosition(node, sourceFile, allowPositionInLeadingTrivia)
		if nodePos > position {
			return 1
		}
		return 0
	}
	visitNode := func(node ast.Handle, _ *ast.HandleVisitor) ast.Handle {
		if node.IsNil() || node.Flags()&ast.NodeFlagsReparsed != 0 {
			return ast.Handle{}
		}
		if nodeAfterLeft.IsNil() {
			nodeAfterLeft = node
		}
		if next.IsNil() {
			result := testNode(node)
			switch result {
			case -1:
				if !ast.IsJSDocKind(node.Kind) {
					left = node.End()
				}
				nodeAfterLeft = ast.Handle{}
			case 0:
				next = node
			}
		}
		return node
	}
	visitNodeList := func(nodeList ast.ListRef, _ *ast.HandleVisitor) ast.ListRef {
		if nodeList == 0 || sourceFile.ParseStore().ListLen(nodeList) == 0 {
			return nodeList
		}
		if nodeAfterLeft.IsNil() {
			for _, node := range sourceFile.ParseStore().ListSlice(nodeList) {
				if node.Flags()&ast.NodeFlagsReparsed == 0 {
					nodeAfterLeft = node
					break
				}
			}
		}
		if next.IsNil() {
			if sourceFile.ParseStore().ListLoc(nodeList).End() == position && includePrecedingTokenAtEndPosition != nil {
				left = sourceFile.ParseStore().ListLoc(nodeList).End()
				nodeAfterLeft = ast.Handle{}
				for i := sourceFile.ParseStore().ListLen(nodeList) - 1; i >= 0; i-- {
					if sourceFile.ParseStore().ListAt(nodeList, i).Flags()&ast.NodeFlagsReparsed == 0 {
						prevSubtree = sourceFile.ParseStore().ListAt(nodeList, i)
						break
					}
				}
			} else if sourceFile.ParseStore().ListLoc(nodeList).End() <= position {
				left = sourceFile.ParseStore().ListLoc(nodeList).End()
				nodeAfterLeft = ast.Handle{}
			} else if sourceFile.ParseStore().ListLoc(nodeList).Pos() <= position {
				nodes := sourceFile.ParseStore().ListSlice(nodeList)
				index, match := core.BinarySearchUniqueFunc(nodes, func(middle int, node ast.Handle) int {
					if node.Flags()&ast.NodeFlagsReparsed != 0 {
						return 0
					}
					cmp := testNode(node)
					if cmp < 0 {
						left = node.End()
						nodeAfterLeft = ast.Handle{}
						for i := middle + 1; i < len(nodes); i++ {
							if nodes[i].Flags()&ast.NodeFlagsReparsed == 0 {
								nodeAfterLeft = nodes[i]
								break
							}
						}
					}
					return cmp
				})
				if match && nodes[index].Flags()&ast.NodeFlagsReparsed != 0 {
					nodes = core.Filter(nodes, func(node ast.Handle) bool {
						return node.Flags()&ast.NodeFlagsReparsed == 0
					})
					index, match = core.BinarySearchUniqueFunc(nodes, func(middle int, node ast.Handle) int {
						cmp := testNode(node)
						if cmp < 0 {
							left = node.End()
							if middle+1 < len(nodes) {
								nodeAfterLeft = nodes[middle+1]
							} else {
								nodeAfterLeft = ast.Handle{}
							}
						}
						return cmp
					})
				}
				if match {
					next = nodes[index]
				}
			}
		}
		return nodeList
	}
	for {
		VisitEachChildAndJSDoc(current, sourceFile, visitNode, visitNodeList)
		if !prevSubtree.IsNil() {
			if child := getIncludedPrecedingToken(prevSubtree); !child.IsNil() {
				return child
			}
			prevSubtree = ast.Handle{}
		}
		if next.IsNil() {
			if ast.IsTokenKind(current.Kind) || shouldSkipChild(current) {
				return current
			}
			scanner := scanner.GetScannerForSourceFile(sourceFile, left)
			end := current.End()
			if !nodeAfterLeft.IsNil() {
				end = nodeAfterLeft.Pos()
			}
			for left < end {
				token := scanNavigationToken(scanner, current)
				tokenFullStart := scanner.TokenFullStart()
				tokenStart := core.IfElse(allowPositionInLeadingTrivia, tokenFullStart, scanner.TokenStart())
				tokenEnd := scanner.TokenEnd()
				flags := scanner.TokenFlags()
				if tokenEnd > end {
					break
				}
				if tokenStart <= position && (position < tokenEnd) {
					if token == ast.KindIdentifier || !ast.IsTokenKind(token) {
						if ast.IsJSDocKind(current.Kind) {
							return current
						}
						panic(fmt.Sprintf("did not expect %s to have %s in its trivia", current.Kind.String(), token.String()))
					}
					return sourceFile.GetOrCreateToken(token, tokenFullStart, tokenEnd, current, flags)
				}
				if includePrecedingTokenAtEndPosition != nil && tokenEnd == position {
					prevToken := sourceFile.GetOrCreateToken(token, tokenFullStart, tokenEnd, current, flags)
					if includePrecedingTokenAtEndPosition(prevToken) {
						return prevToken
					}
				}
				left = tokenEnd
				scanner.Scan()
			}
			return current
		}
		current = next
		left = current.Pos()
		nodeAfterLeft = ast.Handle{}
		next = ast.Handle{}
	}
}
func getPosition(node ast.Handle, sourceFile *ast.SourceFile, allowPositionInLeadingTrivia bool) int {
	if allowPositionInLeadingTrivia {
		return node.Pos()
	}
	return scanner.GetTokenPosOfNode(node, sourceFile, true)
}
func findRightmostNode(node ast.Handle) ast.Handle {
	var next ast.Handle
	current := node
	visitNode := func(node ast.Handle, _ *ast.HandleVisitor) ast.Handle {
		if !node.IsNil() {
			next = node
		}
		return node
	}
	visitNodes := func(nodeList ast.ListRef, visitor *ast.HandleVisitor) ast.ListRef {
		if nodeList != 0 {
			if rightmost := ast.FindLastVisibleNode(node.Store().ListSlice(nodeList)); !rightmost.IsNil() {
				next = rightmost
			}
		}
		return nodeList
	}
	visitor := getNodeVisitor(visitNode, visitNodes)
	for {
		visitor.VisitEachChild(current)
		if next.IsNil() {
			return current
		}
		current = next
		next = ast.Handle{}
	}
}
func VisitEachChildAndJSDoc(node ast.Handle, sourceFile *ast.SourceFile, visitNode func(ast.Handle, *ast.HandleVisitor) ast.Handle, visitNodes func(ast.ListRef, *ast.HandleVisitor) ast.ListRef) {
	visitor := getNodeVisitor(visitNode, visitNodes)
	if visitor.Factory == nil && sourceFile != nil && sourceFile.ParseStore() != nil {
		visitor.Factory = ast.NewFactoryOn(sourceFile.ParseStore(), ast.FactoryHooks{})
	}
	for _, jsdoc := range node.JSDoc(sourceFile) {
		if visitor.Hooks.VisitNode != nil {
			visitor.Hooks.VisitNode(jsdoc, visitor)
		} else {
			visitor.VisitNode(jsdoc)
		}
	}
	visitor.VisitEachChild(node)
}

const (
	comparisonLessThan    = -1
	comparisonEqualTo     = 0
	comparisonGreaterThan = 1
)

func FindPrecedingToken(sourceFile *ast.SourceFile, position int) ast.Handle {
	return FindPrecedingTokenEx(sourceFile, position, ast.Handle{}, false)
}
func FindPrecedingTokenEx(sourceFile *ast.SourceFile, position int, startNode ast.Handle, excludeJSDoc bool) ast.Handle {
	var find func(node ast.Handle) ast.Handle
	find = func(n ast.Handle) ast.Handle {
		if ast.IsNonWhitespaceToken(n) && n.Kind != ast.KindEndOfFile {
			return n
		}
		var foundChild, prevChild ast.Handle
		visitNode := func(node ast.Handle, _ *ast.HandleVisitor) ast.Handle {
			if node.IsNil() || node.Flags()&ast.NodeFlagsReparsed != 0 {
				return node
			}
			if !foundChild.IsNil() {
				return node
			}
			if position < node.End() && (prevChild.IsNil() || prevChild.End() <= position) {
				foundChild = node
			} else {
				prevChild = node
			}
			return node
		}
		visitNodes := func(nodeList ast.ListRef, _ *ast.HandleVisitor) ast.ListRef {
			if !foundChild.IsNil() {
				return nodeList
			}
			if nodeList != 0 && sourceFile.ParseStore().ListLen(nodeList) > 0 {
				nodes := sourceFile.ParseStore().ListSlice(nodeList)
				index, match := core.BinarySearchUniqueFunc(nodes, func(middle int, _ ast.Handle) int {
					if nodes[middle].Flags()&ast.NodeFlagsReparsed != 0 {
						return comparisonLessThan
					}
					if position < nodes[middle].End() {
						if middle == 0 || position >= nodes[middle-1].End() {
							return comparisonEqualTo
						}
						return comparisonGreaterThan
					}
					return comparisonLessThan
				})
				if match {
					foundChild = nodes[index]
				}
				validLookupIndex := core.IfElse(match, index-1, len(nodes)-1)
				for i := validLookupIndex; i >= 0; i-- {
					if nodes[i].Flags()&ast.NodeFlagsReparsed != 0 {
						continue
					}
					if prevChild.IsNil() {
						prevChild = nodes[i]
					}
				}
			}
			return nodeList
		}
		VisitEachChildAndJSDoc(n, sourceFile, visitNode, visitNodes)
		if !foundChild.IsNil() {
			start := GetStartOfNode(foundChild, sourceFile, !excludeJSDoc)
			lookInPreviousChild := start >= position || !isValidPrecedingNode(foundChild, sourceFile)
			if lookInPreviousChild {
				if position >= foundChild.Pos() {
					var jsDoc ast.Handle
					nodeJSDoc := n.JSDoc(sourceFile)
					for i := len(nodeJSDoc) - 1; i >= 0; i-- {
						if nodeJSDoc[i].Pos() >= foundChild.Pos() {
							jsDoc = nodeJSDoc[i]
							break
						}
					}
					if !jsDoc.IsNil() {
						if !excludeJSDoc && position < jsDoc.End() {
							return find(jsDoc)
						} else {
							return findRightmostValidToken(jsDoc.End(), sourceFile, n, position, excludeJSDoc)
						}
					}
					return findRightmostValidToken(foundChild.Pos(), sourceFile, n, -1, excludeJSDoc)
				} else {
					return findRightmostValidToken(foundChild.Pos(), sourceFile, n, position, excludeJSDoc)
				}
			} else {
				return find(foundChild)
			}
		}
		if position >= n.End() {
			return findRightmostValidToken(n.End(), sourceFile, n, -1, excludeJSDoc)
		} else {
			return findRightmostValidToken(n.End(), sourceFile, n, position, excludeJSDoc)
		}
	}
	var node ast.Handle
	if !startNode.IsNil() {
		node = startNode
	} else {
		node = sourceFile.ParseRoot()
	}
	result := find(node)
	if !result.IsNil() && ast.IsWhitespaceOnlyJsxText(result) {
		panic("Expected result to be a non-whitespace token.")
	}
	return result
}
func isValidPrecedingNode(node ast.Handle, sourceFile *ast.SourceFile) bool {
	if node.Kind == ast.KindEndOfFile {
		return len(node.JSDoc(sourceFile)) > 0
	}
	start := GetStartOfNode(node, sourceFile, false)
	width := node.End() - start
	return !(ast.IsWhitespaceOnlyJsxText(node) || width == 0)
}
func GetStartOfNode(node ast.Handle, file *ast.SourceFile, includeJSDoc bool) int {
	return scanner.GetTokenPosOfNode(node, file, includeJSDoc)
}

func findRightmostValidToken(endPos int, sourceFile *ast.SourceFile, containingNode ast.Handle, position int, excludeJSDoc bool) ast.Handle {
	if position == -1 {
		position = containingNode.End()
	}
	var find func(n ast.Handle, endPos int) ast.Handle
	find = func(n ast.Handle, endPos int) ast.Handle {
		if n.IsNil() {
			return ast.Handle{}
		}
		if ast.IsNonWhitespaceToken(n) {
			return n
		}
		var rightmostValidNode ast.Handle
		rightmostVisitedNodes := make([]ast.Handle, 0, 1)
		hasChildren := false
		shouldVisitNode := func(node ast.Handle) bool {
			return !(node.Flags()&ast.NodeFlagsReparsed != 0 || node.End() > endPos || GetStartOfNode(node, sourceFile, !excludeJSDoc) >= position)
		}
		visitNode := func(node ast.Handle, _ *ast.HandleVisitor) ast.Handle {
			if node.IsNil() || node.Flags()&ast.NodeFlagsReparsed != 0 {
				return node
			}
			hasChildren = true
			if !shouldVisitNode(node) {
				return node
			}
			rightmostVisitedNodes = append(rightmostVisitedNodes, node)
			if isValidPrecedingNode(node, sourceFile) {
				rightmostValidNode = node
				rightmostVisitedNodes = rightmostVisitedNodes[:0]
			}
			return node
		}
		visitNodes := func(nodeList ast.ListRef, _ *ast.HandleVisitor) ast.ListRef {
			if nodeList != 0 && n.Store().ListLen(nodeList) > 0 {
				hasChildren = true
				index, _ := core.BinarySearchUniqueFunc(n.Store().ListSlice(nodeList), func(middle int, node ast.Handle) int {
					if node.End() > endPos {
						return comparisonGreaterThan
					}
					return comparisonLessThan
				})
				validIndex := -1
				for i := index - 1; i >= 0; i-- {
					if !shouldVisitNode(n.Store().ListAt(nodeList, i)) {
						continue
					}
					if isValidPrecedingNode(n.Store().ListAt(nodeList, i), sourceFile) {
						validIndex = i
						rightmostValidNode = n.Store().ListAt(nodeList, i)
						break
					}
				}
				for i := validIndex + 1; i < index; i++ {
					if !shouldVisitNode(n.Store().ListAt(nodeList, i)) {
						continue
					}
					rightmostVisitedNodes = append(rightmostVisitedNodes, n.Store().ListAt(nodeList, i))
				}
			}
			return nodeList
		}
		VisitEachChildAndJSDoc(n, sourceFile, visitNode, visitNodes)
		if !shouldSkipChild(n) {
			var startPos int
			if !rightmostValidNode.IsNil() {
				startPos = rightmostValidNode.End()
			} else {
				startPos = n.Pos()
			}
			scanner := scanner.GetScannerForSourceFile(sourceFile, startPos)
			var tokens []ast.Handle
			for _, visitedNode := range rightmostVisitedNodes {
				for startPos < min(visitedNode.Pos(), position) {
					token := scanNavigationToken(scanner, n)
					tokenStart := scanner.TokenStart()
					if tokenStart >= min(visitedNode.Pos(), position) {
						break
					}
					tokenFullStart := scanner.TokenFullStart()
					tokenEnd := scanner.TokenEnd()
					startPos = tokenEnd
					flags := scanner.TokenFlags()
					tokens = append(tokens, sourceFile.GetOrCreateToken(token, tokenFullStart, tokenEnd, n, flags))
					scanner.Scan()
				}
				startPos = visitedNode.End()
				scanner.ResetPos(startPos)
				scanner.Scan()
			}
			for startPos < min(endPos, position) {
				token := scanNavigationToken(scanner, n)
				tokenStart := scanner.TokenStart()
				if tokenStart >= min(endPos, position) {
					break
				}
				tokenFullStart := scanner.TokenFullStart()
				tokenEnd := scanner.TokenEnd()
				startPos = tokenEnd
				flags := scanner.TokenFlags()
				tokens = append(tokens, sourceFile.GetOrCreateToken(token, tokenFullStart, tokenEnd, n, flags))
				scanner.Scan()
			}
			lastToken := len(tokens) - 1
			for i := lastToken; i >= 0; i-- {
				if !ast.IsWhitespaceOnlyJsxText(tokens[i]) {
					return tokens[i]
				}
			}
		}
		if !hasChildren {
			if n != containingNode {
				return n
			}
			return ast.Handle{}
		}
		if !rightmostValidNode.IsNil() {
			endPos = rightmostValidNode.End()
		}
		return find(rightmostValidNode, endPos)
	}
	return find(containingNode, endPos)
}
func FindNextToken(previousToken ast.Handle, parent ast.Handle, file *ast.SourceFile) ast.Handle {
	var find func(n ast.Handle) ast.Handle
	find = func(n ast.Handle) ast.Handle {
		if ast.IsTokenKind(n.Kind) && n.Pos() == previousToken.End() {
			return n
		}
		var foundNode ast.Handle
		visitNode := func(node ast.Handle, _ *ast.HandleVisitor) ast.Handle {
			if !node.IsNil() && node.Flags()&ast.NodeFlagsReparsed == 0 && node.Pos() <= previousToken.End() && node.End() > previousToken.End() {
				foundNode = node
			}
			return node
		}
		visitNodes := func(nodeList ast.ListRef, _ *ast.HandleVisitor) ast.ListRef {
			if nodeList != 0 && parent.Store().ListLen(nodeList) > 0 && foundNode.IsNil() {
				nodes := parent.Store().ListSlice(nodeList)
				index, match := core.BinarySearchUniqueFunc(nodes, func(_ int, node ast.Handle) int {
					if node.Flags()&ast.NodeFlagsReparsed != 0 {
						return comparisonLessThan
					}
					if node.Pos() > previousToken.End() {
						return comparisonGreaterThan
					}
					if node.End() <= previousToken.Pos() {
						return comparisonLessThan
					}
					return comparisonEqualTo
				})
				if match {
					foundNode = nodes[index]
				}
			}
			return nodeList
		}
		VisitEachChildAndJSDoc(n, file, visitNode, visitNodes)
		if !foundNode.IsNil() {
			return find(foundNode)
		}
		startPos := previousToken.End()
		if startPos >= n.Pos() && startPos < n.End() {
			scanner := scanner.GetScannerForSourceFile(file, startPos)
			token := scanner.Token()
			tokenFullStart := scanner.TokenFullStart()
			tokenEnd := scanner.TokenEnd()
			flags := scanner.TokenFlags()
			if tokenFullStart == previousToken.End() {
				return file.GetOrCreateToken(token, tokenFullStart, tokenEnd, n, flags)
			}
			panic(fmt.Sprintf("Expected to find next token at %d, got token %s at %d", previousToken.End(), token, tokenFullStart))
		}
		return ast.Handle{}
	}
	return find(parent)
}
func getNodeVisitor(visitNode func(ast.Handle, *ast.HandleVisitor) ast.Handle, visitNodes func(ast.ListRef, *ast.HandleVisitor) ast.ListRef) *ast.HandleVisitor {
	var wrappedVisitNode func(ast.Handle, *ast.HandleVisitor) ast.Handle
	var wrappedVisitNodes func(ast.ListRef, *ast.HandleVisitor) ast.ListRef
	if visitNode != nil {
		wrappedVisitNode = func(n ast.Handle, v *ast.HandleVisitor) ast.Handle {
			if ast.IsJSDocSingleCommentNodeComment(n) {
				return n
			}
			return visitNode(n, v)
		}
	}
	if visitNodes != nil {
		wrappedVisitNodes = func(n ast.ListRef, v *ast.HandleVisitor) ast.ListRef {
			var store *ast.Store
			if v != nil && v.Factory != nil {
				store = v.Factory.Store()
			}
			if ast.IsJSDocSingleCommentNodeList(store, n) {
				return n
			}
			return visitNodes(n, v)
		}
	}
	return ast.NewHandleVisitor(core.Identity, nil, ast.HandleVisitorHooks{VisitNode: wrappedVisitNode, VisitToken: wrappedVisitNode, VisitNodes: wrappedVisitNodes, VisitModifiers: func(modifiers ast.ListRef, visitor *ast.HandleVisitor) ast.ListRef {
		if modifiers != 0 {
			return wrappedVisitNodes(modifiers, visitor)
		}
		return modifiers
	}})
}
func shouldSkipChild(node ast.Handle) bool {
	return node.Kind == ast.KindJSDoc || node.Kind == ast.KindJSDocText || node.Kind == ast.KindJSDocTypeLiteral || node.Kind == ast.KindJSDocSignature || ast.IsJSDocLinkLike(node) || ast.IsJSDocTag(node)
}

func FindChildOfKind(containingNode ast.Handle, kind ast.Kind, sourceFile *ast.SourceFile) ast.Handle {
	lastNodePos := containingNode.Pos()
	scan := scanner.GetScannerForSourceFile(sourceFile, lastNodePos)
	var foundChild ast.Handle
	visitNode := func(node ast.Handle) bool {
		if node.IsNil() || node.Flags()&ast.NodeFlagsReparsed != 0 {
			return false
		}
		startPos := lastNodePos
		for startPos < node.Pos() {
			tokenKind := scan.Token()
			tokenEnd := scan.TokenEnd()
			if tokenKind == kind {
				tokenFullStart := scan.TokenFullStart()
				flags := scan.TokenFlags()
				foundChild = sourceFile.GetOrCreateToken(tokenKind, tokenFullStart, tokenEnd, containingNode, flags)
				return true
			}
			startPos = tokenEnd
			scan.Scan()
		}
		if node.Kind == kind {
			foundChild = node
			return true
		}
		lastNodePos = node.End()
		scan.ResetPos(lastNodePos)
		return false
	}
	ast.ForEachChildAndJSDoc(containingNode, sourceFile, visitNode)
	if !foundChild.IsNil() {
		return foundChild
	}
	startPos := lastNodePos
	for startPos < containingNode.End() {
		tokenKind := scan.Token()
		tokenEnd := scan.TokenEnd()
		if tokenKind == kind {
			tokenFullStart := scan.TokenFullStart()
			flags := scan.TokenFlags()
			token := sourceFile.GetOrCreateToken(tokenKind, tokenFullStart, tokenEnd, containingNode, flags)
			return token
		}
		startPos = tokenEnd
		scan.Scan()
	}
	return ast.Handle{}
}
