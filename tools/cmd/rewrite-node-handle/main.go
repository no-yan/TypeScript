package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("rewrite-node-handle", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dryRun := fs.Bool("dry-run", false, "rewrite in memory and report leftover *ast.Node / *ast.Expression type sites")
	typesOnly := fs.Bool("types-only", false, "rewrite *ast.Node and *ast.FooNode type sites only")
	dir := fs.String("dir", "", "module directory for packages.Load (default: infer from the first path)")
	printFile := fs.String("print", "", "write the rewritten source of this file path to stdout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	patterns := fs.Args()
	if len(patterns) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rewrite-node-handle [flags] <package-dir>...")
		return 2
	}
	loadDir, pkgs, err := resolveLoad(*dir, patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedCompiledGoFiles,
		Dir:  loadDir,
		Fset: token.NewFileSet(),
		Env:  append(os.Environ(), "GOFLAGS=-mod=readonly"),
	}
	loaded, err := packages.Load(cfg, pkgs...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "packages.Load: %v\n", err)
		return 1
	}
	var loadErrs int
	for _, pkg := range loaded {
		for _, e := range pkg.Errors {
			fmt.Fprintf(os.Stderr, "load %s: %v\n", pkg.PkgPath, e)
			loadErrs++
		}
	}
	if loadErrs > 0 {
		return 1
	}

	r := rewriter{typesOnly: *typesOnly, fset: cfg.Fset}
	var leftoverNode, leftoverExpr int
	var rewritten int
	for _, pkg := range loaded {
		if isAstPkg(pkg.Types) {
			continue
		}
		for i, file := range pkg.Syntax {
			path := pkg.CompiledGoFiles[i]
			if !strings.HasSuffix(path, ".go") {
				continue
			}
			out, n, err := r.rewriteFile(pkg, file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
				return 1
			}
			if n == 0 && *printFile == "" {
				leftoverNode += countStar(out, starNode)
				leftoverExpr += countStar(out, starExpr)
				continue
			}
			rewritten += n
			leftoverNode += countStar(out, starNode)
			leftoverExpr += countStar(out, starExpr)
			if *printFile != "" {
				want, err := filepath.Abs(*printFile)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 1
				}
				got, err := filepath.Abs(path)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 1
				}
				if got == want {
					os.Stdout.Write(out)
				}
			}
			if !*dryRun && *printFile == "" {
				if err := os.WriteFile(path, out, 0644); err != nil {
					fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
					return 1
				}
			}
		}
	}
	fmt.Fprintf(os.Stderr, "rewritten=%d remaining *ast.Node=%d remaining *ast.Expression=%d\n", rewritten, leftoverNode, leftoverExpr)
	return 0
}

func resolveLoad(dirFlag string, patterns []string) (string, []string, error) {
	var pkgs []string
	var mod string
	for _, p := range patterns {
		if strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || filepath.IsAbs(p) || !strings.Contains(p, ".") {
			abs, err := filepath.Abs(p)
			if err != nil {
				return "", nil, err
			}
			info, err := os.Stat(abs)
			if err != nil {
				return "", nil, err
			}
			root := abs
			if !info.IsDir() {
				root = filepath.Dir(abs)
			}
			found, err := findGoMod(root)
			if err != nil {
				return "", nil, err
			}
			if mod == "" {
				mod = found
			} else if mod != found {
				return "", nil, fmt.Errorf("patterns span multiple modules: %s and %s", mod, found)
			}
			rel, err := filepath.Rel(found, root)
			if err != nil {
				return "", nil, err
			}
			pkgs = append(pkgs, "./"+filepath.ToSlash(rel))
			continue
		}
		pkgs = append(pkgs, p)
	}
	if dirFlag != "" {
		abs, err := filepath.Abs(dirFlag)
		if err != nil {
			return "", nil, err
		}
		return abs, pkgs, nil
	}
	if mod == "" {
		return "", nil, fmt.Errorf("set --dir when patterns are import paths")
	}
	return mod, pkgs, nil
}

