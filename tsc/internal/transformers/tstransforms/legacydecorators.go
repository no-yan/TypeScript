package tstransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type LegacyDecoratorsTransformer struct {
	transformers.Transformer
	languageVersion   core.ScriptTarget
	referenceResolver binder.ReferenceResolver
	classAliases      map[ /**
	 * A map that keeps track of aliases created for classes with decorators to avoid issues
	 * with the double-binding behavior of classes.
	 */ // we have to visit all identifiers in classes, just in case they require substitution
	// Decorators are elided. They will be emitted as part of `visitClassDeclaration`.
	// takes the place of `substituteIdentifier` in the strada transform
	// Visit the expression but not the name, since property access names should not be substituted.
	// Strada's onSubstituteNode only fires for EmitHint.Expression, which excludes the
	// .name of PropertyAccessExpression.
	// While we emit the source map for the node after skipping decorators and modifiers,
	// we need to emit the comments for the original range.
	// While we emit the source map for the node after skipping decorators and modifiers,
	// we need to emit the comments for the original range.
	// visitPropertyNameOfClassElement visits the property name of a class element,
	// for use when emitting property initializers. For a computed property on a node
	// with decorators, a temporary value is stored for later use.
	// Legacy decorators were not supported on class expressions
	/**
	* Transforms a non-decorated class declaration.
	*
	* @param node A ClassDeclaration node.
	* @param name The name of the class.
	 */ //  ${modifiers} class ${name} ${heritageClauses} {
	//      ${members}
	//  }
	/**
	* Transforms a decorated class declaration and appends the resulting statements. If
	* the class requires an alias to avoid issues with double-binding, the alias is returned.
	 */ // When we emit an ES6 class that has a class decorator, we must tailor the
	// emit to certain specific cases.
	//
	// In the simplest case, we emit the class declaration as a let declaration, and
	// evaluate decorators after the close of the class body:
	//
	//  [Example 1]
	//  ---------------------------------------------------------------------
	//  TypeScript                      | Javascript
	//  ---------------------------------------------------------------------
	//  @dec                            | let C = class C {
	//  class C {                       | }
	//  }                               | C = __decorate([dec], C);
	//  ---------------------------------------------------------------------
	//  @dec                            | let C = class C {
	//  export class C {                | }
	//  }                               | C = __decorate([dec], C);
	//                                  | export { C };
	//  ---------------------------------------------------------------------
	//
	// If a class declaration contains a reference to itself *inside* of the class body,
	// this introduces two bindings to the class: One outside of the class body, and one
	// inside of the class body. If we apply decorators as in [Example 1] above, there
	// is the possibility that the decorator `dec` will return a new value for the
	// constructor, which would result in the binding inside of the class no longer
	// pointing to the same reference as the binding outside of the class.
	//
	// As a result, we must instead rewrite all references to the class *inside* of the
	// class body to instead point to a local temporary alias for the class:
	//
	//  [Example 2]
	//  ---------------------------------------------------------------------
	//  TypeScript                      | Javascript
	//  ---------------------------------------------------------------------
	//  @dec                            | let C = C_1 = class C {
	//  class C {                       |   static x() { return C_1.y; }
	//    static x() { return C.y; }    | }
	//    static y = 1;                 | C.y = 1;
	//  }                               | C = C_1 = __decorate([dec], C);
	//                                  | var C_1;
	//  ---------------------------------------------------------------------
	//  @dec                            | let C = class C {
	//  export class C {                |   static x() { return C_1.y; }
	//    static x() { return C.y; }    | }
	//    static y = 1;                 | C.y = 1;
	//  }                               | C = C_1 = __decorate([dec], C);
	//                                  | export { C };
	//                                  | var C_1;
	//  ---------------------------------------------------------------------
	//
	// If a class declaration is the default export of a module, we instead emit
	// the export after the decorated declaration:
	//
	//  [Example 3]
	//  ---------------------------------------------------------------------
	//  TypeScript                      | Javascript
	//  ---------------------------------------------------------------------
	//  @dec                            | let default_1 = class {
	//  export default class {          | }
	//  }                               | default_1 = __decorate([dec], default_1);
	//                                  | export default default_1;
	//  ---------------------------------------------------------------------
	//  @dec                            | let C = class C {
	//  export default class C {        | }
	//  }                               | C = __decorate([dec], C);
	//                                  | export default C;
	//  ---------------------------------------------------------------------
	//
	// If the class declaration is the default export and a reference to itself
	// inside of the class body, we must emit both an alias for the class *and*
	// move the export after the declaration:
	//
	//  [Example 4]
	//  ---------------------------------------------------------------------
	//  TypeScript                      | Javascript
	//  ---------------------------------------------------------------------
	//  @dec                            | let C = class C {
	//  export default class C {        |   static x() { return C_1.y; }
	//    static x() { return C.y; }    | }
	//    static y = 1;                 | C.y = 1;
	//  }                               | C = C_1 = __decorate([dec], C);
	//                                  | export default C;
	//                                  | var C_1;
	//  ---------------------------------------------------------------------
	//
	// When we used to transform to ES5/3 this would be moved inside an IIFE and should reference the name
	// without any block-scoped variable collision handling - but we don't support that anymore, so we always
	// use the local name for the class
	//  ... = class ${name} ${heritageClauses} {
	//      ${members}
	//  }
	// If we're emitting to ES2022 or later then we need to reassign the class alias before
	// static initializers are evaluated.
	//  let ${name} = ${classExpression} where name is either declaredName if the class doesn't contain self-reference
	//                                         or decoratedClassAlias if the class contain self-reference.
	// For PropertyAccessExpression, only check the expression, not the name.
	// The .Name() is a property access name, not a value reference to the class.
	/**
	* Gets a local alias for a class declaration if it is a decorated class with an internal
	* reference to the static side of the class. This is necessary to avoid issues with
	* double-binding semantics for the class name.
	 */ /**
	* Generates a __decorate helper call for a class constructor.
	*
	* @param node The class node.
	 */ /**
	* Generates a __decorate helper call for a class constructor.
	*
	* @param node The class node.
	 */ // Decorator expressions are evaluated outside the class body, so references to the
	// class name should use the original binding, not the class alias. In Strada, this is
	// handled by NodeCheckFlags.ConstructorReference which is only set for identifiers

	// inside the class body. Since Corsa lacks per-node flags, we temporarily pop the
	// enclosing class to prevent alias substitution during decorator expression visiting.
	// When we used to transform to ES5/3 this would be moved inside an IIFE and should reference the name
	// without any block-scoped variable collision handling - but we don't support that anymore, so we always
	// use the local name for the class
	/**
	 * Gets an allDecorators object containing the decorators for the class and the decorators for the
	 * parameters of the constructor of the class.
	 *
	 * @param node The class node.
	 *
	 * @internal
	 */ /**
	 * Gets an allDecorators object containing the decorators for the member and its parameters.
	 *
	 * @param parent The class node that contains the member.
	 * @param member The class member.
	 *
	 * @internal
	 */ /**
	 * Gets an allDecorators object containing the decorators for the accessor and its parameters.
	 *
	 * @param parent The class node that contains the accessor.
	 * @param accessor The class accessor member.
	 */ /**
	 * Gets an array of arrays of decorators for the parameters of a function-like node.
	 * The offset into the result array should correspond to the offset of the parameter.
	 *
	 * @param node The function-like node.
	 */ /**
	* Generates statements used to apply decorators to either the static or instance members
	* of a class.
	*
	* @param node The class node.
	* @param isStatic A value indicating whether to generate statements for static or
	*                 instance members.
	 */ /**
	* Determines whether a class member is either a static or an instance member of a class
	* that is decorated, or has parameters that are decorated.
	*
	* @param member The class member.
	 */ /**
	* Gets either the static or instance members of a class that are decorated, or have
	* parameters that are decorated.
	*
	* @param node The class containing the member.
	* @param isStatic A value indicating whether to retrieve static or instance members of
	*                 the class.
	 */ /**
	* Generates expressions used to apply decorators to either the static or instance members
	* of a class.
	*
	* @param node The class node.
	* @param isStatic A value indicating whether to generate expressions for static or
	*                 instance members.
	 */ /**
	* Generates an expression used to evaluate class element decorators at runtime.
	*
	* @param node The class node that contains the member.
	* @param member The class member.
	 */ // Emit the call to __decorate. Given the following:
	//
	//   class C {
	//     @dec method(@dec2 x) {}
	//     @dec get accessor() {}
	//     @dec prop;
	//   }
	//
	// The emit for a method is:
	//
	//   __decorate([
	//       dec,
	//       __param(0, dec2),
	//       __metadata("design:type", Function),
	//       __metadata("design:paramtypes", [Object]),
	//       __metadata("design:returntype", void 0)
	//   ], C.prototype, "method", null);
	//
	// The emit for an accessor is:
	//
	//   __decorate([
	//       dec
	//   ], C.prototype, "accessor", null);
	//
	// The emit for a property is:
	//
	//   __decorate([
	//       dec
	//   ], C.prototype, "prop");
	//
	// We emit `void 0` here to indicate to `__decorate` that it can invoke `Object.defineProperty` directly, but that it
	// should not invoke `Object.getOwnPropertyDescriptor`.
	// We emit `null` here to indicate to `__decorate` that it can invoke `Object.getOwnPropertyDescriptor` directly.
	// We have this extra argument here so that we can inject an explicit property descriptor at a later date.
	/**
	* Transforms all of the decorators for a declaration into an array of expressions.
	*
	* @param allDecorators An object containing all of the decorators for the declaration.
	 */ // ensure that metadata decorators are last
	/**
	* Transforms a list of decorators into an expression.
	*
	* @param decorator The decorator node.
	 */ast.Handle]ast.Handle
	enclosingClasses []ast.Handle
}

