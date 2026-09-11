//go:build binderinvestigation

package binder

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// Only lifetime management differs in the pointer comparison overlay.
// These helpers are never called inside the measured bind loop.
func investigationRegisteredStores() int { return ast.RegisteredStoreCount() }

func investigationReleaseFile(file *ast.SourceFile) {
	ast.UnregisterStore(file.ParseStore())
}
