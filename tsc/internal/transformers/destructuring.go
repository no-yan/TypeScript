package transformers

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"slices"
	"strconv"
)

type FlattenLevel int

const (
	FlattenLevelAll FlattenLevel = iota
	FlattenLevelObjectRest
)

type CreateAssignmentCallback func(name ast.Handle, value ast.Handle, location *core.TextRange) ast.Handle

func FlattenDestructuringAssignment(tx *Transformer, node ast.Handle, needsValue bool, level FlattenLevel, createAssignmentCallback CreateAssignmentCallback) ast.Handle {
	f := newFlattener(tx, level)
	f.createAssignmentCallback = createAssignmentCallback
	f.hoistTempVariables = true
	f.emitBindingOrAssignment = (*flattener).emitAssignment
	f.createArrayBindingOrAssignmentPattern = (*flattener).createArrayAssignmentPattern
	f.createObjectBindingOrAssignmentPattern = (*flattener).createObjectAssignmentPattern
	f.createArrayBindingOrAssignmentElement = (*flattener).createArrayAssignmentElement
	return f.flattenDestructuringAssignment(node, needsValue)
}

type pendingDecl struct {
	pendingExpressions []ast.Handle
	name               ast.Handle
	value              ast.Handle
	location           core.TextRange
	original           ast.Handle
}

func FlattenDestructuringBinding(tx *Transformer, node ast.Handle, rval ast.Handle, level FlattenLevel, hoistTempVariables bool, skipInitializer bool) ast.Handle {
	f := newFlattener(tx, level)
	f.hoistTempVariables = hoistTempVariables
	f.emitBindingOrAssignment = (*flattener).emitBinding
	f.createArrayBindingOrAssignmentPattern = (*flattener).createArrayBindingPattern
	f.createObjectBindingOrAssignmentPattern = (*flattener).createObjectBindingPattern
	f.createArrayBindingOrAssignmentElement = (*flattener).createArrayBindingElement
	return f.flattenDestructuringBinding(node, rval, skipInitializer)
}

type flattener struct {
	tx                                     *Transformer
	level                                  FlattenLevel
	createAssignmentCallback               CreateAssignmentCallback
	expressions                            []ast.Handle
	declarations                           []pendingDecl
	hasTransformedPriorElement             bool
	hoistTempVariables                     bool
	emitBindingOrAssignment                func(f *flattener, target ast.Handle, value ast.Handle, location core.TextRange, original ast.Handle)
	createArrayBindingOrAssignmentPattern  func(f *flattener, elements []ast.Handle) ast.Handle
	createObjectBindingOrAssignmentPattern func(f *flattener, elements []ast.Handle) ast.Handle
	createArrayBindingOrAssignmentElement  func(f *flattener, expr ast.Handle) ast.Handle
}

