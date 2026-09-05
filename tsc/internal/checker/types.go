package checker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/evaluator"
	"math/bits"
	"slices"
	"strings"
)

type ParseFlags uint32

const (
	ParseFlagsNone                   ParseFlags = 0
	ParseFlagsYield                  ParseFlags = 1 << 0
	ParseFlagsAwait                  ParseFlags = 1 << 1
	ParseFlagsType                   ParseFlags = 1 << 2
	ParseFlagsIgnoreMissingOpenBrace ParseFlags = 1 << 4
	ParseFlagsJSDoc                  ParseFlags = 1 << 5
)

type SignatureKind int32

const (
	SignatureKindCall SignatureKind = iota
	SignatureKindConstruct
)

type MemberOverrideStatus int32

const (
	MemberOverrideStatusNone MemberOverrideStatus = iota
	MemberOverrideStatusNeedsOverride
	MemberOverrideStatusHasInvalidOverride
)

type ContextFlags uint32

const (
	ContextFlagsNone                 ContextFlags = 0
	ContextFlagsSignature            ContextFlags = 1 << 0
	ContextFlagsNoConstraints        ContextFlags = 1 << 1
	ContextFlagsIgnoreNodeInferences ContextFlags = 1 << 2
	ContextFlagsSkipBindingPatterns  ContextFlags = 1 << 3
)

type TypeFormatFlags uint32

const (
	TypeFormatFlagsNone                                TypeFormatFlags = 0
	TypeFormatFlagsNoTruncation                        TypeFormatFlags = 1 << 0
	TypeFormatFlagsWriteArrayAsGenericType             TypeFormatFlags = 1 << 1
	TypeFormatFlagsGenerateNamesForShadowedTypeParams  TypeFormatFlags = 1 << 2
	TypeFormatFlagsUseStructuralFallback               TypeFormatFlags = 1 << 3
	TypeFormatFlagsWriteTypeArgumentsOfSignature       TypeFormatFlags = 1 << 5
	TypeFormatFlagsUseFullyQualifiedType               TypeFormatFlags = 1 << 6
	TypeFormatFlagsSuppressAnyReturnType               TypeFormatFlags = 1 << 8
	TypeFormatFlagsMultilineObjectLiterals             TypeFormatFlags = 1 << 10
	TypeFormatFlagsWriteClassExpressionAsTypeLiteral   TypeFormatFlags = 1 << 11
	TypeFormatFlagsUseTypeOfFunction                   TypeFormatFlags = 1 << 12
	TypeFormatFlagsOmitParameterModifiers              TypeFormatFlags = 1 << 13
	TypeFormatFlagsUseAliasDefinedOutsideCurrentScope  TypeFormatFlags = 1 << 14
	TypeFormatFlagsUseSingleQuotesForStringLiteralType TypeFormatFlags = 1 << 28
	TypeFormatFlagsNoTypeReduction                     TypeFormatFlags = 1 << 29
	TypeFormatFlagsUseInstantiationExpressions         TypeFormatFlags = 1 << 30
	TypeFormatFlagsOmitThisParameter                   TypeFormatFlags = 1 << 25
	TypeFormatFlagsWriteCallStyleSignature             TypeFormatFlags = 1 << 27
	TypeFormatFlagsAllowUniqueESSymbolType             TypeFormatFlags = 1 << 20
	TypeFormatFlagsAddUndefined                        TypeFormatFlags = 1 << 17
	TypeFormatFlagsWriteArrowStyleSignature            TypeFormatFlags = 1 << 18
	TypeFormatFlagsInArrayType                         TypeFormatFlags = 1 << 19
	TypeFormatFlagsInElementType                       TypeFormatFlags = 1 << 21
	TypeFormatFlagsInFirstTypeArgument                 TypeFormatFlags = 1 << 22
	TypeFormatFlagsInTypeAlias                         TypeFormatFlags = 1 << 23
	TypeFormatFlagsNodeBuilderFlagsMask                                = TypeFormatFlagsNoTruncation | TypeFormatFlagsWriteArrayAsGenericType | TypeFormatFlagsGenerateNamesForShadowedTypeParams | TypeFormatFlagsUseStructuralFallback | TypeFormatFlagsWriteTypeArgumentsOfSignature | TypeFormatFlagsUseFullyQualifiedType | TypeFormatFlagsSuppressAnyReturnType | TypeFormatFlagsMultilineObjectLiterals | TypeFormatFlagsWriteClassExpressionAsTypeLiteral | TypeFormatFlagsUseTypeOfFunction | TypeFormatFlagsOmitParameterModifiers | TypeFormatFlagsUseAliasDefinedOutsideCurrentScope | TypeFormatFlagsAllowUniqueESSymbolType | TypeFormatFlagsInTypeAlias | TypeFormatFlagsUseInstantiationExpressions | TypeFormatFlagsUseSingleQuotesForStringLiteralType | TypeFormatFlagsNoTypeReduction | TypeFormatFlagsOmitThisParameter
)

type SymbolFormatFlags uint32

const (
	SymbolFormatFlagsNone                               SymbolFormatFlags = 0
	SymbolFormatFlagsWriteTypeParametersOrArguments     SymbolFormatFlags = 1 << 0
	SymbolFormatFlagsUseOnlyExternalAliasing            SymbolFormatFlags = 1 << 1
	SymbolFormatFlagsAllowAnyNodeKind                   SymbolFormatFlags = 1 << 2
	SymbolFormatFlagsUseAliasDefinedOutsideCurrentScope SymbolFormatFlags = 1 << 3
	SymbolFormatFlagsWriteComputedProps                 SymbolFormatFlags = 1 << 4
	SymbolFormatFlagsDoNotIncludeSymbolChain            SymbolFormatFlags = 1 << 5
)

type ExternalEmitHelpers uint32

const (
	ExternalEmitHelpersRest ExternalEmitHelpers = 1 << iota
	ExternalEmitHelpersDecorate
	ExternalEmitHelpersMetadata
	ExternalEmitHelpersParam
	ExternalEmitHelpersAwaiter
	ExternalEmitHelpersAwait
	ExternalEmitHelpersAsyncGenerator
	ExternalEmitHelpersAsyncDelegator
	ExternalEmitHelpersAsyncValues
	ExternalEmitHelpersExportStar
	ExternalEmitHelpersImportStar
	ExternalEmitHelpersImportDefault
	ExternalEmitHelpersMakeTemplateObject
	ExternalEmitHelpersClassPrivateFieldGet
	ExternalEmitHelpersClassPrivateFieldSet
	ExternalEmitHelpersClassPrivateFieldIn
	ExternalEmitHelpersSetFunctionName
	ExternalEmitHelpersPropKey
	ExternalEmitHelpersAddDisposableResourceAndDisposeResources
	ExternalEmitHelpersRewriteRelativeImportExtension
	ExternalEmitHelpersESDecorateAndRunInitializers = ExternalEmitHelpersDecorate
	ExternalEmitHelpersFirstEmitHelper              = ExternalEmitHelpersRest
	ExternalEmitHelpersLastEmitHelper               = ExternalEmitHelpersRewriteRelativeImportExtension
	ExternalEmitHelpersForAwaitOfIncludes           = ExternalEmitHelpersAsyncValues
	ExternalEmitHelpersAsyncGeneratorIncludes       = ExternalEmitHelpersAwait | ExternalEmitHelpersAsyncGenerator
	ExternalEmitHelpersAsyncDelegatorIncludes       = ExternalEmitHelpersAwait | ExternalEmitHelpersAsyncDelegator | ExternalEmitHelpersAsyncValues
)
const externalHelpersModuleNameText = "tslib"

type (
	TypeId      uint32
	SignatureId uint32
)
type SymbolReferenceLinks struct {
	referenceKinds ast.SymbolFlags
}
type ValueSymbolLinks struct {
	resolvedType                 *Type
	writeType                    *Type
	target                       *ast.Symbol
	mapper                       *TypeMapper
	nameType                     *Type
	containingType               *Type
	functionOrConstructorChecked bool
}
type MappedSymbolLinks struct {
	keyType         *Type
	syntheticOrigin *ast.Symbol
}
type DeferredSymbolLinks struct {
	parent            *Type
	constituents      []*Type
	writeConstituents []*Type
}