func NewLegacyDecoratorsTransformer(opt *transformers.TransformOptions) *transformers.Transformer {
	tx := &LegacyDecoratorsTransformer{languageVersion: opt.CompilerOptions.GetEmitScriptTarget(), referenceResolver: opt.Resolver}
	return tx.NewTransformer(tx.visit, opt.Context)
}
func (tx *LegacyDecoratorsTransformer) visit(node ast.Handle) ast.Handle {
	if (node.SubtreeFacts()&ast.SubtreeContainsDecorators) == 0 && len(tx.enclosingClasses) == 0 {
		return node
	}
	switch node.Kind {
	case ast.KindIdentifier:
		return tx.visitIdentifier(node)
	case ast.KindPropertyAccessExpression:
		return tx.visitPropertyAccessExpression(node)
	case ast.KindDecorator:
		return ast.Handle{}
	case ast.KindClassDeclaration:
		return tx.visitClassDeclaration(node)
	case ast.KindClassExpression:
		return tx.visitClassExpression(node)
	case ast.KindConstructor:
		return tx.visitConstructorDeclaration(node)
	case ast.KindMethodDeclaration:
		return tx.visitMethodDeclaration(node)
	case ast.KindSetAccessor:
		return tx.visitSetAccessorDeclaration(node)
	case ast.KindGetAccessor:
		return tx.visitGetAccessorDeclaration(node)
	case ast.KindPropertyDeclaration:
		return tx.visitPropertyDeclaration(node)
	case ast.KindParameter:
		return tx.visitParamerDeclaration(node)
	case ast.KindSourceFile:
		tx.classAliases = make(map[ast.Handle]ast.Handle)
		tx.enclosingClasses = nil
		result := tx.Visitor().VisitEachChild(node)
		tx.EmitContext().AddEmitHelper(result, tx.EmitContext().ReadEmitHelpers()...)
		tx.classAliases = nil
		tx.enclosingClasses = nil
		return result
	default:
		return tx.Visitor().VisitEachChild(node)
	}
}
func (tx *LegacyDecoratorsTransformer) visitIdentifier(node ast.Handle) ast.Handle {
	for _, d := range tx.enclosingClasses {
		if _, ok := tx.classAliases[d]; ok && tx.referenceResolver.GetReferencedValueDeclaration(tx.EmitContext().MostOriginal(node)) == tx.EmitContext().MostOriginal(d) {
			return tx.classAliases[d]
		}
	}
	return node
}
func (tx *LegacyDecoratorsTransformer) visitPropertyAccessExpression(node ast.Handle) ast.Handle {
	expression := tx.Visitor().VisitNode(node.Expression())
	if expression != node.Expression() {
		return tx.Factory().UpdatePropertyAccessExpression(node, expression, node.QuestionDotToken(), node.Name(), node.Flags())
	}
	return node
}
func elideNodes(f *printer.NodeFactory, nodes ast.ListRef) ast.ListRef {
	if nodes == 0 {
		return 0
	}
	return f.NewList(nil)
}
func elideModifiers(f *printer.NodeFactory, nodes ast.ListRef) ast.ListRef {
	if nodes == 0 {
		return 0
	}
	return f.NewList(nil)
}
func (tx *LegacyDecoratorsTransformer) finishClassElement(updated ast.Handle, original ast.Handle) ast.Handle {
	if updated != original {
		tx.EmitContext().SetCommentRange(updated, original.Loc())
		tx.EmitContext().SetSourceMapRange(updated, transformers.MoveRangePastModifiers(original))
	}
	return updated
}
func (tx *LegacyDecoratorsTransformer) visitParamerDeclaration(node ast.Handle) ast.Handle {
	updated := tx.Factory().UpdateParameterDeclaration(node, elideModifiers(tx.Factory(), node.Modifiers()), node.DotDotDotToken(), tx.Visitor().VisitNode(node.Name()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Initializer()))
	if updated != node {
		tx.EmitContext().SetCommentRange(updated, node.Loc())
		newLoc := transformers.MoveRangePastModifiers(node)
		updated.SetLoc(newLoc)
		tx.EmitContext().SetSourceMapRange(updated, newLoc)
		tx.EmitContext().SetEmitFlags(updated.Name(), printer.EFNoTrailingSourceMap)
	}
	return updated
}

