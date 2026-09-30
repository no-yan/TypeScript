// Command storetranslate translates the Pointer AST code that reads nodes
// into the Store AST: the method is
// tsc/internal/ast/docs/store-checker-design-20260923.md 2.1 ("移植の方法"),
// the rules are store-checker-7d0-implementation-instructions-20260924.md 5
// and store-checker-7d2a-implementation-instructions-20260929.md 3. The rules
// are only those that the types decide: go/types on the Pointer code and on
// the Store packages. A declaration the rules cannot translate is written by
// hand in a *_manual.go file of its output package (P2), with the hash of the
// Pointer declaration it replaces; a change of that declaration stops the
// translation.
//
// Usage, from the tools module:
//
//	go run ./cmd/storetranslate                 # writes every output (pkgs.go)
//	go run ./cmd/storetranslate -hash checker.Checker.f   # the hash line of a P2 declaration
//	go run ./cmd/storetranslate -listdraft checker.Checker.f  # the draft of a List version
//	go run ./cmd/storetranslate -binder DIR     # also the 7d-0 binder.go trial into DIR
//
// What each output package gets (pkgs.go): package store the ast utilities
// that the roots reach (U2) and ast/visitor.go and ast/diagnostic.go whole;
// storechecker and storeprinter every type and the reached functions and
// methods; storebinder binder/nameresolver.go and referenceresolver.go;
// storeevaluator the whole evaluator; storescanner, storenodebuilder,
// storepseudochecker, storemodulespecifiers and storemodule the reached
// declarations whose types change. The reach (reach.go) starts from the
// checker's API (7d design 2.7), the entries of the unit equivalences, the
// ast functions that storebinder and storeparser call (roots.txt and their
// sources), and what the P2 and hand-written files name.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	binderOut := flag.String("binder", "", "also translate internal/binder/binder.go and write the trial and its comparison into this directory")
	verbatim := flag.String("verbatim", "", "the 7c verbatim storebinder (binder-verbatim.diff applied to internal/binder/binder.go); default: made from the diff")
	hash := flag.String("hash", "", "print the pointer-hash line of these declarations (comma-separated; pkg.Name or pkg.Type.Name, ast when pkg is omitted) for a P2 declaration, and exit")
	listdraft := flag.String("listdraft", "", "print the draft of the List version (<F>List) of these functions (as -hash), for a P2 file, and exit")
	flag.Parse()
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "storetranslate:", err)
		os.Exit(1)
	}
	if *hash != "" {
		if err := printHashes(root, *hash); err != nil {
			fmt.Fprintln(os.Stderr, "storetranslate:", err)
			os.Exit(1)
		}
		return
	}
	if *listdraft != "" {
		if err := listDraft(root, *listdraft); err != nil {
			fmt.Fprintln(os.Stderr, "storetranslate:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(root, *binderOut, *verbatim); err != nil {
		fmt.Fprintln(os.Stderr, "storetranslate:", err)
		os.Exit(1)
	}
}

// repoRoot is the directory that holds tsc and tools: the parent of the
// working directory (the tools module) or the working directory itself.
func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for _, dir := range []string{wd, filepath.Dir(wd)} {
		if st, err := os.Stat(filepath.Join(dir, "tsc", "go.mod")); err == nil && !st.IsDir() {
			return dir, nil
		}
	}
	return "", fmt.Errorf("run from the tools module or the repository root (no tsc/go.mod from %s)", wd)
}