//go:generate go tool golang.org/x/tools/cmd/stringer -type=SignatureKind -output=stringer_generated.go
	//go:generate npx dprint fmt stringer_generated.go
	// ParseFlags
	// Obtaining contextual signature
	// Don't obtain type variable constraints
	// Ignore inference to current node and parent nodes out to the containing call for, for example, completions
	// Ignore contextual types applied by binding patterns
	// Don't truncate typeToString result
	// Write Array<T> instead T[]
	// When a type parameter T is shadowing another T, generate a name for it so it can still be referenced
	// When an alias cannot be named by its symbol, rather than report an error, fallback to a structural printout if possible
	// hole because there's a hole in node builder flags
	// Write the type arguments instead of type parameters of the signature
	// Write out the fully qualified type name (eg. Module.Type(), instead of Type)
	// hole because `UseOnlyExternalAliasing` is here in node builder flags, but functions which take old flags use `SymbolFormatFlags` instead
	// If the return type is any-like, don't offer a return type.
	// hole because `WriteTypeParametersInQualifiedName` is here in node builder flags, but functions which take old flags use `SymbolFormatFlags` for this instead
	// Always print object literals across multiple lines (only used to map into node builder flags)
	// Write a type literal instead of (Anonymous class)
	// Write typeof instead of function type literal
	// Omit modifiers on parameters
	// For a `type T = ... ` defined in a different file, write `T` instead of its value, even though `T` can't be accessed in the current scope.
	// Use single quotes for string literal type
	// Don't call getReducedType
	// Use instantiation expressions for qualified instantiated names like Foo<string>.Bar
	// Write construct signatures as call style signatures
	// Error Handling
	// This is bit 20 to align with the same bit in `NodeBuilderFlags`
	// TypeFormatFlags exclusive
	// Add undefined to types of initialized, non-optional parameters
	// Write arrow style signature
	// State
	// Writing an array element type
	// Writing an array or union element type
	// Writing first type argument of the instantiated type
	// Writing type in type alias declaration
	// Write symbols's type argument if it is instantiated symbol
	// eg. class C<T> { p: T }   <-- Show p as C<T>.p here
	//     var a: C<number>;
	//     var p = a.p; <--- Here p is property of C<number> so show it as C<number>.p instead of just C.p
	// Use only external alias information to get the symbol name in the given context
	// eg.  module m { export class c { } } import x = m.c;
	// When this flag is specified m.c will be used to refer to the class instead of alias symbol x
	// Build symbol name using any nodes needed, instead of just components of an entity name
	// Prefer aliases which are not directly visible
	// { [E.A]: 1 }
	/** @internal */ // Skip building an accessible symbol chain
	/** @internal */ // __rest (used by ESNext object rest transformation)
	// __decorate (used by TypeScript decorators transformation)
	// __metadata (used by TypeScript decorators transformation)
	// __param (used by TypeScript decorators transformation)
	// __awaiter (used by ES2017 async functions transformation)
	// __await (used by ES2017 async generator transformation)
	// __asyncGenerator (used by ES2017 async generator transformation)
	// __asyncDelegator (used by ES2017 async generator yield* transformation)
	// __asyncValues (used by ES2017 for..await..of transformation)
	// __exportStar (used by CommonJS/AMD/UMD module transformation)
	// __importStar (used by CommonJS/AMD/UMD module transformation)
	// __importDefault (used by CommonJS/AMD/UMD module transformation)
	// __makeTemplateObject (used for constructing template string array objects)
	// __classPrivateFieldGet (used by the class private field transformation)
	// __classPrivateFieldSet (used by the class private field transformation)
	// __classPrivateFieldIn (used by the class private field transformation)
	// __setFunctionName (used by class fields and ECMAScript decorators)
	// __propKey (used by class fields and ECMAScript decorators)
	// __addDisposableResource and __disposeResources (used by ESNext transformations)
	// __rewriteRelativeImportExtension (used by --rewriteRelativeImportExtensions)
	// __esDecorate and __runInitializers (used by ECMAScript decorators transformation)
	// Helpers included by ES2017 for..await..of
	// Helpers included by ES2017 async generators
	// Helpers included by yield* in ES2017 async generators
	// Ids
	// Links for referenced symbols
	// Flags for the meanings of the symbol that were referenced
	// Links for value symbols
	// Type of value symbol
	// Mapped type for mapped type property, containing union or intersection type for synthetic property
	// Additional links for mapped symbols
	// Key type for mapped type member
	// For a property on a mapped or spread type, points back to the original property
	// Additional links for deferred type symbols
	// Source union/intersection of a deferred type
	// Calculated list of constituents for a deferred type
	// Constituents of a deferred `writeType`
	// Links for alias symbols
	// Immediate target of an alias. May be another alias. Do not access directly, use `checker.getImmediateAliasedSymbol` instead.
	// Resolved (non-alias) target of an alias
	// True if alias symbol has been referenced as a value that can be emitted
	// First resolved alias declaration that makes the symbol only usable in type constructs
	// Links for module symbols
	// Resolved exports of module or combined early- and late-bound static members of a class.
	// Set on a module symbol when some of its exports were resolved through a 'export type * from "mod"' declaration
	// References a mapped type
	// References an index type
	// Links for late-bound symbols
	// Links for export type symbols
	// Target symbol
	// Import declaration which produced the symbol, present if the symbol is marked as uncallable but had call signatures in `resolveESModuleSymbol`
	// Links for type aliases
	// Type parameters of type alias (undefined if non-generic)
	// Instantiations of generic type alias (undefined if non-generic)
	// Links for declared types (type parameters, class types, interface types, enums)
	// Links for switch clauses
	// Exhaustive state not computed
	// Exhaustive state computation in progress
	// Switch statement is not exhaustive
	// Switch statement is exhaustive
	// Switch statement exhaustiveness
	// Index of first spread expression (or -1 if none)
	// Index of last spread expression (or -1 if none)
	// Links for late-binding containers
	// Indexed by MembersOrExportsResolutionKind
	// Links for synthetic spread properties
	// Left source for synthetic spread property
	// Right source for synthetic spread property
	// Links for variances of type aliases and interface types
	// Neither covariant nor contravariant
	// Covariant
	// Contravariant
	// Both covariant and contravariant
	// Unwitnessed type parameter
	// Mask containing all measured variances without the unmeasurable flag
	// Variance result is unusable - relationship relies on structural comparisons which are not reflected in generic relationships
	// Variance result is unreliable - checking may produce false negatives, but not false positives
	// Symbol is definitely assigned somewhere
	// Symbols of nodes which which logically contain this one, cached by file the request is made within
	// Containers (other than the parent) which this symbol is aliased in
	// Node has been type checked
	// Contextual types have been assigned
	// Values for enum members have been computed, and any errors have been reported for them.
	// Parameter assignments have been marked
	// Marked on all block-scoped containers containing a class with private identifiers.
	// Marked on all block-scoped containers containing a static initializer with 'super.x' or 'super[x]'.
	// Common links
	// Set of flags specific to Node
	// Set by `useOuterVariableScopeInParameter` in checker when downlevel emit would change the name resolution scope inside of a parameter.
	// Cache boolean if we report statements in ambient context
	// Resolved symbol associated with node
	// Resolved type associated with node
	// Outer type parameters of anonymous object type
	// If the node has a computable name
	// Resolved name associated with the type of the node
	// Links for enum members
	// Constant value of enum member
	// Links for assertion expressions
	// Assertion expression type
	// SourceFile links
	// Signature specific links
	// Cached signature of signature node or call expression
	// Signature with possible control flow effects
	// Signature for decorator as if invoked by the runtime
	// Note that for types of different kinds, the numeric values of TypeFlags determine the order
	// computed by the CompareTypes function and therefore the order of constituent types in union types.
	// Since union type processing often bails out early when a result is known, it is important to order
	// TypeFlags in increasing order of potential type complexity. In particular, indexed access and
	// conditional types should sort last as those types are potentially recursive and possibly infinite.
	// Type of symbol primitive introduced in ES6
	// unique symbol
	// Always combined with StringLiteral, NumberLiteral, or Union
	// Numeric computed enum member value (must be right after EnumLiteral, see getSortOrderFlags)
	// intrinsic object type
	// Never type
	// Type parameter
	// Object type
	// keyof T
	// Template literal type
	// Uppercase/Lowercase type
	// Type parameter substitution
	// T[K]
	// T extends U ? X : Y
	// Union (T | U)
	// Intersection (T & U)
	// Used by union/intersection type construction
	// Used by union/intersection type construction
	// 'TypeFlagsNarrowable' types are types where narrowing actually narrows.
	// This *should* be every type other than null, undefined, void, and never
	// The following flags are aggregated during union and intersection type construction
	// The following flags are used for different purposes during union and intersection type construction
	// FormatTypeFlags returns the individual flag names as a slice of strings.
	// String returns a pipe-separated string of flag names.
	// Types included in TypeFlags.ObjectFlagsType have an objectFlags property. Some ObjectFlags

	// are specific to certain types and reuse the same bit position. Those ObjectFlags require a check
	// for a certain TypeFlags value to determine their meaning.
	// Class
	// Interface
	// Generic type reference
	// Synthesized generic tuple type
	// Anonymous
	// Mapped
	// Instantiated anonymous or mapped type
	// Originates in an object literal
	// Evolving array type
	// Object literal pattern with computed properties
	// Object contains a property from a reverse-mapped type
	// Jsx attributes type
	// Object type declared in JS - disables errors on read/write of nonexisting members
	// Fresh object literal
	// Originates in an array literal
	// Union of only primitive types
	// Type is or contains undefined or null widening type
	// Type is or contains object literal type
	// Type is or contains anyFunctionType or silentNeverType
	// CouldContainTypeVariables flag has been computed
	// Type could contain a type variable
	// Members have been resolved
	// Object flags that uniquely identify the kind of ObjectType
	// Flags that require TypeFlags.Object
	// Object literal contains spread operation
	// Originates in object rest declaration
	// Originates in instantiation expression
	// A single signature type extracted from a potentially broader type
	// Type is a clone of a class instance type
	// Flags that require TypeFlags.Object and ObjectFlags.Reference
	// has had `getSingleBaseForNonAugmentingSubtype` invoked on it already
	// has a defined cachedEquivalentBaseType member
	// Member resolution in process
	// Originates in resolution of AST type node
	// Flags that require TypeFlags.UnionOrIntersection or TypeFlags.Substitution
	// IsGenericObjectType flag has been computed
	// Union or intersection contains generic object type
	// Union or intersection contains generic index type
	// Flags that require TypeFlags.Union
	// Union contains intersections
	// IsUnknownLikeUnion flag has been computed
	// Union of null, undefined, and empty object type
	// IsUniformEnum flag has been computed
	// Union contains uniform literal types
	// Flags that require TypeFlags.Intersection
	// IsNeverLike flag has been computed
	// Intersection reduces to never
	// T & C, where T's constraint and C are primitives, object, or {}
	// TypeAlias
	// Type
	// Type specific data
	// Casts for concrete struct types
	// Casts for embedded struct types
	// Common accessors
	// TypeData
	// TypeBase
	// IntrinsicTypeData
	// LiteralTypeData
	// string | jsnum.Number | bool | PseudoBigInt | nil (computed enum)
	// Fresh version of type
	// Regular version of type
	// UniqueESSymbolTypeData
	// ConstrainedType (type with computed base constraint)
	// StructuredType (base of all types with members)
	// Signatures (call + construct)
	// Count of call signatures
	// Except for tuple type references and reverse mapped types, all object types have an associated symbol.
	// Possible object type instances are listed in the following.
	// InterfaceType:
	// ObjectFlagsClass: Originating non-generic class type
	// ObjectFlagsClass|ObjectFlagsReference: Originating generic class type
	// ObjectFlagsInterface: Originating non-generic interface type
	// ObjectFlagsInterface|ObjectFlagsReference: Originating generic interface type
	// TupleType:
	// ObjectFlagsReference|ObjectFlagsTuple: Originating generic tuple type (synthesized)
	// TypeReference
	// ObjectFlagsReference: Instantiated generic class, interface, or tuple type
	// ObjectType:
	// ObjectFlagsAnonymous: Originating anonymous object type
	// ObjectFlagsAnonymous|ObjectFlagsInstantiated: Instantiated anonymous object type
	// MappedType:
	// ObjectFlagsMapped: Originating mapped type
	// ObjectFlagsMapped|ObjectFlagsInstantiated: Instantiated mapped type
	// InstantiationExpressionType:
	// ObjectFlagsAnonymous|ObjectFlagsInstantiationExpression: Originating instantiation expression type
	// ObjectFlagsAnonymous|ObjectFlagsInstantiated|ObjectFlagsInstantiationExpression: Instantiated instantiation expression type
	// ReverseMappedType:
	// ObjectFlagsAnonymous|ObjectFlagsReverseMapped: Reverse mapped type
	// EvolvingArrayType:
	// ObjectFlagsEvolvingArray: Evolving array type
	// Target of instantiated type
	// Type mapper for instantiated type
	// Map of type instantiations
	// TypeReference (instantiation of an InterfaceType)
	// TypeReferenceNode | ArrayTypeNode | TupleTypeNode when deferred, else nil
	// InterfaceType (when generic, serves as reference to instantiation of itself)
	// Type parameters (outer + local + thisType)
	// Count of outer type parameters
	// The "this" type (nil if none)
	// Declared members
	// Declared call signatures
	// Declared construct signatures
	// Declared index signatures
	// TupleType
	// T
	// T?
	// ...T[]
	// ...T
	// NamedTupleMember | ParameterDeclaration | nil
	// Number of required or variadic elements
	// Number of initial required or optional elements
	// InstantiationExpressionType
	// MappedType
	// ReverseMappedType
	// EvolvingArrayType
	// UnionOrIntersectionTypeData
	// UnionType
	// Denormalized union, intersection, or index type in which union originates
	// Property with unique unit type that exists in every object/intersection in union type
	// Constituents keyed by unit type discriminants
	// IntersectionType
	// Instantiation with type parameters mapped to never type
	// TypeParameter
	// IndexFlags
	// IndexType
	// IndexedAccessType
	// Only includes AccessFlags.Persistent
	// Always one element longer than types
	// Always at least one element
	// Target type
	// Constraint that target type is known to satisfy
	// The `trueType` instantiated with the `combinedMapper`, if present
	// SignatureFlags
	// Propagating flags
	// Indicates last parameter is rest parameter
	// Indicates signature is specialized
	// Indicates signature is a construct signature
	// Indicates signature comes from an abstract class, abstract construct signature, or abstract constructor type
	// Non-propagating flags
	// Indicates signature comes from a CallChain nested in an outer OptionalChain
	// Indicates signature comes from a CallChain that is the outermost chain of an optional expression
	// Indicates signature is from a js file and has no types
	// Indicates signature comes from a non-inferrable type
	// We do not propagate `IsInnerCallChain` or `IsOuterCallChain` to instantiated signatures, as that would result in us
	// attempting to add `| undefined` on each recursive call to `getReturnTypeOfSignature` when
	// instantiating the return type.
	// Signature
	// True for union, false for intersection
	// Individual signatures
	// IndexInfo
	// IndexSignatureDeclaration
	// Synthetic property symbol for this index signature
	// ElementWithComputedPropertyName
	/**
	 * Ternary values are defined such that
	 * x & y picks the lesser in the order False < Unknown < Maybe < True, and
	 * x | y picks the greater in the order False < Unknown < Maybe < True.
	 * Generally, Ternary.Maybe is used as the result of a relation that depends on itself, and
	 * Ternary.Unknown is used as the result of a variance check that depends on itself. We make
	 * a distinction because we don't want to cache circular variance check results.
	 */ // Aliases for types