func (tx *LegacyDecoratorsTransformer) visitPropertyNameOfClassElement(member ast.Handle) ast.Handle {
	name := member.Name()
	if ast.IsComputedPropertyName(name) && ast.HasDecorators(member) {
		expression := tx.Visitor().VisitNode(name.ComputedPropertyNameExpression())
		innerExpression := ast.SkipPartiallyEmittedExpressions(expression)
		if !transformers.IsSimpleInlineableExpression(innerExpression) {
			generatedName := tx.Factory().NewGeneratedNameForNode(name)
			tx.EmitContext().AddVariableDeclaration(generatedName)
			return tx.Factory().UpdateComputedPropertyName(name, tx.Factory().NewAssignmentExpression(generatedName, expression))
		}
	}
	return tx.Visitor().VisitNode(name)
}
func (tx *LegacyDecoratorsTransformer) visitPropertyDeclaration(node ast.Handle) ast.Handle {
	if (node.Flags() & ast.NodeFlagsAmbient) != 0 {
		return ast.Handle{}
	}
	if ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient|ast.ModifierFlagsAbstract) {
		return ast.Handle{}
	}
	return tx.finishClassElement(tx.Factory().UpdatePropertyDeclaration(node, tx.Visitor().VisitModifiers(node.Modifiers()), tx.visitPropertyNameOfClassElement(node), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Initializer())), node)
}
func (tx *LegacyDecoratorsTransformer) visitGetAccessorDeclaration(node ast.Handle) ast.Handle {
	return tx.finishClassElement(tx.Factory().UpdateGetAccessorDeclaration(node, tx.Visitor().VisitModifiers(node.Modifiers()), tx.visitPropertyNameOfClassElement(node), 0, tx.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Body())), node)
}
func (tx *LegacyDecoratorsTransformer) visitSetAccessorDeclaration(node ast.Handle) ast.Handle {
	return tx.finishClassElement(tx.Factory().UpdateSetAccessorDeclaration(node, tx.Visitor().VisitModifiers(node.Modifiers()), tx.visitPropertyNameOfClassElement(node), 0, tx.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Body())), node)
}
func (tx *LegacyDecoratorsTransformer) visitMethodDeclaration(node ast.Handle) ast.Handle {
	return tx.finishClassElement(tx.Factory().UpdateMethodDeclaration(node, tx.Visitor().VisitModifiers(node.Modifiers()), node.AsteriskToken(), tx.visitPropertyNameOfClassElement(node), ast.Handle{}, 0, tx.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Body())), node)
}
func (tx *LegacyDecoratorsTransformer) visitConstructorDeclaration(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateConstructorDeclaration(node, tx.Visitor().VisitModifiers(node.Modifiers()), 0, tx.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Body()))
}
func (tx *LegacyDecoratorsTransformer) visitClassExpression(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateClassExpression(node, tx.Visitor().VisitModifiers(node.Modifiers()), node.Name(), 0, tx.Visitor().VisitNodes(node.HeritageClauses()), tx.Visitor().VisitNodes(node.MemberList()))
}
func (tx *LegacyDecoratorsTransformer) visitClassDeclaration(node ast.Handle) ast.Handle {
	decorated := ast.ClassOrConstructorParameterIsDecorated(true, node)
	if !(decorated || ast.ChildIsDecorated(true, node, ast.Handle{})) {
		return tx.Visitor().VisitEachChild(node)
	}
	if decorated {
		return tx.transformClassDeclarationWithClassDecorators(node, node.Name())
	}
	return tx.transformClassDeclarationWithoutClassDecorators(node, node.Name())
}

