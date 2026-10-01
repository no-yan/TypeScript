package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// P2 (7d-0 instructions 5.3, 7d-2a instructions 3.4): a declaration of a
// *_manual.go file of an output package replaces the translation of the
// Pointer declaration of the same name. It carries the hash of that
// declaration's source, and the translation stops when the Pointer source
// changes. A List version <F>List (3.4) carries the hash of F.
//
// A P2 declaration may carry more than one hash line (7d-2b-1 instructions
// 3.2): the hash of every Pointer declaration it copies, each checked. A
// replacement names exactly one of them (itself or <name>List). A store-only
// declaration, marked by a storetranslate:store-only line, replaces none: its
// name is not the Pointer's, and its hash lines name the declarations it
// copies. A P2 declaration without the mark whose name the Pointer lacks
// stops the translation, so that a misspelt name is not taken as a helper.

type manualDecl struct {
	key       string // name, Type.name or type name, in the output package
	hashes    []hashLine
	storeOnly bool
	isType    bool
	// names are the identifiers of the Store body, and quals the qualified
	// names (package name -> names): the reach follows them instead of the
	// Pointer body.
	names []string
	quals map[string][]string
	sels  []string // the selector names (methods)
}

// hashLine is a storetranslate:pointer-hash line: a Pointer declaration (its
// key in the source package, and its file's base name) and its hash.
type hashLine struct {
	hash, file, pointer string
}

var (
	hashRe      = regexp.MustCompile(`^// storetranslate:pointer-hash ([0-9a-f]{16}) (\S+):(\S+)$`)
	storeOnlyRe = regexp.MustCompile(`^// storetranslate:store-only$`)
)

