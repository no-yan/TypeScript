package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// The reach (E3; 7d-2 design remainder 2.2, 7d-2a instructions 3.2): from the
// roots, every function, method, type, variable and constant that a reached
// declaration names (TypesInfo.Uses, closures and signatures included), in the
// packages of the table. A function named as a value is reached where it is
// named. An interface that reached code names makes the methods of every
// reached named type that implements it reached (a class hierarchy analysis
// closed over the packages of the table): an assignment of the type to the
// interface needs them all to compile. A package that is not translated is
// not followed.

type reacher struct {
	w       *world
	seen    map[string]bool   // pkgPath "." key
	from    map[string]string // the declaration that reached it
	queue   []*declInfo
	ifaces  map[*types.Named]bool
	named   map[*types.Named]bool
	changed map[string]bool // the changed declarations of the modeChanged packages
	roots   []string
}

// reachResult is what the translation outputs.
type reachResult struct {
	// output: the declarations to translate (pkgPath "." key). A P2
	// declaration is reached but not output.
	output map[string]bool
	from   map[string]string
	roots  []string
}

// checkerRoots are the roots of 7d design 2.7, the entries of the unit
// equivalences of 7d-2a (6.2, 6.3), what the type and symbol walker of
// 7d-2b-1 calls (4), and what the harness of 7d-2b-2 checks the properties
// of (6, CompareSymbols).
var checkerRoots = []string{
	"checker.NewChecker",
	"checker.Checker.GetDiagnostics",
	"checker.Checker.GetSuggestionDiagnostics",
	"checker.Checker.GetGlobalDiagnostics",
	"checker.Checker.GetTypeAtLocation",
	"checker.Checker.GetSymbolAtLocation",
	"checker.Checker.TypeToString",
	"checker.NewNodeBuilder",
	"checker.IsTypeAny",
	"printer.NewPrinter",
	"printer.Printer.Write",
	"binder.NameResolver.Resolve",
	"checker.Checker.SymbolToStringEx",
	"checker.IntrinsicType.IntrinsicName",
	"printer.EmitContext.Reset",
	"checker.Checker.CompareSymbols",
}

