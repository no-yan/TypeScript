package printer

import (
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"strconv"
	"strings"
)

type NodeFactory struct {
	*ast.Factory
	emitContext *EmitContext
}

func NewNodeFactory(context *EmitContext) *NodeFactory {
	return &NodeFactory{Factory: ast.NewFactory(context.factoryHooks()), emitContext: context}
}

func (f *NodeFactory) ReleaseArenas() {}

func (f *NodeFactory) NewList(nodes []ast.Handle) ast.ListRef {
	loc := core.UndefinedTextRange()
	if f.emitContext != nil {
		return f.emitContext.StoreFactory().List(loc, nodes...)
	}
	return ast.NewFactory(ast.FactoryHooks{}).List(loc, nodes...)
}
func (f *NodeFactory) newGeneratedIdentifier(kind GeneratedIdentifierFlags, text string, node ast.Handle, options AutoGenerateOptions) ast.Handle {
	id := AutoGenerateId(nextAutoGenerateId.Add(1))
	if len(text) == 0 {
		switch {
		case node.IsNil():
			text = fmt.Sprintf("(auto@%d)", id)
		case ast.IsMemberName(node):
			text = node.Text()
		default:
			text = fmt.Sprintf("(generated@%v)", f.emitContext.NodeIdentity(f.emitContext.getNodeForGeneratedNameWorker(node, id)))
		}
		text = FormatGeneratedName(false, options.Prefix, text, options.Suffix)
	}
	name := f.NewIdentifier(text)
	autoGenerate := &AutoGenerateInfo{Id: id, Flags: kind | (options.Flags & ^GeneratedIdentifierFlagsKindMask), Prefix: options.Prefix, Suffix: options.Suffix, Node: node}
	if f.emitContext.autoGenerate == nil {
		f.emitContext.autoGenerate = make(map[ast.GlobalRef]*AutoGenerateInfo)
	}
	f.emitContext.autoGenerate[f.emitContext.NodeIdentity(name)] = autoGenerate
	return name
}

func (f *NodeFactory) NewTempVariable() ast.Handle {
	return f.NewTempVariableEx(AutoGenerateOptions{})
}

func (f *NodeFactory) NewTempVariableEx(options AutoGenerateOptions) ast.Handle {
	return f.newGeneratedIdentifier(GeneratedIdentifierFlagsAuto, "", ast.Handle{}, options)
}

func (f *NodeFactory) NewLoopVariable() ast.Handle {
	return f.NewLoopVariableEx(AutoGenerateOptions{})
}

func (f *NodeFactory) NewLoopVariableEx(options AutoGenerateOptions) ast.Handle {
	return f.newGeneratedIdentifier(GeneratedIdentifierFlagsLoop, "", ast.Handle{}, options)
}

func (f *NodeFactory) NewUniqueName(text string) ast.Handle {
	return f.NewUniqueNameEx(text, AutoGenerateOptions{})
}

func (f *NodeFactory) NewUniqueNameEx(text string, options AutoGenerateOptions) ast.Handle {
	return f.newGeneratedIdentifier(GeneratedIdentifierFlagsUnique, text, ast.Handle{}, options)
}

func (f *NodeFactory) NewGeneratedNameForNode(node ast.Handle) ast.Handle {
	return f.NewGeneratedNameForNodeEx(node, AutoGenerateOptions{})
}

func (f *NodeFactory) NewGeneratedNameForNodeEx(node ast.Handle, options AutoGenerateOptions) ast.Handle {
	if len(options.Prefix) > 0 || len(options.Suffix) > 0 {
		options.Flags |= GeneratedIdentifierFlagsOptimistic
	}
	return f.newGeneratedIdentifier(GeneratedIdentifierFlagsNode, "", node, options)
}
func (f *NodeFactory) newGeneratedPrivateIdentifier(kind GeneratedIdentifierFlags, text string, node ast.Handle, options AutoGenerateOptions) ast.Handle {
	id := AutoGenerateId(nextAutoGenerateId.Add(1))
	if len(text) == 0 {
		switch {
		case node.IsNil():
			text = fmt.Sprintf("(auto@%d)", id)
		case ast.IsMemberName(node):
			text = node.Text()
		default:
			text = fmt.Sprintf("(generated@%v)", f.emitContext.NodeIdentity(f.emitContext.getNodeForGeneratedNameWorker(node, id)))
		}
		text = FormatGeneratedName(true, options.Prefix, text, options.Suffix)
	} else if !strings.HasPrefix(text, "#") {
		panic("First character of private identifier must be #: " + text)
	}
	name := f.NewPrivateIdentifier(text)
	autoGenerate := &AutoGenerateInfo{Id: id, Flags: kind | (options.Flags &^ GeneratedIdentifierFlagsKindMask), Prefix: options.Prefix, Suffix: options.Suffix, Node: node}
	if f.emitContext.autoGenerate == nil {
		f.emitContext.autoGenerate = make(map[ast.GlobalRef]*AutoGenerateInfo)
	}
	f.emitContext.autoGenerate[f.emitContext.NodeIdentity(name)] = autoGenerate
	return name
}

func (f *NodeFactory) NewUniquePrivateName(text string) ast.Handle {
	return f.NewUniquePrivateNameEx(text, AutoGenerateOptions{})
}

func (f *NodeFactory) NewUniquePrivateNameEx(text string, options AutoGenerateOptions) ast.Handle {
	return f.newGeneratedPrivateIdentifier(GeneratedIdentifierFlagsUnique, text, ast.Handle{}, options)
}

