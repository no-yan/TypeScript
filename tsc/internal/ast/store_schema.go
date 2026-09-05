package ast

import "github.com/microsoft/TypeScript/tsc/internal/core"

// Compatibility argument structs for the first Store-native factory
// experiments. Their implementations now delegate to the generated,
// full-fidelity Handle factory.
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