func newFlattener(tx *Transformer, level FlattenLevel) *flattener {
	return &flattener{tx: tx, level: level}
}
func (f *flattener) createArrayAssignmentPattern(elements []ast.Handle) ast.Handle {
	return f.tx.Factory().NewArrayLiteralExpression(f.tx.Factory().NewList(elements), false)
}
func (f *flattener) createObjectAssignmentPattern(elements []ast.Handle) ast.Handle {
	return f.tx.Factory().NewObjectLiteralExpression(f.tx.Factory().NewList(elements), false)
}
func (f *flattener) createArrayAssignmentElement(expr ast.Handle) ast.Handle {
	return expr
}
func (f *flattener) emitAssignment(target ast.Handle, value ast.Handle, location core.TextRange, original ast.Handle) {
	var expression ast.Handle
	if f.createAssignmentCallback != nil && ast.IsIdentifier(target) {
		expression = f.createAssignmentCallback(target, value, &location)
	} else {
		expression = f.tx.Factory().NewAssignmentExpression(f.tx.Visitor().VisitNode(target), value)
		expression.SetLoc(location)
	}
	f.tx.EmitContext().SetOriginal(expression, original)
	f.emitExpression(expression)
}
func (f *flattener) createArrayBindingPattern(elements []ast.Handle) ast.Handle {
	return f.tx.Factory().NewBindingPattern(ast.KindArrayBindingPattern, f.tx.Factory().NewList(elements))
}
func (f *flattener) createObjectBindingPattern(elements []ast.Handle) ast.Handle {
	return f.tx.Factory().NewBindingPattern(ast.KindObjectBindingPattern, f.tx.Factory().NewList(elements))
}
func (f *flattener) createArrayBindingElement(expr ast.Handle) ast.Handle {
	return f.tx.Factory().NewBindingElement(ast.Handle{}, ast.Handle{}, expr, ast.Handle{})
}
func (f *flattener) emitBinding(target ast.Handle, value ast.Handle, location core.TextRange, original ast.Handle) {
	if len(f.expressions) > 0 {
		value = f.tx.Factory().InlineExpressions(append(f.expressions, value))
		f.expressions = nil
	}
	f.declarations = append(f.declarations, pendingDecl{name: target, value: value, location: location, original: original})
}
func (f *flattener) emitExpression(expr ast.Handle) {
	f.expressions = append(f.expressions, expr)
}
func (f *flattener) ensureIdentifier(value ast.Handle, reuseIdentifierExpressions bool, location core.TextRange) ast.Handle {
	if reuseIdentifierExpressions && ast.IsIdentifier(value) {
		return value
	}
	temp := f.tx.Factory().NewTempVariable()
	if f.hoistTempVariables {
		f.tx.EmitContext().AddVariableDeclaration(temp)
		assign := f.tx.Factory().NewAssignmentExpression(temp, value)
		assign.SetLoc(location)
		f.emitExpression(assign)
	} else {
		f.emitBindingOrAssignment(f, temp, value, location, ast.Handle{})
	}
	return temp
}
func (f *flattener) createDefaultValueCheck(value ast.Handle, defaultValue ast.Handle, location core.TextRange) ast.Handle {
	value = f.ensureIdentifier(value, true, location)
	return f.tx.Factory().NewConditionalExpression(f.tx.Factory().NewTypeCheck(value, "undefined"), f.tx.Factory().NewToken(ast.KindQuestionToken), defaultValue, f.tx.Factory().NewToken(ast.KindColonToken), value)
}
func (f *flattener) createDestructuringPropertyAccess(value ast.Handle, propertyName ast.Handle) ast.Handle {
	if ast.IsComputedPropertyName(propertyName) {
		argumentExpression := f.ensureIdentifier(f.tx.Visitor().VisitNode(propertyName.Expression()), false, propertyName.Loc())
		return f.tx.Factory().NewElementAccessExpression(value, ast.Handle{}, argumentExpression, ast.NodeFlagsNone)
	} else if ast.IsStringOrNumericLiteralLike(propertyName) || ast.IsBigIntLiteral(propertyName) {
		argumentExpression := f.tx.Factory().DeepCloneNode(propertyName)
		return f.tx.Factory().NewElementAccessExpression(value, ast.Handle{}, argumentExpression, ast.NodeFlagsNone)
	} else {
		name := f.tx.Factory().NewIdentifier(propertyName.Text())
		return f.tx.Factory().NewPropertyAccessExpression(value, ast.Handle{}, name, ast.NodeFlagsNone)
	}
}
func (f *flattener) flattenDestructuringAssignment(node ast.Handle, needsValue bool) ast.Handle {
	location := node.Loc()
	var value ast.Handle
	if ast.IsDestructuringAssignment(node) {
		value = node.BinaryExpressionRight()
		for ast.IsEmptyArrayLiteral(node.BinaryExpressionLeft()) || ast.IsEmptyObjectLiteral(node.BinaryExpressionLeft()) {
			if ast.IsDestructuringAssignment(value) {
				node = value
				location = node.Loc()
				value = node.BinaryExpressionRight()
			} else {
				return f.tx.Visitor().VisitNode(value)
			}
		}
	}
	if !value.IsNil() {
		value = f.tx.Visitor().VisitNode(value)
		if ast.IsIdentifier(value) && BindingOrAssignmentElementAssignsToName(node, value.Text()) || BindingOrAssignmentElementContainsNonLiteralComputedName(node) {
			value = f.ensureIdentifier(value, false, location)
		} else if needsValue {
			value = f.ensureIdentifier(value, true, location)
		} else if ast.NodeIsSynthesized(node) {
			location = value.Loc()
		}
	}
	f.flattenBindingOrAssignmentElement(node, value, location, ast.IsDestructuringAssignment(node))
	if !value.IsNil() && needsValue {
		if len(f.expressions) == 0 {
			return value
		}
		f.expressions = append(f.expressions, value)
	}
	res := f.tx.Factory().InlineExpressions(f.expressions)
	if !res.IsNil() {
		return res
	}
	return f.tx.Factory().NewOmittedExpression()
}
func (f *flattener) flattenDestructuringBinding(node ast.Handle, rval ast.Handle, skipInitializer bool) ast.Handle {
	if ast.IsVariableDeclaration(node) {
		initializer := GetInitializerOfBindingOrAssignmentElement(node)
		if !initializer.IsNil() && (ast.IsIdentifier(initializer) && BindingOrAssignmentElementAssignsToName(node, initializer.Text()) || BindingOrAssignmentElementContainsNonLiteralComputedName(node)) {
			initializer = f.ensureIdentifier(f.tx.Visitor().VisitNode(initializer), false, initializer.Loc())
			node = f.tx.Factory().UpdateVariableDeclaration(node, node.Name(), ast.Handle{}, ast.Handle{}, initializer)
		}
	}
	f.flattenBindingOrAssignmentElement(node, rval, node.Loc(), skipInitializer)
	if len(f.expressions) > 0 {
		temp := f.tx.Factory().NewTempVariable()
		if f.hoistTempVariables {
			value := f.tx.Factory().InlineExpressions(f.expressions)
			f.expressions = nil
			f.emitBindingOrAssignment(f, temp, value, core.TextRange{}, ast.Handle{})
		} else {
			f.tx.EmitContext().AddVariableDeclaration(temp)
			last := &f.declarations[len(f.declarations)-1]
			last.pendingExpressions = append(last.pendingExpressions, f.tx.Factory().NewAssignmentExpression(temp, last.value))
			last.pendingExpressions = append(last.pendingExpressions, f.expressions...)
			last.value = temp
		}
	}
	decls := make([]ast.Handle, 0, len(f.declarations))
	for _, pending := range f.declarations {
		expr := pending.value
		if len(pending.pendingExpressions) > 0 {
			expr = f.tx.Factory().InlineExpressions(append(pending.pendingExpressions, pending.value))
		}
		decl := f.tx.Factory().NewVariableDeclaration(pending.name, ast.Handle{}, ast.Handle{}, expr)
		decl.SetLoc(pending.location)
		if !pending.original.IsNil() {
			f.tx.EmitContext().SetOriginal(decl, pending.original)
		}
		decls = append(decls, decl)
	}
	if len(decls) == 1 {
		return decls[0]
	}
	if len(decls) == 0 {
		return ast.Handle{}
	}
	return f.tx.Factory().NewSyntaxList(f.tx.Factory().NewList(decls))
}
func (f *flattener) flattenBindingOrAssignmentElement(element ast.Handle, value ast.Handle, location core.TextRange, skipInitializer bool) {
	bindingTarget := ast.GetTargetOfBindingOrAssignmentElement(element)
	if bindingTarget.IsNil() {
		return
	}
	if !skipInitializer {
		initializer := f.tx.Visitor().VisitNode(GetInitializerOfBindingOrAssignmentElement(element))
		if !initializer.IsNil() {
			if !value.IsNil() {
				value = f.createDefaultValueCheck(value, initializer, location)
				if !IsSimpleCopiableExpression(initializer) && (ast.IsBindingPattern(bindingTarget) || ast.IsAssignmentPattern(bindingTarget)) {
					value = f.ensureIdentifier(value, true, location)
				}
			} else {
				value = initializer
			}
		} else if value.IsNil() {
			value = f.tx.Factory().NewVoidZeroExpression()
		}
	}
	if isObjectBindingOrAssignmentPattern(bindingTarget) {
		f.flattenObjectBindingOrAssignmentPattern(element, bindingTarget, value, location)
	} else if isArrayBindingOrAssignmentPattern(bindingTarget) {
		f.flattenArrayBindingOrAssignmentPattern(element, bindingTarget, value, location)
	} else {
		f.emitBindingOrAssignment(f, bindingTarget, value, location, element)
	}
}
func (f *flattener) flattenObjectBindingOrAssignmentPattern(parent ast.Handle, pattern ast.Handle, value ast.Handle, location core.TextRange) {
	elements := ast.GetElementsOfBindingOrAssignmentPattern(pattern)
	numElements := len(elements)
	if numElements != 1 {
		reuseIdentifierExpressions := !ast.IsDeclarationBindingElement(parent) || numElements != 0
		value = f.ensureIdentifier(value, reuseIdentifierExpressions, location)
	}
	var bindingElements []ast.Handle
	var computedTempVariables []ast.Handle
	for i, element := range elements {
		if ast.GetRestIndicatorOfBindingOrAssignmentElement(element).IsNil() {
			propertyName := ast.TryGetPropertyNameOfBindingOrAssignmentElement(element)
			if f.level >= FlattenLevelObjectRest && element.SubtreeFacts()&(ast.SubtreeContainsRestOrSpread|ast.SubtreeContainsObjectRestOrSpread) == 0 && ast.GetTargetOfBindingOrAssignmentElement(element).SubtreeFacts()&(ast.SubtreeContainsRestOrSpread|ast.SubtreeContainsObjectRestOrSpread) == 0 && !ast.IsComputedPropertyName(propertyName) {
				bindingElements = append(bindingElements, f.tx.Visitor().VisitNode(element))
			} else {
				if len(bindingElements) > 0 {
					f.emitBindingOrAssignment(f, f.createObjectBindingOrAssignmentPattern(f, bindingElements), value, location, pattern)
					bindingElements = nil
				}
				rhsValue := f.createDestructuringPropertyAccess(value, propertyName)
				if ast.IsComputedPropertyName(propertyName) {
					computedTempVariables = append(computedTempVariables, rhsValue.ElementAccessExpressionArgumentExpression())
				}
				f.flattenBindingOrAssignmentElement(element, rhsValue, element.Loc(), false)
			}
		} else if i == numElements-1 {
			if len(bindingElements) > 0 {
				f.emitBindingOrAssignment(f, f.createObjectBindingOrAssignmentPattern(f, bindingElements), value, location, pattern)
				bindingElements = nil
			}
			rhsValue := f.tx.Factory().NewRestHelper(value, elements, computedTempVariables, pattern.Loc())
			f.flattenBindingOrAssignmentElement(element, rhsValue, element.Loc(), false)
		}
	}
	if len(bindingElements) > 0 {
		f.emitBindingOrAssignment(f, f.createObjectBindingOrAssignmentPattern(f, bindingElements), value, location, pattern)
	}
}

