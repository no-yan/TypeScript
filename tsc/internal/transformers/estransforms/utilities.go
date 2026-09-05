package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

func convertClassDeclarationToClassExpression(emitContext *printer.EmitContext, node ast.Handle) ast.Handle {
	updated := emitContext.Factory.NewClassExpression(transformers.ExtractModifiers(emitContext, node.Modifiers(), ^ast.ModifierFlagsExportDefault), node.Name(), node.TypeParameterList(), node.HeritageClauses(), node.MemberList())
	emitContext.SetOriginal(updated, node)
	updated.SetLoc(node.Loc())
	return updated
}
func createNotNullCondition(emitContext *printer.EmitContext, left ast.Handle, right ast.Handle, invert bool) ast.Handle {
	token := ast.KindExclamationEqualsEqualsToken
	op := ast.KindAmpersandAmpersandToken
	if invert {
		token = ast.KindEqualsEqualsEqualsToken
		op = ast.KindBarBarToken
	}
	return emitContext.Factory.NewBinaryExpression(0, emitContext.Factory.NewBinaryExpression(0, left, ast.Handle{}, emitContext.Factory.NewToken(token), emitContext.Factory.NewKeywordExpression(ast.KindNullKeyword)), ast.Handle{}, emitContext.Factory.NewToken(op), emitContext.Factory.NewBinaryExpression(0, right, ast.Handle{}, emitContext.Factory.NewToken(token), emitContext.Factory.NewVoidZeroExpression()))
}

type superAccessState struct {
	factory                    *printer.NodeFactory
	capturedSuperProperties    *collections.OrderedSet[string]
	hasSuperElementAccess      bool
	hasSuperPropertyAssignment bool
	superBinding               ast.Handle
	superIndexBinding          ast.Handle
	superAccessVisitor         *ast.HandleVisitor
}

func (s *superAccessState) initSuperAccessVisitor(emitContext *printer.EmitContext, factory *printer.NodeFactory) {
	s.factory = factory
	s.superAccessVisitor = emitContext.NewNodeVisitor(s.visitSuperAccessNode)
}

func (s *superAccessState) visitSuperAccessNode(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindCallExpression:
		call := node
		if ast.IsSuperProperty(call.Expression()) {
			return s.substituteCallExpressionWithSuperAccess(call, s.superAccessVisitor)
		}
		return s.superAccessVisitor.VisitEachChild(node)
	case ast.KindPropertyAccessExpression:
		if node.Expression().Kind == ast.KindSuperKeyword {
			return s.factory.NewPropertyAccessExpression(s.superBinding, ast.Handle{}, node.Name(), ast.NodeFlagsNone)
		}
		return s.superAccessVisitor.VisitEachChild(node)
	case ast.KindElementAccessExpression:
		if node.Expression().Kind == ast.KindSuperKeyword {
			return s.createSuperElementAccessInAsyncMethod(node.ElementAccessExpressionArgumentExpression())
		}
		return s.superAccessVisitor.VisitEachChild(node)
	case ast.KindFunctionExpression, ast.KindFunctionDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindConstructor, ast.KindClassDeclaration, ast.KindClassExpression:
		return node
	default:
		return s.superAccessVisitor.VisitEachChild(node)
	}
}
func (s *superAccessState) substituteSuperAccessesInBody(body ast.Handle) ast.Handle {
	return s.superAccessVisitor.VisitNode(body)
}

func (s *superAccessState) substituteCallExpressionWithSuperAccess(call ast.Handle, visitor *ast.HandleVisitor) ast.Handle {
	expression := call.Expression()
	var target ast.Handle
	if ast.IsPropertyAccessExpression(expression) {
		target = s.factory.NewPropertyAccessExpression(s.superBinding, ast.Handle{}, expression.PropertyAccessExpressionName(), ast.NodeFlagsNone)
	} else if ast.IsElementAccessExpression(expression) {
		target = s.createSuperElementAccessInAsyncMethod(expression.ElementAccessExpressionArgumentExpression())
	} else {
		return visitor.VisitEachChild(call)
	}
	callTarget := s.factory.NewPropertyAccessExpression(target, ast.Handle{}, s.factory.NewIdentifier("call"), ast.NodeFlagsNone)
	var allArgs []ast.Handle
	allArgs = append(allArgs, s.factory.NewThisExpression())
	if call.ArgumentList() != 0 {
		visitedArgs := visitor.VisitNodes(call.ArgumentList())
		if visitedArgs != 0 {
			// append variadic needs []Handle.
			allArgs = append(allArgs, target.Store().ListSlice(visitedArgs).Slice()...)
		}
	}
	result := s.factory.NewCallExpression(callTarget, ast.Handle{}, 0, s.factory.NewList(allArgs), ast.NodeFlagsNone)
	result.SetLoc(call.Loc())
	return result
}

