package storecompiler

import (
	"sync"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The P2 declarations of the include diagnostics at the border of the config
// and the sources (store-cli-design-20261004.md 6): the config's syntax is the
// Pointer's JSON AST, the sources' the Store's.

// includeProcessor keeps the config's compilerOptions object as the Pointer's.
//
// storetranslate:pointer-hash c733e8288bba12cc includeprocessor.go:includeProcessor
type includeProcessor struct {
	fileIncludeReasons    map[tspath.Path][]*FileIncludeReason
	processingDiagnostics []*processingDiagnostic

	reasonToReferenceLocation  collections.SyncMap[*FileIncludeReason, *referenceFileLocation]
	includeReasonToRelatedInfo collections.SyncMap[*FileIncludeReason, *store.Diagnostic]
	redirectAndFileFormat      collections.SyncMap[tspath.Path, []*store.Diagnostic]
	computedDiagnostics        *store.DiagnosticsCollection
	computedDiagnosticsOnce    sync.Once
	compilerOptionsSyntax      *ast.ObjectLiteralExpression
	compilerOptionsSyntaxOnce  sync.Once
}

// storetranslate:pointer-hash 272cb29deac80c3c includeprocessor.go:includeProcessor.getCompilerOptionsObjectLiteralSyntax
func (i *includeProcessor) getCompilerOptionsObjectLiteralSyntax(program *Program) *ast.ObjectLiteralExpression {
	i.compilerOptionsSyntaxOnce.Do(func() {
		configFile := program.opts.Config.ConfigFile
		if configFile != nil {
			if compilerOptionsProperty := tsoptions.ForEachTsConfigPropArray(configFile.SourceFile, "compilerOptions", core.Identity); compilerOptionsProperty != nil &&
				compilerOptionsProperty.Initializer != nil &&
				ast.IsObjectLiteralExpression(compilerOptionsProperty.Initializer) {
				i.compilerOptionsSyntax = compilerOptionsProperty.Initializer.AsObjectLiteralExpression()
			}
		} else {
			i.compilerOptionsSyntax = nil
		}
	})
	return i.compilerOptionsSyntax
}

// diagnosticAt is tsoptions.CreateDiagnosticForNodeInSourceFile's range on
// the Store file of the reference.
//
// storetranslate:pointer-hash 2b7a33fc4f09a3af fileInclude.go:referenceFileLocation.diagnosticAt
func (r *referenceFileLocation) diagnosticAt(message *diagnostics.Message, args ...any) *store.Diagnostic {
	if !r.node.IsNil() {
		return store.NewDiagnostic(r.file, core.NewTextRange(scanner.SkipTrivia(r.file.Text(), int(r.node.Pos())), int(r.node.End())), message, args...)
	} else {
		return store.NewDiagnostic(r.file, r.ref.TextRange, message, args...)
	}
}