type restIdElemPair struct {
	id      ast.Handle
	element ast.Handle
}

func (f *flattener) flattenArrayBindingOrAssignmentPattern(parent ast.Handle, pattern ast.Handle, value ast.Handle, location core.TextRange) {
	elements := ast.GetElementsOfBindingOrAssignmentPattern(pattern)
	numElements := len(elements)
	if numElements != 1 && (f.level < FlattenLevelObjectRest || numElements == 0) || core.Every(elements, ast.IsOmittedExpression) {
		reuseIdentifierExpressions := !ast.IsDeclarationBindingElement(parent) || numElements != 0
		value = f.ensureIdentifier(value, reuseIdentifierExpressions, location)
	}
	var bindingElements []ast.Handle
	var restContainingElements []restIdElemPair
	for i, element := range elements {
		if f.level >= FlattenLevelObjectRest {
			if element.SubtreeFacts()&ast.SubtreeContainsObjectRestOrSpread != 0 || f.hasTransformedPriorElement && !isSimpleBindingOrAssignmentElement(element) {
				f.hasTransformedPriorElement = true
				temp := f.tx.Factory().NewTempVariable()
				if f.hoistTempVariables {
					f.tx.EmitContext().AddVariableDeclaration(temp)
				}
				restContainingElements = append(restContainingElements, restIdElemPair{temp, element})
				bindingElements = append(bindingElements, f.createArrayBindingOrAssignmentElement(f, temp))
			} else {
				bindingElements = append(bindingElements, element)
			}
		} else if ast.IsOmittedExpression(element) {
			continue
		} else if ast.GetRestIndicatorOfBindingOrAssignmentElement(element).IsNil() {
			rhsValue := f.tx.Factory().NewElementAccessExpression(value, ast.Handle{}, f.tx.Factory().NewNumericLiteral(strconv.Itoa(i), ast.TokenFlagsNone), ast.NodeFlagsNone)
			f.flattenBindingOrAssignmentElement(element, rhsValue, element.Loc(), false)
		} else if i == numElements-1 {
			rhsValue := f.tx.Factory().NewArraySliceCall(value, i)
			f.flattenBindingOrAssignmentElement(element, rhsValue, element.Loc(), false)
		}
	}
	if len(bindingElements) > 0 {
		f.emitBindingOrAssignment(f, f.createArrayBindingOrAssignmentPattern(f, bindingElements), value, location, pattern)
	}
	if len(restContainingElements) > 0 {
		for _, pair := range restContainingElements {
			f.flattenBindingOrAssignmentElement(pair.element, pair.id, pair.element.Loc(), false)
		}
	}
}

