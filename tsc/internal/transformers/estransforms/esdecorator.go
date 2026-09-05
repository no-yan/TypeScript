package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type lexicalEntryKind int

const (
	lexicalEntryKindClass lexicalEntryKind = iota
	lexicalEntryKindClassElement
	lexicalEntryKindName
	lexicalEntryKindOther
)

type lexicalEntry struct {
	kind                    lexicalEntryKind
	next                    *lexicalEntry
	classInfoData           *classInfo
	savedPendingExpressions []ast.Handle
	classThisData           ast.Handle
	classSuperData          ast.Handle
	depth                   int
}

type memberInfo struct {
	memberDecoratorsName        ast.Handle
	memberInitializersName      ast.Handle
	memberExtraInitializersName ast.Handle
	memberDescriptorName        ast.Handle
}

type classInfo struct {
	class                                 ast.Handle
	classDecoratorsName                   ast.Handle
	classDescriptorName                   ast.Handle
	classExtraInitializersName            ast.Handle
	classThis                             ast.Handle
	classSuper                            ast.Handle
	metadataReference                     ast.Handle
	memberInfos                           collections.OrderedMap[ast.Handle, *memberInfo]
	instanceMethodExtraInitializersName   ast.Handle
	staticMethodExtraInitializersName     ast.Handle
	staticNonFieldDecorationStatements    []ast.Handle
	nonStaticNonFieldDecorationStatements []ast.Handle
	staticFieldDecorationStatements       []ast.Handle
	nonStaticFieldDecorationStatements    []ast.Handle
	hasStaticInitializers                 bool
	hasNonAmbientInstanceFields           bool
	hasStaticPrivateClassElements         bool
	pendingStaticInitializers             []ast.Handle
	pendingInstanceInitializers           []ast.Handle
}
type esDecoratorTransformer struct {
	transformers.Transformer
	compilerOptions                            *core.CompilerOptions
	top                                        *lexicalEntry
	classInfoStack                             *classInfo
	classThis                                  ast.Handle
	classSuper                                 ast.Handle
	pendingExpressions                         []ast.Handle
	outerThis                                  ast.Handle
	shouldTransformPrivateStaticElementsInFile bool
	outerThisVisitor                           *ast.HandleVisitor
	discardedVisitor                           *ast.HandleVisitor
	modifierVisitor                            *ast.HandleVisitor
	exportStrippingModifierVisitor             *ast.HandleVisitor
	classElementVisitor                        *ast.HandleVisitor
	nonConstructorClassElementVisitor          *ast.HandleVisitor
	constructorClassElementVisitor             *ast.HandleVisitor
	arrayAssignmentVisitor                     *ast.HandleVisitor
	objectAssignmentVisitor                    *ast.HandleVisitor
	staticOnlyModifierVisitor                  *ast.HandleVisitor
	asyncOnlyModifierVisitor                   *ast.HandleVisitor
	accessorStrippingModifierVisitor           *ast.HandleVisitor
}

func newESDecoratorTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	if opts.CompilerOptions.ExperimentalDecorators.IsTrue() || (opts.CompilerOptions.GetEmitScriptTarget() >= core.ScriptTargetESNext && opts.CompilerOptions.GetUseDefineForClassFields()) {
		return nil
	}
	tx := &esDecoratorTransformer{compilerOptions: opts.CompilerOptions}
	result := tx.NewTransformer(tx.visit, opts.Context)
	ec := tx.EmitContext()
	tx.outerThisVisitor = ec.NewNodeVisitor(tx.outerThisVisit)
	tx.discardedVisitor = ec.NewNodeVisitor(tx.discardedValueVisit)
	tx.modifierVisitor = ec.NewNodeVisitor(tx.modifierVisitorVisit)
	tx.exportStrippingModifierVisitor = ec.NewNodeVisitor(tx.exportStrippingModifierVisit)
	tx.classElementVisitor = ec.NewNodeVisitor(tx.classElementVisitorVisit)
	tx.nonConstructorClassElementVisitor = ec.NewNodeVisitor(tx.nonConstructorClassElementVisit)
	tx.constructorClassElementVisitor = ec.NewNodeVisitor(tx.constructorClassElementVisit)
	tx.arrayAssignmentVisitor = ec.NewNodeVisitor(tx.visitArrayAssignmentElement)
	tx.objectAssignmentVisitor = ec.NewNodeVisitor(tx.visitObjectAssignmentElement)
	tx.staticOnlyModifierVisitor = ec.NewNodeVisitor(func(node ast.Handle) ast.Handle {
		if node.Kind == ast.KindStaticKeyword {
			return node
		}
		return ast.Handle{}
	})
	tx.asyncOnlyModifierVisitor = ec.NewNodeVisitor(func(node ast.Handle) ast.Handle {
		if node.Kind == ast.KindAsyncKeyword {
			return node
		}
		return ast.Handle{}
	})
	tx.accessorStrippingModifierVisitor = ec.NewNodeVisitor(func(node ast.Handle) ast.Handle {
		if node.Kind == ast.KindAccessorKeyword {
			return ast.Handle{}
		}
		return node
	})
	return result
}
func (tx *esDecoratorTransformer) updateState() {
	tx.classInfoStack = nil
	tx.classThis = ast.Handle{}
	tx.classSuper = ast.Handle{}
	if tx.top == nil {
		return
	}
	switch tx.top.kind {
	case lexicalEntryKindClass:
		tx.classInfoStack = tx.top.classInfoData
	case lexicalEntryKindClassElement:
		tx.classInfoStack = tx.top.next.classInfoData
		tx.classThis = tx.top.classThisData
		tx.classSuper = tx.top.classSuperData
	case lexicalEntryKindName:
		grandparent := tx.top.next.next.next
		if grandparent != nil && grandparent.kind == lexicalEntryKindClassElement {
			tx.classInfoStack = grandparent.next.classInfoData
			tx.classThis = grandparent.classThisData
			tx.classSuper = grandparent.classSuperData
		}
	}
}
func (tx *esDecoratorTransformer) enterClass(ci *classInfo) {
	tx.top = &lexicalEntry{kind: lexicalEntryKindClass, next: tx.top, classInfoData: ci, savedPendingExpressions: tx.pendingExpressions}
	tx.pendingExpressions = nil
	tx.updateState()
}
func (tx *esDecoratorTransformer) exitClass() {
	debug.Assert(tx.top != nil && tx.top.kind == lexicalEntryKindClass, "Incorrect value for top.kind. Expected top.kind to be 'class' but got '", tx.top.kind, "' instead.")
	tx.pendingExpressions = tx.top.savedPendingExpressions
	tx.top = tx.top.next
	tx.updateState()
}
func (tx *esDecoratorTransformer) enterClassElement(node ast.Handle) {
	debug.Assert(tx.top != nil && tx.top.kind == lexicalEntryKindClass, "Incorrect value for top.kind. Expected top.kind to be 'class' but got '", tx.top.kind, "' instead.")
	tx.top = &lexicalEntry{kind: lexicalEntryKindClassElement, next: tx.top}
	if ast.IsClassStaticBlockDeclaration(node) || ast.IsPropertyDeclaration(node) && ast.HasStaticModifier(node) {
		if tx.top.next.classInfoData != nil {
			tx.top.classThisData = tx.top.next.classInfoData.classThis
			tx.top.classSuperData = tx.top.next.classInfoData.classSuper
		}
	}
	tx.updateState()
}
func (tx *esDecoratorTransformer) exitClassElement() {
	debug.Assert(tx.top != nil && tx.top.kind == lexicalEntryKindClassElement, "Incorrect value for top.kind. Expected top.kind to be 'class-element' but got '", tx.top.kind, "' instead.")
	debug.Assert(tx.top.next != nil && tx.top.next.kind == lexicalEntryKindClass, "Incorrect value for top.next.kind. Expected top.next.kind to be 'class' but got '", tx.top.next.kind, "' instead.")
	tx.top = tx.top.next
	tx.updateState()
}
func (tx *esDecoratorTransformer) enterName() {
	debug.Assert(tx.top != nil && tx.top.kind == lexicalEntryKindClassElement, "Incorrect value for top.kind. Expected top.kind to be 'class-element' but got '", tx.top.kind, "' instead.")
	tx.top = &lexicalEntry{kind: lexicalEntryKindName, next: tx.top}
	tx.updateState()
}
func (tx *esDecoratorTransformer) exitName() {
	debug.Assert(tx.top != nil && tx.top.kind == lexicalEntryKindName, "Incorrect value for top.kind. Expected top.kind to be 'name' but got '", tx.top.kind, "' instead.")
	tx.top = tx.top.next
	tx.updateState()
}
func (tx *esDecoratorTransformer) enterOther() {
	if tx.top != nil && tx.top.kind == lexicalEntryKindOther {
		debug.Assert(len(tx.pendingExpressions) == 0)
		tx.top.depth++
	} else {
		tx.top = &lexicalEntry{kind: lexicalEntryKindOther, next: tx.top, savedPendingExpressions: tx.pendingExpressions}
		tx.pendingExpressions = nil
		tx.updateState()
	}
}
func (tx *esDecoratorTransformer) exitOther() {
	debug.Assert(tx.top != nil && tx.top.kind == lexicalEntryKindOther, "Incorrect value for top.kind. Expected top.kind to be 'other' but got '", tx.top.kind, "' instead.")
	if tx.top.depth > 0 {
		debug.Assert(len(tx.pendingExpressions) == 0)
		tx.top.depth--
	} else {
		tx.pendingExpressions = tx.top.savedPendingExpressions
		tx.top = tx.top.next
		tx.updateState()
	}
}
func (tx *esDecoratorTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	tx.top = nil
	tx.shouldTransformPrivateStaticElementsInFile = false
	visited := tx.Visitor().VisitEachChild(node)
	tx.EmitContext().AddEmitHelper(visited, tx.EmitContext().ReadEmitHelpers()...)
	if tx.shouldTransformPrivateStaticElementsInFile {
		tx.EmitContext().AddEmitFlags(visited, printer.EFTransformPrivateStaticElements)
		tx.shouldTransformPrivateStaticElementsInFile = false
	}
	return visited
}
func (tx *esDecoratorTransformer) outerThisVisit(n ast.Handle) ast.Handle {
	if n.SubtreeFacts()&ast.SubtreeContainsLexicalThis == 0 && n.Kind != ast.KindThisKeyword {
		return n
	}
	if n.Kind == ast.KindThisKeyword {
		if tx.outerThis.IsNil() {
			tx.outerThis = tx.Factory().NewUniqueNameEx("_outerThis", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
		}
		return tx.outerThis
	}
	return tx.outerThisVisitor.VisitEachChild(n)
}
func (tx *esDecoratorTransformer) shouldVisitNode(node ast.Handle) bool {
	return node.SubtreeFacts()&ast.SubtreeContainsDecorators != 0 || (!tx.classThis.IsNil() && node.SubtreeFacts()&ast.SubtreeContainsLexicalThis != 0) || (!tx.classThis.IsNil() && !tx.classSuper.IsNil() && node.SubtreeFacts()&ast.SubtreeContainsLexicalSuper != 0)
}
func (tx *esDecoratorTransformer) visit(node ast.Handle) ast.Handle {
	if node.Kind == ast.KindSourceFile {
		return tx.visitSourceFile(node)
	}
	if !tx.shouldVisitNode(node) {
		return node
	}
	switch node.Kind {
	case ast.KindDecorator:
		return ast.Handle{}
	case ast.KindClassDeclaration:
		return tx.visitClassDeclaration(node)
	case ast.KindClassExpression:
		return tx.visitClassExpression(node)
	case ast.KindConstructor, ast.KindPropertyDeclaration, ast.KindClassStaticBlockDeclaration:
		debug.Fail("Not supported outside of a class. Use 'classElementVisitor' instead.")
		return ast.Handle{}
	case ast.KindParameter:
		return tx.visitParameterDeclaration(node)
	case ast.KindBinaryExpression:
		return tx.visitBinaryExpression(node, false)
	case ast.KindPropertyAssignment, ast.KindVariableDeclaration, ast.KindBindingElement:
		return tx.visitNamedEvaluationSite(node, node.Initializer())
	case ast.KindExportAssignment:
		return tx.visitExportAssignment(node)
	case ast.KindThisKeyword:
		return tx.visitThisExpression(node)
	case ast.KindForStatement:
		return tx.visitForStatement(node)
	case ast.KindExpressionStatement:
		return tx.visitExpressionStatement(node)
	case ast.KindParenthesizedExpression:
		return tx.visitParenthesizedExpression(node, false)
	case ast.KindPartiallyEmittedExpression:
		return tx.visitPartiallyEmittedExpression(node, false)
	case ast.KindCallExpression:
		return tx.visitCallExpression(node)
	case ast.KindTaggedTemplateExpression:
		return tx.visitTaggedTemplateExpression(node)
	case ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression:
		return tx.visitPreOrPostfixUnaryExpression(node, false)
	case ast.KindPropertyAccessExpression:
		return tx.visitPropertyAccessExpression(node)
	case ast.KindElementAccessExpression:
		return tx.visitElementAccessExpression(node)
	case ast.KindComputedPropertyName:
		return tx.visitComputedPropertyName(node)
	case ast.KindMethodDeclaration, ast.KindSetAccessor, ast.KindGetAccessor, ast.KindFunctionExpression, ast.KindFunctionDeclaration:
		tx.enterOther()
		result := tx.Visitor().VisitEachChild(node)
		tx.exitOther()
		return result
	default:
		return tx.Visitor().VisitEachChild(node)
	}
}
func (tx *esDecoratorTransformer) modifierVisitorVisit(node ast.Handle) ast.Handle {
	if node.Kind == ast.KindDecorator {
		return ast.Handle{}
	}
	return node
}
func (tx *esDecoratorTransformer) classElementVisitorVisit(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindConstructor:
		return tx.visitConstructorDeclaration(node)
	case ast.KindMethodDeclaration:
		return tx.visitMethodDeclaration(node)
	case ast.KindGetAccessor:
		return tx.visitGetAccessorDeclaration(node)
	case ast.KindSetAccessor:
		return tx.visitSetAccessorDeclaration(node)
	case ast.KindPropertyDeclaration:
		return tx.visitPropertyDeclaration(node)
	case ast.KindClassStaticBlockDeclaration:
		return tx.visitClassStaticBlockDeclaration(node)
	default:
		return tx.visit(node)
	}
}
func (tx *esDecoratorTransformer) discardedValueVisit(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression:
		return tx.visitPreOrPostfixUnaryExpression(node, true)
	case ast.KindBinaryExpression:
		return tx.visitBinaryExpression(node, true)
	case ast.KindParenthesizedExpression:
		return tx.visitParenthesizedExpression(node, true)
	case ast.KindPartiallyEmittedExpression:
		return tx.visitPartiallyEmittedExpression(node, true)
	default:
		return tx.visit(node)
	}
}
func (tx *esDecoratorTransformer) nonConstructorClassElementVisit(node ast.Handle) ast.Handle {
	if ast.IsConstructorDeclaration(node) {
		return node
	}
	return tx.classElementVisitorVisit(node)
}
func (tx *esDecoratorTransformer) constructorClassElementVisit(node ast.Handle) ast.Handle {
	if ast.IsConstructorDeclaration(node) {
		return tx.classElementVisitorVisit(node)
	}
	return node
}
func (tx *esDecoratorTransformer) exportStrippingModifierVisit(node ast.Handle) ast.Handle {
	if node.Kind == ast.KindExportKeyword {
		return ast.Handle{}
	}
	return tx.modifierVisitorVisit(node)
}
func getHelperVariableName(ec *printer.EmitContext, node ast.Handle) string {
	name := node.Name()
	declarationName := ""
	switch {
	case !name.IsNil() && ast.IsIdentifier(name) && !transformers.IsGeneratedIdentifier(ec, name):
		declarationName = name.Text()
	case !name.IsNil() && ast.IsPrivateIdentifier(name) && !ec.HasAutoGenerateInfo(name):
		if text := name.Text(); len(text) > 1 {
			declarationName = text[1:]
		}
	case !name.IsNil() && ast.IsStringLiteral(name) && scanner.IsIdentifierText(name.Text(), core.LanguageVariantStandard):
		declarationName = name.Text()
	case ast.IsClassLike(node):
		declarationName = "class"
	default:
		declarationName = "member"
	}
	if ast.IsGetAccessorDeclaration(node) {
		declarationName = "get_" + declarationName
	}
	if ast.IsSetAccessorDeclaration(node) {
		declarationName = "set_" + declarationName
	}
	if !name.IsNil() && ast.IsPrivateIdentifier(name) {
		declarationName = "private_" + declarationName
	}
	if ast.IsStatic(node) {
		declarationName = "static_" + declarationName
	}
	return "_" + declarationName
}
func (tx *esDecoratorTransformer) createHelperVariable(node ast.Handle, suffix string) ast.Handle {
	return tx.Factory().NewUniqueNameEx(getHelperVariableName(tx.EmitContext(), node)+"_"+suffix, printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsReservedInNestedScopes})
}
func (tx *esDecoratorTransformer) createLet(name ast.Handle, initializer ast.Handle) ast.Handle {
	return tx.Factory().NewVariableStatement(0, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableDeclaration(name, ast.Handle{}, ast.Handle{}, initializer)}), ast.NodeFlagsLet))
}
func (tx *esDecoratorTransformer) createClassInfo(node ast.Handle) *classInfo {
	f := tx.Factory()
	ci := &classInfo{class: node, metadataReference: f.NewUniqueNameEx("_metadata", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})}
	if ast.NodeIsDecorated(false, node, ast.Handle{}, ast.Handle{}) {
		needsUniqueClassThis := core.Some(node.Members(), func(member ast.Handle) bool {
			return (ast.IsPrivateIdentifierClassElementDeclaration(member) || ast.IsAutoAccessorPropertyDeclaration(member)) && ast.HasStaticModifier(member)
		})
		var flags printer.GeneratedIdentifierFlags = printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel
		if needsUniqueClassThis {
			flags = printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsReservedInNestedScopes
		}
		ci.classThis = f.NewUniqueNameEx("_classThis", printer.AutoGenerateOptions{Flags: flags})
	}
	for _, member := range node.Members() {
		if ast.IsMethodOrAccessor(member) && ast.NodeOrChildIsDecorated(false, member, node, ast.Handle{}) {
			if ast.HasStaticModifier(member) {
				if ci.staticMethodExtraInitializersName.IsNil() {
					ci.staticMethodExtraInitializersName = f.NewUniqueNameEx("_staticExtraInitializers", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
					var renamedClassThis ast.Handle
					if !ci.classThis.IsNil() {
						renamedClassThis = ci.classThis
					} else {
						renamedClassThis = f.NewThisExpression()
					}
					initializer := f.NewRunInitializersHelper(renamedClassThis, ci.staticMethodExtraInitializersName, ast.Handle{})
					nameRange := node.Name()
					if !nameRange.IsNil() {
						tx.EmitContext().SetSourceMapRange(initializer, nameRange.Loc())
					} else {
						tx.EmitContext().SetSourceMapRange(initializer, transformers.MoveRangePastDecorators(node))
					}
					ci.pendingStaticInitializers = append(ci.pendingStaticInitializers, initializer)
				}
			} else {
				if ci.instanceMethodExtraInitializersName.IsNil() {
					ci.instanceMethodExtraInitializersName = f.NewUniqueNameEx("_instanceExtraInitializers", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
					initializer := f.NewRunInitializersHelper(f.NewThisExpression(), ci.instanceMethodExtraInitializersName, ast.Handle{})
					nameRange := node.Name()
					if !nameRange.IsNil() {
						tx.EmitContext().SetSourceMapRange(initializer, nameRange.Loc())
					} else {
						tx.EmitContext().SetSourceMapRange(initializer, transformers.MoveRangePastDecorators(node))
					}
					ci.pendingInstanceInitializers = append(ci.pendingInstanceInitializers, initializer)
				}
			}
		}
		if ast.IsClassStaticBlockDeclaration(member) {
			if !isClassNamedEvaluationHelperBlock(tx.EmitContext(), member) {
				ci.hasStaticInitializers = true
			}
		} else if ast.IsPropertyDeclaration(member) {
			if ast.HasStaticModifier(member) {
				ci.hasStaticInitializers = ci.hasStaticInitializers || !member.Initializer().IsNil() || ast.HasDecorators(member)
			} else {
				ci.hasNonAmbientInstanceFields = ci.hasNonAmbientInstanceFields || !ast.HasSyntacticModifier(member, ast.ModifierFlagsAmbient)
			}
		}
		if (ast.IsPrivateIdentifierClassElementDeclaration(member) || ast.IsAutoAccessorPropertyDeclaration(member)) && ast.HasStaticModifier(member) {
			ci.hasStaticPrivateClassElements = true
		}
		if !ci.staticMethodExtraInitializersName.IsNil() && !ci.instanceMethodExtraInitializersName.IsNil() && ci.hasStaticInitializers && ci.hasNonAmbientInstanceFields && ci.hasStaticPrivateClassElements {
			break
		}
	}
	return ci
}
func (tx *esDecoratorTransformer) transformClassLike(node ast.Handle) ast.Handle {
	f := tx.Factory()
	ec := tx.EmitContext()
	ec.StartVariableEnvironment()
	if !classHasDeclaredOrExplicitlyAssignedName(ec, node) && ast.ClassOrConstructorParameterIsDecorated(false, node) {
		node = injectClassNamedEvaluationHelperBlockIfMissing(ec, node, f.NewStringLiteral("", 0), ast.Handle{})
	}
	classReference := f.GetLocalNameEx(node, printer.AssignedNameOptions{})
	ci := tx.createClassInfo(node)
	classDefinitionStatements := []ast.Handle{}
	var leadingBlockStatements []ast.Handle
	var trailingBlockStatements []ast.Handle
	var syntheticConstructor ast.Handle
	var heritageClauses ast.ListRef
	shouldTransformPrivateStaticElementsInClass := false
	classDecorators := tx.transformAllDecoratorsOfDeclaration(node.Decorators())
	if len(classDecorators) > 0 {
		debug.Assert(!ci.classThis.IsNil())
		ci.classDecoratorsName = f.NewUniqueNameEx("_classDecorators", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
		ci.classDescriptorName = f.NewUniqueNameEx("_classDescriptor", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
		ci.classExtraInitializersName = f.NewUniqueNameEx("_classExtraInitializers", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
		decoratorsArray := f.NewArrayLiteralExpression(f.NewList(classDecorators), false)
		classDefinitionStatements = append(classDefinitionStatements, tx.createLet(ci.classDecoratorsName, decoratorsArray), tx.createLet(ci.classDescriptorName, ast.Handle{}), tx.createLet(ci.classExtraInitializersName, f.NewArrayLiteralExpression(f.NewList(nil), false)), tx.createLet(ci.classThis, ast.Handle{}))
		if len(classDecorators) > 0 && ci.hasStaticPrivateClassElements {
			shouldTransformPrivateStaticElementsInClass = true
			tx.shouldTransformPrivateStaticElementsInFile = true
		}
	}
	extendsClause := ast.GetHeritageClause(node, ast.KindExtendsKeyword)
	var extendsElement ast.Handle
	if !extendsClause.IsNil() {
		hc := extendsClause
		if types := hc.Types(); len(types) > 0 {
			extendsElement = types[0]
		}
	}
	var extendsExpression ast.Handle
	if !extendsElement.IsNil() {
		extendsExpression = tx.Visitor().VisitNode(extendsElement.ExpressionWithTypeArgumentsExpression())
	}
	if !extendsExpression.IsNil() {
		ci.classSuper = f.NewUniqueNameEx("_classSuper", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
		unwrapped := ast.SkipOuterExpressions(extendsExpression, ast.OEKAll)
		safeExtendsExpression := extendsExpression
		if (ast.IsClassExpression(unwrapped) && unwrapped.Name().IsNil()) || (ast.IsFunctionExpression(unwrapped) && unwrapped.Name().IsNil()) || ast.IsArrowFunction(unwrapped) {
			safeExtendsExpression = f.NewCommaExpression(f.NewNumericLiteral("0", 0), extendsExpression)
		}
		classDefinitionStatements = append(classDefinitionStatements, tx.createLet(ci.classSuper, safeExtendsExpression))
		updatedExtendsElement := f.UpdateExpressionWithTypeArguments(extendsElement, ci.classSuper, 0)
		hc := extendsClause
		updatedExtendsClause := f.UpdateHeritageClause(hc, hc.HeritageClauseToken(), f.NewList([]ast.Handle{updatedExtendsElement}))
		heritageClauses = f.NewList([]ast.Handle{updatedExtendsClause})
	}
	var renamedClassThis ast.Handle
	if !ci.classThis.IsNil() {
		renamedClassThis = ci.classThis
	} else {
		renamedClassThis = f.NewThisExpression()
	}
	tx.enterClass(ci)
	leadingBlockStatements = append(leadingBlockStatements, tx.createMetadata(ci.metadataReference, ci.classSuper))
	members := tx.nonConstructorClassElementVisitor.VisitNodes(node.MemberList())
	members = tx.constructorClassElementVisitor.VisitNodes(members)
	if len(tx.pendingExpressions) > 0 {
		tx.outerThis = ast.Handle{}
		for _, expr := range tx.pendingExpressions {
			if expr.SubtreeFacts()&ast.SubtreeContainsLexicalThis != 0 {
				expr = tx.outerThisVisitor.VisitNode(expr)
			}
			statement := f.NewExpressionStatement(expr)
			leadingBlockStatements = append(leadingBlockStatements, statement)
		}
		if !tx.outerThis.IsNil() {
			classDefinitionStatements = append([]ast.Handle{tx.createLet(tx.outerThis, f.NewThisExpression())}, classDefinitionStatements...)
		}
		tx.pendingExpressions = nil
	}
	tx.exitClass()
	if len(ci.pendingInstanceInitializers) > 0 && ast.GetFirstConstructorWithBody(node).IsNil() {
		initializerStatements := tx.prepareConstructor(ci)
		if len(initializerStatements) > 0 {
			isDerivedClass := !extendsElement.IsNil() && ast.SkipOuterExpressions(extendsElement.ExpressionWithTypeArgumentsExpression(), ast.OEKAll).Kind != ast.KindNullKeyword
			constructorStatements := []ast.Handle{}
			if isDerivedClass {
				spreadArguments := f.NewSpreadElement(f.NewIdentifier("arguments"))
				superCall := f.NewCallExpression(f.NewKeywordExpression(ast.KindSuperKeyword), ast.Handle{}, 0, f.NewList([]ast.Handle{spreadArguments}), ast.NodeFlagsNone)
				constructorStatements = append(constructorStatements, f.NewExpressionStatement(superCall))
			}
			constructorStatements = append(constructorStatements, initializerStatements...)
			constructorBody := f.NewBlock(f.NewList(constructorStatements), true)
			syntheticConstructor = f.NewConstructorDeclaration(0, 0, f.NewList(nil), ast.Handle{}, ast.Handle{}, constructorBody)
		}
	}
	if !ci.staticMethodExtraInitializersName.IsNil() {
		classDefinitionStatements = append(classDefinitionStatements, tx.createLet(ci.staticMethodExtraInitializersName, f.NewArrayLiteralExpression(f.NewList(nil), false)))
	}
	if !ci.instanceMethodExtraInitializersName.IsNil() {
		classDefinitionStatements = append(classDefinitionStatements, tx.createLet(ci.instanceMethodExtraInitializersName, f.NewArrayLiteralExpression(f.NewList(nil), false)))
	}
	if ci.memberInfos.Size() > 0 {
		classDefinitionStatements = append(classDefinitionStatements, tx.emitMemberInfoDeclarations(ci, true)...)
		classDefinitionStatements = append(classDefinitionStatements, tx.emitMemberInfoDeclarations(ci, false)...)
	}
	leadingBlockStatements = append(leadingBlockStatements, ci.staticNonFieldDecorationStatements...)
	leadingBlockStatements = append(leadingBlockStatements, ci.nonStaticNonFieldDecorationStatements...)
	leadingBlockStatements = append(leadingBlockStatements, ci.staticFieldDecorationStatements...)
	leadingBlockStatements = append(leadingBlockStatements, ci.nonStaticFieldDecorationStatements...)
	if !ci.classDescriptorName.IsNil() && !ci.classDecoratorsName.IsNil() && !ci.classExtraInitializersName.IsNil() && !ci.classThis.IsNil() {
		valueProperty := f.NewPropertyAssignment(0, f.NewIdentifier("value"), ast.Handle{}, ast.Handle{}, renamedClassThis)
		classDescriptor := f.NewObjectLiteralExpression(f.NewList([]ast.Handle{valueProperty}), false)
		classDescriptorAssignment := f.NewAssignmentExpression(ci.classDescriptorName, classDescriptor)
		classNameReference := f.NewPropertyAccessExpression(renamedClassThis, ast.Handle{}, f.NewIdentifier("name"), ast.NodeFlagsNone)
		contextObj := f.NewESDecorateClassContextObject(classNameReference, ci.metadataReference)
		esDecorateHelper := f.NewESDecorateHelper(f.NewToken(ast.KindNullKeyword), classDescriptorAssignment, ci.classDecoratorsName, contextObj, f.NewToken(ast.KindNullKeyword), ci.classExtraInitializersName)
		esDecorateStatement := f.NewExpressionStatement(esDecorateHelper)
		ec.SetSourceMapRange(esDecorateStatement, transformers.MoveRangePastDecorators(node))
		leadingBlockStatements = append(leadingBlockStatements, esDecorateStatement)
		classDescriptorValueRef := f.NewPropertyAccessExpression(ci.classDescriptorName, ast.Handle{}, f.NewIdentifier("value"), ast.NodeFlagsNone)
		classThisAssignment := f.NewAssignmentExpression(ci.classThis, classDescriptorValueRef)
		classReferenceAssignment := f.NewAssignmentExpression(classReference, classThisAssignment)
		leadingBlockStatements = append(leadingBlockStatements, f.NewExpressionStatement(classReferenceAssignment))
	}
	leadingBlockStatements = append(leadingBlockStatements, tx.createSymbolMetadata(renamedClassThis, ci.metadataReference))
	if len(ci.pendingStaticInitializers) > 0 {
		for _, initializer := range ci.pendingStaticInitializers {
			initializerStatement := f.NewExpressionStatement(initializer)
			ec.SetSourceMapRange(initializerStatement, ec.SourceMapRange(initializer))
			trailingBlockStatements = append(trailingBlockStatements, initializerStatement)
		}
		ci.pendingStaticInitializers = nil
	}
	if !ci.classExtraInitializersName.IsNil() {
		runClassInitializersHelper := f.NewRunInitializersHelper(renamedClassThis, ci.classExtraInitializersName, ast.Handle{})
		runClassInitializersStatement := f.NewExpressionStatement(runClassInitializersHelper)
		if !node.Name().IsNil() {
			ec.SetSourceMapRange(runClassInitializersStatement, node.Name().Loc())
		} else {
			ec.SetSourceMapRange(runClassInitializersStatement, transformers.MoveRangePastDecorators(node))
		}
		trailingBlockStatements = append(trailingBlockStatements, runClassInitializersStatement)
	}
	if len(leadingBlockStatements) > 0 && len(trailingBlockStatements) > 0 && !ci.hasStaticInitializers {
		leadingBlockStatements = append(leadingBlockStatements, trailingBlockStatements...)
		trailingBlockStatements = nil
	}
	var leadingStaticBlock ast.Handle
	if len(leadingBlockStatements) > 0 {
		leadingStaticBlock = f.NewClassStaticBlockDeclaration(0, f.NewBlock(f.NewList(leadingBlockStatements), true))
	}
	if !leadingStaticBlock.IsNil() && shouldTransformPrivateStaticElementsInClass {
		ec.SetEmitFlags(leadingStaticBlock, printer.EFTransformPrivateStaticElements)
	}
	var trailingStaticBlock ast.Handle
	if len(trailingBlockStatements) > 0 {
		trailingStaticBlock = f.NewClassStaticBlockDeclaration(0, f.NewBlock(f.NewList(trailingBlockStatements), true))
	}
	if !leadingStaticBlock.IsNil() || !syntheticConstructor.IsNil() || !trailingStaticBlock.IsNil() {
		newMembers := make([]ast.Handle, 0, node.Store().ListLen(members)+3)
		existingNamedEvaluationHelperBlockIndex := -1
		for i, m := range node.Store().ListSlice(members) {
			if isClassNamedEvaluationHelperBlock(ec, m) {
				existingNamedEvaluationHelperBlockIndex = i
				break
			}
		}
		// Slice once for member splice/append into newMembers.
		membersSlice := node.Store().ListSlice(members).Slice()
		if !leadingStaticBlock.IsNil() {
			newMembers = append(newMembers, membersSlice[:existingNamedEvaluationHelperBlockIndex+1]...)
			newMembers = append(newMembers, leadingStaticBlock)
			newMembers = append(newMembers, membersSlice[existingNamedEvaluationHelperBlockIndex+1:]...)
		} else {
			newMembers = append(newMembers, membersSlice...)
		}
		if !syntheticConstructor.IsNil() {
			newMembers = append(newMembers, syntheticConstructor)
		}
		if !trailingStaticBlock.IsNil() {
			newMembers = append(newMembers, trailingStaticBlock)
		}
		membersList := f.List(node.Store().ListLoc(members), newMembers...)
		members = membersList
	}
	lexicalEnvironment := ec.EndVariableEnvironment()
	var classExpression ast.Handle
	if len(classDecorators) > 0 {
		classExpression = f.NewClassExpression(0, ast.Handle{}, 0, heritageClauses, members)
		ec.SetOriginal(classExpression, node)
		if !ci.classThis.IsNil() {
			classExpression = injectClassThisAssignmentIfMissing(ec, f, classExpression, ci.classThis)
		}
		classReferenceDeclaration := f.NewVariableDeclaration(classReference, ast.Handle{}, ast.Handle{}, classExpression)
		classReferenceVarDeclList := f.NewVariableDeclarationList(f.NewList([]ast.Handle{classReferenceDeclaration}), ast.NodeFlagsNone)
		var returnExpr ast.Handle
		if !ci.classThis.IsNil() {
			returnExpr = f.NewAssignmentExpression(classReference, ci.classThis)
		} else {
			returnExpr = classReference
		}
		classDefinitionStatements = append(classDefinitionStatements, f.NewVariableStatement(0, classReferenceVarDeclList), f.NewReturnStatement(returnExpr))
	} else {
		classExpression = f.NewClassExpression(0, node.Name(), 0, heritageClauses, members)
		ec.SetOriginal(classExpression, node)
		classDefinitionStatements = append(classDefinitionStatements, f.NewReturnStatement(classExpression))
	}
	if shouldTransformPrivateStaticElementsInClass {
		ec.AddEmitFlags(classExpression, printer.EFTransformPrivateStaticElements)
		for _, member := range classExpression.Members() {
			if (ast.IsPrivateIdentifierClassElementDeclaration(member) || ast.IsAutoAccessorPropertyDeclaration(member)) && ast.HasStaticModifier(member) {
				ec.AddEmitFlags(member, printer.EFTransformPrivateStaticElements)
			}
		}
	}
	mergedStatements := ec.MergeEnvironment(classDefinitionStatements, lexicalEnvironment)
	return f.NewImmediatelyInvokedArrowFunction(mergedStatements)
}

func (tx *esDecoratorTransformer) emitMemberInfoDeclarations(ci *classInfo, isStatic bool) []ast.Handle {
	f := tx.Factory()
	var stmts []ast.Handle
	for member, mi := range ci.memberInfos.Entries() {
		if ast.IsStatic(member) != isStatic {
			continue
		}
		stmts = append(stmts, tx.createLet(mi.memberDecoratorsName, ast.Handle{}))
		if !mi.memberInitializersName.IsNil() {
			stmts = append(stmts, tx.createLet(mi.memberInitializersName, f.NewArrayLiteralExpression(f.NewList(nil), false)))
		}
		if !mi.memberExtraInitializersName.IsNil() {
			stmts = append(stmts, tx.createLet(mi.memberExtraInitializersName, f.NewArrayLiteralExpression(f.NewList(nil), false)))
		}
		if !mi.memberDescriptorName.IsNil() {
			stmts = append(stmts, tx.createLet(mi.memberDescriptorName, ast.Handle{}))
		}
	}
	return stmts
}
func isDecoratedClassLike(node ast.Handle) bool {
	return ast.ClassOrConstructorParameterIsDecorated(false, node) || ast.ChildIsDecorated(false, node, ast.Handle{})
}
func (tx *esDecoratorTransformer) visitClassDeclaration(node ast.Handle) ast.Handle {
	if isDecoratedClassLike(node) {
		f := tx.Factory()
		ec := tx.EmitContext()
		statements := []ast.Handle{}
		originalClass := ec.MostOriginal(node)
		if !ast.IsClassLike(originalClass) {
			originalClass = node
		}
		var className ast.Handle
		if !originalClass.Name().IsNil() {
			className = f.NewStringLiteralFromNode(originalClass.Name())
		} else {
			className = f.NewStringLiteral("default", 0)
		}
		isExport := ast.HasSyntacticModifier(node, ast.ModifierFlagsExport)
		isDefault := ast.HasSyntacticModifier(node, ast.ModifierFlagsDefault)
		classNode := node
		if node.Name().IsNil() {
			classNode = injectClassNamedEvaluationHelperBlockIfMissing(ec, classNode, className, ast.Handle{})
		}
		if isExport && isDefault {
			iife := tx.transformClassLike(classNode)
			if !classNode.Name().IsNil() {
				varDecl := f.NewVariableDeclaration(f.GetLocalName(classNode), ast.Handle{}, ast.Handle{}, iife)
				ec.SetOriginal(varDecl, classNode)
				varDecls := f.NewVariableDeclarationList(f.NewList([]ast.Handle{varDecl}), ast.NodeFlagsLet)
				varStatement := f.NewVariableStatement(0, varDecls)
				statements = append(statements, varStatement)
				exportStatement := f.NewExportDefault(f.GetDeclarationName(classNode))
				ec.SetOriginal(exportStatement, classNode)
				ec.AssignCommentRange(exportStatement, classNode)
				ec.SetSourceMapRange(exportStatement, transformers.MoveRangePastDecorators(classNode))
				statements = append(statements, exportStatement)
			} else {
				exportStatement := f.NewExportDefault(iife)
				ec.SetOriginal(exportStatement, classNode)
				ec.AssignCommentRange(exportStatement, classNode)
				ec.SetSourceMapRange(exportStatement, transformers.MoveRangePastDecorators(classNode))
				statements = append(statements, exportStatement)
			}
		} else {
			debug.Assert(!classNode.Name().IsNil(), "A class declaration that is not a default export must have a name.")
			iife := tx.transformClassLike(classNode)
			modifiers := tx.exportStrippingModifierVisitor.VisitModifiers(classNode.Modifiers())
			declName := f.GetLocalNameEx(classNode, printer.AssignedNameOptions{AllowSourceMaps: true})
			varDecl := f.NewVariableDeclaration(declName, ast.Handle{}, ast.Handle{}, iife)
			ec.SetOriginal(varDecl, classNode)
			varDecls := f.NewVariableDeclarationList(f.NewList([]ast.Handle{varDecl}), ast.NodeFlagsLet)
			varStatement := f.NewVariableStatement(modifiers, varDecls)
			ec.SetOriginal(varStatement, classNode)
			ec.AssignCommentRange(varStatement, classNode)
			statements = append(statements, varStatement)
			if isExport {
				exportStatement := f.NewExternalModuleExport(declName)
				ec.SetOriginal(exportStatement, classNode)
				statements = append(statements, exportStatement)
			}
		}
		return transformers.SingleOrMany(statements, f)
	}
	modifiers := tx.modifierVisitor.VisitModifiers(node.Modifiers())
	heritageClauses := tx.Visitor().VisitNodes(node.HeritageClauses())
	tx.enterClass(nil)
	members := tx.classElementVisitor.VisitNodes(node.MemberList())
	tx.exitClass()
	return tx.Factory().UpdateClassDeclaration(node, modifiers, node.Name(), 0, heritageClauses, members)
}
func (tx *esDecoratorTransformer) visitClassExpression(node ast.Handle) ast.Handle {
	if isDecoratedClassLike(node) {
		iife := tx.transformClassLike(node)
		tx.EmitContext().SetOriginal(iife, node)
		return iife
	}
	modifiers := tx.modifierVisitor.VisitModifiers(node.Modifiers())
	heritageClauses := tx.Visitor().VisitNodes(node.HeritageClauses())
	tx.enterClass(nil)
	members := tx.classElementVisitor.VisitNodes(node.MemberList())
	tx.exitClass()
	return tx.Factory().UpdateClassExpression(node, modifiers, node.Name(), 0, heritageClauses, members)
}
func (tx *esDecoratorTransformer) prepareConstructor(ci *classInfo) []ast.Handle {
	if len(ci.pendingInstanceInitializers) == 0 {
		return nil
	}
	f := tx.Factory()
	statements := []ast.Handle{f.NewExpressionStatement(f.InlineExpressions(ci.pendingInstanceInitializers))}
	ci.pendingInstanceInitializers = nil
	return statements
}
func (tx *esDecoratorTransformer) transformConstructorBodyWorker(statementsOut []ast.Handle, statementsIn []ast.Handle, statementOffset int, superPath []int, superPathDepth int, initializerStatements []ast.Handle) []ast.Handle {
	superStatementIndex := superPath[superPathDepth]
	if superStatementIndex > statementOffset {
		for _, s := range statementsIn[statementOffset:superStatementIndex] {
			statementsOut = append(statementsOut, tx.Visitor().VisitNode(s))
		}
	}
	superStatement := statementsIn[superStatementIndex]
	if ast.IsTryStatement(superStatement) {
		tryBlockNode := superStatement.TryStatementTryBlock()
		tryBlock := tryBlockNode
		tryBlockStatements := tx.transformConstructorBodyWorker(nil, tryBlock.Statements(), 0, superPath, superPathDepth+1, initializerStatements)
		newTryBlock := tx.Factory().NewBlock(tx.Factory().NewList(tryBlockStatements), true)
		newTryBlock.SetLoc(tryBlockNode.Loc())
		var catchClause ast.Handle
		if !superStatement.TryStatementCatchClause().IsNil() {
			catchClause = tx.Visitor().VisitNode(superStatement.TryStatementCatchClause())
		}
		var finallyBlock ast.Handle
		if !superStatement.TryStatementFinallyBlock().IsNil() {
			finallyBlock = tx.Visitor().VisitNode(superStatement.TryStatementFinallyBlock())
		}
		updated := tx.Factory().UpdateTryStatement(superStatement, newTryBlock, catchClause, finallyBlock)
		statementsOut = append(statementsOut, updated)
	} else {
		statementsOut = append(statementsOut, tx.Visitor().VisitNode(superStatement))
		statementsOut = append(statementsOut, initializerStatements...)
	}
	if superStatementIndex+1 < len(statementsIn) {
		for _, s := range statementsIn[superStatementIndex+1:] {
			statementsOut = append(statementsOut, tx.Visitor().VisitNode(s))
		}
	}
	return statementsOut
}
func (tx *esDecoratorTransformer) visitConstructorDeclaration(node ast.Handle) ast.Handle {
	tx.enterClassElement(node)
	modifiers := tx.modifierVisitor.VisitModifiers(node.Modifiers())
	parameters := tx.Visitor().VisitNodes(node.ParameterList())
	var body ast.Handle
	ctor := node
	if !ctor.Body().IsNil() && tx.classInfoStack != nil {
		initializerStatements := tx.prepareConstructor(tx.classInfoStack)
		if len(initializerStatements) > 0 {
			stmts := []ast.Handle{}
			// SplitStandardPrologue partitions []Handle.
			prologue, rest := tx.Factory().SplitStandardPrologue(ctor.Body().StatementsSeq().Slice())
			stmts = append(stmts, prologue...)
			superStatementIndices := transformers.FindSuperStatementIndexPath(rest, 0)
			if len(superStatementIndices) > 0 {
				stmts = tx.transformConstructorBodyWorker(stmts, rest, 0, superStatementIndices, 0, initializerStatements)
			} else {
				stmts = append(stmts, initializerStatements...)
				visited := tx.Visitor().VisitSlice(rest)
				stmts = append(stmts, visited...)
			}
			body = tx.Factory().NewBlock(tx.Factory().NewList(stmts), true)
			tx.EmitContext().SetOriginal(body, ctor.Body())
			body.SetLoc(ctor.Body().Loc())
		}
	}
	if body.IsNil() {
		body = tx.Visitor().VisitNode(ctor.Body())
	}
	tx.exitClassElement()
	return tx.Factory().UpdateConstructorDeclaration(ctor, modifiers, 0, parameters, ast.Handle{}, ast.Handle{}, body)
}
func (tx *esDecoratorTransformer) finishClassElement(updated ast.Handle, original ast.Handle) ast.Handle {
	if updated != original {
		tx.EmitContext().AssignCommentRange(updated, original)
		tx.EmitContext().SetSourceMapRange(updated, transformers.MoveRangePastDecorators(original))
	}
	return updated
}

type partialResult struct {
	modifiers             ast.ListRef
	referencedName        ast.Handle
	name                  ast.Handle
	initializersName      ast.Handle
	extraInitializersName ast.Handle
	descriptorName        ast.Handle
	thisArg               ast.Handle
}
type createDescriptorFunc func(member ast.Handle, modifiers ast.ListRef) ast.Handle

func (tx *esDecoratorTransformer) partialTransformClassElement(member ast.Handle, ci *classInfo, createDescriptor createDescriptorFunc) partialResult {
	f := tx.Factory()
	ec := tx.EmitContext()
	if ci == nil {
		modifiers := tx.modifierVisitor.VisitModifiers(member.Modifiers())
		tx.enterName()
		name := tx.visitPropertyName(member.Name())
		tx.exitName()
		return partialResult{modifiers: modifiers, name: name}
	}
	savedClassThis := tx.classThis
	tx.classThis = ast.Handle{}
	memberDecorators := tx.transformAllDecoratorsOfDeclaration(member.Decorators())
	tx.classThis = savedClassThis
	modifiers := tx.modifierVisitor.VisitModifiers(member.Modifiers())
	var result partialResult
	result.modifiers = modifiers
	if len(memberDecorators) > 0 {
		memberDecoratorsName := tx.createHelperVariable(member, "decorators")
		memberDecoratorsArray := f.NewArrayLiteralExpression(f.NewList(memberDecorators), false)
		memberDecoratorsAssignment := f.NewAssignmentExpression(memberDecoratorsName, memberDecoratorsArray)
		mi := &memberInfo{memberDecoratorsName: memberDecoratorsName}
		ci.memberInfos.Set(member, mi)
		tx.pendingExpressions = append(tx.pendingExpressions, memberDecoratorsAssignment)
		var kind string
		switch {
		case ast.IsGetAccessorDeclaration(member):
			kind = "getter"
		case ast.IsSetAccessorDeclaration(member):
			kind = "setter"
		case ast.IsMethodDeclaration(member):
			kind = "method"
		case ast.IsAutoAccessorPropertyDeclaration(member):
			kind = "accessor"
		case ast.IsPropertyDeclaration(member):
			kind = "field"
		default:
			debug.Fail("Unexpected class element kind.")
		}
		var propertyNameComputed bool
		var propertyNameExpr ast.Handle
		if !member.Name().IsNil() && (ast.IsIdentifier(member.Name()) || ast.IsPrivateIdentifier(member.Name())) {
			propertyNameComputed = false
			propertyNameExpr = member.Name()
		} else if !member.Name().IsNil() && ast.IsPropertyNameLiteral(member.Name()) {
			propertyNameComputed = true
			propertyNameExpr = f.NewStringLiteralFromNode(member.Name())
		} else if !member.Name().IsNil() && ast.IsComputedPropertyName(member.Name()) {
			cpn := member.Name()
			if ast.IsPropertyNameLiteral(cpn.Expression()) && !ast.IsIdentifier(cpn.Expression()) {
				propertyNameComputed = true
				propertyNameExpr = f.NewStringLiteralFromNode(cpn.Expression())
			} else {
				tx.enterName()
				result.referencedName, result.name = tx.visitReferencedPropertyName(member.Name())
				tx.exitName()
				propertyNameComputed = true
				propertyNameExpr = result.referencedName
			}
		}
		contextObj := f.NewESDecorateClassElementContextObject(kind, propertyNameComputed, propertyNameExpr, ast.IsStatic(member), !member.Name().IsNil() && ast.IsPrivateIdentifier(member.Name()), ast.IsPropertyDeclaration(member) || ast.IsGetAccessorDeclaration(member) || ast.IsMethodDeclaration(member), ast.IsPropertyDeclaration(member) || ast.IsSetAccessorDeclaration(member), ci.metadataReference)
		if ast.IsMethodOrAccessor(member) {
			methodExtraInitializersName := ci.instanceMethodExtraInitializersName
			if ast.IsStatic(member) {
				methodExtraInitializersName = ci.staticMethodExtraInitializersName
			}
			debug.Assert(!methodExtraInitializersName.IsNil(), "methodExtraInitializersName should be defined")
			var descriptorArg ast.Handle
			if ast.IsPrivateIdentifierClassElementDeclaration(member) && createDescriptor != nil {
				asyncMods := tx.asyncOnlyModifierVisitor.VisitModifiers(modifiers)
				descriptor := createDescriptor(member, asyncMods)
				mi.memberDescriptorName = tx.createHelperVariable(member, "descriptor")
				result.descriptorName = mi.memberDescriptorName
				descriptorArg = f.NewAssignmentExpression(mi.memberDescriptorName, descriptor)
			} else {
				descriptorArg = f.NewToken(ast.KindNullKeyword)
			}
			esDecorateExpr := f.NewESDecorateHelper(f.NewThisExpression(), descriptorArg, memberDecoratorsName, contextObj, f.NewToken(ast.KindNullKeyword), methodExtraInitializersName)
			esDecorateStatement := f.NewExpressionStatement(esDecorateExpr)
			ec.SetSourceMapRange(esDecorateStatement, transformers.MoveRangePastDecorators(member))
			tx.appendDecorationStatement(ci, member, esDecorateStatement)
		} else if ast.IsPropertyDeclaration(member) {
			mi.memberInitializersName = tx.createHelperVariable(member, "initializers")
			mi.memberExtraInitializersName = tx.createHelperVariable(member, "extraInitializers")
			result.initializersName = mi.memberInitializersName
			result.extraInitializersName = mi.memberExtraInitializersName
			if ast.IsStatic(member) {
				result.thisArg = ci.classThis
			}
			var ctorArg ast.Handle
			if ast.IsAutoAccessorPropertyDeclaration(member) {
				ctorArg = f.NewThisExpression()
			} else {
				ctorArg = f.NewToken(ast.KindNullKeyword)
			}
			var descriptorArg ast.Handle
			if ast.IsPrivateIdentifierClassElementDeclaration(member) && ast.HasAccessorModifier(member) && createDescriptor != nil {
				descriptor := createDescriptor(member, 0)
				mi.memberDescriptorName = tx.createHelperVariable(member, "descriptor")
				result.descriptorName = mi.memberDescriptorName
				descriptorArg = f.NewAssignmentExpression(mi.memberDescriptorName, descriptor)
			} else {
				descriptorArg = f.NewToken(ast.KindNullKeyword)
			}
			esDecorateExpr := f.NewESDecorateHelper(ctorArg, descriptorArg, memberDecoratorsName, contextObj, mi.memberInitializersName, mi.memberExtraInitializersName)
			esDecorateStatement := f.NewExpressionStatement(esDecorateExpr)
			ec.SetSourceMapRange(esDecorateStatement, transformers.MoveRangePastDecorators(member))
			tx.appendDecorationStatement(ci, member, esDecorateStatement)
		}
	}
	if result.name.IsNil() {
		tx.enterName()
		result.name = tx.visitPropertyName(member.Name())
		tx.exitName()
	}
	if (modifiers == 0 || member.Store().ListLen(modifiers) == 0) && (ast.IsMethodDeclaration(member) || ast.IsPropertyDeclaration(member)) {
		ec.SetEmitFlags(result.name, printer.EFNoLeadingComments)
	}
	return result
}

func (tx *esDecoratorTransformer) appendDecorationStatement(ci *classInfo, member ast.Handle, stmt ast.Handle) {
	if ast.IsMethodOrAccessor(member) || ast.IsAutoAccessorPropertyDeclaration(member) {
		if ast.IsStatic(member) {
			ci.staticNonFieldDecorationStatements = append(ci.staticNonFieldDecorationStatements, stmt)
		} else {
			ci.nonStaticNonFieldDecorationStatements = append(ci.nonStaticNonFieldDecorationStatements, stmt)
		}
	} else if ast.IsPropertyDeclaration(member) && !ast.IsAutoAccessorPropertyDeclaration(member) {
		if ast.IsStatic(member) {
			ci.staticFieldDecorationStatements = append(ci.staticFieldDecorationStatements, stmt)
		} else {
			ci.nonStaticFieldDecorationStatements = append(ci.nonStaticFieldDecorationStatements, stmt)
		}
	} else {
		debug.Fail("Unexpected class element kind.")
	}
}
func (tx *esDecoratorTransformer) visitMethodDeclaration(node ast.Handle) ast.Handle {
	tx.enterClassElement(node)
	result := tx.partialTransformClassElement(node, tx.classInfoStack, tx.createMethodDescriptorObject)
	if !result.descriptorName.IsNil() {
		tx.exitClassElement()
		return tx.finishClassElement(tx.createMethodDescriptorForwarder(result.modifiers, result.name, result.descriptorName), node)
	}
	parameters := tx.Visitor().VisitNodes(node.ParameterList())
	body := tx.Visitor().VisitNode(node.Body())
	tx.exitClassElement()
	method := node
	return tx.finishClassElement(tx.Factory().UpdateMethodDeclaration(method, result.modifiers, method.AsteriskToken(), result.name, ast.Handle{}, 0, parameters, ast.Handle{}, ast.Handle{}, body), node)
}
func (tx *esDecoratorTransformer) visitGetAccessorDeclaration(node ast.Handle) ast.Handle {
	tx.enterClassElement(node)
	result := tx.partialTransformClassElement(node, tx.classInfoStack, tx.createGetAccessorDescriptorObject)
	if !result.descriptorName.IsNil() {
		tx.exitClassElement()
		return tx.finishClassElement(tx.createGetAccessorDescriptorForwarder(result.modifiers, result.name, result.descriptorName), node)
	}
	parameters := tx.Visitor().VisitNodes(node.ParameterList())
	body := tx.Visitor().VisitNode(node.Body())
	tx.exitClassElement()
	accessor := node
	return tx.finishClassElement(tx.Factory().UpdateGetAccessorDeclaration(accessor, result.modifiers, result.name, 0, parameters, ast.Handle{}, ast.Handle{}, body), node)
}
func (tx *esDecoratorTransformer) visitSetAccessorDeclaration(node ast.Handle) ast.Handle {
	tx.enterClassElement(node)
	result := tx.partialTransformClassElement(node, tx.classInfoStack, tx.createSetAccessorDescriptorObject)
	if !result.descriptorName.IsNil() {
		tx.exitClassElement()
		return tx.finishClassElement(tx.createSetAccessorDescriptorForwarder(result.modifiers, result.name, result.descriptorName), node)
	}
	parameters := tx.Visitor().VisitNodes(node.ParameterList())
	body := tx.Visitor().VisitNode(node.Body())
	tx.exitClassElement()
	accessor := node
	return tx.finishClassElement(tx.Factory().UpdateSetAccessorDeclaration(accessor, result.modifiers, result.name, 0, parameters, ast.Handle{}, ast.Handle{}, body), node)
}
func (tx *esDecoratorTransformer) visitClassStaticBlockDeclaration(node ast.Handle) ast.Handle {
	tx.enterClassElement(node)
	f := tx.Factory()
	var result ast.Handle
	if isClassNamedEvaluationHelperBlock(tx.EmitContext(), node) {
		result = tx.Visitor().VisitEachChild(node)
		if assignedName := tx.EmitContext().AssignedName(node); !assignedName.IsNil() && result != node {
			tx.EmitContext().SetAssignedName(result, assignedName)
		}
	} else if isClassThisAssignmentBlock(tx.EmitContext(), node) {
		savedClassThis := tx.classThis
		tx.classThis = ast.Handle{}
		result = tx.Visitor().VisitEachChild(node)
		tx.classThis = savedClassThis
	} else {
		ec := tx.EmitContext()
		ec.StartVariableEnvironment()
		result = tx.Visitor().VisitEachChild(node)
		varStatements := ec.EndVariableEnvironment()
		if len(varStatements) > 0 {
			blockBody := result.ClassStaticBlockDeclarationBody()
			newStmts := make([]ast.Handle, 0, len(varStatements)+len(blockBody.Statements()))
			newStmts = append(newStmts, varStatements...)
			newStmts = append(newStmts, blockBody.Statements()...)
			result = f.NewClassStaticBlockDeclaration(0, f.NewBlock(f.NewList(newStmts), blockBody.MultiLine()))
		}
		if tx.classInfoStack != nil {
			tx.classInfoStack.hasStaticInitializers = true
			if len(tx.classInfoStack.pendingStaticInitializers) > 0 {
				stmts := []ast.Handle{}
				for _, init := range tx.classInfoStack.pendingStaticInitializers {
					initStmt := f.NewExpressionStatement(init)
					tx.EmitContext().SetSourceMapRange(initStmt, tx.EmitContext().SourceMapRange(init))
					stmts = append(stmts, initStmt)
				}
				body := f.NewBlock(f.NewList(stmts), true)
				staticBlock := f.NewClassStaticBlockDeclaration(0, body)
				tx.classInfoStack.pendingStaticInitializers = nil
				tx.exitClassElement()
				return transformers.SingleOrMany([]ast.Handle{staticBlock, result}, tx.Factory())
			}
		}
	}
	tx.exitClassElement()
	return result
}
func (tx *esDecoratorTransformer) visitPropertyDeclaration(node ast.Handle) ast.Handle {
	if isNamedEvaluationAnd(tx.EmitContext(), node, isAnonymousClassNeedingAssignedName) {
		node = transformNamedEvaluation(tx.EmitContext(), node, canIgnoreEmptyStringLiteralInAssignedName(node.Initializer()), "")
	}
	tx.enterClassElement(node)
	debug.Assert(!ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient), "Not yet implemented.")
	f := tx.Factory()
	ec := tx.EmitContext()
	var createDescriptor createDescriptorFunc
	if ast.HasAccessorModifier(node) {
		createDescriptor = tx.createAccessorPropertyDescriptorObject
	}
	result := tx.partialTransformClassElement(node, tx.classInfoStack, createDescriptor)
	ec.StartVariableEnvironment()
	initializer := tx.Visitor().VisitNode(node.Initializer())
	if !result.initializersName.IsNil() {
		var thisArg ast.Handle
		if !result.thisArg.IsNil() {
			thisArg = result.thisArg
		} else {
			thisArg = f.NewThisExpression()
		}
		if initializer.IsNil() {
			initializer = f.NewVoidZeroExpression()
		}
		initializer = f.NewRunInitializersHelper(thisArg, result.initializersName, initializer)
	}
	if ast.IsStatic(node) && tx.classInfoStack != nil && !initializer.IsNil() {
		tx.classInfoStack.hasStaticInitializers = true
	}
	declarations := ec.EndVariableEnvironment()
	if len(declarations) > 0 {
		stmts := make([]ast.Handle, len(declarations)+1)
		copy(stmts, declarations)
		stmts[len(declarations)] = f.NewReturnStatement(initializer)
		initializer = f.NewImmediatelyInvokedArrowFunction(stmts)
	}
	if tx.classInfoStack != nil {
		if ast.IsStatic(node) {
			initializer = tx.injectPendingInitializers(tx.classInfoStack, true, initializer)
			if !result.extraInitializersName.IsNil() {
				var thisArg ast.Handle
				if !tx.classInfoStack.classThis.IsNil() {
					thisArg = tx.classInfoStack.classThis
				} else {
					thisArg = f.NewThisExpression()
				}
				tx.classInfoStack.pendingStaticInitializers = append(tx.classInfoStack.pendingStaticInitializers, f.NewRunInitializersHelper(thisArg, result.extraInitializersName, ast.Handle{}))
			}
		} else {
			initializer = tx.injectPendingInitializers(tx.classInfoStack, false, initializer)
			if !result.extraInitializersName.IsNil() {
				tx.classInfoStack.pendingInstanceInitializers = append(tx.classInfoStack.pendingInstanceInitializers, f.NewRunInitializersHelper(f.NewThisExpression(), result.extraInitializersName, ast.Handle{}))
			}
		}
	}
	tx.exitClassElement()
	if ast.HasAccessorModifier(node) && !result.descriptorName.IsNil() {
		commentRange := ec.CommentRange(node)
		sourceMapRange := ec.SourceMapRange(node)
		propName := node.Name()
		getterName := result.name
		setterName := result.name
		if ast.IsComputedPropertyName(propName) && !transformers.IsSimpleInlineableExpression(propName.Expression()) {
			cacheAssignment := findComputedPropertyNameCacheAssignment(ec, propName)
			if !cacheAssignment.IsNil() {
				getterName = f.UpdateComputedPropertyName(propName, tx.Visitor().VisitNode(propName.Expression()))
				setterName = f.UpdateComputedPropertyName(propName, cacheAssignment.Left())
			} else {
				temp := f.NewTempVariable()
				ec.SetSourceMapRange(temp, propName.Expression().Loc())
				ec.AddVariableDeclaration(temp)
				expression := tx.Visitor().VisitNode(propName.Expression())
				assignment := f.NewAssignmentExpression(temp, expression)
				ec.SetSourceMapRange(assignment, propName.Expression().Loc())
				getterName = f.UpdateComputedPropertyName(propName, assignment)
				setterName = f.UpdateComputedPropertyName(propName, temp)
			}
		}
		modifiersWithoutAccessor := tx.accessorStrippingModifierVisitor.VisitModifiers(result.modifiers)
		backingField := createAccessorPropertyBackingField(f, node, modifiersWithoutAccessor, initializer)
		ec.SetOriginal(backingField, node)
		ec.SetEmitFlags(backingField, printer.EFNoComments)
		ec.SetSourceMapRange(backingField, sourceMapRange)
		ec.SetSourceMapRange(backingField.PropertyDeclarationName(), ec.SourceMapRange(node.Name()))
		getter := tx.createGetAccessorDescriptorForwarder(modifiersWithoutAccessor, getterName, result.descriptorName)
		ec.SetOriginal(getter, node)
		ec.SetCommentRange(getter, commentRange)
		ec.SetSourceMapRange(getter, sourceMapRange)
		setter := tx.createSetAccessorDescriptorForwarder(modifiersWithoutAccessor, setterName, result.descriptorName)
		ec.SetOriginal(setter, node)
		ec.SetEmitFlags(setter, printer.EFNoComments)
		ec.SetSourceMapRange(setter, sourceMapRange)
		return transformers.SingleOrMany([]ast.Handle{backingField, getter, setter}, f)
	}
	prop := node
	return tx.finishClassElement(f.UpdatePropertyDeclaration(prop, result.modifiers, result.name, ast.Handle{}, ast.Handle{}, initializer), node)
}
func (tx *esDecoratorTransformer) visitThisExpression(node ast.Handle) ast.Handle {
	if !tx.classThis.IsNil() {
		return tx.classThis
	}
	return node
}
func (tx *esDecoratorTransformer) visitCallExpression(node ast.Handle) ast.Handle {
	call := node
	if ast.IsSuperProperty(call.Expression()) && !tx.classThis.IsNil() {
		expression := tx.Visitor().VisitNode(call.Expression())
		argumentsList := tx.Visitor().VisitNodes(call.ArgumentList())
		// NewFunctionCallCall takes []Handle arguments.
		invocation := tx.Factory().NewFunctionCallCall(expression, tx.classThis, node.Store().ListSlice(argumentsList).Slice())
		tx.EmitContext().SetOriginal(invocation, node)
		invocation.SetLoc(node.Loc())
		return invocation
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitTaggedTemplateExpression(node ast.Handle) ast.Handle {
	tte := node
	if ast.IsSuperProperty(tte.Tag()) && !tx.classThis.IsNil() {
		tag := tx.Visitor().VisitNode(tte.Tag())
		boundTag := tx.Factory().NewFunctionBindCall(tag, tx.classThis, []ast.Handle{})
		tx.EmitContext().SetOriginal(boundTag, node)
		boundTag.SetLoc(node.Loc())
		template := tx.Visitor().VisitNode(tte.Template())
		return tx.Factory().UpdateTaggedTemplateExpression(tte, boundTag, ast.Handle{}, 0, template, tte.Flags())
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitPropertyAccessExpression(node ast.Handle) ast.Handle {
	pa := node
	if ast.IsSuperProperty(node) && ast.IsIdentifier(pa.Name()) && !tx.classThis.IsNil() && !tx.classSuper.IsNil() {
		propertyName := tx.Factory().NewStringLiteralFromNode(pa.Name())
		superProperty := tx.Factory().NewReflectGetCall(tx.classSuper, propertyName, tx.classThis)
		tx.EmitContext().SetOriginal(superProperty, pa.Expression())
		superProperty.SetLoc(pa.Expression().Loc())
		return superProperty
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitElementAccessExpression(node ast.Handle) ast.Handle {
	ea := node
	if ast.IsSuperProperty(node) && !tx.classThis.IsNil() && !tx.classSuper.IsNil() {
		propertyName := tx.Visitor().VisitNode(ea.ArgumentExpression())
		superProperty := tx.Factory().NewReflectGetCall(tx.classSuper, propertyName, tx.classThis)
		tx.EmitContext().SetOriginal(superProperty, ea.Expression())
		superProperty.SetLoc(ea.Expression().Loc())
		return superProperty
	}
	return tx.Visitor().VisitEachChild(node)
}

func (tx *esDecoratorTransformer) visitParameterDeclaration(node ast.Handle) ast.Handle {
	paramNode := node
	if isNamedEvaluationAnd(tx.EmitContext(), paramNode, isAnonymousClassNeedingAssignedName) {
		paramNode = transformNamedEvaluation(tx.EmitContext(), paramNode, canIgnoreEmptyStringLiteralInAssignedName(paramNode.Initializer()), "")
		node = paramNode
	}
	updated := tx.Factory().UpdateParameterDeclaration(node, 0, node.DotDotDotToken(), tx.Visitor().VisitNode(node.Name()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Initializer()))
	if updated != paramNode {
		tx.EmitContext().SetCommentRange(updated, paramNode.Loc())
		newLoc := transformers.MoveRangePastModifiers(paramNode)
		updated.SetLoc(newLoc)
		tx.EmitContext().SetSourceMapRange(updated, newLoc)
		tx.EmitContext().SetEmitFlags(updated.Name(), printer.EFNoTrailingSourceMap)
	}
	return updated
}

func (tx *esDecoratorTransformer) visitNamedEvaluationSite(node ast.Handle, classExpr ast.Handle) ast.Handle {
	if isNamedEvaluationAnd(tx.EmitContext(), node, isAnonymousClassNeedingAssignedName) {
		node = transformNamedEvaluation(tx.EmitContext(), node, canIgnoreEmptyStringLiteralInAssignedName(classExpr), "")
	}
	return tx.Visitor().VisitEachChild(node)
}
func isAnonymousClassNeedingAssignedName(node ast.Handle) bool {
	return ast.IsClassExpression(node) && node.Name().IsNil() && isDecoratedClassLike(node)
}

func canIgnoreEmptyStringLiteralInAssignedName(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	innerExpression := ast.SkipOuterExpressions(node, ast.OEKAll)
	return ast.IsClassExpression(innerExpression) && innerExpression.Name().IsNil() && !ast.ClassOrConstructorParameterIsDecorated(false, innerExpression)
}
func (tx *esDecoratorTransformer) visitForStatement(node ast.Handle) ast.Handle {
	f := tx.Factory()
	forStmt := node
	return f.UpdateForStatement(forStmt, tx.discardedVisitor.VisitNode(forStmt.Initializer()), tx.Visitor().VisitNode(forStmt.Condition()), tx.discardedVisitor.VisitNode(forStmt.Incrementor()), tx.EmitContext().VisitIterationBody(forStmt.Statement(), tx.Visitor()))
}
func (tx *esDecoratorTransformer) visitExpressionStatement(node ast.Handle) ast.Handle {
	return tx.discardedVisitor.VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitBinaryExpression(node ast.Handle, discarded bool) ast.Handle {
	f := tx.Factory()
	ec := tx.EmitContext()
	bin := node
	if ast.IsDestructuringAssignment(node) {
		left := tx.visitAssignmentPattern(bin.Left())
		right := tx.Visitor().VisitNode(bin.Right())
		return f.UpdateBinaryExpression(bin, 0, left, ast.Handle{}, bin.OperatorToken(), right)
	}
	if ast.IsAssignmentExpression(node, false) {
		if isNamedEvaluationAnd(ec, node, isAnonymousClassNeedingAssignedName) {
			node = transformNamedEvaluation(ec, node, canIgnoreEmptyStringLiteralInAssignedName(bin.Right()), "")
			return tx.Visitor().VisitEachChild(node)
		}
		if ast.IsSuperProperty(bin.Left()) && !tx.classThis.IsNil() && !tx.classSuper.IsNil() {
			var setterName ast.Handle
			if ast.IsElementAccessExpression(bin.Left()) {
				setterName = tx.Visitor().VisitNode(bin.Left().ElementAccessExpressionArgumentExpression())
			} else if ast.IsPropertyAccessExpression(bin.Left()) && ast.IsIdentifier(bin.Left().PropertyAccessExpressionName()) {
				setterName = f.NewStringLiteralFromNode(bin.Left().PropertyAccessExpressionName())
			}
			if !setterName.IsNil() {
				expression := tx.Visitor().VisitNode(bin.Right())
				if ast.IsCompoundAssignment(bin.OperatorToken().Kind) {
					getterName := setterName
					if !transformers.IsSimpleInlineableExpression(setterName) {
						getterName = f.NewTempVariable()
						ec.AddVariableDeclaration(getterName)
						setterName = f.NewAssignmentExpression(getterName, setterName)
					}
					superPropertyGet := f.NewReflectGetCall(tx.classSuper, getterName, tx.classThis)
					ec.SetOriginal(superPropertyGet, bin.Left())
					superPropertyGet.SetLoc(bin.Left().Loc())
					expression = f.NewBinaryExpression(0, superPropertyGet, ast.Handle{}, f.NewToken(transformers.GetNonAssignmentOperatorForCompoundAssignment(bin.OperatorToken().Kind)), expression)
					expression.SetLoc(node.Loc())
				}
				var temp ast.Handle
				if !discarded {
					temp = f.NewTempVariable()
					ec.AddVariableDeclaration(temp)
				}
				if !temp.IsNil() {
					expression = f.NewAssignmentExpression(temp, expression)
					expression.SetLoc(node.Loc())
				}
				expression = f.NewReflectSetCall(tx.classSuper, setterName, expression, tx.classThis)
				ec.SetOriginal(expression, node)
				expression.SetLoc(node.Loc())
				if !temp.IsNil() {
					expression = f.NewCommaExpression(expression, temp)
					expression.SetLoc(node.Loc())
				}
				return expression
			}
		}
	}
	if bin.OperatorToken().Kind == ast.KindCommaToken {
		left := tx.discardedVisitor.VisitNode(bin.Left())
		var right ast.Handle
		if discarded {
			right = tx.discardedVisitor.VisitNode(bin.Right())
		} else {
			right = tx.Visitor().VisitNode(bin.Right())
		}
		return f.UpdateBinaryExpression(bin, 0, left, ast.Handle{}, bin.OperatorToken(), right)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitPreOrPostfixUnaryExpression(node ast.Handle, discarded bool) ast.Handle {
	f := tx.Factory()
	ec := tx.EmitContext()
	var operator ast.Kind
	var operandNode ast.Handle
	if ast.IsPrefixUnaryExpression(node) {
		operator = node.PrefixUnaryExpressionOperator()
		operandNode = node.PrefixUnaryExpressionOperand()
	} else {
		operator = node.PostfixUnaryExpressionOperator()
		operandNode = node.PostfixUnaryExpressionOperand()
	}
	if operator == ast.KindPlusPlusToken || operator == ast.KindMinusMinusToken {
		operand := ast.SkipParentheses(operandNode)
		if ast.IsSuperProperty(operand) && !tx.classThis.IsNil() && !tx.classSuper.IsNil() {
			var setterName ast.Handle
			if ast.IsElementAccessExpression(operand) {
				setterName = tx.Visitor().VisitNode(operand.ElementAccessExpressionArgumentExpression())
			} else if ast.IsPropertyAccessExpression(operand) && ast.IsIdentifier(operand.PropertyAccessExpressionName()) {
				setterName = f.NewStringLiteralFromNode(operand.PropertyAccessExpressionName())
			}
			if !setterName.IsNil() {
				getterName := setterName
				if !transformers.IsSimpleInlineableExpression(setterName) {
					getterName = f.NewTempVariable()
					ec.AddVariableDeclaration(getterName)
					setterName = f.NewAssignmentExpression(getterName, setterName)
				}
				expression := f.NewReflectGetCall(tx.classSuper, getterName, tx.classThis)
				ec.SetOriginal(expression, node)
				expression.SetLoc(node.Loc())
				var temp ast.Handle
				if !discarded {
					temp = f.NewTempVariable()
					ec.AddVariableDeclaration(temp)
				}
				expression = expandPreOrPostfixIncrementOrDecrementExpression(f, ec, node, expression, temp)
				expression = f.NewReflectSetCall(tx.classSuper, setterName, expression, tx.classThis)
				ec.SetOriginal(expression, node)
				expression.SetLoc(node.Loc())
				if !temp.IsNil() {
					expression = f.NewCommaExpression(expression, temp)
					expression.SetLoc(node.Loc())
				}
				return expression
			}
		}
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitReferencedPropertyName(node ast.Handle) (ast.Handle, ast.Handle) {
	if ast.IsPropertyNameLiteral(node) || ast.IsPrivateIdentifier(node) {
		return tx.Factory().NewStringLiteralFromNode(node), tx.Visitor().VisitNode(node)
	}
	cpn := node
	if ast.IsPropertyNameLiteral(cpn.Expression()) && !ast.IsIdentifier(cpn.Expression()) {
		return tx.Factory().NewStringLiteralFromNode(cpn.Expression()), tx.Visitor().VisitNode(node)
	}
	referencedName := tx.Factory().NewGeneratedNameForNode(node)
	tx.EmitContext().AddVariableDeclaration(referencedName)
	key := tx.Factory().NewPropKeyHelper(tx.Visitor().VisitNode(cpn.Expression()))
	assignment := tx.Factory().NewAssignmentExpression(referencedName, key)
	updatedName := tx.Factory().UpdateComputedPropertyName(cpn, tx.injectPendingExpressions(assignment))
	return referencedName, updatedName
}
func (tx *esDecoratorTransformer) visitPropertyName(node ast.Handle) ast.Handle {
	if ast.IsComputedPropertyName(node) {
		return tx.visitComputedPropertyName(node)
	}
	return tx.Visitor().VisitNode(node)
}
func (tx *esDecoratorTransformer) visitComputedPropertyName(node ast.Handle) ast.Handle {
	cpn := node
	expression := tx.Visitor().VisitNode(cpn.Expression())
	if !transformers.IsSimpleInlineableExpression(expression) {
		expression = tx.injectPendingExpressions(expression)
	}
	return tx.Factory().UpdateComputedPropertyName(cpn, expression)
}
func (tx *esDecoratorTransformer) visitDestructuringAssignmentTarget(node ast.Handle) ast.Handle {
	if ast.IsObjectLiteralExpression(node) || ast.IsArrayLiteralExpression(node) {
		return tx.visitAssignmentPattern(node)
	}
	if ast.IsSuperProperty(node) && !tx.classThis.IsNil() && !tx.classSuper.IsNil() {
		f := tx.Factory()
		ec := tx.EmitContext()
		var propertyName ast.Handle
		if ast.IsElementAccessExpression(node) {
			propertyName = tx.Visitor().VisitNode(node.ElementAccessExpressionArgumentExpression())
		} else if ast.IsPropertyAccessExpression(node) && ast.IsIdentifier(node.PropertyAccessExpressionName()) {
			propertyName = f.NewStringLiteralFromNode(node.PropertyAccessExpressionName())
		}
		if !propertyName.IsNil() {
			paramName := f.NewTempVariable()
			expression := f.NewAssignmentTargetWrapper(paramName, f.NewReflectSetCall(tx.classSuper, propertyName, paramName, tx.classThis))
			ec.SetOriginal(expression, node)
			expression.SetLoc(node.Loc())
			return expression
		}
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitAssignmentElement(node ast.Handle) ast.Handle {
	if ast.IsAssignmentExpression(node, true) {
		f := tx.Factory()
		bin := node
		if isNamedEvaluationAnd(tx.EmitContext(), node, isAnonymousClassNeedingAssignedName) {
			node = transformNamedEvaluation(tx.EmitContext(), node, canIgnoreEmptyStringLiteralInAssignedName(bin.Right()), "")
			bin = node
		}
		assignmentTarget := tx.visitDestructuringAssignmentTarget(bin.Left())
		initializer := tx.Visitor().VisitNode(bin.Right())
		return f.UpdateBinaryExpression(bin, 0, assignmentTarget, ast.Handle{}, bin.OperatorToken(), initializer)
	}
	return tx.visitDestructuringAssignmentTarget(node)
}
func (tx *esDecoratorTransformer) visitAssignmentRestElement(node ast.Handle) ast.Handle {
	se := node
	if ast.IsLeftHandSideExpression(se.Expression()) {
		f := tx.Factory()
		expression := tx.visitDestructuringAssignmentTarget(se.Expression())
		return f.UpdateSpreadElement(se, expression)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitArrayAssignmentElement(node ast.Handle) ast.Handle {
	debug.Assert(ast.IsArrayBindingOrAssignmentElement(node))
	if ast.IsSpreadElement(node) {
		return tx.visitAssignmentRestElement(node)
	}
	if !ast.IsOmittedExpression(node) {
		return tx.visitAssignmentElement(node)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitAssignmentPropertyNode(node ast.Handle) ast.Handle {
	f := tx.Factory()
	pa := node
	name := tx.Visitor().VisitNode(pa.Name())
	if ast.IsAssignmentExpression(pa.Initializer(), true) {
		assignmentElement := tx.visitAssignmentElement(pa.Initializer())
		return f.UpdatePropertyAssignment(pa, 0, name, ast.Handle{}, ast.Handle{}, assignmentElement)
	}
	if ast.IsLeftHandSideExpression(pa.Initializer()) {
		assignmentElement := tx.visitDestructuringAssignmentTarget(pa.Initializer())
		return f.UpdatePropertyAssignment(pa, 0, name, ast.Handle{}, ast.Handle{}, assignmentElement)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitShorthandAssignmentProperty(node ast.Handle) ast.Handle {
	if isNamedEvaluationAnd(tx.EmitContext(), node, isAnonymousClassNeedingAssignedName) {
		node = transformNamedEvaluation(tx.EmitContext(), node, canIgnoreEmptyStringLiteralInAssignedName(node.ShorthandPropertyAssignmentObjectAssignmentInitializer()), "")
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitAssignmentRestProperty(node ast.Handle) ast.Handle {
	sa := node
	if ast.IsLeftHandSideExpression(sa.Expression()) {
		f := tx.Factory()
		expression := tx.visitDestructuringAssignmentTarget(sa.Expression())
		return f.UpdateSpreadAssignment(sa, expression)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitObjectAssignmentElement(node ast.Handle) ast.Handle {
	debug.Assert(ast.IsObjectBindingOrAssignmentElement(node))
	if ast.IsSpreadAssignment(node) {
		return tx.visitAssignmentRestProperty(node)
	}
	if ast.IsShorthandPropertyAssignment(node) {
		return tx.visitShorthandAssignmentProperty(node)
	}
	if ast.IsPropertyAssignment(node) {
		return tx.visitAssignmentPropertyNode(node)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *esDecoratorTransformer) visitAssignmentPattern(node ast.Handle) ast.Handle {
	f := tx.Factory()
	if ast.IsArrayLiteralExpression(node) {
		ale := node
		elements := tx.arrayAssignmentVisitor.VisitNodes(ale.ElementList())
		return f.UpdateArrayLiteralExpression(ale, elements, ale.MultiLine())
	}
	ole := node
	properties := tx.objectAssignmentVisitor.VisitNodes(ole.PropertyList())
	return f.UpdateObjectLiteralExpression(ole, properties, ole.MultiLine())
}
func (tx *esDecoratorTransformer) visitExportAssignment(node ast.Handle) ast.Handle {
	return tx.visitNamedEvaluationSite(node, node.Expression())
}
func (tx *esDecoratorTransformer) visitParenthesizedExpression(node ast.Handle, discarded bool) ast.Handle {
	f := tx.Factory()
	pe := node
	var expression ast.Handle
	if discarded {
		expression = tx.discardedVisitor.VisitNode(pe.Expression())
	} else {
		expression = tx.Visitor().VisitNode(pe.Expression())
	}
	return f.UpdateParenthesizedExpression(pe, expression)
}
func (tx *esDecoratorTransformer) visitPartiallyEmittedExpression(node ast.Handle, discarded bool) ast.Handle {
	pe := node
	var expression ast.Handle
	if discarded {
		expression = tx.discardedVisitor.VisitNode(pe.Expression())
	} else {
		expression = tx.Visitor().VisitNode(pe.Expression())
	}
	return tx.Factory().UpdatePartiallyEmittedExpression(pe, expression)
}

func (tx *esDecoratorTransformer) prependExpressions(pending []ast.Handle, expression ast.Handle) ast.Handle {
	f := tx.Factory()
	if len(pending) == 0 {
		return expression
	}
	if expression.IsNil() {
		return f.InlineExpressions(pending)
	}
	if ast.IsParenthesizedExpression(expression) {
		pe := expression
		exprs := make([]ast.Handle, len(pending)+1)
		copy(exprs, pending)
		exprs[len(pending)] = pe.Expression()
		return f.UpdateParenthesizedExpression(pe, f.InlineExpressions(exprs))
	}
	exprs := make([]ast.Handle, len(pending)+1)
	copy(exprs, pending)
	exprs[len(pending)] = expression
	return f.InlineExpressions(exprs)
}
func (tx *esDecoratorTransformer) injectPendingExpressions(expression ast.Handle) ast.Handle {
	result := tx.prependExpressions(tx.pendingExpressions, expression)
	debug.Assert(!result.IsNil())
	if result != expression {
		tx.pendingExpressions = nil
	}
	return result
}
func (tx *esDecoratorTransformer) injectPendingInitializers(ci *classInfo, isStatic bool, expression ast.Handle) ast.Handle {
	var pending *[]ast.Handle
	if isStatic {
		pending = &ci.pendingStaticInitializers
	} else {
		pending = &ci.pendingInstanceInitializers
	}
	result := tx.prependExpressions(*pending, expression)
	if result != expression {
		*pending = nil
	}
	return result
}

func (tx *esDecoratorTransformer) transformAllDecoratorsOfDeclaration(decorators []ast.Handle) []ast.Handle {
	if len(decorators) == 0 {
		return nil
	}
	result := make([]ast.Handle, 0, len(decorators))
	for _, d := range decorators {
		result = append(result, tx.transformDecorator(d))
	}
	return result
}

func (tx *esDecoratorTransformer) transformDecorator(decorator ast.Handle) ast.Handle {
	expression := tx.Visitor().VisitNode(decorator.DecoratorExpression())
	tx.EmitContext().SetEmitFlags(expression, printer.EFNoComments)
	innerExpression := ast.SkipOuterExpressions(expression, ast.OEKAll)
	if ast.IsAccessExpression(innerExpression) {
		target, thisArg := tx.createCallBinding(expression)
		bindCall := tx.Factory().NewFunctionBindCall(target, thisArg, nil)
		return tx.Factory().RestoreOuterExpressions(expression, bindCall, ast.OEKAll)
	}
	return expression
}
func (tx *esDecoratorTransformer) createCallBinding(expression ast.Handle) (ast.Handle, ast.Handle) {
	f := tx.Factory()
	callee := ast.SkipOuterExpressions(expression, ast.OEKAll)
	if ast.IsSuperProperty(callee) {
		return callee, f.NewThisExpression()
	}
	if callee.Kind == ast.KindSuperKeyword {
		return callee, f.NewThisExpression()
	}
	if tx.EmitContext().EmitFlags(callee)&printer.EFHelperName != 0 {
		return callee, f.NewVoidZeroExpression()
	}
	if ast.IsPropertyAccessExpression(callee) {
		pa := callee
		if tx.shouldBeCapturedInTempVariable(pa.Expression()) {
			thisArg := f.NewTempVariable()
			tx.EmitContext().AddVariableDeclaration(thisArg)
			assign := f.NewAssignmentExpression(thisArg, pa.Expression())
			assign.SetLoc(pa.Expression().Loc())
			target := f.NewPropertyAccessExpression(assign, ast.Handle{}, pa.Name(), ast.NodeFlagsNone)
			target.SetLoc(callee.Loc())
			return target, thisArg
		}
		return callee, pa.Expression()
	}
	if ast.IsElementAccessExpression(callee) {
		ea := callee
		if tx.shouldBeCapturedInTempVariable(ea.Expression()) {
			thisArg := f.NewTempVariable()
			tx.EmitContext().AddVariableDeclaration(thisArg)
			assign := f.NewAssignmentExpression(thisArg, ea.Expression())
			assign.SetLoc(ea.Expression().Loc())
			target := f.NewElementAccessExpression(assign, ast.Handle{}, ea.ArgumentExpression(), ast.NodeFlagsNone)
			target.SetLoc(callee.Loc())
			return target, thisArg
		}
		return callee, ea.Expression()
	}
	return expression, f.NewVoidZeroExpression()
}
func (tx *esDecoratorTransformer) shouldBeCapturedInTempVariable(node ast.Handle) bool {
	target := ast.SkipParentheses(node)
	switch target.Kind {
	case ast.KindIdentifier:
		return true
	case ast.KindThisKeyword, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindStringLiteral:
		return false
	default:
		return true
	}
}

func (tx *esDecoratorTransformer) createDescriptorMethod(original ast.Handle, name ast.Handle, modifiers ast.ListRef, asteriskToken ast.Handle, kind string, parameters ast.ListRef, body ast.Handle) ast.Handle {
	f := tx.Factory()
	ec := tx.EmitContext()
	if body.IsNil() {
		body = f.NewBlock(f.NewList([]ast.Handle{}), false)
	}
	funcExpr := f.NewFunctionExpression(modifiers, asteriskToken, ast.Handle{}, 0, parameters, ast.Handle{}, ast.Handle{}, body)
	ec.SetOriginal(funcExpr, original)
	ec.SetSourceMapRange(funcExpr, transformers.MoveRangePastDecorators(original))
	ec.SetEmitFlags(funcExpr, printer.EFNoComments)
	var prefix string
	if kind == "get" || kind == "set" {
		prefix = kind
	}
	functionName := f.NewStringLiteralFromNode(name)
	namedFunction := f.NewSetFunctionNameHelper(funcExpr, functionName, prefix)
	method := f.NewPropertyAssignment(0, f.NewIdentifier(kind), ast.Handle{}, ast.Handle{}, namedFunction)
	ec.SetOriginal(method, original)
	ec.SetSourceMapRange(method, transformers.MoveRangePastDecorators(original))
	ec.SetEmitFlags(method, printer.EFNoComments)
	return method
}

func (tx *esDecoratorTransformer) createMethodDescriptorObject(member ast.Handle, modifiers ast.ListRef) ast.Handle {
	f := tx.Factory()
	parameters := tx.Visitor().VisitNodes(member.ParameterList())
	body := tx.Visitor().VisitNode(member.Body())
	method := member
	return f.NewObjectLiteralExpression(f.NewList([]ast.Handle{tx.createDescriptorMethod(member, member.Name(), modifiers, method.AsteriskToken(), "value", parameters, body)}), false)
}

func (tx *esDecoratorTransformer) createGetAccessorDescriptorObject(member ast.Handle, modifiers ast.ListRef) ast.Handle {
	f := tx.Factory()
	body := tx.Visitor().VisitNode(member.Body())
	return f.NewObjectLiteralExpression(f.NewList([]ast.Handle{tx.createDescriptorMethod(member, member.Name(), modifiers, ast.Handle{}, "get", f.NewList([]ast.Handle{}), body)}), false)
}

func (tx *esDecoratorTransformer) createSetAccessorDescriptorObject(member ast.Handle, modifiers ast.ListRef) ast.Handle {
	f := tx.Factory()
	parameters := tx.Visitor().VisitNodes(member.ParameterList())
	body := tx.Visitor().VisitNode(member.Body())
	return f.NewObjectLiteralExpression(f.NewList([]ast.Handle{tx.createDescriptorMethod(member, member.Name(), modifiers, ast.Handle{}, "set", parameters, body)}), false)
}

func (tx *esDecoratorTransformer) createAccessorPropertyDescriptorObject(member ast.Handle, _ ast.ListRef) ast.Handle {
	f := tx.Factory()
	backingFieldName := f.NewGeneratedPrivateNameForNodeEx(member.Name(), printer.AutoGenerateOptions{Suffix: "_accessor_storage"})
	return f.NewObjectLiteralExpression(f.NewList([]ast.Handle{tx.createDescriptorMethod(member, member.Name(), 0, ast.Handle{}, "get", f.NewList([]ast.Handle{}), f.NewBlock(f.NewList([]ast.Handle{f.NewReturnStatement(f.NewPropertyAccessExpression(f.NewThisExpression(), ast.Handle{}, backingFieldName, ast.NodeFlagsNone))}), false)), tx.createDescriptorMethod(member, member.Name(), 0, ast.Handle{}, "set", f.NewList([]ast.Handle{f.NewParameterDeclaration(0, ast.Handle{}, f.NewIdentifier("value"), ast.Handle{}, ast.Handle{}, ast.Handle{})}), f.NewBlock(f.NewList([]ast.Handle{f.NewExpressionStatement(f.NewAssignmentExpression(f.NewPropertyAccessExpression(f.NewThisExpression(), ast.Handle{}, backingFieldName, ast.NodeFlagsNone), f.NewIdentifier("value")))}), false))}), false)
}

func (tx *esDecoratorTransformer) createMethodDescriptorForwarder(modifiers ast.ListRef, name ast.Handle, descriptorName ast.Handle) ast.Handle {
	f := tx.Factory()
	staticOnly := tx.staticOnlyModifierVisitor.VisitModifiers(modifiers)
	return f.NewGetAccessorDeclaration(staticOnly, name, 0, f.NewList([]ast.Handle{}), ast.Handle{}, ast.Handle{}, f.NewBlock(f.NewList([]ast.Handle{f.NewReturnStatement(f.NewPropertyAccessExpression(descriptorName, ast.Handle{}, f.NewIdentifier("value"), ast.NodeFlagsNone))}), false))
}

func (tx *esDecoratorTransformer) createGetAccessorDescriptorForwarder(modifiers ast.ListRef, name ast.Handle, descriptorName ast.Handle) ast.Handle {
	f := tx.Factory()
	staticOnly := tx.staticOnlyModifierVisitor.VisitModifiers(modifiers)
	return f.NewGetAccessorDeclaration(staticOnly, name, 0, f.NewList([]ast.Handle{}), ast.Handle{}, ast.Handle{}, f.NewBlock(f.NewList([]ast.Handle{f.NewReturnStatement(f.NewFunctionCallCall(f.NewPropertyAccessExpression(descriptorName, ast.Handle{}, f.NewIdentifier("get"), ast.NodeFlagsNone), f.NewThisExpression(), nil))}), false))
}

func (tx *esDecoratorTransformer) createSetAccessorDescriptorForwarder(modifiers ast.ListRef, name ast.Handle, descriptorName ast.Handle) ast.Handle {
	f := tx.Factory()
	staticOnly := tx.staticOnlyModifierVisitor.VisitModifiers(modifiers)
	return f.NewSetAccessorDeclaration(staticOnly, name, 0, f.NewList([]ast.Handle{f.NewParameterDeclaration(0, ast.Handle{}, f.NewIdentifier("value"), ast.Handle{}, ast.Handle{}, ast.Handle{})}), ast.Handle{}, ast.Handle{}, f.NewBlock(f.NewList([]ast.Handle{f.NewReturnStatement(f.NewFunctionCallCall(f.NewPropertyAccessExpression(descriptorName, ast.Handle{}, f.NewIdentifier("set"), ast.NodeFlagsNone), f.NewThisExpression(), []ast.Handle{f.NewIdentifier("value")}))}), false))
}
func (tx *esDecoratorTransformer) createMetadata(name ast.Handle, classSuper ast.Handle) ast.Handle {
	f := tx.Factory()
	var superMetadata ast.Handle
	if !classSuper.IsNil() {
		superMetadata = tx.createSymbolMetadataReference(classSuper)
	} else {
		superMetadata = f.NewToken(ast.KindNullKeyword)
	}
	objectCreate := f.NewCallExpression(f.NewPropertyAccessExpression(f.NewIdentifier("Object"), ast.Handle{}, f.NewIdentifier("create"), ast.NodeFlagsNone), ast.Handle{}, 0, f.NewList([]ast.Handle{superMetadata}), ast.NodeFlagsNone)
	symbolCheck := f.NewLogicalANDExpression(f.NewTypeCheck(f.NewIdentifier("Symbol"), "function"), f.NewPropertyAccessExpression(f.NewIdentifier("Symbol"), ast.Handle{}, f.NewIdentifier("metadata"), ast.NodeFlagsNone))
	conditional := f.NewConditionalExpression(symbolCheck, f.NewToken(ast.KindQuestionToken), objectCreate, f.NewToken(ast.KindColonToken), f.NewVoidZeroExpression())
	varDecl := f.NewVariableDeclaration(name, ast.Handle{}, ast.Handle{}, conditional)
	varDeclList := f.NewVariableDeclarationList(f.NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst)
	return f.NewVariableStatement(0, varDeclList)
}
func (tx *esDecoratorTransformer) createSymbolMetadata(target ast.Handle, value ast.Handle) ast.Handle {
	f := tx.Factory()
	symbolMetadata := f.NewPropertyAccessExpression(f.NewIdentifier("Symbol"), ast.Handle{}, f.NewIdentifier("metadata"), ast.NodeFlagsNone)
	descriptorProps := []ast.Handle{f.NewPropertyAssignment(0, f.NewIdentifier("enumerable"), ast.Handle{}, ast.Handle{}, f.NewTrueExpression()), f.NewPropertyAssignment(0, f.NewIdentifier("configurable"), ast.Handle{}, ast.Handle{}, f.NewTrueExpression()), f.NewPropertyAssignment(0, f.NewIdentifier("writable"), ast.Handle{}, ast.Handle{}, f.NewTrueExpression()), f.NewPropertyAssignment(0, f.NewIdentifier("value"), ast.Handle{}, ast.Handle{}, value)}
	descriptor := f.NewObjectLiteralExpression(f.NewList(descriptorProps), false)
	defineProperty := f.NewCallExpression(f.NewPropertyAccessExpression(f.NewIdentifier("Object"), ast.Handle{}, f.NewIdentifier("defineProperty"), ast.NodeFlagsNone), ast.Handle{}, 0, f.NewList([]ast.Handle{target, symbolMetadata, descriptor}), ast.NodeFlagsNone)
	ifStatement := f.NewIfStatement(value, f.NewExpressionStatement(defineProperty), ast.Handle{})
	tx.EmitContext().SetEmitFlags(ifStatement, printer.EFSingleLine)
	return ifStatement
}
func (tx *esDecoratorTransformer) createSymbolMetadataReference(classSuper ast.Handle) ast.Handle {
	f := tx.Factory()
	symbolMetadata := f.NewPropertyAccessExpression(f.NewIdentifier("Symbol"), ast.Handle{}, f.NewIdentifier("metadata"), ast.NodeFlagsNone)
	elementAccess := f.NewElementAccessExpression(classSuper, ast.Handle{}, symbolMetadata, ast.NodeFlagsNone)
	return f.NewBinaryExpression(0, elementAccess, ast.Handle{}, f.NewToken(ast.KindQuestionQuestionToken), f.NewToken(ast.KindNullKeyword))
}
func injectClassThisAssignmentIfMissing(ec *printer.EmitContext, f *printer.NodeFactory, node ast.Handle, classThis ast.Handle) ast.Handle {
	if classHasClassThisAssignment(ec, node) {
		return node
	}
	expression := f.NewAssignmentExpression(classThis, f.NewThisExpression())
	statement := f.NewExpressionStatement(expression)
	body := f.NewBlock(f.NewList([]ast.Handle{statement}), false)
	staticBlock := f.NewClassStaticBlockDeclaration(0, body)
	ec.SetClassThis(staticBlock, classThis)
	if !node.Name().IsNil() {
		ec.SetSourceMapRange(statement, node.Name().Loc())
	}
	newMembers := make([]ast.Handle, 0, 1+len(node.Members()))
	newMembers = append(newMembers, staticBlock)
	newMembers = append(newMembers, node.Members()...)
	membersList := f.List(node.Store().ListLoc(node.MemberList()), newMembers...)
	var updatedNode ast.Handle
	if ast.IsClassDeclaration(node) {
		cd := node
		updatedNode = f.UpdateClassDeclaration(cd, cd.Modifiers(), cd.Name(), 0, cd.HeritageClauses(), membersList)
	} else {
		ce := node
		updatedNode = f.UpdateClassExpression(ce, ce.Modifiers(), ce.Name(), 0, ce.HeritageClauses(), membersList)
	}
	ec.SetClassThis(updatedNode, classThis)
	return updatedNode
}
