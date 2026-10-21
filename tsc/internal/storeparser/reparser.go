package storeparser

// internal/parser/reparser.go on the Store (TODO 7d-1: internal/ast/docs/
// store-checker-design-20260923.md 2.2, "reparse の Builder API", and
// store-checker-7d1-implementation-instructions-20260928.md 3.1 and 4). The
// Pointer reparser makes a node with the factory (no range, no flags), sets
// members on it and finishes it; here the node is made by the constructor
// with the flags 0 and the range -1, -1 of an unfinished Pointer node, the
// members are set by the Builder's setters, and finishReparsedNode writes
// the flags and the range (Builder.SetFlags, SetRange). A list that the
// Pointer reparser appends to in place is a new block with the old range;
// the old block is dead. A node read across a Builder append is held as a
// NodeRef and viewed again after it.

import (
	"slices"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

func (p *Parser) finishReparsedNode(node store.NodeRef, locationNode store.NodeRef) {
	p.b.SetFlags(node, p.contextFlags|ast.NodeFlagsReparsed)
	p.b.SetRange(node, int32(p.pos(locationNode)), int32(p.end(locationNode)))
	p.b.ReparentChildren(node)
}

func (p *Parser) finishMutatedNode(node store.NodeRef) {
	p.b.ReparentChildren(node)
}

// Deep-clone the given node and add the clone to the reparsed clone list. The list is used by ast.GetReparsedNodeForNode
// to locate reparsed clones of JSDoc nodes. Since the binder attaches symbols to reparsed nodes and not to JSDoc nodes, we
// need the mapping when obtaining symbols and types from JSDoc nodes.
func (p *Parser) addDeepCloneReparse(node store.NodeRef) store.NodeRef {
	clone := p.b.DeepCloneReparse(node)
	if clone != store.NoNodeRef {
		p.reparsedClones = append(p.reparsedClones, clone)
	}
	return clone
}

func (p *Parser) addTransformedReparse(newNode store.NodeRef, old store.NodeRef) store.NodeRef {
	p.finishReparsedNode(newNode, old)
	p.b.AddFlags(newNode, ast.NodeFlagsReparserTransformedLiteral)
	p.reparsedClones = append(p.reparsedClones, newNode)
	return newNode
}

func (p *Parser) checkNonIdentifierName(name store.NodeRef) store.NodeRef {
	// Handles the case of anonymous functions
	if name == store.NoNodeRef {
		return store.NoNodeRef
	}
	if n := p.b.View(name); store.IsIdentifier(n) && !scanner.IsValidIdentifier(n.AsIdentifier().Text()) {
		errLoc := n.Loc()
		if errLoc.Len() == 0 { // missing name, emit error on the character before the missing name node
			errLoc = core.NewTextRange(errLoc.Pos()-1, errLoc.Pos())
		}
		p.parseErrorAtRange(errLoc, diagnostics.Identifier_expected)
	}
	return name
}

// Hosted tags find a host and add their children to the correct location under the host.
// Unhosted tags add synthetic nodes to the reparse list.
func (p *Parser) reparseTags(parent store.NodeRef, jsDoc []store.NodeRef) {
	for _, j := range jsDoc {
		isLast := j == jsDoc[len(jsDoc)-1]
		tags := p.b.View(j).AsJSDoc().Tags()
		if tags.IsNil() {
			continue
		}
		for _, tag := range slices.Clone(tags.Refs()) {
			p.reparseUnhosted(tag, parent, j)
			if isLast {
				p.reparseHosted(tag, parent, j)
			}
		}
	}
}

func (p *Parser) reparseUnhosted(tag store.NodeRef, parent store.NodeRef, jsDoc store.NodeRef) {
	switch p.b.View(tag).Kind() {
	case ast.KindJSDocTypedefTag:
		typeExpression := p.b.View(tag).TypeExpression().Ref()
		if typeExpression == store.NoNodeRef {
			break
		}
		fullName := p.b.View(tag).Name().Ref()
		isNamespace := fullName != store.NoNodeRef && store.IsModuleDeclaration(p.b.View(fullName))
		var modifiers store.ListRef
		if isNamespace {
			modifiers = p.createExportModifier(tag)
		}
		typeAlias := p.b.NewTypeAliasDeclaration(ast.KindJSTypeAliasDeclaration, 0, -1, -1, modifiers, p.addDeepCloneReparse(p.checkNonIdentifierName(p.getInnermostNameOfJSDocNamespace(fullName))), store.NoListRef, store.NoNodeRef)
		p.b.SetTypeParameters(typeAlias, p.gatherTypeParameters(jsDoc, true /*typedefOrCallback*/))
		var t store.NodeRef
		switch p.b.View(typeExpression).Kind() {
		case ast.KindJSDocTypeExpression:
			t = p.addDeepCloneReparse(p.b.View(typeExpression).Type().Ref())
		case ast.KindJSDocTypeLiteral:
			t = p.reparseJSDocTypeLiteral(typeExpression)
		default:
			panic("typedef tag type expression should be a name reference or a type expression" + p.b.View(typeExpression).Kind().String())
		}
		p.b.SetType(typeAlias, t)
		p.finishReparsedNode(typeAlias, tag)
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: typeAlias, jsDocs: []store.NodeRef{jsDoc}})
		p.b.AddFlags(typeAlias, ast.NodeFlagsHasJSDoc)
		result := p.wrapInJSDocNamespace(fullName, typeAlias, false /*nested*/)
		p.reparseList = append(p.reparseList, result)
	case ast.KindJSDocCallbackTag:
		typeExpression := p.b.View(tag).TypeExpression().Ref()
		if typeExpression == store.NoNodeRef {
			break
		}
		fullName := p.b.View(tag).Name().Ref()
		isNamespace := fullName != store.NoNodeRef && store.IsModuleDeclaration(p.b.View(fullName))
		var modifiers store.ListRef
		if isNamespace {
			modifiers = p.createExportModifier(tag)
		}
		functionType := p.reparseJSDocSignature(typeExpression, tag, jsDoc, tag, store.NoListRef)
		typeAlias := p.b.NewTypeAliasDeclaration(ast.KindJSTypeAliasDeclaration, 0, -1, -1, modifiers, p.addDeepCloneReparse(p.getInnermostNameOfJSDocNamespace(fullName)), store.NoListRef, functionType)
		p.b.SetTypeParameters(typeAlias, p.gatherTypeParameters(jsDoc, true /*typedefOrCallback*/))
		p.finishReparsedNode(typeAlias, tag)
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: typeAlias, jsDocs: []store.NodeRef{jsDoc}})
		p.b.AddFlags(typeAlias, ast.NodeFlagsHasJSDoc)
		result := p.wrapInJSDocNamespace(fullName, typeAlias, false /*nested*/)
		p.reparseList = append(p.reparseList, result)
	case ast.KindJSDocImportTag:
		if p.b.View(tag).AsJSDocImportTag().ImportClause().IsNil() {
			break
		}
		importClause := p.addDeepCloneReparse(p.b.View(tag).AsJSDocImportTag().ImportClause().Ref())
		p.b.SetPhaseModifier(importClause, ast.KindTypeKeyword)
		importDeclaration := p.b.NewImportDeclaration(ast.KindJSImportDeclaration, 0, -1, -1,
			p.b.DeepCloneReparseModifiers(p.b.View(tag).Modifiers().Ref()),
			importClause,
			p.addDeepCloneReparse(p.b.View(tag).AsJSDocImportTag().ModuleSpecifier().Ref()),
			p.addDeepCloneReparse(p.b.View(tag).AsJSDocImportTag().Attributes().Ref()),
		)
		p.finishReparsedNode(importDeclaration, tag)
		p.reparseList = append(p.reparseList, importDeclaration)
	case ast.KindJSDocOverloadTag:
		// Create overload signatures only for function, method, and constructor declarations outside object literals
		if host := p.b.View(parent); (store.IsFunctionDeclaration(host) || store.IsMethodDeclaration(host) || store.IsConstructorDeclaration(host)) && p.parsingContexts&(1<<PCObjectLiteralMembers) == 0 {
			p.reparseList = append(p.reparseList, p.reparseJSDocSignature(p.b.View(tag).AsJSDocOverloadTag().TypeExpression().Ref(), parent, jsDoc, tag, p.b.View(parent).Modifiers().Ref()))
		}
	}
}

