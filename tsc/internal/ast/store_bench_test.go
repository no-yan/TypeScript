package ast_test

import (
	"runtime"
	"strconv"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

const benchTreeDepth = 16

type ptrNode struct {
	kind     ast.Kind
	flags    ast.NodeFlags
	loc      core.TextRange
	parent   *ptrNode
	children []*ptrNode
	text     string
}

func buildPtrTree(depth int) *ptrNode {
	if depth <= 1 {
		return &ptrNode{kind: ast.KindIdentifier, loc: core.UndefinedTextRange(), text: "x"}
	}
	left := buildPtrTree(depth - 1)
	right := buildPtrTree(depth - 1)
	n := &ptrNode{
		kind:     ast.KindBinaryExpression,
		loc:      core.UndefinedTextRange(),
		children: []*ptrNode{left, right},
	}
	left.parent = n
	right.parent = n
	return n
}

func walkPtr(n *ptrNode, visit func(*ptrNode)) {
	if n == nil {
		return
	}
	visit(n)
	for _, c := range n.children {
		walkPtr(c, visit)
	}
}

func buildFactoryTree(f *ast.NodeFactory, depth int) *ast.Node {
	if depth <= 1 {
		return f.NewIdentifier("x")
	}
	left := buildFactoryTree(f, depth-1)
	right := buildFactoryTree(f, depth-1)
	op := f.NewToken(ast.KindPlusToken)
	return f.NewBinaryExpression(nil, left, nil, op, right)
}

func walkAst(n *ast.Node, visit func(*ast.Node)) {
	if n == nil {
		return
	}
	visit(n)
	n.ForEachChild(func(child *ast.Node) bool {
		walkAst(child, visit)
		return false
	})
}

func BenchmarkWalkStore(b *testing.B) {
	s := ast.NewStore(1 << 16)
	root := buildStoreTree(s, benchTreeDepth)
	var sink int
	for b.Loop() {
		sink = 0
		walkStore(root, func(h ast.Handle) { sink += int(h.Kind()) })
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(s)
}

func BenchmarkWalkPtrTree(b *testing.B) {
	root := buildPtrTree(benchTreeDepth)
	var sink int
	for b.Loop() {
		sink = 0
		walkPtr(root, func(n *ptrNode) { sink += int(n.kind) })
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(root)
}

func BenchmarkWalkFactoryTree(b *testing.B) {
	f := ast.NewNodeFactory(ast.NodeFactoryHooks{})
	root := buildFactoryTree(f, benchTreeDepth)
	var sink int
	for b.Loop() {
		sink = 0
		walkAst(root, func(n *ast.Node) { sink += int(n.Kind) })
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(root)
}

func BenchmarkGCPauseStore(b *testing.B) {
	s := ast.NewStore(1 << 16)
	root := buildStoreTree(s, benchTreeDepth)
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(s)
	runtime.KeepAlive(root)
}

func BenchmarkGCPausePtrTree(b *testing.B) {
	root := buildPtrTree(benchTreeDepth)
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(root)
}

func BenchmarkGCPauseFactoryTree(b *testing.B) {
	f := ast.NewNodeFactory(ast.NodeFactoryHooks{})
	root := buildFactoryTree(f, benchTreeDepth)
	runtime.GC()
	b.ResetTimer()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(f)
	runtime.KeepAlive(root)
}

func BenchmarkAllocStore(b *testing.B) {
	for b.Loop() {
		s := ast.NewStore(1024)
		for i := range 1024 {
			h := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
			h.SetIdent(s.Intern(strconv.Itoa(i % 32)))
		}
		runtime.KeepAlive(s)
	}
}

const parserShapedDeclarationCount = 256

func buildDualWriteParserShape() (*ast.NodeFactory, *ast.Node) {
	f := ast.NewNodeFactory(ast.NodeFactoryHooks{})
	f.AttachStore(ast.NewStore(parserShapedDeclarationCount * 5))
	statements := make([]*ast.Node, 0, parserShapedDeclarationCount)
	for i := range parserShapedDeclarationCount {
		start := i * 16
		name := f.NewIdentifier("value" + strconv.Itoa(i))
		name.Loc = core.NewTextRange(start+6, start+11)
		f.StoreSync(name)
		value := f.NewNumericLiteral(strconv.Itoa(i), ast.TokenFlagsNone)
		value.Loc = core.NewTextRange(start+14, start+15)
		f.StoreSync(value)
		declaration := f.NewVariableDeclaration(name, nil, nil, value)
		declaration.Loc = core.NewTextRange(start+6, start+15)
		f.StoreSync(declaration)
		declarations := f.NewNodeList([]*ast.Node{declaration})
		declarations.Loc = declaration.Loc
		declarationList := f.NewVariableDeclarationList(declarations, ast.NodeFlagsConst)
		declarationList.Loc = core.NewTextRange(start, start+15)
		f.StoreSync(declarationList)
		statement := f.NewVariableStatement(nil, declarationList)
		statement.Loc = core.NewTextRange(start, start+16)
		f.StoreSync(statement)
		statements = append(statements, statement)
	}
	list := f.NewNodeList(statements)
	list.Loc = core.NewTextRange(0, parserShapedDeclarationCount*16)
	root := f.NewBlock(list, true)
	root.Loc = core.NewTextRange(0, parserShapedDeclarationCount*16+2)
	f.StoreSync(root)
	return f, root
}

func buildHandleNativeParserShape() (*ast.Factory, ast.Handle) {
	f := ast.NewFactoryHint(ast.FactoryHooks{}, parserShapedDeclarationCount*5)
	statements := make([]ast.Handle, 0, parserShapedDeclarationCount)
	for i := range parserShapedDeclarationCount {
		start := i * 16
		name := f.Finish(
			f.NewIdentifier("value"+strconv.Itoa(i)),
			core.NewTextRange(start+6, start+11),
		)
		value := f.Finish(
			f.NewNumericLiteral(strconv.Itoa(i), ast.TokenFlagsNone),
			core.NewTextRange(start+14, start+15),
		)
		declaration := f.Finish(
			f.NewVariableDeclaration(name, ast.Handle{}, ast.Handle{}, value),
			core.NewTextRange(start+6, start+15),
		)
		declarations := f.List(declaration.Loc(), declaration)
		declarationList := f.Finish(
			f.NewVariableDeclarationList(declarations, ast.NodeFlagsConst),
			core.NewTextRange(start, start+15),
		)
		statement := f.Finish(
			f.NewVariableStatement(0, declarationList),
			core.NewTextRange(start, start+16),
		)
		statements = append(statements, statement)
	}
	list := f.List(core.NewTextRange(0, parserShapedDeclarationCount*16), statements...)
	root := f.Finish(
		f.NewBlock(list, true),
		core.NewTextRange(0, parserShapedDeclarationCount*16+2),
	)
	return f, root
}

func BenchmarkParserShapedConstruction(b *testing.B) {
	b.Run("DualWriteNodeFactory", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			f, root := buildDualWriteParserShape()
			runtime.KeepAlive(f)
			runtime.KeepAlive(root)
		}
	})
	b.Run("HandleNativeFactory", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			f, root := buildHandleNativeParserShape()
			runtime.KeepAlive(f)
			runtime.KeepAlive(root)
		}
	})
}
