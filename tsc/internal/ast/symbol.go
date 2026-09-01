package ast

import (
	"strings"
	"sync/atomic"
)

// Symbol

type Symbol struct {
	Flags            SymbolFlags
	CheckFlags       CheckFlags // Non-zero only in transient symbols created by Checker
	Name             string
	Declarations     []GlobalRef
	ValueDeclaration GlobalRef
	Members          SymbolTable
	Exports          SymbolTable
	id               atomic.Uint64
	Parent           *Symbol
	ExportSymbol     *Symbol
}

func (s *Symbol) IsExternalModule() bool {
	return s.Flags&SymbolFlagsModule != 0 && len(s.Name) > 0 && s.Name[0] == '"'
}

func (s *Symbol) IsStatic() bool {
	decl := NodeOf(s.ValueDeclaration)
	if decl.IsNil() {
		return false
	}
	modifierFlags := decl.ModifierFlags()
	return modifierFlags&ModifierFlagsStatic != 0
}

// See comment on `declareModuleMember` in `binder.go`.
func (s *Symbol) CombinedLocalAndExportSymbolFlags() SymbolFlags {
	if s.ExportSymbol != nil {
		return s.Flags | s.ExportSymbol.Flags
	}
	return s.Flags
}

// SymbolTable

type SymbolTable map[string]*Symbol

const InternalSymbolNamePrefix = "\xFE" // Invalid UTF8 sequence, will never occur as IdentifierName

const (
	InternalSymbolNameCall                    = InternalSymbolNamePrefix + "call"                    // Call signatures
	InternalSymbolNameConstructor             = InternalSymbolNamePrefix + "constructor"             // Constructor implementations
	InternalSymbolNameNew                     = InternalSymbolNamePrefix + "new"                     // Constructor signatures
	InternalSymbolNameIndex                   = InternalSymbolNamePrefix + "index"                   // Index signatures
	InternalSymbolNameExportStar              = InternalSymbolNamePrefix + "export"                  // Module export * declarations
	InternalSymbolNameGlobal                  = InternalSymbolNamePrefix + "global"                  // Global self-reference
	InternalSymbolNameMissing                 = InternalSymbolNamePrefix + "missing"                 // Indicates missing symbol
	InternalSymbolNameType                    = InternalSymbolNamePrefix + "type"                    // Anonymous type literal symbol
	InternalSymbolNameObject                  = InternalSymbolNamePrefix + "object"                  // Anonymous object literal declaration
	InternalSymbolNameJSXAttributes           = InternalSymbolNamePrefix + "jsxAttributes"           // Anonymous JSX attributes object literal declaration
	InternalSymbolNameClass                   = InternalSymbolNamePrefix + "class"                   // Unnamed class expression
	InternalSymbolNameFunction                = InternalSymbolNamePrefix + "function"                // Unnamed function expression
	InternalSymbolNameComputed                = InternalSymbolNamePrefix + "computed"                // Computed property name declaration with dynamic name
	InternalSymbolNameAssignmentDeclaration   = InternalSymbolNamePrefix + "assignment"              // Assignment declarations
	InternalSymbolNameInstantiationExpression = InternalSymbolNamePrefix + "instantiationExpression" // Instantiation expressions
	InternalSymbolNameImportAttributes        = InternalSymbolNamePrefix + "importAttributes"
	InternalSymbolNameExportEquals            = "export=" // Export assignment symbol
	InternalSymbolNameDefault                 = "default" // Default export symbol (technically not wholly internal, but included here for usability)
	InternalSymbolNameThis                    = "this"
	InternalSymbolNameModuleExports           = "module.exports"
)

func SymbolName(symbol *Symbol) string {
	decl := NodeOf(symbol.ValueDeclaration)
	if !decl.IsNil() && IsPrivateIdentifierClassElementDeclaration(decl) {
		return decl.Name().Text()
	}
	return symbol.Name
}

// EscapeAllInternalSymbolNames replaces internal symbol name markers ("\xFE") with "__".
func EscapeAllInternalSymbolNames(name string) string {
	return strings.ReplaceAll(name, InternalSymbolNamePrefix, "__")
}

func EscapeInternalSymbolName(name string) string {
	if rest, ok := strings.CutPrefix(name, InternalSymbolNamePrefix); ok {
		return "__" + rest
	}
	return name
}

// EscapeSymbolName converts a binder symbol name into its escaped "__String"
// form. Internal names (prefixed with the "\xFE" sentinel) become "__"-prefixed,
// and user names that already begin with "__" gain an extra leading underscore
// so they can be distinguished from internal names.
func EscapeSymbolName(name string) string {
	if rest, ok := strings.CutPrefix(name, InternalSymbolNamePrefix); ok {
		return "__" + rest
	}
	if len(name) >= 2 && name[0] == '_' && name[1] == '_' {
		return "_" + name
	}
	return name
}

func FindSymbolDeclaration(symbol *Symbol, pred func(Handle) bool) Handle {
	if symbol == nil {
		return Handle{}
	}
	for _, g := range symbol.Declarations {
		d := NodeOf(g)
		if !d.IsNil() && pred(d) {
			return d
		}
	}
	return Handle{}
}

func FindLastSymbolDeclaration(symbol *Symbol, pred func(Handle) bool) Handle {
	if symbol == nil {
		return Handle{}
	}
	for i := len(symbol.Declarations) - 1; i >= 0; i-- {
		d := NodeOf(symbol.Declarations[i])
		if !d.IsNil() && pred(d) {
			return d
		}
	}
	return Handle{}
}

func DeclarationNodes(symbol *Symbol) []Handle {
	if symbol == nil {
		return nil
	}
	out := make([]Handle, 0, len(symbol.Declarations))
	for _, g := range symbol.Declarations {
		if n := NodeOf(g); !n.IsNil() {
			out = append(out, n)
		}
	}
	return out
}

func SomeDeclaration(symbol *Symbol, pred func(Handle) bool) bool {
	return !FindSymbolDeclaration(symbol, pred).IsNil()
}

func EveryDeclaration(symbol *Symbol, pred func(Handle) bool) bool {
	if symbol == nil {
		return true
	}
	for _, g := range symbol.Declarations {
		d := NodeOf(g)
		if d.IsNil() || !pred(d) {
			return false
		}
	}
	return true
}