func (tx *LegacyDecoratorsTransformer) transformClassDeclarationWithoutClassDecorators(node ast.Handle, name ast.Handle) ast.Handle {
	modifiers := tx.Visitor().VisitModifiers(node.Modifiers())
	heritageClauses := tx.Visitor().VisitNodes(node.HeritageClauses())
	initialMembers := tx.Visitor().VisitNodes(node.MemberList())
	members, decorationStatements := tx.transformDecoratorsOfClassElements(node, initialMembers)
	if name.IsNil() && len(decorationStatements) > 0 {
		name = tx.Factory().NewGeneratedNameForNode(node)
	}
	updated := tx.Factory().UpdateClassDeclaration(node, modifiers, name, 0, heritageClauses, members)
	if len(decorationStatements) == 0 {
		return updated
	}
	return tx.Factory().NewSyntaxList(tx.Factory().NewList(append([]ast.Handle{updated}, decorationStatements...)))
}
func (tx *LegacyDecoratorsTransformer) popEnclosingClass() {
	tx.enclosingClasses = tx.enclosingClasses[:len(tx.enclosingClasses)-1]
}
func (tx *LegacyDecoratorsTransformer) pushEnclosingClass(cls ast.Handle) {
	tx.enclosingClasses = append(tx.enclosingClasses, cls)
}

