package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// The packages that the translation reads and writes
// (store-checker-7d2a-implementation-instructions-20260929.md 3.1): each
// Pointer package has one output package, and what goes there depends on its
// mode.

type pkgMode int

const (
	// modeAST: package ast into package store. The reached package-level
	// functions go to utilities.go (U2); the files of fileLevel go whole, file
	// by file. Everything else of ast is a rule's (the node types, the
	// methods of Node) or is shared (Kind, NodeFlags, ...).
	modeAST pkgMode = iota
	// modeFull: checker and printer. Every type declaration is output; the
	// functions, methods, variables and constants that the reach finds.
	modeFull
	// modeFile: the files of fileLevel are output whole; nothing else.
	modeFile
	// modeChanged: the reached declarations whose type, signature or body
	// mentions a type that the translation changes; the others are called in
	// the Pointer package, which the Store shares.
	modeChanged
)

const (
	tscModule = "github.com/microsoft/TypeScript/tsc"
	internal  = tscModule + "/internal/"
)

// pkgSpec is a row of the table of 3.1.
type pkgSpec struct {
	src       string // the Pointer package, relative to internal/
	out       string // the Store package, relative to internal/
	mode      pkgMode
	fileLevel []string // modeAST and modeFile: the files output whole; nil = every non-test file
	// shared are the types (with their methods) and functions that the Store
	// calls in the Pointer package: the reach stops at them, whatever their
	// bodies name (store-cli-design-20261004.md 4.1).
	shared []string
}

var pkgTable = []pkgSpec{
	{src: "ast", out: "ast/store", mode: modeAST, fileLevel: []string{"visitor.go", "diagnostic.go"}},
	{src: "checker", out: "storechecker", mode: modeFull},
	{src: "printer", out: "storeprinter", mode: modeFull},
	{src: "binder", out: "storebinder", mode: modeFile, fileLevel: []string{"nameresolver.go", "referenceresolver.go"}},
	{src: "evaluator", out: "storeevaluator", mode: modeFile},
	{src: "scanner", out: "storescanner", mode: modeChanged},
	{src: "nodebuilder", out: "storenodebuilder", mode: modeChanged},
	{src: "pseudochecker", out: "storepseudochecker", mode: modeChanged},
	{src: "modulespecifiers", out: "storemodulespecifiers", mode: modeChanged},
	// module.GetResolutionDiagnostic takes the importing *ast.SourceFile. The
	// resolver reads no node: the Store loader (compiler) shares it.
	{src: "module", out: "storemodule", mode: modeChanged, shared: []string{"Resolver", "NewResolver"}},
	{src: "compiler", out: "storecompiler", mode: modeChanged},
}

// outPkg is a translated package: its Pointer source and what its output
// package has.
type outPkg struct {
	pkgSpec
	srcPath string // import path of the Pointer package
	path    string // import path of the output
	name    string // package name of the output
	dir     string // absolute directory of the output
	pkg     *packages.Package
	// manual are the P2 declarations of the *_manual.go files: functions by
	// name, methods by Type.name, types by name.
	manual map[string]*manualDecl
	// hand are the package-level names of the other hand-written files of the
	// output package (package store's own API, storebinder's binder): a
	// reference to a name here is a reference to the Store version.
	hand map[string]bool
	// handRefs are what the hand-written files name: the reach follows them
	// as it follows a P2 declaration, so that what they call is output.
	handRefs []*manualDecl
	// generated are the files of the output directory that the translator
	// wrote before: they are read as empty (phase 1) or as stubs (phase 2),
	// and removed when not written again.
	generated map[string]bool
}

// isShared reports whether key (a name, or Type.name) is shared or a method
// of a shared type.
func (o *outPkg) isShared(key string) bool {
	if i := strings.Index(key, "."); i >= 0 {
		key = key[:i]
	}
	return slices.Contains(o.shared, key)
}

func (o *outPkg) isFileLevel(file string) bool {
	if o.mode != modeAST && o.mode != modeFile {
		return false
	}
	if o.fileLevel == nil {
		return true
	}
	for _, f := range o.fileLevel {
		if f == file {
			return true
		}
	}
	return false
}

// newOutPkgs makes the table's packages, with what the output directories
// already hold: the P2 files, the hand-written names and the generated files.
func newOutPkgs(root string) (map[string]*outPkg, error) {
	out := map[string]*outPkg{}
	for _, spec := range pkgTable {
		o := &outPkg{
			pkgSpec:   spec,
			srcPath:   internal + spec.src,
			path:      internal + spec.out,
			name:      filepath.Base(spec.out),
			dir:       filepath.Join(root, "tsc/internal", spec.out),
			manual:    map[string]*manualDecl{},
			hand:      map[string]bool{},
			generated: map[string]bool{},
		}
		if err := o.scan(); err != nil {
			return nil, err
		}
		out[o.srcPath] = o
	}
	return out, nil
}

// scan reads the output directory: which files are generated, which are P2
// and which are hand-written, and the names the hand-written ones declare.
func (o *outPkg) scan() error {
	entries, err := os.ReadDir(o.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(o.dir, name)
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(string(text), generatedHeader) {
			o.generated[name] = true
			continue
		}
		if isManualFile(name) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, text, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, d := range file.Decls {
			for _, name := range declNames(d) {
				o.hand[name] = true
			}
			m := &manualDecl{key: name}
			m.names, m.quals, m.sels = bodyRefs(d)
			o.handRefs = append(o.handRefs, m)
		}
	}
	return nil
}

func isManualFile(name string) bool { return strings.HasSuffix(name, "_manual.go") }

// declNames are the names a declaration declares at package level: a
// function by name, a method by Type.name.
func declNames(d ast.Decl) []string {
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil {
			return []string{recvTypeName(d.Recv.List[0].Type) + "." + d.Name.Name}
		}
		return []string{d.Name.Name}
	case *ast.GenDecl:
		var out []string
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				out = append(out, s.Name.Name)
			case *ast.ValueSpec:
				for _, n := range s.Names {
					out = append(out, n.Name)
				}
			}
		}
		return out
	}
	return nil
}

// recvTypeName is the type name of a receiver: T of T, *T, T[P] and *T[P].
func recvTypeName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}
