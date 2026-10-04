package main

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/execute/tsc"
)

// The language server and the API refuse --astBackend store before they
// start (store-cli-design-20261004.md 3), and take pointer.
func TestASTBackendLSPAndAPI(t *testing.T) {
	if got := runLSP([]string{"--stdio", "--astBackend", "store"}); got != int(tsc.ExitStatusNotImplemented) {
		t.Errorf("--lsp --astBackend store: %d", got)
	}
	if got := runAPI([]string{"--astBackend", "store"}); got != int(tsc.ExitStatusNotImplemented) {
		t.Errorf("--api --astBackend store: %d", got)
	}
	if _, ok := rejectASTBackend("pointer", "--lsp"); !ok {
		t.Error("--lsp refuses --astBackend pointer")
	}
	if status, ok := rejectASTBackend("foo", "--api"); ok || status != int(tsc.ExitStatusNotImplemented) {
		t.Errorf("--api --astBackend foo: %d %v", status, ok)
	}
}
