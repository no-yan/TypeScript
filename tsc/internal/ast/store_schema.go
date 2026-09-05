package ast

import "github.com/microsoft/TypeScript/tsc/internal/core"

// Kind-schema slot layouts for Store-backed nodes. Child order matches
// the pointer AST's ForEachChild where the subset overlaps.

const (
	binSlotLeft = iota
	binSlotType
	binSlotOperator
	binSlotRight
	binSlotCount
)

const (
	paramSlotDotDotDot = iota
	paramSlotName
	paramSlotQuestion
	paramSlotType
	paramSlotInitializer
	paramSlotCount
)

// BinaryParts is the Store-native BinaryExpression shape (no modifiers /
// type annotation in this slice; add slots when those kinds land).
type BinaryParts struct {
	Left     Handle
	Operator Handle
	Right    Handle
	Loc      core.TextRange
}

type ParameterParts struct {
	DotDotDot   Handle
	Name        Handle
	Question    Handle
	Type        Handle
	Initializer Handle
	Loc         core.TextRange
}

type ArrayLiteralParts struct {
	Elements ListRef
	Loc      core.TextRange
}
