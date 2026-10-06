package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// sources are what the rules read from the tsc source: the closures passed to
// the ancestor walks, and the bodies that give a function's origin.
type sources struct {
	callbacks map[string]bool              // pkg.Recv.Name.Closure
	bodies    map[string]map[string]string // pkg -> Recv.Name -> body, the Store packages named as the Pointer's
}

// originPairs are the packages whose functions get an origin (5.2).
var originPairs = [][2]string{{"checker", "storechecker"}, {"binder", "storebinder"}, {"compiler", "storecompiler"}}

// walkCallers are the walks that take a callback.
var walkCallers = map[string]bool{"FindAncestor": true, "FindAncestorOrQuit": true, "FindManyAncestors": true}

// loadSources reads the packages of tscInternal (tsc/internal) that the rules
// need, with the default build tags.
func loadSources(tscInternal string) (*sources, error) {
	s := &sources{callbacks: map[string]bool{}, bodies: map[string]map[string]string{}}
	pkgs := map[string]bool{"ast": true, "ast/store": true}
	for _, p := range originPairs {
		pkgs[p[0]], pkgs[p[1]] = true, true
	}
	for pkg := range pkgs {
		files, err := parseDir(filepath.Join(tscInternal, pkg))
		if err != nil {
			return nil, err
		}
		bodies := map[string]string{}
		for _, file := range files {
			for _, decl := range file.Decls {
				d, ok := decl.(*ast.FuncDecl)
				if !ok || d.Body == nil {
					continue
				}
				recv := recvName(d)
				s.addCallbacks(pkg, recv, d)
				key := d.Name.Name
				if recv != "" {
					key = recv + "." + key
				}
				if _, dup := bodies[key]; !dup {
					bodies[key] = printBody(d.Body)
				}
			}
		}
		s.bodies[pkg] = bodies
	}
	return s, nil
}

func parseDir(dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if ok, err := build.Default.MatchFile(dir, name); err != nil || !ok {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func recvName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return ""
	}
	t := d.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return fmt.Sprint(x)
		}
	}
}

// addCallbacks records the closures of d that are passed to a walk, named as
// the compiler names them: funcN for the Nth closure of d in source order,
// funcN.M for the Mth closure inside it.
func (s *sources) addCallbacks(pkg, recv string, d *ast.FuncDecl) {
	names := map[*ast.FuncLit]string{}
	var number func(n ast.Node, prefix string)
	number = func(n ast.Node, prefix string) {
		i := 0
		ast.Inspect(n, func(c ast.Node) bool {
			lit, ok := c.(*ast.FuncLit)
			if !ok || c == n {
				return true
			}
			i++
			name := fmt.Sprintf("%s%d", prefix, i)
			names[lit] = name
			number(lit, name+".")
			return false
		})
	}
	number(d.Body, "func")
	ast.Inspect(d.Body, func(c ast.Node) bool {
		call, ok := c.(*ast.CallExpr)
		if !ok {
			return true
		}
		var callee string
		switch f := call.Fun.(type) {
		case *ast.Ident:
			callee = f.Name
		case *ast.SelectorExpr:
			callee = f.Sel.Name
		}
		if !walkCallers[callee] {
			return true
		}
		for _, arg := range call.Args {
			if lit, ok := arg.(*ast.FuncLit); ok {
				s.callbacks[pkg+"."+recv+"."+d.Name.Name+"."+names[lit]] = true
			}
		}
		return true
	})
}

func (s *sources) isWalkCallback(f fn) bool {
	return f.Closure != "" && s.callbacks[f.Pkg+"."+f.Recv+"."+f.Name+"."+f.Closure]
}

// storeIdents are the Store package names as the Pointer's, so that a body
// that differs only in the package it names is the same.
var storeIdents = map[string]string{
	"store": "ast", "storechecker": "checker", "storebinder": "binder", "storecompiler": "compiler",
	"storeparser": "parser", "storeprinter": "printer", "storescanner": "scanner", "storeevaluator": "evaluator",
	"storemodule": "module", "storemodulespecifiers": "modulespecifiers", "storenodebuilder": "nodebuilder",
	"storepseudochecker": "pseudochecker",
}

func printBody(body *ast.BlockStmt) string {
	ast.Inspect(body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				if p, ok := storeIdents[id.Name]; ok {
					id.Name = p
				}
			}
		}
		return true
	})
	var b bytes.Buffer
	if err := printer.Fprint(&b, token.NewFileSet(), body); err != nil {
		panic(err)
	}
	return b.String()
}

// Origins of a function (5.2).
const (
	originSame    = "same name, same body"
	originChanged = "ported, changed"
	originOneSide = "one side only"
)

func (s *sources) origin(f fn) string {
	for _, p := range originPairs {
		if f.Pkg != p[0] && f.Pkg != p[1] {
			continue
		}
		key := f.Name
		if f.Recv != "" {
			key = f.Recv + "." + f.Name
		}
		a, okA := s.bodies[p[0]][key]
		b, okB := s.bodies[p[1]][key]
		switch {
		case okA && okB && a == b:
			return originSame
		case okA && okB:
			return originChanged
		default:
			return originOneSide
		}
	}
	return ""
}
