package pseudochecker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

type PseudoTypeKind int16

const (
	PseudoTypeKindDirect PseudoTypeKind = iota
	PseudoTypeKindInferred
	PseudoTypeKindNoResult
	PseudoTypeKindMaybeConstLocation
	PseudoTypeKindUnion
	PseudoTypeKindUndefined
	PseudoTypeKindNull
	PseudoTypeKindAny
	PseudoTypeKindString
	PseudoTypeKindNumber
	PseudoTypeKindBigInt
	PseudoTypeKindBoolean
	PseudoTypeKindFalse
	PseudoTypeKindTrue
	PseudoTypeKindSingleCallSignature
	PseudoTypeKindTuple
	PseudoTypeKindObjectLiteral
	PseudoTypeKindStringLiteral
	PseudoTypeKindNumericLiteral
	PseudoTypeKindBigIntLiteral
)

type PseudoType struct {
	Kind PseudoTypeKind
	data pseudoTypeData
}

func newPseudoType(kind PseudoTypeKind, data pseudoTypeData) *PseudoType {
	n := data.AsPseudoType()
	n.Kind = kind
	n.data = data
	return n
}

type pseudoTypeData interface {
	AsPseudoType( // `PseudoType`s are skeletons of types - partially interpreted expressions and type nodes
	// composed to represent how you *should* construct a type out of them. They can be trivially
	// mapped into actual types by a real `Checker`, or into a tree of `Node`s directly, without
	// needing to make any intermediate types, by a `NodeBuilder`. Unlike checker `Type`s, these are
	// never normalized, and multiple pseudo-types may refer to the same underlying `Type`.
	// In strada, these were implicit in the AST nodes constructed in `expressionToTypeNode.ts`, which
	// repurposed AST nodes for this purpose, but in so doing, often confused weather or not it had validated
	// nested nodes for use at a given use-site. By keeping the mapping deferred like this, we can know we haven't
	// done any use-site checks until we're ready to map the `PseudoType` into a `Node`, and can cache
	// `PseudoType`s across multiple target positions.
	// PseudoTypeDirect directly encodes the type referred to by a given TypeNode
	// PseudoTypeInferred directly encodes the type referred to by a given Expression
	// These represent cases where the expression was too complex for the pseudochecker.
	// Most of the time, these locations will produce an error under ID.
	// Specific error nodes (shorthand properties, spread assignments, etc.) are stored on the
	// ErrorNodes field, collected during pseudochecker construction.
	// PseudoTypeNoResult is analogous to PseudoTypeInferred in that it references a case
	// where the type was too complex for the pseudochecker. Rather than an expression, however,
	// it is referring to the return type of a signature or declaration.
	// PseudoTypeMaybeConstLocation encodes the const/regular types of a location so the builder
	// can later select the appropriate pseudotype based on the location's context. This is used
	// to ensure accuracy in nested expressions without exposing type-based functionality to the pseudochecker.
	// A nodebuilder that doesn't do contextual typing would need to, as policy, reject these types if they
	// are in a contextually typed position! (Otherwise they could pick one, but either type could be wrong, depending on context!)
	// At the top-level, which is generally what ID is concerned with, nothing is contextually typed, so these cases don't generally
	// cause problems. Once you get into reused nodes in nested expressions, however, this becomes important.
	// In strada, checker `isConstContext` functionality exposed to the pseudochecker + type comparison sanity checking
	// on nested results masks the need for this abstraction, but with it present it clearly highlights a shortcoming
	// of the ID infernce model and how "standalone" it can(n't) truly be without substantial restrictions on expression inference.
	// PseudoTypeUnion is a collection of psudotypes joined into a union
	// PseudoTypeSingleCallSignature represents an object type with a single call signature, like an arrow or function expression
	// PseudoTypeTuple represents a tuple originaing from an `as const` array literal
	// PseudoTypeObjectLiteral represents an object type originaing from an object literal
	// PseudoTypeLiteral represents a literal type
	) *PseudoType
}
type PseudoTypeDefault struct{ PseudoType }

func (b *PseudoTypeDefault) AsPseudoType() *PseudoType {
	return &b.PseudoType
}

type PseudoTypeBase struct{ PseudoTypeDefault }

var (
	PseudoTypeUndefined = newPseudoType(PseudoTypeKindUndefined, &PseudoTypeBase{})
	PseudoTypeNull      = newPseudoType(PseudoTypeKindNull, &PseudoTypeBase{})
	PseudoTypeAny       = newPseudoType(PseudoTypeKindAny, &PseudoTypeBase{})
	PseudoTypeString    = newPseudoType(PseudoTypeKindString, &PseudoTypeBase{})
	PseudoTypeNumber    = newPseudoType(PseudoTypeKindNumber, &PseudoTypeBase{})
	PseudoTypeBigInt    = newPseudoType(PseudoTypeKindBigInt, &PseudoTypeBase{})
	PseudoTypeBoolean   = newPseudoType(PseudoTypeKindBoolean, &PseudoTypeBase{})
	PseudoTypeFalse     = newPseudoType(PseudoTypeKindFalse, &PseudoTypeBase{})
	PseudoTypeTrue      = newPseudoType(PseudoTypeKindTrue, &PseudoTypeBase{})
)