func findGoMod(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

type rewriter struct {
	typesOnly bool
	fset      *token.FileSet
	info      *types.Info
	pkg       *packages.Package
	file      *ast.File
	astName   string
	funcSigs  []*types.Signature
	compLits  []*ast.CompositeLit
	edits     int
}

func (r *rewriter) rewriteFile(pkg *packages.Package, file *ast.File) ([]byte, int, error) {
	r.info = pkg.TypesInfo
	r.pkg = pkg
	r.file = file
	r.astName = astImportName(file)
	r.funcSigs = r.funcSigs[:0]
	r.compLits = r.compLits[:0]
	r.edits = 0
	astutil.Apply(file, r.pre, r.post)
	if r.edits > 0 {
		stripFuncListClosing(file)
	}
	var buf bytes.Buffer
	// Print with the load FileSet so comments keep their original positions.
	// token.NewFileSet() dumps comments between a selector and its Ident
	// (`ast.` + comment + `Handle`) and splits every converted type.
	if err := format.Node(&buf, r.fset, file); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), r.edits, nil
}

func (r *rewriter) pre(c *astutil.Cursor) bool {
	switch n := c.Node().(type) {
	case *ast.FuncDecl:
		if n.Name == nil {
			return true
		}
		if obj, ok := r.info.Defs[n.Name]; ok && obj != nil {
			if sig, ok := obj.Type().(*types.Signature); ok {
				r.funcSigs = append(r.funcSigs, sig)
			}
		}
	case *ast.FuncLit:
		if tv, ok := r.info.Types[n]; ok {
			if sig, ok := tv.Type.(*types.Signature); ok {
				r.funcSigs = append(r.funcSigs, sig)
			}
		}
	case *ast.CompositeLit:
		r.compLits = append(r.compLits, n)
	case *ast.StarExpr:
		if isAstNodePointer(r.info.TypeOf(n)) {
			c.Replace(r.handleIdentAt(n.Pos()))
			r.edits++
			return false
		}
	case *ast.AssignStmt:
		if r.typesOnly {
			return true
		}
		if r.rewriteAssign(c, n) {
			return false
		}
	case *ast.CallExpr:
		if r.typesOnly {
			return true
		}
		if tv, ok := r.info.Types[n.Fun]; ok && tv.IsType() && isAstNodePointer(tv.Type) {
			if len(n.Args) == 1 && isNilIdent(n.Args[0]) {
				c.Replace(r.handleLitAt(n.Pos()))
				r.edits++
				return false
			}
		}
	case *ast.SelectorExpr:
		if r.rewriteTypeName(c, n) {
			return false
		}
		if r.typesOnly {
			return true
		}
		if r.rewriteSelector(c, n) {
			return false
		}
	case *ast.BinaryExpr:
		if r.typesOnly {
			return true
		}
		if r.rewriteNilCompare(c, n) {
			return false
		}
	case *ast.Ident:
		if r.rewriteTypeName(c, n) {
			return false
		}
		if r.typesOnly || n.Name != "nil" {
			return true
		}
		if r.rewriteNilValue(c, n) {
			return false
		}
	}
	return true
}

func (r *rewriter) post(c *astutil.Cursor) bool {
	switch c.Node().(type) {
	case *ast.FuncDecl, *ast.FuncLit:
		if len(r.funcSigs) > 0 {
			r.funcSigs = r.funcSigs[:len(r.funcSigs)-1]
		}
	case *ast.CompositeLit:
		if len(r.compLits) > 0 {
			r.compLits = r.compLits[:len(r.compLits)-1]
		}
	}
	return true
}

var nodeSetters = map[string]string{
	"Flags":  "SetFlags",
	"Parent": "SetParent",
	"Loc":    "SetLoc",
}

func (r *rewriter) rewriteAssign(c *astutil.Cursor, as *ast.AssignStmt) bool {
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return false
	}
	sel, ok := as.Lhs[0].(*ast.SelectorExpr)
	if !ok || skipAsXxx(sel) {
		return false
	}
	if !isAstNodeLike(r.info.TypeOf(sel.X)) {
		return false
	}
	setter, ok := nodeSetters[sel.Sel.Name]
	if !ok {
		return false
	}
	recv := sel.X
	fun := &ast.SelectorExpr{X: recv, Sel: ast.NewIdent(setter)}
	arg := as.Rhs[0]
	if isNilIdent(arg) && setter == "SetParent" {
		arg = r.handleLitAt(arg.Pos())
	}
	if as.Tok != token.ASSIGN {
		op, ok := assignToBinary[as.Tok]
		if !ok {
			return false
		}
		arg = &ast.BinaryExpr{
			X: &ast.CallExpr{
				Fun: &ast.SelectorExpr{X: recv, Sel: ast.NewIdent(sel.Sel.Name)},
			},
			Op: op,
			Y:  as.Rhs[0],
		}
	}
	c.Replace(&ast.ExprStmt{X: &ast.CallExpr{Fun: fun, Args: []ast.Expr{arg}}})
	r.edits++
	return true
}

