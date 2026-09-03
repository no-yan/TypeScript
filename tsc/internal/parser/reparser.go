package parser

import (
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

func (p *Parser) finishReparsedNode(node ast.Handle, locationNode ast.Handle) {
	node.SetFlags(p.contextFlags | ast.NodeFlagsReparsed)
	node.SetLoc(locationNode.Loc())
	node.SetParentsInChildren()
}

func (p *Parser) finishMutatedNode(node ast.Handle) {
	node.SetParentsInChildren()
}

// Deep-clone the given node and add the clone to the reparsed clone list. The list is used by ast.GetReparsedNodeForNode
// to locate reparsed clones of JSDoc nodes. Since the binder attaches symbols to reparsed nodes and not to JSDoc nodes, we
// need the mapping when obtaining symbols and types from JSDoc nodes.
func (p *Parser) addDeepCloneReparse(node ast.Handle) ast.Handle {
	clone := p.factory.CopySubtree(node)
	if !clone.IsNil() {
		clone.SetFlags(clone.Flags() | p.contextFlags | ast.NodeFlagsReparsed)
		p.reparsedClones = append(p.reparsedClones, clone)
	}
	return clone
}

func (p *Parser) addTransformedReparse(newNode ast.Handle, old ast.Handle) ast.Handle {
	p.finishReparsedNode(newNode, old)
	newNode.SetFlags(newNode.Flags() | ast.NodeFlagsReparserTransformedLiteral)
	p.reparsedClones = append(p.reparsedClones, newNode)
	return newNode
}

func (p *Parser) addDeepCloneReparseModifiers(list ast.ListRef) ast.ListRef {
	if list == 0 {
		return 0
	}
	cloned := make([]ast.Handle, 0, p.factory.Store().ListLen(list))
	for _, h := range p.listHandles(list) {
		cloned = append(cloned, p.addDeepCloneReparse(h))
	}
	return p.newList(p.factory.Store().ListLoc(list), cloned)
}

func (p *Parser) checkNonIdentifierName(name ast.Handle) ast.Handle {
	// Handles the case of anonymous functions
	if name.IsNil() {
		return ast.Handle{}
	}
	if name.Kind == ast.KindIdentifier && !scanner.IsValidIdentifier(name.IdentifierText()) {
		errLoc := name.Loc()
		if errLoc.Len() == 0 { // missing name, emit error on the character before the missing name node
			errLoc = core.NewTextRange(name.Loc().Pos()-1, name.Loc().Pos())
		}
		p.parseErrorAtRange(errLoc, diagnostics.Identifier_expected)
	}
	return name
}

// Hosted tags find a host and add their children to the correct location under the host.
// Unhosted tags add synthetic nodes to the reparse list.
func (p *Parser) reparseTags(parent ast.Handle, jsDoc []ast.Handle) {
	for _, j := range jsDoc {
		isLast := j == jsDoc[len(jsDoc)-1]
		tags := j.JSDocTags()
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

func (p *Parser) reparseUnhosted(tag ast.Handle, parent ast.Handle, jsDoc ast.Handle) {
	switch tag.Kind {
	case ast.KindJSDocTypedefTag:
		typeExpression := tag.TypeExpression()
		if typeExpression.IsNil() {
			break
		}
		fullName := tag.Name()
		isNamespace := !fullName.IsNil() && fullName.Kind == ast.KindModuleDeclaration
		var modifiers ast.ListRef
		if isNamespace {
			modifiers = p.createExportModifier(tag)
		}
		typeAlias := p.factory.NewJSTypeAliasDeclaration(modifiers, p.addDeepCloneReparse(p.checkNonIdentifierName(p.getInnermostNameOfJSDocNamespace(fullName))), 0, ast.Handle{})
		typeAlias.SetTypeAliasDeclarationTypeParameters(p.gatherTypeParameters(jsDoc, true /*typedefOrCallback*/))
		var t ast.Handle
		switch typeExpression.Kind {
		case ast.KindJSDocTypeExpression:
			t = p.addDeepCloneReparse(typeExpression.Type())
		case ast.KindJSDocTypeLiteral:
			t = p.reparseJSDocTypeLiteral(typeExpression)
		default:
			panic("typedef tag type expression should be a name reference or a type expression" + typeExpression.Kind.String())
		}
		typeAlias.SetTypeAliasDeclarationType(t)
		p.finishReparsedNode(typeAlias, tag)
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: typeAlias, jsDocs: []ast.Handle{jsDoc}})
		typeAlias.SetFlags(typeAlias.Flags() | ast.NodeFlagsHasJSDoc)
		result := p.wrapInJSDocNamespace(fullName, typeAlias, false /*nested*/)
		p.reparseList = append(p.reparseList, result)
	case ast.KindJSDocCallbackTag:
		typeExpression := tag.TypeExpression()
		if typeExpression.IsNil() {
			break
		}
		fullName := tag.Name()
		isNamespace := !fullName.IsNil() && fullName.Kind == ast.KindModuleDeclaration
		var modifiers ast.ListRef
		if isNamespace {
			modifiers = p.createExportModifier(tag)
		}
		functionType := p.reparseJSDocSignature(typeExpression, tag, jsDoc, tag, 0)
		typeAlias := p.factory.NewJSTypeAliasDeclaration(modifiers, p.addDeepCloneReparse(p.getInnermostNameOfJSDocNamespace(fullName)), 0, functionType)
		typeAlias.SetTypeAliasDeclarationTypeParameters(p.gatherTypeParameters(jsDoc, true /*typedefOrCallback*/))
		p.finishReparsedNode(typeAlias, tag)
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: typeAlias, jsDocs: []ast.Handle{jsDoc}})
		typeAlias.SetFlags(typeAlias.Flags() | ast.NodeFlagsHasJSDoc)
		result := p.wrapInJSDocNamespace(fullName, typeAlias, false /*nested*/)
		p.reparseList = append(p.reparseList, result)
	case ast.KindJSDocImportTag:
		if tag.JSDocImportTagImportClause().IsNil() {
			break
		}
		importClause := p.addDeepCloneReparse(tag.JSDocImportTagImportClause())
		importClause.SetImportClausePhaseModifier(ast.KindTypeKeyword)
		importDeclaration := p.factory.NewJSImportDeclaration(
			p.addDeepCloneReparseModifiers(0),
			importClause,
			p.addDeepCloneReparse(tag.JSDocImportTagModuleSpecifier()),
			p.addDeepCloneReparse(tag.JSDocImportTagAttributes()),
		)
		p.finishReparsedNode(importDeclaration, tag)
		p.reparseList = append(p.reparseList, importDeclaration)
	case ast.KindJSDocOverloadTag:
		// Create overload signatures only for function, method, and constructor declarations outside object literals
		if (parent.Kind == ast.KindFunctionDeclaration || parent.Kind == ast.KindMethodDeclaration || parent.Kind == ast.KindConstructor) && p.parsingContexts&(1<<PCObjectLiteralMembers) == 0 {
			p.reparseList = append(p.reparseList, p.reparseJSDocSignature(tag.JSDocOverloadTagTypeExpression(), parent, jsDoc, tag, parent.Modifiers()))
		}
	}
}