func (p *Parser) reparseJSDocSignature(jsSignature store.NodeRef, fun store.NodeRef, jsDoc store.NodeRef, tag store.NodeRef, modifiers store.ListRef) store.NodeRef {
	var signature store.NodeRef
	clonedModifiers := p.b.DeepCloneReparseModifiers(modifiers)
	switch p.b.View(fun).Kind() {
	case ast.KindFunctionDeclaration:
		signature = p.b.NewFunctionDeclaration(0, -1, -1, clonedModifiers, store.NoNodeRef, p.b.DeepCloneReparse(p.checkNonIdentifierName(p.b.View(fun).Name().Ref())), store.NoListRef, store.NoListRef, store.NoNodeRef, store.NoNodeRef, store.NoNodeRef)
	case ast.KindMethodDeclaration:
		signature = p.b.NewMethodDeclaration(0, -1, -1, clonedModifiers, store.NoNodeRef, p.b.DeepCloneReparse(p.checkNonIdentifierName(p.b.View(fun).Name().Ref())), store.NoNodeRef, store.NoListRef, store.NoListRef, store.NoNodeRef, store.NoNodeRef, store.NoNodeRef)
	case ast.KindConstructor:
		signature = p.b.NewConstructorDeclaration(0, -1, -1, clonedModifiers, store.NoListRef, store.NoListRef, store.NoNodeRef, store.NoNodeRef, store.NoNodeRef)
	case ast.KindJSDocCallbackTag:
		signature = p.b.NewFunctionTypeNode(0, -1, -1, store.NoListRef, store.NoListRef, p.b.NewToken(ast.KindAnyKeyword, 0, -1, -1))
	default:
		panic("Unexpected kind " + p.b.View(fun).Kind().String())
	}

	if p.b.View(tag).Kind() != ast.KindJSDocCallbackTag {
		p.b.SetTypeParameters(signature, p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
	}
	parameters := []store.NodeRef{}
	for pi, param := range slices.Clone(p.b.View(jsSignature).Parameters().Refs()) {
		var parameter store.NodeRef
		if p.b.View(param).Kind() == ast.KindJSDocThisTag {
			thisIdent := p.b.NewIdentifier(p.contextFlags|ast.NodeFlagsReparsed, int32(p.pos(param)), int32(p.end(param)), "this")
			parameter = p.b.NewParameterDeclaration(0, -1, -1, store.NoListRef, store.NoNodeRef, thisIdent, store.NoNodeRef, store.NoNodeRef, store.NoNodeRef)
			if thisTag := p.b.View(param).AsJSDocThisTag(); !thisTag.TypeExpression().IsNil() {
				p.b.SetType(parameter, p.addDeepCloneReparse(thisTag.TypeExpression().Type().Ref()))
			}
		} else if p.b.View(param).Kind() == ast.KindJSDocParameterTag || p.b.View(param).Kind() == ast.KindJSDocPropertyTag {
			jsparam := param
			// Skip sub-property parameters (e.g., @param x.y) - these have QualifiedNames
			// and describe properties of a parent parameter, not standalone parameters.
			if store.IsQualifiedName(p.b.View(jsparam).AsJSDocParameterOrPropertyTag().Name()) {
				continue
			}
			var dotDotDotToken store.NodeRef
			var paramType store.NodeRef

			if typeExpression := p.b.View(jsparam).AsJSDocParameterOrPropertyTag().TypeExpression(); !typeExpression.IsNil() {
				if typeExpression.Type().Kind() == ast.KindJSDocVariadicType {
					dotDotDotToken = p.b.NewToken(ast.KindDotDotDotToken, p.contextFlags|ast.NodeFlagsReparsed, int32(p.pos(jsparam)), int32(p.end(jsparam)))

					variadicType := p.b.View(jsparam).AsJSDocParameterOrPropertyTag().TypeExpression().Type().AsJSDocVariadicType()
					paramType = p.reparseJSDocTypeLiteral(variadicType.Type().Ref())
				} else {
					paramType = p.reparseJSDocTypeLiteral(typeExpression.Type().Ref())
				}
			}
			name := p.b.View(jsparam).AsJSDocParameterOrPropertyTag().Name().Ref()
			if n := p.b.View(name); store.IsIdentifier(n) && !scanner.IsValidIdentifier(n.AsIdentifier().Text()) {
				// drop invalid chars for _, if empty, write _0, etc., so we have a valid param name to emit later
				result := strings.Builder{}
				for i, ch := range n.AsIdentifier().Text() {
					if i == 0 {
						if !scanner.IsIdentifierStart(ch) {
							result.WriteRune('_')
						} else {
							result.WriteRune(ch)
						}
						continue
					} else if !scanner.IsIdentifierPart(ch) {
						result.WriteRune('_')
					} else {
						result.WriteRune(ch)
					}
				}
				if result.Len() == 0 {
					result.WriteRune('_')
					result.WriteString(strconv.Itoa(pi))
				}
				name = p.addTransformedReparse(p.b.NewIdentifier(0, -1, -1, result.String()), name)
			} else {
				name = p.addDeepCloneReparse(name)
			}
			parameter = p.b.NewParameterDeclaration(0, -1, -1, store.NoListRef, dotDotDotToken, name, p.makeQuestionIfOptional(jsparam), paramType, store.NoNodeRef)
		}
		p.finishReparsedNode(parameter, param)
		parameters = append(parameters, parameter)
		p.reparseJSDocComment(parameter, param)
	}
	loc := p.listLoc(p.b.View(jsSignature).AsJSDocSignature().Parameters().Ref())
	p.b.SetParameters(signature, p.b.List(int32(loc.Pos()), int32(loc.End()), parameters))

	if returnTag := p.b.View(jsSignature).Type(); !returnTag.IsNil() && !returnTag.TypeExpression().IsNil() {
		p.b.SetType(signature, p.addDeepCloneReparse(returnTag.TypeExpression().Type().Ref()))
	}
	locationNode := jsSignature
	if p.b.View(tag).Kind() == ast.KindJSDocOverloadTag {
		locationNode = p.b.View(tag).TagName().Ref()
	}
	p.finishReparsedNode(signature, locationNode)
	return signature
}

func (p *Parser) reparseJSDocTypeLiteral(t store.NodeRef) store.NodeRef {
	if t == store.NoNodeRef {
		return store.NoNodeRef
	}
	if p.b.View(t).Kind() == ast.KindJSDocTypeLiteral {
		jstypeliteral := t
		isArrayType := p.b.View(jstypeliteral).AsJSDocTypeLiteral().IsArrayType()
		properties := []store.NodeRef{}
		for _, prop := range slices.Clone(p.b.View(jstypeliteral).AsJSDocTypeLiteral().JSDocPropertyTags().Refs()) {
			if p.b.View(prop).Kind() != ast.KindJSDocPropertyTag && p.b.View(prop).Kind() != ast.KindJSDocParameterTag {
				continue
			}
			jsprop := prop
			name := p.b.View(prop).Name().Ref()
			if p.b.View(name).Kind() == ast.KindQualifiedName {
				name = p.b.View(name).AsQualifiedName().Right().Ref()
			}
			if n := p.b.View(name); store.IsIdentifier(n) && !scanner.IsValidIdentifier(n.AsIdentifier().Text()) {
				name = p.addTransformedReparse(p.b.NewStringLiteral(0, -1, -1, n.AsIdentifier().Text(), ast.TokenFlagsNone), name)
			} else {
				name = p.addDeepCloneReparse(name)
			}
			property := p.b.NewPropertySignatureDeclaration(0, -1, -1, store.NoListRef, name, p.makeQuestionIfOptional(jsprop), store.NoNodeRef, store.NoNodeRef)
			if typeExpression := p.b.View(jsprop).AsJSDocParameterOrPropertyTag().TypeExpression(); !typeExpression.IsNil() {
				p.b.SetType(property, p.reparseJSDocTypeLiteral(typeExpression.Type().Ref()))
			}
			p.finishReparsedNode(property, prop)
			properties = append(properties, property)
			p.reparseJSDocComment(property, prop)
		}
		t = p.b.NewTypeLiteralNode(0, -1, -1, p.b.List(int32(p.pos(jstypeliteral)), int32(p.end(jstypeliteral)), properties))
		if isArrayType {
			p.finishReparsedNode(t, jstypeliteral)
			t = p.b.NewArrayTypeNode(0, -1, -1, t)
		}
		p.finishReparsedNode(t, jstypeliteral)
		return t
	}
	return p.addDeepCloneReparse(t)
}

func (p *Parser) reparseJSDocComment(node store.NodeRef, tag store.NodeRef) {
	if comment := p.b.View(tag).Comment().Ref(); comment != store.NoListRef {
		elems := slices.Clone(p.b.ViewList(comment).Refs())
		for i, elem := range elems {
			elems[i] = p.b.DeepCloneReparse(elem)
		}
		loc := p.listLoc(comment)
		newComment := p.b.List(int32(loc.Pos()), int32(loc.End()), elems)
		propJSDoc := p.b.NewJSDoc(0, -1, -1, newComment, store.NoListRef)
		p.finishReparsedNode(propJSDoc, tag)
		p.b.SetParent(propJSDoc, node)
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: node, jsDocs: []store.NodeRef{propJSDoc}})
		p.b.AddFlags(node, ast.NodeFlagsHasJSDoc)
	}
}

