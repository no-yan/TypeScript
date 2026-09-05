package jsxtransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type JSXTransformer struct {
	transformers.Transformer
	compilerOptions     *core.CompilerOptions
	emitResolver        printer.EmitResolver
	importSpecifier     string
	filenameDeclaration ast.Handle
	utilizedImplicitRuntimeImports collections.OrderedMap[string, map[string]ast.Handle]
	inJsxChild                     bool
	currentSourceFile              *ast.SourceFile
}

func NewJSXTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	compilerOptions := opts.CompilerOptions
	emitContext := opts.Context
	tx := &JSXTransformer{compilerOptions: compilerOptions, emitResolver: opts.EmitResolver}
	return tx.NewTransformer(tx.visit, emitContext)
}
func (tx *JSXTransformer) getCurrentFileNameExpression() ast.Handle {
	if !tx.filenameDeclaration.IsNil() {
		return tx.filenameDeclaration.VariableDeclarationName()
	}
	d := tx.Factory().NewVariableDeclaration(tx.Factory().NewUniqueNameEx("_jsxFileName", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel}), ast.Handle{}, ast.Handle{}, tx.Factory().NewStringLiteral(tx.currentSourceFile.FileName(), ast.TokenFlagsNone))
	tx.filenameDeclaration = d
	return d.VariableDeclarationName()
}
func (tx *JSXTransformer) getJsxFactoryCalleePrimitive(isStaticChildren bool) string {
	if tx.compilerOptions.Jsx == core.JsxEmitReactJSXDev {
		return "jsxDEV"
	}
	if isStaticChildren {
		return "jsxs"
	}
	return "jsx"
}
func (tx *JSXTransformer) getJsxFactoryCallee(isStaticChildren bool) ast.Handle {
	t := tx.getJsxFactoryCalleePrimitive(isStaticChildren)
	return tx.getImplicitImportForName(t)
}
func (tx *JSXTransformer) getImplicitJsxFragmentReference() ast.Handle {
	return tx.getImplicitImportForName("Fragment")
}
func (tx *JSXTransformer) getImplicitImportForName(name string) ast.Handle {
	importSource := tx.importSpecifier
	if name != "createElement" {
		importSource = ast.GetJSXRuntimeImport(importSource, tx.compilerOptions)
	}
	existing, ok := tx.utilizedImplicitRuntimeImports.Get(importSource)
	if ok {
		elem, ok := existing[name]
		if ok {
			return elem.ImportSpecifierName()
		}
	} else {
		existing = make(map[string]ast.Handle)
		tx.utilizedImplicitRuntimeImports.Set(importSource, existing)
	}
	generatedName := tx.Factory().NewUniqueNameEx("_"+name, printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel | printer.GeneratedIdentifierFlagsAllowNameSubstitution})
	specifier := tx.Factory().NewImportSpecifier(false, tx.Factory().NewIdentifier(name), generatedName)
	tx.emitResolver.SetReferencedImportDeclaration(generatedName, specifier)
	existing[name] = specifier
	return specifier.Name()
}
func (tx *JSXTransformer) setInChild(v bool) {
	tx.inJsxChild = v
}
func (tx *JSXTransformer) visit(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	if node.SubtreeFacts()&ast.SubtreeContainsJsx == 0 {
		return node
	}
	switch node.Kind {
	case ast.KindSourceFile:
		tx.setInChild(false)
		return tx.visitSourceFile(node)
	case ast.KindJsxElement:
		return tx.visitJsxElement(node)
	case ast.KindJsxSelfClosingElement:
		return tx.visitJsxSelfClosingElement(node)
	case ast.KindJsxFragment:
		return tx.visitJsxFragment(node)
	case ast.KindJsxOpeningElement:
		panic("JsxOpeningElement should not be visited, handled in visitJsxElement")
	case ast.KindJsxOpeningFragment:
		panic("JsxOpeningFragment should not be visited, handled in visitJsxFragment")
	case ast.KindJsxText:
		tx.setInChild(false)
		return tx.visitJsxText(node)
	case ast.KindJsxExpression:
		tx.setInChild(false)
		return tx.visitJsxExpression(node)
	}
	tx.setInChild(false)
	return tx.Visitor().VisitEachChild(node)
}

