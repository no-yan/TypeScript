package transformers

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"slices"
)

func IsGeneratedIdentifier(emitContext *printer.EmitContext, name ast.Handle) bool {
	return emitContext.HasAutoGenerateInfo(name)
}
func IsHelperName(emitContext *printer.EmitContext, name ast.Handle) bool {
	return emitContext.EmitFlags(name)&printer.EFHelperName != 0
}
func IsLocalName(emitContext *printer.EmitContext, name ast.Handle) bool {
	return emitContext.EmitFlags(name)&printer.EFLocalName != 0
}
func IsExportName(emitContext *printer.EmitContext, name ast.Handle) bool {
	return emitContext.EmitFlags(name)&printer.EFExportName != 0
}
func IsIdentifierReference(name ast.Handle, parent ast.Handle) bool {
	switch parent.Kind {
	case ast.KindBinaryExpression, ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression, ast.KindYieldExpression, ast.KindAsExpression, ast.KindSatisfiesExpression, ast.KindElementAccessExpression, ast.KindNonNullExpression, ast.KindSpreadElement, ast.KindSpreadAssignment, ast.KindParenthesizedExpression, ast.KindArrayLiteralExpression, ast.KindDeleteExpression, ast.KindTypeOfExpression, ast.KindVoidExpression, ast.KindAwaitExpression, ast.KindTypeAssertionExpression, ast.KindExpressionWithTypeArguments, ast.KindJsxSelfClosingElement, ast.KindJsxSpreadAttribute, ast.KindJsxExpression, ast.KindPartiallyEmittedExpression:
		return true
	case ast.KindComputedPropertyName, ast.KindDecorator, ast.KindIfStatement, ast.KindDoStatement, ast.KindWhileStatement, ast.KindWithStatement, ast.KindReturnStatement, ast.KindSwitchStatement, ast.KindCaseClause, ast.KindThrowStatement, ast.KindExpressionStatement, ast.KindExportAssignment, ast.KindPropertyAccessExpression, ast.KindTemplateSpan:
		return parent.Expression() == name
	case ast.KindVariableDeclaration, ast.KindParameter, ast.KindBindingElement, ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindPropertyAssignment, ast.KindEnumMember, ast.KindJsxAttribute:
		return parent.Initializer() == name
	case ast.KindShorthandPropertyAssignment:
		return parent.ShorthandPropertyAssignmentObjectAssignmentInitializer() == name
	case ast.KindForStatement:
		return parent.Initializer() == name || parent.ForStatementCondition() == name || parent.ForStatementIncrementor() == name
	case ast.KindForInStatement, ast.KindForOfStatement:
		return parent.Initializer() == name || parent.Expression() == name
	case ast.KindImportEqualsDeclaration:
		return parent.ImportEqualsDeclarationModuleReference() == name
	case ast.KindArrowFunction:
		return parent.Body() == name
	case ast.KindConditionalExpression:
		return parent.ConditionalExpressionCondition() == name || parent.ConditionalExpressionWhenTrue() == name || parent.ConditionalExpressionWhenFalse() == name
	case ast.KindCallExpression, ast.KindNewExpression:
		return parent.Expression() == name || slices.Contains(parent.Arguments(), name)
	case ast.KindTaggedTemplateExpression:
		return parent.TaggedTemplateExpressionTag() == name
	case ast.KindImportAttribute:
		return parent.ImportAttributeValue() == name
	case ast.KindJsxOpeningElement, ast.KindJsxClosingElement:
		return parent.TagName() == name
	default:
		return false
	}
}
func convertBindingElementToArrayAssignmentElement(emitContext *printer.EmitContext, element ast.Handle) ast.Handle {
	if element.Name().IsNil() {
		elision := emitContext.Factory.NewOmittedExpression()
		emitContext.SetOriginal(elision, element)
		emitContext.AssignCommentAndSourceMapRanges(elision, element)
		return elision
	}
	if !element.DotDotDotToken().IsNil() {
		spread := emitContext.Factory.NewSpreadElement(element.Name())
		emitContext.SetOriginal(spread, element)
		emitContext.AssignCommentAndSourceMapRanges(spread, element)
		return spread
	}
	expression := convertBindingNameToAssignmentElementTarget(emitContext, element.Name())
	if !element.Initializer().IsNil() {
		assignment := emitContext.Factory.NewAssignmentExpression(expression, element.Initializer())
		emitContext.SetOriginal(assignment, element)
		emitContext.AssignCommentAndSourceMapRanges(assignment, element)
		return assignment
	}
	return expression
}
func convertBindingElementToObjectAssignmentElement(emitContext *printer.EmitContext, element ast.Handle) ast.Handle {
	if !element.DotDotDotToken().IsNil() {
		spread := emitContext.Factory.NewSpreadAssignment(element.Name())
		emitContext.SetOriginal(spread, element)
		emitContext.AssignCommentAndSourceMapRanges(spread, element)
		return spread
	}
	if !element.PropertyName().IsNil() {
		expression := convertBindingNameToAssignmentElementTarget(emitContext, element.Name())
		if !element.Initializer().IsNil() {
			expression = emitContext.Factory.NewAssignmentExpression(expression, element.Initializer())
		}
		assignment := emitContext.Factory.NewPropertyAssignment(0, element.PropertyName(), ast.Handle{}, ast.Handle{}, expression)
		emitContext.SetOriginal(assignment, element)
		emitContext.AssignCommentAndSourceMapRanges(assignment, element)
		return assignment
	}
	var equalsToken ast.Handle
	if !element.Initializer().IsNil() {
		equalsToken = emitContext.Factory.NewToken(ast.KindEqualsToken)
	}
	assignment := emitContext.Factory.NewShorthandPropertyAssignment(0, element.Name(), ast.Handle{}, ast.Handle{}, equalsToken, element.Initializer())
	emitContext.SetOriginal(assignment, element)
	emitContext.AssignCommentAndSourceMapRanges(assignment, element)
	return assignment
}
func ConvertBindingPatternToAssignmentPattern(emitContext *printer.EmitContext, element ast.Handle) ast.Handle {
	switch element.Kind {
	case ast.KindArrayBindingPattern:
		return convertBindingElementToArrayAssignmentPattern(emitContext, element)
	case ast.KindObjectBindingPattern:
		return convertBindingElementToObjectAssignmentPattern(emitContext, element)
	default:
		panic("Unknown binding pattern")
	}
}
func convertBindingElementToObjectAssignmentPattern(emitContext *printer.EmitContext, element ast.Handle) ast.Handle {
	var properties []ast.Handle
	for _, child := range element.Elements() {
		properties = append(properties, convertBindingElementToObjectAssignmentElement(emitContext, child))
	}
	loc := emitContext.Factory.Store().ListLoc(element.ElementList())
	propertyList := emitContext.Factory.List(loc, properties...)
	object := emitContext.Factory.NewObjectLiteralExpression(propertyList, false)
	emitContext.SetOriginal(object, element)
	emitContext.AssignCommentAndSourceMapRanges(object, element)
	return object
}
func convertBindingElementToArrayAssignmentPattern(emitContext *printer.EmitContext, element ast.Handle) ast.Handle {
	var elements []ast.Handle
	for _, child := range element.Elements() {
		elements = append(elements, convertBindingElementToArrayAssignmentElement(emitContext, child))
	}
	loc := emitContext.Factory.Store().ListLoc(element.ElementList())
	elementList := emitContext.Factory.List(loc, elements...)
	object := emitContext.Factory.NewArrayLiteralExpression(elementList, false)
	emitContext.SetOriginal(object, element)
	emitContext.AssignCommentAndSourceMapRanges(object, element)
	return object
}
func convertBindingNameToAssignmentElementTarget(emitContext *printer.EmitContext, element ast.Handle) ast.Handle {
	if ast.IsBindingPattern(element) {
		return ConvertBindingPatternToAssignmentPattern(emitContext, element)
	}
	return element
}
func ConvertVariableDeclarationToAssignmentExpression(emitContext *printer.EmitContext, element ast.Handle) ast.Handle {
	if element.Initializer().IsNil() {
		return ast.Handle{}
	}
	expression := convertBindingNameToAssignmentElementTarget(emitContext, element.Name())
	assignment := emitContext.Factory.NewAssignmentExpression(expression, element.Initializer())
	emitContext.SetOriginal(assignment, element)
	emitContext.AssignCommentAndSourceMapRanges(assignment, element)
	return assignment
}
func SingleOrMany(nodes []ast.Handle, factory *printer.NodeFactory) ast.Handle {
	if nodes == nil {
		return ast.Handle{}
	}
	if len(nodes) == 1 {
		return nodes[0]
	}
	return factory.NewSyntaxList(factory.NewList(nodes))
}

