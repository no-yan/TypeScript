package store

import (
	"strings"
	"sync/atomic"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// internal/ast/symbol.go with the two node fields as Node (D2) and two ids:
// id, the dense index in the Bound, given at creation, and gid, the lazy
// process-wide id of ast.GetSymbolId (I1). internal/ast/docs/
// store-checker-design-20260923.md 2.4 and 2.5, and
// store-checker-review-contracts-20260926.md 1. The InternalSymbolName
// constants are ast's.

// SymbolId is the index of a symbol in Bound.symbols: 1-based, dense per file.
// 0 is nil. It is the Bound's reference only: a private name or anything that
// needs a symbol's identity across files uses GetSymbolId.
type SymbolId uint32

// Symbol

type Symbol struct {
	Flags            ast.SymbolFlags
	CheckFlags       ast.CheckFlags // Non-zero only in transient symbols created by Checker
	Name             string
	Declarations     []Node
	ValueDeclaration Node // zero = nil
	Members          SymbolTable
	Exports          SymbolTable
	id               SymbolId
	gid              atomic.Uint64
	Parent           *Symbol
	ExportSymbol     *Symbol
}

func (s *Symbol) Id() SymbolId { return s.id }

var nextSymbolId atomic.Uint64

// newSymbolId is the counter of GetSymbolId (utilities_manual.go). It never
// wraps: at the end of the range it panics without moving, so that no later
// call reissues an id.
func newSymbolId() uint64 {
	for {
		id := nextSymbolId.Load()
		if id == ^uint64(0) {
			panic("store: the symbol ids are exhausted")
		}
		if nextSymbolId.CompareAndSwap(id, id+1) {
			return id + 1
		}
	}
}

func (s *Symbol) IsExternalModule() bool {
	return s.Flags&ast.SymbolFlagsModule != 0 && ast.IsAmbientModuleSymbolName(s.Name)
}

func (s *Symbol) IsStatic() bool {
	if s.ValueDeclaration.IsNil() {
		return false
	}
	modifierFlags := s.ValueDeclaration.ModifierFlags()
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
