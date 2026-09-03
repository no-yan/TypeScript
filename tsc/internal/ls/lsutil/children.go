package lsutil

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

func GetLastChild(node ast.Handle, sourceFile *ast.SourceFile) ast.Handle {
	lastChildNode := GetLastVisitedChild(node, sourceFile)
	if ast.IsJSDocSingleCommentNode(node) && lastChildNode.IsNil() {
		return ast.Handle{}
	}
	var tokenStartPos int
	if !lastChildNode.IsNil() {
		tokenStartPos = lastChildNode.End()
	} else {
		tokenStartPos = node.Pos()
	}
	var lastToken ast.Handle
	scanner := scanner.GetScannerForSourceFile(sourceFile, tokenStartPos)
	for startPos := tokenStartPos; startPos < node.End(); {
		tokenKind := scanner.Token()
		tokenFullStart := scanner.TokenFullStart()
		tokenEnd := scanner.TokenEnd()
		lastToken = sourceFile.GetOrCreateToken(tokenKind, tokenFullStart, tokenEnd, node, scanner.TokenFlags())
		startPos = tokenEnd
		scanner.Scan()
	}
	return core.IfElse(!lastToken.IsNil(), lastToken, lastChildNode)
}
func GetLastToken(node ast.Handle, sourceFile *ast.SourceFile) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	if ast.IsTokenKind(node.Kind) || ast.IsIdentifier(node) {
		return ast.Handle{}
	}
	AssertHasRealPosition(node)
	lastChild := GetLastChild(node, sourceFile)
	if lastChild.IsNil() {
		return ast.Handle{}
	}
	if lastChild.Kind < ast.KindFirstNode {
		return lastChild
	} else {
		return GetLastToken(lastChild, sourceFile)
	}
}

func GetLastVisitedChild(node ast.Handle, sourceFile *ast.SourceFile) ast.Handle {
	var lastChild ast.Handle
	visitNode := func(n ast.Handle, _ *ast.HandleVisitor) ast.Handle {
		if !n.IsNil() && n.Flags()&ast.NodeFlagsReparsed == 0 {
			lastChild = n
		}
		return n
	}
	visitNodeList := func(nodeList ast.ListRef, _ *ast.HandleVisitor) ast.ListRef {
		if nodeList != 0 && node.Store().ListLen(nodeList) > 0 {
			for i := node.Store().ListLen(nodeList) - 1; i >= 0; i-- {
				if node.Store().ListAt(nodeList, i).Flags()&ast.NodeFlagsReparsed == 0 {
					lastChild = node.Store().ListAt(nodeList, i)
					break
				}
			}
		}
		return nodeList
	}
	astnav.VisitEachChildAndJSDoc(node, sourceFile, visitNode, visitNodeList)
	return lastChild
}
func GetFirstToken(node ast.Handle, sourceFile *ast.SourceFile) ast.Handle {
	if ast.IsIdentifier(node) || ast.IsTokenKind(node.Kind) {
		return ast.Handle{}
	}
	AssertHasRealPosition(node)
	var firstChild ast.Handle
	node.ForEachChild(func(n ast.Handle) bool {
		if n.IsNil() || node.Flags()&ast.NodeFlagsReparsed != 0 {
			return false
		}
		firstChild = n
		return true
	})
	var tokenEndPosition int
	if !firstChild.IsNil() {
		tokenEndPosition = firstChild.Pos()
	} else {
		tokenEndPosition = node.End()
	}
	scanner := scanner.GetScannerForSourceFile(sourceFile, node.Pos())
	var firstToken ast.Handle
	if node.Pos() < tokenEndPosition {
		tokenKind := scanner.Token()
		tokenFullStart := scanner.TokenFullStart()
		tokenEnd := scanner.TokenEnd()
		firstToken = sourceFile.GetOrCreateToken(tokenKind, tokenFullStart, tokenEnd, node, scanner.TokenFlags())
	}
	if !firstToken.IsNil() {
		return firstToken
	}
	if firstChild.IsNil() {
		return ast.Handle{}
	}
	if firstChild.Kind < ast.KindFirstNode {
		return firstChild
	}
	return GetFirstToken(firstChild, sourceFile)
}
func AssertHasRealPosition(node ast.Handle) {
	if ast.PositionIsSynthesized(node.Pos()) || ast.PositionIsSynthesized(node.End()) {
		panic("Node must have a real position for this operation.")
	}
}
