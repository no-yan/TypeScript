package store

import (
	"sync"
	"sync/atomic"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// File is the result of the Store parser: the fields of ast.SourceFile that
// the parser sets without walking the tree, the external module indicator and
// the module references, the JSDoc cache (jsdoc.go), and the binder's output
// (Bound). It is not named SourceFile because that is the typed view of the
// root node.
type File struct {
	Store                                                            *Store
	fileName                                                         tspath.RootedFilePath
	pathKey                                                          tspath.PathKey
	text                                                             string // = Store.src
	contentMapper                                                    string // the mapper that produced the text, "" for none
	ScriptKind                                                       core.ScriptKind
	LanguageVariant                                                  core.LanguageVariant
	IsDeclarationFile                                                bool
	Flags                                                            ast.NodeFlags // the root's flags, sourceFlags included; Root().Flags() reads the same value
	IdentifierCount                                                  int
	NodeCount                                                        int               // len(nodes) - 1, dead nodes included
	CreatedNodeCount                                                 int               // the nodes the Pointer parser makes: the rows kept and those not (a rewind's, a typedef's unfinished JSDocTypeLiteral); the checker association's work
	diagnostics                                                      []*ast.Diagnostic // file is nil: attachFileToDiagnostics belongs to the program (7d)
	jsDiagnostics                                                    []*ast.Diagnostic
	CommentDirectives                                                []ast.CommentDirective
	Pragmas                                                          []ast.Pragma
	ReferencedFiles, TypeReferenceDirectives, LibReferenceDirectives []*ast.FileReference
	CheckJsDirective                                                 *ast.CheckJsDirective

	// Set by the parser (references.go). ExternalModuleIndicator is the root
	// when the file was forced to be a module.
	ExternalModuleIndicator     NodeRef
	imports                     []NodeRef // the module specifier literals
	ModuleAugmentations         []NodeRef
	AmbientModuleNames          []string
	UsesUriStyleNodeCoreModules core.Tristate

	// Set by the parser (jsdoc.go). The cache maps a host (a node of Store)
	// to its JSDoc: filled at the end of the parse for the eager JSDoc (JS,
	// and TS comments with @see or @link), and under jsdocMu for the lazy
	// JSDoc of a TS file (hasLazyJSDoc).
	jsdocDiagnostics []*ast.Diagnostic
	jsdocCache       map[NodeRef][]Node
	jsdocMu          sync.RWMutex
	hasLazyJSDoc     bool
	ReparsedClones   []NodeRef // sorted by position, as ast.SourceFile.ReparsedClones

	// Set by the binder. Bound is attached when the bind starts, because the
	// binder writes CommonJSModuleIndicator during the bind; isBound is set
	// once it is done, as in ast.SourceFile.
	Bound    *Bound
	bindOnce sync.Once
	isBound  atomic.Bool

	// ECMALineMap's and HasIdentifier's caches (checker_api.go).
	importsOnce     sync.Once
	importNodes     []Node
	ecmaLineMapMu   sync.RWMutex
	ecmaLineMap     []core.TextPos
	identifiersOnce sync.Once
	identifiers     collections.Set[string]
}

// NewFile is the File of a parse, before its Store: the three fields of
// ast.SourceFile that the parse options give. They are read through methods
// named as ast.SourceFile's, so that *File is an ast.HasFileName, which the
// checker's hosts take (store-checker-7d2a-implementation-instructions-20260929.md
// 4.2).
func NewFile(fileName tspath.RootedFilePath, pathKey tspath.PathKey, text string) *File {
	return &File{fileName: fileName, pathKey: pathKey, text: text}
}

func (f *File) FileName() tspath.RootedFilePath { return f.fileName }
func (f *File) PathKey() tspath.PathKey         { return f.pathKey }
func (f *File) Text() string                    { return f.text }

// ContentMapper is (*ast.SourceFile).ContentMapper: the name of the content
// mapper that produced the file's text, or "". The Store program has none
// (the CLI rejects content mappers); the harness, which parses the Pointer
// program's files, copies the Pointer file's.
func (f *File) ContentMapper() string { return f.contentMapper }

// SetContentMapper names the content mapper of a file parsed from a
// content-mapped Pointer file.
func (f *File) SetContentMapper(name string) { f.contentMapper = name }

// Imports is (*ast.SourceFile).Imports: the module specifier literals, as
// Nodes. The parse keeps them as NodeRefs (AppendImport); the first call
// resolves them once and keeps the result, so a later call does not allocate
// (store-checker-7d2a: the modulespecifiers host takes the Nodes; 2026-09-30,
// the user's decision).
func (f *File) Imports() []Node {
	f.importsOnce.Do(func() {
		if len(f.imports) == 0 {
			return
		}
		f.importNodes = make([]Node, len(f.imports))
		for i, ref := range f.imports {
			f.importNodes[i] = f.Store.Node(ref)
		}
	})
	return f.importNodes
}

// ImportRefs are the module specifier literals as NodeRefs of the Store.
func (f *File) ImportRefs() []NodeRef { return f.imports }

// AppendImport adds a module specifier literal of the file's Store.
func (f *File) AppendImport(ref NodeRef) { f.imports = append(f.imports, ref) }

var _ ast.HasFileName = (*File)(nil)

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
	return f.Bound.SymbolOf(f.Root().SymbolId())
}

func (f *File) IsExternalModule() bool { return f.ExternalModuleIndicator != NoNodeRef }

func (f *File) IsExternalOrCommonJSModule() bool {
	return f.ExternalModuleIndicator != NoNodeRef || f.Bound != nil && f.Bound.CommonJSModuleIndicator != NoNodeRef
}