func BindingOrAssignmentElementAssignsToName(element ast.Handle, name string) bool {
	target := ast.GetTargetOfBindingOrAssignmentElement(element)
	if target.IsNil() {
		return false
	}
	if ast.IsBindingPattern(target) || ast.IsAssignmentPattern(target) {
		return bindingOrAssignmentPatternAssignsToName(target, name)
	} else if ast.IsIdentifier(target) {
		return target.Text() == name
	}
	return false
}
func bindingOrAssignmentPatternAssignsToName(pattern ast.Handle, name string) bool {
	elements := ast.GetElementsOfBindingOrAssignmentPattern(pattern)
	for _, element := range elements {
		if BindingOrAssignmentElementAssignsToName(element, name) {
			return true
		}
	}
	return false
}

func BindingOrAssignmentElementContainsNonLiteralComputedName(element ast.Handle) bool {
	propertyName := ast.TryGetPropertyNameOfBindingOrAssignmentElement(element)
	if !propertyName.IsNil() && ast.IsComputedPropertyName(propertyName) && !ast.IsLiteralExpression(propertyName.Expression()) {
		return true
	}
	target := ast.GetTargetOfBindingOrAssignmentElement(element)
	return !target.IsNil() && (ast.IsBindingPattern(target) || ast.IsAssignmentPattern(target)) && bindingOrAssignmentPatternContainsNonLiteralComputedName(target)
}
func bindingOrAssignmentPatternContainsNonLiteralComputedName(pattern ast.Handle) bool {
	elements := ast.GetElementsOfBindingOrAssignmentPattern(pattern)
	return slices.ContainsFunc(elements, BindingOrAssignmentElementContainsNonLiteralComputedName)
}