type AliasSymbolLinks struct {
	immediateTarget     *ast.Symbol
	aliasTarget         *ast.Symbol
	referenced          bool
	typeOnlyDeclaration ast.Handle
}
type ModuleSymbolLinks struct {
	resolvedExports       ast.SymbolTable
	typeOnlyExportStarMap map[string]ast.Handle
	exportsChecked        bool
}
type ReverseMappedSymbolLinks struct {
	propertyType   *Type
	mappedType     *Type
	constraintType *Type
}
type LateBoundLinks struct{ lateSymbol *ast.Symbol }
type ExportTypeLinks struct {
	target            *ast.Symbol
	originatingImport ast.Handle
}
type TypeAliasLinks struct {
	declaredType                  *Type
	typeParameters                []*Type
	instantiations                map[CacheHashKey]*Type
	isConstructorDeclaredProperty bool
}
type DeclaredTypeLinks struct {
	declaredType           *Type
	interfaceChecked       bool
	indexSignaturesChecked bool
	typeParametersChecked  bool
	enumChecked            bool
}
type ExhaustiveState byte

const (
	ExhaustiveStateUnknown ExhaustiveState = iota
	ExhaustiveStateComputing
	ExhaustiveStateFalse
	ExhaustiveStateTrue
)

type SwitchStatementLinks struct {
	exhaustiveState     ExhaustiveState
	switchTypesComputed bool
	witnessesComputed   bool
	switchTypes         []*Type
	witnesses           []string
}
type ArrayLiteralLinks struct {
	indicesComputed  bool
	firstSpreadIndex int
	lastSpreadIndex  int
}
type MembersOrExportsResolutionKind int

const (
	MembersOrExportsResolutionKindResolvedExports MembersOrExportsResolutionKind = 0
	MembersOrExportsResolutionKindResolvedMembers MembersOrExportsResolutionKind = 1
)

type MembersAndExportsLinks [2]ast.SymbolTable
type SpreadLinks struct {
	leftSpread  *ast.Symbol
	rightSpread *ast.Symbol
}
type VarianceLinks struct{ variances []VarianceFlags }
type VarianceFlags uint32