func (p *Parser) gatherTypeParameters(j store.NodeRef, typedefOrCallback bool) store.ListRef {
	var typeParameters []store.NodeRef
	pos := -1
	endPos := -1
	firstTemplate := true
	for _, tag := range slices.Clone(p.b.View(j).AsJSDoc().Tags().Refs()) {
		// When a JSDoc comment contains an `@typedef` or `@callback` tag, `@template` type parameter
		// declarations apply to the type being defined.
		if !typedefOrCallback && (store.IsJSDocTypedefTag(p.b.View(tag)) || store.IsJSDocCallbackTag(p.b.View(tag))) {
			return store.NoListRef
		}
		if !store.IsJSDocTemplateTag(p.b.View(tag)) {
			continue
		}
		if firstTemplate {
			pos = p.pos(tag)
			firstTemplate = false
		}
		endPos = p.end(tag)
		constraint := p.b.View(tag).AsJSDocTemplateTag().Constraint().Ref()
		firstTypeParameter := true
		for _, tp := range slices.Clone(p.b.View(tag).TypeParameters().Refs()) {
			var reparse store.NodeRef
			if constraint != store.NoNodeRef && firstTypeParameter {
				reparse = p.b.NewTypeParameterDeclaration(0, -1, -1,
					p.b.DeepCloneReparseModifiers(p.b.View(tp).Modifiers().Ref()),
					p.addDeepCloneReparse(p.checkNonIdentifierName(p.b.View(tp).Name().Ref())),
					p.addDeepCloneReparse(p.b.View(constraint).Type().Ref()),
					store.NoNodeRef, // expression
					p.addDeepCloneReparse(p.b.View(tp).AsTypeParameterDeclaration().DefaultType().Ref()),
				)
				p.finishReparsedNode(reparse, tp)
			} else {
				reparse = p.addDeepCloneReparse(tp)
			}
			typeParameters = append(typeParameters, reparse)
			firstTypeParameter = false
		}
	}
	if len(typeParameters) == 0 {
		return store.NoListRef
	} else {
		return p.b.List(int32(pos), int32(endPos), typeParameters)
	}
}