// readManuals reads the *_manual.go files of every output package and checks
// each hash against the Pointer declaration.
func readManuals(w *world) error {
	var errs []string
	for _, o := range w.outs {
		entries, err := os.ReadDir(o.dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, e := range entries {
			if !isManualFile(e.Name()) {
				continue
			}
			es, err := readManualFile(w, o, filepath.Join(o.dir, e.Name()))
			if err != nil {
				return err
			}
			errs = append(errs, es...)
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("the P2 declarations:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

func readManualFile(w *world, o *outPkg, path string) ([]string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, text, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(path)
	var errs []string
	for _, d := range file.Decls {
		var doc *ast.CommentGroup
		var key string
		isType := false
		var body ast.Node
		switch d := d.(type) {
		case *ast.FuncDecl:
			doc, body = d.Doc, d
			key = declNames(d)[0]
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				// Constants and variables of a P2 file are the Store's own.
				continue
			}
			if len(d.Specs) != 1 {
				errs = append(errs, fmt.Sprintf("%s: a P2 type declaration declares one type", base))
				continue
			}
			doc, body, isType = d.Doc, d, true
			key = d.Specs[0].(*ast.TypeSpec).Name.Name
		default:
			continue
		}
		m := &manualDecl{key: key, isType: isType, quals: map[string][]string{}}
		if doc != nil {
			for _, c := range doc.List {
				if sub := hashRe.FindStringSubmatch(c.Text); sub != nil {
					m.hashes = append(m.hashes, hashLine{hash: sub[1], file: sub[2], pointer: sub[3]})
				}
				if storeOnlyRe.MatchString(c.Text) {
					m.storeOnly = true
				}
			}
		}
		if len(m.hashes) == 0 {
			errs = append(errs, fmt.Sprintf("%s: %s: no storetranslate:pointer-hash line", base, key))
			continue
		}
		if m.storeOnly {
			if w.decl(o.srcPath, key) != nil {
				errs = append(errs, fmt.Sprintf("%s: %s: marked store-only, but it names the Pointer declaration %s in %s: make it a replacement", base, key, key, o.src))
				continue
			}
		} else {
			var replaced []string
			for _, h := range m.hashes {
				if h.pointer == key || h.pointer+"List" == key {
					replaced = append(replaced, h.pointer)
				}
			}
			if len(replaced) != 1 {
				var named []string
				for _, h := range m.hashes {
					named = append(named, h.pointer)
				}
				errs = append(errs, fmt.Sprintf("%s: %s: the hash lines name %s, and one must name the declaration it replaces (%s or its List version; a helper of the Store's own is marked storetranslate:store-only)", base, key, strings.Join(named, ", "), key))
				continue
			}
		}
		broken := false
		for _, h := range m.hashes {
			pd := w.decl(o.srcPath, h.pointer)
			if pd == nil {
				errs = append(errs, fmt.Sprintf("%s: %s: no declaration %s in %s", base, key, h.pointer, o.src))
				broken = true
				continue
			}
			if pd.fileName != h.file {
				errs = append(errs, fmt.Sprintf("%s: %s: the Pointer declaration is in %s, not %s", base, key, pd.fileName, h.file))
			}
			if got := pd.hash(); got != h.hash {
				errs = append(errs, fmt.Sprintf("%s: %s: the Pointer declaration %s changed (hash %s, recorded %s): check the P2 version against it and update the hash", base, key, h.pointer, got, h.hash))
			}
		}
		if broken {
			continue
		}
		m.names, m.quals, m.sels = bodyRefs(body)
		o.manual[key] = m
	}
	return errs, nil
}

// hash is the first 16 hex digits of the sha256 of a Pointer declaration's
// source: a function from func to the closing brace, a type from type to the
// end of its type (the doc comment is not part of it).
func (d *declInfo) hash() string {
	sum := sha256.Sum256([]byte(d.source()))
	return hex.EncodeToString(sum[:])[:16]
}

func (d *declInfo) source() string {
	switch n := d.node.(type) {
	case *ast.FuncDecl:
		return sourceOf(d.w.fset, n.Pos(), n.End())
	case *ast.TypeSpec:
		if len(d.gen.Specs) == 1 {
			return sourceOf(d.w.fset, d.gen.Pos(), d.gen.End())
		}
		return "type " + sourceOf(d.w.fset, n.Pos(), n.End())
	}
	return sourceOf(d.w.fset, d.node.Pos(), d.node.End())
}

func sourceOf(fset *token.FileSet, from, to token.Pos) string {
	file := fset.File(from)
	text, err := os.ReadFile(file.Name())
	if err != nil {
		panic(err)
	}
	return string(text[file.Offset(from):file.Offset(to)])
}

// printHashes prints the pointer-hash line of Pointer declarations, named
// pkg.Name or pkg.Type.Name (pkg relative to internal/; ast when omitted).
func printHashes(root, names string) error {
	w, err := loadWorld(root, nil)
	if err != nil {
		return err
	}
	for _, name := range strings.Split(names, ",") {
		pkg, rest := "ast", name
		if i := strings.Index(name, "."); i >= 0 && w.outs[internal+name[:i]] != nil {
			pkg, rest = name[:i], name[i+1:]
		}
		d := w.decl(internal+pkg, rest)
		if d == nil {
			return fmt.Errorf("no declaration %s in %s", rest, pkg)
		}
		fmt.Printf("// storetranslate:pointer-hash %s %s:%s\n", d.hash(), d.fileName, rest)
	}
	return nil
}

// bodyRefs are the identifiers of Store code that the reach follows: the
// unqualified names that the code does not declare itself (parameters,
// locals), the qualified names by package name, and the selector names.
func bodyRefs(body ast.Node) (names []string, quals map[string][]string, sels []string) {
	quals = map[string][]string{}
	local := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Field:
			for _, id := range n.Names {
				local[id.Name] = true
			}
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, l := range n.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						local[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range n.Names {
				local[id.Name] = true
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{n.Key, n.Value} {
				if id, ok := e.(*ast.Ident); ok && n.Tok == token.DEFINE {
					local[id.Name] = true
				}
			}
		}
		return true
	})
	selIdents := map[*ast.Ident]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			selIdents[n.Sel] = true
			if id, ok := n.X.(*ast.Ident); ok {
				selIdents[id] = true // a package name, or a local
				quals[id.Name] = append(quals[id.Name], n.Sel.Name)
				if !local[id.Name] {
					names = append(names, id.Name)
				}
			}
			sels = append(sels, n.Sel.Name)
		case *ast.KeyValueExpr:
			if id, ok := n.Key.(*ast.Ident); ok {
				selIdents[id] = true // a field name
			}
		case *ast.FuncDecl:
			selIdents[n.Name] = true // the declared name
		case *ast.TypeSpec:
			selIdents[n.Name] = true
		case *ast.Ident:
			if !local[n.Name] && !selIdents[n] {
				names = append(names, n.Name)
			}
		}
		return true
	})
	return names, quals, sels
}