const (
	VarianceFlagsInvariant                VarianceFlags = 0
	VarianceFlagsCovariant                VarianceFlags = 1 << 0
	VarianceFlagsContravariant            VarianceFlags = 1 << 1
	VarianceFlagsBivariant                VarianceFlags = VarianceFlagsCovariant | VarianceFlagsContravariant
	VarianceFlagsIndependent              VarianceFlags = 1 << 2
	VarianceFlagsVarianceMask             VarianceFlags = VarianceFlagsInvariant | VarianceFlagsCovariant | VarianceFlagsContravariant | VarianceFlagsIndependent
	VarianceFlagsUnmeasurable             VarianceFlags = 1 << 3
	VarianceFlagsUnreliable               VarianceFlags = 1 << 4
	VarianceFlagsAllowsStructuralFallback               = VarianceFlagsUnmeasurable | VarianceFlagsUnreliable
)

type MarkedAssignmentSymbolLinks struct {
	lastAssignmentPos     int32
	hasDefiniteAssignment bool
}
type accessibleChainCacheKey struct {
	useOnlyExternalAliasing bool
	location                ast.Handle
	meaning                 ast.SymbolFlags
}
type ContainingSymbolLinks struct {
	extendedContainersByFile map[ast.NodeId][]*ast.Symbol
	extendedContainers       *[]*ast.Symbol
	accessibleChainCache     map[accessibleChainCacheKey][]*ast.Symbol
}
type AccessFlags uint32

const (
	AccessFlagsNone                       AccessFlags = 0
	AccessFlagsIncludeUndefined           AccessFlags = 1 << 0
	AccessFlagsNoIndexSignatures          AccessFlags = 1 << 1
	AccessFlagsWriting                    AccessFlags = 1 << 2
	AccessFlagsCacheSymbol                AccessFlags = 1 << 3
	AccessFlagsAllowMissing               AccessFlags = 1 << 4
	AccessFlagsExpressionPosition         AccessFlags = 1 << 5
	AccessFlagsReportDeprecated           AccessFlags = 1 << 6
	AccessFlagsSuppressNoImplicitAnyError AccessFlags = 1 << 7
	AccessFlagsContextual                 AccessFlags = 1 << 8
	AccessFlagsPersistent                             = AccessFlagsIncludeUndefined
)

type NodeCheckFlags uint32

const (
	NodeCheckFlagsNone                                     NodeCheckFlags = 0
	NodeCheckFlagsTypeChecked                              NodeCheckFlags = 1 << 0
	NodeCheckFlagsContextChecked                           NodeCheckFlags = 1 << 6
	NodeCheckFlagsEnumValuesComputed                       NodeCheckFlags = 1 << 10
	NodeCheckFlagsAssignmentsMarked                        NodeCheckFlags = 1 << 17
	NodeCheckFlagsContainsClassWithPrivateIdentifiers      NodeCheckFlags = 1 << 20
	NodeCheckFlagsContainsSuperPropertyInStaticInitializer NodeCheckFlags = 1 << 21
	NodeCheckFlagsInCheckIdentifier                        NodeCheckFlags = 1 << 22
	NodeCheckFlagsInitializerIsUndefined                   NodeCheckFlags = 1 << 24
	NodeCheckFlagsInitializerIsUndefinedComputed           NodeCheckFlags = 1 << 25
)

type NodeLinks struct {
	flags                                NodeCheckFlags
	declarationRequiresScopeChange       core.Tristate
	hasReportedStatementInAmbientContext bool
}
type SymbolNodeLinks struct {
	resolvedSymbol *ast.Symbol
}
type TypeNodeLinks struct {
	resolvedType        *Type
	outerTypeParameters []*Type
}
type ComputedNameNodeLinks struct {
	hasName *bool
	name    string
}
type EnumMemberLinks struct {
	value evaluator.Result
}
type AssertionLinks struct {
	exprType *Type
}
type SourceFileLinks struct {
	typeChecked                  bool
	unusedChecked                bool
	externalHelpersModule        *ast.Symbol
	requestedExternalEmitHelpers ExternalEmitHelpers
	deferredNodes                collections.OrderedSet[ast.Handle]
	identifierCheckNodes         []ast.Handle
	localJsxNamespace            string
	localJsxFragmentNamespace    string
	localJsxFactory              ast.Handle
	localJsxFragmentFactory      ast.Handle
	jsxFactoryEntity             ast.Handle
	jsxFragmentFactoryEntity     ast.Handle
	jsxFragmentType              *Type
}
type SignatureLinks struct {
	resolvedSignature  *Signature
	effectsSignature   *Signature
	decoratorSignature *Signature
}
type TypeFlags uint32