func (f *NodeFactory) NewGeneratedPrivateNameForNode(node ast.Handle) ast.Handle {
	return f.NewGeneratedPrivateNameForNodeEx(node, AutoGenerateOptions{})
}

func (f *NodeFactory) NewGeneratedPrivateNameForNodeEx(node ast.Handle, options AutoGenerateOptions) ast.Handle {
	if len(options.Prefix) > 0 || len(options.Suffix) > 0 {
		options.Flags |= GeneratedIdentifierFlagsOptimistic
	}
	return f.newGeneratedPrivateIdentifier(GeneratedIdentifierFlagsNode, "", node, options)
}

func (f *NodeFactory) NewStringLiteralFromNode(textSourceNode ast.Handle) ast.Handle {
	var text string
	switch textSourceNode.Kind() {
	case ast.KindIdentifier, ast.KindPrivateIdentifier, ast.KindJsxNamespacedName, ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateHead, ast.KindTemplateMiddle, ast.KindTemplateTail, ast.KindRegularExpressionLiteral:
		text = textSourceNode.Text()
	}
	node := f.NewStringLiteral(text, ast.TokenFlagsNone)
	f.emitContext.SetTextSource(node, textSourceNode)
	return node
}
func (f *NodeFactory) NewThisExpression() ast.Handle {
	return f.NewKeywordExpression(ast.KindThisKeyword)
}
func (f *NodeFactory) NewTrueExpression() ast.Handle {
	return f.NewKeywordExpression(ast.KindTrueKeyword)
}
func (f *NodeFactory) NewFalseExpression() ast.Handle {
	return f.NewKeywordExpression(ast.KindFalseKeyword)
}
func (f *NodeFactory) NewCommaExpression(left ast.Handle, right ast.Handle) ast.Handle {
	return f.NewBinaryExpression(0, left, ast.Handle{}, f.NewToken(ast.KindCommaToken), right)
}
func (f *NodeFactory) NewAssignmentExpression(left ast.Handle, right ast.Handle) ast.Handle {
	return f.NewBinaryExpression(0, left, ast.Handle{}, f.NewToken(ast.KindEqualsToken), right)
}
func (f *NodeFactory) NewLogicalORExpression(left ast.Handle, right ast.Handle) ast.Handle {
	return f.NewBinaryExpression(0, left, ast.Handle{}, f.NewToken(ast.KindBarBarToken), right)
}
func (f *NodeFactory) NewLogicalANDExpression(left ast.Handle, right ast.Handle) ast.Handle {
	return f.NewBinaryExpression(0, left, ast.Handle{}, f.NewToken(ast.KindAmpersandAmpersandToken), right)
}

func (f *NodeFactory) NewStrictEqualityExpression(left ast.Handle, right ast.Handle) ast.Handle {
	return f.NewBinaryExpression(0, left, ast.Handle{}, f.NewToken(ast.KindEqualsEqualsEqualsToken), right)
}
func (f *NodeFactory) NewStrictInequalityExpression(left ast.Handle, right ast.Handle) ast.Handle {
	return f.NewBinaryExpression(0, left, ast.Handle{}, f.NewToken(ast.KindExclamationEqualsEqualsToken), right)
}
func (f *NodeFactory) NewVoidZeroExpression() ast.Handle {
	return f.NewVoidExpression(f.NewNumericLiteral("0", ast.TokenFlagsNone))
}
func flattenCommaElement(node ast.Handle, expressions []ast.Handle) []ast.Handle {
	if ast.IsBinaryExpression(node) && ast.NodeIsSynthesized(node) && node.BinaryExpressionOperatorToken().Kind() == ast.KindCommaToken {
		expressions = flattenCommaElement(node.BinaryExpressionLeft(), expressions)
		expressions = flattenCommaElement(node.BinaryExpressionRight(), expressions)
	} else {
		expressions = append(expressions, node)
	}
	return expressions
}
func flattenCommaElements(expressions []ast.Handle) []ast.Handle {
	var result []ast.Handle
	for _, expression := range expressions {
		result = flattenCommaElement(expression, result)
	}
	return result
}

func (f *NodeFactory) InlineExpressions(expressions []ast.Handle) ast.Handle {
	if len(expressions) == 0 {
		return ast.Handle{}
	}
	if len(expressions) == 1 {
		return expressions[0]
	}
	expressions = flattenCommaElements(expressions)
	expression := expressions[0]
	for _, next := range expressions[1:] {
		expression = f.NewCommaExpression(expression, next)
	}
	return expression
}
func (f *NodeFactory) CreateExpressionFromEntityName(node ast.Handle) ast.Handle {
	if ast.IsQualifiedName(node) {
		left := f.CreateExpressionFromEntityName(node.QualifiedNameLeft())
		right := f.DeepCloneNode(node.QualifiedNameRight())
		right.SetLoc(node.QualifiedNameRight().Loc())
		right.SetParent(node.QualifiedNameRight().Parent())
		propAccess := f.NewPropertyAccessExpression(left, ast.Handle{}, right, ast.NodeFlagsNone)
		propAccess.SetLoc(node.Loc())
		return propAccess
	}
	res := f.DeepCloneNode(node)
	res.SetLoc(node.Loc())
	res.SetParent(node.Parent())
	return res
}
func (f *NodeFactory) RestoreEnclosingLabel(node ast.Handle, outermostLabeledStatement ast.Handle) ast.Handle {
	if outermostLabeledStatement.IsNil() {
		return node
	}
	innerLabel := node
	if ast.IsLabeledStatement(outermostLabeledStatement.Statement()) {
		innerLabel = f.RestoreEnclosingLabel(node, outermostLabeledStatement.Statement())
	}
	return f.UpdateLabeledStatement(outermostLabeledStatement, outermostLabeledStatement.Label(), innerLabel)
}

