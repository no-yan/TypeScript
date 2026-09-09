package parser

import (
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

func (p *Parser) finishReparsedNode(node ast.NodeRef, locationNode ast.NodeRef) {
	h := p.at(node)
	h.SetFlags(p.contextFlags | ast.NodeFlagsReparsed)
	h.SetLoc(p.at(locationNode).Loc())
	h.SetParentsInChildren()
}

func (p *Parser) finishMutatedNode(node ast.NodeRef) {
	p.at(node).SetParentsInChildren()
}

// Deep-clone the given node and add the clone to the reparsed clone list. The list is used by ast.GetReparsedNodeForNode
// to locate reparsed clones of JSDoc nodes. Since the binder attaches symbols to reparsed nodes and not to JSDoc nodes, we
// need the mapping when obtaining symbols and types from JSDoc nodes.
func (p *Parser) addDeepCloneReparse(node ast.NodeRef) ast.NodeRef {
	clone := p.factory.CopySubtree(p.at(node))
	if !clone.IsNil() {
		clone.SetFlags(clone.Flags() | p.contextFlags | ast.NodeFlagsReparsed)
		p.reparsedClones = append(p.reparsedClones, clone)
	}
	return clone.Ref()
}

func (p *Parser) addTransformedReparse(newNode ast.NodeRef, old ast.NodeRef) ast.NodeRef {
	p.finishReparsedNode(newNode, old)
	h := p.at(newNode)
	h.SetFlags(h.Flags() | ast.NodeFlagsReparserTransformedLiteral)
	p.reparsedClones = append(p.reparsedClones, h)
	return newNode
}

func (p *Parser) addDeepCloneReparseModifiers(list ast.ListRef) ast.ListRef {
	if list == 0 {
		return 0
	}
	cloned := make([]ast.NodeRef, 0, p.factory.Store().ListLen(list))
	for _, h := range p.listHandles(list) {
		cloned = append(cloned, p.addDeepCloneReparse(h))
	}
	return p.newListRefs(p.factory.Store().ListLoc(list), cloned)
}

func (p *Parser) checkNonIdentifierName(name ast.NodeRef) ast.NodeRef {
	// Handles the case of anonymous functions
	if name == 0 {
		return 0
	}
	if p.factory.Store().KindAt(name) == ast.KindIdentifier && !scanner.IsValidIdentifier(p.at(name).IdentifierText()) {
		errLoc := p.at(name).Loc()
		if errLoc.Len() == 0 { // missing name, emit error on the character before the missing name node
			errLoc = core.NewTextRange(p.at(name).Loc().Pos()-1, p.at(name).Loc().Pos())
		}
		p.parseErrorAtRange(errLoc, diagnostics.Identifier_expected)
	}
	return name
}

// Hosted tags find a host and add their children to the correct location under the host.
// Unhosted tags add synthetic nodes to the reparse list.
func (p *Parser) reparseTags(parent ast.NodeRef, jsDoc []ast.NodeRef) {
	for _, j := range jsDoc {
		isLast := j == jsDoc[len(jsDoc)-1]
		tags := p.at(j).JSDocTags()
		if tags == 0 {
			continue
		}
		for _, tag := range p.listHandles(tags) {
			p.reparseUnhosted(tag, parent, j)
			if isLast {
				p.reparseHosted(tag, parent, j)
			}
		}
	}
}

func (p *Parser) reparseUnhosted(tag ast.NodeRef, parent ast.NodeRef, jsDoc ast.NodeRef) {
	switch p.factory.Store().KindAt(tag) {
	case ast.KindJSDocTypedefTag:
		typeExpression := p.at(tag).TypeExpression().Ref()
		if typeExpression == 0 {
			break
		}
		fullName := p.at(tag).Name().Ref()
		isNamespace := fullName != 0 && p.factory.Store().KindAt(fullName) == ast.KindModuleDeclaration
		var modifiers ast.ListRef
		if isNamespace {
			modifiers = p.createExportModifier(tag)
		}
		typeAlias := p.factory.ParseJSTypeAliasDeclaration(modifiers, p.addDeepCloneReparse(p.checkNonIdentifierName(p.getInnermostNameOfJSDocNamespace(fullName))), 0, 0)
		p.at(typeAlias).SetTypeAliasDeclarationTypeParameters(p.gatherTypeParameters(jsDoc, true /*typedefOrCallback*/))
		var t ast.NodeRef
		switch p.factory.Store().KindAt(typeExpression) {
		case ast.KindJSDocTypeExpression:
			t = p.addDeepCloneReparse(p.at(typeExpression).Type().Ref())
		case ast.KindJSDocTypeLiteral:
			t = p.reparseJSDocTypeLiteral(typeExpression)
		default:
			panic("typedef tag type expression should be a name reference or a type expression" + p.factory.Store().KindAt(typeExpression).String())
		}
		p.at(typeAlias).SetTypeAliasDeclarationType(p.at(t))
		p.finishReparsedNode(typeAlias, tag)
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: typeAlias, jsDocs: []ast.NodeRef{jsDoc}})
		p.at(typeAlias).SetFlags(p.at(typeAlias).Flags() | ast.NodeFlagsHasJSDoc)
		result := p.wrapInJSDocNamespace(fullName, typeAlias, false /*nested*/)
		p.reparseList = append(p.reparseList, result)
	case ast.KindJSDocCallbackTag:
		typeExpression := p.at(tag).TypeExpression().Ref()
		if typeExpression == 0 {
			break
		}
		fullName := p.at(tag).Name().Ref()
		isNamespace := fullName != 0 && p.factory.Store().KindAt(fullName) == ast.KindModuleDeclaration
		var modifiers ast.ListRef
		if isNamespace {
			modifiers = p.createExportModifier(tag)
		}
		functionType := p.reparseJSDocSignature(typeExpression, tag, jsDoc, tag, 0)
		typeAlias := p.factory.ParseJSTypeAliasDeclaration(modifiers, p.addDeepCloneReparse(p.getInnermostNameOfJSDocNamespace(fullName)), 0, functionType)
		p.at(typeAlias).SetTypeAliasDeclarationTypeParameters(p.gatherTypeParameters(jsDoc, true /*typedefOrCallback*/))
		p.finishReparsedNode(typeAlias, tag)
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: typeAlias, jsDocs: []ast.NodeRef{jsDoc}})
		p.at(typeAlias).SetFlags(p.at(typeAlias).Flags() | ast.NodeFlagsHasJSDoc)
		result := p.wrapInJSDocNamespace(fullName, typeAlias, false /*nested*/)
		p.reparseList = append(p.reparseList, result)
	case ast.KindJSDocImportTag:
		if p.at(tag).JSDocImportTagImportClause().IsNil() {
			break
		}
		importClause := p.addDeepCloneReparse(p.at(tag).JSDocImportTagImportClause().Ref())
		p.at(importClause).SetImportClausePhaseModifier(ast.KindTypeKeyword)
		importDeclaration := p.factory.ParseJSImportDeclaration(
			p.addDeepCloneReparseModifiers(0),
			importClause,
			p.addDeepCloneReparse(p.at(tag).JSDocImportTagModuleSpecifier().Ref()),
			p.addDeepCloneReparse(p.at(tag).JSDocImportTagAttributes().Ref()),
		)
		p.finishReparsedNode(importDeclaration, tag)
		p.reparseList = append(p.reparseList, importDeclaration)
	case ast.KindJSDocOverloadTag:
		// Create overload signatures only for function, method, and constructor declarations outside object literals
		if (p.factory.Store().KindAt(parent) == ast.KindFunctionDeclaration || p.factory.Store().KindAt(parent) == ast.KindMethodDeclaration || p.factory.Store().KindAt(parent) == ast.KindConstructor) && p.parsingContexts&(1<<PCObjectLiteralMembers) == 0 {
			p.reparseList = append(p.reparseList, p.reparseJSDocSignature(p.at(tag).JSDocOverloadTagTypeExpression().Ref(), parent, jsDoc, tag, p.at(parent).Modifiers()))
		}
	}
}

