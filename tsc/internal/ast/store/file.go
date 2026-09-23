package store

import (
	"sync"
	"sync/atomic"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// File is the result of the Store parser: the fields of ast.SourceFile that
// the parser sets without walking the tree, the external module indicator and
// the module references, and the binder's output (Bound). It is not named
// SourceFile because that is the typed view of the root node. The JSDoc cache
// is not here (store-ast-design-20260922.md 5 and 6).
type File struct {
	Store                                                            *Store
	FileName                                                         string
	Path                                                             tspath.Path
	Text                                                             string // = Store.src
	ScriptKind                                                       core.ScriptKind
	LanguageVariant                                                  core.LanguageVariant
	IsDeclarationFile                                                bool
	Flags                                                            ast.NodeFlags // the root's flags, sourceFlags included; Root().Flags() reads the same value
	IdentifierCount                                                  int
	NodeCount                                                        int               // len(nodes) - 1, dead nodes included
	diagnostics                                                      []*ast.Diagnostic // file is nil: attachFileToDiagnostics belongs to the program (7d)
	jsDiagnostics                                                    []*ast.Diagnostic
	CommentDirectives                                                []ast.CommentDirective
	Pragmas                                                          []ast.Pragma
	ReferencedFiles, TypeReferenceDirectives, LibReferenceDirectives []*ast.FileReference
	CheckJsDirective                                                 *ast.CheckJsDirective

	// Set by the parser (references.go). ExternalModuleIndicator is the root
	// when the file was forced to be a module.
	ExternalModuleIndicator     NodeRef
	Imports                     []NodeRef // the module specifier literals
	ModuleAugmentations         []NodeRef
	AmbientModuleNames          []string
	UsesUriStyleNodeCoreModules core.Tristate

	// Set by the binder. Bound is attached when the bind starts, because the
	// binder writes CommonJSModuleIndicator during the bind; isBound is set
	// once it is done, as in ast.SourceFile.
	Bound    *Bound
	bindOnce sync.Once
	isBound  atomic.Bool
}

func (f *File) Root() Node { return f.Store.Root() }

func (f *File) Diagnostics() []*ast.Diagnostic       { return f.diagnostics }
func (f *File) SetDiagnostics(d []*ast.Diagnostic)   { f.diagnostics = d }
func (f *File) JSDiagnostics() []*ast.Diagnostic     { return f.jsDiagnostics }
func (f *File) SetJSDiagnostics(d []*ast.Diagnostic) { f.jsDiagnostics = d }

func (f *File) IsBound() bool { return f.isBound.Load() }

// BindOnce has the form of ast.SourceFile.BindOnce: bind runs once, and
// isBound is set after it.
func (f *File) BindOnce(bind func()) {
	f.bindOnce.Do(func() {
		bind()
		f.isBound.Store(true)
	})
}

// Symbol is the file's symbol: the root header's symbol, as the Pointer
// file.Symbol is the root's DeclarationBase.Symbol.
func (f *File) Symbol() *Symbol {
	if f.Bound == nil {
		return nil
	}
	return f.Bound.SymbolOf(f.Root().Symbol())
}

func (f *File) IsExternalModule() bool { return f.ExternalModuleIndicator != NoNodeRef }

func (f *File) IsExternalOrCommonJSModule() bool {
	return f.ExternalModuleIndicator != NoNodeRef || f.Bound != nil && f.Bound.CommonJSModuleIndicator != NoNodeRef
}