func hasKeyAfterPropsSpread(node ast.Handle) bool {
	spread := false
	opener := node
	if node.Kind == ast.KindJsxElement {
		opener = node.JsxElementOpeningElement()
	}
	for _, elem := range opener.Attributes().Properties() {
		if ast.IsJsxSpreadAttribute(elem) && (!ast.IsObjectLiteralExpression(elem.Expression()) || core.Some(elem.Expression().Properties(), ast.IsSpreadAssignment)) {
			spread = true
		} else if spread && ast.IsJsxAttribute(elem) && ast.IsIdentifier(elem.Name()) && elem.Name().Text() == "key" {
			return true
		}
	}
	return false
}
func (tx *JSXTransformer) shouldUseCreateElement(node ast.Handle) bool {
	return len(tx.importSpecifier) == 0 || hasKeyAfterPropsSpread(node)
}
func insertStatementAfterPrologue[T any](to []ast.Handle, statement ast.Handle, isPrologueDirective func(callee T, node ast.Handle) bool, callee T) []ast.Handle {
	if statement.IsNil() {
		return to
	}
	statementIdx := 0
	for ; statementIdx < len(to); statementIdx++ {
		if !isPrologueDirective(callee, to[statementIdx]) {
			break
		}
	}
	return slices.Insert(to, statementIdx, statement)
}
func (tx *JSXTransformer) isAnyPrologueDirective(node ast.Handle) bool {
	return ast.IsPrologueDirective(node) || (tx.EmitContext().EmitFlags(node)&printer.EFCustomPrologue != 0)
}
func (tx *JSXTransformer) insertStatementAfterCustomPrologue(to []ast.Handle, statement ast.Handle) []ast.Handle {
	return insertStatementAfterPrologue(to, statement, (*JSXTransformer).isAnyPrologueDirective, tx)
}
func sortImportSpecifiers(a ast.Handle, b ast.Handle) int {
	res := stringutil.CompareStringsCaseSensitive(a.PropertyName().Text(), b.PropertyName().Text())
	if res != 0 {
		return res
	}
	return stringutil.CompareStringsCaseSensitive(a.ImportSpecifierName().Text(), b.ImportSpecifierName().Text())
}
func getSortedSpecifiers(m map[string]ast.Handle) []ast.Handle {
	res := slices.Collect(maps.Values(m))
	slices.SortFunc(res, sortImportSpecifiers)
	return res
}
func (tx *JSXTransformer) visitSourceFile(file ast.Handle) ast.Handle {
	if ast.GetSourceFileOfNode(file) != nil && ast.GetSourceFileOfNode(file).IsDeclarationFile {
		return file
	}
	tx.currentSourceFile = ast.GetSourceFileOfNode(file)
	tx.importSpecifier = ast.GetJSXImplicitImportBase(tx.compilerOptions, tx.currentSourceFile)
	tx.filenameDeclaration = ast.Handle{}
	tx.utilizedImplicitRuntimeImports.Clear()
	visited := tx.Visitor().VisitEachChild(file)
	tx.EmitContext().AddEmitHelper(visited, tx.EmitContext().ReadEmitHelpers()...)
	statements := visited.Statements()
	statementsUpdated := false
	if !tx.filenameDeclaration.IsNil() {
		statements = tx.insertStatementAfterCustomPrologue(statements, tx.Factory().NewVariableStatement(0, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.filenameDeclaration}), ast.NodeFlagsConst)))
		statementsUpdated = true
	}
	if tx.utilizedImplicitRuntimeImports.Size() > 0 {
		if ast.IsExternalModule(tx.currentSourceFile) {
			statementsUpdated = true
			newStatements := make([]ast.Handle, 0, tx.utilizedImplicitRuntimeImports.Size())
			for importSource, importSpecifiersMap := range tx.utilizedImplicitRuntimeImports.Entries() {
				s := tx.Factory().NewImportDeclaration(0, tx.Factory().NewImportClause(ast.KindUnknown, ast.Handle{}, tx.Factory().NewNamedImports(tx.Factory().NewList(getSortedSpecifiers(importSpecifiersMap)))), tx.Factory().NewStringLiteral(importSource, ast.TokenFlagsNone), ast.Handle{})
				s.SetParentsInChildren()
				newStatements = append(newStatements, s)
			}
			for _, e := range newStatements {
				statements = tx.insertStatementAfterCustomPrologue(statements, e)
			}
		} else if ast.IsExternalOrCommonJSModule(tx.currentSourceFile) {
			statementsUpdated = true
			newStatements := make([]ast.Handle, 0, tx.utilizedImplicitRuntimeImports.Size())
			for importSource, importSpecifiersMap := range tx.utilizedImplicitRuntimeImports.Entries() {
				sorted := getSortedSpecifiers(importSpecifiersMap)
				asBindingElems := make([]ast.Handle, 0, len(sorted))
				for _, elem := range sorted {
					asBindingElems = append(asBindingElems, tx.Factory().NewBindingElement(ast.Handle{}, elem.PropertyName(), elem.ImportSpecifierName(), ast.Handle{}))
				}
				s := tx.Factory().NewVariableStatement(0, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableDeclaration(tx.Factory().NewBindingPattern(ast.KindObjectBindingPattern, tx.Factory().NewList(asBindingElems)), ast.Handle{}, ast.Handle{}, tx.Factory().NewCallExpression(tx.Factory().NewIdentifier("require"), ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{tx.Factory().NewStringLiteral(importSource, ast.TokenFlagsNone)}), ast.NodeFlagsNone))}), ast.NodeFlagsConst))
				s.SetParentsInChildren()
				newStatements = append(newStatements, s)
			}
			for _, e := range newStatements {
				statements = tx.insertStatementAfterCustomPrologue(statements, e)
			}
		} else {
		}
	}
	if statementsUpdated {
		visited = tx.Factory().UpdateSourceFile(file, tx.Factory().NewList(statements), file.EndOfFileToken())
	}
	tx.currentSourceFile = nil
	tx.importSpecifier = ""
	tx.filenameDeclaration = ast.Handle{}
	tx.utilizedImplicitRuntimeImports.Clear()
	return visited
}
func (tx *JSXTransformer) visitJsxElement(element ast.Handle) ast.Handle {
	tagTransform := (*JSXTransformer).visitJsxOpeningLikeElementJSX
	if tx.shouldUseCreateElement(element) {
		tagTransform = (*JSXTransformer).visitJsxOpeningLikeElementCreateElement
	}
	location := core.NewTextRange(scanner.SkipTrivia(tx.currentSourceFile.Text(), element.Pos()), element.End())
	return tagTransform(tx, element.OpeningElement(), element.ChildList(), location)
}
func (tx *JSXTransformer) visitJsxSelfClosingElement(element ast.Handle) ast.Handle {
	tagTransform := (*JSXTransformer).visitJsxOpeningLikeElementJSX
	if tx.shouldUseCreateElement(element) {
		tagTransform = (*JSXTransformer).visitJsxOpeningLikeElementCreateElement
	}
	location := core.NewTextRange(scanner.SkipTrivia(tx.currentSourceFile.Text(), element.Pos()), element.End())
	return tagTransform(tx, element, 0, location)
}
func (tx *JSXTransformer) visitJsxFragment(fragment ast.Handle) ast.Handle {
	tagTransform := (*JSXTransformer).visitJsxOpeningFragmentJSX
	if len(tx.importSpecifier) == 0 {
		tagTransform = (*JSXTransformer).visitJsxOpeningFragmentCreateElement
	}
	location := core.NewTextRange(scanner.SkipTrivia(tx.currentSourceFile.Text(), fragment.Pos()), fragment.End())
	return tagTransform(tx, fragment.OpeningFragment(), fragment.ChildList(), location)
}
func (tx *JSXTransformer) convertJsxChildrenToChildrenPropObject(children []ast.Handle) ast.Handle {
	prop := tx.convertJsxChildrenToChildrenPropAssignment(children)
	if prop.IsNil() {
		return ast.Handle{}
	}
	return tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList([]ast.Handle{prop}), false)
}
func (tx *JSXTransformer) transformJsxChildToExpression(node ast.Handle) ast.Handle {
	prev := tx.inJsxChild
	tx.setInChild(true)
	defer tx.setInChild(prev)
	return tx.Visitor().Visit(node)
}
func (tx *JSXTransformer) convertJsxChildrenToChildrenPropAssignment(children []ast.Handle) ast.Handle {
	nonWhitespceChildren := ast.GetSemanticJsxChildren(children)
	if len(nonWhitespceChildren) == 1 && (nonWhitespceChildren[0].Kind != ast.KindJsxExpression || nonWhitespceChildren[0].JsxExpressionDotDotDotToken().IsNil()) {
		result := tx.transformJsxChildToExpression(nonWhitespceChildren[0])
		if result.IsNil() {
			return ast.Handle{}
		}
		return tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("children"), ast.Handle{}, ast.Handle{}, result)
	}
	results := make([]ast.Handle, 0, len(nonWhitespceChildren))
	for _, child := range nonWhitespceChildren {
		res := tx.transformJsxChildToExpression(child)
		if res.IsNil() {
			continue
		}
		tx.EmitContext().SetEmitFlags(res, tx.EmitContext().EmitFlags(res) & ^printer.EFStartOnNewLine)
		results = append(results, res)
	}
	if len(results) == 0 {
		return ast.Handle{}
	}
	return tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("children"), ast.Handle{}, ast.Handle{}, tx.Factory().NewArrayLiteralExpression(tx.Factory().NewList(results), false))
}
func (tx *JSXTransformer) getTagName(node ast.Handle) ast.Handle {
	if node.Kind == ast.KindJsxElement {
		return tx.getTagName(node.JsxElementOpeningElement())
	} else if ast.IsJsxOpeningLikeElement(node) {
		tagName := node.TagName()
		if ast.IsIdentifier(tagName) && scanner.IsIntrinsicJsxName(tagName.Text()) {
			return tx.Factory().NewStringLiteral(tagName.Text(), ast.TokenFlagsNone)
		} else if ast.IsJsxNamespacedName(tagName) {
			return tx.Factory().NewStringLiteral(tagName.JsxNamespacedNameNamespace().Text()+":"+tagName.JsxNamespacedNameName().Text(), ast.TokenFlagsNone)
		} else {
			return tx.Factory().CreateExpressionFromEntityName(tagName)
		}
	} else {
		panic("unhandled node kind passed to getTagName: " + node.Kind.String())
	}
}
func (tx *JSXTransformer) visitJsxOpeningLikeElementJSX(element ast.Handle, children ast.ListRef, location core.TextRange) ast.Handle {
	tagName := tx.getTagName(element)
	var childrenProp ast.Handle
	if children != 0 && element.Store().ListLen(children) > 0 {
		childrenProp = tx.convertJsxChildrenToChildrenPropAssignment(element.Store().ListSlice(children))
	}
	var keyAttr ast.Handle
	attrs := element.Attributes().Properties()
	for i, p := range attrs {
		if p.Kind == ast.KindJsxAttribute && !p.JsxAttributeName().IsNil() && ast.IsIdentifier(p.JsxAttributeName()) && p.JsxAttributeName().Text() == "key" {
			keyAttr = p
			attrs = slices.Clone(attrs)
			attrs = slices.Delete(attrs, i, i+1)
			break
		}
	}
	var object ast.Handle
	if len(attrs) > 0 {
		object = tx.transformJsxAttributesToObjectProps(attrs, childrenProp)
	} else {
		objectChildren := []ast.Handle{}
		if !childrenProp.IsNil() {
			objectChildren = append(objectChildren, childrenProp)
		}
		object = tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList(objectChildren), false)
	}
	return tx.visitJsxOpeningLikeElementOrFragmentJSX(tagName, object, keyAttr, children, location)
}
func (tx *JSXTransformer) transformJsxAttributesToObjectProps(attrs []ast.Handle, childrenProp ast.Handle) ast.Handle {
	target := tx.compilerOptions.GetEmitScriptTarget()
	if target >= core.ScriptTargetES2018 {
		return tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList(tx.transformJsxAttributesToProps(attrs, childrenProp)), false)
	}
	return tx.transformJsxAttributesToExpression(attrs, childrenProp)
}
func (tx *JSXTransformer) transformJsxAttributesToExpression(attrs []ast.Handle, childrenProp ast.Handle) ast.Handle {
	expressions := make([]ast.Handle, 0, 2)
	properties := make([]ast.Handle, 0, len(attrs))
	for _, attr := range attrs {
		if ast.IsJsxSpreadAttribute(attr) {
			if ast.IsObjectLiteralExpression(attr.Expression()) && !hasProto(attr.Expression()) {
				for _, prop := range attr.Expression().Properties() {
					if ast.IsSpreadAssignment(prop) {
						expressions, properties = tx.combinePropertiesIntoNewExpression(expressions, properties)
						expressions = append(expressions, tx.Visitor().Visit(prop.Expression()))
						continue
					}
					properties = append(properties, tx.Visitor().Visit(prop))
				}
				continue
			}
			expressions, properties = tx.combinePropertiesIntoNewExpression(expressions, properties)
			expressions = append(expressions, tx.Visitor().Visit(attr.Expression()))
			continue
		}
		properties = append(properties, tx.transformJsxAttributeToObjectLiteralElement(attr))
	}
	if !childrenProp.IsNil() {
		properties = append(properties, childrenProp)
	}
	expressions, _ = tx.combinePropertiesIntoNewExpression(expressions, properties)
	if len(expressions) > 0 && !ast.IsObjectLiteralExpression(expressions[0]) {
		expressions = append([]ast.Handle{tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList([]ast.Handle{}), false)}, expressions...)
	}
	if len(expressions) == 1 {
		return expressions[0]
	}
	return tx.Factory().NewAssignHelper(expressions, tx.compilerOptions.GetEmitScriptTarget())
}
func (tx *JSXTransformer) combinePropertiesIntoNewExpression(expressions []ast.Handle, props []ast.Handle) ([]ast.Handle, []ast.Handle) {
	if len(props) == 0 {
		return expressions, props
	}
	newObj := tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList(props), false)
	expressions = append(expressions, newObj)
	return expressions, nil
}
func (tx *JSXTransformer) transformJsxAttributesToProps(attrs []ast.Handle, childrenProp ast.Handle) []ast.Handle {
	props := make([]ast.Handle, 0, len(attrs))
	for _, attr := range attrs {
		if attr.Kind == ast.KindJsxSpreadAttribute {
			res := tx.transformJsxSpreadAttributesToProps(attr)
			props = append(props, res...)
		} else {
			props = append(props, tx.transformJsxAttributeToObjectLiteralElement(attr))
		}
	}
	if !childrenProp.IsNil() {
		props = append(props, childrenProp)
	}
	return props
}
func hasProto(obj ast.Handle) bool {
	for _, p := range obj.Properties() {
		if ast.IsPropertyAssignment(p) && (ast.IsStringLiteral(p.Name()) || ast.IsIdentifier(p.Name())) && p.Name().Text() == "__proto__" {
			return true
		}
	}
	return false
}
func (tx *JSXTransformer) transformJsxSpreadAttributesToProps(node ast.Handle) []ast.Handle {
	if ast.IsObjectLiteralExpression(node.Expression()) && !hasProto(node.Expression()) {
		res := tx.Visitor().VisitSlice(node.Expression().Properties())
		return res
	}
	return []ast.Handle{tx.Factory().NewSpreadAssignment(tx.Visitor().Visit(node.Expression()))}
}
func (tx *JSXTransformer) transformJsxAttributeToObjectLiteralElement(node ast.Handle) ast.Handle {
	name := tx.getAttributeName(node)
	expression := tx.transformJsxAttributeInitializer(node.Initializer())
	return tx.Factory().NewPropertyAssignment(0, name, ast.Handle{}, ast.Handle{}, expression)
}