func (p *Parser) reparseJSDocSignature(jsSignature ast.NodeRef, fun ast.NodeRef, jsDoc ast.NodeRef, tag ast.NodeRef, modifiers ast.ListRef) ast.NodeRef {
	var signature ast.NodeRef
	clonedModifiers := p.addDeepCloneReparseModifiers(modifiers)
	switch p.factory.Store().KindAt(fun) {
	case ast.KindFunctionDeclaration:
		signature = p.factory.ParseFunctionDeclaration(clonedModifiers, 0, p.factory.CopySubtree(p.at(p.checkNonIdentifierName(p.at(fun).Name().Ref()))).Ref(), 0, 0, 0, 0, 0)
	case ast.KindMethodDeclaration:
		signature = p.factory.ParseMethodDeclaration(clonedModifiers, 0, p.factory.CopySubtree(p.at(p.checkNonIdentifierName(p.at(fun).Name().Ref()))).Ref(), 0, 0, 0, 0, 0, 0)
	case ast.KindConstructor:
		signature = p.factory.ParseConstructorDeclaration(clonedModifiers, 0, 0, 0, 0, 0)
	case ast.KindJSDocCallbackTag:
		signature = p.factory.ParseFunctionTypeNode(0, 0, p.factory.ParseKeywordTypeNode(ast.KindAnyKeyword))
	default:
		panic("Unexpected kind " + p.factory.Store().KindAt(fun).String())
	}

	if p.factory.Store().KindAt(tag) != ast.KindJSDocCallbackTag {
		p.at(signature).SetTypeParameters(p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
	}
	parameters := make([]ast.NodeRef, 0)
	for pi, param := range p.at(jsSignature).Parameters() {
		var parameter ast.NodeRef
		if param.Kind == ast.KindJSDocThisTag {
			thisIdent := p.factory.ParseIdentifier("this")
			p.at(thisIdent).SetLoc(param.Loc())
			p.at(thisIdent).SetFlags(p.contextFlags | ast.NodeFlagsReparsed)
			parameter = p.factory.ParseParameterDeclaration(0, 0, thisIdent, 0, 0, 0)
			if !param.JSDocThisTagTypeExpression().IsNil() {
				p.at(parameter).SetParameterDeclarationType(p.at(p.addDeepCloneReparse(param.JSDocThisTagTypeExpression().Type().Ref())))
			}
		} else if param.Kind == ast.KindJSDocParameterTag || param.Kind == ast.KindJSDocPropertyTag {
			if param.JSDocParameterOrPropertyTagName().Kind == ast.KindQualifiedName {
				continue
			}
			var dotDotDotToken ast.NodeRef
			var paramType ast.NodeRef

			if !param.JSDocParameterOrPropertyTagTypeExpression().IsNil() {
				if param.JSDocParameterOrPropertyTagTypeExpression().Type().Kind == ast.KindJSDocVariadicType {
					dotDotDotToken = p.factory.ParseToken(ast.KindDotDotDotToken)
					p.at(dotDotDotToken).SetLoc(param.Loc())
					p.at(dotDotDotToken).SetFlags(p.contextFlags | ast.NodeFlagsReparsed)

					variadicType := param.JSDocParameterOrPropertyTagTypeExpression().Type()
					paramType = p.reparseJSDocTypeLiteral(variadicType.Type().Ref())
				} else {
					paramType = p.reparseJSDocTypeLiteral(param.JSDocParameterOrPropertyTagTypeExpression().Type().Ref())
				}
			}
			name := param.JSDocParameterOrPropertyTagName().Ref()
			if p.factory.Store().KindAt(name) == ast.KindIdentifier && !scanner.IsValidIdentifier(p.at(name).IdentifierText()) {
				result := strings.Builder{}
				for i, ch := range p.at(name).IdentifierText() {
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
				name = p.addTransformedReparse(p.factory.ParseIdentifier(result.String()), name)
			} else {
				name = p.addDeepCloneReparse(name)
			}
			parameter = p.factory.ParseParameterDeclaration(0, dotDotDotToken, name, p.makeQuestionIfOptional(param.Ref()), paramType, 0)
		}
		p.finishReparsedNode(parameter, param.Ref())
		parameters = append(parameters, parameter)
		p.reparseJSDocComment(parameter, param.Ref())
	}
	p.at(signature).SetParameters(p.newListRefs(p.factory.Store().ListLoc(p.at(jsSignature).JSDocSignatureParameters()), parameters))

	if !p.at(jsSignature).Type().IsNil() && !p.at(jsSignature).Type().TypeExpression().IsNil() {
		p.at(signature).SetType(p.at(p.addDeepCloneReparse(p.at(jsSignature).Type().TypeExpression().Type().Ref())))
	}
	loc := jsSignature
	if p.factory.Store().KindAt(tag) == ast.KindJSDocOverloadTag {
		loc = p.at(tag).TagName().Ref()
	}
	p.finishReparsedNode(signature, loc)
	return signature
}

func (p *Parser) reparseJSDocTypeLiteral(t ast.NodeRef) ast.NodeRef {
	if t == 0 {
		return 0
	}
	if p.factory.Store().KindAt(t) == ast.KindJSDocTypeLiteral {
		loc := t
		isArrayType := p.at(t).JSDocTypeLiteralIsArrayType()
		properties := make([]ast.NodeRef, 0)
		for _, prop := range p.listHandles(p.at(t).JSDocTypeLiteralJSDocPropertyTags()) {
			propH := p.at(prop)
			if propH.Kind != ast.KindJSDocPropertyTag && propH.Kind != ast.KindJSDocParameterTag {
				continue
			}
			name := propH.Name().Ref()
			if p.factory.Store().KindAt(name) == ast.KindQualifiedName {
				name = p.at(name).QualifiedNameRight().Ref()
			}
			if p.factory.Store().KindAt(name) == ast.KindIdentifier && !scanner.IsValidIdentifier(p.at(name).IdentifierText()) {
				name = p.addTransformedReparse(p.factory.ParseStringLiteral(p.at(name).IdentifierText(), ast.TokenFlagsNone), name)
			} else {
				name = p.addDeepCloneReparse(name)
			}
			property := p.factory.ParsePropertySignatureDeclaration(0, name, p.makeQuestionIfOptional(prop), 0, 0)
			if !propH.JSDocParameterOrPropertyTagTypeExpression().IsNil() {
				p.at(property).SetPropertySignatureDeclarationType(p.at(p.reparseJSDocTypeLiteral(propH.JSDocParameterOrPropertyTagTypeExpression().Type().Ref())))
			}
			p.finishReparsedNode(property, prop)
			properties = append(properties, property)
			p.reparseJSDocComment(property, prop)
		}
		t = p.factory.ParseTypeLiteralNode(p.newListRefs(p.at(loc).Loc(), properties))
		if isArrayType {
			p.finishReparsedNode(t, loc)
			t = p.factory.ParseArrayTypeNode(t)
		}
		p.finishReparsedNode(t, loc)
		return t
	}
	return p.addDeepCloneReparse(t)
}

func (p *Parser) reparseJSDocComment(node ast.NodeRef, tag ast.NodeRef) {
	if comment := p.at(tag).CommentList(); comment != 0 {
		cloned := make([]ast.NodeRef, 0, p.factory.Store().ListLen(comment))
		for _, h := range p.listHandles(comment) {
			cloned = append(cloned, p.addDeepCloneReparse(h))
		}
		newComment := p.newListRefs(p.factory.Store().ListLoc(comment), cloned)

		propJSDoc := p.factory.ParseJSDoc(newComment, 0)
		p.finishReparsedNode(propJSDoc, tag)
		p.at(propJSDoc).SetParent(p.at(node))
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: node, jsDocs: []ast.NodeRef{propJSDoc}})
		p.at(node).SetFlags(p.at(node).Flags() | ast.NodeFlagsHasJSDoc)
	}
}

