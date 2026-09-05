package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"slices"
)

func isClassNamedEvaluationHelperBlock(emitContext *printer.EmitContext, node ast.Handle) bool {
	if !ast.IsClassStaticBlockDeclaration(node) || len(node.ClassStaticBlockDeclarationBody().Statements()) != 1 {
		return false
	}
	statement := node.ClassStaticBlockDeclarationBody().Statements()[0]
	if ast.IsExpressionStatement(statement) {
		expression := statement.Expression()
		if emitContext.IsCallToHelper(expression, "__setFunctionName") {
			arguments := expression.CallExpressionArguments()
			return node.Store().ListLen(arguments) >= 2 && node.Store().ListAt(arguments, 1) == emitContext.AssignedName(node)
		}
	}
	return false
}

func classHasExplicitlyAssignedName(emitContext *printer.EmitContext, node ast.Handle) bool {
	if assignedName := emitContext.AssignedName(node); !assignedName.IsNil() {
		for _, member := range node.Members() {
			if isClassNamedEvaluationHelperBlock(emitContext, member) {
				return true
			}
		}
	}
	return false
}

func classHasDeclaredOrExplicitlyAssignedName(emitContext *printer.EmitContext, node ast.Handle) bool {
	return !node.Name().IsNil() || classHasExplicitlyAssignedName(emitContext, node)
}

type anonymousFunctionDefinition = ast.Handle

func isAnonymousFunctionDefinition(emitContext *printer.EmitContext, node ast.Handle, cb func(ast.Handle) bool) bool {
	node = ast.SkipOuterExpressions(node, ast.OEKAll)
	switch node.Kind {
	case ast.KindClassExpression:
		if classHasDeclaredOrExplicitlyAssignedName(emitContext, node) {
			return false
		}
		break
	case ast.KindFunctionExpression:
		if !node.FunctionExpressionName().IsNil() {
			return false
		}
		break
	case ast.KindArrowFunction:
		break
	default:
		return false
	}
	if cb != nil {
		return cb(node)
	}
	return true
}
func isNamedEvaluation(emitContext *printer.EmitContext, node ast.Handle) bool {
	return isNamedEvaluationAnd(emitContext, node, nil)
}
func isNamedEvaluationAnd(emitContext *printer.EmitContext, node ast.Handle, cb func(ast.Handle) bool) bool {
	if !ast.IsNamedEvaluationSource(node) {
		return false
	}
	switch node.Kind {
	case ast.KindShorthandPropertyAssignment:
		return isAnonymousFunctionDefinition(emitContext, node.ShorthandPropertyAssignmentObjectAssignmentInitializer(), cb)
	case ast.KindPropertyAssignment, ast.KindVariableDeclaration, ast.KindParameter, ast.KindBindingElement, ast.KindPropertyDeclaration:
		return isAnonymousFunctionDefinition(emitContext, node.Initializer(), cb)
	case ast.KindBinaryExpression:
		return isAnonymousFunctionDefinition(emitContext, node.BinaryExpressionRight(), cb)
	case ast.KindExportAssignment:
		return isAnonymousFunctionDefinition(emitContext, node.Expression(), cb)
	default:
		debug.Fail("Unhandled case in isNamedEvaluation")
		return false
	}
}