func computeReach(w *world) (*reachResult, error) {
	r := &reacher{w: w, seen: map[string]bool{}, from: map[string]string{}, ifaces: map[*types.Named]bool{}, named: map[*types.Named]bool{}, changed: map[string]bool{}}
	// The named types that the output changes: every type of a full or
	// whole-file package, and the changed types of the modeChanged packages
	// (selectChanged fills r.changed).
	w.u.changedType = func(n *types.Named) bool {
		o := w.outs[pkgPathOf(n.Obj())]
		if o == nil {
			return false
		}
		switch o.mode {
		case modeFull:
			return true
		case modeAST:
			key := o.srcPath + "." + n.Obj().Name()
			return w.u.astTypeInStore(n) || r.seen[key]
		case modeFile:
			d := w.decl(o.srcPath, n.Obj().Name())
			return d != nil && o.isFileLevel(d.fileName)
		}
		return r.changed[o.srcPath+"."+n.Obj().Name()]
	}
	add := func(pkgPath, key, why string) error {
		d := w.decl(pkgPath, key)
		if d == nil {
			return fmt.Errorf("root %s.%s: no such declaration", pkgPath, key)
		}
		r.add(d, why)
		return nil
	}
	for _, root := range checkerRoots {
		i := strings.Index(root, ".")
		if err := add(internal+root[:i], root[i+1:], "root"); err != nil {
			return nil, err
		}
	}
	astRoots, err := r.astRoots()
	if err != nil {
		return nil, err
	}
	for _, name := range astRoots {
		d := w.decl(astPath, name)
		if d == nil {
			return nil, fmt.Errorf("root ast.%s: no such function", name)
		}
		if fn, ok := d.objs[0].(*types.Func); ok && (keptInAST(w.u, fn) || ruleRewritten[name]) {
			continue
		}
		r.add(d, "root")
	}
	// What the hand-written files of the output packages name.
	for _, o := range sortedOuts(w) {
		for _, m := range o.handRefs {
			r.followManual(&declInfo{w: w, key: "hand:" + m.key, out: o}, m)
		}
	}
	// What the store-only P2 declarations name (7d-2b-1 instructions 3.2):
	// they replace no Pointer declaration, so no reached declaration leads
	// to them, and they are followed as the hand-written files are.
	for _, o := range sortedOuts(w) {
		var keys []string
		for key, m := range o.manual {
			if m.storeOnly {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			r.followManual(&declInfo{w: w, key: "store-only:" + key, out: o}, o.manual[key])
		}
	}
	// init functions and the files output whole.
	for _, d := range w.decls {
		if !r.eligible(d) {
			continue
		}
		if strings.HasPrefix(d.key, "init#") {
			r.add(d, "init")
		}
		if d.out.isFileLevel(d.fileName) {
			r.add(d, "file "+d.fileName)
		}
	}
	for {
		for len(r.queue) > 0 {
			d := r.queue[0]
			r.queue = r.queue[1:]
			r.process(d)
		}
		if !r.closeInterfaces() {
			break
		}
	}
	res := &reachResult{output: map[string]bool{}, from: r.from, roots: r.roots}
	for key := range r.seen {
		res.output[key] = true
	}
	r.selectChanged(res)
	// The type declarations of the full packages are output whole, and the
	// constants of a group that is output in part (iota).
	for key, d := range w.decls {
		if d.out.mode == modeFull {
			if _, ok := d.node.(*ast.TypeSpec); ok {
				res.output[key] = true
			}
		}
	}
	for key, d := range w.decls {
		if d.gen == nil || d.gen.Tok != token.CONST || res.output[key] {
			continue
		}
		for _, spec := range d.gen.Specs {
			for _, n := range spec.(*ast.ValueSpec).Names {
				if res.output[d.out.srcPath+"."+n.Name] {
					res.output[key] = true
				}
			}
		}
	}
	// A P2 declaration replaces the output.
	for key, d := range w.decls {
		if _, ok := d.out.manual[d.key]; ok && res.output[key] {
			delete(res.output, key)
		}
	}
	if os.Getenv("STORETRANSLATE_REACH") != "" {
		var keys []string
		for k := range res.output {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(os.Stderr, "reach %s <- %s\n", strings.TrimPrefix(k, internal), strings.TrimPrefix(r.from[k], internal))
		}
	}
	return res, nil
}

func (r *reacher) add(d *declInfo, why string) {
	k := d.out.srcPath + "." + d.key
	if r.seen[k] {
		return
	}
	if why == "root" {
		r.roots = append(r.roots, k)
	}
	r.seen[k] = true
	r.from[k] = why
	r.queue = append(r.queue, d)
}

// visit reaches the declaration of obj, named by d.
func (r *reacher) visit(obj types.Object, d *declInfo) {
	if tn, ok := obj.(*types.TypeName); ok {
		r.noteType(tn.Type())
	}
	if fn, ok := obj.(*types.Func); ok {
		if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
			if named, ok := types.Unalias(recv.Type()).(*types.Named); ok && types.IsInterface(named) {
				r.ifaces[named.Origin()] = true
				obj = named.Origin().Obj()
				r.noteType(named)
			}
		}
	}
	o, key := r.w.objKey(obj)
	if o == nil {
		return
	}
	target := r.w.decl(o.srcPath, key)
	if target == nil {
		return
	}
	switch o.mode {
	case modeAST:
		if target.out.isFileLevel(target.fileName) {
			break
		}
		if tn, ok := obj.(*types.TypeName); ok && r.astTypeTranslated(tn) {
			// A type of ast that holds nodes (AllAccessorDeclarations): the
			// Store needs its own, with the utilities.
			break
		}
		fn, ok := obj.(*types.Func)
		if !ok || fn.Type().(*types.Signature).Recv() != nil {
			// The methods of the node types and the shared types of ast are
			// the rules'.
			return
		}
		if keptInAST(r.w.u, fn) || ruleRewritten[fn.Name()] {
			return
		}
	case modeFile:
		if !target.out.isFileLevel(target.fileName) {
			// Another function of the package: the Store's hand-written one,
			// or the Pointer one.
			return
		}
	}
	r.add(target, d.out.srcPath+"."+d.key)
}

func (r *reacher) noteType(t types.Type) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return
	}
	named = named.Origin()
	if types.IsInterface(named) {
		r.ifaces[named] = true
		return
	}
	if o, key := r.w.objKey(named.Obj()); o != nil {
		if d := r.w.decl(o.srcPath, key); d != nil && r.eligible(d) {
			r.named[named] = true
		}
	}
}

