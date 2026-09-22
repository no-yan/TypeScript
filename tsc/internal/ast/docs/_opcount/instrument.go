// instrument rewrites the tsc packages into an overlay that counts every AST
// accessor call and every read of an ast struct field. The tree is untouched.
//
//	cd tsc && go run <this file> -out <dir>
//	go test -overlay <dir>/overlay.json -run TestOpCount ./internal/compiler
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const astPath = "github.com/microsoft/TypeScript/tsc/internal/ast"

type edit struct {
	off    int
	text   string
	prefix bool
	xEnd   int // for prefixes at the same offset: outer (larger xEnd) first
}

var (
	names   []string
	nameIdx = map[string]int{}
)

func opIndex(name string) int {
	if i, ok := nameIdx[name]; ok {
		return i
	}
	nameIdx[name] = len(names)
	names = append(names, name)
	return len(names) - 1
}

func main() {
	out := flag.String("out", "", "output dir")
	driver := flag.String("driver", "", "driver test file to add to internal/compiler")
	flag.Parse()
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo}
	pkgs, err := packages.Load(cfg, "./internal/...")
	if err != nil {
		panic(err)
	}
	replace := map[string]string{}
	var astDir string
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			fmt.Fprintln(os.Stderr, "skip (errors):", pkg.PkgPath, pkg.Errors[0])
			continue
		}
		if strings.Contains(pkg.PkgPath, "/testutil") {
			continue
		}
		inAst := pkg.PkgPath == astPath
		for i, file := range pkg.Syntax {
			filename := pkg.CompiledGoFiles[i]
			if inAst {
				astDir = filepath.Dir(filename)
			}
			edits := rewriteFile(pkg, file, inAst)
			if len(edits) == 0 {
				continue
			}
			src, err := os.ReadFile(filename)
			if err != nil {
				panic(err)
			}
			if !inAst {
				end := pkg.Fset.Position(file.Name.End()).Offset
				edits = append(edits, edit{off: end, text: "; import opast \"" + astPath + "\"", prefix: false})
			}
			dst := filepath.Join(*out, strings.ReplaceAll(strings.TrimPrefix(pkg.PkgPath, "github.com/microsoft/TypeScript/tsc/"), "/", "__")+"__"+filepath.Base(filename))
			if err := os.WriteFile(dst, apply(src, edits), 0o644); err != nil {
				panic(err)
			}
			replace[filename] = dst
		}
	}
	// counter runtime
	var b strings.Builder
	b.WriteString(runtimeSrc)
	fmt.Fprintf(&b, "\nvar opNames = [...]string{\n")
	for _, n := range names {
		fmt.Fprintf(&b, "\t%q,\n", n)
	}
	b.WriteString("}\n")
	rt := filepath.Join(*out, "zz_opcount.go")
	if err := os.WriteFile(rt, []byte(b.String()), 0o644); err != nil {
		panic(err)
	}
	replace[filepath.Join(astDir, "zz_opcount.go")] = rt
	if *driver != "" {
		replace[filepath.Join(filepath.Dir(astDir), "compiler", "zz_opcount_test.go")] = *driver
	}
	data, _ := json.MarshalIndent(map[string]any{"Replace": replace}, "", " ")
	if err := os.WriteFile(filepath.Join(*out, "overlay.json"), data, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("ops=%d files=%d\n", len(names), len(replace))
}

func apply(src []byte, edits []edit) []byte {
	sort.SliceStable(edits, func(i, j int) bool {
		a, b := edits[i], edits[j]
		if a.off != b.off {
			return a.off < b.off
		}
		if a.prefix != b.prefix {
			return !a.prefix // close parens before a new prefix
		}
		if a.prefix {
			return a.xEnd > b.xEnd
		}
		return false
	})
	var out []byte
	last := 0
	for _, e := range edits {
		out = append(out, src[last:e.off]...)
		out = append(out, e.text...)
		last = e.off
	}
	return append(out, src[last:]...)
}

func isNodePtr(t types.Type) bool {
	p, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := p.Elem().(*types.Named)
	return ok && n.Obj().Name() == "Node" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == astPath
}