func (tx *JSXTransformer) getAttributeName(node ast.Handle) ast.Handle {
	name := node.Name()
	if ast.IsIdentifier(name) {
		text := name.Text()
		if scanner.IsIdentifierText(text, core.LanguageVariantStandard) {
			return name
		}
		return tx.Factory().NewStringLiteral(text, ast.TokenFlagsNone)
	}
	return tx.Factory().NewStringLiteral(name.JsxNamespacedNameNamespace().Text()+":"+name.JsxNamespacedNameName().Text(), ast.TokenFlagsNone)
}
func (tx *JSXTransformer) transformJsxAttributeInitializer(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return tx.Factory().NewTrueExpression()
	}
	if node.Kind == ast.KindStringLiteral {
		res := tx.Factory().NewStringLiteral(decodeEntities(node.Text()), node.StringLiteralTokenFlags())
		res.SetLoc(node.Loc())
		res.SetStringLiteralTokenFlags(node.StringLiteralTokenFlags())
		return res
	}
	if node.Kind == ast.KindJsxExpression {
		if node.Expression().IsNil() {
			return tx.Factory().NewTrueExpression()
		}
		return tx.Visitor().Visit(node.Expression())
	}
	if ast.IsJsxElement(node) || ast.IsJsxSelfClosingElement(node) || ast.IsJsxFragment(node) {
		tx.setInChild(false)
		return tx.Visitor().Visit(node)
	}
	panic("Unhandled node kind found in jsx initializer: " + node.Kind.String())
}
func (tx *JSXTransformer) visitJsxOpeningLikeElementOrFragmentJSX(tagName ast.Handle, object ast.Handle, keyAttr ast.Handle, children ast.ListRef, location core.TextRange) ast.Handle {
	var nonWhitespaceChildren []ast.Handle
	if children != 0 {
		nonWhitespaceChildren = ast.GetSemanticJsxChildren(tagName.Store().ListSlice(children))
	}
	isStaticChildren := len(nonWhitespaceChildren) > 1 || (len(nonWhitespaceChildren) == 1 && ast.IsJsxExpression(nonWhitespaceChildren[0]) && !nonWhitespaceChildren[0].JsxExpressionDotDotDotToken().IsNil())
	args := make([]ast.Handle, 0, 3)
	args = append(args, tagName, object)
	if !keyAttr.IsNil() {
		args = append(args, tx.transformJsxAttributeInitializer(keyAttr.Initializer()))
	}
	if tx.compilerOptions.Jsx == core.JsxEmitReactJSXDev {
		originalFile := tx.EmitContext().MostOriginal(tx.currentSourceFile.ParseRoot())
		if !originalFile.IsNil() && ast.IsSourceFile(originalFile) {
			if keyAttr.IsNil() {
				args = append(args, tx.Factory().NewVoidZeroExpression())
			}
			if isStaticChildren {
				args = append(args, tx.Factory().NewTrueExpression())
			} else {
				args = append(args, tx.Factory().NewFalseExpression())
			}
			line, col := scanner.GetECMALineAndUTF16CharacterOfPosition(ast.GetSourceFileOfNode(originalFile), location.Pos())
			args = append(args, tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList([]ast.Handle{tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("fileName"), ast.Handle{}, ast.Handle{}, tx.getCurrentFileNameExpression()), tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("lineNumber"), ast.Handle{}, ast.Handle{}, tx.Factory().NewNumericLiteral(strconv.FormatInt(int64(line+1), 10), ast.TokenFlagsNone)), tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("columnNumber"), ast.Handle{}, ast.Handle{}, tx.Factory().NewNumericLiteral(strconv.FormatInt(int64(col)+1, 10), ast.TokenFlagsNone))}), false))
			args = append(args, tx.Factory().NewThisExpression())
		}
	}
	element := tx.Factory().NewCallExpression(tx.getJsxFactoryCallee(isStaticChildren), ast.Handle{}, 0, tx.Factory().NewList(args), ast.NodeFlagsNone)
	element.SetLoc(location)
	if tx.inJsxChild {
		tx.EmitContext().AddEmitFlags(element, printer.EFStartOnNewLine)
	}
	return element
}
func (tx *JSXTransformer) visitJsxOpeningFragmentJSX(fragment ast.Handle, children ast.ListRef, location core.TextRange) ast.Handle {
	var childrenProps ast.Handle
	if children != 0 && fragment.Store().ListLen(children) > 0 {
		result := tx.convertJsxChildrenToChildrenPropObject(fragment.Store().ListSlice(children))
		if !result.IsNil() {
			childrenProps = result
		}
	}
	if childrenProps.IsNil() {
		childrenProps = tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList([]ast.Handle{}), false)
	}
	return tx.visitJsxOpeningLikeElementOrFragmentJSX(tx.getImplicitJsxFragmentReference(), childrenProps, ast.Handle{}, children, location)
}
func (tx *JSXTransformer) createReactNamespace(reactNamespace string, parent ast.Handle) ast.Handle {
	if len(reactNamespace) == 0 {
		reactNamespace = "React"
	}
	react := tx.Factory().NewIdentifier(reactNamespace)
	react.SetFlags(react.Flags() &^ ast.NodeFlagsSynthesized)
	react.SetParent(tx.EmitContext().ParseNode(parent))
	if container := tx.emitResolver.GetReferencedExportContainer(react, false); !container.IsNil() && ast.IsModuleDeclaration(container) {
		containerName := tx.Factory().NewGeneratedNameForNode(container)
		return tx.Factory().NewPropertyAccessExpression(containerName, ast.Handle{}, react, ast.NodeFlagsNone)
	}
	return react
}
func (tx *JSXTransformer) createJsxFactoryExpressionFromEntityName(e ast.Handle, parent ast.Handle) ast.Handle {
	if e.Kind == ast.KindQualifiedName {
		left := tx.createJsxFactoryExpressionFromEntityName(e.QualifiedNameLeft(), parent)
		right := tx.Factory().NewIdentifier(e.QualifiedNameRight().IdentifierText())
		return tx.Factory().NewPropertyAccessExpression(left, ast.Handle{}, right, ast.NodeFlagsNone)
	}
	return tx.createReactNamespace(e.IdentifierText(), parent)
}
func (tx *JSXTransformer) createJsxPseudoFactoryExpression(parent ast.Handle, e ast.Handle, target string) ast.Handle {
	if !e.IsNil() {
		return tx.createJsxFactoryExpressionFromEntityName(e, parent)
	}
	return tx.Factory().NewPropertyAccessExpression(tx.createReactNamespace(tx.compilerOptions.ReactNamespace, parent), ast.Handle{}, tx.Factory().NewIdentifier(target), ast.NodeFlagsNone)
}
func (tx *JSXTransformer) createJsxFactoryExpression(parent ast.Handle) ast.Handle {
	e := tx.emitResolver.GetJsxFactoryEntity(tx.currentSourceFile.ParseRoot())
	return tx.createJsxPseudoFactoryExpression(parent, e, "createElement")
}
func (tx *JSXTransformer) createJsxFragmentFactoryExpression(parent ast.Handle) ast.Handle {
	e := tx.emitResolver.GetJsxFragmentFactoryEntity(tx.currentSourceFile.ParseRoot())
	return tx.createJsxPseudoFactoryExpression(parent, e, "Fragment")
}
func (tx *JSXTransformer) visitJsxOpeningLikeElementCreateElement(element ast.Handle, children ast.ListRef, location core.TextRange) ast.Handle {
	tagName := tx.getTagName(element)
	attrs := element.Attributes().Properties()
	var objectProperties ast.Handle
	if len(attrs) > 0 {
		objectProperties = tx.transformJsxAttributesToObjectProps(attrs, ast.Handle{})
	} else {
		objectProperties = tx.Factory().NewKeywordExpression(ast.KindNullKeyword)
	}
	var callee ast.Handle
	if len(tx.importSpecifier) == 0 {
		callee = tx.createJsxFactoryExpression(element)
	} else {
		callee = tx.getImplicitImportForName("createElement")
	}
	var newChildren []ast.Handle
	if children != 0 && element.Store().ListLen(children) > 0 {
		for _, c := range element.Store().ListSlice(children) {
			res := tx.transformJsxChildToExpression(c)
			if !res.IsNil() {
				newChildren = append(newChildren, res)
			}
		}
	}
	if len(newChildren) > 1 {
		for _, child := range newChildren {
			tx.EmitContext().AddEmitFlags(child, printer.EFStartOnNewLine)
		}
	}
	args := make([]ast.Handle, 0, len(newChildren)+2)
	args = append(args, tagName)
	args = append(args, objectProperties)
	args = append(args, newChildren...)
	result := tx.Factory().NewCallExpression(callee, ast.Handle{}, 0, tx.Factory().NewList(args), ast.NodeFlagsNone)
	result.SetLoc(location)
	if tx.inJsxChild {
		tx.EmitContext().AddEmitFlags(result, printer.EFStartOnNewLine)
	}
	return result
}
func (tx *JSXTransformer) visitJsxOpeningFragmentCreateElement(fragment ast.Handle, children ast.ListRef, location core.TextRange) ast.Handle {
	tagName := tx.createJsxFragmentFactoryExpression(fragment)
	callee := tx.createJsxFactoryExpression(fragment)
	var newChildren []ast.Handle
	if children != 0 && fragment.Store().ListLen(children) > 0 {
		for _, c := range fragment.Store().ListSlice(children) {
			res := tx.transformJsxChildToExpression(c)
			if !res.IsNil() {
				newChildren = append(newChildren, res)
			}
		}
	}
	if len(newChildren) > 1 {
		for _, child := range newChildren {
			tx.EmitContext().AddEmitFlags(child, printer.EFStartOnNewLine)
		}
	}
	args := make([]ast.Handle, 0, len(newChildren)+2)
	args = append(args, tagName)
	args = append(args, tx.Factory().NewKeywordExpression(ast.KindNullKeyword))
	args = append(args, newChildren...)
	result := tx.Factory().NewCallExpression(callee, ast.Handle{}, 0, tx.Factory().NewList(args), ast.NodeFlagsNone)
	result.SetLoc(location)
	if tx.inJsxChild {
		tx.EmitContext().AddEmitFlags(result, printer.EFStartOnNewLine)
	}
	return result
}
func (tx *JSXTransformer) visitJsxText(text ast.Handle) ast.Handle {
	fixed := fixupWhitespaceAndDecodeEntities(text.Text())
	if len(fixed) == 0 {
		return ast.Handle{}
	}
	return tx.Factory().NewStringLiteral(fixed, ast.TokenFlagsNone)
}
func addLineOfJsxText(b *strings.Builder, trimmedLine string, isInitial bool) {
	decoded := decodeEntities(trimmedLine)
	if !isInitial {
		b.WriteString(" ")
	}
	b.WriteString(decoded)
}

