package main

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// translateFile translates every declaration of one source file into a file
// of the target package. It is the whole-file mode: the binder.go trial and
// the tests of the rules use it; the utilities use the reach instead.
func translateFile(p *program, u *universe, pkg *packages.Package, file *ast.File, out *target) ([]byte, []problem, error) {
	keys, keyProblems := keyFields(u, p.fset, pkg.Types, pkg.TypesInfo, pkg.Syntax)
	u.keyFields = keys
	defer func() { u.keyFields = nil }()
	tr := &translator{universe: u, fset: p.fset, src: pkg.Types, info: pkg.TypesInfo, out: out}
	problems := append([]problem(nil), keyProblems...)
	var decls []ast.Decl
	var comments []*ast.CommentGroup
	for _, d := range file.Decls {
		f := tr.newFn(d)
		switch d := d.(type) {
		case *ast.FuncDecl:
			decls = append(decls, f.funcDecl(d))
		case *ast.GenDecl:
			if d.Tok.String() == "import" {
				continue
			}
			decls = append(decls, f.genDecl(d))
		}
		problems = append(problems, f.problems...)
		comments = append(comments, commentsIn(p, d)...)
	}
	header := fmt.Sprintf("%s\n\n// Translated from %s by tools/cmd/storetranslate.\n", generatedHeader, filepath.Base(p.fset.Position(file.Pos()).Filename))
	src, err := render(p.fset, out.name, out.imports, decls, comments, header)
	return src, problems, err
}

// binderTrial translates internal/binder/binder.go as storebinder would have
// it, and compares it with the 7c verbatim version, function by function and
// hunk by hunk (7d-0 instructions 5.2). The output is a report; storebinder
// stays hand-written.
func binderTrial(p *program, u *universe, dir, verbatimPath string, storeFuncs map[string]bool) error {
	var file *ast.File
	for _, f := range p.binder.Syntax {
		if filepath.Base(p.fset.Position(f.Pos()).Filename) == "binder.go" {
			file = f
		}
	}
	if file == nil {
		return fmt.Errorf("no binder.go in package binder")
	}
	out := &target{name: "storebinder", funcs: storeFuncs, local: true, imports: map[string]string{}}
	src, problems, err := translateFile(p, u, p.binder, file, out)
	if err != nil {
		// A rule that makes invalid Go is a bug of the translator; keep the
		// text for the report.
		fmt.Fprintln(os.Stderr, "storetranslate: the binder trial does not format:", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "binder-trial.go.txt"), src, 0o644); err != nil {
		return err
	}
	var pb strings.Builder
	for _, pr := range problems {
		fmt.Fprintf(&pb, "%s: %s\n", relPos(p.root, pr.pos.String()), pr.msg)
	}
	if err := os.WriteFile(filepath.Join(dir, "binder-trial-problems.txt"), []byte(pb.String()), 0o644); err != nil {
		return err
	}
	pointer, err := os.ReadFile(filepath.Join(p.root, "tsc/internal/binder/binder.go"))
	if err != nil {
		return err
	}
	var verbatim []byte
	if verbatimPath != "" {
		verbatim, err = os.ReadFile(verbatimPath)
	} else {
		var diff []byte
		diff, err = os.ReadFile(filepath.Join(p.root, "tsc/internal/ast/docs/_store-binder-results/binder-verbatim.diff"))
		if err == nil {
			verbatim, err = applyUnifiedDiff(pointer, diff)
		}
	}
	if err != nil {
		return fmt.Errorf("the 7c verbatim binder: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "binder-verbatim.go.txt"), verbatim, 0o644); err != nil {
		return err
	}
	diff, err := os.ReadFile(filepath.Join(p.root, "tsc/internal/ast/docs/_store-binder-results/binder-verbatim.diff"))
	if err != nil {
		return err
	}
	report, err := compareTrial(src, verbatim, diff, problems)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "binder-trial-compare.txt"), []byte(report), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote the binder trial into %s: %d problems\n%s", dir, len(problems), firstLines(report, 16))
	return nil
}

func relPos(root, pos string) string {
	return strings.TrimPrefix(pos, root+"/")
}

func firstLines(s string, n int) string {
	lines := strings.SplitAfter(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "")
}
