package store

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// internal/ast/symbol.go with the two node fields as Ref, and the id given at
// creation instead of on demand (store-binder-design-20260922.md 2.4). The
// InternalSymbolName constants are ast's.

// SymbolId is the index of a symbol in Bound.symbols: 1-based, dense per file.
// 0 is nil.
type SymbolId uint32

// Symbol

type Symbol struct {
	Flags            ast.SymbolFlags
	CheckFlags       ast.CheckFlags // Non-zero only in transient symbols created by Checker
	Name             string
	Declarations     []Ref
	ValueDeclaration Ref // zero = nil
	Members          SymbolTable
	Exports          SymbolTable
	id               SymbolId
	Parent           *Symbol
	ExportSymbol     *Symbol
}

func (s *Symbol) Id() SymbolId { return s.id }

func (s *Symbol) IsExternalModule() bool {
	return s.Flags&ast.SymbolFlagsModule != 0 && ast.IsAmbientModuleSymbolName(s.Name)
}

// IsStatic reads the ValueDeclaration in st, the Store of the file that holds it.
func (s *Symbol) IsStatic(st *Store) bool {
	if s.ValueDeclaration == (Ref{}) {
		return false
	}
	modifierFlags := st.Node(s.ValueDeclaration.Id()).ModifierFlags()
	return modifierFlags&ast.ModifierFlagsStatic != 0
}

// See comment on `declareModuleMember` in `binder.go`.
func (s *Symbol) CombinedLocalAndExportSymbolFlags() ast.SymbolFlags {
	if s.ExportSymbol != nil {
		return s.Flags | s.ExportSymbol.Flags
	}
	return s.Flags
}

// SymbolTable

type SymbolTable map[string]*Symbol

type PatternAmbientModule struct {
	Pattern core.Pattern
	Symbol  *Symbol
}

// SymbolName reads the ValueDeclaration in st, the Store of the file that holds it.
func SymbolName(st *Store, symbol *Symbol) string {
	if symbol.ValueDeclaration != (Ref{}) && IsPrivateIdentifierClassElementDeclaration(st.Node(symbol.ValueDeclaration.Id())) {
		return st.Node(symbol.ValueDeclaration.Id()).Name().Text()
	}
	return symbol.Name
}

// EscapeAllInternalSymbolNames replaces internal symbol name markers ("\xFE") with "__".
func EscapeAllInternalSymbolNames(name string) string {
	return strings.ReplaceAll(name, ast.InternalSymbolNamePrefix, "__")
}

func EscapeInternalSymbolName(name string) string {
	if rest, ok := strings.CutPrefix(name, ast.InternalSymbolNamePrefix); ok {
		return "__" + rest
	}
	return name
}

// EscapeSymbolName converts a binder symbol name into its escaped "__String"
// form. Internal names (prefixed with the "\xFE" sentinel) become "__"-prefixed,
// and user names that already begin with "__" gain an extra leading underscore
// so they can be distinguished from internal names.
func EscapeSymbolName(name string) string {
	if rest, ok := strings.CutPrefix(name, ast.InternalSymbolNamePrefix); ok {
		return "__" + rest
	}
	if len(name) >= 2 && name[0] == '_' && name[1] == '_' {
		return "_" + name
	}
	return name
}

// The lazy tables, from internal/ast/utilities.go.

func GetSymbolTable(data *SymbolTable) SymbolTable {
	if *data == nil {
		*data = make(SymbolTable)
	}
	return *data
}

func GetMembers(symbol *Symbol) SymbolTable {
	return GetSymbolTable(&symbol.Members)
}

func GetExports(symbol *Symbol) SymbolTable {
	return GetSymbolTable(&symbol.Exports)
}

// GetLocals is ast.GetLocals through the reserved slot: a container's slot
// holds the index of its table in bound, made on first use. A kind without the
// slot panics, as the Pointer version does on a nil LocalsContainerData.
func GetLocals(bound *Bound, container Node) SymbolTable {
	i, ok := container.LocalsSlot()
	if !ok {
		panic("store: " + container.Kind().String() + " is not a locals container")
	}
	if i == 0 {
		i = bound.NewLocals()
		container.SetLocalsSlot(i)
	}
	return bound.Locals(i)
}