func fixupWhitespaceAndDecodeEntities(text string) string {
	acc := &strings.Builder{}
	initial := true
	firstNonWhitespace := 0
	lastNonWhitespaceEnd := -1
	for i := 0; i < len(text); i++ {
		c, size := utf8.DecodeRuneInString(text[i:])
		if stringutil.IsLineBreak(c) {
			if firstNonWhitespace != -1 && lastNonWhitespaceEnd != -1 {
				addLineOfJsxText(acc, text[firstNonWhitespace:lastNonWhitespaceEnd+1], initial)
				initial = false
			}
			firstNonWhitespace = -1
		} else if !stringutil.IsWhiteSpaceSingleLine(c) {
			lastNonWhitespaceEnd = i + size - 1
			if firstNonWhitespace == -1 {
				firstNonWhitespace = i
			}
		}
		if size > 1 {
			i += (size - 1)
		}
	}
	if firstNonWhitespace != -1 {
		addLineOfJsxText(acc, text[firstNonWhitespace:], initial)
	}
	return acc.String()
}
func (tx *JSXTransformer) visitJsxExpression(expression ast.Handle) ast.Handle {
	e := tx.Visitor().Visit(expression.Expression())
	if !expression.DotDotDotToken().IsNil() {
		return tx.Factory().NewSpreadElement(e)
	}
	return e
}