func (tx *LegacyDecoratorsTransformer) transformClassDeclarationWithClassDecorators(node ast.Handle, name ast.Handle) ast.Handle {
	isExport := ast.HasSyntacticModifier(node, ast.ModifierFlagsExport)
	isDefault := ast.HasSyntacticModifier(node, ast.ModifierFlagsDefault)
	var modifiers ast.ListRef
	if node.Modifiers() != 0 && node.Store().ListLen(node.Modifiers()) > 0 {
		mods := node.Store().ListSlice(node.Modifiers())
		modifierNodes := core.Filter(mods.Slice(), isNotExportOrDefaultOrDecorator)
		if len(modifierNodes) != mods.Len() {
			modifiers = tx.Factory().RelocateList(tx.Factory().NewModifierList(modifierNodes), node.Store().ListLoc(node.Modifiers()))
		} else {
			modifiers = node.Modifiers()
		}
	}
	location := transformers.MoveRangePastModifiers(node)
	classAlias := tx.getClassAliasIfNeeded(node)
	if !classAlias.IsNil() {
		tx.pushEnclosingClass(node)
		defer tx.popEnclosingClass()
	}
	declName := tx.Factory().GetLocalNameEx(node, printer.AssignedNameOptions{AllowComments: false, AllowSourceMaps: true})
	heritageClauses := tx.Visitor().VisitNodes(node.HeritageClauses())
	members := tx.Visitor().VisitNodes(node.MemberList())
	members, decorationStatements := tx.transformDecoratorsOfClassElements(node, members)
	assignClassAliasInStaticBlock := tx.languageVersion >= core.ScriptTargetES2022 && !classAlias.IsNil() && members != 0 && name.Store().ListLen(members) > 0 && name.Store().ListSlice(members).Some(isClassStaticBlockDeclarationOrStaticProperty)
	if assignClassAliasInStaticBlock {
		memberList := []ast.Handle{}
		memberList = append(memberList, tx.Factory().NewClassStaticBlockDeclaration(0, tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{tx.Factory().NewExpressionStatement(tx.Factory().NewAssignmentExpression(classAlias, tx.Factory().NewKeywordExpression(ast.KindThisKeyword)))}), false)))
		memberList = append(memberList, name.Store().ListSlice(members).Slice()...)
		newList := tx.Factory().List(name.Store().ListLoc(members), memberList...)
		members = newList
	}
	exprName := name
	if !name.IsNil() && transformers.IsGeneratedIdentifier(tx.EmitContext(), name) {
		exprName = ast.Handle{}
	}
	classExpression := tx.Factory().NewClassExpression(modifiers, exprName, 0, heritageClauses, members)
	tx.EmitContext().SetOriginal(classExpression, node)
	classExpression.SetLoc(location)
	varInitializer := classExpression
	if !classAlias.IsNil() && !assignClassAliasInStaticBlock {
		varInitializer = tx.Factory().NewAssignmentExpression(classAlias, classExpression)
	}
	varDecl := tx.Factory().NewVariableDeclaration(declName, ast.Handle{}, ast.Handle{}, varInitializer)
	tx.EmitContext().SetOriginal(varDecl, node)
	varDeclList := tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{varDecl}), ast.NodeFlagsLet)
	varStatement := tx.Factory().NewVariableStatement(0, varDeclList)
	tx.EmitContext().SetOriginal(varStatement, node)
	varStatement.SetLoc(location)
	tx.EmitContext().SetCommentRange(varStatement, node.Loc())
	statements := []ast.Handle{varStatement}
	statements = append(statements, decorationStatements...)
	statements = append(statements, tx.getConstructorDecorationStatement(node))
	if isExport {
		var exportStatement ast.Handle
		if isDefault {
			exportStatement = tx.Factory().NewExportDefault(declName)
		} else {
			exportStatement = tx.Factory().NewExternalModuleExport(tx.Factory().GetDeclarationName(node))
		}
		statements = append(statements, exportStatement)
	}
	if len(statements) == 1 {
		return statements[0]
	}
	return tx.Factory().NewSyntaxList(tx.Factory().NewList(statements))
}
func (tx *LegacyDecoratorsTransformer) hasInternalStaticReference(node ast.Handle) bool {
	classNode := tx.EmitContext().MostOriginal(node)
	var isOrContainsStaticSelfReference func(n ast.Handle) bool
	isOrContainsStaticSelfReference = func(n ast.Handle) bool {
		if ast.IsIdentifier(n) && tx.referenceResolver.GetReferencedValueDeclaration(tx.EmitContext().MostOriginal(n)) == classNode {
			return true
		}
		if ast.IsPropertyAccessExpression(n) {
			return isOrContainsStaticSelfReference(n.Expression())
		}
		return n.ForEachChild(isOrContainsStaticSelfReference)
	}
	for _, member := range node.Members() {
		if member.ForEachChild(isOrContainsStaticSelfReference) {
			return true
		}
	}
	return false
}