type PseudoTypeDirect struct {
	PseudoTypeBase
	TypeNode ast.Handle
}

func NewPseudoTypeDirect(typeNode ast.Handle) *PseudoType {
	return newPseudoType(PseudoTypeKindDirect, &PseudoTypeDirect{TypeNode: typeNode})
}
func (t *PseudoType) AsPseudoTypeDirect() *PseudoTypeDirect {
	return t.data.(*PseudoTypeDirect)
}

type PseudoTypeInferred struct {
	PseudoTypeBase
	Expression        ast.Handle
	ErrorNodes        []ast.Handle
	IsSignatureReturn bool
}

func NewPseudoTypeInferred(expr ast.Handle, isSignatureReturn bool) *PseudoType {
	return newPseudoType(PseudoTypeKindInferred, &PseudoTypeInferred{Expression: expr, IsSignatureReturn: isSignatureReturn})
}
func NewPseudoTypeInferredWithErrors(expr ast.Handle, isSignatureReturn bool, errorNodes []ast.Handle) *PseudoType {
	return newPseudoType(PseudoTypeKindInferred, &PseudoTypeInferred{Expression: expr, ErrorNodes: errorNodes, IsSignatureReturn: isSignatureReturn})
}
func (t *PseudoType) AsPseudoTypeInferred() *PseudoTypeInferred {
	return t.data.(*PseudoTypeInferred)
}

type PseudoTypeNoResult struct {
	PseudoTypeBase
	Declaration ast.Handle
}

func NewPseudoTypeNoResult(decl ast.Handle) *PseudoType {
	return newPseudoType(PseudoTypeKindNoResult, &PseudoTypeNoResult{Declaration: decl})
}
func (t *PseudoType) AsPseudoTypeNoResult() *PseudoTypeNoResult {
	return t.data.(*PseudoTypeNoResult)
}

type PseudoTypeMaybeConstLocation struct {
	PseudoTypeBase
	Node        ast.Handle
	ConstType   *PseudoType
	RegularType *PseudoType
}

func NewPseudoTypeMaybeConstLocation(loc ast.Handle, ct *PseudoType, reg *PseudoType) *PseudoType {
	return newPseudoType(PseudoTypeKindMaybeConstLocation, &PseudoTypeMaybeConstLocation{Node: loc, ConstType: ct, RegularType: reg})
}
func (t *PseudoType) AsPseudoTypeMaybeConstLocation() *PseudoTypeMaybeConstLocation {
	return t.data.(*PseudoTypeMaybeConstLocation)
}

type PseudoTypeUnion struct {
	PseudoTypeBase
	Types []*PseudoType
}

func NewPseudoTypeUnion(types []*PseudoType) *PseudoType {
	return newPseudoType(PseudoTypeKindUnion, &PseudoTypeUnion{Types: types})
}
func (t *PseudoType) AsPseudoTypeUnion() *PseudoTypeUnion {
	return t.data.(*PseudoTypeUnion)
}

type PseudoParameter struct {
	Rest     bool
	Name     ast.Handle
	Optional bool
	Type     *PseudoType
}

func NewPseudoParameter(isRest bool, name ast.Handle, isOptional bool, t *PseudoType) *PseudoParameter {
	return &PseudoParameter{Rest: isRest, Name: name, Optional: isOptional, Type: t}
}

type PseudoTypeSingleCallSignature struct {
	PseudoTypeBase
	Signature      ast.Handle
	Parameters     []*PseudoParameter
	TypeParameters []ast.Handle
	ReturnType     *PseudoType
}

func NewPseudoTypeSingleCallSignature(signature ast.Handle, parameters []*PseudoParameter, typeParameters []ast.Handle, returnType *PseudoType) *PseudoType {
	return newPseudoType(PseudoTypeKindSingleCallSignature, &PseudoTypeSingleCallSignature{Signature: signature, Parameters: parameters, TypeParameters: typeParameters, ReturnType: returnType})
}
func (t *PseudoType) AsPseudoTypeSingleCallSignature() *PseudoTypeSingleCallSignature {
	return t.data.(*PseudoTypeSingleCallSignature)
}

type PseudoTypeTuple struct {
	PseudoTypeBase
	Elements []*PseudoType
}

func NewPseudoTypeTuple(elements []*PseudoType) *PseudoType {
	return newPseudoType(PseudoTypeKindTuple, &PseudoTypeTuple{Elements: elements})
}
func (t *PseudoType) AsPseudoTypeTuple() *PseudoTypeTuple {
	return t.data.(*PseudoTypeTuple)
}

type PseudoObjectElement struct {
	Name     ast.Handle
	Optional bool
	Kind     PseudoObjectElementKind
	data     pseudoObjectElementData
}