func decodeEntities(text string) string {
	i := strings.IndexByte(text, '&')
	if i < 0 {
		return text
	}
	var result strings.Builder
	result.Grow(len(text))
	for {
		result.WriteString(text[:i])
		text = text[i:]
		semi := strings.IndexByte(text, ';')
		if semi < 0 {
			break
		}
		for {
			nextAmp := strings.IndexByte(text[1:semi], '&')
			if nextAmp < 0 {
				break
			}
			result.WriteString(text[:nextAmp+1])
			text = text[nextAmp+1:]
			semi -= nextAmp + 1
		}
		entity := text[1:semi]
		decoded, ok := decodeEntity(entity)
		if ok {
			result.WriteString(stringutil.EncodeJSStringRune(decoded))
		} else {
			result.WriteString(text[:semi+1])
		}
		text = text[semi+1:]
		i = strings.IndexByte(text, '&')
		if i < 0 {
			break
		}
	}
	result.WriteString(text)
	return result.String()
}
func decodeEntity(entity string) (rune, bool) {
	if len(entity) == 0 {
		return 0, false
	}
	if entity[0] == '#' {
		entity = entity[1:]
		if len(entity) == 0 {
			return 0, false
		}
		base := 10
		if entity[0] == 'x' {
			base = 16
			entity = entity[1:]
		}
		if len(entity) == 0 {
			return 0, false
		}
		for _, c := range entity {
			if base == 16 && !stringutil.IsHexDigit(c) {
				return 0, false
			}
			if base == 10 && !stringutil.IsDigit(c) {
				return 0, false
			}
		}
		parsed, err := strconv.ParseInt(entity, base, 32)
		if err != nil {
			return 0, false
		}
		return rune(parsed), true
	}
	r, ok := entities[entity]
	return r, ok
}

