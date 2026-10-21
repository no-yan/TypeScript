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

// Symbol stores flags, checkFlags, and gid uniquely for every symbol instance, but may share
// symbolData with other symbols. This is done so that instantiated symbols in the checker can
// all share the same underlying symbolData. Every Symbol needs its data field initialized either
// through SymbolWithData.Initialize or by explicitly setting it with Symbol.SetSymbolData.
type Symbol struct {
	flags      ast.SymbolFlags
	checkFlags ast.CheckFlags // Non-zero only in transient symbols created by Checker
	gid        atomic.Uint64
	data       *symbolData
}

// Every symbol references a symbolData structure that may be shared with other symbols.
// id is the Bound's index of the binder symbol that owns the data: a symbol
// sharing it (an instantiated one, which the checker makes) has the owner's id,
// and only the binder reads ids, of its own symbols (upstream-sync-20261021.md
// S5).
type symbolData struct {
	name             string
	declarations     []Node
	valueDeclaration Node // zero = nil
	parent           *Symbol
	extra            *symbolExtra
	id               SymbolId
}

type symbolExtra struct {
	members      SymbolTable
	exports      SymbolTable
	exportSymbol *Symbol
}

// SymbolExtraArena batches optional symbol metadata without adding an allocator
// pointer to every symbol. References from symbolData keep its blocks alive.
type SymbolExtraArena struct {
	arena core.Arena[symbolExtra]
}

func (s *Symbol) Flags() ast.SymbolFlags     { return s.flags }
func (s *Symbol) CheckFlags() ast.CheckFlags { return s.checkFlags }
func (s *Symbol) Name() string               { return s.data.name }
func (s *Symbol) Declarations() []Node       { return s.data.declarations }
func (s *Symbol) ValueDeclaration() Node     { return s.data.valueDeclaration }
func (s *Symbol) Parent() *Symbol            { return s.data.parent }

func (s *Symbol) Members() SymbolTable {
	if s.data.extra == nil {
		return nil
	}
	return s.data.extra.members
}

func (s *Symbol) Exports() SymbolTable {
	if s.data.extra == nil {
		return nil
	}
	return s.data.extra.exports
}

func (s *Symbol) ExportSymbol() *Symbol {
	if s.data.extra == nil {
		return nil
	}
	return s.data.extra.exportSymbol
}

func (s *Symbol) SetFlags(value ast.SymbolFlags)     { s.flags = value }
func (s *Symbol) SetCheckFlags(value ast.CheckFlags) { s.checkFlags = value }
func (s *Symbol) SetName(value string)               { s.data.name = value }
func (s *Symbol) SetDeclarations(value []Node)       { s.data.declarations = value }
func (s *Symbol) SetValueDeclaration(value Node)     { s.data.valueDeclaration = value }
func (s *Symbol) SetParent(value *Symbol)            { s.data.parent = value }

func (s *Symbol) ensureExtra(arena *SymbolExtraArena) *symbolExtra {
	if s.data.extra == nil {
		s.data.extra = arena.arena.New()
	}
	return s.data.extra
}

func (s *Symbol) SetMembers(value SymbolTable, arena *SymbolExtraArena) {
	if value != nil || s.data.extra != nil {
		s.ensureExtra(arena).members = value
	}
}

func (s *Symbol) SetExports(value SymbolTable, arena *SymbolExtraArena) {
	if value != nil || s.data.extra != nil {
		s.ensureExtra(arena).exports = value
	}
}

func (s *Symbol) SetExportSymbol(value *Symbol, arena *SymbolExtraArena) {
	if value != nil || s.data.extra != nil {
		s.ensureExtra(arena).exportSymbol = value
	}
}

// SymbolWithData is a helper structure that contains both a Symbol and its associated symbolData.
type SymbolWithData struct {
	s Symbol
	d symbolData
}

// Initializes a SymbolWithData instance and returns the embedded Symbol.
func (sd *SymbolWithData) Initialize() *Symbol {
	sd.s.data = &sd.d
	return &sd.s
}

// Initializes a Symbol's data field with the data from another Symbol. This allows multiple
// symbols to share the same underlying symbolData.
func (s *Symbol) SetSymbolData(other *Symbol) {
	s.data = other.data
}

func (s *Symbol) Id() SymbolId { return s.data.id }

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

// SymbolIdGenerator is ast.SymbolIdGenerator (aa8149273b): a checker's dealer
// of gids in blocks of ast.BlockIdSize, from ast.BlockIdOffset up, so that the
// links of its symbols are dense. Its GetSymbolId is P2 (utilities_manual.go).
type SymbolIdGenerator struct {
	nextId uint64
	lastId uint64
}

// nextSymbolBlockId is the counter of SymbolIdGenerator's blocks.
var nextSymbolBlockId atomic.Uint64

func (s *Symbol) IsExternalModule() bool {
	return s.Flags()&ast.SymbolFlagsModule != 0 && ast.IsAmbientModuleSymbolName(s.Name())
}

func (s *Symbol) IsStatic() bool {
	if s.ValueDeclaration().IsNil() {
		return false
	}
	modifierFlags := s.ValueDeclaration().ModifierFlags()
	return modifierFlags&ast.ModifierFlagsStatic != 0
}

// See comment on `declareModuleMember` in `binder.go`.
func (s *Symbol) CombinedLocalAndExportSymbolFlags() ast.SymbolFlags {
	if s.ExportSymbol() != nil {
		return s.Flags() | s.ExportSymbol().Flags()
	}
	return s.Flags()
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
