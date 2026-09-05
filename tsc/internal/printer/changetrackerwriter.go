package printer

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"unicode/utf8"
)

type ChangeTrackerWriter struct {
	textWriter
	lastNonTriviaPosition int
	pos                   map[any]int
	end                   map[any]int
}

func NewChangeTrackerWriter(newline string, indentSize int) *ChangeTrackerWriter {
	if indentSize < 0 {
		indentSize = defaultIndentSize
	}
	ctw := &ChangeTrackerWriter{textWriter: textWriter{newLine: newline, indentSize: indentSize}, lastNonTriviaPosition: 0, pos: map[any]int{}, end: map[any]int{}}
	ctw.textWriter.Clear()
	return ctw
}
func (ct *ChangeTrackerWriter) GetPrintHandlers() PrintHandlers {
	return PrintHandlers{OnBeforeEmitNode: func(nodeOpt ast.Handle) {
		if !nodeOpt.IsNil() {
			ct.setPos(nodeOpt)
		}
	}, OnAfterEmitNode: func(nodeOpt ast.Handle) {
		if !nodeOpt.IsNil() {
			ct.setEnd(nodeOpt)
		}
	}, OnBeforeEmitNodeList: func(nodesOpt ast.ListRef) {
		if nodesOpt != 0 {
			ct.setPos(nodesOpt)
		}
	}, OnAfterEmitNodeList: func(nodesOpt ast.ListRef) {
		if nodesOpt != 0 {
			ct.setEnd(nodesOpt)
		}
	}, OnBeforeEmitToken: func(nodeOpt ast.Handle) {
		if !nodeOpt.IsNil() {
			ct.setPos(nodeOpt)
		}
	}, OnAfterEmitToken: func(nodeOpt ast.Handle) {
		if !nodeOpt.IsNil() {
			ct.setEnd(nodeOpt)
		}
	}}
}
func (ct *ChangeTrackerWriter) setPos(node any) {
	ct.pos[node] = ct.lastNonTriviaPosition
}
func (ct *ChangeTrackerWriter) setEnd(node any) {
	ct.end[node] = ct.lastNonTriviaPosition
}
func (ct *ChangeTrackerWriter) getPos(node any) int {
	return ct.pos[node]
}
func (ct *ChangeTrackerWriter) getEnd(node any) int {
	return ct.end[node]
}
func (ct *ChangeTrackerWriter) setLastNonTriviaPosition(s string, force bool) {
	if force || scanner.SkipTrivia(s, 0) != len(s) {
		ct.lastNonTriviaPosition = ct.textWriter.GetTextPos()
		pos := len(s)
		for pos > 0 {
			r, size := utf8.DecodeLastRuneInString(s[:pos])
			if stringutil.IsWhiteSpaceLike(r) {
				pos -= size
			} else {
				break
			}
		}
		ct.lastNonTriviaPosition -= len(s) - pos
	}
}
func (ct *ChangeTrackerWriter) AssignPositionsToNode(node ast.Handle, factory ast.HandleFactory) ast.Handle {
	var visitor *ast.HandleVisitor
	visitor = ast.NewHandleVisitor(func(n ast.Handle) ast.Handle {
		return ct.assignPositionsToNodeWorker(n, visitor)
	}, factory, ast.HandleVisitorHooks{VisitNode: ct.assignPositionsToNodeWorker, VisitNodes: ct.assignPositionsToNodeArray, VisitToken: ct.assignPositionsToNodeWorker, VisitModifiers: func(modifiers ast.ListRef, v *ast.HandleVisitor) ast.ListRef {
		if modifiers != 0 {
			return ct.assignPositionsToNodeArray(modifiers, v)
		}
		return modifiers
	}})
	return ct.assignPositionsToNodeWorker(node, visitor)
}
func (ct *ChangeTrackerWriter) assignPositionsToNodeWorker(node ast.Handle, v *ast.HandleVisitor) ast.Handle {
	if node.IsNil() {
		return node
	}
	visited := node.VisitEachChild(v)
	newNode := visited
	if !ast.NodeIsSynthesized(visited) {
		newNode = v.Factory.DeepCloneNode(visited)
	}
	newNode.ForEachChild(func(child ast.Handle) bool {
		child.SetParent(newNode)
		return true
	})
	newNode.SetLoc(core.NewTextRange(ct.getPos(node), ct.getEnd(node)))
	return newNode
}
func (ct *ChangeTrackerWriter) assignPositionsToNodeArray(nodes ast.ListRef, v *ast.HandleVisitor) ast.ListRef {
	visited := v.VisitNodes(nodes)
	if visited == 0 {
		return 0
	}
	if nodes == 0 {
		panic("if nodes is 0, visited should not be 0")
	}
	store := v.Factory.Store()
	elems := store.ListSlice(visited).Slice()
	return v.Factory.List(core.NewTextRange(ct.getPos(nodes), ct.getEnd(nodes)), elems...)
}
func (ct *ChangeTrackerWriter) Write(text string) {
	ct.textWriter.Write(text)
	ct.setLastNonTriviaPosition(text, false)
}
func (ct *ChangeTrackerWriter) WriteTrailingSemicolon(text string) {
	ct.textWriter.WriteTrailingSemicolon(text)
	ct.setLastNonTriviaPosition(text, false)
}
func (ct *ChangeTrackerWriter) WriteComment(text string) {
	ct.textWriter.WriteComment(text)
}
func (ct *ChangeTrackerWriter) WriteKeyword(text string) {
	ct.textWriter.WriteKeyword(text)
	ct.setLastNonTriviaPosition(text, false)
}
func (ct *ChangeTrackerWriter) WriteOperator(text string) {
	ct.textWriter.WriteOperator(text)
	ct.setLastNonTriviaPosition(text, false)
}
func (ct *ChangeTrackerWriter) WritePunctuation(text string) {
	ct.textWriter.WritePunctuation(text)
	ct.setLastNonTriviaPosition(text, false)
}
func (ct *ChangeTrackerWriter) WriteSpace(text string) {
	ct.textWriter.WriteSpace(text)
	ct.setLastNonTriviaPosition(text, false)
}
func (ct *ChangeTrackerWriter) WriteStringLiteral(text string) {
	ct.textWriter.WriteStringLiteral(text)
	ct.setLastNonTriviaPosition(text, false)
}
func (ct *ChangeTrackerWriter) WriteParameter(text string) {
	ct.textWriter.WriteParameter(text)
	ct.setLastNonTriviaPosition(text, false)
}
func (ct *ChangeTrackerWriter) WriteProperty(text string) {
	ct.textWriter.WriteProperty(text)
	ct.setLastNonTriviaPosition(text, false)
}
func (ct *ChangeTrackerWriter) WriteSymbol(text string, symbol *ast.Symbol) {
	ct.textWriter.WriteSymbol(text, symbol)
	ct.setLastNonTriviaPosition(text, false)
}
func (ct *ChangeTrackerWriter) WriteLine() {
	ct.textWriter.WriteLine()
}
func (ct *ChangeTrackerWriter) WriteLineForce(force bool) {
	ct.textWriter.WriteLineForce(force)
}
func (ct *ChangeTrackerWriter) IncreaseIndent() {
	ct.textWriter.IncreaseIndent()
}
func (ct *ChangeTrackerWriter) DecreaseIndent() {
	ct.textWriter.DecreaseIndent()
}
func (ct *ChangeTrackerWriter) Clear() {
	ct.textWriter.Clear()
	ct.lastNonTriviaPosition = 0
}
func (ct *ChangeTrackerWriter) String() string {
	return ct.textWriter.String()
}
func (ct *ChangeTrackerWriter) RawWrite(s string) {
	ct.textWriter.RawWrite(s)
	ct.setLastNonTriviaPosition(s, false)
}
func (ct *ChangeTrackerWriter) WriteLiteral(s string) {
	ct.textWriter.WriteLiteral(s)
	ct.setLastNonTriviaPosition(s, true)
}
func (ct *ChangeTrackerWriter) GetTextPos() int {
	return ct.textWriter.GetTextPos()
}
func (ct *ChangeTrackerWriter) GetLine() int {
	return ct.textWriter.GetLine()
}
func (ct *ChangeTrackerWriter) GetColumn() core.UTF16Offset {
	return ct.textWriter.GetColumn()
}
func (ct *ChangeTrackerWriter) GetIndent() int {
	return ct.textWriter.GetIndent()
}
func (ct *ChangeTrackerWriter) IsAtStartOfLine() bool {
	return ct.textWriter.IsAtStartOfLine()
}
func (ct *ChangeTrackerWriter) HasTrailingComment() bool {
	return ct.textWriter.HasTrailingComment()
}
func (ct *ChangeTrackerWriter) HasTrailingWhitespace() bool {
	return ct.textWriter.HasTrailingWhitespace()
}