var assignToBinary = map[token.Token]token.Token{
	token.OR_ASSIGN:  token.OR,
	token.AND_ASSIGN: token.AND,
	token.XOR_ASSIGN: token.XOR,
	token.ADD_ASSIGN: token.ADD,
	token.SUB_ASSIGN: token.SUB,
}

func (r *rewriter) rewriteTypeName(c *astutil.Cursor, n ast.Expr) bool {
	tv, ok := r.info.Types[n]
	if !ok || !tv.IsType() || !isAstNode(tv.Type) {
		return false
	}
	if _, ok := c.Parent().(*ast.StarExpr); ok {
		return false
	}
	c.Replace(r.handleIdentAt(n.Pos()))
	r.edits++
	return true
}

func (r *rewriter) rewriteSelector(c *astutil.Cursor, sel *ast.SelectorExpr) bool {
	if skipAsXxx(sel) {
		return false
	}
	name := sel.Sel.Name
	if name != "Kind" && name != "Flags" && name != "Parent" && name != "Loc" {
		return false
	}
	if !isAstNodeLike(r.info.TypeOf(sel.X)) {
		return false
	}
	seln, ok := r.info.Selections[sel]
	if !ok {
		return false
	}
	switch seln.Kind() {
	case types.FieldVal:
		c.Replace(&ast.CallExpr{Fun: sel})
		r.edits++
		return true
	case types.MethodVal:
		if p, ok := c.Parent().(*ast.CallExpr); ok && p.Fun == sel {
			return false
		}
		c.Replace(&ast.CallExpr{Fun: sel})
		r.edits++
		return true
	}
	return false
}

func (r *rewriter) rewriteNilCompare(c *astutil.Cursor, bin *ast.BinaryExpr) bool {
	if bin.Op != token.EQL && bin.Op != token.NEQ {
		return false
	}
	var node ast.Expr
	switch {
	case isNilIdent(bin.Y) && isAstNodeLike(r.info.TypeOf(bin.X)):
		node = bin.X
	case isNilIdent(bin.X) && isAstNodeLike(r.info.TypeOf(bin.Y)):
		node = bin.Y
	default:
		return false
	}
	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{X: node, Sel: ast.NewIdent("IsNil")},
	}
	var expr ast.Expr = call
	if bin.Op == token.NEQ {
		expr = &ast.UnaryExpr{Op: token.NOT, X: call}
	}
	c.Replace(expr)
	r.edits++
	return true
}

func (r *rewriter) rewriteNilValue(c *astutil.Cursor, n *ast.Ident) bool {
	if expected, ok := r.nilExpectedType(c, n); ok && isAstNodeLike(expected) {
		c.Replace(r.handleLitAt(n.Pos()))
		r.edits++
		return true
	}
	return false
}

