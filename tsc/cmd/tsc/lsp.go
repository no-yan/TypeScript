package main

import (
	"fmt"
	"os"
)

// --lsp is out of the PR-7 compile-path gate. It pulled internal/ls.
func runLSP(args []string) int {
	_ = args
	fmt.Fprintln(os.Stderr, "--lsp is disabled on this cutover tree")
	return 2
}
