// Command storetranslate translates the Pointer AST code that reads nodes
// into the Store AST (TODO 7d-0; the method is
// tsc/internal/ast/docs/store-checker-design-20260923.md 2.1, "移植の方法",
// and the rules are store-checker-7d0-implementation-instructions-20260924.md
// 5). The rules are only those that the types decide: go/types on the
// Pointer code and the API of package store. A function the rules cannot
// translate is written by hand in store/utilities_manual.go (P2), with the
// hash of the Pointer function it replaces; a change of that function stops
// the translation.
//
// Usage, from the tools module:
//
//	go run ./cmd/storetranslate            # writes tsc/internal/ast/store/utilities.go
//	go run ./cmd/storetranslate -binder DIR # also the binder.go trial into DIR
//
// The utilities (U2): the roots are the functions of the previous
// store/utilities.go and the ast functions that storebinder and storeparser
// call; every package-level function of package ast that they reach (named
// in a body, called or as a value; not followed into other packages) is
// translated into store/utilities.go.
//
// The binder.go trial: internal/binder/binder.go translated in full, compared
// function by function with the hand-written verbatim version of 7c. The
// output is not used by storebinder.
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
	hash := flag.String("hash", "", "print the pointer-hash line of these ast functions (comma-separated) for a P2 function, and exit")
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
