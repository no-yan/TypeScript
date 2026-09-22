package store

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// File is the result of the Store parser: the fields of ast.SourceFile that
// the parser sets without walking the tree. It is not named SourceFile because
// that is the typed view of the root node. The external module indicator, the
// module references, the JSDoc cache and the binder's fields are not here yet
// (TODO 7c and later, store-ast-design-20260922.md 6).
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
	diagnostics                                                      []*ast.Diagnostic // file is nil: attachFileToDiagnostics belongs to the program (7c)
	jsDiagnostics                                                    []*ast.Diagnostic
	CommentDirectives                                                []ast.CommentDirective
	Pragmas                                                          []ast.Pragma
	ReferencedFiles, TypeReferenceDirectives, LibReferenceDirectives []*ast.FileReference
	CheckJsDirective                                                 *ast.CheckJsDirective
}

func (f *File) Root() Node { return f.Store.Root() }

func (f *File) Diagnostics() []*ast.Diagnostic       { return f.diagnostics }
func (f *File) SetDiagnostics(d []*ast.Diagnostic)   { f.diagnostics = d }
func (f *File) JSDiagnostics() []*ast.Diagnostic     { return f.jsDiagnostics }
func (f *File) SetJSDiagnostics(d []*ast.Diagnostic) { f.jsDiagnostics = d }