func getAssignedNameOfIdentifier(emitContext *printer.EmitContext, name ast.Handle, expression ast.Handle) ast.Handle {
	original := emitContext.MostOriginal(ast.SkipOuterExpressions(expression, ast.OEKAll))
	if (ast.IsClassDeclaration(original) || ast.IsFunctionDeclaration(original)) && original.Name().IsNil() && ast.HasSyntacticModifier(original, ast.ModifierFlagsDefault) {
		return emitContext.Factory.NewStringLiteral("default", ast.TokenFlagsNone)
	}
	return emitContext.Factory.NewStringLiteralFromNode(name)
}
func getAssignedNameOfPropertyName(emitContext *printer.EmitContext, name ast.Handle, assignedNameText string) (assignedName ast.Handle, updatedName ast.Handle) {
	factory := emitContext.Factory
	if len(assignedNameText) > 0 {
		assignedName := factory.NewStringLiteral(assignedNameText, ast.TokenFlagsNone)
		return assignedName, name
	}
	if ast.IsPropertyNameLiteral(name) || ast.IsPrivateIdentifier(name) {
		assignedName := factory.NewStringLiteralFromNode(name)
		return assignedName, name
	}
	expression := name.Expression()
	if ast.IsPropertyNameLiteral(expression) && !ast.IsIdentifier(expression) {
		assignedName := factory.NewStringLiteralFromNode(expression)
		return assignedName, name
	}
	debug.Assert(ast.IsComputedPropertyName(name), "Expected computed property name")
	assignedName = factory.NewGeneratedNameForNode(name)
	emitContext.AddVariableDeclaration(assignedName)
	key := factory.NewPropKeyHelper(expression)
	assignment := factory.NewAssignmentExpression(assignedName, key)
	updatedName = factory.UpdateComputedPropertyName(name, assignment)
	return assignedName, updatedName
}

func createClassNamedEvaluationHelperBlock(emitContext *printer.EmitContext, assignedName ast.Handle, thisExpression ast.Handle) ast.Handle {
	if thisExpression.IsNil() {
		thisExpression = emitContext.Factory.NewThisExpression()
	}
	factory := emitContext.Factory
	expression := factory.NewSetFunctionNameHelper(thisExpression, assignedName, "")
	statement := factory.NewExpressionStatement(expression)
	body := factory.NewBlock(factory.NewList([]ast.Handle{statement}), false)
	block := factory.NewClassStaticBlockDeclaration(0, body)
	emitContext.SetAssignedName(block, assignedName)
	return block
}

