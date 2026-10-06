package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

const (
	pChecker = tscPrefix + "checker."
	sChecker = tscPrefix + "storechecker."
	pAst     = tscPrefix + "ast."
	sAst     = tscPrefix + "ast/store."
	core     = tscPrefix + "core."
	pRegion  = tscPrefix + "storeclitest.xctraceCheckPointer"
	sRegion  = tscPrefix + "storeclitest.xctraceCheckStore"
	shape    = "[go.shape.*github.com/x/y.T,go.shape.struct { github.com/x/y.a *github.com/x/y.T }]"
)

func TestParseFunc(t *testing.T) {
	for _, c := range []struct {
		raw                            string
		pkg, recv, name, closure, keyS string
		generic                        bool
	}{
		{pChecker + "(*Checker).getControlFlowContainer.func1", "checker", "Checker", "getControlFlowContainer", "func1", "checker.(Checker).getControlFlowContainer", false},
		{sChecker + "getExistingNodeTreeVisitor.func8.1", "storechecker", "", "getExistingNodeTreeVisitor", "func8.1", "checker.getExistingNodeTreeVisitor", false},
		{core + "(*LinkStore" + shape + ").Get", "core", "LinkStore", "Get", "", "core.(LinkStore).Get", true},
		{sAst + "Node.Parent", "ast/store", "Node", "Parent", "", "ast.(Node).Parent", false},
		{"slices.Grow[go.shape.[]*github.com/x/y.T,go.shape.*uint8] (inline)", "slices", "", "Grow", "", "slices.Grow", true},
		{"internal/runtime/maps.(*Map).getWithKeySmall", "internal/runtime/maps", "Map", "getWithKeySmall", "", "internal/runtime/maps.(Map).getWithKeySmall", false},
		{sChecker + "(*Checker).createUnionOrIntersectionProperty-range1", "storechecker", "Checker", "createUnionOrIntersectionProperty", "", "checker.(Checker).createUnionOrIntersectionProperty", false},
		{tscPrefix + "storeclitest.markStages[go.shape.*uint8].func5", "storeclitest", "", "markStages", "func5", "storeclitest.markStages", true},
		{pChecker + "glob..func1", "checker", "", "glob", "func1", "checker.glob", false},
		{"runtime.mallocgc", "runtime", "", "mallocgc", "", "runtime.mallocgc", false},
		{pChecker + "(*Checker).getTypeOfSymbol-fm", "checker", "Checker", "getTypeOfSymbol", "", "checker.(Checker).getTypeOfSymbol", false},
	} {
		f := parseFunc(c.raw)
		if f.Pkg != c.pkg || f.Recv != c.recv || f.Name != c.name || f.Closure != c.closure || f.Key() != c.keyS || f.Generic != c.generic {
			t.Errorf("%s: got %+v key %s", c.raw, f, f.Key())
		}
	}
}

func testSources() *sources {
	return &sources{
		callbacks: map[string]bool{"checker.Checker.getControlFlowContainer.func1": true},
		bodies: map[string]map[string]string{
			"checker":      {"Checker.f": "A", "Checker.g": "B", "Checker.h": "C"},
			"storechecker": {"Checker.f": "A", "Checker.g": "X"},
		},
	}
}

