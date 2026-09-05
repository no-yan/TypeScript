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

func buildFactoryTree(f *ast.Factory, depth int) ast.Handle {
	if depth <= 1 {
		return f.NewIdentifier("x")
	}
	left := buildFactoryTree(f, depth-1)
	right := buildFactoryTree(f, depth-1)
	op := f.NewToken(ast.KindPlusToken)
	return f.NewBinaryExpression(0, left, ast.Handle{}, op, right)
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
		walkStore(root, func(h ast.Handle) { sink += int(h.Kind) })
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
	f := ast.NewFactory(ast.FactoryHooks{})
	root := buildFactoryTree(f, benchTreeDepth)
	var sink int
	for b.Loop() {
		sink = 0
		walkStore(root, func(h ast.Handle) { sink += int(h.Kind) })
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
	s := ast.NewStore(1 << 16)
	f := ast.NewFactoryOn(s, ast.FactoryHooks{})
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
	b.Run("HandleNativeFactory", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			f, root := buildHandleNativeParserShape()
			runtime.KeepAlive(f)
			runtime.KeepAlive(root)
		}
	})
}

func BenchmarkCopySubtree(b *testing.B) {
	src := ast.NewFactory(ast.FactoryHooks{})
	root := buildFactoryTree(src, benchTreeDepth)
	b.ReportAllocs()
	for b.Loop() {
		dst := ast.NewFactoryOn(ast.NewStore(1<<16), ast.FactoryHooks{})
		copied := dst.CopySubtree(root)
		runtime.KeepAlive(copied)
		runtime.KeepAlive(dst)
	}
	runtime.KeepAlive(src)
	runtime.KeepAlive(root)
}

const nodeSeqBenchCount = 256

func BenchmarkListSlice(b *testing.B) {
	f := ast.NewFactoryHint(ast.FactoryHooks{}, nodeSeqBenchCount+8)
	elems := make([]ast.Handle, nodeSeqBenchCount)
	for i := range elems {
		elems[i] = f.NewIdentifier("n" + strconv.Itoa(i))
	}
	list := f.List(core.UndefinedTextRange(), elems...)
	s := f.Store()
	b.ReportAllocs()
	var sink int
	for b.Loop() {
		for i, h := range s.ListSlice(list).All() {
			sink += i + int(h.Kind)
		}
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(f)
}

func BenchmarkDeclarationNodes(b *testing.B) {
	s := ast.NewStore(nodeSeqBenchCount + 8)
	ast.RegisterStore(s)
	decls := make([]ast.GlobalRef, 0, nodeSeqBenchCount)
	for i := range nodeSeqBenchCount {
		h := s.Alloc(ast.KindIdentifier, 0, core.UndefinedTextRange(), 0)
		h.SetIdent(s.Intern("d" + strconv.Itoa(i)))
		decls = append(decls, h.Global())
	}
	sym := &ast.Symbol{Name: "bench", Declarations: decls}
	b.ReportAllocs()
	var sink int
	for b.Loop() {
		for i, h := range ast.DeclarationNodes(sym).All() {
			sink += i + int(h.Kind)
		}
	}
	runtime.KeepAlive(sink)
	runtime.KeepAlive(sym)
	runtime.KeepAlive(s)
}