const (
	TypeFlagsNone                            TypeFlags = 0
	TypeFlagsAny                             TypeFlags = 1 << 0
	TypeFlagsUnknown                         TypeFlags = 1 << 1
	TypeFlagsUndefined                       TypeFlags = 1 << 2
	TypeFlagsNull                            TypeFlags = 1 << 3
	TypeFlagsVoid                            TypeFlags = 1 << 4
	TypeFlagsString                          TypeFlags = 1 << 5
	TypeFlagsNumber                          TypeFlags = 1 << 6
	TypeFlagsBigInt                          TypeFlags = 1 << 7
	TypeFlagsBoolean                         TypeFlags = 1 << 8
	TypeFlagsESSymbol                        TypeFlags = 1 << 9
	TypeFlagsStringLiteral                   TypeFlags = 1 << 10
	TypeFlagsNumberLiteral                   TypeFlags = 1 << 11
	TypeFlagsBigIntLiteral                   TypeFlags = 1 << 12
	TypeFlagsBooleanLiteral                  TypeFlags = 1 << 13
	TypeFlagsUniqueESSymbol                  TypeFlags = 1 << 14
	TypeFlagsEnumLiteral                     TypeFlags = 1 << 15
	TypeFlagsEnum                            TypeFlags = 1 << 16
	TypeFlagsNonPrimitive                    TypeFlags = 1 << 17
	TypeFlagsNever                           TypeFlags = 1 << 18
	TypeFlagsTypeParameter                   TypeFlags = 1 << 19
	TypeFlagsObject                          TypeFlags = 1 << 20
	TypeFlagsIndex                           TypeFlags = 1 << 21
	TypeFlagsTemplateLiteral                 TypeFlags = 1 << 22
	TypeFlagsStringMapping                   TypeFlags = 1 << 23
	TypeFlagsSubstitution                    TypeFlags = 1 << 24
	TypeFlagsIndexedAccess                   TypeFlags = 1 << 25
	TypeFlagsConditional                     TypeFlags = 1 << 26
	TypeFlagsUnion                           TypeFlags = 1 << 27
	TypeFlagsIntersection                    TypeFlags = 1 << 28
	TypeFlagsReserved1                       TypeFlags = 1 << 29
	TypeFlagsReserved2                       TypeFlags = 1 << 30
	TypeFlagsReserved3                       TypeFlags = 1 << 31
	TypeFlagsAnyOrUnknown                              = TypeFlagsAny | TypeFlagsUnknown
	TypeFlagsNullable                                  = TypeFlagsUndefined | TypeFlagsNull
	TypeFlagsLiteral                                   = TypeFlagsStringLiteral | TypeFlagsNumberLiteral | TypeFlagsBigIntLiteral | TypeFlagsBooleanLiteral
	TypeFlagsUnit                                      = TypeFlagsEnum | TypeFlagsLiteral | TypeFlagsUniqueESSymbol | TypeFlagsNullable
	TypeFlagsFreshable                                 = TypeFlagsEnum | TypeFlagsLiteral
	TypeFlagsStringOrNumberLiteral                     = TypeFlagsStringLiteral | TypeFlagsNumberLiteral
	TypeFlagsStringOrNumberLiteralOrUnique             = TypeFlagsStringLiteral | TypeFlagsNumberLiteral | TypeFlagsUniqueESSymbol
	TypeFlagsDefinitelyFalsy                           = TypeFlagsStringLiteral | TypeFlagsNumberLiteral | TypeFlagsBigIntLiteral | TypeFlagsBooleanLiteral | TypeFlagsVoid | TypeFlagsUndefined | TypeFlagsNull
	TypeFlagsPossiblyFalsy                             = TypeFlagsDefinitelyFalsy | TypeFlagsString | TypeFlagsNumber | TypeFlagsBigInt | TypeFlagsBoolean
	TypeFlagsIntrinsic                                 = TypeFlagsAny | TypeFlagsUnknown | TypeFlagsString | TypeFlagsNumber | TypeFlagsBigInt | TypeFlagsESSymbol | TypeFlagsVoid | TypeFlagsUndefined | TypeFlagsNull | TypeFlagsNever | TypeFlagsNonPrimitive
	TypeFlagsStringLike                                = TypeFlagsString | TypeFlagsStringLiteral | TypeFlagsTemplateLiteral | TypeFlagsStringMapping
	TypeFlagsNumberLike                                = TypeFlagsNumber | TypeFlagsNumberLiteral | TypeFlagsEnum
	TypeFlagsBigIntLike                                = TypeFlagsBigInt | TypeFlagsBigIntLiteral
	TypeFlagsBooleanLike                               = TypeFlagsBoolean | TypeFlagsBooleanLiteral
	TypeFlagsEnumLike                                  = TypeFlagsEnum | TypeFlagsEnumLiteral
	TypeFlagsESSymbolLike                              = TypeFlagsESSymbol | TypeFlagsUniqueESSymbol
	TypeFlagsVoidLike                                  = TypeFlagsVoid | TypeFlagsUndefined
	TypeFlagsPrimitive                                 = TypeFlagsStringLike | TypeFlagsNumberLike | TypeFlagsBigIntLike | TypeFlagsBooleanLike | TypeFlagsEnumLike | TypeFlagsESSymbolLike | TypeFlagsVoidLike | TypeFlagsNull
	TypeFlagsDefinitelyNonNullable                     = TypeFlagsStringLike | TypeFlagsNumberLike | TypeFlagsBigIntLike | TypeFlagsBooleanLike | TypeFlagsEnumLike | TypeFlagsESSymbolLike | TypeFlagsObject | TypeFlagsNonPrimitive
	TypeFlagsDisjointDomains                           = TypeFlagsNonPrimitive | TypeFlagsStringLike | TypeFlagsNumberLike | TypeFlagsBigIntLike | TypeFlagsBooleanLike | TypeFlagsESSymbolLike | TypeFlagsVoidLike | TypeFlagsNull
	TypeFlagsUnionOrIntersection                       = TypeFlagsUnion | TypeFlagsIntersection
	TypeFlagsStructuredType                            = TypeFlagsObject | TypeFlagsUnion | TypeFlagsIntersection
	TypeFlagsTypeVariable                              = TypeFlagsTypeParameter | TypeFlagsIndexedAccess
	TypeFlagsInstantiableNonPrimitive                  = TypeFlagsTypeVariable | TypeFlagsConditional | TypeFlagsSubstitution
	TypeFlagsInstantiablePrimitive                     = TypeFlagsIndex | TypeFlagsTemplateLiteral | TypeFlagsStringMapping
	TypeFlagsInstantiable                              = TypeFlagsInstantiableNonPrimitive | TypeFlagsInstantiablePrimitive
	TypeFlagsStructuredOrInstantiable                  = TypeFlagsStructuredType | TypeFlagsInstantiable
	TypeFlagsObjectFlagsType                           = TypeFlagsAny | TypeFlagsNullable | TypeFlagsNever | TypeFlagsObject | TypeFlagsUnion | TypeFlagsIntersection
	TypeFlagsSimplifiable                              = TypeFlagsIndexedAccess | TypeFlagsConditional | TypeFlagsIndex
	TypeFlagsSingleton                                 = TypeFlagsAny | TypeFlagsUnknown | TypeFlagsString | TypeFlagsNumber | TypeFlagsBoolean | TypeFlagsBigInt | TypeFlagsESSymbol | TypeFlagsVoid | TypeFlagsUndefined | TypeFlagsNull | TypeFlagsNever | TypeFlagsNonPrimitive
	TypeFlagsNarrowable                                = TypeFlagsAny | TypeFlagsUnknown | TypeFlagsStructuredOrInstantiable | TypeFlagsStringLike | TypeFlagsNumberLike | TypeFlagsBigIntLike | TypeFlagsBooleanLike | TypeFlagsESSymbol | TypeFlagsUniqueESSymbol | TypeFlagsNonPrimitive
	TypeFlagsIncludesMask                              = TypeFlagsAny | TypeFlagsUnknown | TypeFlagsPrimitive | TypeFlagsNever | TypeFlagsObject | TypeFlagsUnion | TypeFlagsIntersection | TypeFlagsNonPrimitive | TypeFlagsTemplateLiteral | TypeFlagsStringMapping
	TypeFlagsIncludesMissingType                       = TypeFlagsTypeParameter
	TypeFlagsIncludesNonWideningType                   = TypeFlagsIndex
	TypeFlagsIncludesWildcard                          = TypeFlagsIndexedAccess
	TypeFlagsIncludesEmptyObject                       = TypeFlagsConditional
	TypeFlagsIncludesInstantiable                      = TypeFlagsSubstitution
	TypeFlagsIncludesConstrainedTypeVariable           = TypeFlagsReserved1
	TypeFlagsIncludesError                             = TypeFlagsReserved2
	TypeFlagsNotPrimitiveUnion                         = TypeFlagsAny | TypeFlagsUnknown | TypeFlagsVoid | TypeFlagsNever | TypeFlagsObject | TypeFlagsIntersection | TypeFlagsIncludesInstantiable
)

var typeFlagNames = [...]struct {
	flag TypeFlags
	name string
}{{TypeFlagsAny, "Any"}, {TypeFlagsUnknown, "Unknown"}, {TypeFlagsUndefined, "Undefined"}, {TypeFlagsNull, "Null"}, {TypeFlagsVoid, "Void"}, {TypeFlagsString, "String"}, {TypeFlagsNumber, "Number"}, {TypeFlagsBigInt, "BigInt"}, {TypeFlagsBoolean, "Boolean"}, {TypeFlagsESSymbol, "ESSymbol"}, {TypeFlagsStringLiteral, "StringLiteral"}, {TypeFlagsNumberLiteral, "NumberLiteral"}, {TypeFlagsBigIntLiteral, "BigIntLiteral"}, {TypeFlagsBooleanLiteral, "BooleanLiteral"}, {TypeFlagsUniqueESSymbol, "UniqueESSymbol"}, {TypeFlagsEnumLiteral, "EnumLiteral"}, {TypeFlagsEnum, "Enum"}, {TypeFlagsNonPrimitive, "NonPrimitive"}, {TypeFlagsNever, "Never"}, {TypeFlagsTypeParameter, "TypeParameter"}, {TypeFlagsObject, "Object"}, {TypeFlagsIndex, "Index"}, {TypeFlagsTemplateLiteral, "TemplateLiteral"}, {TypeFlagsStringMapping, "StringMapping"}, {TypeFlagsSubstitution, "Substitution"}, {TypeFlagsIndexedAccess, "IndexedAccess"}, {TypeFlagsConditional, "Conditional"}, {TypeFlagsUnion, "Union"}, {TypeFlagsIntersection, "Intersection"}}

func FormatTypeFlags(flags TypeFlags) []string {
	result := make([]string, 0, bits.OnesCount32(uint32(flags)))
	for _, fn := range typeFlagNames {
		if flags&fn.flag != 0 {
			result = append(result, fn.name)
		}
	}
	if len(result) == 0 {
		result = append(result, "None")
	}
	return result
}

func (f TypeFlags) String() string {
	return strings.Join(FormatTypeFlags(f), "|")
}
func (v VarianceFlags) String() string {
	variance := v & VarianceFlagsVarianceMask
	var result string
	switch variance {
	case VarianceFlagsInvariant:
		result = "in out"
	case VarianceFlagsBivariant:
		result = "[bivariant]"
	case VarianceFlagsContravariant:
		result = "in"
	case VarianceFlagsCovariant:
		result = "out"
	case VarianceFlagsIndependent:
		result = "[independent]"
	default:
		result = ""
	}
	if v&VarianceFlagsUnmeasurable != 0 {
		result += " (unmeasurable)"
	} else if v&VarianceFlagsUnreliable != 0 {
		result += " (unreliable)"
	}
	return result
}

type ObjectFlags uint32