var entities = map[string]rune{"quot": 0x0022, "amp": 0x0026, "apos": 0x0027, "lt": 0x003C, "gt": 0x003E, "nbsp": 0x00A0, "iexcl": 0x00A1, "cent": 0x00A2, "pound": 0x00A3, "curren": 0x00A4, "yen": 0x00A5, "brvbar": 0x00A6, "sect": 0x00A7, "uml": 0x00A8, "copy": 0x00A9, "ordf": 0x00AA, "laquo": 0x00AB, "not": 0x00AC, "shy": 0x00AD, "reg": 0x00AE, "macr": 0x00AF, "deg": 0x00B0, "plusmn": 0x00B1, "sup2": 0x00B2, "sup3": 0x00B3, "acute": 0x00B4, "micro": 0x00B5, "para": 0x00B6, "middot": 0x00B7, "cedil": 0x00B8, "sup1": 0x00B9, "ordm": 0x00BA, "raquo": 0x00BB, "frac14": 0x00BC, "frac12": 0x00BD, "frac34": 0x00BE, "iquest": 0x00BF, "Agrave": 0x00C0, "Aacute": 0x00C1, "Acirc": 0x00C2, "Atilde": 0x00C3, "Auml": 0x00C4, "Aring": 0x00C5, "AElig": 0x00C6, "Ccedil": 0x00C7, "Egrave": 0x00C8, "Eacute": 0x00C9, "Ecirc": 0x00CA, "Euml": 0x00CB, "Igrave": 0x00CC, "Iacute": 0x00CD, "Icirc": 0x00CE, "Iuml": 0x00CF, "ETH": 0x00D0, "Ntilde": 0x00D1, "Ograve": 0x00D2, "Oacute": 0x00D3, "Ocirc": 0x00D4, "Otilde": 0x00D5, "Ouml": 0x00D6, "times": 0x00D7, "Oslash": 0x00D8, "Ugrave": 0x00D9, "Uacute": 0x00DA, "Ucirc": 0x00DB, "Uuml": 0x00DC, "Yacute": 0x00DD, "THORN": 0x00DE, "szlig": 0x00DF, "agrave": 0x00E0, "aacute": 0x00E1, "acirc": 0x00E2, "atilde": 0x00E3, "auml": 0x00E4, "aring": 0x00E5, "aelig": 0x00E6, "ccedil": 0x00E7, "egrave": 0x00E8, "eacute": 0x00E9, "ecirc": 0x00EA, "euml": 0x00EB, "igrave": 0x00EC, "iacute": 0x00ED, "icirc": 0x00EE, "iuml": 0x00EF, "eth": 0x00F0, "ntilde": 0x00F1, "ograve": 0x00F2, "oacute": 0x00F3, "ocirc": 0x00F4, "otilde": 0x00F5, "ouml": 0x00F6, "divide": 0x00F7, "oslash": 0x00F8, "ugrave": 0x00F9, "uacute": 0x00FA, "ucirc": 0x00FB, "uuml": 0x00FC, "yacute": 0x00FD, "thorn": 0x00FE, "yuml": 0x00FF, "OElig": 0x0152, "oelig": 0x0153, "Scaron": 0x0160, "scaron": 0x0161, "Yuml": 0x0178, "fnof": 0x0192, "circ": 0x02C6, "tilde": 0x02DC, "Alpha": 0x0391, "Beta": 0x0392, "Gamma": 0x0393, "Delta": 0x0394, "Epsilon": 0x0395, "Zeta": 0x0396, "Eta": 0x0397, "Theta": 0x0398, "Iota": 0x0399, "Kappa": 0x039A, "Lambda": 0x039B, "Mu": 0x039C, "Nu": 0x039D, "Xi": 0x039E, "Omicron": 0x039F, "Pi": 0x03A0, "Rho": 0x03A1, "Sigma": 0x03A3, "Tau": 0x03A4, "Upsilon": 0x03A5, "Phi": 0x03A6, "Chi": 0x03A7, "Psi": 0x03A8, "Omega": 0x03A9, "alpha": 0x03B1, "beta": 0x03B2, "gamma": 0x03B3, "delta": 0x03B4, "epsilon": 0x03B5, "zeta": 0x03B6, "eta": 0x03B7, "theta": 0x03B8, "iota": 0x03B9, "kappa": 0x03BA, "lambda": 0x03BB, "mu": 0x03BC, "nu": 0x03BD, "xi": 0x03BE, "omicron": 0x03BF, "pi": 0x03C0, "rho": 0x03C1, "sigmaf": 0x03C2, "sigma": 0x03C3, "tau": 0x03C4, "upsilon": 0x03C5, "phi": 0x03C6, "chi": 0x03C7, "psi": 0x03C8, "omega": 0x03C9, "thetasym": 0x03D1, "upsih": 0x03D2, "piv": 0x03D6, "ensp": 0x2002, "emsp": 0x2003, "thinsp": 0x2009, "zwnj": 0x200C, "zwj": 0x200D, "lrm": 0x200E, "rlm": 0x200F, "ndash": 0x2013, "mdash": 0x2014, "lsquo": 0x2018, "rsquo": 0x2019, "sbquo": 0x201A, "ldquo": 0x201C, "rdquo": 0x201D, "bdquo": 0x201E, "dagger": 0x2020, "Dagger": 0x2021, "bull": 0x2022, "hellip": 0x2026, "permil": 0x2030, "prime": 0x2032, "Prime": 0x2033, "lsaquo": 0x2039, "rsaquo": 0x203A, "oline": 0x203E, "frasl": 0x2044, "euro": 0x20AC, "image": 0x2111, "weierp": 0x2118, "real": 0x211C, "trade": 0x2122, "alefsym": 0x2135, "larr": 0x2190, "uarr": 0x2191, "rarr": 0x2192, "darr": 0x2193, "harr": 0x2194, "crarr": 0x21B5, "lArr": 0x21D0, "uArr": 0x21D1, "rArr": 0x21D2, "dArr": 0x21D3, "hArr": 0x21D4, "forall": 0x2200, "part": 0x2202, "exist": 0x2203, "empty": 0x2205, "nabla": 0x2207, "isin": 0x2208, "notin": 0x2209, "ni": 0x220B, "prod": 0x220F, "sum": 0x2211, "minus": 0x2212, "lowast": 0x2217, "radic": 0x221A, "prop": 0x221D, "infin": 0x221E, "ang": 0x2220, "and": 0x2227, "or": 0x2228, "cap": 0x2229, "cup": 0x222A, "int": 0x222B, "there4": 0x2234, "sim": 0x223C, "cong": 0x2245, "asymp": 0x2248, "ne": 0x2260, "equiv": 0x2261, "le": 0x2264, "ge": 0x2265, "sub": 0x2282, "sup": 0x2283, "nsub": 0x2284, "sube": 0x2286, "supe": 0x2287, "oplus": 0x2295, "otimes": 0x2297, "perp": 0x22A5, "sdot": 0x22C5, "lceil": 0x2308, "rceil": 0x2309, "lfloor": 0x230A, "rfloor": 0x230B, "lang": 0x2329, "rang": 0x232A, "loz": 0x25CA, "spades": 0x2660, "clubs": 0x2663, "hearts": 0x2665, "diams": 0x2666}