func (p *Parser) reparseJSDocSignature(jsSignature ast.Handle, fun ast.Handle, jsDoc ast.Handle, tag ast.Handle, modifiers ast.ListRef) ast.Handle {
	var signature ast.Handle
	clonedModifiers := p.addDeepCloneReparseModifiers(modifiers)
	switch fun.Kind {
	case ast.KindFunctionDeclaration:
		signature = p.factory.NewFunctionDeclaration(clonedModifiers, ast.Handle{}, p.factory.CopySubtree(p.checkNonIdentifierName(fun.Name())), 0, 0, ast.Handle{}, ast.Handle{}, ast.Handle{})
	case ast.KindMethodDeclaration:
		signature = p.factory.NewMethodDeclaration(clonedModifiers, ast.Handle{}, p.factory.CopySubtree(p.checkNonIdentifierName(fun.Name())), ast.Handle{}, 0, 0, ast.Handle{}, ast.Handle{}, ast.Handle{})
	case ast.KindConstructor:
		signature = p.factory.NewConstructorDeclaration(clonedModifiers, 0, 0, ast.Handle{}, ast.Handle{}, ast.Handle{})
	case ast.KindJSDocCallbackTag:
		signature = p.factory.NewFunctionTypeNode(0, 0, p.factory.NewKeywordTypeNode(ast.KindAnyKeyword))
	default:
		panic("Unexpected kind " + fun.Kind.String())
	}

	if tag.Kind != ast.KindJSDocCallbackTag {
		signature.SetTypeParameters(p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
	}
	parameters := make([]ast.Handle, 0)
	for pi, param := range jsSignature.Parameters() {
		var parameter ast.Handle
		if param.Kind == ast.KindJSDocThisTag {
			thisIdent := p.factory.NewIdentifier("this")
			thisIdent.SetLoc(param.Loc())
			thisIdent.SetFlags(p.contextFlags | ast.NodeFlagsReparsed)
			parameter = p.factory.NewParameterDeclaration(0, ast.Handle{}, thisIdent, ast.Handle{}, ast.Handle{}, ast.Handle{})
			if !param.JSDocThisTagTypeExpression().IsNil() {
				parameter.SetParameterDeclarationType(p.addDeepCloneReparse(param.JSDocThisTagTypeExpression().Type()))
			}
		} else if param.Kind == ast.KindJSDocParameterTag || param.Kind == ast.KindJSDocPropertyTag {
			// Skip sub-property parameters (e.g., @param x.y) - these have QualifiedNames
			// and describe properties of a parent parameter, not standalone parameters.
			if param.JSDocParameterOrPropertyTagName().Kind == ast.KindQualifiedName {
				continue
			}
			var dotDotDotToken ast.Handle
			var paramType ast.Handle

			if !param.JSDocParameterOrPropertyTagTypeExpression().IsNil() {
				if param.JSDocParameterOrPropertyTagTypeExpression().Type().Kind == ast.KindJSDocVariadicType {
					dotDotDotToken = p.factory.NewToken(ast.KindDotDotDotToken)
					dotDotDotToken.SetLoc(param.Loc())
					dotDotDotToken.SetFlags(p.contextFlags | ast.NodeFlagsReparsed)

					variadicType := param.JSDocParameterOrPropertyTagTypeExpression().Type()
					paramType = p.reparseJSDocTypeLiteral(variadicType.Type())
				} else {
					paramType = p.reparseJSDocTypeLiteral(param.JSDocParameterOrPropertyTagTypeExpression().Type())
				}
			}
			name := param.JSDocParameterOrPropertyTagName()
			if name.Kind == ast.KindIdentifier && !scanner.IsValidIdentifier(name.IdentifierText()) {
				// drop invalid chars for _, if empty, write _0, etc., so we have a valid param name to emit later
				result := strings.Builder{}
				for i, ch := range name.IdentifierText() {
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
				name = p.addTransformedReparse(p.factory.NewIdentifier(result.String()), name)
			} else {
				name = p.addDeepCloneReparse(name)
			}
			parameter = p.factory.NewParameterDeclaration(0, dotDotDotToken, name, p.makeQuestionIfOptional(param), paramType, ast.Handle{})
		}
		p.finishReparsedNode(parameter, param)
		parameters = append(parameters, parameter)
		p.reparseJSDocComment(parameter, param)
	}
	signature.SetParameters(p.newList(p.factory.Store().ListLoc(jsSignature.JSDocSignatureParameters()), parameters))

	if !jsSignature.Type().IsNil() && !jsSignature.Type().TypeExpression().IsNil() {
		signature.SetType(p.addDeepCloneReparse(jsSignature.Type().TypeExpression().Type()))
	}
	loc := jsSignature
	if tag.Kind == ast.KindJSDocOverloadTag {
		loc = tag.TagName()
	}
	p.finishReparsedNode(signature, loc)
	return signature
}

func (p *Parser) reparseJSDocTypeLiteral(t ast.Handle) ast.Handle {
	if t.IsNil() {
		return ast.Handle{}
	}
	if t.Kind == ast.KindJSDocTypeLiteral {
		loc := t
		isArrayType := t.JSDocTypeLiteralIsArrayType()
		properties := make([]ast.Handle, 0)
		for _, prop := range p.listHandles(t.JSDocTypeLiteralJSDocPropertyTags()) {
			if prop.Kind != ast.KindJSDocPropertyTag && prop.Kind != ast.KindJSDocParameterTag {
				continue
			}
			name := prop.Name()
			if name.Kind == ast.KindQualifiedName {
				name = name.QualifiedNameRight()
			}
			if name.Kind == ast.KindIdentifier && !scanner.IsValidIdentifier(name.IdentifierText()) {
				name = p.addTransformedReparse(p.factory.NewStringLiteral(name.IdentifierText(), ast.TokenFlagsNone), name)
			} else {
				name = p.addDeepCloneReparse(name)
			}
			property := p.factory.NewPropertySignatureDeclaration(0, name, p.makeQuestionIfOptional(prop), ast.Handle{}, ast.Handle{})
			if !prop.JSDocParameterOrPropertyTagTypeExpression().IsNil() {
				property.SetPropertySignatureDeclarationType(p.reparseJSDocTypeLiteral(prop.JSDocParameterOrPropertyTagTypeExpression().Type()))
			}
			p.finishReparsedNode(property, prop)
			properties = append(properties, property)
			p.reparseJSDocComment(property, prop)
		}
		t = p.factory.NewTypeLiteralNode(p.newList(loc.Loc(), properties))
		if isArrayType {
			p.finishReparsedNode(t, loc)
			t = p.factory.NewArrayTypeNode(t)
		}
		p.finishReparsedNode(t, loc)
		return t
	}
	return p.addDeepCloneReparse(t)
}

func (p *Parser) reparseJSDocComment(node ast.Handle, tag ast.Handle) {
	if comment := tag.CommentList(); comment != 0 {
		newComment := p.newList(p.factory.Store().ListLoc(comment), core.Map(p.listHandles(comment), p.addDeepCloneReparse))

		propJSDoc := p.factory.NewJSDoc(newComment, 0)
		p.finishReparsedNode(propJSDoc, tag)
		propJSDoc.SetParent(node)
		p.jsdocInfos = append(p.jsdocInfos, JSDocInfo{parent: node, jsDocs: []ast.Handle{propJSDoc}})
		node.SetFlags(node.Flags() | ast.NodeFlagsHasJSDoc)
	}
}

func (p *Parser) gatherTypeParameters(j ast.Handle, typedefOrCallback bool) ast.ListRef {
	var typeParameters []ast.Handle
	pos := -1
	endPos := -1
	firstTemplate := true
	for _, tag := range p.listHandles(j.JSDocTags()) {
		// When a JSDoc comment contains an `@typedef` or `@callback` tag, `@template` type parameter
		// declarations apply to the type being defined.
		if !typedefOrCallback && (tag.Kind == ast.KindJSDocTypedefTag || tag.Kind == ast.KindJSDocCallbackTag) {
			return 0
		}
		if tag.Kind != ast.KindJSDocTemplateTag {
			continue
		}
		if firstTemplate {
			pos = tag.Pos()
			firstTemplate = false
		}
		endPos = tag.End()
		constraint := tag.JSDocTemplateTagConstraint()
		firstTypeParameter := true
		for _, tp := range tag.TypeParameters() {
			var reparse ast.Handle
			if !constraint.IsNil() && firstTypeParameter {
				reparse = p.factory.NewTypeParameterDeclaration(
					p.addDeepCloneReparseModifiers(tp.Modifiers()),
					p.addDeepCloneReparse(p.checkNonIdentifierName(tp.Name())),
					p.addDeepCloneReparse(constraint.Type()), ast.Handle{}, // expression
					p.addDeepCloneReparse(tp.TypeParameterDeclarationDefaultType()),
				)
				p.finishReparsedNode(reparse, tp)
			} else {
				reparse = p.addDeepCloneReparse(tp)
			}
			if typeParameters == nil {
				typeParameters = make([]ast.Handle, 0)
			}
			typeParameters = append(typeParameters, reparse)
			firstTypeParameter = false
		}
	}
	if len(typeParameters) == 0 {
		return 0
	} else {
		return p.newList(core.NewTextRange(pos, endPos), typeParameters)
	}
}

func (p *Parser) reparseHosted(tag ast.Handle, parent ast.Handle, jsDoc ast.Handle) {
	switch tag.Kind {
	case ast.KindJSDocTypeTag:
		switch parent.Kind {
		case ast.KindVariableStatement:
			if !parent.VariableStatementDeclarationList().IsNil() {
				for _, declaration := range p.listHandles(parent.VariableStatementDeclarationList().VariableDeclarationListDeclarations()) {
					if declaration.Type().IsNil() && !tag.TypeExpression().IsNil() {
						declaration.SetType(p.addDeepCloneReparse(tag.TypeExpression().Type()))
						p.finishMutatedNode(declaration)
						return
					}
				}
			}
		case ast.KindVariableDeclaration, ast.KindExportAssignment, ast.KindPropertyDeclaration, ast.KindPropertyAssignment,
			ast.KindShorthandPropertyAssignment, ast.KindGetAccessor:
			if parent.Type().IsNil() && !tag.TypeExpression().IsNil() {
				parent.SetType(p.addDeepCloneReparse(tag.TypeExpression().Type()))
				p.finishMutatedNode(parent)
				return
			}
		case ast.KindParameter:
			if parent.Type().IsNil() && !tag.TypeExpression().IsNil() {
				parent.SetType(p.reparseJSDocTypeLiteral(tag.TypeExpression().Type()))
				p.finishMutatedNode(parent)
				return
			}
		case ast.KindExpressionStatement:
			if parent.Expression().Kind == ast.KindBinaryExpression {
				bin := parent.Expression()
				if bin.LooksLikeAssignmentDeclaration() && !tag.TypeExpression().IsNil() {
					bin.SetType(p.addDeepCloneReparse(tag.TypeExpression().Type()))
					p.finishMutatedNode(bin)
					return
				}
			}
		case ast.KindReturnStatement, ast.KindParenthesizedExpression:
			if !parent.Expression().IsNil() && !tag.TypeExpression().IsNil() {
				parent.SetExpression(p.makeNewCast(
					p.addDeepCloneReparse(tag.TypeExpression().Type()),
					parent.Expression(),
					true, /*isAssertion*/
				))
				p.finishMutatedNode(parent)
				return
			}
		}
		if fun := getFunctionLikeHost(parent); !fun.IsNil() {
			noTypedParams := core.Every(fun.Parameters(), func(param ast.Handle) bool { return param.Type().IsNil() })
			if fun.TypeParameterList() == 0 && fun.Type().IsNil() && noTypedParams && !tag.TypeExpression().IsNil() {
				fun.SetFullSignature(p.addDeepCloneReparse(tag.TypeExpression().Type()))
				p.finishMutatedNode(fun)
			}
		}
	case ast.KindJSDocSatisfiesTag:
		switch parent.Kind {
		case ast.KindVariableStatement:
			if !parent.VariableStatementDeclarationList().IsNil() {
				for _, declaration := range p.listHandles(parent.VariableStatementDeclarationList().VariableDeclarationListDeclarations()) {
					if !declaration.Initializer().IsNil() && !tag.TypeExpression().IsNil() {
						declaration.SetInitializer(p.makeNewCast(
							p.addDeepCloneReparse(tag.TypeExpression().Type()),
							declaration.Initializer(),
							false, /*isAssertion*/
						))
						p.finishMutatedNode(declaration)
						break
					}
				}
			}
		case ast.KindVariableDeclaration, ast.KindPropertyDeclaration, ast.KindPropertyAssignment:
			if !parent.Initializer().IsNil() && !tag.TypeExpression().IsNil() {
				parent.SetInitializer(p.makeNewCast(
					p.addDeepCloneReparse(tag.TypeExpression().Type()),
					parent.Initializer(),
					false, /*isAssertion*/
				))
				p.finishMutatedNode(parent)
			}
		case ast.KindShorthandPropertyAssignment:
			if !parent.ShorthandPropertyAssignmentObjectAssignmentInitializer().IsNil() && !tag.JSDocSatisfiesTagTypeExpression().IsNil() {
				parent.SetShorthandPropertyAssignmentObjectAssignmentInitializer(p.makeNewCast(
					p.addDeepCloneReparse(tag.JSDocSatisfiesTagTypeExpression().Type()),
					parent.ShorthandPropertyAssignmentObjectAssignmentInitializer(),
					false, /*isAssertion*/
				))
				p.finishMutatedNode(parent)
			}
		case ast.KindReturnStatement, ast.KindParenthesizedExpression, ast.KindExportAssignment:
			if !parent.Expression().IsNil() && !tag.TypeExpression().IsNil() {
				parent.SetExpression(p.makeNewCast(
					p.addDeepCloneReparse(tag.TypeExpression().Type()),
					parent.Expression(),
					false, /*isAssertion*/
				))
				p.finishMutatedNode(parent)
			}
		case ast.KindExpressionStatement:
			if parent.Expression().Kind == ast.KindBinaryExpression {
				bin := parent.Expression()
				if bin.LooksLikeAssignmentDeclaration() && !tag.TypeExpression().IsNil() {
					bin.SetBinaryExpressionRight(p.makeNewCast(
						p.addDeepCloneReparse(tag.TypeExpression().Type()),
						bin.BinaryExpressionRight(),
						false, /*isAssertion*/
					))
					p.finishMutatedNode(bin)
				}
			}
		}
	case ast.KindJSDocTemplateTag:
		if fun := getFunctionLikeHost(parent); !fun.IsNil() {
			if fun.TypeParameterList() == 0 && fun.FullSignature().IsNil() {
				fun.SetTypeParameterList(p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
				p.finishMutatedNode(fun)
			}
		} else if parent.Kind == ast.KindClassDeclaration {
			if parent.ClassDeclarationTypeParameters() == 0 {
				parent.SetClassDeclarationTypeParameters(p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
				p.finishMutatedNode(parent)
			}
		} else if parent.Kind == ast.KindClassExpression {
			if parent.ClassExpressionTypeParameters() == 0 {
				parent.SetClassExpressionTypeParameters(p.gatherTypeParameters(jsDoc, false /*typedefOrCallback*/))
				p.finishMutatedNode(parent)
			}
		}
	case ast.KindJSDocParameterTag:
		if fun := getFunctionLikeHost(parent); !fun.IsNil() && fun.FullSignature().IsNil() {
			if param, ok := findMatchingParameter(fun, tag, jsDoc); ok {
				if param.Type().IsNil() && !tag.JSDocParameterOrPropertyTagTypeExpression().IsNil() {
					param.SetParameterDeclarationType(p.reparseJSDocTypeLiteral(tag.JSDocParameterOrPropertyTagTypeExpression().Type()))
				}
				if param.ParamQuestion().IsNil() {
					if question := p.makeQuestionIfOptional(tag); !question.IsNil() {
						param.SetParamQuestion(question)
					}
				}
				p.finishMutatedNode(param)
			}
		}
	case ast.KindJSDocThisTag:
		if fun := getFunctionLikeHost(parent); !fun.IsNil() {
			params := fun.Parameters()
			if len(params) == 0 || (params[0].Name().Kind != ast.KindThisKeyword && !ast.IsThisIdentifier(params[0].Name())) {
				thisParam := p.factory.NewParameterDeclaration(
					0, ast.Handle{}, p.factory.NewIdentifier("this"), ast.Handle{}, ast.Handle{}, ast.Handle{},
				)
				if !tag.JSDocThisTagTypeExpression().IsNil() {
					thisParam.SetParameterDeclarationType(p.addDeepCloneReparse(tag.JSDocThisTagTypeExpression().Type()))
				}
				p.finishReparsedNode(thisParam, tag.TagName())

				newParams := make([]ast.Handle, len(params)+1)
				newParams[0] = thisParam
				for i, param := range params {
					newParams[i+1] = param
				}

				fun.SetParameters(p.newList(p.factory.Store().ListLoc(fun.ParameterList()), newParams))
				p.finishMutatedNode(fun)
			}
		}
	case ast.KindJSDocReturnTag:
		if fun := getFunctionLikeHost(parent); !fun.IsNil() && fun.FullSignature().IsNil() {
			if fun.Type().IsNil() && !tag.TypeExpression().IsNil() {
				fun.SetType(p.addDeepCloneReparse(tag.TypeExpression().Type()))
				p.finishMutatedNode(fun)
			}
		}
	case ast.KindJSDocReadonlyTag, ast.KindJSDocPrivateTag, ast.KindJSDocPublicTag, ast.KindJSDocProtectedTag, ast.KindJSDocOverrideTag:
		if parent.Kind == ast.KindExpressionStatement {
			parent = parent.Expression()
		}
		switch parent.Kind {
		case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
			// In object literals these aren't parent-like members, so JSDoc modifiers like @override
			// or @readonly aren't real modifiers there; reparsing them produces spurious grammar errors (#4437).
			if p.parsingContexts&(1<<PCObjectLiteralMembers) != 0 {
				return
			}
			fallthrough
		case ast.KindPropertyDeclaration, ast.KindConstructor, ast.KindBinaryExpression:
			var keyword ast.Kind
			switch tag.Kind {
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
			modifier := p.factory.NewToken(keyword)
			modifier.SetLoc(tag.Loc())
			modifier.SetFlags(p.contextFlags | ast.NodeFlagsReparsed)
			var nodes []ast.Handle
			var loc core.TextRange
			if parent.Modifiers() == 0 {
				nodes = []ast.Handle{modifier}
				loc = tag.Loc()
			} else {
				nodes = append(p.listHandles(parent.Modifiers()), modifier)
				loc = p.factory.Store().ListLoc(parent.Modifiers())
			}
			parent.SetModifiers(p.newList(loc, nodes))
			p.finishMutatedNode(parent)
		}
	case ast.KindJSDocImplementsTag:
		if parent.Kind != ast.KindClassDeclaration && parent.Kind != ast.KindClassExpression {
			break
		}
		className := tag.JSDocImplementsTagClassName()
		heritage := parent.HeritageClauses()
		if heritage != 0 {
			for _, clause := range p.listHandles(heritage) {
				if clause.HeritageClauseToken() == ast.KindImplementsKeyword {
					types := append(p.listHandles(clause.HeritageClauseTypes()), p.addDeepCloneReparse(className))
					clause.SetHeritageClauseTypes(p.newList(p.factory.Store().ListLoc(clause.HeritageClauseTypes()), types))
					p.finishMutatedNode(clause)
					return
				}
			}
		}
		typesList := p.newList(className.Loc(), []ast.Handle{p.addDeepCloneReparse(className)})
		heritageClause := p.factory.NewHeritageClause(ast.KindImplementsKeyword, typesList)
		p.finishReparsedNode(heritageClause, className)
		if heritage == 0 {
			parent.SetHeritageClauses(p.newList(className.Loc(), []ast.Handle{heritageClause}))
		} else {
			parent.SetHeritageClauses(p.newList(p.factory.Store().ListLoc(heritage), append(p.listHandles(heritage), heritageClause)))
		}
		p.finishMutatedNode(parent)
	case ast.KindJSDocAugmentsTag:
		if (parent.Kind != ast.KindClassDeclaration && parent.Kind != ast.KindClassExpression) || parent.HeritageClauses() == 0 {
			break
		}
		source := tag.JSDocAugmentsTagClassName()
		for _, clause := range p.listHandles(parent.HeritageClauses()) {
			if clause.HeritageClauseToken() != ast.KindExtendsKeyword {
				continue
			}
			types := p.listHandles(clause.HeritageClauseTypes())
			if len(types) != 1 {
				continue
			}
			target := types[0]
			if handleText(target.Expression()) == handleText(source.Expression()) {
				if target.ExpressionWithTypeArgumentsTypeArguments() == 0 && source.ExpressionWithTypeArgumentsTypeArguments() != 0 {
					var newArguments []ast.Handle
					for _, arg := range p.listHandles(source.ExpressionWithTypeArgumentsTypeArguments()) {
						newArguments = append(newArguments, p.addDeepCloneReparse(arg))
					}
					target.SetExpressionWithTypeArgumentsTypeArguments(p.newList(p.factory.Store().ListLoc(source.ExpressionWithTypeArgumentsTypeArguments()), newArguments))
					p.finishMutatedNode(target)
				}
			}
		}
	}
}

func (p *Parser) makeQuestionIfOptional(parameter ast.Handle) ast.Handle {
	var questionToken ast.Handle
	if parameter.JSDocParameterOrPropertyTagIsBracketed() ||
		(!parameter.JSDocParameterOrPropertyTagTypeExpression().IsNil() &&
			parameter.JSDocParameterOrPropertyTagTypeExpression().Type().Kind == ast.KindJSDocOptionalType) {
		questionToken = p.factory.NewToken(ast.KindQuestionToken)
		questionToken.SetLoc(parameter.Loc())
		questionToken.SetFlags(p.contextFlags | ast.NodeFlagsReparsed)
	}
	return questionToken
}

func findMatchingParameter(fun ast.Handle, parameterTag ast.Handle, jsDoc ast.Handle) (ast.Handle, bool) {
	tagIndex := -1
	paramCount := -1
	s := jsDoc.Store()
	tags := jsDoc.JSDocTags()
	for i := 0; i < s.ListLen(tags); i++ {
		tag := s.ListAt(tags, i)
		if tag.Kind == ast.KindJSDocParameterTag {
			paramCount++
			if tag == parameterTag {
				tagIndex = paramCount
				break
			}
		}
	}
	for parameterIndex, parameter := range fun.Parameters() {
		if parameter.Name().Kind == ast.KindIdentifier {
			if parameterTag.Name().Kind == ast.KindIdentifier &&
				((parameter.Name().Text() == parameterTag.Name().Text()) || (parameterIndex == tagIndex && len(parameterTag.Name().Text()) == 0)) {
				return parameter, true
			}
		} else if parameterIndex == tagIndex {
			return parameter, true
		}
	}
	return ast.Handle{}, false
}

func skipSatisfiesExpressions(node ast.Handle) ast.Handle {
	for !node.IsNil() && node.Kind == ast.KindSatisfiesExpression {
		node = node.Expression()
	}
	return node
}

func getFunctionLikeHost(host ast.Handle) ast.Handle {
	fun := host
	switch host.Kind {
	case ast.KindVariableStatement:
		decls := host.VariableStatementDeclarationList().VariableDeclarationListDeclarations()
		if decls != 0 {
			s := host.Store()
			if s.ListLen(decls) != 0 {
				fun = s.ListAt(decls, 0).Initializer()
			}
		}
	case ast.KindPropertyAssignment, ast.KindPropertyDeclaration:
		fun = host.Initializer()
	case ast.KindExportAssignment, ast.KindReturnStatement:
		fun = host.Expression()
	case ast.KindExpressionStatement:
		fun = host.Expression().RightMostAssigned()
	}
	fun = skipSatisfiesExpressions(fun)
	if ast.IsFunctionLike(fun) {
		return fun
	}
	return ast.Handle{}
}

func (p *Parser) makeNewCast(t ast.Handle, e ast.Handle, isAssertion bool) ast.Handle {
	var assert ast.Handle
	if isAssertion {
		assert = p.factory.NewAsExpression(e, t)
	} else {
		assert = p.factory.NewSatisfiesExpression(e, t)
	}
	p.finishHandleWithEnd(assert, e.Pos(), e.End())
	return assert
}

func (p *Parser) createExportModifier(locationNode ast.Handle) ast.ListRef {
	exportModifier := p.factory.NewToken(ast.KindExportKeyword)
	exportModifier.SetLoc(locationNode.Loc())
	exportModifier.SetFlags(p.contextFlags | ast.NodeFlagsReparsed)
	nodes := []ast.Handle{exportModifier}
	return p.newList(locationNode.Loc(), nodes)
}

// getInnermostNameOfJSDocNamespace returns the innermost identifier from a
// JSDoc namespace chain (ModuleDeclaration). For a simple identifier, it returns
// the identifier itself. For "A.B.C", it returns the identifier "C".
func (p *Parser) getInnermostNameOfJSDocNamespace(fullName ast.Handle) ast.Handle {
	if fullName.IsNil() {
		return ast.Handle{}
	}
	for fullName.Kind == ast.KindModuleDeclaration {
		body := fullName.ModuleDeclarationBody()
		if body.IsNil() {
			return fullName.Name()
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
func (p *Parser) wrapInJSDocNamespace(fullName ast.Handle, statement ast.Handle, nested bool) ast.Handle {
	if fullName.IsNil() || fullName.Kind != ast.KindModuleDeclaration {
		return statement
	}
	// Recursively wrap from outermost to innermost. Inner namespaces always get an export modifier
	// so members are accessible via dotted access from outside. The outermost namespace is treated as
	// exported only in module files via IsImplicitlyExportedJSDocDeclaration (in the binder), so it
	// does not get an explicit export modifier here.
	wrapped := p.wrapInJSDocNamespace(fullName.Body(), statement, true /*nested*/)
	block := p.factory.NewModuleBlock(p.newList(fullName.Loc(), []ast.Handle{wrapped}))
	p.finishReparsedNode(block, fullName)
	var modifiers ast.ListRef
	if nested {
		modifiers = p.createExportModifier(fullName)
	}
	result := p.factory.NewModuleDeclaration(modifiers, ast.KindNamespaceKeyword, p.addDeepCloneReparse(fullName.Name()), block)
	p.finishReparsedNode(result, fullName)
	p.reparsedClones = append(p.reparsedClones, result)
	return result
}