const (
	ObjectFlagsNone                                       ObjectFlags = 0
	ObjectFlagsClass                                      ObjectFlags = 1 << 0
	ObjectFlagsInterface                                  ObjectFlags = 1 << 1
	ObjectFlagsReference                                  ObjectFlags = 1 << 2
	ObjectFlagsTuple                                      ObjectFlags = 1 << 3
	ObjectFlagsAnonymous                                  ObjectFlags = 1 << 4
	ObjectFlagsMapped                                     ObjectFlags = 1 << 5
	ObjectFlagsInstantiated                               ObjectFlags = 1 << 6
	ObjectFlagsObjectLiteral                              ObjectFlags = 1 << 7
	ObjectFlagsEvolvingArray                              ObjectFlags = 1 << 8
	ObjectFlagsObjectLiteralPatternWithComputedProperties ObjectFlags = 1 << 9
	ObjectFlagsReverseMapped                              ObjectFlags = 1 << 10
	ObjectFlagsJsxAttributes                              ObjectFlags = 1 << 11
	ObjectFlagsJSLiteral                                  ObjectFlags = 1 << 12
	ObjectFlagsFreshLiteral                               ObjectFlags = 1 << 13
	ObjectFlagsArrayLiteral                               ObjectFlags = 1 << 14
	ObjectFlagsPrimitiveUnion                             ObjectFlags = 1 << 15
	ObjectFlagsContainsWideningType                       ObjectFlags = 1 << 16
	ObjectFlagsContainsObjectOrArrayLiteral               ObjectFlags = 1 << 17
	ObjectFlagsNonInferrableType                          ObjectFlags = 1 << 18
	ObjectFlagsCouldContainTypeVariablesComputed          ObjectFlags = 1 << 19
	ObjectFlagsCouldContainTypeVariables                  ObjectFlags = 1 << 20
	ObjectFlagsMembersResolved                            ObjectFlags = 1 << 21
	ObjectFlagsClassOrInterface                                       = ObjectFlagsClass | ObjectFlagsInterface
	ObjectFlagsRequiresWidening                                       = ObjectFlagsContainsWideningType | ObjectFlagsContainsObjectOrArrayLiteral
	ObjectFlagsPropagatingFlags                                       = ObjectFlagsContainsWideningType | ObjectFlagsContainsObjectOrArrayLiteral | ObjectFlagsNonInferrableType
	ObjectFlagsInstantiatedMapped                                     = ObjectFlagsMapped | ObjectFlagsInstantiated
	ObjectFlagsObjectTypeKindMask                                     = ObjectFlagsClassOrInterface | ObjectFlagsReference | ObjectFlagsTuple | ObjectFlagsAnonymous | ObjectFlagsMapped | ObjectFlagsReverseMapped | ObjectFlagsEvolvingArray | ObjectFlagsInstantiationExpressionType | ObjectFlagsSingleSignatureType
	ObjectFlagsContainsSpread                                         = 1 << 22
	ObjectFlagsObjectRestType                                         = 1 << 23
	ObjectFlagsInstantiationExpressionType                            = 1 << 24
	ObjectFlagsSingleSignatureType                                    = 1 << 25
	ObjectFlagsIsClassInstanceClone                                   = 1 << 26
	ObjectFlagsIdenticalBaseTypeCalculated                            = 1 << 27
	ObjectFlagsIdenticalBaseTypeExists                                = 1 << 28
	ObjectFlagsUnresolvedMembers                                      = 1 << 29
	ObjectFlagsFromTypeNode                                           = 1 << 30
	ObjectFlagsIsGenericTypeComputed                                  = 1 << 22
	ObjectFlagsIsGenericObjectType                                    = 1 << 23
	ObjectFlagsIsGenericIndexType                                     = 1 << 24
	ObjectFlagsIsGenericType                                          = ObjectFlagsIsGenericObjectType | ObjectFlagsIsGenericIndexType
	ObjectFlagsContainsIntersections                                  = 1 << 25
	ObjectFlagsIsUnknownLikeUnionComputed                             = 1 << 26
	ObjectFlagsIsUnknownLikeUnion                                     = 1 << 27
	ObjectFlagsIsUniformEnumComputed                                  = 1 << 28
	ObjectFlagsIsUniformEnum                                          = 1 << 29
	ObjectFlagsIsNeverIntersectionComputed                            = 1 << 25
	ObjectFlagsIsNeverIntersection                                    = 1 << 26
	ObjectFlagsIsConstrainedTypeVariable                              = 1 << 27
)

type TypeAlias struct {
	symbol        *ast.Symbol
	typeArguments []*Type
}

func (a *TypeAlias) Symbol() *ast.Symbol {
	if a == nil {
		return nil
	}
	return a.symbol
}
func (a *TypeAlias) TypeArguments() []*Type {
	if a == nil {
		return nil
	}
	return a.typeArguments
}

type Type struct {
	flags       TypeFlags
	objectFlags ObjectFlags
	id          TypeId
	symbol      *ast.Symbol
	alias       *TypeAlias
	checker     *Checker
	data        TypeData
}

func (t *Type) Id() TypeId {
	return t.id
}
func (t *Type) Flags() TypeFlags {
	return t.flags
}
func (t *Type) ObjectFlags() ObjectFlags {
	return t.objectFlags
}
func (t *Type) AsIntrinsicType() *IntrinsicType {
	return t.data.(*IntrinsicType)
}
func (t *Type) AsLiteralType() *LiteralType {
	return t.data.(*LiteralType)
}
func (t *Type) AsUniqueESSymbolType() *UniqueESSymbolType {
	return t.data.(*UniqueESSymbolType)
}
func (t *Type) AsTupleType() *TupleType {
	return t.data.(*TupleType)
}
func (t *Type) AsInstantiationExpressionType() *InstantiationExpressionType {
	return t.data.(*InstantiationExpressionType)
}
func (t *Type) AsMappedType() *MappedType {
	return t.data.(*MappedType)
}
func (t *Type) AsReverseMappedType() *ReverseMappedType {
	return t.data.(*ReverseMappedType)
}
func (t *Type) AsEvolvingArrayType() *EvolvingArrayType {
	return t.data.(*EvolvingArrayType)
}
func (t *Type) AsTypeParameter() *TypeParameter {
	return t.data.(*TypeParameter)
}
func (t *Type) AsUnionType() *UnionType {
	return t.data.(*UnionType)
}
func (t *Type) AsIntersectionType() *IntersectionType {
	return t.data.(*IntersectionType)
}
func (t *Type) AsIndexType() *IndexType {
	return t.data.(*IndexType)
}
func (t *Type) AsIndexedAccessType() *IndexedAccessType {
	return t.data.(*IndexedAccessType)
}
func (t *Type) AsTemplateLiteralType() *TemplateLiteralType {
	return t.data.(*TemplateLiteralType)
}
func (t *Type) AsStringMappingType() *StringMappingType {
	return t.data.(*StringMappingType)
}
func (t *Type) AsSubstitutionType() *SubstitutionType {
	return t.data.(*SubstitutionType)
}
func (t *Type) AsConditionalType() *ConditionalType {
	return t.data.(*ConditionalType)
}
func (t *Type) AsConstrainedType() *ConstrainedType {
	return t.data.AsConstrainedType()
}
func (t *Type) AsStructuredType() *StructuredType {
	return t.data.AsStructuredType()
}
func (t *Type) AsObjectType() *ObjectType {
	return t.data.AsObjectType()
}
func (t *Type) AsTypeReference() *TypeReference {
	return t.data.AsTypeReference()
}
func (t *Type) AsInterfaceType() *InterfaceType {
	return t.data.AsInterfaceType()
}
func (t *Type) AsUnionOrIntersectionType() *UnionOrIntersectionType {
	return t.data.AsUnionOrIntersectionType()
}
func (t *Type) Distributed() []*Type {
	switch {
	case t.flags&TypeFlagsUnion != 0:
		return t.AsUnionType().types
	case t.flags&TypeFlagsNever != 0:
		return nil
	}
	return []*Type{t}
}
func (t *Type) Target() *Type {
	switch {
	case t.flags&TypeFlagsObject != 0:
		return t.AsObjectType().target
	case t.flags&TypeFlagsTypeParameter != 0:
		return t.AsTypeParameter().target
	case t.flags&TypeFlagsIndex != 0:
		return t.AsIndexType().target
	case t.flags&TypeFlagsStringMapping != 0:
		return t.AsStringMappingType().target
	case t.flags&TypeFlagsObject != 0 && t.objectFlags&ObjectFlagsMapped != 0:
		return t.AsMappedType().target
	}
	panic("Unhandled case in Type.Target")
}
func (t *Type) Mapper() *TypeMapper {
	switch {
	case t.flags&TypeFlagsObject != 0:
		return t.AsObjectType().mapper
	case t.flags&TypeFlagsTypeParameter != 0:
		return t.AsTypeParameter().mapper
	case t.flags&TypeFlagsConditional != 0:
		return t.AsConditionalType().mapper
	}
	panic("Unhandled case in Type.Mapper")
}
func (t *Type) Types() []*Type {
	switch {
	case t.flags&TypeFlagsUnionOrIntersection != 0:
		return t.AsUnionOrIntersectionType().types
	case t.flags&TypeFlagsTemplateLiteral != 0:
		return t.AsTemplateLiteralType().types
	}
	panic("Unhandled case in Type.Types")
}
func (t *Type) TargetInterfaceType() *InterfaceType {
	return t.AsTypeReference().target.AsInterfaceType()
}
func (t *Type) TargetTupleType() *TupleType {
	return t.AsTypeReference().target.AsTupleType()
}
func (t *Type) Symbol() *ast.Symbol {
	return t.symbol
}
func (t *Type) Alias() *TypeAlias {
	return t.alias
}
func (t *Type) IsUnion() bool {
	return t.flags&TypeFlagsUnion != 0
}
func (t *Type) IsString() bool {
	return t.flags&TypeFlagsString != 0
}
func (t *Type) IsIntersection() bool {
	return t.flags&TypeFlagsIntersection != 0
}
func (t *Type) IsStringLiteral() bool {
	return t.flags&TypeFlagsStringLiteral != 0
}
func (t *Type) IsNumberLiteral() bool {
	return t.flags&TypeFlagsNumberLiteral != 0
}
func (t *Type) IsBigIntLiteral() bool {
	return t.flags&TypeFlagsBigIntLiteral != 0
}
func (t *Type) IsEnumLiteral() bool {
	return t.flags&TypeFlagsEnumLiteral != 0
}
func (t *Type) IsBooleanLike() bool {
	return t.flags&TypeFlagsBooleanLike != 0
}
func (t *Type) IsStringLike() bool {
	return t.flags&TypeFlagsStringLike != 0
}
func (t *Type) IsClass() bool {
	return t.objectFlags&ObjectFlagsClass != 0
}
func (t *Type) IsTypeParameter() bool {
	return t.flags&TypeFlagsTypeParameter != 0
}
func (t *Type) IsIndex() bool {
	return t.flags&TypeFlagsIndex != 0
}
func (t *Type) IsTupleType() bool {
	return isTupleType(t)
}