func (p *Parser) gatherTypeParameters(j ast.NodeRef, typedefOrCallback bool) ast.ListRef {
	var typeParameters []ast.NodeRef
	pos := -1
	endPos := -1
	firstTemplate := true
	for _, tag := range p.listHandles(p.at(j).JSDocTags()) {
		tagH := p.at(tag)
		if !typedefOrCallback && (tagH.Kind == ast.KindJSDocTypedefTag || tagH.Kind == ast.KindJSDocCallbackTag) {
			return 0
		}
		if tagH.Kind != ast.KindJSDocTemplateTag {
			continue
		}
		if firstTemplate {
			pos = tagH.Pos()
			firstTemplate = false
		}
		endPos = tagH.End()
		constraint := tagH.JSDocTemplateTagConstraint()
		firstTypeParameter := true
		for _, tp := range tagH.TypeParameters() {
			var reparse ast.NodeRef
			if !constraint.IsNil() && firstTypeParameter {
				reparse = p.factory.ParseTypeParameterDeclaration(
					p.addDeepCloneReparseModifiers(tp.Modifiers()),
					p.addDeepCloneReparse(p.checkNonIdentifierName(tp.Name().Ref())),
					p.addDeepCloneReparse(constraint.Type().Ref()), 0,
					p.addDeepCloneReparse(tp.TypeParameterDeclarationDefaultType().Ref()),
				)
				p.finishReparsedNode(reparse, tp.Ref())
			} else {
				reparse = p.addDeepCloneReparse(tp.Ref())
			}
			if typeParameters == nil {
				typeParameters = make([]ast.NodeRef, 0)
			}
			typeParameters = append(typeParameters, reparse)
			firstTypeParameter = false
		}
	}
	if len(typeParameters) == 0 {
		return 0
	} else {
		return p.newListRefs(core.NewTextRange(pos, endPos), typeParameters)
	}
}