func (tx *LegacyDecoratorsTransformer) getClassAliasIfNeeded(node ast.Handle) ast.Handle {
	if !tx.hasInternalStaticReference(node) {
		return ast.Handle{}
	}
	nameText := "default"
	if !node.Name().IsNil() && !transformers.IsGeneratedIdentifier(tx.EmitContext(), node.Name()) {
		nameText = node.Name().Text()
	}
	classAlias := tx.Factory().NewUniqueName(nameText)
	tx.EmitContext().AddVariableDeclaration(classAlias)
	tx.classAliases[node] = classAlias
	return classAlias
}

func (tx *LegacyDecoratorsTransformer) getConstructorDecorationStatement(node ast.Handle) ast.Handle {
	expression := tx.generateConstructorDecorationExpression(node)
	if !expression.IsNil() {
		result := tx.Factory().NewExpressionStatement(expression)
		tx.EmitContext().SetOriginal(result, node)
		return result
	}
	return ast.Handle{}
}

func (tx *LegacyDecoratorsTransformer) generateConstructorDecorationExpression(node ast.Handle) ast.Handle {
	allDecorators := getAllDecoratorsOfClass(node, true)
	hasAlias := len(tx.enclosingClasses) > 0 && tx.enclosingClasses[len(tx.enclosingClasses)-1] == node
	if hasAlias {
		tx.popEnclosingClass()
	}
	decoratorExpressions := tx.transformAllDecoratorsOfDeclaration(allDecorators)
	if hasAlias {
		tx.pushEnclosingClass(node)
	}
	if len(decoratorExpressions) == 0 {
		return ast.Handle{}
	}
	var classAlias ast.Handle
	if tx.classAliases != nil {
		classAlias, _ = tx.classAliases[node]
	}
	localName := tx.Factory().GetDeclarationNameEx(node, printer.NameOptions{AllowComments: false, AllowSourceMaps: true})
	decorate := tx.Factory().NewDecorateHelper(decoratorExpressions, localName, ast.Handle{}, ast.Handle{})
	assignmentTarget := decorate
	if !classAlias.IsNil() {
		assignmentTarget = tx.Factory().NewAssignmentExpression(classAlias, decorate)
	}
	expression := tx.Factory().NewAssignmentExpression(localName, assignmentTarget)
	tx.EmitContext().SetEmitFlags(expression, printer.EFNoComments)
	tx.EmitContext().SetSourceMapRange(expression, transformers.MoveRangePastModifiers(node))
	return expression
}
func isClassStaticBlockDeclarationOrStaticProperty(node ast.Handle) bool {
	return ast.IsClassStaticBlockDeclaration(node) || (ast.IsPropertyDeclaration(node) && ast.HasStaticModifier(node))
}
func isNotExportOrDefaultOrDecorator(node ast.Handle) bool {
	return !(ast.IsDecorator(node) || node.Kind == ast.KindExportKeyword || node.Kind == ast.KindDefaultKeyword)
}
func decoratorContainsPrivateIdentifierInExpression(decorator ast.Handle) bool {
	return (decorator.SubtreeFacts() & ast.SubtreeContainsPrivateIdentifierInExpression) != 0
}
func parameterDecoratorsContainPrivateIdentifierInExpression(parameterDecorators []ast.Handle) bool {
	return core.Some(parameterDecorators, decoratorContainsPrivateIdentifierInExpression)
}
func hasClassElementWithDecoratorContainingPrivateIdentifierInExpression(node ast.Handle) bool {
	if node.Members() == nil || len(node.Members()) == 0 {
		return false
	}
	for _, member := range node.Members() {
		if !ast.CanHaveDecorators(member) {
			continue
		}
		allDecorators := getAllDecoratorsOfClassElement(member, node, true)
		if allDecorators == nil {
			continue
		}
		if core.Some(allDecorators.decorators, decoratorContainsPrivateIdentifierInExpression) {
			return true
		}
		if core.Some(allDecorators.parameters, parameterDecoratorsContainPrivateIdentifierInExpression) {
			return true
		}
	}
	return false
}

type allDecorators struct {
	decorators []ast.Handle
	parameters [][]ast.Handle
}

func getAllDecoratorsOfClass(node ast.Handle, useLegacyDecorators bool) *allDecorators {
	decorators := node.Decorators()
	var parameters [][]ast.Handle
	if useLegacyDecorators {
		parameters = getDecoratorsOfParameters(ast.GetFirstConstructorWithBody(node))
	}
	if len(decorators) == 0 && len(parameters) == 0 {
		return nil
	}
	return &allDecorators{decorators: decorators, parameters: parameters}
}

func getAllDecoratorsOfClassElement(member ast.Handle, parent ast.Handle, useLegacyDecorators bool) *allDecorators {
	switch member.Kind {
	case ast.KindGetAccessor, ast.KindSetAccessor:
		if !useLegacyDecorators {
			return getAllDecoratorsOfMethod(member, false)
		}
		return getAllDecoratorsOfAccessors(member, parent, true)
	case ast.KindMethodDeclaration:
		return getAllDecoratorsOfMethod(member, useLegacyDecorators)
	case ast.KindPropertyDeclaration:
		return getAllDecoratorsOfProperty(member)
	default:
		return nil
	}
}

