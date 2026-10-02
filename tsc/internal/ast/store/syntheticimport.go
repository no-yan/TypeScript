package store

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// NewSyntheticImport is the fileloader's createSyntheticImport
// (compiler/fileloader.go): the module specifier of an import the program
// adds to a file, of the import helpers or of the JSX runtime. It is a
// StringLiteral of text whose parent is an ImportDeclaration with no other
// member, whose parent is file's root, which does not list it as a statement;
// every node at (-1, -1), with no flags.
//
// The two nodes are a Store of their own in file's program, the program's
// and immutable (2026-10-02, the user): the next StoreID of the program, no
// owner and no file, sealed, as a Store of FinishSynthetic is. Every region of
// the program may take them as children or parents (invariant 26) and none
// may write them. File walks the parents to file. The program makes one for a
// file and a role, and keeps it: each call makes a new one.
func NewSyntheticImport(file *File, text string) Node {
	space := file.Store.space
	if space == nil {
		panic("store: a synthetic import belongs to a program")
	}
	b := NewBuilder("", space, space.Next())
	literal := b.NewStringLiteral(0, -1, -1, text, ast.TokenFlagsNone)
	decl := b.NewImportDeclaration(ast.KindImportDeclaration, 0, -1, -1, NoListRef, NoNodeRef, literal, NoNodeRef)
	s := b.Finish()
	// The declaration's parent is the root, a foreign entry, as a lazy JSDoc
	// root's is its host (FinishLazyJSDoc).
	s.foreignNodes = []Node{file.Root()}
	s.nodes[decl].parent = foreignBit
	s.sealed = true
	return s.node(literal)
}