type TypeData interface {
	AsType() *Type
	AsConstrainedType() *ConstrainedType
	AsStructuredType() *StructuredType
	AsObjectType() *ObjectType
	AsTypeReference() *TypeReference
	AsInterfaceType() *InterfaceType
	AsUnionOrIntersectionType() *UnionOrIntersectionType
}
type TypeBase struct{ Type }

func (t *TypeBase) AsType() *Type {
	return &t.Type
}
func (t *TypeBase) AsConstrainedType() *ConstrainedType {
	return nil
}
func (t *TypeBase) AsStructuredType() *StructuredType {
	return nil
}
func (t *TypeBase) AsObjectType() *ObjectType {
	return nil
}
func (t *TypeBase) AsTypeReference() *TypeReference {
	return nil
}
func (t *TypeBase) AsInterfaceType() *InterfaceType {
	return nil
}
func (t *TypeBase) AsUnionOrIntersectionType() *UnionOrIntersectionType {
	return nil
}

type IntrinsicType struct {
	TypeBase
	intrinsicName string
}

func (t *IntrinsicType) IntrinsicName() string {
	return t.intrinsicName
}

type LiteralType struct {
	TypeBase
	value       any
	freshType   *Type
	regularType *Type
}

func (t *LiteralType) Value() any {
	return t.value
}
func (t *LiteralType) FreshType() *Type {
	return t.freshType
}
func (t *LiteralType) RegularType() *Type {
	return t.regularType
}
func (t *LiteralType) String() string {
	return ValueToString(t.value)
}

type UniqueESSymbolType struct {
	TypeBase
	name string
}
type ConstrainedType struct {
	TypeBase
	resolvedBaseConstraint *Type
}

func (t *ConstrainedType) AsConstrainedType() *ConstrainedType {
	return t
}

type StructuredType struct {
	ConstrainedType
	members                                      ast.SymbolTable
	properties                                   []*ast.Symbol
	signatures                                   []*Signature
	callSignatureCount                           int
	indexInfos                                   []*IndexInfo
	objectTypeWithoutAbstractConstructSignatures *Type
}

func (t *StructuredType) AsStructuredType() *StructuredType {
	return t
}
func (t *StructuredType) CallSignatures() []*Signature {
	return slices.Clip(t.signatures[:t.callSignatureCount])
}
func (t *StructuredType) ConstructSignatures() []*Signature {
	return slices.Clip(t.signatures[t.callSignatureCount:])
}
func (t *StructuredType) Properties() []*ast.Symbol {
	return t.properties
}

type ObjectType struct {
	StructuredType
	target         *Type
	mapper         *TypeMapper
	instantiations map[CacheHashKey]*Type
}

func (t *ObjectType) AsObjectType() *ObjectType {
	return t
}

type TypeReference struct {
	ObjectType
	node                  ast.Handle
	resolvedTypeArguments []*Type
}

func (t *TypeReference) AsTypeReference() *TypeReference {
	return t
}

type InterfaceType struct {
	TypeReference
	allTypeParameters           []*Type
	outerTypeParameterCount     int
	thisType                    *Type
	baseTypesResolved           bool
	declaredMembersResolved     bool
	resolvedBaseConstructorType *Type
	resolvedBaseTypes           []*Type
	declaredMembers             ast.SymbolTable
	declaredCallSignatures      []*Signature
	declaredConstructSignatures []*Signature
	declaredIndexInfos          []*IndexInfo
}

func (t *InterfaceType) AsInterfaceType() *InterfaceType {
	return t
}
func (t *InterfaceType) OuterTypeParameters() []*Type {
	if len(t.allTypeParameters) == 0 {
		return nil
	}
	return slices.Clip(t.allTypeParameters[:t.outerTypeParameterCount])
}
func (t *InterfaceType) LocalTypeParameters() []*Type {
	if len(t.allTypeParameters) == 0 {
		return nil
	}
	return slices.Clip(t.allTypeParameters[t.outerTypeParameterCount : len(t.allTypeParameters)-1])
}
func (t *InterfaceType) TypeParameters() []*Type {
	if len(t.allTypeParameters) == 0 {
		return nil
	}
	return slices.Clip(t.allTypeParameters[:len(t.allTypeParameters)-1])
}

type ElementFlags uint32

const (
	ElementFlagsNone        ElementFlags = 0
	ElementFlagsRequired    ElementFlags = 1 << 0
	ElementFlagsOptional    ElementFlags = 1 << 1
	ElementFlagsRest        ElementFlags = 1 << 2
	ElementFlagsVariadic    ElementFlags = 1 << 3
	ElementFlagsFixed                    = ElementFlagsRequired | ElementFlagsOptional
	ElementFlagsVariable                 = ElementFlagsRest | ElementFlagsVariadic
	ElementFlagsNonRequired              = ElementFlagsOptional | ElementFlagsRest | ElementFlagsVariadic
	ElementFlagsNonRest                  = ElementFlagsRequired | ElementFlagsOptional | ElementFlagsVariadic
)

type TupleElementInfo struct {
	flags              ElementFlags
	labeledDeclaration ast.Handle
}

func (t *TupleElementInfo) TupleElementFlags() ElementFlags {
	return t.flags
}
func (t *TupleElementInfo) LabeledDeclaration() ast.Handle {
	return t.labeledDeclaration
}

type TupleType struct {
	InterfaceType
	elementInfos  []TupleElementInfo
	minLength     int
	fixedLength   int
	combinedFlags ElementFlags
	readonly      bool
}

func (t *TupleType) FixedLength() int {
	return t.fixedLength
}
func (t *TupleType) IsReadonly() bool {
	return t.readonly
}
func (t *TupleType) ElementFlags() []ElementFlags {
	elementFlags := make([]ElementFlags, len(t.elementInfos))
	for i, info := range t.elementInfos {
		elementFlags[i] = info.flags
	}
	return elementFlags
}
func (t *TupleType) ElementInfos() []TupleElementInfo {
	return t.elementInfos
}

type InstantiationExpressionType struct {
	ObjectType
	node ast.Handle
}
type MappedType struct {
	ObjectType
	declaration          ast.Handle
	typeParameter        *Type
	constraintType       *Type
	nameType             *Type
	templateType         *Type
	modifiersType        *Type
	resolvedApparentType *Type
	containsError        bool
}
type ReverseMappedType struct {
	ObjectType
	source         *Type
	mappedType     *Type
	constraintType *Type
}
type EvolvingArrayType struct {
	ObjectType
	elementType    *Type
	finalArrayType *Type
}
type UnionOrIntersectionType struct {
	StructuredType
	types                                       []*Type
	propertyCache                               ast.SymbolTable
	propertyCacheWithoutFunctionPropertyAugment ast.SymbolTable
	resolvedProperties                          []*ast.Symbol
}

func (t *UnionOrIntersectionType) AsUnionOrIntersectionType() *UnionOrIntersectionType {
	return t
}
func (t *UnionOrIntersectionType) Types() []*Type {
	return t.types
}

type UnionType struct {
	UnionOrIntersectionType
	resolvedReducedType *Type
	regularType         *Type
	origin              *Type
	keyPropertyName     string
	constituentMap      map[*Type]*Type
}
type IntersectionType struct {
	UnionOrIntersectionType
	resolvedApparentType             *Type
	uniqueLiteralFilledInstantiation *Type
}
type TypeParameter struct {
	ConstrainedType
	constraint          *Type
	target              *Type
	mapper              *TypeMapper
	isThisType          bool
	resolvedDefaultType *Type
}

func (t *TypeParameter) IsThisType() bool {
	return t.isThisType
}

type IndexFlags uint32

const (
	IndexFlagsNone              IndexFlags = 0
	IndexFlagsStringsOnly       IndexFlags = 1 << 0
	IndexFlagsNoIndexSignatures IndexFlags = 1 << 1
	IndexFlagsNoReducibleCheck  IndexFlags = 1 << 2
)

