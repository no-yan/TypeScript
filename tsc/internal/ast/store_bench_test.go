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
