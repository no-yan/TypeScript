package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// world is one load of the tsc packages: the Pointer packages of the table,
// package store, and in phase 2 the output packages with their generated
// files replaced by stubs (stubs.go). The output is regenerated from nothing:
// a generated file is never read as it is on disk, so that a stale or broken
// output does not feed the next one.
type world struct {
	root      string
	fset      *token.FileSet
	all       []*packages.Package
	byPath    map[string]*packages.Package
	outs      map[string]*outPkg // by Pointer path
	outByPath map[string]*outPkg // by output path
	ast       *packages.Package
	store     *packages.Package
	u         *universe
	decls     map[string]*declInfo // pkgPath "." key
	// stubbed is true in phase 2: the output packages are loaded, with stubs.
	stubbed bool
}

// declInfo is a package-level declaration of a Pointer package of the table.
type declInfo struct {
	w        *world
	key      string // name, or Type.name for a method
	out      *outPkg
	file     *ast.File
	fileName string
	node     ast.Node // *ast.FuncDecl, *ast.TypeSpec or *ast.ValueSpec
	gen      *ast.GenDecl
	objs     []types.Object
}

func (w *world) decl(pkgPath, key string) *declInfo { return w.decls[pkgPath+"."+key] }

// loadWorld loads the Pointer packages and package store. overlay replaces
// files (absolute paths); with stubs, the output packages are loaded too.
func loadWorld(root string, stubs map[string][]byte, extra ...string) (*world, error) {
	return loadWorldOverlay(root, stubs, nil, extra...)
}

// loadWorldOverlay is loadWorld with more files (absolute paths), and more
// packages (patterns relative to tsc): the tests of the rules load a fixture
// package so.
func loadWorldOverlay(root string, stubs, files map[string][]byte, extra ...string) (*world, error) {
	outs, err := newOutPkgs(root)
	if err != nil {
		return nil, err
	}
	w := &world{root: root, fset: token.NewFileSet(), outs: outs, outByPath: map[string]*outPkg{}, byPath: map[string]*packages.Package{}, decls: map[string]*declInfo{}, stubbed: stubs != nil}
	overlay := map[string][]byte{}
	var patterns []string
	for _, o := range outs {
		w.outByPath[o.path] = o
		patterns = append(patterns, o.srcPath)
		for name := range o.generated {
			overlay[filepath.Join(o.dir, name)] = []byte("package " + o.name + "\n")
		}
		if stubs != nil {
			patterns = append(patterns, o.path)
		}
	}
	patterns = append(patterns, storePath)
	for path, text := range stubs {
		overlay[path] = text
	}
	for path, text := range files {
		overlay[path] = text
	}
	patterns = append(patterns, extra...)
	cfg := &packages.Config{
		Dir:     filepath.Join(root, "tsc"),
		Fset:    w.fset,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Overlay: overlay,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		w.all = append(w.all, pkg)
		w.byPath[pkg.PkgPath] = pkg
	})
	for _, o := range outs {
		o.pkg = w.byPath[o.srcPath]
		if o.pkg == nil {
			return nil, fmt.Errorf("no package %s", o.srcPath)
		}
		if len(o.pkg.Errors) > 0 {
			return nil, fmt.Errorf("%s: %v", o.srcPath, o.pkg.Errors[0])
		}
	}
	w.ast = w.byPath[astPath]
	w.store = w.byPath[storePath]
	w.u = &universe{ast: w.ast.Types, store: w.store.Types, w: w}
	w.indexDecls()
	return w, nil
}

// indexDecls indexes the package-level declarations of the Pointer packages.
func (w *world) indexDecls() {
	for _, o := range w.outs {
		info := o.pkg.TypesInfo
		for _, file := range o.pkg.Syntax {
			fileName := filepath.Base(w.fset.Position(file.Pos()).Filename)
			if strings.HasSuffix(fileName, "_test.go") {
				continue
			}
			for _, d := range file.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					key := declNames(d)[0]
					if d.Name.Name == "init" || d.Name.Name == "_" {
						// init is keyed by its position: a package may have several.
						key = fmt.Sprintf("init#%d", w.fset.Position(d.Pos()).Offset) + "@" + fileName
					}
					w.decls[o.srcPath+"."+key] = &declInfo{w: w, key: key, out: o, file: file, fileName: fileName, node: d, objs: []types.Object{info.Defs[d.Name]}}
				case *ast.GenDecl:
					for _, s := range d.Specs {
						switch s := s.(type) {
						case *ast.TypeSpec:
							w.decls[o.srcPath+"."+s.Name.Name] = &declInfo{w: w, key: s.Name.Name, out: o, file: file, fileName: fileName, node: s, gen: d, objs: []types.Object{info.Defs[s.Name]}}
						case *ast.ValueSpec:
							di := &declInfo{w: w, out: o, file: file, fileName: fileName, node: s, gen: d}
							for _, n := range s.Names {
								if obj := info.Defs[n]; obj != nil {
									di.objs = append(di.objs, obj)
								}
							}
							for _, n := range s.Names {
								// Dropped on purpose: an interface assertion
								// (var _ I = (*T)(nil)) is not a root of the
								// reach, and the Store type has only the methods
								// reached, so the assertion would not compile.
								// Make these roots when emit comes into range.
								if n.Name == "_" {
									continue
								}
								k := *di
								k.key = n.Name
								w.decls[o.srcPath+"."+n.Name] = &k
							}
						}
					}
				}
			}
		}
	}
}

// objKey is the key of a package-level object or method of a Pointer package
// of the table, or "".
func (w *world) objKey(obj types.Object) (*outPkg, string) {
	if obj == nil || obj.Pkg() == nil {
		return nil, ""
	}
	o := w.outs[obj.Pkg().Path()]
	if o == nil {
		return nil, ""
	}
	if fn, ok := obj.(*types.Func); ok {
		fn = fn.Origin()
		sig := fn.Type().(*types.Signature)
		if recv := sig.Recv(); recv != nil {
			t := recv.Type()
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem()
			}
			named, ok := types.Unalias(t).(*types.Named)
			if !ok {
				return nil, ""
			}
			return o, named.Origin().Obj().Name() + "." + fn.Name()
		}
		if fn.Parent() != fn.Pkg().Scope() {
			return nil, ""
		}
		return o, fn.Name()
	}
	if obj.Parent() != obj.Pkg().Scope() {
		return nil, ""
	}
	switch obj.(type) {
	case *types.TypeName, *types.Var, *types.Const:
		return o, obj.Name()
	}
	return nil, ""
}

// outputFile is the absolute path of the output of a source file.
func (o *outPkg) outputFile(fileName string) string {
	return filepath.Join(o.dir, fileName)
}

func mustMkdir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