func (f *NodeFactory) CreateForOfBindingStatement(node ast.Handle, boundValue ast.Handle) ast.Handle {
	if ast.IsVariableDeclarationList(node) {
		firstDeclaration := node.Store().ListSlice(node.VariableDeclarationListDeclarations())[0]
		updatedDeclaration := f.UpdateVariableDeclaration(firstDeclaration, firstDeclaration.Name(), ast.Handle{}, ast.Handle{}, boundValue)
		statement := f.NewVariableStatement(0, f.UpdateVariableDeclarationList(node, f.NewList([]ast.Handle{updatedDeclaration}), node.Flags()))
		statement.SetLoc(node.Loc())
		return statement
	}
	updatedExpression := f.NewAssignmentExpression(node, boundValue)
	updatedExpression.SetLoc(node.Loc())
	statement := f.NewExpressionStatement(updatedExpression)
	statement.SetLoc(node.Loc())
	return statement
}
func (f *NodeFactory) NewTypeCheck(value ast.Handle, tag string) ast.Handle {
	if tag == "null" {
		return f.NewStrictEqualityExpression(value, f.NewKeywordExpression(ast.KindNullKeyword))
	} else if tag == "undefined" {
		return f.NewStrictEqualityExpression(value, f.NewVoidZeroExpression())
	} else {
		return f.NewStrictEqualityExpression(f.NewTypeOfExpression(value), f.NewStringLiteral(tag, ast.TokenFlagsNone))
	}
}
func (f *NodeFactory) NewMethodCall(object ast.Handle, methodName ast.Handle, argumentsList []ast.Handle) ast.Handle {
	if ast.IsCallExpression(object) && (object.Flags()&ast.NodeFlagsOptionalChain != 0) {
		return f.NewCallExpression(f.NewPropertyAccessExpression(object, ast.Handle{}, methodName, ast.NodeFlagsNone), ast.Handle{}, 0, f.NewList(argumentsList), ast.NodeFlagsOptionalChain)
	}
	return f.NewCallExpression(f.NewPropertyAccessExpression(object, ast.Handle{}, methodName, ast.NodeFlagsNone), ast.Handle{}, 0, f.NewList(argumentsList), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewGlobalMethodCall(globalObjectName string, methodName string, argumentsList []ast.Handle) ast.Handle {
	return f.NewMethodCall(f.NewIdentifier(globalObjectName), f.NewIdentifier(methodName), argumentsList)
}
func (f *NodeFactory) NewFunctionCallCall(target ast.Handle, thisArg ast.Handle, argumentsList []ast.Handle) ast.Handle {
	if thisArg.IsNil() {
		panic("Attempted to construct function call call without this argument expression")
	}
	args := append([]ast.Handle{thisArg}, argumentsList...)
	return f.NewMethodCall(target, f.NewIdentifier("call"), args)
}
func (f *NodeFactory) NewArraySliceCall(array ast.Handle, start int) ast.Handle {
	var args []ast.Handle
	if start != 0 {
		args = append(args, f.NewNumericLiteral(strconv.Itoa(start), ast.TokenFlagsNone))
	}
	return f.NewMethodCall(array, f.NewIdentifier("slice"), args)
}

func (f *NodeFactory) isIgnorableParen(node ast.Handle) bool {
	return ast.IsParenthesizedExpression(node) && ast.NodeIsSynthesized(node) && ast.RangeIsSynthesized(f.emitContext.SourceMapRange(node)) && ast.RangeIsSynthesized(f.emitContext.CommentRange(node))
}
func (f *NodeFactory) updateOuterExpression(outerExpression ast.Handle, expression ast.Handle) ast.Handle {
	switch outerExpression.Kind() {
	case ast.KindParenthesizedExpression:
		return f.UpdateParenthesizedExpression(outerExpression, expression)
	case ast.KindTypeAssertionExpression:
		return f.UpdateTypeAssertion(outerExpression, outerExpression.Type(), expression)
	case ast.KindAsExpression:
		return f.UpdateAsExpression(outerExpression, expression, outerExpression.Type())
	case ast.KindSatisfiesExpression:
		return f.UpdateSatisfiesExpression(outerExpression, expression, outerExpression.Type())
	case ast.KindNonNullExpression:
		return f.UpdateNonNullExpression(outerExpression, expression, outerExpression.Flags())
	case ast.KindExpressionWithTypeArguments:
		return f.UpdateExpressionWithTypeArguments(outerExpression, expression, outerExpression.TypeArgumentList())
	case ast.KindPartiallyEmittedExpression:
		return f.UpdatePartiallyEmittedExpression(outerExpression, expression)
	default:
		panic(fmt.Sprintf("Unexpected outer expression kind: %s", outerExpression.Kind()))
	}
}
func (f *NodeFactory) RestoreOuterExpressions(outerExpression ast.Handle, innerExpression ast.Handle, kinds ast.OuterExpressionKinds) ast.Handle {
	if !outerExpression.IsNil() && ast.IsOuterExpression(outerExpression, kinds) && !f.isIgnorableParen(outerExpression) {
		return f.updateOuterExpression(outerExpression, f.RestoreOuterExpressions(outerExpression.Expression(), innerExpression, ast.OEKAll))
	}
	return innerExpression
}

func (f *NodeFactory) EnsureUseStrict(statements []ast.Handle) []ast.Handle {
	for _, statement := range statements {
		if ast.IsPrologueDirective(statement) && statement.Expression().Text() == "use strict" {
			return statements
		} else {
			break
		}
	}
	useStrictPrologue := f.NewExpressionStatement(f.NewStringLiteral("use strict", ast.TokenFlagsNone))
	statements = append([]ast.Handle{useStrictPrologue}, statements...)
	return statements
}

func (f *NodeFactory) SplitStandardPrologue(source []ast.Handle) (prologue []ast.Handle, rest []ast.Handle) {
	for i, statement := range source {
		if !ast.IsPrologueDirective(statement) {
			return source[:i], source[i:]
		}
	}
	return source, nil
}

func (f *NodeFactory) SplitCustomPrologue(source []ast.Handle) (prologue []ast.Handle, rest []ast.Handle) {
	for i, statement := range source {
		if ast.IsPrologueDirective(statement) || f.emitContext.EmitFlags(statement)&EFCustomPrologue == 0 {
			return source[:i], source[i:]
		}
	}
	return nil, source
}

type NameOptions struct {
	AllowComments   bool
	AllowSourceMaps bool
}
type AssignedNameOptions struct {
	AllowComments      bool
	AllowSourceMaps    bool
	IgnoreAssignedName bool
}

func (f *NodeFactory) getName(node ast.Handle, emitFlags EmitFlags, opts AssignedNameOptions) ast.Handle {
	var nodeName ast.Handle
	if !node.IsNil() {
		if opts.IgnoreAssignedName {
			nodeName = ast.GetNonAssignedNameOfDeclaration(node)
		} else {
			nodeName = ast.GetNameOfDeclaration(node)
		}
	}
	if !nodeName.IsNil() {
		name := f.DeepCloneNode(nodeName)
		if !opts.AllowComments {
			emitFlags |= EFNoComments
		}
		if !opts.AllowSourceMaps {
			emitFlags |= EFNoSourceMap
		}
		f.emitContext.AddEmitFlags(name, emitFlags)
		return name
	}
	return f.NewGeneratedNameForNode(node)
}

func (f *NodeFactory) GetLocalName(node ast.Handle) ast.Handle {
	return f.GetLocalNameEx(node, AssignedNameOptions{})
}

func (f *NodeFactory) GetLocalNameEx(node ast.Handle, opts AssignedNameOptions) ast.Handle {
	return f.getName(node, EFLocalName, opts)
}

func (f *NodeFactory) GetExportName(node ast.Handle) ast.Handle {
	return f.GetExportNameEx(node, AssignedNameOptions{})
}

func (f *NodeFactory) GetExportNameEx(node ast.Handle, opts AssignedNameOptions) ast.Handle {
	return f.getName(node, EFExportName, opts)
}

func (f *NodeFactory) GetDeclarationName(node ast.Handle) ast.Handle {
	return f.GetDeclarationNameEx(node, NameOptions{})
}

func (f *NodeFactory) GetDeclarationNameEx(node ast.Handle, opts NameOptions) ast.Handle {
	return f.getName(node, EFNone, AssignedNameOptions{AllowComments: opts.AllowComments, AllowSourceMaps: opts.AllowSourceMaps})
}
func (f *NodeFactory) GetNamespaceMemberName(ns ast.Handle, name ast.Handle, opts NameOptions) ast.Handle {
	if !f.emitContext.HasAutoGenerateInfo(name) {
		name = f.DeepCloneNode(name)
	}
	qualifiedName := f.NewPropertyAccessExpression(ns, ast.Handle{}, name, ast.NodeFlagsNone)
	f.emitContext.AssignCommentAndSourceMapRanges(qualifiedName, name)
	if !opts.AllowComments {
		f.emitContext.AddEmitFlags(qualifiedName, EFNoComments)
	}
	if !opts.AllowSourceMaps {
		f.emitContext.AddEmitFlags(qualifiedName, EFNoSourceMap)
	}
	return qualifiedName
}

func (f *NodeFactory) GetExternalModuleOrNamespaceExportName(ns ast.Handle, node ast.Handle, allowComments bool, allowSourceMaps bool) ast.Handle {
	if !ns.IsNil() && ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
		nameOpts := NameOptions{AllowComments: allowComments, AllowSourceMaps: allowSourceMaps}
		return f.GetNamespaceMemberName(ns, f.GetDeclarationNameEx(node, nameOpts), nameOpts)
	}
	return f.GetExportNameEx(node, AssignedNameOptions{AllowComments: allowComments, AllowSourceMaps: allowSourceMaps})
}

func (f *NodeFactory) NewUnscopedHelperName(name string) ast.Handle {
	node := f.NewIdentifier(name)
	f.emitContext.SetEmitFlags(node, EFHelperName)
	return node
}
func (f *NodeFactory) NewDecorateHelper(decoratorExpressions []ast.Handle, target ast.Handle, memberName ast.Handle, descriptor ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(decorateHelper)
	var argumentsArray []ast.Handle
	argumentsArray = append(argumentsArray, f.NewArrayLiteralExpression(f.NewList(decoratorExpressions), true))
	argumentsArray = append(argumentsArray, target)
	if !memberName.IsNil() {
		argumentsArray = append(argumentsArray, memberName)
		if !descriptor.IsNil() {
			argumentsArray = append(argumentsArray, descriptor)
		}
	}
	return f.NewCallExpression(f.NewUnscopedHelperName("__decorate"), ast.Handle{}, 0, f.NewList(argumentsArray), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewMetadataHelper(metadataKey string, metadataValue ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(metadataHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__metadata"), ast.Handle{}, 0, f.NewList([]ast.Handle{f.NewStringLiteral(metadataKey, ast.TokenFlagsNone), metadataValue}), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewParamHelper(expression ast.Handle, parameterOffset int, location core.TextRange) ast.Handle {
	f.emitContext.RequestEmitHelper(paramHelper)
	helper := f.NewCallExpression(f.NewUnscopedHelperName("__param"), ast.Handle{}, 0, f.NewList([]ast.Handle{f.NewNumericLiteral(strconv.Itoa(parameterOffset), ast.TokenFlagsNone), expression}), ast.NodeFlagsNone)
	helper.SetLoc(location)
	return helper
}
func (f *NodeFactory) NewAddDisposableResourceHelper(envBinding ast.Handle, value ast.Handle, async bool) ast.Handle {
	f.emitContext.RequestEmitHelper(addDisposableResourceHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__addDisposableResource"), ast.Handle{}, 0, f.NewList([]ast.Handle{envBinding, value, f.NewKeywordExpression(core.IfElse(async, ast.KindTrueKeyword, ast.KindFalseKeyword))}), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewDisposeResourcesHelper(envBinding ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(disposeResourcesHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__disposeResources"), ast.Handle{}, 0, f.NewList([]ast.Handle{envBinding}), ast.NodeFlagsNone)
}

type PrivateIdentifierKind string

const (
	PrivateIdentifierKindField         PrivateIdentifierKind = "f"
	PrivateIdentifierKindMethod        PrivateIdentifierKind = "m"
	PrivateIdentifierKindAccessor      PrivateIdentifierKind = "a"
	PrivateIdentifierKindUntransformed PrivateIdentifierKind = "untransformed"
)

func (f *NodeFactory) NewClassPrivateFieldGetHelper(receiver ast.Handle, state ast.Handle, kind PrivateIdentifierKind, fn ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(classPrivateFieldGetHelper)
	var args []ast.Handle
	if fn.IsNil() {
		args = []ast.Handle{receiver, state, f.NewStringLiteral(string(kind), ast.TokenFlagsNone)}
	} else {
		args = []ast.Handle{receiver, state, f.NewStringLiteral(string(kind), ast.TokenFlagsNone), fn}
	}
	return f.NewCallExpression(f.NewUnscopedHelperName("__classPrivateFieldGet"), ast.Handle{}, 0, f.NewList(args), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewClassPrivateFieldSetHelper(receiver ast.Handle, state ast.Handle, value ast.Handle, kind PrivateIdentifierKind, fn ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(classPrivateFieldSetHelper)
	var args []ast.Handle
	if fn.IsNil() {
		args = []ast.Handle{receiver, state, value, f.NewStringLiteral(string(kind), ast.TokenFlagsNone)}
	} else {
		args = []ast.Handle{receiver, state, value, f.NewStringLiteral(string(kind), ast.TokenFlagsNone), fn}
	}
	return f.NewCallExpression(f.NewUnscopedHelperName("__classPrivateFieldSet"), ast.Handle{}, 0, f.NewList(args), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewClassPrivateFieldInHelper(state ast.Handle, receiver ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(classPrivateFieldInHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__classPrivateFieldIn"), ast.Handle{}, 0, f.NewList([]ast.Handle{state, receiver}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewObjectDefinePropertyCall(target ast.Handle, name ast.Handle, descriptor ast.Handle) ast.Handle {
	return f.NewCallExpression(f.NewPropertyAccessExpression(f.NewIdentifier("Object"), ast.Handle{}, f.NewIdentifier("defineProperty"), ast.NodeFlagsNone), ast.Handle{}, 0, f.NewList([]ast.Handle{target, name, descriptor}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewReflectGetCall(target ast.Handle, propertyKey ast.Handle, receiver ast.Handle) ast.Handle {
	return f.NewCallExpression(f.NewPropertyAccessExpression(f.NewIdentifier("Reflect"), ast.Handle{}, f.NewIdentifier("get"), ast.NodeFlagsNone), ast.Handle{}, 0, f.NewList([]ast.Handle{target, propertyKey, receiver}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewReflectSetCall(target ast.Handle, propertyKey ast.Handle, value ast.Handle, receiver ast.Handle) ast.Handle {
	return f.NewCallExpression(f.NewPropertyAccessExpression(f.NewIdentifier("Reflect"), ast.Handle{}, f.NewIdentifier("set"), ast.NodeFlagsNone), ast.Handle{}, 0, f.NewList([]ast.Handle{target, propertyKey, value, receiver}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewFunctionBindCall(target ast.Handle, thisArg ast.Handle, argumentsList []ast.Handle) ast.Handle {
	args := make([]ast.Handle, 0, 1+len(argumentsList))
	args = append(args, thisArg)
	args = append(args, argumentsList...)
	return f.NewMethodCall(target, f.NewIdentifier("bind"), args)
}

func (f *NodeFactory) NewImmediatelyInvokedArrowFunction(statements []ast.Handle) ast.Handle {
	arrow := f.NewArrowFunction(0, 0, f.NewList([]ast.Handle{}), ast.Handle{}, ast.Handle{}, f.NewToken(ast.KindEqualsGreaterThanToken), f.NewBlock(f.NewList(statements), true))
	return f.NewCallExpression(f.NewParenthesizedExpression(arrow), ast.Handle{}, 0, f.NewList([]ast.Handle{}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewExportDefault(expression ast.Handle) ast.Handle {
	return f.NewExportAssignment(0, false, ast.Handle{}, expression)
}

func (f *NodeFactory) NewExternalModuleExport(name ast.Handle) ast.Handle {
	specifier := f.NewExportSpecifier(false, ast.Handle{}, name)
	namedExports := f.NewNamedExports(f.NewList([]ast.Handle{specifier}))
	return f.NewExportDeclaration(0, false, namedExports, ast.Handle{}, ast.Handle{})
}

func (f *NodeFactory) NewAssignHelper(attributesSegments []ast.Handle, scriptTarget core.ScriptTarget) ast.Handle {
	return f.NewCallExpression(f.NewPropertyAccessExpression(f.NewIdentifier("Object"), ast.Handle{}, f.NewIdentifier("assign"), ast.NodeFlagsNone), ast.Handle{}, 0, f.NewList(attributesSegments), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewRestHelper(value ast.Handle, elements []ast.Handle, computedTempVariables []ast.Handle, location core.TextRange) ast.Handle {
	f.emitContext.RequestEmitHelper(restHelper)
	var propertyNames []ast.Handle
	computedTempVariableOffset := 0
	for i, element := range elements {
		if i == len(elements)-1 {
			break
		}
		propertyName := ast.TryGetPropertyNameOfBindingOrAssignmentElement(element)
		if !propertyName.IsNil() {
			if ast.IsComputedPropertyName(propertyName) {
				debug.Assert(computedTempVariables != nil, "Encountered computed property name but 'computedTempVariables' argument was not provided.")
				temp := computedTempVariables[computedTempVariableOffset]
				computedTempVariableOffset++
				propertyNames = append(propertyNames, f.NewConditionalExpression(f.NewTypeCheck(temp, "symbol"), f.NewToken(ast.KindQuestionToken), temp, f.NewToken(ast.KindColonToken), f.NewBinaryExpression(0, temp, ast.Handle{}, f.NewToken(ast.KindPlusToken), f.NewStringLiteral("", ast.TokenFlagsNone))))
			} else {
				propertyNames = append(propertyNames, f.NewStringLiteralFromNode(propertyName))
			}
		}
	}
	propNames := f.NewArrayLiteralExpression(f.NewList(propertyNames), false)
	propNames.SetLoc(location)
	return f.NewCallExpression(f.NewUnscopedHelperName("__rest"), ast.Handle{}, 0, f.NewList([]ast.Handle{value, propNames}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewAwaitHelper(expression ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(awaitHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__await"), ast.Handle{}, 0, f.NewList([]ast.Handle{expression}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewAsyncGeneratorHelper(generatorFunc ast.Handle, hasLexicalThis bool) ast.Handle {
	f.emitContext.RequestEmitHelper(awaitHelper)
	f.emitContext.RequestEmitHelper(asyncGeneratorHelper)
	f.emitContext.AddEmitFlags(generatorFunc, EFAsyncFunctionBody|EFReuseTempVariableScope)
	var thisArg ast.Handle
	if hasLexicalThis {
		thisArg = f.NewKeywordExpression(ast.KindThisKeyword)
	} else {
		thisArg = f.NewVoidZeroExpression()
	}
	return f.NewCallExpression(f.NewUnscopedHelperName("__asyncGenerator"), ast.Handle{}, 0, f.NewList([]ast.Handle{thisArg, f.NewIdentifier("arguments"), generatorFunc}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewAsyncDelegatorHelper(expression ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(awaitHelper)
	f.emitContext.RequestEmitHelper(asyncDelegatorHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__asyncDelegator"), ast.Handle{}, 0, f.NewList([]ast.Handle{expression}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewAsyncValuesHelper(expression ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(asyncValuesHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__asyncValues"), ast.Handle{}, 0, f.NewList([]ast.Handle{expression}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewAwaiterHelper(hasLexicalThis bool, argumentsExpression ast.Handle, parameters ast.ListRef, body ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(awaiterHelper)
	var params ast.ListRef
	if parameters != 0 {
		params = parameters
	} else {
		params = f.NewList([]ast.Handle{})
	}
	generatorFunc := f.NewFunctionExpression(0, f.NewToken(ast.KindAsteriskToken), ast.Handle{}, 0, params, ast.Handle{}, ast.Handle{}, body)
	f.emitContext.AddEmitFlags(generatorFunc, EFAsyncFunctionBody|EFReuseTempVariableScope)
	var thisArg ast.Handle
	if hasLexicalThis {
		thisArg = f.NewKeywordExpression(ast.KindThisKeyword)
	} else {
		thisArg = f.NewVoidZeroExpression()
	}
	var argsArg ast.Handle
	if !argumentsExpression.IsNil() {
		argsArg = argumentsExpression
	} else {
		argsArg = f.NewVoidZeroExpression()
	}
	return f.NewCallExpression(f.NewUnscopedHelperName("__awaiter"), ast.Handle{}, 0, f.NewList([]ast.Handle{thisArg, argsArg, f.NewVoidZeroExpression(), generatorFunc}), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewESDecorateClassContextObject(nameExpr ast.Handle, metadata ast.Handle) ast.Handle {
	props := []ast.Handle{f.NewPropertyAssignment(0, f.NewIdentifier("kind"), ast.Handle{}, ast.Handle{}, f.NewStringLiteral("class", 0)), f.NewPropertyAssignment(0, f.NewIdentifier("name"), ast.Handle{}, ast.Handle{}, nameExpr), f.NewPropertyAssignment(0, f.NewIdentifier("metadata"), ast.Handle{}, ast.Handle{}, metadata)}
	return f.NewObjectLiteralExpression(f.NewList(props), false)
}
func (f *NodeFactory) NewESDecorateClassElementAccessGetMethod(nameComputed bool, nameExpr ast.Handle) ast.Handle {
	var accessor ast.Handle
	if nameComputed {
		accessor = f.NewElementAccessExpression(f.NewIdentifier("obj"), ast.Handle{}, nameExpr, ast.NodeFlagsNone)
	} else {
		accessor = f.NewPropertyAccessExpression(f.NewIdentifier("obj"), ast.Handle{}, nameExpr, ast.NodeFlagsNone)
	}
	objParam := f.NewParameterDeclaration(0, ast.Handle{}, f.NewIdentifier("obj"), ast.Handle{}, ast.Handle{}, ast.Handle{})
	arrow := f.NewArrowFunction(0, 0, f.NewList([]ast.Handle{objParam}), ast.Handle{}, ast.Handle{}, f.NewToken(ast.KindEqualsGreaterThanToken), accessor)
	return f.NewPropertyAssignment(0, f.NewIdentifier("get"), ast.Handle{}, ast.Handle{}, arrow)
}
func (f *NodeFactory) NewESDecorateClassElementAccessSetMethod(nameComputed bool, nameExpr ast.Handle) ast.Handle {
	var accessor ast.Handle
	if nameComputed {
		accessor = f.NewElementAccessExpression(f.NewIdentifier("obj"), ast.Handle{}, nameExpr, ast.NodeFlagsNone)
	} else {
		accessor = f.NewPropertyAccessExpression(f.NewIdentifier("obj"), ast.Handle{}, nameExpr, ast.NodeFlagsNone)
	}
	assignment := f.NewAssignmentExpression(accessor, f.NewIdentifier("value"))
	stmt := f.NewExpressionStatement(assignment)
	body := f.NewBlock(f.NewList([]ast.Handle{stmt}), false)
	objParam := f.NewParameterDeclaration(0, ast.Handle{}, f.NewIdentifier("obj"), ast.Handle{}, ast.Handle{}, ast.Handle{})
	valueParam := f.NewParameterDeclaration(0, ast.Handle{}, f.NewIdentifier("value"), ast.Handle{}, ast.Handle{}, ast.Handle{})
	arrow := f.NewArrowFunction(0, 0, f.NewList([]ast.Handle{objParam, valueParam}), ast.Handle{}, ast.Handle{}, f.NewToken(ast.KindEqualsGreaterThanToken), body)
	return f.NewPropertyAssignment(0, f.NewIdentifier("set"), ast.Handle{}, ast.Handle{}, arrow)
}
func (f *NodeFactory) NewESDecorateClassElementAccessHasMethod(nameComputed bool, nameExpr ast.Handle) ast.Handle {
	var propertyName ast.Handle
	if !nameComputed && !nameExpr.IsNil() && ast.IsIdentifier(nameExpr) {
		propertyName = f.NewStringLiteralFromNode(nameExpr)
	} else {
		propertyName = nameExpr
	}
	objParam := f.NewParameterDeclaration(0, ast.Handle{}, f.NewIdentifier("obj"), ast.Handle{}, ast.Handle{}, ast.Handle{})
	inExpr := f.NewBinaryExpression(0, propertyName, ast.Handle{}, f.NewToken(ast.KindInKeyword), f.NewIdentifier("obj"))
	arrow := f.NewArrowFunction(0, 0, f.NewList([]ast.Handle{objParam}), ast.Handle{}, ast.Handle{}, f.NewToken(ast.KindEqualsGreaterThanToken), inExpr)
	return f.NewPropertyAssignment(0, f.NewIdentifier("has"), ast.Handle{}, ast.Handle{}, arrow)
}

func (f *NodeFactory) NewESDecorateClassElementAccessObject(nameComputed bool, nameExpr ast.Handle, hasGet bool, hasSet bool) ast.Handle {
	accessProps := []ast.Handle{}
	accessProps = append(accessProps, f.NewESDecorateClassElementAccessHasMethod(nameComputed, nameExpr))
	if hasGet {
		accessProps = append(accessProps, f.NewESDecorateClassElementAccessGetMethod(nameComputed, nameExpr))
	}
	if hasSet {
		accessProps = append(accessProps, f.NewESDecorateClassElementAccessSetMethod(nameComputed, nameExpr))
	}
	return f.NewObjectLiteralExpression(f.NewList(accessProps), false)
}
func (f *NodeFactory) NewESDecorateClassElementContextObject(kind string, nameComputed bool, nameExpr ast.Handle, isStatic bool, isPrivate bool, hasGet bool, hasSet bool, metadata ast.Handle) ast.Handle {
	var nameValue ast.Handle
	if !nameComputed && !nameExpr.IsNil() && (ast.IsPrivateIdentifier(nameExpr) || ast.IsIdentifier(nameExpr)) {
		nameValue = f.NewStringLiteralFromNode(nameExpr)
	} else {
		nameValue = nameExpr
	}
	accessObj := f.NewESDecorateClassElementAccessObject(nameComputed, nameExpr, hasGet, hasSet)
	var staticExpr ast.Handle
	if isStatic {
		staticExpr = f.NewTrueExpression()
	} else {
		staticExpr = f.NewFalseExpression()
	}
	var privateExpr ast.Handle
	if isPrivate {
		privateExpr = f.NewTrueExpression()
	} else {
		privateExpr = f.NewFalseExpression()
	}
	props := []ast.Handle{f.NewPropertyAssignment(0, f.NewIdentifier("kind"), ast.Handle{}, ast.Handle{}, f.NewStringLiteral(kind, 0)), f.NewPropertyAssignment(0, f.NewIdentifier("name"), ast.Handle{}, ast.Handle{}, nameValue), f.NewPropertyAssignment(0, f.NewIdentifier("static"), ast.Handle{}, ast.Handle{}, staticExpr), f.NewPropertyAssignment(0, f.NewIdentifier("private"), ast.Handle{}, ast.Handle{}, privateExpr), f.NewPropertyAssignment(0, f.NewIdentifier("access"), ast.Handle{}, ast.Handle{}, accessObj), f.NewPropertyAssignment(0, f.NewIdentifier("metadata"), ast.Handle{}, ast.Handle{}, metadata)}
	return f.NewObjectLiteralExpression(f.NewList(props), false)
}
func (f *NodeFactory) NewESDecorateHelper(ctor ast.Handle, descriptorIn ast.Handle, decorators ast.Handle, contextIn ast.Handle, initializers ast.Handle, extraInitializers ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(esDecorateHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__esDecorate"), ast.Handle{}, 0, f.NewList([]ast.Handle{ctor, descriptorIn, decorators, contextIn, initializers, extraInitializers}), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewRunInitializersHelper(thisArg ast.Handle, initializers ast.Handle, value ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(runInitializersHelper)
	var arguments []ast.Handle
	if !value.IsNil() {
		arguments = []ast.Handle{thisArg, initializers, value}
	} else {
		arguments = []ast.Handle{thisArg, initializers}
	}
	return f.NewCallExpression(f.NewUnscopedHelperName("__runInitializers"), ast.Handle{}, 0, f.NewList(arguments), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewTemplateObjectHelper(cookedArray ast.Handle, rawArray ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(makeTemplateObjectHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__makeTemplateObject"), ast.Handle{}, 0, f.NewList([]ast.Handle{cookedArray, rawArray}), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewPropKeyHelper(expr ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(propKeyHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__propKey"), ast.Handle{}, 0, f.NewList([]ast.Handle{expr}), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewSetFunctionNameHelper(fn ast.Handle, name ast.Handle, prefix string) ast.Handle {
	f.emitContext.RequestEmitHelper(setFunctionNameHelper)
	var arguments []ast.Handle
	if len(prefix) > 0 {
		arguments = []ast.Handle{fn, name, f.NewStringLiteral(prefix, ast.TokenFlagsNone)}
	} else {
		arguments = []ast.Handle{fn, name}
	}
	return f.NewCallExpression(f.NewUnscopedHelperName("__setFunctionName"), ast.Handle{}, 0, f.NewList(arguments), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewImportDefaultHelper(expression ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(importDefaultHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__importDefault"), ast.Handle{}, 0, f.NewList([]ast.Handle{expression}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewImportStarHelper(expression ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(importStarHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__importStar"), ast.Handle{}, 0, f.NewList([]ast.Handle{expression}), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewExportStarHelper(moduleExpression ast.Handle, exportsExpression ast.Handle) ast.Handle {
	f.emitContext.RequestEmitHelper(exportStarHelper)
	return f.NewCallExpression(f.NewUnscopedHelperName("__exportStar"), ast.Handle{}, 0, f.NewList([]ast.Handle{moduleExpression, exportsExpression}), ast.NodeFlagsNone)
}
func (f *NodeFactory) NewAssignmentTargetWrapper(paramName ast.Handle, expression ast.Handle) ast.Handle {
	setAccessor := f.NewSetAccessorDeclaration(0, f.NewIdentifier("value"), 0, f.NewList([]ast.Handle{f.NewParameterDeclaration(0, ast.Handle{}, paramName, ast.Handle{}, ast.Handle{}, ast.Handle{})}), ast.Handle{}, ast.Handle{}, f.NewBlock(f.NewList([]ast.Handle{f.NewExpressionStatement(expression)}), false))
	objLiteral := f.NewObjectLiteralExpression(f.NewList([]ast.Handle{setAccessor}), false)
	return f.NewPropertyAccessExpression(f.NewParenthesizedExpression(objLiteral), ast.Handle{}, f.NewIdentifier("value"), ast.NodeFlagsNone)
}

func (f *NodeFactory) NewRewriteRelativeImportExtensionsHelper(firstArgument ast.Handle, preserveJsx bool) ast.Handle {
	f.emitContext.RequestEmitHelper(rewriteRelativeImportExtensionsHelper)
	var arguments []ast.Handle
	if preserveJsx {
		arguments = []ast.Handle{firstArgument, f.NewToken(ast.KindTrueKeyword)}
	} else {
		arguments = []ast.Handle{firstArgument}
	}
	return f.NewCallExpression(f.NewUnscopedHelperName("__rewriteRelativeImportExtension"), ast.Handle{}, 0, f.NewList(arguments), ast.NodeFlagsNone)
}