func (e *PseudoObjectElement) AsPseudoObjectElement() *PseudoObjectElement {
	return e
}
func (e *PseudoObjectElement) Signature() ast.Handle {
	switch e.Kind {
	case PseudoObjectElementKindMethod:
		return e.AsPseudoObjectMethod().Signature
	case PseudoObjectElementKindSetAccessor:
		return e.AsPseudoSetAccessor().Signature
	case PseudoObjectElementKindGetAccessor:
		return e.AsPseudoGetAccessor().Signature
	default:
		return ast.Handle{}
	}
}

type PseudoObjectElementKind int8

const (
	PseudoObjectElementKindMethod PseudoObjectElementKind = iota
	PseudoObjectElementKindPropertyAssignment
	PseudoObjectElementKindSetAccessor
	PseudoObjectElementKindGetAccessor
)

type pseudoObjectElementData interface{ AsPseudoObjectElement() *PseudoObjectElement }

func newPseudoObjectElement(kind PseudoObjectElementKind, name ast.Handle, optional bool, data pseudoObjectElementData) *PseudoObjectElement {
	e := data.AsPseudoObjectElement()
	e.Kind = kind
	e.Name = name
	e.Optional = optional
	e.data = data
	return e
}

type PseudoObjectMethod struct {
	PseudoObjectElement
	Signature      ast.Handle
	TypeParameters []ast.Handle
	Parameters     []*PseudoParameter
	ReturnType     *PseudoType
}

func NewPseudoObjectMethod(signature ast.Handle, name ast.Handle, optional bool, typeParameters []ast.Handle, parameters []*PseudoParameter, returnType *PseudoType) *PseudoObjectElement {
	return newPseudoObjectElement(PseudoObjectElementKindMethod, name, optional, &PseudoObjectMethod{Signature: signature, TypeParameters: typeParameters, Parameters: parameters, ReturnType: returnType})
}
func (e *PseudoObjectElement) AsPseudoObjectMethod() *PseudoObjectMethod {
	return e.data.(*PseudoObjectMethod)
}

type PseudoPropertyAssignment struct {
	PseudoObjectElement
	Readonly bool
	Type     *PseudoType
}

func NewPseudoPropertyAssignment(readonly bool, name ast.Handle, optional bool, t *PseudoType) *PseudoObjectElement {
	return newPseudoObjectElement(PseudoObjectElementKindPropertyAssignment, name, optional, &PseudoPropertyAssignment{Readonly: readonly, Type: t})
}
func (e *PseudoObjectElement) AsPseudoPropertyAssignment() *PseudoPropertyAssignment {
	return e.data.(*PseudoPropertyAssignment)
}

type PseudoSetAccessor struct {
	PseudoObjectElement
	Signature ast.Handle
	Parameter *PseudoParameter
}

func NewPseudoSetAccessor(signature ast.Handle, name ast.Handle, optional bool, p *PseudoParameter) *PseudoObjectElement {
	return newPseudoObjectElement(PseudoObjectElementKindSetAccessor, name, optional, &PseudoSetAccessor{Signature: signature, Parameter: p})
}
func (e *PseudoObjectElement) AsPseudoSetAccessor() *PseudoSetAccessor {
	return e.data.(*PseudoSetAccessor)
}

type PseudoGetAccessor struct {
	PseudoObjectElement
	Signature ast.Handle
	Type      *PseudoType
}

func NewPseudoGetAccessor(signature ast.Handle, name ast.Handle, optional bool, t *PseudoType) *PseudoObjectElement {
	return newPseudoObjectElement(PseudoObjectElementKindGetAccessor, name, optional, &PseudoGetAccessor{Signature: signature, Type: t})
}
func (e *PseudoObjectElement) AsPseudoGetAccessor() *PseudoGetAccessor {
	return e.data.(*PseudoGetAccessor)
}

type PseudoTypeObjectLiteral struct {
	PseudoTypeBase
	Elements []*PseudoObjectElement
}

func NewPseudoTypeObjectLiteral(elements []*PseudoObjectElement) *PseudoType {
	return newPseudoType(PseudoTypeKindObjectLiteral, &PseudoTypeObjectLiteral{Elements: elements})
}
func (t *PseudoType) AsPseudoTypeObjectLiteral() *PseudoTypeObjectLiteral {
	return t.data.(*PseudoTypeObjectLiteral)
}

type PseudoTypeLiteral struct {
	PseudoTypeBase
	Node ast.Handle
}

func NewPseudoTypeStringLiteral(node ast.Handle) *PseudoType {
	return newPseudoType(PseudoTypeKindStringLiteral, &PseudoTypeLiteral{Node: node})
}
func NewPseudoTypeNumericLiteral(node ast.Handle) *PseudoType {
	return newPseudoType(PseudoTypeKindNumericLiteral, &PseudoTypeLiteral{Node: node})
}
func NewPseudoTypeBigIntLiteral(node ast.Handle) *PseudoType {
	return newPseudoType(PseudoTypeKindBigIntLiteral, &PseudoTypeLiteral{Node: node})
}
func (t *PseudoType) AsPseudoTypeLiteral() *PseudoTypeLiteral {
	return t.data.(*PseudoTypeLiteral)
}