// eligible: a declaration that its package's mode lets the output have. In
// ast and the whole-file packages, only the files output whole.
func (r *reacher) eligible(d *declInfo) bool {
	switch d.out.mode {
	case modeFile:
		return d.out.isFileLevel(d.fileName)
	case modeAST:
		if d.out.isFileLevel(d.fileName) {
			return true
		}
		if ts, ok := d.node.(*ast.TypeSpec); ok {
			tn, _ := d.out.pkg.TypesInfo.Defs[ts.Name].(*types.TypeName)
			return tn != nil && r.astTypeTranslated(tn)
		}
		// The package-level functions of ast go to the utilities.
		fd, ok := d.node.(*ast.FuncDecl)
		return ok && fd.Recv == nil && !strings.HasPrefix(d.key, "init#")
	}
	return true
}

func pkgPathOf(obj types.Object) string {
	if obj.Pkg() == nil {
		return ""
	}
	return obj.Pkg().Path()
}

// process follows what d names: the Pointer declaration, or, for a P2
// declaration, the names of its Store version.
func (r *reacher) process(d *declInfo) {
	o := d.out
	if m, ok := o.manual[d.key]; ok {
		r.followManual(d, m)
	} else {
		r.inspect(d, d.node)
	}
	if m, ok := o.manual[d.key+"List"]; ok {
		r.followManual(d, m)
	}
}

func (r *reacher) inspect(d *declInfo, node ast.Node) {
	info := d.out.pkg.TypesInfo
	ast.Inspect(node, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if obj := info.Uses[id]; obj != nil {
			r.visit(obj, d)
		}
		if obj := info.Defs[id]; obj != nil {
			if v, ok := obj.(*types.Var); ok {
				r.noteType(v.Type())
			}
		}
		return true
	})
	// The types of the expressions: a value of an interface type that the
	// code never names still needs the methods of what is assigned to it.
	ast.Inspect(node, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			if t := info.TypeOf(e); t != nil {
				r.noteType(t)
				if p, ok := t.(*types.Pointer); ok {
					r.noteType(p.Elem())
				}
			}
		}
		return true
	})
}

// followManual reaches what a P2 declaration names: its identifiers in the
// source package, its selectors as the methods of that name in the source
// package, and its qualified names in the package they translate.
func (r *reacher) followManual(d *declInfo, m *manualDecl) {
	src := d.out.pkg.Types
	for _, name := range m.names {
		if d.out.hand[name] {
			// The output package's own name, not the source package's.
			continue
		}
		if obj := src.Scope().Lookup(name); obj != nil {
			r.visit(obj, d)
		}
	}
	for _, sel := range m.sels {
		for _, md := range r.methodsNamed(d.out, sel) {
			if r.eligible(md) {
				r.add(md, d.out.srcPath+"."+d.key)
			}
		}
	}
	for qual, names := range m.quals {
		for _, o := range r.w.outs {
			if o.name != qual {
				continue
			}
			for _, name := range names {
				if obj := o.pkg.Types.Scope().Lookup(name); obj != nil {
					r.visit(obj, d)
				}
			}
		}
	}
}