func getAllDecoratorsOfAccessors(accessor ast.Handle, parent ast.Handle, useLegacyDecorators bool) *allDecorators {
	if accessor.Body().IsNil() {
		return nil
	}
	decls := ast.GetAllAccessorDeclarations(parent.Members(), accessor)
	var firstAccessorWithDecorators ast.Handle
	if ast.HasDecorators(decls.FirstAccessor) {
		firstAccessorWithDecorators = decls.FirstAccessor
	} else if !decls.SecondAccessor.IsNil() && ast.HasDecorators(decls.SecondAccessor) {
		firstAccessorWithDecorators = decls.SecondAccessor
	}
	if firstAccessorWithDecorators.IsNil() || accessor != firstAccessorWithDecorators {
		return nil
	}
	decorators := firstAccessorWithDecorators.Decorators()
	var parameters [][]ast.Handle
	if useLegacyDecorators && !decls.SetAccessor.IsNil() {
		parameters = getDecoratorsOfParameters(decls.SetAccessor)
	}
	if len(decorators) == 0 && len(parameters) == 0 {
		return nil
	}
	return &allDecorators{decorators: decorators, parameters: parameters}
}
func getAllDecoratorsOfProperty(property ast.Handle) *allDecorators {
	decorators := property.Decorators()
	if len(decorators) == 0 {
		return nil
	}
	return &allDecorators{decorators: decorators}
}
func getAllDecoratorsOfMethod(method ast.Handle, useLegacyDecorators bool) *allDecorators {
	if method.Body().IsNil() {
		return nil
	}
	decorators := method.Decorators()
	var parameters [][]ast.Handle
	if useLegacyDecorators {
		parameters = getDecoratorsOfParameters(method)
	}
	if len(decorators) == 0 && len(parameters) == 0 {
		return nil
	}
	return &allDecorators{decorators: decorators, parameters: parameters}
}

func getDecoratorsOfParameters(node ast.Handle) [][]ast.Handle {
	var decorators [][]ast.Handle
	if !node.IsNil() {
		parameters := node.Parameters()
		firstParameterIsThis := len(parameters) > 0 && ast.IsThisParameter(parameters[0])
		firstParameterOffset := 0
		numParameters := len(parameters)
		if firstParameterIsThis {
			firstParameterOffset = 1
			numParameters = numParameters - 1
		}
		for i := range numParameters {
			p := parameters[i+firstParameterOffset]
			if len(decorators) > 0 || ast.HasDecorators(p) {
				if len(decorators) == 0 {
					decorators = make([][]ast.Handle, numParameters)
				}
				decorators[i] = p.Decorators()
			}
		}
	}
	return decorators
}
func (tx *LegacyDecoratorsTransformer) transformDecoratorsOfClassElements(node ast.Handle, members ast.ListRef) (ast.ListRef, []ast.Handle) {
	var decorationStatements []ast.Handle
	decorationStatements = append(decorationStatements, tx.getClassElementDecorationStatements(node, false)...)
	decorationStatements = append(decorationStatements, tx.getClassElementDecorationStatements(node, true)...)
	if hasClassElementWithDecoratorContainingPrivateIdentifierInExpression(node) {
		var memberNodes []ast.Handle
		if members != 0 {
			memberNodes = tx.EmitContext().StoreFile().ParseStore().ListSlice(members).Slice()
		}
		members = tx.Factory().NewList(append(append([]ast.Handle{}, memberNodes...), tx.Factory().NewClassStaticBlockDeclaration(0, tx.Factory().NewBlock(tx.Factory().NewList(decorationStatements), true))))
		decorationStatements = nil
	}
	return members, decorationStatements
}

func (tx *LegacyDecoratorsTransformer) getClassElementDecorationStatements(node ast.Handle, isStatic bool) []ast.Handle {
	exprs := tx.generateClassElementDecorationExpressions(node, isStatic)
	var statements []ast.Handle
	for _, e := range exprs {
		statements = append(statements, tx.Factory().NewExpressionStatement(e))
	}
	return statements
}

func isDecoratedClassElement(member ast.Handle, isStaticElement bool, parent ast.Handle) bool {
	return isStaticElement == ast.IsStatic(member) && ast.NodeOrChildIsDecorated(true, member, parent, ast.Handle{})
}

func getDecoratedClassElements(node ast.Handle, isStatic bool) []ast.Handle {
	if node.Members() == nil || len(node.Members()) == 0 {
		return nil
	}
	var members []ast.Handle
	for _, member := range node.Members() {
		if isDecoratedClassElement(member, isStatic, node) {
			members = append(members, member)
		}
	}
	return members
}

