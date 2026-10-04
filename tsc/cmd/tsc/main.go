package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/execute"
	"github.com/microsoft/TypeScript/tsc/internal/execute/tsc"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
	"github.com/microsoft/TypeScript/tsc/internal/osutil"
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	core.ApplyDebugStackLimit()
	args := osutil.Args()[1:]
	if len(args) > 0 {
		switch args[0] {
		case "--lsp":
			return runLSP(args[1:])
		case "--api":
			return runAPI(args[1:])
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	result := execute.CommandLine(ctx, newSystem(), args, nil)
	return int(result.Status)
}

// rejectASTBackend refuses an --astBackend other than pointer for the language
// server and the API, which have the Pointer AST only
// (store-cli-design-20261004.md 3): the CLI's check mode is the Store's only
// entry.
func rejectASTBackend(value, mode string) (int, bool) {
	if value == "pointer" {
		return 0, true
	}
	fmt.Fprintln(os.Stderr, diagnostics.Option_0_cannot_be_used_with_1.Localize(locale.Default, "--astBackend "+value, mode))
	return int(tsc.ExitStatusNotImplemented), false
}
