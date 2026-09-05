package printer

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/evaluator"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
)

type SymbolAccessibility int32

const (
	SymbolAccessibilityAccessible SymbolAccessibility = iota
	SymbolAccessibilityNotAccessible
	SymbolAccessibilityCannotBeNamed
	SymbolAccessibilityNotResolved
)

type SymbolAccessibilityResult struct {
	Accessibility        SymbolAccessibility
	AliasesToMakeVisible []ast.Handle
	ErrorSymbolName string
	ErrorNode       ast.Handle
	ErrorModuleName string
}

type TypeReferenceSerializationKind int32

const (
	TypeReferenceSerializationKindUnknown = iota
	TypeReferenceSerializationKindTypeWithConstructSignatureAndValue
	TypeReferenceSerializationKindVoidNullableOrNeverType
	TypeReferenceSerializationKindNumberLikeType
	TypeReferenceSerializationKindBigIntLikeType
	TypeReferenceSerializationKindStringLikeType
	TypeReferenceSerializationKindBooleanType
	TypeReferenceSerializationKindArrayLikeType
	TypeReferenceSerializationKindESSymbolType
	TypeReferenceSerializationKindPromise
	TypeReferenceSerializationKindTypeWithCallSignature
	TypeReferenceSerializationKindObjectType
)

type EmitResolver interface {
	binder.ReferenceResolver
	IsReferencedAliasDeclaration(node ast.Handle) bool
	IsValueAliasDeclaration(node ast.Handle) bool
	IsTopLevelValueImportEqualsWithEntityName(node ast.Handle) bool
	MarkLinkedReferencesRecursively(file *ast.SourceFile)
	GetExternalModuleFileFromDeclaration(node ast.Handle) *ast.SourceFile
	GetEffectiveDeclarationFlags(node ast.Handle, flags ast.ModifierFlags) ast.ModifierFlags
	GetResolutionModeOverride(node ast.Handle) core.ResolutionMode
	GetTypeReferenceSerializationKind(name ast.Handle, serialScope ast.Handle) TypeReferenceSerializationKind
	GetConstantValue(node ast.Handle) any
	GetJsxFactoryEntity(location ast.Handle) ast.Handle
	GetJsxFragmentFactoryEntity(location ast.Handle) ast.Handle
	SetReferencedImportDeclaration(node ast.Handle, ref ast.Handle)
	PrecalculateDeclarationEmitVisibility(file *ast.SourceFile)
	IsSymbolAccessible(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags, shouldComputeAliasToMarkVisible bool) SymbolAccessibilityResult
	IsEntityNameVisible(entityName ast.Handle, enclosingDeclaration ast.Handle) SymbolAccessibilityResult
	IsExpandoFunctionDeclaration(node ast.Handle) bool
	IsExpandoFunctionDeclarationUnsafe(node ast.Handle) bool
	IsLiteralConstDeclaration(node ast.Handle) bool
	RequiresAddingImplicitUndefined(node ast.Handle, symbol *ast.Symbol, enclosingDeclaration ast.Handle) bool
	IsDeclarationVisible(node ast.Handle) bool
	IsNameResolvable(location ast.Handle, name string) bool
	IsImportRequiredByAugmentation(decl ast.Handle) bool
	IsDefinitelyReferenceToGlobalSymbolObject(node ast.Handle) bool
	IsImplementationOfOverload(node ast.Handle) bool
	GetEnumMemberValue(node ast.Handle) evaluator.Result
	IsLateBound(node ast.Handle) bool
	IsOptionalParameter(node ast.Handle) bool
	IsThisPropertyAssignmentDeclarationRedundant(node ast.Handle) bool
	GetPropertiesOfContainerFunction(node ast.Handle) []*ast.Symbol
	RequiresAddingImplicitUndefinedUnsafe(node ast.Handle, symbol *ast.Symbol, enclosingDeclaration ast.Handle) bool
	GetReferencedValueDeclarationUnsafe(node ast.Handle) ast.Handle
	CreateTypeOfDeclaration(emitContext *EmitContext, declaration ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle
	CreateReturnTypeOfSignatureDeclaration(emitContext *EmitContext, signatureDeclaration ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle
	CreateTypeParametersOfSignatureDeclaration(emitContext *EmitContext, signatureDeclaration ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) []ast.Handle
	CreateLiteralConstValue(emitContext *EmitContext, node ast.Handle, tracker nodebuilder.SymbolTracker) ast.Handle
	CreateTypeOfExpression(emitContext *EmitContext, expression ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle
	CreateLateBoundIndexSignatures(emitContext *EmitContext, container ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) []ast.Handle
	TryJSTypeNodeToTypeNode(emitContext *EmitContext, typeNode ast.Handle, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle
}