func TestClassify3a(t *testing.T) {
	s := testSources()
	for _, c := range []struct {
		name      string
		frames    []string
		mechanism string
		function  string
	}{
		{"map under a links table", []string{"internal/runtime/maps.(*Map).getWithKeySmall", "runtime.mapaccess1_fast64", core + "(*LinkStore" + shape + ").Get", pChecker + "(*Checker).f", pRegion}, mechLinks, "core.(LinkStore).Get"},
		{"map under the checker", []string{"internal/runtime/maps.(*Map).getWithKeySmall", "runtime.mapaccess2_faststr", pChecker + "(*Checker).f", pRegion}, mechOtherMap, "checker.(Checker).f"},
		{"map growth is the map", []string{"runtime.mallocgc", "runtime.newarray", "internal/runtime/maps.newTable", "runtime.mapassign_fast64", sChecker + "(*Checker).f", sRegion}, mechOtherMap, "checker.(Checker).f"},
		{"allocation", []string{"runtime.nextFreeFast", "runtime.mallocgc", "runtime.newobject", pChecker + "(*Checker).f", pRegion}, mechAlloc, "checker.(Checker).f"},
		{"growslice's memmove is the allocation", []string{"runtime.memmove", "runtime.growslice", pChecker + "(*Checker).f", pRegion}, mechAlloc, "checker.(Checker).f"},
		{"memmove", []string{"runtime.memmove", pChecker + "(*Checker).f", pRegion}, mechMemmove, "checker.(Checker).f"},
		{"memequal outside a map", []string{"runtime.memequal", pChecker + "(*Checker).f", pRegion}, mechRuntime, "checker.(Checker).f"},
		{"walk", []string{sAst + "FindAncestor", sChecker + "(*Checker).f", sRegion}, mechWalk, "ast.FindAncestor"},
		{"Node.File is a walk", []string{sAst + "Node.File", sChecker + "(*Checker).f", sRegion}, mechWalk, "ast.(Node).File"},
		{"walk callback", []string{pChecker + "(*Checker).getControlFlowContainer.func1", pAst + "FindAncestor", pRegion}, mechWalk, "checker.(Checker).getControlFlowContainer"},
		{"another closure of the same function", []string{pChecker + "(*Checker).getControlFlowContainer.func2", pRegion}, mechSelf, "checker.(Checker).getControlFlowContainer"},
		{"Store accessor", []string{sAst + "Node.Parent", sChecker + "(*Checker).f", sRegion}, mechAccessor, "ast.(Node).Parent"},
		{"Pointer accessor", []string{pAst + "(*Identifier).Text", pChecker + "(*Checker).f", pRegion}, mechAccessor, "ast.(Identifier).Text"},
		{"factory is not an accessor", []string{sAst + "(*Factory).NewIdentifier", sChecker + "(*Checker).f", sRegion}, mechSelf, "ast.(Factory).NewIdentifier"},
		{"links method", []string{sChecker + "(*nodeLinkStore" + shape + ").Get", sChecker + "(*Checker).f", sRegion}, mechLinks, "checker.(nodeLinkStore).Get"},
		{"generic helper goes to its caller", []string{"slices.BinarySearchFunc" + shape, pChecker + "(*Checker).f", pRegion}, mechSelf, "checker.(Checker).f"},
		{"arena goes to its caller", []string{core + "(*Arena" + shape + ").New", pChecker + "(*Checker).newSymbol", pRegion}, mechSelf, "checker.(Checker).newSymbol"},
		{"stdlib", []string{"strings.Index", pChecker + "(*Checker).f", pRegion}, mechStdlib, "strings.Index"},
		{"adapter", []string{sChecker + "(*Checker).f", tscPrefix + "storeharness.(*adapter).Display", sRegion}, mechAdapter, "checker.(Checker).f"},
		{"outside", []string{pChecker + "(*Checker).f", tscPrefix + "storeclitest.TestXctraceNativeCheck"}, mechOutside, ""},
		{"interval function self", []string{sRegion, tscPrefix + "storeclitest.TestXctraceNativeCheck"}, mechRegionSelf, "storeclitest.xctraceCheckStore"},
		{"runtime right under the interval", []string{"runtime.mallocgc", "runtime.growslice", sRegion}, mechAlloc, "storeclitest.xctraceCheckStore"},
	} {
		got := s.classify3a(c.frames, isRegion)
		if got.Mechanism != c.mechanism || got.Function != c.function {
			t.Errorf("%s: got %s %q, want %s %q", c.name, got.Mechanism, got.Function, c.mechanism, c.function)
		}
	}
}

