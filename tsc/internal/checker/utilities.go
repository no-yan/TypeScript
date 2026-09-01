package checker

import (
	"cmp"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

func NewDiagnosticForNode(node ast.Handle, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	var file *ast.SourceFile
	var loc core.TextRange
	if !node.IsNil() {
		file = ast.GetSourceFileOfNode(node)
		loc = scanner.GetErrorRangeForNode(file, node)
	}
	return ast.NewDiagnostic(file, loc, message, args...)
}
func NewDiagnosticChainForNode(chain *ast.Diagnostic, node ast.Handle, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	if chain != nil {
		return ast.NewDiagnosticChain(chain, message, args...)
	}
	return NewDiagnosticForNode(node, message, args...)
}
func findInMap[K comparable, V any](m map[K]V, predicate func(V) bool) V {
	for _, value := range m {
		if predicate(value) {
			return value
		}
	}
	return *new(V)
}
func tokenIsIdentifierOrKeyword(token ast.Kind) bool {
	return token >= ast.KindIdentifier
}
func tokenIsIdentifierOrKeywordOrGreaterThan(token ast.Kind) bool {
	return token == ast.KindGreaterThanToken || tokenIsIdentifierOrKeyword(token)
}
func hasOverrideModifier(node ast.Handle) bool {
	return ast.HasSyntacticModifier(node, ast.ModifierFlagsOverride)
}
func hasAsyncModifier(node ast.Handle) bool {
	return ast.HasSyntacticModifier(node, ast.ModifierFlagsAsync)
}
func getSelectedModifierFlags(node ast.Handle, flags ast.ModifierFlags) ast.ModifierFlags {
	return node.ModifierFlags() & flags
}
func hasReadonlyModifier(node ast.Handle) bool {
	return ast.HasModifier(node, ast.ModifierFlagsReadonly)
}
func isStaticPrivateIdentifierProperty(s *ast.Symbol) bool {
	return s.ValueDeclaration != 0 && ast.IsPrivateIdentifierClassElementDeclaration(ast.NodeOf(s.ValueDeclaration)) && ast.IsStatic(ast.NodeOf(s.ValueDeclaration))
}
func isEmptyObjectLiteral(expression ast.Handle) bool {
	return ast.IsObjectLiteralExpression(expression) && len(expression.Properties()) == 0
}

type AssignmentKind int32

const (
	AssignmentKindNone AssignmentKind = iota
	AssignmentKindDefinite
	AssignmentKindCompound
)

type AssignmentTarget = ast.Handle

func getAssignmentTargetKind(node ast.Handle) AssignmentKind {
	target := ast.GetAssignmentTarget(node)
	if target.IsNil() {
		return AssignmentKindNone
	}
	switch target.Kind() {
	case ast.KindBinaryExpression:
		binaryOperator := target.BinaryExpressionOperatorToken().Kind()
		if binaryOperator == ast.KindEqualsToken || ast.IsLogicalOrCoalescingAssignmentOperator(binaryOperator) {
			return AssignmentKindDefinite
		}
		return AssignmentKindCompound
	case ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression:
		return AssignmentKindCompound
	case ast.KindForInStatement, ast.KindForOfStatement:
		return AssignmentKindDefinite
	}
	panic("Unhandled case in getAssignmentTargetKind")
}
func isDeleteTarget(node ast.Handle) bool {
	if !ast.IsAccessExpression(node) {
		return false
	}
	node = ast.WalkUpParenthesizedExpressions(node.Parent())
	return !node.IsNil() && node.Kind() == ast.KindDeleteExpression
}
func isInCompoundLikeAssignment(node ast.Handle) bool {
	target := ast.GetAssignmentTarget(node)
	return !target.IsNil() && ast.IsAssignmentExpression(target, true) && isCompoundLikeAssignment(target)
}
func isCompoundLikeAssignment(assignment ast.Handle) bool {
	right := ast.SkipParentheses(assignment.BinaryExpressionRight())
	return right.Kind() == ast.KindBinaryExpression && isShiftOperatorOrHigher(right.BinaryExpressionOperatorToken().Kind())
}
func isConstTypeReference(node ast.Handle) bool {
	return ast.IsTypeReferenceNode(node) && len(node.TypeArguments()) == 0 && ast.IsIdentifier(node.TypeReferenceNodeTypeName()) && node.TypeReferenceNodeTypeName().Text() == "const"
}

func isConstTypeReferenceName(node ast.Handle) bool {
	return !node.IsNil() && ast.IsIdentifier(node) && !node.Parent().IsNil() && isConstTypeReference(node.Parent()) && !node.Parent().Parent().IsNil() && ast.IsAssertionExpression(node.Parent().Parent())
}

func isExportAssignmentExpressionName(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	current := node
	for !current.Parent().IsNil() && ast.IsPropertyAccessOrQualifiedName(current.Parent()) {
		current = current.Parent()
	}
	return !current.Parent().IsNil() && ast.IsExportAssignment(current.Parent()) && current.Parent().Expression() == current
}
func GetSingleVariableOfVariableStatement(node ast.Handle) ast.Handle {
	if !ast.IsVariableStatement(node) {
		return ast.Handle{}
	}
	return core.FirstOrNil(node.Store().ListSlice(node.VariableStatementDeclarationList().VariableDeclarationListDeclarations()))
}
func isTypeReferenceIdentifier(node ast.Handle) bool {
	for node.Parent().Kind() == ast.KindQualifiedName {
		node = node.Parent()
	}
	return ast.IsTypeReferenceNode(node.Parent())
}
func IsInTypeQuery(node ast.Handle) bool {
	return !ast.FindAncestorOrQuit(node, func(n ast.Handle) ast.FindAncestorResult {
		switch n.Kind() {
		case ast.KindTypeQuery:
			return ast.FindAncestorTrue
		case ast.KindIdentifier, ast.KindQualifiedName:
			return ast.FindAncestorFalse
		}
		return ast.FindAncestorQuit
	}).IsNil()
}
func canHaveLocals(node ast.Handle) bool {
	switch node.Kind() {
	case ast.KindArrowFunction, ast.KindBlock, ast.KindCallSignature, ast.KindCaseBlock, ast.KindCatchClause, ast.KindClassStaticBlockDeclaration, ast.KindConditionalType, ast.KindConstructor, ast.KindConstructorType, ast.KindConstructSignature, ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindFunctionType, ast.KindGetAccessor, ast.KindIndexSignature, ast.KindJSDocSignature, ast.KindMappedType, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindModuleDeclaration, ast.KindSetAccessor, ast.KindSourceFile, ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration:
		return true
	}
	return false
}
func isShorthandAmbientModuleSymbol(moduleSymbol *ast.Symbol) bool {
	return isShorthandAmbientModule(ast.NodeOf(moduleSymbol.ValueDeclaration))
}
func isShorthandAmbientModule(node ast.Handle) bool {
	return !node.IsNil() && node.Kind() == ast.KindModuleDeclaration && node.Body().IsNil()
}
func getAliasDeclarationFromName(node ast.Handle) ast.Handle {
	switch node.Parent().Kind() {
	case ast.KindImportClause, ast.KindImportSpecifier, ast.KindNamespaceImport, ast.KindExportSpecifier, ast.KindExportAssignment, ast.KindImportEqualsDeclaration, ast.KindNamespaceExport:
		return node.Parent()
	case ast.KindQualifiedName:
		return getAliasDeclarationFromName(node.Parent())
	}
	return ast.Handle{}
}
func entityNameToString(name ast.Handle) string {
	return ast.EntityNameToString(name, scanner.GetTextOfNode)
}
func getContainingQualifiedNameNode(node ast.Handle) ast.Handle {
	for ast.IsQualifiedName(node.Parent()) {
		node = node.Parent()
	}
	return node
}
func isSideEffectImport(node ast.Handle) bool {
	ancestor := ast.FindAncestor(node, ast.IsImportDeclaration)
	return !ancestor.IsNil() && ancestor.ImportClause().IsNil()
}
func getExternalModuleRequireArgument(node ast.Handle) ast.Handle {
	if ast.IsVariableDeclarationInitializedToRequire(node) {
		return node.Initializer().Arguments()[0]
	}
	return ast.Handle{}
}
func isRightSideOfAccessExpression(node ast.Handle) bool {
	return !node.Parent().IsNil() && (ast.IsPropertyAccessExpression(node.Parent()) && node.Parent().Name() == node || ast.IsElementAccessExpression(node.Parent()) && node.Parent().ElementAccessExpressionArgumentExpression() == node)
}
func isTopLevelInExternalModuleAugmentation(node ast.Handle) bool {
	return !node.IsNil() && !node.Parent().IsNil() && ast.IsModuleBlock(node.Parent()) && ast.IsExternalModuleAugmentation(node.Parent().Parent())
}
func isSyntacticDefault(node ast.Handle) bool {
	return (ast.IsExportAssignment(node) && !node.ExportAssignmentIsExportEquals()) || ast.HasSyntacticModifier(node, ast.ModifierFlagsDefault) || ast.IsExportSpecifier(node) || ast.IsNamespaceExport(node)
}
func hasExportAssignmentSymbol(moduleSymbol *ast.Symbol) bool {
	return moduleSymbol.Exports[ast.InternalSymbolNameExportEquals] != nil
}
func isTypeAlias(node ast.Handle) bool {
	return ast.IsTypeOrJSTypeAliasDeclaration(node)
}
func hasOnlyExpressionInitializer(node ast.Handle) bool {
	switch node.Kind() {
	case ast.KindVariableDeclaration, ast.KindParameter, ast.KindBindingElement, ast.KindPropertyDeclaration, ast.KindPropertyAssignment, ast.KindEnumMember:
		return true
	}
	return false
}
func hasDotDotDotToken(node ast.Handle) bool {
	switch node.Kind() {
	case ast.KindParameter:
		return !node.ParameterDeclarationDotDotDotToken().IsNil()
	case ast.KindBindingElement:
		return !node.BindingElementDotDotDotToken().IsNil()
	case ast.KindNamedTupleMember:
		return !node.NamedTupleMemberDotDotDotToken().IsNil()
	case ast.KindJsxExpression:
		return !node.JsxExpressionDotDotDotToken().IsNil()
	}
	return false
}
func IsTypeAny(t *Type) bool {
	return t != nil && t.flags&TypeFlagsAny != 0
}
func isJSDocOptionalParameter(node ast.Handle) bool {
	return false
}
func isExclamationToken(node ast.Handle) bool {
	return !node.IsNil() && node.Kind() == ast.KindExclamationToken
}
func isOptionalDeclaration(declaration ast.Handle) bool {
	return ast.HasQuestionToken(declaration)
}
func (c *Checker) isOptionalParameter(node ast.Handle) bool {
	if ast.IsParameterDeclaration(node) && !node.QuestionToken().IsNil() {
		return true
	}
	if !ast.IsParameterDeclaration(node) {
		return false
	}
	if !node.Initializer().IsNil() {
		signature := c.getSignatureFromDeclaration(node.Parent())
		parameterIndex := core.FindIndex(node.Parent().Parameters(), func(p ast.Handle) bool {
			return p == node
		})
		debug.Assert(parameterIndex >= 0)
		return parameterIndex >= c.getMinArgumentCountEx(signature, MinArgumentCountFlagsStrongArityForUntypedJS|MinArgumentCountFlagsVoidIsNonOptional)
	}
	iife := ast.GetImmediatelyInvokedFunctionExpression(node.Parent())
	if !iife.IsNil() {
		parameterIndex := core.FindIndex(node.Parent().Parameters(), func(p ast.Handle) bool {
			return p == node
		})
		return node.Type().IsNil() && node.ParameterDeclarationDotDotDotToken().IsNil() && parameterIndex >= len(c.getEffectiveCallArguments(iife))
	}
	return false
}
func isEmptyArrayLiteral(expression ast.Handle) bool {
	return ast.IsArrayLiteralExpression(expression) && len(expression.Elements()) == 0
}
func declarationBelongsToPrivateAmbientMember(declaration ast.Handle) bool {
	root := ast.GetRootDeclaration(declaration)
	memberDeclaration := root
	if root.Kind() == ast.KindParameter {
		memberDeclaration = root.Parent()
	}
	return isPrivateWithinAmbient(memberDeclaration)
}
func isPrivateWithinAmbient(node ast.Handle) bool {
	return (ast.HasModifier(node, ast.ModifierFlagsPrivate) || ast.IsPrivateIdentifierClassElementDeclaration(node)) && node.Flags()&ast.NodeFlagsAmbient != 0
}
func isTypeAssertion(node ast.Handle) bool {
	return ast.IsAssertionExpression(ast.SkipParentheses(node))
}
func createSymbolTable(symbols []*ast.Symbol) ast.SymbolTable {
	if len(symbols) == 0 {
		return nil
	}
	result := make(ast.SymbolTable)
	for _, symbol := range symbols {
		result[symbol.Name] = symbol
	}
	return result
}
func (c *Checker) sortSymbols(symbols []*ast.Symbol) {
	slices.SortFunc(symbols, c.compareSymbols)
}
func (c *Checker) compareSymbolsWorker(s1, s2 *ast.Symbol) int {
	if s1 == s2 {
		return 0
	}
	if s1 == nil {
		return 1
	}
	if s2 == nil {
		return -1
	}
	if len(s1.Declarations) != 0 && len(s2.Declarations) != 0 {
		if r := c.compareNodes(ast.NodeOf(s1.Declarations[0]), ast.NodeOf(s2.Declarations[0])); r != 0 {
			return r
		}
	} else if len(s1.Declarations) != 0 {
		return -1
	} else if len(s2.Declarations) != 0 {
		return 1
	}
	if r := strings.Compare(s1.Name, s2.Name); r != 0 {
		return r
	}
	return int(ast.GetSymbolId(s1)) - int(ast.GetSymbolId(s2))
}
func (c *Checker) compareNodes(n1, n2 ast.Handle) int {
	if n1 == n2 {
		return 0
	}
	if n1.IsNil() {
		return 1
	}
	if n2.IsNil() {
		return -1
	}
	s1 := ast.GetSourceFileOfNode(n1)
	s2 := ast.GetSourceFileOfNode(n2)
	if s1 != s2 {
		f1 := c.fileIndexMap[s1]
		f2 := c.fileIndexMap[s2]
		return f1 - f2
	}
	return n1.Pos() - n2.Pos()
}
func CompareTypes(t1, t2 *Type) int {
	if t1 == t2 {
		return 0
	}
	if t1 == nil {
		return -1
	}
	if t2 == nil {
		return 1
	}
	if t1.checker != t2.checker {
		panic("Cannot compare types from different checkers")
	}
	if c := getSortOrderFlags(t1) - getSortOrderFlags(t2); c != 0 {
		return c
	}
	if c := compareTypeNames(t1, t2); c != 0 {
		return c
	}
	switch {
	case t1.flags&(TypeFlagsAny|TypeFlagsUnknown|TypeFlagsString|TypeFlagsNumber|TypeFlagsBoolean|TypeFlagsBigInt|TypeFlagsESSymbol|TypeFlagsVoid|TypeFlagsUndefined|TypeFlagsNull|TypeFlagsNever|TypeFlagsNonPrimitive) != 0:
	case t1.flags&TypeFlagsObject != 0:
		if c := t1.checker.compareSymbols(t1.symbol, t2.symbol); c != 0 {
			return c
		}
		if t1.objectFlags&ObjectFlagsReference != 0 && t2.objectFlags&ObjectFlagsReference != 0 {
			r1 := t1.AsTypeReference()
			r2 := t2.AsTypeReference()
			if r1.target.objectFlags&ObjectFlagsTuple != 0 && r2.target.objectFlags&ObjectFlagsTuple != 0 {
				if c := compareTupleTypes(r1.target.AsTupleType(), r2.target.AsTupleType()); c != 0 {
					return c
				}
			}
			if r1.node.IsNil() && r2.node.IsNil() {
				if c := compareTypeLists(t1.AsTypeReference().resolvedTypeArguments, t2.AsTypeReference().resolvedTypeArguments); c != 0 {
					return c
				}
			} else {
				if c := t1.checker.compareNodes(r1.node, r2.node); c != 0 {
					return c
				}
				if c := compareTypeMappers(t1.AsObjectType().mapper, t2.AsObjectType().mapper); c != 0 {
					return c
				}
			}
		} else if t1.objectFlags&ObjectFlagsReference != 0 {
			return -1
		} else if t2.objectFlags&ObjectFlagsReference != 0 {
			return 1
		} else {
			if c := int(t1.objectFlags&ObjectFlagsObjectTypeKindMask) - int(t2.objectFlags&ObjectFlagsObjectTypeKindMask); c != 0 {
				return c
			}
			if c := compareTypeMappers(t1.AsObjectType().mapper, t2.AsObjectType().mapper); c != 0 {
				return c
			}
		}
	case t1.flags&TypeFlagsUnion != 0:
		o1 := t1.AsUnionType().origin
		o2 := t2.AsUnionType().origin
		if o1 == nil && o2 == nil {
			if c := compareTypeLists(t1.Types(), t2.Types()); c != 0 {
				return c
			}
		} else if o1 == nil {
			return 1
		} else if o2 == nil {
			return -1
		} else {
			if c := CompareTypes(o1, o2); c != 0 {
				return c
			}
		}
	case t1.flags&TypeFlagsIntersection != 0:
		if c := compareTypeLists(t1.Types(), t2.Types()); c != 0 {
			return c
		}
	case t1.flags&(TypeFlagsEnum|TypeFlagsEnumLiteral|TypeFlagsUniqueESSymbol) != 0:
		if c := t1.checker.compareSymbols(t1.symbol, t2.symbol); c != 0 {
			return c
		}
	case t1.flags&TypeFlagsStringLiteral != 0:
		if c := strings.Compare(t1.AsLiteralType().value.(string), t2.AsLiteralType().value.(string)); c != 0 {
			return c
		}
	case t1.flags&TypeFlagsNumberLiteral != 0:
		if c := cmp.Compare(t1.AsLiteralType().value.(jsnum.Number), t2.AsLiteralType().value.(jsnum.Number)); c != 0 {
			return c
		}
	case t1.flags&TypeFlagsBooleanLiteral != 0:
		b1 := t1.AsLiteralType().value.(bool)
		b2 := t2.AsLiteralType().value.(bool)
		if b1 != b2 {
			if b1 {
				return 1
			}
			return -1
		}
	case t1.flags&TypeFlagsTypeParameter != 0:
		if c := t1.checker.compareSymbols(t1.symbol, t2.symbol); c != 0 {
			return c
		}
	case t1.flags&TypeFlagsIndex != 0:
		if c := CompareTypes(t1.AsIndexType().target, t2.AsIndexType().target); c != 0 {
			return c
		}
		if c := int(t1.AsIndexType().indexFlags) - int(t2.AsIndexType().indexFlags); c != 0 {
			return c
		}
	case t1.flags&TypeFlagsIndexedAccess != 0:
		if c := CompareTypes(t1.AsIndexedAccessType().objectType, t2.AsIndexedAccessType().objectType); c != 0 {
			return c
		}
		if c := CompareTypes(t1.AsIndexedAccessType().indexType, t2.AsIndexedAccessType().indexType); c != 0 {
			return c
		}
	case t1.flags&TypeFlagsConditional != 0:
		if c := t1.checker.compareNodes(t1.AsConditionalType().root.node, t2.AsConditionalType().root.node); c != 0 {
			return c
		}
		if c := compareTypeMappers(t1.AsConditionalType().mapper, t2.AsConditionalType().mapper); c != 0 {
			return c
		}
	case t1.flags&TypeFlagsSubstitution != 0:
		if c := CompareTypes(t1.AsSubstitutionType().baseType, t2.AsSubstitutionType().baseType); c != 0 {
			return c
		}
		if c := CompareTypes(t1.AsSubstitutionType().constraint, t2.AsSubstitutionType().constraint); c != 0 {
			return c
		}
	case t1.flags&TypeFlagsTemplateLiteral != 0:
		if c := slices.Compare(t1.AsTemplateLiteralType().texts, t2.AsTemplateLiteralType().texts); c != 0 {
			return c
		}
		if c := compareTypeLists(t1.AsTemplateLiteralType().types, t2.AsTemplateLiteralType().types); c != 0 {
			return c
		}
	case t1.flags&TypeFlagsStringMapping != 0:
		if c := CompareTypes(t1.AsStringMappingType().target, t2.AsStringMappingType().target); c != 0 {
			return c
		}
	}
	return int(t1.id) - int(t2.id)
}
func getSortOrderFlags(t *Type) int {
	if t.flags&(TypeFlagsEnumLiteral|TypeFlagsEnum) != 0 && t.flags&TypeFlagsUnion == 0 {
		return int(TypeFlagsEnum)
	}
	return int(t.flags)
}
func compareTypeNames(t1, t2 *Type) int {
	s1 := getTypeNameSymbol(t1)
	s2 := getTypeNameSymbol(t2)
	if s1 == s2 {
		if t1.alias != nil {
			return compareTypeLists(t1.alias.typeArguments, t2.alias.typeArguments)
		}
		return 0
	}
	if s1 == nil {
		return 1
	}
	if s2 == nil {
		return -1
	}
	return strings.Compare(s1.Name, s2.Name)
}
func getTypeNameSymbol(t *Type) *ast.Symbol {
	if t.alias != nil {
		return t.alias.symbol
	}
	if t.flags&(TypeFlagsTypeParameter|TypeFlagsStringMapping) != 0 || t.objectFlags&(ObjectFlagsClassOrInterface|ObjectFlagsReference) != 0 {
		return t.symbol
	}
	return nil
}
func getObjectTypeName(t *Type) *ast.Symbol {
	if t.objectFlags&(ObjectFlagsClassOrInterface|ObjectFlagsReference) != 0 {
		return t.symbol
	}
	return nil
}
func compareTupleTypes(t1, t2 *TupleType) int {
	if t1 == t2 {
		return 0
	}
	if t1.readonly != t2.readonly {
		return core.IfElse(t1.readonly, 1, -1)
	}
	if len(t1.elementInfos) != len(t2.elementInfos) {
		return len(t1.elementInfos) - len(t2.elementInfos)
	}
	for i := range t1.elementInfos {
		if c := int(t1.elementInfos[i].flags) - int(t2.elementInfos[i].flags); c != 0 {
			return c
		}
	}
	for i := range t1.elementInfos {
		if c := compareElementLabels(t1.elementInfos[i].labeledDeclaration, t2.elementInfos[i].labeledDeclaration); c != 0 {
			return c
		}
	}
	return 0
}
func compareElementLabels(n1, n2 ast.Handle) int {
	if n1 == n2 {
		return 0
	}
	if n1.IsNil() {
		return -1
	}
	if n2.IsNil() {
		return 1
	}
	return strings.Compare(n1.Name().Text(), n2.Name().Text())
}
func compareTypeLists(s1, s2 []*Type) int {
	if len(s1) != len(s2) {
		return len(s1) - len(s2)
	}
	for i, t1 := range s1 {
		if c := CompareTypes(t1, s2[i]); c != 0 {
			return c
		}
	}
	return 0
}
func compareTypeMappers(m1, m2 *TypeMapper) int {
	if m1 == m2 {
		return 0
	}
	if m1 == nil {
		return 1
	}
	if m2 == nil {
		return -1
	}
	kind1 := m1.Kind()
	kind2 := m2.Kind()
	if kind1 != kind2 {
		return int(kind1) - int(kind2)
	}
	switch kind1 {
	case TypeMapperKindSimple:
		m1 := m1.data.(*SimpleTypeMapper)
		m2 := m2.data.(*SimpleTypeMapper)
		if c := CompareTypes(m1.source, m2.source); c != 0 {
			return c
		}
		return CompareTypes(m1.target, m2.target)
	case TypeMapperKindArray:
		m1 := m1.data.(*ArrayTypeMapper)
		m2 := m2.data.(*ArrayTypeMapper)
		if c := compareTypeLists(m1.sources, m2.sources); c != 0 {
			return c
		}
		return compareTypeLists(m1.targets, m2.targets)
	case TypeMapperKindMerged:
		m1 := m1.data.(*MergedTypeMapper)
		m2 := m2.data.(*MergedTypeMapper)
		if c := compareTypeMappers(m1.m1, m2.m1); c != 0 {
			return c
		}
		return compareTypeMappers(m1.m2, m2.m2)
	}
	return 0
}
func getDeclarationModifierFlagsFromSymbol(s *ast.Symbol) ast.ModifierFlags {
	return getDeclarationModifierFlagsFromSymbolEx(s, false)
}
func getDeclarationModifierFlagsFromSymbolEx(s *ast.Symbol, isWrite bool) ast.ModifierFlags {
	if s.CheckFlags&ast.CheckFlagsSynthetic != 0 {
		var accessModifier ast.ModifierFlags
		switch {
		case !isWrite && s.CheckFlags&ast.CheckFlagsContainsPublic != 0 || isWrite && s.CheckFlags&ast.CheckFlagsContainsWritePublic != 0:
			accessModifier = ast.ModifierFlagsPublic
		case !isWrite && s.CheckFlags&ast.CheckFlagsContainsProtected != 0 || isWrite && s.CheckFlags&ast.CheckFlagsContainsWriteProtected != 0:
			accessModifier = ast.ModifierFlagsProtected
		case !isWrite && s.CheckFlags&ast.CheckFlagsContainsPrivate != 0 || isWrite && s.CheckFlags&ast.CheckFlagsContainsWritePrivate != 0:
			accessModifier = ast.ModifierFlagsPrivate
		}
		if s.CheckFlags&ast.CheckFlagsContainsStatic != 0 {
			return accessModifier | ast.ModifierFlagsStatic
		}
		return accessModifier
	}
	if s.ValueDeclaration != 0 {
		var declaration ast.Handle
		if isWrite {
			declaration = ast.FindSymbolDeclaration(s, ast.IsSetAccessorDeclaration)
		}
		if declaration.IsNil() && s.Flags&ast.SymbolFlagsGetAccessor != 0 {
			declaration = ast.FindSymbolDeclaration(s, ast.IsGetAccessorDeclaration)
		}
		if declaration.IsNil() {
			declaration = ast.NodeOf(s.ValueDeclaration)
		}
		flags := ast.GetCombinedModifierFlags(declaration)
		if s.Parent != nil && s.Parent.Flags&ast.SymbolFlagsClass != 0 {
			return flags
		}
		return flags & ^ast.ModifierFlagsAccessibilityModifier
	}
	if s.Flags&ast.SymbolFlagsPrototype != 0 {
		return ast.ModifierFlagsPublic | ast.ModifierFlagsStatic
	}
	return ast.ModifierFlagsNone
}
func isExponentiationOperator(kind ast.Kind) bool {
	return kind == ast.KindAsteriskAsteriskToken
}
func isMultiplicativeOperator(kind ast.Kind) bool {
	return kind == ast.KindAsteriskToken || kind == ast.KindSlashToken || kind == ast.KindPercentToken
}
func isMultiplicativeOperatorOrHigher(kind ast.Kind) bool {
	return isExponentiationOperator(kind) || isMultiplicativeOperator(kind)
}
func isAdditiveOperator(kind ast.Kind) bool {
	return kind == ast.KindPlusToken || kind == ast.KindMinusToken
}
func isAdditiveOperatorOrHigher(kind ast.Kind) bool {
	return isAdditiveOperator(kind) || isMultiplicativeOperatorOrHigher(kind)
}
func isShiftOperator(kind ast.Kind) bool {
	return kind == ast.KindLessThanLessThanToken || kind == ast.KindGreaterThanGreaterThanToken || kind == ast.KindGreaterThanGreaterThanGreaterThanToken
}
func isShiftOperatorOrHigher(kind ast.Kind) bool {
	return isShiftOperator(kind) || isAdditiveOperatorOrHigher(kind)
}
func isRelationalOperator(kind ast.Kind) bool {
	return kind == ast.KindLessThanToken || kind == ast.KindLessThanEqualsToken || kind == ast.KindGreaterThanToken || kind == ast.KindGreaterThanEqualsToken || kind == ast.KindInstanceOfKeyword || kind == ast.KindInKeyword
}
func isRelationalOperatorOrHigher(kind ast.Kind) bool {
	return isRelationalOperator(kind) || isShiftOperatorOrHigher(kind)
}
func isEqualityOperator(kind ast.Kind) bool {
	return kind == ast.KindEqualsEqualsToken || kind == ast.KindEqualsEqualsEqualsToken || kind == ast.KindExclamationEqualsToken || kind == ast.KindExclamationEqualsEqualsToken
}
func isEqualityOperatorOrHigher(kind ast.Kind) bool {
	return isEqualityOperator(kind) || isRelationalOperatorOrHigher(kind)
}
func isBitwiseOperator(kind ast.Kind) bool {
	return kind == ast.KindAmpersandToken || kind == ast.KindBarToken || kind == ast.KindCaretToken
}
func isBitwiseOperatorOrHigher(kind ast.Kind) bool {
	return isBitwiseOperator(kind) || isEqualityOperatorOrHigher(kind)
}
func isLogicalOperatorOrHigher(kind ast.Kind) bool {
	return ast.IsLogicalBinaryOperator(kind) || isBitwiseOperatorOrHigher(kind)
}
func isAssignmentOperatorOrHigher(kind ast.Kind) bool {
	return kind == ast.KindQuestionQuestionToken || isLogicalOperatorOrHigher(kind) || ast.IsAssignmentOperator(kind)
}
func isBinaryOperator(kind ast.Kind) bool {
	return isAssignmentOperatorOrHigher(kind) || kind == ast.KindCommaToken
}
func isObjectLiteralType(t *Type) bool {
	return t.objectFlags&ObjectFlagsObjectLiteral != 0
}
func isDeclarationReadonly(declaration ast.Handle) bool {
	return ast.GetCombinedModifierFlags(declaration)&ast.ModifierFlagsReadonly != 0 && !ast.IsParameterPropertyDeclaration(declaration, declaration.Parent())
}

const orderedSetMapThreshold = 16

type orderedSet[T comparable] struct {
	valuesByKey map[T]struct{}
	values      []T
}

func (s *orderedSet[T]) contains(value T) bool {
	if s.valuesByKey == nil {
		return slices.Contains(s.values, value)
	}
	_, ok := s.valuesByKey[value]
	return ok
}
func (s *orderedSet[T]) add(value T) {
	s.values = append(s.values, value)
	if s.valuesByKey == nil {
		if len(s.values) <= orderedSetMapThreshold {
			return
		}
		s.valuesByKey = make(map[T]struct{}, len(s.values))
		for _, v := range s.values[:len(s.values)-1] {
			s.valuesByKey[v] = struct{}{}
		}
	}
	s.valuesByKey[value] = struct{}{}
}
func getContainingFunctionOrClassStaticBlock(node ast.Handle) ast.Handle {
	return ast.FindAncestor(node.Parent(), ast.IsFunctionLikeOrClassStaticBlockDeclaration)
}
func isNodeDescendantOf(node ast.Handle, ancestor ast.Handle) bool {
	for !node.IsNil() {
		if node == ancestor {
			return true
		}
		node = node.Parent()
	}
	return false
}
func isTypeUsableAsPropertyName(t *Type) bool {
	return t.flags&TypeFlagsStringOrNumberLiteralOrUnique != 0
}

func getPropertyNameFromType(t *Type) string {
	switch {
	case t.flags&TypeFlagsStringLiteral != 0:
		return t.AsLiteralType().value.(string)
	case t.flags&TypeFlagsNumberLiteral != 0:
		return t.AsLiteralType().value.(jsnum.Number).String()
	case t.flags&TypeFlagsUniqueESSymbol != 0:
		return t.AsUniqueESSymbolType().name
	}
	panic("Unhandled case in getPropertyNameFromType")
}
func isNumericLiteralName(name string) bool {
	return jsnum.FromString(name).String() == name
}
func isThisProperty(node ast.Handle) bool {
	return (ast.IsPropertyAccessExpression(node) || ast.IsElementAccessExpression(node)) && node.Expression().Kind() == ast.KindThisKeyword
}
func isValidNumberString(s string, roundTripOnly bool) bool {
	if s == "" {
		return false
	}
	n := jsnum.FromString(s)
	return !n.IsNaN() && !n.IsInf() && (!roundTripOnly || n.String() == s)
}
func isValidBigIntString(s string, roundTripOnly bool) bool {
	if s == "" {
		return false
	}
	scanner := scanner.NewScanner()
	scanner.SetSkipTrivia(false)
	success := true
	scanner.SetOnError(func(diagnostic *diagnostics.Message, start, length int, args ...any) {
		success = false
	})
	scanner.SetText(s + "n")
	result := scanner.Scan()
	negative := result == ast.KindMinusToken
	if negative {
		result = scanner.Scan()
	}
	flags := scanner.TokenFlags()
	return success && result == ast.KindBigIntLiteral && scanner.TokenEnd() == len(s)+1 && flags&ast.TokenFlagsContainsSeparator == 0 && (!roundTripOnly || s == pseudoBigIntToString(jsnum.NewPseudoBigInt(jsnum.ParsePseudoBigInt(scanner.TokenValue()), negative)))
}
func isValidESSymbolDeclaration(node ast.Handle) bool {
	if ast.IsVariableDeclaration(node) {
		return ast.IsVarConst(node) && ast.IsIdentifier(node.VariableDeclarationName()) && isVariableDeclarationInVariableStatement(node)
	}
	if ast.IsPropertyDeclaration(node) {
		return hasReadonlyModifier(node) && ast.HasStaticModifier(node)
	}
	return ast.IsPropertySignatureDeclaration(node) && hasReadonlyModifier(node)
}
func isVariableDeclarationInVariableStatement(node ast.Handle) bool {
	return ast.IsVariableDeclarationList(node.Parent()) && ast.IsVariableStatement(node.Parent().Parent())
}
func IsKnownSymbol(symbol *ast.Symbol) bool {
	return isLateBoundName(symbol.Name)
}
func IsPrivateIdentifierSymbol(symbol *ast.Symbol) bool {
	if symbol == nil {
		return false
	}
	return strings.HasPrefix(symbol.Name, ast.InternalSymbolNamePrefix+"#")
}
func isLateBoundName(name string) bool {
	return len(name) >= 2 && name[0] == '\xfe' && name[1] == '@'
}
func isObjectOrArrayLiteralType(t *Type) bool {
	return t.objectFlags&(ObjectFlagsObjectLiteral|ObjectFlagsArrayLiteral) != 0
}
func getContainingClassExcludingClassDecorators(node ast.Handle) ast.Handle {
	decorator := ast.FindAncestorOrQuit(node.Parent(), func(n ast.Handle) ast.FindAncestorResult {
		if ast.IsClassLike(n) {
			return ast.FindAncestorQuit
		}
		if ast.IsDecorator(n) {
			return ast.FindAncestorTrue
		}
		return ast.FindAncestorFalse
	})
	if !decorator.IsNil() && ast.IsClassLike(decorator.Parent()) {
		return ast.GetContainingClass(decorator.Parent())
	}
	if !decorator.IsNil() {
		return ast.GetContainingClass(decorator)
	}
	return ast.GetContainingClass(node)
}
func isThisTypeParameter(t *Type) bool {
	return t.flags&TypeFlagsTypeParameter != 0 && t.AsTypeParameter().isThisType
}
func isClassInstanceProperty(node ast.Handle) bool {
	if ast.IsInJSFile(node) && ast.IsExpandoPropertyDeclaration(node) {
		left := node.BinaryExpressionLeft()
		return (!ast.IsBindableStaticAccessExpression(left, false) || !ast.IsPrototypeAccess(left.Expression())) && !ast.IsBindableStaticNameExpression(left, true)
	}
	return !node.Parent().IsNil() && ast.IsClassLike(node.Parent()) && ast.IsPropertyDeclaration(node) && !ast.HasAccessorModifier(node)
}
func isThisInitializedObjectBindingExpression(node ast.Handle) bool {
	return !node.IsNil() && (ast.IsShorthandPropertyAssignment(node) || ast.IsPropertyAssignment(node)) && ast.IsBinaryExpression(node.Parent().Parent()) && node.Parent().Parent().BinaryExpressionOperatorToken().Kind() == ast.KindEqualsToken && node.Parent().Parent().BinaryExpressionRight().Kind() == ast.KindThisKeyword
}
func isThisInitializedDeclaration(node ast.Handle) bool {
	return !node.IsNil() && ast.IsVariableDeclaration(node) && !node.Initializer().IsNil() && node.Initializer().Kind() == ast.KindThisKeyword
}
func isInfinityOrNaNString(name string) bool {
	return name == "Infinity" || name == "-Infinity" || name == "NaN"
}
func (c *Checker) isConstantVariable(symbol *ast.Symbol) bool {
	return symbol.Flags&ast.SymbolFlagsVariable != 0 && (c.getDeclarationNodeFlagsFromSymbol(symbol)&ast.NodeFlagsConstant) != 0
}
func (c *Checker) isParameterOrMutableLocalVariable(symbol *ast.Symbol) bool {
	if symbol.ValueDeclaration != 0 {
		declaration := ast.GetRootDeclaration(ast.NodeOf(symbol.ValueDeclaration))
		return !declaration.IsNil() && (ast.IsParameterDeclaration(declaration) || ast.IsVariableDeclaration(declaration) && (ast.IsCatchClause(declaration.Parent()) || c.isMutableLocalVariableDeclaration(declaration)))
	}
	return false
}
func (c *Checker) isMutableLocalVariableDeclaration(declaration ast.Handle) bool {
	return declaration.Parent().Flags()&ast.NodeFlagsLet != 0 && !(ast.GetCombinedModifierFlags(declaration)&ast.ModifierFlagsExport != 0 || declaration.Parent().Parent().Kind() == ast.KindVariableStatement && ast.IsGlobalSourceFile(declaration.Parent().Parent().Parent()))
}
func isInAmbientOrTypeNode(node ast.Handle) bool {
	return node.Flags()&ast.NodeFlagsAmbient != 0 || !ast.FindAncestor(node, func(n ast.Handle) bool {
		return ast.IsInterfaceDeclaration(n) || ast.IsTypeOrJSTypeAliasDeclaration(n) || ast.IsTypeLiteralNode(n)
	}).IsNil()
}
func isLiteralExpressionOfObject(node ast.Handle) bool {
	switch node.Kind() {
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression, ast.KindRegularExpressionLiteral, ast.KindFunctionExpression, ast.KindClassExpression:
		return true
	}
	return false
}
func canHaveFlowNode(node ast.Handle) bool {
	return node.FlowNode() != nil
}
func isNonNullAccess(node ast.Handle) bool {
	return ast.IsAccessExpression(node) && ast.IsNonNullExpression(node.Expression())
}
func getBindingElementPropertyName(node ast.Handle) ast.Handle {
	return node.PropertyNameOrName()
}
func isCallChain(node ast.Handle) bool {
	return ast.IsCallExpression(node) && node.Flags()&ast.NodeFlagsOptionalChain != 0
}
func (c *Checker) callLikeExpressionMayHaveTypeArguments(node ast.Handle) bool {
	return ast.IsCallOrNewExpression(node) || ast.IsTaggedTemplateExpression(node) || ast.IsJsxOpeningLikeElement(node)
}
func isSuperCall(n ast.Handle) bool {
	return ast.IsCallExpression(n) && n.Expression().Kind() == ast.KindSuperKeyword
}
func getMembersOfDeclaration(node ast.Handle) []ast.Handle {
	if list := node.MemberList(); list != 0 {
		return node.Store().ListSlice(list)
	}
	return node.Properties()
}
func isInRightSideOfImportOrExportAssignment(node ast.Handle) bool {
	for node.Parent().Kind() == ast.KindQualifiedName {
		node = node.Parent()
	}
	return node.Parent().Kind() == ast.KindImportEqualsDeclaration && node.Parent().ImportEqualsDeclarationModuleReference() == node || node.Parent().Kind() == ast.KindExportAssignment && node.Parent().Expression() == node
}
func isJsxIntrinsicTagName(tagName ast.Handle) bool {
	return ast.IsIdentifier(tagName) && scanner.IsIntrinsicJsxName(tagName.Text()) || ast.IsJsxNamespacedName(tagName)
}
func getContainingObjectLiteral(f ast.Handle) ast.Handle {
	if (f.Kind() == ast.KindMethodDeclaration || f.Kind() == ast.KindGetAccessor || f.Kind() == ast.KindSetAccessor) && f.Parent().Kind() == ast.KindObjectLiteralExpression {
		return f.Parent()
	} else if f.Kind() == ast.KindFunctionExpression && f.Parent().Kind() == ast.KindPropertyAssignment {
		return f.Parent().Parent()
	}
	return ast.Handle{}
}
func isImportTypeQualifierPart(node ast.Handle) ast.Handle {
	parent := node.Parent()
	for ast.IsQualifiedName(parent) {
		node = parent
		parent = parent.Parent()
	}
	if !parent.IsNil() && parent.Kind() == ast.KindImportType && parent.ImportTypeNodeQualifier() == node {
		return parent
	}
	return ast.Handle{}
}
func isInNameOfExpressionWithTypeArgumentsOrHeritageTypeReference(node ast.Handle) bool {
	for node.Parent().Kind() == ast.KindPropertyAccessExpression || node.Parent().Kind() == ast.KindQualifiedName {
		node = node.Parent()
	}
	return node.Parent().Kind() == ast.KindExpressionWithTypeArguments || ast.IsNameOfHeritageClauseTypeReference(node)
}
func getIndexSymbolFromSymbolTable(symbolTable ast.SymbolTable) *ast.Symbol {
	return symbolTable[ast.InternalSymbolNameIndex]
}

func expressionResultIsUnused(node ast.Handle) bool {
	for {
		parent := node.Parent()
		if ast.IsParenthesizedExpression(parent) {
			node = parent
			continue
		}
		if ast.IsExpressionStatement(parent) || ast.IsVoidExpression(parent) || ast.IsForStatement(parent) && (parent.Initializer() == node || parent.ForStatementIncrementor() == node) {
			return true
		}
		if ast.IsBinaryExpression(parent) && parent.BinaryExpressionOperatorToken().Kind() == ast.KindCommaToken {
			if node == parent.BinaryExpressionLeft() {
				return true
			}
			node = parent
			continue
		}
		return false
	}
}
func pseudoBigIntToString(value jsnum.PseudoBigInt) string {
	return value.String()
}
func getSuperContainer(node ast.Handle, stopOnFunctions bool) ast.Handle {
	for {
		node = node.Parent()
		if node.IsNil() {
			return ast.Handle{}
		}
		switch node.Kind() {
		case ast.KindComputedPropertyName:
			node = node.Parent()
		case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
			if !stopOnFunctions {
				continue
			}
			fallthrough
		case ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindClassStaticBlockDeclaration:
			return node
		case ast.KindDecorator:
			if ast.IsParameterDeclaration(node.Parent()) && ast.IsClassElement(node.Parent().Parent()) {
				node = node.Parent().Parent()
			} else if ast.IsClassElement(node.Parent()) {
				node = node.Parent()
			}
		}
	}
}
func forEachYieldExpression(body ast.Handle, visitor func(expr ast.Handle) bool) bool {
	var traverse func(ast.Handle) bool
	traverse = func(node ast.Handle) bool {
		switch node.Kind() {
		case ast.KindYieldExpression:
			if visitor(node) {
				return true
			}
			operand := node.Expression()
			if operand.IsNil() {
				return false
			}
			return traverse(operand)
		case ast.KindEnumDeclaration, ast.KindInterfaceDeclaration, ast.KindModuleDeclaration, ast.KindTypeAliasDeclaration:
		default:
			if ast.IsFunctionLike(node) {
				if !node.Name().IsNil() && ast.IsComputedPropertyName(node.Name()) {
					return traverse(node.Name().Expression())
				}
			} else if !ast.IsPartOfTypeNode(node) {
				return node.ForEachChild(traverse)
			}
		}
		return false
	}
	return traverse(body)
}
func getEnclosingContainer(node ast.Handle) ast.Handle {
	return ast.FindAncestor(node.Parent(), func(n ast.Handle) bool {
		return binder.GetContainerFlags(n)&binder.ContainerFlagsIsContainer != 0
	})
}
func getDeclarationsOfKind(symbol *ast.Symbol, kind ast.Kind) []ast.Handle {
	return core.Filter(ast.DeclarationNodes(symbol), func(d ast.Handle) bool {
		return d.Kind() == kind
	})
}
func hasType(node ast.Handle) bool {
	return !node.Type().IsNil()
}
func getNonRestParameterCount(sig *Signature) int {
	return len(sig.parameters) - core.IfElse(signatureHasRestParameter(sig), 1, 0)
}
func minAndMax[T any](slice []T, getValue func(value T) int) (int, int) {
	var minValue, maxValue int
	for i, element := range slice {
		value := getValue(element)
		if i == 0 {
			minValue = value
			maxValue = value
		} else {
			minValue = min(minValue, value)
			maxValue = max(maxValue, value)
		}
	}
	return minValue, maxValue
}

type FeatureMapEntry struct {
	lib   string
	props []string
}

var getFeatureMap = sync.OnceValue(func() map[string][]FeatureMapEntry {
	return map[string][]FeatureMapEntry{"Array": {{lib: "es2015", props: []string{"find", "findIndex", "fill", "copyWithin", "entries", "keys", "values"}}, {lib: "es2016", props: []string{"includes"}}, {lib: "es2019", props: []string{"flat", "flatMap"}}, {lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "Iterator": {{lib: "es2015", props: []string{}}}, "AsyncIterator": {{lib: "es2015", props: []string{}}}, "ArrayBuffer": {{lib: "es2024", props: []string{"maxByteLength", "resizable", "resize", "detached", "transfer", "transferToFixedLength"}}}, "Atomics": {{lib: "es2017", props: []string{"add", "and", "compareExchange", "exchange", "isLockFree", "load", "or", "store", "sub", "wait", "notify", "xor"}}, {lib: "es2024", props: []string{"waitAsync"}}}, "SharedArrayBuffer": {{lib: "es2017", props: []string{"byteLength", "slice"}}, {lib: "es2024", props: []string{"growable", "maxByteLength", "grow"}}}, "AsyncIterable": {{lib: "es2018", props: []string{}}}, "AsyncIterableIterator": {{lib: "es2018", props: []string{}}}, "AsyncGenerator": {{lib: "es2018", props: []string{}}}, "AsyncGeneratorFunction": {{lib: "es2018", props: []string{}}}, "RegExp": {{lib: "es2015", props: []string{"flags", "sticky", "unicode"}}, {lib: "es2018", props: []string{"dotAll"}}, {lib: "es2024", props: []string{"unicodeSets"}}}, "RegExpConstructor": {{lib: "es2025", props: []string{"escape"}}}, "Reflect": {{lib: "es2015", props: []string{"apply", "construct", "defineProperty", "deleteProperty", "get", "getOwnPropertyDescriptor", "getPrototypeOf", "has", "isExtensible", "ownKeys", "preventExtensions", "set", "setPrototypeOf"}}}, "ArrayConstructor": {{lib: "es2015", props: []string{"from", "of"}}, {lib: "esnext", props: []string{"fromAsync"}}}, "ObjectConstructor": {{lib: "es2015", props: []string{"assign", "getOwnPropertySymbols", "keys", "is", "setPrototypeOf"}}, {lib: "es2017", props: []string{"values", "entries", "getOwnPropertyDescriptors"}}, {lib: "es2019", props: []string{"fromEntries"}}, {lib: "es2022", props: []string{"hasOwn"}}, {lib: "es2024", props: []string{"groupBy"}}}, "NumberConstructor": {{lib: "es2015", props: []string{"isFinite", "isInteger", "isNaN", "isSafeInteger", "parseFloat", "parseInt"}}}, "Math": {{lib: "es2015", props: []string{"clz32", "imul", "sign", "log10", "log2", "log1p", "expm1", "cosh", "sinh", "tanh", "acosh", "asinh", "atanh", "hypot", "trunc", "fround", "cbrt"}}, {lib: "es2025", props: []string{"f16round"}}}, "Map": {{lib: "es2015", props: []string{"entries", "keys", "values"}}, {lib: "esnext", props: []string{"getOrInsert", "getOrInsertComputed"}}}, "MapConstructor": {{lib: "es2024", props: []string{"groupBy"}}}, "Set": {{lib: "es2015", props: []string{"entries", "keys", "values"}}, {lib: "es2025", props: []string{"union", "intersection", "difference", "symmetricDifference", "isSubsetOf", "isSupersetOf", "isDisjointFrom"}}}, "PromiseConstructor": {{lib: "es2015", props: []string{"all", "race", "reject", "resolve"}}, {lib: "es2020", props: []string{"allSettled"}}, {lib: "es2021", props: []string{"any"}}, {lib: "es2024", props: []string{"withResolvers"}}, {lib: "es2025", props: []string{"try"}}}, "Symbol": {{lib: "es2015", props: []string{"for", "keyFor"}}, {lib: "es2019", props: []string{"description"}}}, "WeakMap": {{lib: "es2015", props: []string{}}, {lib: "esnext", props: []string{"getOrInsert", "getOrInsertComputed"}}}, "WeakSet": {{lib: "es2015", props: []string{}}}, "String": {{lib: "es2015", props: []string{"codePointAt", "includes", "endsWith", "normalize", "repeat", "startsWith", "anchor", "big", "blink", "bold", "fixed", "fontcolor", "fontsize", "italics", "link", "small", "strike", "sub", "sup"}}, {lib: "es2017", props: []string{"padStart", "padEnd"}}, {lib: "es2019", props: []string{"trimStart", "trimEnd", "trimLeft", "trimRight"}}, {lib: "es2020", props: []string{"matchAll"}}, {lib: "es2021", props: []string{"replaceAll"}}, {lib: "es2022", props: []string{"at"}}, {lib: "es2024", props: []string{"isWellFormed", "toWellFormed"}}}, "StringConstructor": {{lib: "es2015", props: []string{"fromCodePoint", "raw"}}}, "DateTimeFormat": {{lib: "es2017", props: []string{"formatToParts"}}}, "Promise": {{lib: "es2015", props: []string{}}, {lib: "es2018", props: []string{"finally"}}}, "RegExpMatchArray": {{lib: "es2018", props: []string{"groups"}}}, "RegExpExecArray": {{lib: "es2018", props: []string{"groups"}}}, "Intl": {{lib: "es2018", props: []string{"PluralRules"}}, {lib: "es2020", props: []string{"RelativeTimeFormat", "Locale", "DisplayNames"}}, {lib: "es2021", props: []string{"ListFormat", "DateTimeFormat"}}, {lib: "es2022", props: []string{"Segmenter"}}, {lib: "es2025", props: []string{"DurationFormat"}}}, "NumberFormat": {{lib: "es2018", props: []string{"formatToParts"}}}, "SymbolConstructor": {{lib: "es2020", props: []string{"matchAll"}}, {lib: "esnext", props: []string{"metadata", "dispose", "asyncDispose"}}}, "DataView": {{lib: "es2020", props: []string{"setBigInt64", "setBigUint64", "getBigInt64", "getBigUint64"}}, {lib: "es2025", props: []string{"setFloat16", "getFloat16"}}}, "BigInt": {{lib: "es2020", props: []string{}}}, "RelativeTimeFormat": {{lib: "es2020", props: []string{"format", "formatToParts", "resolvedOptions"}}}, "Int8Array": {{lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "Uint8Array": {{lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "Uint8ClampedArray": {{lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "Int16Array": {{lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "Uint16Array": {{lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "Int32Array": {{lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "Uint32Array": {{lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "Float16Array": {{lib: "es2025", props: []string{}}}, "Float32Array": {{lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "Float64Array": {{lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "BigInt64Array": {{lib: "es2020", props: []string{}}, {lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "BigUint64Array": {{lib: "es2020", props: []string{}}, {lib: "es2022", props: []string{"at"}}, {lib: "es2023", props: []string{"findLastIndex", "findLast", "toReversed", "toSorted", "toSpliced", "with"}}}, "Error": {{lib: "es2022", props: []string{"cause"}}}, "ErrorConstructor": {{lib: "esnext", props: []string{"isError"}}}, "Uint8ArrayConstructor": {{lib: "esnext", props: []string{"fromBase64", "fromHex"}}}, "DisposableStack": {{lib: "esnext", props: []string{}}}, "AsyncDisposableStack": {{lib: "esnext", props: []string{}}}, "Date": {{lib: "esnext", props: []string{"toTemporalInstant"}}}}
})

func rangeOfTypeParameters(sourceFile *ast.SourceFile, typeParameters ast.ListRef) core.TextRange {
	return core.NewTextRange(sourceFile.ParseStore().ListLoc(typeParameters).Pos()-1, min(len(sourceFile.Text()), scanner.SkipTrivia(sourceFile.Text(), sourceFile.ParseStore().ListLoc(typeParameters).End())+1))
}
func tryGetPropertyAccessOrIdentifierToString(expr ast.Handle) string {
	switch {
	case ast.IsPropertyAccessExpression(expr):
		baseStr := tryGetPropertyAccessOrIdentifierToString(expr.Expression())
		if baseStr != "" {
			return baseStr + "." + entityNameToString(expr.Name())
		}
	case ast.IsElementAccessExpression(expr):
		baseStr := tryGetPropertyAccessOrIdentifierToString(expr.Expression())
		if baseStr != "" && ast.IsPropertyName(expr.ElementAccessExpressionArgumentExpression()) {
			return baseStr + "." + ast.GetPropertyNameForPropertyNameNode(expr.ElementAccessExpressionArgumentExpression())
		}
	case ast.IsIdentifier(expr):
		return expr.Text()
	case ast.IsJsxNamespacedName(expr):
		return entityNameToString(expr)
	}
	return ""
}
func allDeclarationsInSameSourceFile(symbol *ast.Symbol) bool {
	if len(symbol.Declarations) > 1 {
		var sourceFile *ast.SourceFile
		for i, d := range ast.DeclarationNodes(symbol) {
			if i == 0 {
				sourceFile = ast.GetSourceFileOfNode(d)
			} else if ast.GetSourceFileOfNode(d) != sourceFile {
				return false
			}
		}
	}
	return true
}
func containsNonMissingUndefinedType(c *Checker, t *Type) bool {
	var candidate *Type
	if t.flags&TypeFlagsUnion != 0 {
		candidate = t.AsUnionType().types[0]
	} else {
		candidate = t
	}
	return candidate.flags&TypeFlagsUndefined != 0 && candidate != c.missingType
}
func getAnyImportSyntax(node ast.Handle) ast.Handle {
	var importNode ast.Handle
	switch node.Kind() {
	case ast.KindImportEqualsDeclaration:
		importNode = node
	case ast.KindImportClause:
		importNode = node.Parent()
	case ast.KindNamespaceImport:
		importNode = node.Parent().Parent()
	case ast.KindImportSpecifier:
		importNode = node.Parent().Parent().Parent()
	default:
		return ast.Handle{}
	}
	return importNode
}

func isReservedMemberName(name string) bool {
	return len(name) >= 2 && name[0] == '\xFE' && name[1] != '@' && name[1] != '#'
}
func introducesArgumentsExoticObject(node ast.Handle) bool {
	switch node.Kind() {
	case ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindFunctionDeclaration, ast.KindFunctionExpression:
		return true
	}
	return false
}
func symbolsToArray(symbols ast.SymbolTable) []*ast.Symbol {
	var result []*ast.Symbol
	for id, symbol := range symbols {
		if !isReservedMemberName(id) {
			result = append(result, symbol)
		}
	}
	return result
}
func SkipAlias(symbol *ast.Symbol, checker *Checker) *ast.Symbol {
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		return checker.GetAliasedSymbol(symbol)
	}
	return symbol
}

func IsExternalModuleSymbol(moduleSymbol *ast.Symbol) bool {
	firstRune, _ := utf8.DecodeRuneInString(moduleSymbol.Name)
	return moduleSymbol.Flags&ast.SymbolFlagsModule != 0 && firstRune == '"'
}
func (c *Checker) isCanceled() bool {
	return c.ctx != nil && c.ctx.Err() != nil
}
func (c *Checker) checkNotCanceled() {
	if c.wasCanceled {
		panic("Checker was previously cancelled")
	}
}
func (c *Checker) getPackagesMap() map[string]bool {
	if c.packagesMap == nil {
		c.packagesMap = make(map[string]bool)
		resolvedModules := c.program.GetResolvedModules()
		for _, resolvedModulesInFile := range resolvedModules {
			for _, module := range resolvedModulesInFile {
				if module.PackageId.Name != "" {
					c.packagesMap[module.PackageId.Name] = c.packagesMap[module.PackageId.Name] || module.Extension == tspath.ExtensionDts
				}
			}
		}
	}
	return c.packagesMap
}
func (c *Checker) typesPackageExists(packageName string) bool {
	packagesMap := c.getPackagesMap()
	_, ok := packagesMap[module.GetTypesPackageName(packageName)]
	return ok
}
func (c *Checker) packageBundlesTypes(packageName string) bool {
	packagesMap := c.getPackagesMap()
	hasTypes, _ := packagesMap[packageName]
	return hasTypes
}
func ValueToString(value any) string {
	switch value := value.(type) {
	case string:
		return "\"" + printer.EscapeString(value, '"') + "\""
	case jsnum.Number:
		return value.String()
	case bool:
		return core.IfElse(value, "true", "false")
	case jsnum.PseudoBigInt:
		return value.String() + "n"
	}
	panic("unhandled value type in valueToString")
}
func nodeStartsNewLexicalEnvironment(node ast.Handle) bool {
	switch node.Kind() {
	case ast.KindConstructor, ast.KindFunctionExpression, ast.KindFunctionDeclaration, ast.KindArrowFunction, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindModuleDeclaration, ast.KindSourceFile:
		return true
	}
	return false
}

func (c *Checker) isUncheckedJSSuggestion(node ast.Handle, suggestion *ast.Symbol, excludeClasses bool) bool {
	file := ast.GetSourceFileOfNode(node)
	if file != nil {
		if c.compilerOptions.CheckJs.IsUnknown() && file.CheckJsDirective == nil && (file.ScriptKind == core.ScriptKindJS || file.ScriptKind == core.ScriptKindJSX) {
			var declarationFile *ast.SourceFile
			if suggestion != nil {
				if firstDeclaration := core.FirstOrNil(ast.DeclarationNodes(suggestion)); !firstDeclaration.IsNil() {
					declarationFile = ast.GetSourceFileOfNode(firstDeclaration)
				}
			}
			suggestionHasNoExtendsOrDecorators := suggestion == nil || suggestion.ValueDeclaration == 0 || !ast.IsClassLike(ast.NodeOf(suggestion.ValueDeclaration)) || len(ast.GetExtendsHeritageClauseElements(ast.NodeOf(suggestion.ValueDeclaration))) != 0 || ast.ClassOrConstructorParameterIsDecorated(false, ast.NodeOf(suggestion.ValueDeclaration))
			return !(file != declarationFile && declarationFile != nil && ast.IsGlobalSourceFile(declarationFile.ParseRoot())) && !(excludeClasses && suggestion != nil && suggestion.Flags&ast.SymbolFlagsClass != 0 && suggestionHasNoExtendsOrDecorators) && !(!node.IsNil() && excludeClasses && ast.IsPropertyAccessExpression(node) && node.Expression().Kind() == ast.KindThisKeyword && suggestionHasNoExtendsOrDecorators)
		}
	}
	return false
}

func (c *Checker) isJSLiteralType(t *Type) bool {
	if c.noImplicitAny {
		return false
	}
	if t.objectFlags&ObjectFlagsJSLiteral != 0 {
		return true
	}
	if t.flags&TypeFlagsUnion != 0 {
		return core.Every(t.AsUnionType().types, c.isJSLiteralType)
	}
	if t.flags&TypeFlagsIntersection != 0 {
		return core.Some(t.AsIntersectionType().types, c.isJSLiteralType)
	}
	if t.flags&TypeFlagsInstantiable != 0 {
		constraint := c.getResolvedBaseConstraint(t, nil)
		return constraint != t && c.isJSLiteralType(constraint)
	}
	return false
}

type DiagnosticDetails struct {
	Message *diagnostics.Message
	Args    []any
}

func CreateModuleNotFoundChain(program Program, file *ast.SourceFile, moduleReference string, mode core.ResolutionMode, packageName string) DiagnosticDetails {
	resolvedModule := program.GetResolvedModule(file, moduleReference, mode)
	if resolvedModule != nil && resolvedModule.AlternateResult != "" {
		if strings.Contains(resolvedModule.AlternateResult, "/node_modules/@types/") {
			packageName = "@types/" + module.MangleScopedPackageName(packageName)
		}
		return DiagnosticDetails{Message: diagnostics.There_are_types_at_0_but_this_result_could_not_be_resolved_when_respecting_package_json_exports_The_1_library_may_need_to_update_its_package_json_or_typings, Args: []any{resolvedModule.AlternateResult, packageName}}
	}
	packagesMap := program.GetPackagesMap()
	if _, ok := packagesMap[module.GetTypesPackageName(packageName)]; ok {
		return DiagnosticDetails{Message: diagnostics.If_the_0_package_actually_exposes_this_module_consider_sending_a_pull_request_to_amend_https_Colon_Slash_Slashgithub_com_SlashDefinitelyTyped_SlashDefinitelyTyped_Slashtree_Slashmaster_Slashtypes_Slash_1, Args: []any{packageName, module.MangleScopedPackageName(packageName)}}
	}
	if packagesMap[packageName] {
		return DiagnosticDetails{Message: diagnostics.If_the_0_package_actually_exposes_this_module_try_adding_a_new_declaration_d_ts_file_containing_declare_module_1, Args: []any{packageName, moduleReference}}
	}
	return DiagnosticDetails{Message: diagnostics.Try_npm_i_save_dev_types_Slash_1_if_it_exists_or_add_a_new_declaration_d_ts_file_containing_declare_module_0, Args: []any{moduleReference, module.MangleScopedPackageName(packageName)}}
}

func CreateModeMismatchDetails(program Program, file *ast.SourceFile) DiagnosticDetails {
	ext := tspath.TryGetExtensionFromPath(file.FileName())
	targetExt := core.IfElse(ext == tspath.ExtensionTs, tspath.ExtensionMts, core.IfElse(ext == tspath.ExtensionJs, tspath.ExtensionMjs, ""))
	meta := program.GetSourceFileMetaData(file.Path())
	packageJsonType := meta.PackageJsonType
	packageJsonDirectory := meta.PackageJsonDirectory
	if packageJsonDirectory != "" && packageJsonType == "" {
		if targetExt != "" {
			return DiagnosticDetails{Message: diagnostics.To_convert_this_file_to_an_ECMAScript_module_change_its_file_extension_to_0_or_add_the_field_type_Colon_module_to_1, Args: []any{targetExt, tspath.CombinePaths(packageJsonDirectory, "package.json")}}
		}
		return DiagnosticDetails{Message: diagnostics.To_convert_this_file_to_an_ECMAScript_module_add_the_field_type_Colon_module_to_0, Args: []any{tspath.CombinePaths(packageJsonDirectory, "package.json")}}
	}
	if targetExt != "" {
		return DiagnosticDetails{Message: diagnostics.To_convert_this_file_to_an_ECMAScript_module_change_its_file_extension_to_0_or_create_a_local_package_json_file_with_type_Colon_module, Args: []any{targetExt}}
	}
	return DiagnosticDetails{Message: diagnostics.To_convert_this_file_to_an_ECMAScript_module_create_a_local_package_json_file_with_type_Colon_module, Args: nil}
}
func walkUpOuterExpressions(node ast.Handle) ast.Handle {
	parent := node.Parent()
	for !parent.IsNil() && ast.IsOuterExpression(parent, ast.OEKAll) {
		parent = parent.Parent()
	}
	return parent
}
func GetSetAccessorValueParameter(accessor ast.Handle) ast.Handle {
	parameters := accessor.Parameters()
	if len(parameters) > 0 {
		hasThis := len(parameters) == 2 && ast.IsThisParameter(parameters[0])
		return parameters[core.IfElse(hasThis, 1, 0)]
	}
	return ast.Handle{}
}