// isEmptyList is len(n.X()) == 0 for a Pointer accessor that returns the
// elements of a list: nil for a nil list, and nil for an empty parsed list
// too (core.Arena.Clone).
func isEmptyList(l store.List) bool {
	return l.IsNil() || l.Len() == 0
}

func (p *Parser) reparseHosted(tag store.NodeRef, parent store.NodeRef, jsDoc store.NodeRef) {
	switch p.b.View(tag).Kind() {
	case ast.KindJSDocTypeTag:
		switch p.b.View(parent).Kind() {
		case ast.KindVariableStatement:
			if declarationList := p.b.View(parent).AsVariableStatement().DeclarationList(); !declarationList.IsNil() {
				for _, declaration := range slices.Clone(declarationList.AsVariableDeclarationList().Declarations().Refs()) {
					if p.b.View(declaration).Type().IsNil() && !p.b.View(tag).TypeExpression().IsNil() {
						p.b.SetType(declaration, p.addDeepCloneReparse(p.b.View(tag).TypeExpression().Type().Ref()))
						p.finishMutatedNode(declaration)
						return
					}
				}
			}
		case ast.KindVariableDeclaration, ast.KindExportAssignment, ast.KindPropertyDeclaration, ast.KindPropertyAssignment,
			ast.KindShorthandPropertyAssignment, ast.KindGetAccessor:
			if p.b.View(parent).Type().IsNil() && !p.b.View(tag).TypeExpression().IsNil() {
				p.b.SetType(parent, p.addDeepCloneReparse(p.b.View(tag).TypeExpression().Type().Ref()))
				p.finishMutatedNode(parent)
				return
			}
		case ast.KindParameter:
			if p.b.View(parent).Type().IsNil() && !p.b.View(tag).TypeExpression().IsNil() {
				p.b.SetType(parent, p.reparseJSDocTypeLiteral(p.b.View(tag).TypeExpression().Type().Ref()))
				p.finishMutatedNode(parent)
				return
			}
		case ast.KindExpressionStatement:
			if p.b.View(parent).Expression().Kind() == ast.KindBinaryExpression {
				bin := p.b.View(parent).Expression().Ref()
				if kind := store.GetAssignmentDeclarationKind(p.b.View(bin)); kind != ast.JSDeclarationKindNone && !p.b.View(tag).TypeExpression().IsNil() {
					p.b.SetType(bin, p.addDeepCloneReparse(p.b.View(tag).TypeExpression().Type().Ref()))
					p.finishMutatedNode(bin)
					return
				}
			}
		case ast.KindReturnStatement, ast.KindParenthesizedExpression:
			if !p.b.View(parent).Expression().IsNil() && !p.b.View(tag).TypeExpression().IsNil() {
				p.b.SetExpression(parent, p.makeNewCast(
					p.addDeepCloneReparse(p.b.View(tag).TypeExpression().Type().Ref()),
					p.b.View(parent).Expression().Ref(),
					true, /*isAssertion*/
				))
				p.finishMutatedNode(parent)
				return
			}
		}
		if fun := p.functionLikeHost(parent); fun != store.NoNodeRef {
			noTypedParams := true
			for i := range p.b.View(fun).Parameters().Len() {
				if !p.b.View(fun).Parameters().At(i).Type().IsNil() {
					noTypedParams = false
					break
				}
			}
			if p.b.View(fun).TypeParameters().IsNil() && p.b.View(fun).Type().IsNil() && noTypedParams && !p.b.View(tag).TypeExpression().IsNil() {
				p.b.SetFullSignature(fun, p.addDeepCloneReparse(p.b.View(tag).TypeExpression().Type().Ref()))
				p.finishMutatedNode(fun)
			}
		}
	case ast.KindJSDocSatisfiesTag:
		switch p.b.View(parent).Kind() {
		case ast.KindVariableStatement:
			if declarationList := p.b.View(parent).AsVariableStatement().DeclarationList(); !declarationList.IsNil() {
				for _, declaration := range slices.Clone(declarationList.AsVariableDeclarationList().Declarations().Refs()) {
					if !p.b.View(declaration).Initializer().IsNil() && !p.b.View(tag).TypeExpression().IsNil() {
						p.b.SetInitializer(declaration, p.makeNewCast(
							p.addDeepCloneReparse(p.b.View(tag).TypeExpression().Type().Ref()),
							p.b.View(declaration).Initializer().Ref(),
							false, /*isAssertion*/
						))
						p.finishMutatedNode(declaration)
						break
					}
				}
			}
		case ast.KindVariableDeclaration, ast.KindPropertyDeclaration, ast.KindPropertyAssignment:
			if !p.b.View(parent).Initializer().IsNil() && !p.b.View(tag).TypeExpression().IsNil() {
				p.b.SetInitializer(parent, p.makeNewCast(
					p.addDeepCloneReparse(p.b.View(tag).TypeExpression().Type().Ref()),
					p.b.View(parent).Initializer().Ref(),
					false, /*isAssertion*/
				))
				p.finishMutatedNode(parent)
			}
		case ast.KindShorthandPropertyAssignment:
			if !p.b.View(parent).AsShorthandPropertyAssignment().ObjectAssignmentInitializer().IsNil() && !p.b.View(tag).AsJSDocSatisfiesTag().TypeExpression().IsNil() {
				p.b.SetObjectAssignmentInitializer(parent, p.makeNewCast(
					p.addDeepCloneReparse(p.b.View(tag).AsJSDocSatisfiesTag().TypeExpression().Type().Ref()),
					p.b.View(parent).AsShorthandPropertyAssignment().ObjectAssignmentInitializer().Ref(),
					false, /*isAssertion*/
				))
				p.finishMutatedNode(parent)
			}
		case ast.KindReturnStatement, ast.KindParenthesizedExpression, ast.KindExportAssignment:
			if !p.b.View(parent).Expression().IsNil() && !p.b.View(tag).TypeExpression().IsNil() {
				p.b.SetExpression(parent, p.makeNewCast(
					p.addDeepCloneReparse(p.b.View(tag).TypeExpression().Type().Ref()),
					p.b.View(parent).Expression().Ref(),
					false, /*isAssertion*/
				))
				p.finishMutatedNode(parent)
			}
		case ast.KindExpressionStatement:
			if p.b.View(parent).Expression().Kind() == ast.KindBinaryExpression {
				bin := p.b.View(parent).Expression().Ref()
				if kind := store.GetAssignmentDeclarationKind(p.b.View(bin)); kind != ast.JSDeclarationKindNone && !p.b.View(tag).TypeExpression().IsNil() {
					p.b.SetRight(bin, p.makeNewCast(
						p.addDeepCloneReparse(p.b.View(tag).TypeExpression().Type().Ref()),
						p.b.View(bin).AsBinaryExpression().Right().Ref(),
						false, /*isAssertion*/
					))
					p.finishMutatedNode(bin)
				}
			}
		}
	case ast.KindJSDocTemplateTag:
		if fun := p.functionLikeHost(parent); fun != store.NoNodeRef {
			if isEmptyList(p.b.View(fun).TypeParameters()) && p.b.View(fun).FullSignature().IsNil() {
				p.b.SetTypeParameters(fun, p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
				p.finishMutatedNode(fun)
			}
		} else if p.b.View(parent).Kind() == ast.KindClassDeclaration {
			if p.b.View(parent).AsClassDeclaration().TypeParameters().IsNil() {
				p.b.SetTypeParameters(parent, p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
				p.finishMutatedNode(parent)
			}
		} else if p.b.View(parent).Kind() == ast.KindClassExpression {
			if p.b.View(parent).AsClassExpression().TypeParameters().IsNil() {
				p.b.SetTypeParameters(parent, p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
				p.finishMutatedNode(parent)
			}
		}
	case ast.KindJSDocParameterTag:
		if fun := p.functionLikeHost(parent); fun != store.NoNodeRef && p.b.View(fun).FullSignature().IsNil() {
			parameterTag := tag
			if param, ok := findMatchingParameter(p.b.View(fun), p.b.View(parameterTag), p.b.View(jsDoc)); ok {
				if p.b.View(param).Type().IsNil() && !p.b.View(parameterTag).AsJSDocParameterOrPropertyTag().TypeExpression().IsNil() {
					p.b.SetType(param, p.reparseJSDocTypeLiteral(p.b.View(parameterTag).AsJSDocParameterOrPropertyTag().TypeExpression().Type().Ref()))
				}
				if p.b.View(param).QuestionToken().IsNil() {
					if question := p.makeQuestionIfOptional(parameterTag); question != store.NoNodeRef {
						p.b.SetQuestionToken(param, question)
					}
				}
				p.finishMutatedNode(param)
			}
		}
	case ast.KindJSDocThisTag:
		if fun := p.functionLikeHost(parent); fun != store.NoNodeRef {
			params := slices.Clone(p.b.View(fun).Parameters().Refs())
			if len(params) == 0 || (p.b.View(params[0]).Name().Kind() != ast.KindThisKeyword && !store.IsThisIdentifier(p.b.View(params[0]).Name())) {
				thisParam := p.b.NewParameterDeclaration(0, -1, -1,
					store.NoListRef, /* modifiers */
					store.NoNodeRef, /* dotDotDotToken */
					p.b.NewIdentifier(0, -1, -1, "this"),
					store.NoNodeRef, /* questionToken */
					store.NoNodeRef, /* type */
					store.NoNodeRef, /* initializer */
				)
				if typeExpression := p.b.View(tag).AsJSDocThisTag().TypeExpression(); !typeExpression.IsNil() {
					p.b.SetType(thisParam, p.addDeepCloneReparse(typeExpression.Type().Ref()))
				}
				p.finishReparsedNode(thisParam, p.b.View(tag).TagName().Ref())

				newParams := make([]store.NodeRef, len(params)+1)
				newParams[0] = thisParam
				for i, param := range params {
					newParams[i+1] = param
				}

				loc := p.listLoc(p.b.View(fun).Parameters().Ref())
				p.b.SetParameters(fun, p.b.List(int32(loc.Pos()), int32(loc.End()), newParams))
				p.finishMutatedNode(fun)
			}
		}
	case ast.KindJSDocReturnTag:
		if fun := p.functionLikeHost(parent); fun != store.NoNodeRef && p.b.View(fun).FullSignature().IsNil() {
			if p.b.View(fun).Type().IsNil() && !p.b.View(tag).TypeExpression().IsNil() {
				p.b.SetType(fun, p.addDeepCloneReparse(p.b.View(tag).TypeExpression().Type().Ref()))
				p.finishMutatedNode(fun)
			}
		}
	case ast.KindJSDocReadonlyTag, ast.KindJSDocPrivateTag, ast.KindJSDocPublicTag, ast.KindJSDocProtectedTag, ast.KindJSDocOverrideTag:
		if p.b.View(parent).Kind() == ast.KindExpressionStatement {
			parent = p.b.View(parent).Expression().Ref()
		}
		switch p.b.View(parent).Kind() {
		case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
			// In object literals these aren't class-like members, so JSDoc modifiers like @override
			// or @readonly aren't real modifiers there; reparsing them produces spurious grammar errors (#4437).
			if p.parsingContexts&(1<<PCObjectLiteralMembers) != 0 {
				return
			}
			fallthrough
		case ast.KindPropertyDeclaration, ast.KindConstructor, ast.KindBinaryExpression:
			var keyword ast.Kind
			switch p.b.View(tag).Kind() {
			case ast.KindJSDocReadonlyTag:
				keyword = ast.KindReadonlyKeyword
			case ast.KindJSDocPrivateTag:
				keyword = ast.KindPrivateKeyword
			case ast.KindJSDocPublicTag:
				keyword = ast.KindPublicKeyword
			case ast.KindJSDocProtectedTag:
				keyword = ast.KindProtectedKeyword
			case ast.KindJSDocOverrideTag:
				keyword = ast.KindOverrideKeyword
			}
			modifier := p.b.NewToken(keyword, p.contextFlags|ast.NodeFlagsReparsed, int32(p.pos(tag)), int32(p.end(tag)))
			var nodes []store.NodeRef
			var loc core.TextRange
			if modifiers := p.b.View(parent).Modifiers(); modifiers.IsNil() {
				nodes = []store.NodeRef{modifier}
				loc = p.loc(tag)
			} else {
				nodes = append(slices.Clone(modifiers.Refs()), modifier)
				loc = p.listLoc(modifiers.Ref())
			}
			p.b.SetModifiers(parent, p.b.List(int32(loc.Pos()), int32(loc.End()), nodes))
			p.finishMutatedNode(parent)
		}
	case ast.KindJSDocImplementsTag:
		if class := getClassLikeData(p.b.View(parent)); !class.IsNil() {
			implementsTag := tag

			if heritageClauses := class.HeritageClauses(); !heritageClauses.IsNil() {
				if implementsClause := core.Find(slices.Clone(heritageClauses.Refs()), func(node store.NodeRef) bool {
					return p.b.View(node).AsHeritageClause().Token() == ast.KindImplementsKeyword
				}); implementsClause != store.NoNodeRef {
					types := p.b.View(implementsClause).AsHeritageClause().Types()
					typesLoc := p.listLoc(types.Ref())
					nodes := append(slices.Clone(types.Refs()), p.addDeepCloneReparse(p.b.View(implementsTag).AsJSDocImplementsTag().ClassName().Ref()))
					p.b.SetTypes(implementsClause, p.b.List(int32(typesLoc.Pos()), int32(typesLoc.End()), nodes))
					p.finishMutatedNode(implementsClause)
					return
				}
			}
			className := p.b.View(implementsTag).AsJSDocImplementsTag().ClassName().Ref()
			typesList := p.b.List(int32(p.pos(className)), int32(p.end(className)), []store.NodeRef{p.addDeepCloneReparse(className)})

			heritageClause := p.b.NewHeritageClause(0, -1, -1, ast.KindImplementsKeyword, typesList)
			p.finishReparsedNode(heritageClause, className)

			if heritageClauses := p.b.View(parent).HeritageClauses(); heritageClauses.IsNil() {
				p.b.SetHeritageClauses(parent, p.b.List(int32(p.pos(className)), int32(p.end(className)), []store.NodeRef{heritageClause}))
			} else {
				loc := p.listLoc(heritageClauses.Ref())
				p.b.SetHeritageClauses(parent, p.b.List(int32(loc.Pos()), int32(loc.End()), append(slices.Clone(heritageClauses.Refs()), heritageClause)))
			}
			p.finishMutatedNode(parent)
		}
	case ast.KindJSDocAugmentsTag:
		if class := getClassLikeData(p.b.View(parent)); !class.IsNil() && !class.HeritageClauses().IsNil() {
			if extendsClause := core.Find(slices.Clone(class.HeritageClauses().Refs()), func(node store.NodeRef) bool {
				return p.b.View(node).AsHeritageClause().Token() == ast.KindExtendsKeyword
			}); extendsClause != store.NoNodeRef && p.b.View(extendsClause).AsHeritageClause().Types().Len() == 1 {
				target := p.b.View(extendsClause).AsHeritageClause().Types().At(0).Ref()
				source := p.b.View(tag).AsJSDocAugmentsTag().ClassName().Ref()
				targetExpression := p.b.View(target).Expression()
				if p.b.View(target).AsExpressionWithTypeArguments().TypeArguments().IsNil() && !p.b.View(source).AsExpressionWithTypeArguments().TypeArguments().IsNil() && targetExpression.Kind() != ast.KindNullKeyword && (!store.IsEntityNameExpression(targetExpression) || store.HasSamePropertyAccessName(targetExpression, p.b.View(source).Expression())) {
					p.setReparsedTypeArguments(target, p.b.View(source).AsExpressionWithTypeArguments().TypeArguments().Ref())
				}
			}
		}
	}
}

func (p *Parser) setReparsedTypeArguments(target store.NodeRef, source store.ListRef) {
	typeArguments := slices.Clone(p.b.ViewList(source).Refs())
	for i, typeArgument := range typeArguments {
		typeArguments[i] = p.addDeepCloneReparse(typeArgument)
	}
	loc := p.listLoc(source)
	p.b.SetTypeArguments(target, p.b.List(int32(loc.Pos()), int32(loc.End()), typeArguments))
	p.finishMutatedNode(target)
}

func (p *Parser) makeQuestionIfOptional(parameter store.NodeRef) store.NodeRef {
	var questionToken store.NodeRef
	if tag := p.b.View(parameter).AsJSDocParameterOrPropertyTag(); tag.IsBracketed() || !tag.TypeExpression().IsNil() && tag.TypeExpression().Type().Kind() == ast.KindJSDocOptionalType {
		questionToken = p.b.NewToken(ast.KindQuestionToken, p.contextFlags|ast.NodeFlagsReparsed, int32(p.pos(parameter)), int32(p.end(parameter)))
	}
	return questionToken
}

func findMatchingParameter(fun store.Node, parameterTag store.Node, jsDoc store.Node) (store.NodeRef, bool) {
	tagIndex := -1
	paramCount := -1
	tags := jsDoc.AsJSDoc().Tags()
	for i := range tags.Len() {
		if tag := tags.At(i); tag.Kind() == ast.KindJSDocParameterTag {
			paramCount++
			if store.SameNode(tag, parameterTag) {
				tagIndex = paramCount
				break
			}
		}
	}
	parameters := fun.Parameters()
	for parameterIndex := range parameters.Len() {
		parameter := parameters.At(parameterIndex)
		if parameter.Name().Kind() == ast.KindIdentifier {
			if parameterTag.Name().Kind() == ast.KindIdentifier &&
				((parameter.Name().Text() == parameterTag.Name().Text()) || (parameterIndex == tagIndex && len(parameterTag.Name().Text()) == 0)) {
				return parameter.Ref(), true
			}
		} else if parameterIndex == tagIndex {
			return parameter.Ref(), true
		}
	}
	return store.NoNodeRef, false
}

func skipSatisfiesExpressions(node store.Node) store.Node {
	for !node.IsNil() && node.Kind() == ast.KindSatisfiesExpression {
		node = node.Expression()
	}
	return node
}

func getFunctionLikeHost(host store.Node) store.Node {
	fun := host
	switch host.Kind() {
	case ast.KindVariableStatement:
		if nodes := host.AsVariableStatement().DeclarationList().AsVariableDeclarationList().Declarations(); nodes.Len() != 0 {
			fun = nodes.At(0).Initializer()
		}
	case ast.KindPropertyAssignment, ast.KindPropertyDeclaration:
		fun = host.Initializer()
	case ast.KindExportAssignment, ast.KindReturnStatement:
		fun = host.Expression()
	case ast.KindExpressionStatement:
		fun = store.GetRightMostAssignedExpression(host.Expression())
	}
	fun = skipSatisfiesExpressions(fun)
	if store.IsFunctionLike(fun) {
		return fun
	}
	return store.Node{}
}

// functionLikeHost is getFunctionLikeHost on the scratch: the NodeRef of the
// function, or NoNodeRef.
func (p *Parser) functionLikeHost(host store.NodeRef) store.NodeRef {
	if fun := getFunctionLikeHost(p.b.View(host)); !fun.IsNil() {
		return fun.Ref()
	}
	return store.NoNodeRef
}

func (p *Parser) makeNewCast(t store.NodeRef, e store.NodeRef, isAssertion bool) store.NodeRef {
	var assert store.NodeRef
	if isAssertion {
		assert = p.b.NewAsExpression(p.flags(), int32(p.pos(e)), int32(p.end(e)), e, t)
	} else {
		assert = p.b.NewSatisfiesExpression(p.flags(), int32(p.pos(e)), int32(p.end(e)), e, t)
	}
	return assert
}

// getClassLikeData returns the class itself, or a nil Node.
func getClassLikeData(parent store.Node) store.Node {
	switch parent.Kind() {
	case ast.KindClassDeclaration, ast.KindClassExpression:
		return parent
	}
	return store.Node{}
}

func (p *Parser) createExportModifier(locationNode store.NodeRef) store.ListRef {
	loc := p.loc(locationNode)
	exportModifier := p.b.NewToken(ast.KindExportKeyword, p.contextFlags|ast.NodeFlagsReparsed, int32(loc.Pos()), int32(loc.End()))
	return p.b.List(int32(loc.Pos()), int32(loc.End()), []store.NodeRef{exportModifier})
}

// getInnermostNameOfJSDocNamespace returns the innermost identifier from a
// JSDoc namespace chain (ModuleDeclaration). For a simple identifier, it returns
// the identifier itself. For "A.B.C", it returns the identifier "C".
func (p *Parser) getInnermostNameOfJSDocNamespace(fullName store.NodeRef) store.NodeRef {
	if fullName == store.NoNodeRef {
		return store.NoNodeRef
	}
	for p.b.View(fullName).Kind() == ast.KindModuleDeclaration {
		body := p.b.View(fullName).AsModuleDeclaration().Body().Ref()
		if body == store.NoNodeRef {
			return p.b.View(fullName).Name().Ref()
		}
		fullName = body
	}
	return fullName
}

// wrapInJSDocNamespace wraps a statement (typically a type alias) in namespace
// declarations corresponding to a JSDoc dotted name. For example, given name
// "A.B.C" and a type alias for C, this produces:
//
//	namespace A { namespace B { type C = ... } }
//
// If the name is a simple identifier (not a ModuleDeclaration), it returns the
// statement as-is.
func (p *Parser) wrapInJSDocNamespace(fullName store.NodeRef, statement store.NodeRef, nested bool) store.NodeRef {
	if fullName == store.NoNodeRef || !store.IsModuleDeclaration(p.b.View(fullName)) {
		return statement
	}
	// Recursively wrap from outermost to innermost. Inner namespaces always get an export modifier
	// so members are accessible via dotted access from outside. The outermost namespace is treated as
	// exported only in module files via IsImplicitlyExportedJSDocDeclaration (in the binder), so it
	// does not get an explicit export modifier here.
	wrapped := p.wrapInJSDocNamespace(p.b.View(fullName).Body().Ref(), statement, true /*nested*/)
	block := p.b.NewModuleBlock(0, -1, -1, p.b.List(int32(p.pos(fullName)), int32(p.end(fullName)), []store.NodeRef{wrapped}))
	p.finishReparsedNode(block, fullName)
	var modifiers store.ListRef
	if nested {
		modifiers = p.createExportModifier(fullName)
	}
	result := p.b.NewModuleDeclaration(0, -1, -1, modifiers, ast.KindNamespaceKeyword, p.addDeepCloneReparse(p.b.View(fullName).Name().Ref()), store.NoNodeRef, block)
	p.finishReparsedNode(result, fullName)
	p.reparsedClones = append(p.reparsedClones, result)
	return result
}