func GetInitializerOfBindingOrAssignmentElement(bindingElement ast.Handle) ast.Handle {
	if bindingElement.IsNil() {
		return ast.Handle{}
	}
	if ast.IsDeclarationBindingElement(bindingElement) {
		return bindingElement.Initializer()
	}
	if ast.IsPropertyAssignment(bindingElement) {
		initializer := bindingElement.Initializer()
		if ast.IsAssignmentExpression(initializer, true) {
			return initializer.BinaryExpressionRight()
		}
		return ast.Handle{}
	}
	if ast.IsShorthandPropertyAssignment(bindingElement) {
		return bindingElement.ShorthandPropertyAssignmentObjectAssignmentInitializer()
	}
	if ast.IsAssignmentExpression(bindingElement, true) {
		return bindingElement.BinaryExpressionRight()
	}
	if ast.IsSpreadElement(bindingElement) {
		return GetInitializerOfBindingOrAssignmentElement(bindingElement.Expression())
	}
	return ast.Handle{}
}
func isObjectBindingOrAssignmentPattern(node ast.Handle) bool {
	return !node.IsNil() && (node.Kind == ast.KindObjectBindingPattern || node.Kind == ast.KindObjectLiteralExpression)
}
func isArrayBindingOrAssignmentPattern(node ast.Handle) bool {
	return !node.IsNil() && (node.Kind == ast.KindArrayBindingPattern || node.Kind == ast.KindArrayLiteralExpression)
}
func isSimpleBindingOrAssignmentElement(element ast.Handle) bool {
	target := ast.GetTargetOfBindingOrAssignmentElement(element)
	if target.IsNil() || ast.IsOmittedExpression(target) {
		return true
	}
	propertyName := ast.TryGetPropertyNameOfBindingOrAssignmentElement(element)
	if !propertyName.IsNil() && !ast.IsPropertyNameLiteral(propertyName) {
		return false
	}
	initializer := GetInitializerOfBindingOrAssignmentElement(element)
	if !initializer.IsNil() && !IsSimpleInlineableExpression(initializer) {
		return false
	}
	if ast.IsBindingPattern(target) || ast.IsAssignmentPattern(target) {
		return core.Every(ast.GetElementsOfBindingOrAssignmentPattern(target), isSimpleBindingOrAssignmentElement)
	}
	return ast.IsIdentifier(target)
}