type IndexType struct {
	ConstrainedType
	target     *Type
	indexFlags IndexFlags
}

func (t *IndexType) Target() *Type {
	return t.target
}

type IndexedAccessType struct {
	ConstrainedType
	objectType  *Type
	indexType   *Type
	accessFlags AccessFlags
}

func (t *IndexedAccessType) ObjectType() *Type {
	return t.objectType
}
func (t *IndexedAccessType) IndexType() *Type {
	return t.indexType
}

type TemplateLiteralType struct {
	ConstrainedType
	texts []string
	types []*Type
}

func (t *TemplateLiteralType) Texts() []string {
	return t.texts
}
func (t *TemplateLiteralType) Types() []*Type {
	return t.types
}

type StringMappingType struct {
	ConstrainedType
	target *Type
}

func (t *StringMappingType) Target() *Type {
	return t.target
}

type SubstitutionType struct {
	ConstrainedType
	baseType   *Type
	constraint *Type
}

func (t *SubstitutionType) BaseType() *Type {
	return t.baseType
}
func (t *SubstitutionType) SubstConstraint() *Type {
	return t.constraint
}

type ConditionalRoot struct {
	node                ast.Handle
	checkType           *Type
	extendsType         *Type
	isDistributive      bool
	inferTypeParameters []*Type
	outerTypeParameters []*Type
	instantiations      map[CacheHashKey]*Type
	alias               *TypeAlias
}
type ConditionalType struct {
	ConstrainedType
	root                             *ConditionalRoot
	checkType                        *Type
	extendsType                      *Type
	resolvedTrueType                 *Type
	resolvedFalseType                *Type
	resolvedInferredTrueType         *Type
	resolvedDefaultConstraint        *Type
	resolvedConstraintOfDistributive *Type
	mapper                           *TypeMapper
	combinedMapper                   *TypeMapper
}

func (t *ConditionalType) CheckType() *Type {
	return t.checkType
}
func (t *ConditionalType) ExtendsType() *Type {
	return t.extendsType
}

type SignatureFlags uint32

const (
	SignatureFlagsNone                                   SignatureFlags = 0
	SignatureFlagsHasRestParameter                       SignatureFlags = 1 << 0
	SignatureFlagsHasLiteralTypes                        SignatureFlags = 1 << 1
	SignatureFlagsConstruct                              SignatureFlags = 1 << 2
	SignatureFlagsAbstract                               SignatureFlags = 1 << 3
	SignatureFlagsIsInnerCallChain                       SignatureFlags = 1 << 4
	SignatureFlagsIsOuterCallChain                       SignatureFlags = 1 << 5
	SignatureFlagsIsUntypedSignatureInJSFile             SignatureFlags = 1 << 6
	SignatureFlagsIsNonInferrable                        SignatureFlags = 1 << 7
	SignatureFlagsIsSignatureCandidateForOverloadFailure SignatureFlags = 1 << 8
	SignatureFlagsPropagatingFlags                                      = SignatureFlagsHasRestParameter | SignatureFlagsHasLiteralTypes | SignatureFlagsConstruct | SignatureFlagsAbstract | SignatureFlagsIsUntypedSignatureInJSFile | SignatureFlagsIsSignatureCandidateForOverloadFailure
	SignatureFlagsCallChainFlags                                        = SignatureFlagsIsInnerCallChain | SignatureFlagsIsOuterCallChain
)

type Signature struct {
	id                       SignatureId
	flags                    SignatureFlags
	minArgumentCount         int32
	resolvedMinArgumentCount int32
	declaration              ast.Handle
	typeParameters           []*Type
	parameters               []*ast.Symbol
	thisParameter            *ast.Symbol
	resolvedReturnType       *Type
	resolvedTypePredicate    *TypePredicate
	target                   *Signature
	mapper                   *TypeMapper
	isolatedSignatureType    *Type
	composite                *CompositeSignature
}

func (s *Signature) Id() SignatureId {
	return s.id
}
func (s *Signature) Flags() SignatureFlags {
	return s.flags
}
func (s *Signature) TypeParameters() []*Type {
	return s.typeParameters
}
func (s *Signature) Declaration() ast.Handle {
	return s.declaration
}
func (s *Signature) Target() *Signature {
	return s.target
}
func (s *Signature) ThisParameter() *ast.Symbol {
	return s.thisParameter
}
func (s *Signature) Parameters() []*ast.Symbol {
	return s.parameters
}
func (s *Signature) HasRestParameter() bool {
	return s.flags&SignatureFlagsHasRestParameter != 0
}
func (s *Signature) MinArgumentCount() int {
	return int(s.minArgumentCount)
}

type CompositeSignature struct {
	isUnion    bool
	signatures []*Signature
}
type TypePredicateKind int32

const (
	TypePredicateKindThis TypePredicateKind = iota
	TypePredicateKindIdentifier
	TypePredicateKindAssertsThis
	TypePredicateKindAssertsIdentifier
)

type TypePredicate struct {
	kind           TypePredicateKind
	parameterIndex int32
	parameterName  string
	t              *Type
}

func (typePredicate *TypePredicate) Type() *Type {
	return typePredicate.t
}
func (typePredicate *TypePredicate) Kind() TypePredicateKind {
	return typePredicate.kind
}
func (typePredicate *TypePredicate) ParameterIndex() int32 {
	return typePredicate.parameterIndex
}
func (typePredicate *TypePredicate) ParameterName() string {
	return typePredicate.parameterName
}

type IndexInfo struct {
	keyType     *Type
	valueType   *Type
	isReadonly  bool
	declaration ast.Handle
	indexSymbol *ast.Symbol
	components  []ast.Handle
}

func (info *IndexInfo) KeyType() *Type {
	return info.keyType
}
func (info *IndexInfo) ValueType() *Type {
	return info.valueType
}
func (info *IndexInfo) IsReadonly() bool {
	return info.isReadonly
}
func (info *IndexInfo) Declaration() ast.Handle {
	return info.declaration
}

type Ternary int8

const (
	TernaryFalse   Ternary = 0
	TernaryUnknown Ternary = 1
	TernaryMaybe   Ternary = 3
	TernaryTrue    Ternary = -1
)

type TypeComparer func(s *Type, t *Type, reportErrors bool) Ternary
type LanguageFeatureMinimumTargetMap struct {
	Exponentiation                    core.ScriptTarget
	AsyncFunctions                    core.ScriptTarget
	ForAwaitOf                        core.ScriptTarget
	AsyncGenerators                   core.ScriptTarget
	AsyncIteration                    core.ScriptTarget
	ObjectSpreadRest                  core.ScriptTarget
	RegularExpressionFlagsDotAll      core.ScriptTarget
	BindinglessCatch                  core.ScriptTarget
	BigInt                            core.ScriptTarget
	NullishCoalesce                   core.ScriptTarget
	OptionalChaining                  core.ScriptTarget
	LogicalAssignment                 core.ScriptTarget
	TopLevelAwait                     core.ScriptTarget
	ClassFields                       core.ScriptTarget
	PrivateNamesAndClassStaticBlocks  core.ScriptTarget
	RegularExpressionFlagsHasIndices  core.ScriptTarget
	ShebangComments                   core.ScriptTarget
	UsingAndAwaitUsing                core.ScriptTarget
	ClassAndClassElementDecorators    core.ScriptTarget
	RegularExpressionFlagsUnicodeSets core.ScriptTarget
}

var LanguageFeatureMinimumTarget = LanguageFeatureMinimumTargetMap{Exponentiation: core.ScriptTargetES2016, AsyncFunctions: core.ScriptTargetES2017, ForAwaitOf: core.ScriptTargetES2018, AsyncGenerators: core.ScriptTargetES2018, AsyncIteration: core.ScriptTargetES2018, ObjectSpreadRest: core.ScriptTargetES2018, RegularExpressionFlagsDotAll: core.ScriptTargetES2018, BindinglessCatch: core.ScriptTargetES2019, BigInt: core.ScriptTargetES2020, NullishCoalesce: core.ScriptTargetES2020, OptionalChaining: core.ScriptTargetES2020, LogicalAssignment: core.ScriptTargetES2021, TopLevelAwait: core.ScriptTargetES2022, ClassFields: core.ScriptTargetES2022, PrivateNamesAndClassStaticBlocks: core.ScriptTargetES2022, RegularExpressionFlagsHasIndices: core.ScriptTargetES2022, ShebangComments: core.ScriptTargetESNext, UsingAndAwaitUsing: core.ScriptTargetESNext, ClassAndClassElementDecorators: core.ScriptTargetESNext, RegularExpressionFlagsUnicodeSets: core.ScriptTargetESNext}

type StringLiteralType = Type