func IsSimpleCopiableExpression(expression ast.Handle) bool {
	return ast.IsStringLiteralLike(expression) || ast.IsNumericLiteral(expression) || ast.IsKeywordKind(expression.Kind) || ast.IsIdentifier(expression)
}
func IsOriginalNodeSingleLine(emitContext *printer.EmitContext, node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	original := emitContext.MostOriginal(node)
	if original.IsNil() {
		return false
	}
	source := ast.GetSourceFileOfNode(original)
	if source == nil {
		return false
	}
	startLine := scanner.GetECMALineOfPosition(source, original.Loc().Pos())
	endLine := scanner.GetECMALineOfPosition(source, original.Loc().End())
	return startLine == endLine
}

func IsSimpleInlineableExpression(expression ast.Handle) bool {
	return !ast.IsIdentifier(expression) && IsSimpleCopiableExpression(expression)
}

func FindSuperStatementIndexPath(statements []ast.Handle, start int) []int {
	indices := findSuperStatementIndexPathWorker(statements, start, nil)
	slices.Reverse(indices)
	return indices
}
func findSuperStatementIndexPathWorker(statements []ast.Handle, start int, indices []int) []int {
	for i := start; i < len(statements); i++ {
		statement := statements[i]
		if !GetSuperCallFromStatement(statement).IsNil() {
			return append(indices, i)
		} else if ast.IsTryStatement(statement) {
			if result := findSuperStatementIndexPathWorker(statement.TryStatementTryBlock().Statements(), 0, indices); result != nil {
				return append(result, i)
			}
		}
	}
	return nil
}