func (tx *LegacyDecoratorsTransformer) generateClassElementDecorationExpressions(node ast.Handle, isStatic bool) []ast.Handle {
	members := getDecoratedClassElements(node, isStatic)
	var expressions []ast.Handle
	for _, member := range members {
		expr := tx.generateClassElementDecorationExpression(node, member)
		if !expr.IsNil() {
			expressions = append(expressions, expr)
		}
	}
	return expressions
}

func (tx *LegacyDecoratorsTransformer) generateClassElementDecorationExpression(node ast.Handle, member ast.Handle) ast.Handle {
	allDecorators := getAllDecoratorsOfClassElement(member, node, true)
	decoratorExpressions := tx.transformAllDecoratorsOfDeclaration(allDecorators)
	if len(decoratorExpressions) == 0 {
		return ast.Handle{}
	}
	prefix := tx.getClassMemberPrefix(node, member)
	memberName := tx.getExpressionForPropertyName(member, member.Flags()&ast.NodeFlagsAmbient == 0)
	var descriptor ast.Handle
	if ast.IsPropertyDeclaration(member) && !ast.HasAccessorModifier(member) {
		descriptor = tx.Factory().NewVoidZeroExpression()
	} else {
		descriptor = tx.Factory().NewKeywordExpression(ast.KindNullKeyword)
	}
	helper := tx.Factory().NewDecorateHelper(decoratorExpressions, prefix, memberName, descriptor)
	tx.EmitContext().SetEmitFlags(helper, printer.EFNoComments)
	tx.EmitContext().SetSourceMapRange(helper, transformers.MoveRangePastModifiers(member))
	return helper
}
func (tx *LegacyDecoratorsTransformer) isSyntheticMetadataDecorator(node ast.Handle) bool {
	return tx.EmitContext().IsCallToHelper(node.Expression(), "__metadata")
}

func (tx *LegacyDecoratorsTransformer) transformAllDecoratorsOfDeclaration(allDecorators *allDecorators) []ast.Handle {
	if allDecorators == nil {
		return nil
	}
	mm := collections.GroupBy(allDecorators.decorators, tx.isSyntheticMetadataDecorator)
	metadata := mm.Get(true)
	decorators := mm.Get(false)
	var decoratorExpressions []ast.Handle
	decoratorExpressions = append(decoratorExpressions, tx.transformDecorators(decorators)...)
	decoratorExpressions = append(decoratorExpressions, tx.transformDecoratorsOfParameters(allDecorators.parameters)...)
	decoratorExpressions = append(decoratorExpressions, tx.transformDecorators(metadata)...)
	return decoratorExpressions
}
func (tx *LegacyDecoratorsTransformer) transformDecoratorsOfParameters(parameters [][]ast.Handle) []ast.Handle {
	var results []ast.Handle
	for i, decorators := range parameters {
		if len(decorators) > 0 {
			for _, decorator := range decorators {
				helper := tx.Factory().NewParamHelper(tx.Visitor().VisitNode(decorator.Expression()), i, decorator.Expression().Loc())
				tx.EmitContext().SetEmitFlags(helper, printer.EFNoComments)
				results = append(results, helper)
			}
		}
	}
	return results
}

func (tx *LegacyDecoratorsTransformer) transformDecorators(decorators []ast.Handle) []ast.Handle {
	var results []ast.Handle
	for _, d := range decorators {
		results = append(results, tx.Visitor().VisitNode(d.Expression()))
	}
	return results
}
func (tx *LegacyDecoratorsTransformer) getClassMemberPrefix(node ast.Handle, member ast.Handle) ast.Handle {
	if ast.IsStatic(member) {
		return tx.Factory().GetDeclarationName(node)
	}
	return tx.getClassPrototype(node)
}
func (tx *LegacyDecoratorsTransformer) getClassPrototype(node ast.Handle) ast.Handle {
	return tx.Factory().NewPropertyAccessExpression(tx.Factory().GetDeclarationName(node), ast.Handle{}, tx.Factory().NewIdentifier("prototype"), ast.NodeFlagsNone)
}
func (tx *LegacyDecoratorsTransformer) getExpressionForPropertyName(member ast.Handle, generateNameForComputedPropertyName bool) ast.Handle {
	name := member.Name()
	if ast.IsPrivateIdentifier(name) {
		return tx.Factory().NewIdentifier("")
	} else if ast.IsComputedPropertyName(name) {
		if generateNameForComputedPropertyName && !transformers.IsSimpleInlineableExpression(name.ComputedPropertyNameExpression()) {
			return tx.Factory().NewGeneratedNameForNode(name)
		}
		return name.ComputedPropertyNameExpression()
	} else if ast.IsIdentifier(name) {
		return tx.Factory().NewStringLiteral(name.Text(), ast.TokenFlagsNone)
	} else {
		return tx.Factory().DeepCloneNode(name)
	}
}