func (p *Parser) reparseHosted(tag ast.NodeRef, parent ast.NodeRef, jsDoc ast.NodeRef) {
	tagH := p.at(tag)
	parentH := p.at(parent)
	switch tagH.Kind {
	case ast.KindJSDocTypeTag:
		switch parentH.Kind {
		case ast.KindVariableStatement:
			if !parentH.VariableStatementDeclarationList().IsNil() {
				for _, declaration := range p.listHandles(parentH.VariableStatementDeclarationList().VariableDeclarationListDeclarations()) {
					declH := p.at(declaration)
					if declH.Type().IsNil() && !tagH.TypeExpression().IsNil() {
						declH.SetType(p.at(p.addDeepCloneReparse(tagH.TypeExpression().Type().Ref())))
						p.finishMutatedNode(declaration)
						return
					}
				}
			}
		case ast.KindVariableDeclaration, ast.KindExportAssignment, ast.KindPropertyDeclaration, ast.KindPropertyAssignment,
			ast.KindShorthandPropertyAssignment, ast.KindGetAccessor:
			if parentH.Type().IsNil() && !tagH.TypeExpression().IsNil() {
				parentH.SetType(p.at(p.addDeepCloneReparse(tagH.TypeExpression().Type().Ref())))
				p.finishMutatedNode(parent)
				return
			}
		case ast.KindParameter:
			if parentH.Type().IsNil() && !tagH.TypeExpression().IsNil() {
				parentH.SetType(p.at(p.reparseJSDocTypeLiteral(tagH.TypeExpression().Type().Ref())))
				p.finishMutatedNode(parent)
				return
			}
		case ast.KindExpressionStatement:
			if parentH.Expression().Kind == ast.KindBinaryExpression {
				bin := parentH.Expression()
				if bin.LooksLikeAssignmentDeclaration() && !tagH.TypeExpression().IsNil() {
					bin.SetType(p.at(p.addDeepCloneReparse(tagH.TypeExpression().Type().Ref())))
					p.finishMutatedNode(bin.Ref())
					return
				}
			}
		case ast.KindReturnStatement, ast.KindParenthesizedExpression:
			if !parentH.Expression().IsNil() && !tagH.TypeExpression().IsNil() {
				parentH.SetExpression(p.at(p.makeNewCast(
					p.addDeepCloneReparse(tagH.TypeExpression().Type().Ref()),
					parentH.Expression().Ref(),
					true, /*isAssertion*/
				)))
				p.finishMutatedNode(parent)
				return
			}
		}
		if fun := p.getFunctionLikeHost(parent); fun != 0 {
			noTypedParams := core.Every(p.at(fun).Parameters(), func(param ast.Handle) bool { return param.Type().IsNil() })
			if p.at(fun).TypeParameterList() == 0 && p.at(fun).Type().IsNil() && noTypedParams && !tagH.TypeExpression().IsNil() {
				p.at(fun).SetFullSignature(p.at(p.addDeepCloneReparse(tagH.TypeExpression().Type().Ref())))
				p.finishMutatedNode(fun)
			}
		}
	case ast.KindJSDocSatisfiesTag:
		switch parentH.Kind {
		case ast.KindVariableStatement:
			if !parentH.VariableStatementDeclarationList().IsNil() {
				for _, declaration := range p.listHandles(parentH.VariableStatementDeclarationList().VariableDeclarationListDeclarations()) {
					declH := p.at(declaration)
					if !declH.Initializer().IsNil() && !tagH.TypeExpression().IsNil() {
						declH.SetInitializer(p.at(p.makeNewCast(
							p.addDeepCloneReparse(tagH.TypeExpression().Type().Ref()),
							declH.Initializer().Ref(),
							false, /*isAssertion*/
						)))
						p.finishMutatedNode(declaration)
						break
					}
				}
			}
		case ast.KindVariableDeclaration, ast.KindPropertyDeclaration, ast.KindPropertyAssignment:
			if !parentH.Initializer().IsNil() && !tagH.TypeExpression().IsNil() {
				parentH.SetInitializer(p.at(p.makeNewCast(
					p.addDeepCloneReparse(tagH.TypeExpression().Type().Ref()),
					parentH.Initializer().Ref(),
					false, /*isAssertion*/
				)))
				p.finishMutatedNode(parent)
			}
		case ast.KindShorthandPropertyAssignment:
			if !parentH.ShorthandPropertyAssignmentObjectAssignmentInitializer().IsNil() && !tagH.JSDocSatisfiesTagTypeExpression().IsNil() {
				parentH.SetShorthandPropertyAssignmentObjectAssignmentInitializer(p.at(p.makeNewCast(
					p.addDeepCloneReparse(tagH.JSDocSatisfiesTagTypeExpression().Type().Ref()),
					parentH.ShorthandPropertyAssignmentObjectAssignmentInitializer().Ref(),
					false, /*isAssertion*/
				)))
				p.finishMutatedNode(parent)
			}
		case ast.KindReturnStatement, ast.KindParenthesizedExpression, ast.KindExportAssignment:
			if !parentH.Expression().IsNil() && !tagH.TypeExpression().IsNil() {
				parentH.SetExpression(p.at(p.makeNewCast(
					p.addDeepCloneReparse(tagH.TypeExpression().Type().Ref()),
					parentH.Expression().Ref(),
					false, /*isAssertion*/
				)))
				p.finishMutatedNode(parent)
			}
		case ast.KindExpressionStatement:
			if parentH.Expression().Kind == ast.KindBinaryExpression {
				bin := parentH.Expression()
				if bin.LooksLikeAssignmentDeclaration() && !tagH.TypeExpression().IsNil() {
					bin.SetBinaryExpressionRight(p.at(p.makeNewCast(
						p.addDeepCloneReparse(tagH.TypeExpression().Type().Ref()),
						bin.BinaryExpressionRight().Ref(),
						false, /*isAssertion*/
					)))
					p.finishMutatedNode(bin.Ref())
				}
			}
		}
	case ast.KindJSDocTemplateTag:
		if fun := p.getFunctionLikeHost(parent); fun != 0 {
			if p.at(fun).TypeParameterList() == 0 && p.at(fun).FullSignature().IsNil() {
				p.at(fun).SetTypeParameterList(p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
				p.finishMutatedNode(fun)
			}
		} else if parentH.Kind == ast.KindClassDeclaration {
			if parentH.ClassDeclarationTypeParameters() == 0 {
				parentH.SetClassDeclarationTypeParameters(p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
				p.finishMutatedNode(parent)
			}
		} else if parentH.Kind == ast.KindClassExpression {
			if parentH.ClassExpressionTypeParameters() == 0 {
				parentH.SetClassExpressionTypeParameters(p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
				p.finishMutatedNode(parent)
			}
		}
	case ast.KindJSDocParameterTag:
		if fun := p.getFunctionLikeHost(parent); fun != 0 && p.at(fun).FullSignature().IsNil() {
			if param, ok := p.findMatchingParameter(fun, tag, jsDoc); ok {
				if p.at(param).Type().IsNil() && !tagH.JSDocParameterOrPropertyTagTypeExpression().IsNil() {
					p.at(param).SetParameterDeclarationType(p.at(p.reparseJSDocTypeLiteral(tagH.JSDocParameterOrPropertyTagTypeExpression().Type().Ref())))
				}
				if p.at(param).ParamQuestion().IsNil() {
					if question := p.makeQuestionIfOptional(tag); question != 0 {
						p.at(param).SetParamQuestion(p.at(question))
					}
				}
				p.finishMutatedNode(param)
			}
		}
	case ast.KindJSDocThisTag:
		if fun := p.getFunctionLikeHost(parent); fun != 0 {
			params := p.at(fun).Parameters()
			if len(params) == 0 || (params[0].Name().Kind != ast.KindThisKeyword && !ast.IsThisIdentifier(params[0].Name())) {
				thisParam := p.factory.ParseParameterDeclaration(
					0, 0, p.factory.ParseIdentifier("this"), 0, 0, 0,
				)
				if !tagH.JSDocThisTagTypeExpression().IsNil() {
					p.at(thisParam).SetParameterDeclarationType(p.at(p.addDeepCloneReparse(tagH.JSDocThisTagTypeExpression().Type().Ref())))
				}
				p.finishReparsedNode(thisParam, tagH.TagName().Ref())

				newParams := make([]ast.NodeRef, len(params)+1)
				newParams[0] = thisParam
				for i, param := range params {
					newParams[i+1] = param.Ref()
				}

				p.at(fun).SetParameters(p.newListRefs(p.factory.Store().ListLoc(p.at(fun).ParameterList()), newParams))
				p.finishMutatedNode(fun)
			}
		}
	case ast.KindJSDocReturnTag:
		if fun := p.getFunctionLikeHost(parent); fun != 0 && p.at(fun).FullSignature().IsNil() {
			if p.at(fun).Type().IsNil() && !tagH.TypeExpression().IsNil() {
				p.at(fun).SetType(p.at(p.addDeepCloneReparse(tagH.TypeExpression().Type().Ref())))
				p.finishMutatedNode(fun)
			}
		}
	case ast.KindJSDocReadonlyTag, ast.KindJSDocPrivateTag, ast.KindJSDocPublicTag, ast.KindJSDocProtectedTag, ast.KindJSDocOverrideTag:
		if parentH.Kind == ast.KindExpressionStatement {
			parent = parentH.Expression().Ref()
			parentH = p.at(parent)
		}
		switch parentH.Kind {
		case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
			// In object literals these aren't parent-like members, so JSDoc modifiers like @override
			// or @readonly aren't real modifiers there; reparsing them produces spurious grammar errors (#4437).
			if p.parsingContexts&(1<<PCObjectLiteralMembers) != 0 {
				return
			}
			fallthrough
		case ast.KindPropertyDeclaration, ast.KindConstructor, ast.KindBinaryExpression:
			var keyword ast.Kind
			switch tagH.Kind {
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
			modifier := p.factory.ParseToken(keyword)
			p.at(modifier).SetLoc(tagH.Loc())
			p.at(modifier).SetFlags(p.contextFlags | ast.NodeFlagsReparsed)
			var nodes []ast.NodeRef
			var loc core.TextRange
			if parentH.Modifiers() == 0 {
				nodes = []ast.NodeRef{modifier}
				loc = tagH.Loc()
			} else {
				nodes = append(p.listHandles(parentH.Modifiers()), modifier)
				loc = p.factory.Store().ListLoc(parentH.Modifiers())
			}
			parentH.SetModifiers(p.newListRefs(loc, nodes))
			p.finishMutatedNode(parent)
		}
	case ast.KindJSDocImplementsTag:
		if parentH.Kind != ast.KindClassDeclaration && parentH.Kind != ast.KindClassExpression {
			break
		}
		className := tagH.JSDocImplementsTagClassName()
		heritage := parentH.HeritageClauses()
		if heritage != 0 {
			for _, clause := range p.listHandles(heritage) {
				clauseH := p.at(clause)
				if clauseH.HeritageClauseToken() == ast.KindImplementsKeyword {
					types := append(p.listHandles(clauseH.HeritageClauseTypes()), p.addDeepCloneReparse(className.Ref()))
					clauseH.SetHeritageClauseTypes(p.newListRefs(p.factory.Store().ListLoc(clauseH.HeritageClauseTypes()), types))
					p.finishMutatedNode(clause)
					return
				}
			}
		}
		typesList := p.newListRefs(className.Loc(), []ast.NodeRef{p.addDeepCloneReparse(className.Ref())})
		heritageClause := p.factory.ParseHeritageClause(ast.KindImplementsKeyword, typesList)
		p.finishReparsedNode(heritageClause, className.Ref())
		if heritage == 0 {
			parentH.SetHeritageClauses(p.newListRefs(className.Loc(), []ast.NodeRef{heritageClause}))
		} else {
			parentH.SetHeritageClauses(p.newListRefs(p.factory.Store().ListLoc(heritage), append(p.listHandles(heritage), heritageClause)))
		}
		p.finishMutatedNode(parent)
	case ast.KindJSDocAugmentsTag:
		if (parentH.Kind != ast.KindClassDeclaration && parentH.Kind != ast.KindClassExpression) || parentH.HeritageClauses() == 0 {
			break
		}
		source := tagH.JSDocAugmentsTagClassName()
		for _, clause := range p.listHandles(parentH.HeritageClauses()) {
			clauseH := p.at(clause)
			if clauseH.HeritageClauseToken() != ast.KindExtendsKeyword {
				continue
			}
			types := p.listHandles(clauseH.HeritageClauseTypes())
			if len(types) != 1 {
				continue
			}
			target := p.at(types[0])
			if handleText(target.Expression()) == handleText(source.Expression()) {
				if target.ExpressionWithTypeArgumentsTypeArguments() == 0 && source.ExpressionWithTypeArgumentsTypeArguments() != 0 {
					var newArguments []ast.NodeRef
					for _, arg := range p.listHandles(source.ExpressionWithTypeArgumentsTypeArguments()) {
						newArguments = append(newArguments, p.addDeepCloneReparse(arg))
					}
					target.SetExpressionWithTypeArgumentsTypeArguments(p.newListRefs(p.factory.Store().ListLoc(source.ExpressionWithTypeArgumentsTypeArguments()), newArguments))
					p.finishMutatedNode(target.Ref())
				}
			}
		}
	}
}

func (p *Parser) makeQuestionIfOptional(parameter ast.NodeRef) ast.NodeRef {
	var questionToken ast.NodeRef
	if p.at(parameter).JSDocParameterOrPropertyTagIsBracketed() ||
		(!p.at(parameter).JSDocParameterOrPropertyTagTypeExpression().IsNil() &&
			p.at(parameter).JSDocParameterOrPropertyTagTypeExpression().Type().Kind == ast.KindJSDocOptionalType) {
		questionToken = p.factory.ParseToken(ast.KindQuestionToken)
		p.at(questionToken).SetLoc(p.at(parameter).Loc())
		p.at(questionToken).SetFlags(p.contextFlags | ast.NodeFlagsReparsed)
	}
	return questionToken
}

func (p *Parser) findMatchingParameter(fun ast.NodeRef, parameterTag ast.NodeRef, jsDoc ast.NodeRef) (ast.NodeRef, bool) {
	tagIndex := -1
	paramCount := -1
	s := p.factory.Store()
	tags := p.at(jsDoc).JSDocTags()
	for i := 0; i < s.ListLen(tags); i++ {
		tag := s.ListRefAt(tags, i)
		if s.KindAt(tag) == ast.KindJSDocParameterTag {
			paramCount++
			if tag == parameterTag {
				tagIndex = paramCount
				break
			}
		}
	}
	params := p.at(fun).ParameterList()
	for parameterIndex := 0; parameterIndex < s.ListLen(params); parameterIndex++ {
		parameter := s.ListRefAt(params, parameterIndex)
		name := p.at(parameter).Name()
		tagName := p.at(parameterTag).Name()
		if name.Kind == ast.KindIdentifier {
			if tagName.Kind == ast.KindIdentifier &&
				((name.Text() == tagName.Text()) || (parameterIndex == tagIndex && len(tagName.Text()) == 0)) {
				return parameter, true
			}
		} else if parameterIndex == tagIndex {
			return parameter, true
		}
	}
	return 0, false
}

func (p *Parser) skipSatisfiesExpressions(node ast.NodeRef) ast.NodeRef {
	for node != 0 && p.factory.Store().KindAt(node) == ast.KindSatisfiesExpression {
		node = p.at(node).Expression().Ref()
	}
	return node
}

func (p *Parser) getFunctionLikeHost(host ast.NodeRef) ast.NodeRef {
	fun := host
	s := p.factory.Store()
	switch s.KindAt(host) {
	case ast.KindVariableStatement:
		declList := p.at(host).VariableStatementDeclarationList()
		if !declList.IsNil() {
			decls := declList.VariableDeclarationListDeclarations()
			if decls != 0 && s.ListLen(decls) != 0 {
				fun = s.ListAt(decls, 0).Initializer().Ref()
			}
		}
	case ast.KindPropertyAssignment, ast.KindPropertyDeclaration:
		fun = p.at(host).Initializer().Ref()
	case ast.KindExportAssignment, ast.KindReturnStatement:
		fun = p.at(host).Expression().Ref()
	case ast.KindExpressionStatement:
		fun = p.at(host).Expression().RightMostAssigned().Ref()
	}
	fun = p.skipSatisfiesExpressions(fun)
	if ast.IsFunctionLike(p.at(fun)) {
		return fun
	}
	return 0
}

func (p *Parser) makeNewCast(t ast.NodeRef, e ast.NodeRef, isAssertion bool) ast.NodeRef {
	var assert ast.NodeRef
	if isAssertion {
		assert = p.factory.ParseAsExpression(e, t)
	} else {
		assert = p.factory.ParseSatisfiesExpression(e, t)
	}
	p.finishParseWithEnd(assert, p.at(e).Pos(), p.at(e).End())
	return assert
}

func (p *Parser) createExportModifier(locationNode ast.NodeRef) ast.ListRef {
	exportModifier := p.factory.ParseToken(ast.KindExportKeyword)
	p.at(exportModifier).SetLoc(p.at(locationNode).Loc())
	p.at(exportModifier).SetFlags(p.contextFlags | ast.NodeFlagsReparsed)
	nodes := []ast.NodeRef{exportModifier}
	return p.newListRefs(p.at(locationNode).Loc(), nodes)
}

// getInnermostNameOfJSDocNamespace returns the innermost identifier from a
// JSDoc namespace chain (ModuleDeclaration). For a simple identifier, it returns
// the identifier itself. For "A.B.C", it returns the identifier "C".
func (p *Parser) getInnermostNameOfJSDocNamespace(fullName ast.NodeRef) ast.NodeRef {
	if fullName == 0 {
		return 0
	}
	for p.factory.Store().KindAt(fullName) == ast.KindModuleDeclaration {
		body := p.at(fullName).ModuleDeclarationBody()
		if body.IsNil() {
			return p.at(fullName).Name().Ref()
		}
		fullName = body.Ref()
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
func (p *Parser) wrapInJSDocNamespace(fullName ast.NodeRef, statement ast.NodeRef, nested bool) ast.NodeRef {
	if fullName == 0 || p.factory.Store().KindAt(fullName) != ast.KindModuleDeclaration {
		return statement
	}
	// Recursively wrap from outermost to innermost. Inner namespaces always get an export modifier
	// so members are accessible via dotted access from outside. The outermost namespace is treated as
	// exported only in module files via IsImplicitlyExportedJSDocDeclaration (in the binder), so it
	// does not get an explicit export modifier here.
	wrapped := p.wrapInJSDocNamespace(p.at(fullName).Body().Ref(), statement, true /*nested*/)
	block := p.factory.ParseModuleBlock(p.newListRefs(p.at(fullName).Loc(), []ast.NodeRef{wrapped}))
	p.finishReparsedNode(block, fullName)
	var modifiers ast.ListRef
	if nested {
		modifiers = p.createExportModifier(fullName)
	}
	result := p.factory.ParseModuleDeclaration(modifiers, ast.KindNamespaceKeyword, p.addDeepCloneReparse(p.at(fullName).Name().Ref()), block)
	p.finishReparsedNode(result, fullName)
	p.reparsedClones = append(p.reparsedClones, p.at(result))
	return result
}