func (r *rewriter) nilExpectedType(c *astutil.Cursor, n *ast.Ident) (types.Type, bool) {
	switch p := c.Parent().(type) {
	case *ast.AssignStmt:
		for i, rhs := range p.Rhs {
			if rhs == n && i < len(p.Lhs) {
				return r.info.TypeOf(p.Lhs[i]), true
			}
		}
	case *ast.ValueSpec:
		for _, val := range p.Values {
			if val == n {
				if p.Type != nil {
					return r.info.TypeOf(p.Type), true
				}
				if len(p.Names) > 0 {
					return r.info.TypeOf(p.Names[0]), true
				}
			}
		}
	case *ast.CallExpr:
		for i, arg := range p.Args {
			if arg != n {
				continue
			}
			if tv, ok := r.info.Types[p.Fun]; ok && tv.IsType() {
				return tv.Type, true
			}
			sig := signatureOf(r.info.TypeOf(p.Fun))
			if sig == nil {
				return nil, false
			}
			if sig.Params() == nil {
				return nil, false
			}
			last := sig.Params().Len() - 1
			if sig.Variadic() && i >= last {
				s, ok := sig.Params().At(last).Type().(*types.Slice)
				if !ok {
					return nil, false
				}
				return s.Elem(), true
			}
			if i < sig.Params().Len() {
				return sig.Params().At(i).Type(), true
			}
		}
	case *ast.ReturnStmt:
		for i, res := range p.Results {
			if res != n {
				continue
			}
			if len(r.funcSigs) == 0 {
				return nil, false
			}
			sig := r.funcSigs[len(r.funcSigs)-1]
			if sig.Results() == nil || i >= sig.Results().Len() {
				return nil, false
			}
			return sig.Results().At(i).Type(), true
		}
	case *ast.CompositeLit:
		return compositeElemType(r.info.TypeOf(p)), true
	case *ast.KeyValueExpr:
		if p.Value != n || len(r.compLits) == 0 {
			return nil, false
		}
		t := types.Unalias(r.info.TypeOf(r.compLits[len(r.compLits)-1]))
		if m, ok := t.(*types.Map); ok {
			return m.Elem(), true
		}
		return compositeElemType(t), true
	}
	return nil, false
}

func compositeElemType(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	t = types.Unalias(t)
	switch s := t.(type) {
	case *types.Slice:
		return s.Elem()
	case *types.Array:
		return s.Elem()
	case *types.Pointer:
		return compositeElemType(s.Elem())
	}
	return nil
}

func (r *rewriter) handleIdentAt(pos token.Pos) ast.Expr {
	_ = pos
	if r.astName == "." || r.astName == "" {
		return ast.NewIdent("Handle")
	}
	return &ast.SelectorExpr{
		X:   ast.NewIdent(r.astName),
		Sel: ast.NewIdent("Handle"),
	}
}

func (r *rewriter) handleLitAt(pos token.Pos) ast.Expr {
	return &ast.CompositeLit{Type: r.handleIdentAt(pos)}
}

func stripFuncListClosing(file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		ft, ok := n.(*ast.FuncType)
		if !ok {
			return true
		}
		if ft.Params != nil {
			ft.Params.Closing = token.NoPos
		}
		if ft.Results != nil {
			ft.Results.Closing = token.NoPos
		}
		return true
	})
}

func skipAsXxx(sel *ast.SelectorExpr) bool {
	name := sel.Sel.Name
	return strings.HasPrefix(name, "As") && len(name) > 2 && name[2] >= 'A' && name[2] <= 'Z'
}

func isNilIdent(n ast.Expr) bool {
	id, ok := n.(*ast.Ident)
	return ok && id.Name == "nil"
}

func signatureOf(t types.Type) *types.Signature {
	if t == nil {
		return nil
	}
	t = types.Unalias(t)
	if s, ok := t.(*types.Signature); ok {
		return s
	}
	if n, ok := t.(*types.Named); ok {
		return signatureOf(n.Underlying())
	}
	return nil
}

func isAstPkg(pkg *types.Package) bool {
	return pkg != nil && strings.HasSuffix(pkg.Path(), "/internal/ast")
}

func isAstNodePointer(t types.Type) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	p, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	return isAstNode(p.Elem())
}

func isAstNodeLike(t types.Type) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	if isAstNode(t) {
		return true
	}
	return isAstNodePointer(t)
}

func isAstNode(t types.Type) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	if obj == nil || !isAstPkg(obj.Pkg()) {
		return false
	}
	switch obj.Name() {
	case "Node", "Handle":
		return true
	}
	return false
}

func astImportName(file *ast.File) string {
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !strings.HasSuffix(path, "/internal/ast") {
			continue
		}
		if imp.Name == nil {
			return "ast"
		}
		switch imp.Name.Name {
		case "_":
			return ""
		case ".":
			return "."
		default:
			return imp.Name.Name
		}
	}
	return "ast"
}

var (
	starNode = regexp.MustCompile(`\*ast\.Node\b`)
	starExpr = regexp.MustCompile(`\*ast\.Expression\b`)
)

func countStar(src []byte, re *regexp.Regexp) int {
	return len(re.FindAll(src, -1))
}
