package main

import (
	"fmt"
	"os"
)

// --api is out of the PR-7 compile-path gate. It pulled internal/ls via internal/api.
func runAPI(args []string) int {
	_ = args
	fmt.Fprintln(os.Stderr, "--api is disabled on this cutover tree")
	return 2
}