func (s *superAccessState) createSuperElementAccessInAsyncMethod(argumentExpression ast.Handle) ast.Handle {
	superIndexCall := s.factory.NewCallExpression(s.superIndexBinding, ast.Handle{}, 0, s.factory.NewList([]ast.Handle{argumentExpression}), ast.NodeFlagsNone)
	if s.hasSuperPropertyAssignment {
		return s.factory.NewPropertyAccessExpression(superIndexCall, ast.Handle{}, s.factory.NewIdentifier("value"), ast.NodeFlagsNone)
	}
	return superIndexCall
}

func (s *superAccessState) createSuperAccessVariableStatement() ast.Handle {
	f := s.factory
	var accessors []ast.Handle
	for name := range s.capturedSuperProperties.Values() {
		var descriptorProperties []ast.Handle
		getterBody := f.NewPropertyAccessExpression(f.NewKeywordExpression(ast.KindSuperKeyword), ast.Handle{}, f.NewIdentifier(name), ast.NodeFlagsNone)
		getterArrow := f.NewArrowFunction(0, 0, f.NewList([]ast.Handle{}), ast.Handle{}, ast.Handle{}, f.NewToken(ast.KindEqualsGreaterThanToken), getterBody)
		getter := f.NewPropertyAssignment(0, f.NewIdentifier("get"), ast.Handle{}, ast.Handle{}, getterArrow)
		descriptorProperties = append(descriptorProperties, getter)
		if s.hasSuperPropertyAssignment {
			vParam := f.NewParameterDeclaration(0, ast.Handle{}, f.NewIdentifier("v"), ast.Handle{}, ast.Handle{}, ast.Handle{})
			superProp := f.NewPropertyAccessExpression(f.NewKeywordExpression(ast.KindSuperKeyword), ast.Handle{}, f.NewIdentifier(name), ast.NodeFlagsNone)
			assignExpr := f.NewAssignmentExpression(superProp, f.NewIdentifier("v"))
			setterArrow := f.NewArrowFunction(0, 0, f.NewList([]ast.Handle{vParam}), ast.Handle{}, ast.Handle{}, f.NewToken(ast.KindEqualsGreaterThanToken), assignExpr)
			setter := f.NewPropertyAssignment(0, f.NewIdentifier("set"), ast.Handle{}, ast.Handle{}, setterArrow)
			descriptorProperties = append(descriptorProperties, setter)
		}
		descriptor := f.NewObjectLiteralExpression(f.NewList(descriptorProperties), false)
		accessor := f.NewPropertyAssignment(0, f.NewIdentifier(name), ast.Handle{}, ast.Handle{}, descriptor)
		accessors = append(accessors, accessor)
	}
	descriptorsObject := f.NewObjectLiteralExpression(f.NewList(accessors), true)
	objectCreateCall := f.NewCallExpression(f.NewPropertyAccessExpression(f.NewIdentifier("Object"), ast.Handle{}, f.NewIdentifier("create"), ast.NodeFlagsNone), ast.Handle{}, 0, f.NewList([]ast.Handle{f.NewKeywordExpression(ast.KindNullKeyword), descriptorsObject}), ast.NodeFlagsNone)
	decl := f.NewVariableDeclaration(s.superBinding, ast.Handle{}, ast.Handle{}, objectCreateCall)
	declList := f.NewVariableDeclarationList(f.NewList([]ast.Handle{decl}), ast.NodeFlagsConst)
	return f.NewVariableStatement(0, declList)
}

func (s *superAccessState) trackSuperAccess(node ast.Handle) {
	if s.capturedSuperProperties == nil {
		return
	}
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		if node.Expression().Kind == ast.KindSuperKeyword {
			s.capturedSuperProperties.Add(node.Name().Text())
		}
	case ast.KindElementAccessExpression:
		if node.Expression().Kind == ast.KindSuperKeyword {
			s.hasSuperElementAccess = true
		}
	case ast.KindBinaryExpression:
		if ast.IsAssignmentOperator(node.BinaryExpressionOperatorToken().Kind) && assignmentTargetContainsSuperProperty(node.BinaryExpressionLeft()) {
			s.hasSuperPropertyAssignment = true
		}
	case ast.KindPrefixUnaryExpression:
		if isUpdateExpression(node) && assignmentTargetContainsSuperProperty(node.PrefixUnaryExpressionOperand()) {
			s.hasSuperPropertyAssignment = true
		}
	case ast.KindPostfixUnaryExpression:
		if isUpdateExpression(node) && assignmentTargetContainsSuperProperty(node.PostfixUnaryExpressionOperand()) {
			s.hasSuperPropertyAssignment = true
		}
	}
}

func createAccessorPropertyBackingField(f *printer.NodeFactory, node ast.Handle, modifiers ast.ListRef, initializer ast.Handle) ast.Handle {
	return f.UpdatePropertyDeclaration(node, modifiers, f.NewGeneratedPrivateNameForNodeEx(node.Name(), printer.AutoGenerateOptions{Suffix: "_accessor_storage"}), ast.Handle{}, ast.Handle{}, initializer)
}