func injectClassNamedEvaluationHelperBlockIfMissing(emitContext *printer.EmitContext, node ast.Handle, assignedName ast.Handle, thisExpression ast.Handle) ast.Handle {
	if classHasExplicitlyAssignedName(emitContext, node) {
		return node
	}
	factory := emitContext.Factory
	namedEvaluationBlock := createClassNamedEvaluationHelperBlock(emitContext, assignedName, thisExpression)
	if !node.Name().IsNil() {
		emitContext.SetSourceMapRange(namedEvaluationBlock.Body().Statements()[0], node.Name().Loc())
	}
	insertionIndex := slices.IndexFunc(node.Members(), func(n ast.Handle) bool {
		return isClassThisAssignmentBlock(emitContext, n)
	}) + 1
	leading := slices.Clone(node.Members()[:insertionIndex])
	trailing := slices.Clone(node.Members()[insertionIndex:])
	var members []ast.Handle
	members = append(members, leading...)
	members = append(members, namedEvaluationBlock)
	members = append(members, trailing...)
	membersList := factory.List(node.Store().ListLoc(node.MemberList()), members...)
	oldNode := node
	if ast.IsClassDeclaration(node) {
		node = factory.UpdateClassDeclaration(node, node.Modifiers(), node.Name(), node.TypeParameterList(), node.ClassDeclarationHeritageClauses(), membersList)
	} else {
		node = factory.UpdateClassExpression(node, node.Modifiers(), node.Name(), node.TypeParameterList(), node.ClassExpressionHeritageClauses(), membersList)
	}
	emitContext.SetAssignedName(node, assignedName)
	if ct := emitContext.ClassThis(oldNode); !ct.IsNil() {
		emitContext.SetClassThis(node, ct)
	}
	return node
}
func finishTransformNamedEvaluation(emitContext *printer.EmitContext, expression ast.Handle, assignedName ast.Handle, ignoreEmptyStringLiteral bool) ast.Handle {
	if ignoreEmptyStringLiteral && ast.IsStringLiteral(assignedName) && len(assignedName.Text()) == 0 {
		return expression
	}
	factory := emitContext.Factory
	innerExpression := ast.SkipOuterExpressions(expression, ast.OEKAll)
	var updatedExpression ast.Handle
	if ast.IsClassExpression(innerExpression) {
		updatedExpression = injectClassNamedEvaluationHelperBlockIfMissing(emitContext, innerExpression, assignedName, ast.Handle{})
	} else {
		updatedExpression = factory.NewSetFunctionNameHelper(innerExpression, assignedName, "")
	}
	return factory.RestoreOuterExpressions(expression, updatedExpression, ast.OEKAll)
}
func transformNamedEvaluationOfPropertyAssignment(context *printer.EmitContext, node ast.Handle, ignoreEmptyStringLiteral bool, assignedNameText string) ast.Handle {
	factory := context.Factory
	assignedName, name := getAssignedNameOfPropertyName(context, node.Name(), assignedNameText)
	initializer := finishTransformNamedEvaluation(context, node.Initializer(), assignedName, ignoreEmptyStringLiteral)
	return factory.UpdatePropertyAssignment(node, 0, name, ast.Handle{}, ast.Handle{}, initializer)
}
func transformNamedEvaluationOfShorthandAssignmentProperty(emitContext *printer.EmitContext, node ast.Handle, ignoreEmptyStringLiteral bool, assignedNameText string) ast.Handle {
	factory := emitContext.Factory
	var assignedName ast.Handle
	if len(assignedNameText) > 0 {
		assignedName = factory.NewStringLiteral(assignedNameText, ast.TokenFlagsNone)
	} else {
		assignedName = getAssignedNameOfIdentifier(emitContext, node.Name(), node.ObjectAssignmentInitializer())
	}
	objectAssignmentInitializer := finishTransformNamedEvaluation(emitContext, node.ObjectAssignmentInitializer(), assignedName, ignoreEmptyStringLiteral)
	return factory.UpdateShorthandPropertyAssignment(node, 0, node.Name(), ast.Handle{}, ast.Handle{}, node.EqualsToken(), objectAssignmentInitializer)
}
func transformNamedEvaluationOfVariableDeclaration(emitContext *printer.EmitContext, node ast.Handle, ignoreEmptyStringLiteral bool, assignedNameText string) ast.Handle {
	factory := emitContext.Factory
	var assignedName ast.Handle
	if len(assignedNameText) > 0 {
		assignedName = factory.NewStringLiteral(assignedNameText, ast.TokenFlagsNone)
	} else {
		assignedName = getAssignedNameOfIdentifier(emitContext, node.Name(), node.Initializer())
	}
	initializer := finishTransformNamedEvaluation(emitContext, node.Initializer(), assignedName, ignoreEmptyStringLiteral)
	return factory.UpdateVariableDeclaration(node, node.Name(), ast.Handle{}, ast.Handle{}, initializer)
}
func transformNamedEvaluationOfParameterDeclaration(emitContext *printer.EmitContext, node ast.Handle, ignoreEmptyStringLiteral bool, assignedNameText string) ast.Handle {
	factory := emitContext.Factory
	var assignedName ast.Handle
	if len(assignedNameText) > 0 {
		assignedName = factory.NewStringLiteral(assignedNameText, ast.TokenFlagsNone)
	} else {
		assignedName = getAssignedNameOfIdentifier(emitContext, node.Name(), node.Initializer())
	}
	initializer := finishTransformNamedEvaluation(emitContext, node.Initializer(), assignedName, ignoreEmptyStringLiteral)
	return factory.UpdateParameterDeclaration(node, 0, node.DotDotDotToken(), node.Name(), ast.Handle{}, ast.Handle{}, initializer)
}
func transformNamedEvaluationOfBindingElement(emitContext *printer.EmitContext, node ast.Handle, ignoreEmptyStringLiteral bool, assignedNameText string) ast.Handle {
	factory := emitContext.Factory
	var assignedName ast.Handle
	if len(assignedNameText) > 0 {
		assignedName = factory.NewStringLiteral(assignedNameText, ast.TokenFlagsNone)
	} else {
		assignedName = getAssignedNameOfIdentifier(emitContext, node.Name(), node.Initializer())
	}
	initializer := finishTransformNamedEvaluation(emitContext, node.Initializer(), assignedName, ignoreEmptyStringLiteral)
	return factory.UpdateBindingElement(node, node.DotDotDotToken(), node.PropertyName(), node.Name(), initializer)
}
func transformNamedEvaluationOfPropertyDeclaration(emitContext *printer.EmitContext, node ast.Handle, ignoreEmptyStringLiteral bool, assignedNameText string) ast.Handle {
	factory := emitContext.Factory
	assignedName, name := getAssignedNameOfPropertyName(emitContext, node.Name(), assignedNameText)
	initializer := finishTransformNamedEvaluation(emitContext, node.Initializer(), assignedName, ignoreEmptyStringLiteral)
	return factory.UpdatePropertyDeclaration(node, node.Modifiers(), name, ast.Handle{}, ast.Handle{}, initializer)
}
func transformNamedEvaluationOfAssignmentExpression(emitContext *printer.EmitContext, node ast.Handle, ignoreEmptyStringLiteral bool, assignedNameText string) ast.Handle {
	factory := emitContext.Factory
	var assignedName ast.Handle
	if len(assignedNameText) > 0 {
		assignedName = factory.NewStringLiteral(assignedNameText, ast.TokenFlagsNone)
	} else {
		assignedName = getAssignedNameOfIdentifier(emitContext, node.Left(), node.Right())
	}
	right := finishTransformNamedEvaluation(emitContext, node.Right(), assignedName, ignoreEmptyStringLiteral)
	return factory.UpdateBinaryExpression(node, 0, node.Left(), ast.Handle{}, node.OperatorToken(), right)
}
func transformNamedEvaluationOfExportAssignment(emitContext *printer.EmitContext, node ast.Handle, ignoreEmptyStringLiteral bool, assignedNameText string) ast.Handle {
	factory := emitContext.Factory
	var assignedName ast.Handle
	if len(assignedNameText) > 0 {
		assignedName = factory.NewStringLiteral(assignedNameText, ast.TokenFlagsNone)
	} else if node.IsExportEquals() {
		assignedName = factory.NewStringLiteral("", ast.TokenFlagsNone)
	} else {
		assignedName = factory.NewStringLiteral("default", ast.TokenFlagsNone)
	}
	expression := finishTransformNamedEvaluation(emitContext, node.Expression(), assignedName, ignoreEmptyStringLiteral)
	return factory.UpdateExportAssignment(node, 0, node.IsExportEquals(), ast.Handle{}, expression)
}