func TestOrigin(t *testing.T) {
	s := testSources()
	for _, c := range []struct{ raw, want string }{
		{pChecker + "(*Checker).f", originSame},
		{sChecker + "(*Checker).f.func2", originSame},
		{sChecker + "(*Checker).g", originChanged},
		{pChecker + "(*Checker).h", originOneSide},
		{sAst + "Node.Parent", ""},
	} {
		if got := s.origin(parseFunc(c.raw)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestPrintBodyNamesStoreAsPointer(t *testing.T) {
	body := func(src string) string {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", "package p\nfunc f() {"+src+"}", 0)
		if err != nil {
			t.Fatal(err)
		}
		return printBody(f.Decls[0].(*ast.FuncDecl).Body)
	}
	if a, b := body("return ast.IsIdentifier(n)"), body("return store.IsIdentifier(n)"); a != b {
		t.Errorf("%q != %q", a, b)
	}
	if a, b := body("return ast.IsIdentifier(n)"), body("return ast.IsStringLiteral(n)"); a == b {
		t.Errorf("different bodies print the same")
	}
}

func TestClassify3b(t *testing.T) {
	for _, c := range []struct {
		name     string
		frames   []string
		category string
		site     string
	}{
		{"the Store's slabs", []string{sAst + "(*Builder).Finish", tscPrefix + "storeparser.(*Parser).parseSourceFileWorker", tscPrefix + "storecompiler.f"}, catParseBind, "ast/store.(Builder).Finish"},
		{"strings.Clone in Finish", []string{"internal/stringslite.Clone", "strings.Clone", sAst + "(*Builder).Finish", tscPrefix + "storeparser.(*Parser).parseSourceFileWorker"}, catParseBind, "internal/stringslite.Clone"},
		{"a links table's arena", []string{"slices.Grow" + shape, core + "(*Arena" + shape + ").New", core + "(*LinkStore" + shape + ").Get", sChecker + "(*Checker).f"}, catLinks, "core.(LinkStore).Get"},
		{"a links page", []string{sChecker + "(*nodeLinkStore" + shape + ").newPage", sChecker + "(*nodeLinkStore" + shape + ").Get", sChecker + "(*Checker).f"}, catLinks, "storechecker.(nodeLinkStore).newPage"},
		{"checker symbol", []string{core + "(*Arena" + shape + ").New", pChecker + "(*Checker).newSymbol", pChecker + "(*Checker).f"}, catChecker, "checker.(Checker).newSymbol"},
		{"a node made by the checker", []string{pAst + "(*NodeFactory).NewIdentifier", pChecker + "(*Checker).f"}, catChecker, "ast.(NodeFactory).NewIdentifier"},
		{"a node made by the parser", []string{pAst + "(*NodeFactory).NewIdentifier", tscPrefix + "parser.(*Parser).newIdentifier"}, catParseBind, "ast.(NodeFactory).NewIdentifier"},
		{"source text", []string{"os.readFileContents", "os.ReadFile", tscPrefix + "vfs/osvfs.(*osFS).ReadFile", tscPrefix + "compiler.f"}, catOther, "os.readFileContents"},
		{"binder symbol", []string{core + "(*Arena" + shape + ").New", tscPrefix + "binder.(*Binder).newSymbol"}, catParseBind, "binder.(Binder).newSymbol"},
	} {
		got := classify3b(c.frames)
		if got.Category != c.category || got.Site != c.site {
			t.Errorf("%s: got %s %q, want %s %q", c.name, got.Category, got.Site, c.category, c.site)
		}
	}
}

func TestParseTraces(t *testing.T) {
	in := `File: x
Type: inuse_space
-----------+-------------------------------------------------------
     bytes:  18kB
    18639B   a.f
             a.g (inline)
-----------+-------------------------------------------------------
         0   b.f
-----------+-------------------------------------------------------
   -4112B   c.f
`
	traces, err := parseTraces(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(traces) != 3 || traces[0].Value != 18639 || len(traces[0].Frames) != 2 || traces[0].Frames[1] != "a.g (inline)" || traces[1].Value != 0 || traces[2].Value != -4112 {
		t.Errorf("got %+v", traces)
	}
}