func GetSuperCallFromStatement(statement ast.Handle) ast.Handle {
	if !ast.IsExpressionStatement(statement) {
		return ast.Handle{}
	}
	expression := ast.SkipParentheses(statement.Expression())
	if ast.IsSuperCall(expression) {
		return expression
	}
	return ast.Handle{}
}

func MoveRangePastModifiers(node ast.Handle) core.TextRange {
	if ast.IsPropertyDeclaration(node) || ast.IsMethodDeclaration(node) {
		return core.NewTextRange(node.Name().Pos(), node.End())
	}
	var lastModifier ast.Handle
	if ast.CanHaveModifiers(node) {
		lastModifier = core.LastOrNil(node.ModifierNodes())
	}
	if !lastModifier.IsNil() && !ast.PositionIsSynthesized(lastModifier.End()) {
		return core.NewTextRange(lastModifier.End(), node.End())
	}
	return MoveRangePastDecorators(node)
}

func MoveRangePastDecorators(node ast.Handle) core.TextRange {
	var lastDecorator ast.Handle
	if ast.CanHaveModifiers(node) {
		nodes := node.ModifierNodes()
		if nodes != nil {
			lastDecorator = core.FindLast(nodes, ast.IsDecorator)
		}
	}
	if !lastDecorator.IsNil() && !ast.PositionIsSynthesized(lastDecorator.End()) {
		return core.NewTextRange(lastDecorator.End(), node.End())
	}
	return node.Loc()
}

func GetNonAssignmentOperatorForCompoundAssignment(kind ast.Kind) ast.Kind {
	switch kind {
	case ast.KindPlusEqualsToken:
		return ast.KindPlusToken
	case ast.KindMinusEqualsToken:
		return ast.KindMinusToken
	case ast.KindAsteriskEqualsToken:
		return ast.KindAsteriskToken
	case ast.KindAsteriskAsteriskEqualsToken:
		return ast.KindAsteriskAsteriskToken
	case ast.KindSlashEqualsToken:
		return ast.KindSlashToken
	case ast.KindPercentEqualsToken:
		return ast.KindPercentToken
	case ast.KindLessThanLessThanEqualsToken:
		return ast.KindLessThanLessThanToken
	case ast.KindGreaterThanGreaterThanEqualsToken:
		return ast.KindGreaterThanGreaterThanToken
	case ast.KindGreaterThanGreaterThanGreaterThanEqualsToken:
		return ast.KindGreaterThanGreaterThanGreaterThanToken
	case ast.KindAmpersandEqualsToken:
		return ast.KindAmpersandToken
	case ast.KindBarEqualsToken:
		return ast.KindBarToken
	case ast.KindCaretEqualsToken:
		return ast.KindCaretToken
	case ast.KindBarBarEqualsToken:
		return ast.KindBarBarToken
	case ast.KindAmpersandAmpersandEqualsToken:
		return ast.KindAmpersandAmpersandToken
	case ast.KindQuestionQuestionEqualsToken:
		return ast.KindQuestionQuestionToken
	}
	return kind
}