func transformNamedEvaluation(context *printer.EmitContext, node ast.Handle, ignoreEmptyStringLiteral bool, assignedName string) ast.Handle {
	switch node.Kind {
	case ast.KindPropertyAssignment:
		return transformNamedEvaluationOfPropertyAssignment(context, node, ignoreEmptyStringLiteral, assignedName)
	case ast.KindShorthandPropertyAssignment:
		return transformNamedEvaluationOfShorthandAssignmentProperty(context, node, ignoreEmptyStringLiteral, assignedName)
	case ast.KindVariableDeclaration:
		return transformNamedEvaluationOfVariableDeclaration(context, node, ignoreEmptyStringLiteral, assignedName)
	case ast.KindParameter:
		return transformNamedEvaluationOfParameterDeclaration(context, node, ignoreEmptyStringLiteral, assignedName)
	case ast.KindBindingElement:
		return transformNamedEvaluationOfBindingElement(context, node, ignoreEmptyStringLiteral, assignedName)
	case ast.KindPropertyDeclaration:
		return transformNamedEvaluationOfPropertyDeclaration(context, node, ignoreEmptyStringLiteral, assignedName)
	case ast.KindBinaryExpression:
		return transformNamedEvaluationOfAssignmentExpression(context, node, ignoreEmptyStringLiteral, assignedName)
	case ast.KindExportAssignment:
		return transformNamedEvaluationOfExportAssignment(context, node, ignoreEmptyStringLiteral, assignedName)
	default:
		debug.Fail("Unhandled case in transformNamedEvaluation")
		return node
	}
}