func rewriteFile(pkg *packages.Package, file *ast.File, inAst bool) []edit {
	fset := pkg.Fset
	info := pkg.TypesInfo
	var edits []edit
	qual := "opast."
	where := "out"
	if inAst {
		qual = ""
		where = "ast"
	}
	offset := func(p token.Pos) int { return fset.Position(p).Offset }

	// 1. function-entry counters, package ast only
	if inAst {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var param *ast.Field
			label := ""
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				param = fn.Recv.List[0]
				label = "M:Node." + fn.Name.Name + "()"
			} else if fn.Recv == nil && fn.Type.Params != nil && len(fn.Type.Params.List) > 0 && fn.Type.TypeParams == nil {
				param = fn.Type.Params.List[0]
				label = "F:ast." + fn.Name.Name + "()"
			}
			if param == nil || len(param.Names) == 0 || param.Names[0].Name == "_" {
				continue
			}
			if !isNodePtr(info.TypeOf(param.Type)) {
				continue
			}
			idx := opIndex(label)
			edits = append(edits, edit{off: offset(fn.Body.Lbrace) + 1, text: fmt.Sprintf(" opCountNode(%d, %s);", idx, param.Names[0].Name)})
		}
	}

	// 2. field reads of ast structs
	skip := map[ast.Expr]bool{}
	markLHS := func(e ast.Expr) {
		for {
			switch x := e.(type) {
			case *ast.ParenExpr:
				e = x.X
				continue
			case *ast.IndexExpr:
				e = x.X
				continue
			}
			break
		}
		skip[e] = true
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for _, l := range s.Lhs {
				markLHS(l)
			}
		case *ast.IncDecStmt:
			markLHS(s.X)
		case *ast.RangeStmt:
			if s.Key != nil {
				markLHS(s.Key)
			}
			if s.Value != nil {
				markLHS(s.Value)
			}
		case *ast.UnaryExpr:
			if s.Op == token.AND {
				markLHS(s.X)
			}
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || skip[sel] {
			return true
		}
		selection := info.Selections[sel]
		if selection == nil || selection.Kind() != types.FieldVal {
			return true
		}
		field := selection.Obj()
		if field.Pkg() == nil || field.Pkg().Path() != astPath {
			return true
		}
		recv := selection.Recv()
		ptr, ok := recv.(*types.Pointer)
		if !ok {
			return true // value receivers: the wrapper would return a non-addressable copy
		}
		named, ok := ptr.Elem().(*types.Named)
		if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != astPath || named.TypeArgs().Len() > 0 {
			return true
		}
		fn, tag := "OpF", "S:"
		if named.Obj().Name() == "Node" {
			fn, tag = "OpN", "N:"
		}
		idx := opIndex(tag + named.Obj().Name() + "." + field.Name() + "@" + where)
		edits = append(edits,
			edit{off: offset(sel.X.Pos()), text: fmt.Sprintf("%s%s(%d, ", qual, fn, idx), prefix: true, xEnd: offset(sel.X.End())},
			edit{off: offset(sel.X.End()), text: ")"},
		)
		return true
	})
	return edits
}

const runtimeSrc = `package ast

import (
	"fmt"
	"io"
	"sync/atomic"
)

const opKinds = int(KindCount) + 1

var opCounts = make([]uint64, 8192*opKinds)

func opCountNode(i int, n *Node) {
	k := int(KindCount)
	if n != nil {
		k = int(n.Kind)
	}
	atomic.AddUint64(&opCounts[i*opKinds+k], 1)
}

func OpN(i int, n *Node) *Node {
	opCountNode(i, n)
	return n
}

func OpF[T any](i int, x T) T {
	atomic.AddUint64(&opCounts[i*opKinds], 1)
	return x
}

func OpCountReset() {
	for i := range opCounts {
		atomic.StoreUint64(&opCounts[i], 0)
	}
}

// OpCountDump writes "phase\top\tkind\tcount" rows and resets the counters.
func OpCountDump(w io.Writer, phase string) {
	for i, name := range opNames {
		for k := 0; k < opKinds; k++ {
			c := atomic.SwapUint64(&opCounts[i*opKinds+k], 0)
			if c == 0 {
				continue
			}
			kind := "-"
			if name[:2] != "S:" {
				if k == int(KindCount) {
					kind = "nil"
				} else {
					kind = Kind(k).String()
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", phase, name, kind, c)
		}
	}
}
`