func (r *reacher) methodsNamed(o *outPkg, name string) []*declInfo {
	var out []*declInfo
	for _, d := range r.w.decls {
		if d.out == o {
			if i := strings.Index(d.key, "."); i >= 0 && d.key[i+1:] == name {
				out = append(out, d)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// closeInterfaces reaches the methods of every reached named type that
// implements a reached interface. It reports whether it reached anything.
func (r *reacher) closeInterfaces() bool {
	before := len(r.seen)
	var ifaces []*types.Named
	for i := range r.ifaces {
		ifaces = append(ifaces, i)
	}
	var named []*types.Named
	for t := range r.named {
		named = append(named, t)
	}
	for _, i := range ifaces {
		iface, ok := i.Underlying().(*types.Interface)
		if !ok || iface.NumMethods() == 0 || i.TypeParams().Len() > 0 {
			continue
		}
		for _, t := range named {
			if t.TypeParams().Len() > 0 {
				continue
			}
			var recv types.Type
			switch {
			case types.Implements(t, iface):
				recv = t
			case types.Implements(types.NewPointer(t), iface):
				recv = types.NewPointer(t)
			default:
				continue
			}
			ms := types.NewMethodSet(recv)
			for k := range iface.NumMethods() {
				sel := ms.Lookup(iface.Method(k).Pkg(), iface.Method(k).Name())
				if sel == nil {
					continue
				}
				o, key := r.w.objKey(sel.Obj())
				if o == nil {
					continue
				}
				if d := r.w.decl(o.srcPath, key); d != nil && r.eligible(d) {
					r.add(d, "interface "+i.Obj().Pkg().Name()+"."+i.Obj().Name()+" of "+t.Obj().Name())
				}
			}
		}
	}
	return len(r.seen) > before
}

// selectChanged drops from the output the reached declarations of the
// modeChanged packages that the translation does not change (3.1): their
// callers call the Pointer package. A changed declaration keeps the
// unexported declarations of its package that it names.
func (r *reacher) selectChanged(res *reachResult) {
	changed := r.changed
	isChangedPkg := func(o *outPkg) bool { return o.mode == modeChanged }
	mark := func(k, from string) bool {
		if changed[k] || r.w.decls[k] == nil {
			return false
		}
		changed[k] = true
		if _, ok := res.output[k]; !ok {
			res.from[k] = from
		}
		return true
	}
	owners := map[*outPkg]map[*ast.Ident]*types.Named{}
	for {
		progress := false
		// The declarations whose types change.
		for key := range res.output {
			d := r.w.decls[key]
			if d == nil || !isChangedPkg(d.out) || changed[key] {
				continue
			}
			if r.declChanges(d) {
				changed[key] = true
				progress = true
			}
		}
		// A method is declared with its receiver type, and a changed type
		// takes its reached methods.
		for key := range changed {
			d := r.w.decls[key]
			if d == nil {
				continue
			}
			if i := strings.Index(d.key, "."); i >= 0 {
				progress = mark(d.out.srcPath+"."+d.key[:i], key) || progress
			} else if _, ok := d.node.(*ast.TypeSpec); ok {
				for k := range res.output {
					if strings.HasPrefix(k, d.out.srcPath+"."+d.key+".") {
						progress = mark(k, key) || progress
					}
				}
			}
		}
		// The unexported declarations that a changed one names, and the
		// types whose unexported fields or methods it reads: the output
		// package cannot reach those in the Pointer package. A P2
		// declaration's Pointer body is not the output's.
		for key := range changed {
			d := r.w.decls[key]
			if d == nil {
				continue
			}
			if _, ok := d.out.manual[d.key]; ok {
				continue
			}
			info := d.out.pkg.TypesInfo
			own := owners[d.out]
			if own == nil {
				own = memberOwners(d.out.pkg)
				owners[d.out] = own
			}
			ast.Inspect(d.node, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				obj := info.Uses[id]
				if obj == nil || obj.Exported() {
					return true
				}
				if v, ok := obj.(*types.Var); ok && v.IsField() || isMethod(obj) {
					if owner := own[id]; owner != nil && pkgPathOf(owner.Obj()) == d.out.srcPath {
						progress = mark(d.out.srcPath+"."+owner.Obj().Name(), key) || progress
					}
					return true
				}
				o, k := r.w.objKey(obj)
				if o != d.out {
					return true
				}
				progress = mark(o.srcPath+"."+k, key) || progress
				return true
			})
		}
		if !progress {
			break
		}
	}
	for key := range res.output {
		d := r.w.decls[key]
		if d != nil && isChangedPkg(d.out) && !changed[key] {
			delete(res.output, key)
		}
	}
	for key := range changed {
		res.output[key] = true
	}
}

func isMethod(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	return ok && fn.Type().(*types.Signature).Recv() != nil
}

// memberOwners maps the identifier of each field or method selection, and of
// each key of a struct literal, to the named type that declares the member.
func memberOwners(pkg *packages.Package) map[*ast.Ident]*types.Named {
	out := map[*ast.Ident]*types.Named{}
	named := func(t types.Type) *types.Named {
		if p, ok := types.Unalias(t).(*types.Pointer); ok {
			t = p.Elem()
		}
		n, _ := types.Unalias(t).(*types.Named)
		if n != nil {
			n = n.Origin()
		}
		return n
	}
	for sel, s := range pkg.TypesInfo.Selections {
		// The type that declares the member: the receiver, through the
		// embedded fields of the path.
		t := s.Recv()
		idx := s.Index()
		for _, i := range idx[:len(idx)-1] {
			st, ok := underStruct(t)
			if !ok {
				break
			}
			t = st.Field(i).Type()
		}
		out[sel.Sel] = named(t)
	}
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			if cl, ok := n.(*ast.CompositeLit); ok {
				t := named(pkg.TypesInfo.TypeOf(cl))
				for _, el := range cl.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok && t != nil {
							if _, isStruct := t.Underlying().(*types.Struct); isStruct {
								out[id] = t
							}
						}
					}
				}
			}
			return true
		})
	}
	return out
}

func underStruct(t types.Type) (*types.Struct, bool) {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		t = p.Elem()
	}
	s, ok := t.Underlying().(*types.Struct)
	return s, ok
}

// declChanges: the declaration's types, or the type of any expression in it,
// mention a type that the translation changes.
func (r *reacher) declChanges(d *declInfo) bool {
	u := r.w.u
	info := d.out.pkg.TypesInfo
	if _, ok := d.node.(*ast.TypeSpec); ok {
		// A type holds what others make: a type that holds an ast type of the
		// whole files only (module.ResolvedModule's []*ast.Diagnostic, made by
		// the resolver) is the Pointer's, as the parse diagnostics of a
		// store.File are (R11). Nodes, and the types of the translated
		// packages, change it.
		return u.mentions(d.objs[0].Type().Underlying(), func(n *types.Named) bool {
			return n.Obj().Pkg() != u.ast && u.changedType != nil && u.changedType(n.Origin())
		})
	}
	for _, obj := range d.objs {
		if obj != nil && u.changes(obj.Type()) {
			return true
		}
	}
	found := false
	ast.Inspect(d.node, func(n ast.Node) bool {
		if found {
			return false
		}
		if e, ok := n.(ast.Expr); ok {
			if t := info.TypeOf(e); t != nil && u.changes(t) {
				found = true
			}
		}
		return !found
	})
	return found
}

// astRoots are the roots of the utilities (U2): the functions of roots.txt
// (store/utilities.go of 7c, fixed so that the roots do not depend on the
// output), and the ast functions that storebinder and storeparser call through
// store. The other outputs reach the rest.
func (r *reacher) astRoots() ([]string, error) {
	w := r.w
	roots := map[string]bool{}
	previous, err := os.ReadFile(w.root + "/tools/cmd/storetranslate/roots.txt")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(previous), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if w.decl(astPath, line) == nil {
			return nil, fmt.Errorf("roots.txt: %s is not a function of package ast", line)
		}
		roots[line] = true
	}
	callRe := regexp.MustCompile(`\bstore\.([A-Z]\w*)\(`)
	for _, dir := range []string{"tsc/internal/storebinder", "tsc/internal/storeparser"} {
		files, _ := globGo(w.root + "/" + dir)
		for _, file := range files {
			text, err := os.ReadFile(file)
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(string(text), generatedHeader) {
				continue
			}
			for _, m := range callRe.FindAllStringSubmatch(string(text), -1) {
				if d := w.decl(astPath, m[1]); d != nil {
					if _, ok := d.node.(*ast.FuncDecl); ok {
						roots[m[1]] = true
					}
				}
			}
		}
	}
	store := w.outs[astPath]
	var out []string
	for name := range roots {
		if store.hand[name] {
			return nil, fmt.Errorf("%s is a root and is written by hand outside the P2 files", name)
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// globGo lists the non-test Go files of a directory.
func globGo(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			out = append(out, f)
		}
	}
	return out, nil
}

// ruleRewritten are the ast functions whose calls a rule rewrites as a
// whole: they are never called in the output.
var ruleRewritten = map[string]bool{"GetSourceFileOfNode": true, "GetNodeId": true}

// astTypeTranslated: a type of package ast outside the whole files, not a
// node type, whose fields hold nodes. The utilities that return it need the
// Store's.
func (r *reacher) astTypeTranslated(tn *types.TypeName) bool {
	if tn.IsAlias() {
		return false
	}
	s, ok := tn.Type().Underlying().(*types.Struct)
	if !ok || r.w.u.isNodeStruct(tn.Type()) || r.w.outs[astPath].hand[tn.Name()] {
		// Package store has its own (PatternAmbientModule).
		return false
	}
	switch tn.Name() {
	case "NodeList", "ModifierList", "SourceFile", "Symbol", "FlowNode", "FlowList", "NodeFactory":
		return false
	}
	return r.w.u.containsNode(s)
}
