package checker

import (
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

func isExpanding(ctx *NodeBuilderContext) bool {
	return ctx.maxExpansionDepth != -1
}

func (b *NodeBuilderImpl) expandSymbolForHover(symbol *ast.Symbol) []ast.Handle {
	var results []ast.Handle
	if symbol.Flags&ast.SymbolFlagsEnum != 0 {
		if node := b.expandEnumDecl(symbol); !node.IsNil() {
			results = append(results, node)
		}
	}
	if symbol.Flags&ast.SymbolFlagsClass != 0 {
		if node := b.expandClassDecl(symbol); !node.IsNil() {
			results = append(results, node)
		}
	}
	if symbol.Flags&(ast.SymbolFlagsValueModule|ast.SymbolFlagsNamespaceModule) != 0 {
		if node := b.expandModuleDecl(symbol); !node.IsNil() {
			results = append(results, node)
		}
	}
	if symbol.Flags&ast.SymbolFlagsInterface != 0 && symbol.Flags&ast.SymbolFlagsClass == 0 {
		if node := b.expandInterfaceDecl(symbol); !node.IsNil() {
			results = append(results, node)
		}
	}
	return results
}

func (b *NodeBuilderImpl) expandEnumDecl(symbol *ast.Symbol) ast.Handle {
	name := ast.SymbolName(symbol)
	b.ctx.approximateLength += 9 + len(name)
	memberProps := core.Filter(b.ch.getPropertiesOfType(b.ch.getTypeOfSymbol(symbol)), func(p *ast.Symbol) bool {
		return p.Flags&ast.SymbolFlagsEnumMember != 0
	})
	var members []ast.Handle
	for i, p := range memberProps {
		if b.checkTruncationLengthIfExpanding() && i+3 < len(memberProps)-1 {
			b.ctx.expansionTruncated = true
			members = append(members, b.f.NewEnumMember(b.f.NewStringLiteral(fmt.Sprintf(" ... %d more ... ", len(memberProps)-i-1), 0), ast.Handle{}))
			last := memberProps[len(memberProps)-1]
			members = append(members, b.f.NewEnumMember(b.f.NewIdentifier(last.Name), b.enumMemberInitializer(last)))
			break
		}
		memberDecl := ast.FindSymbolDeclaration(p, ast.IsEnumMember)
		var initializer ast.Handle
		if !memberDecl.IsNil() && !memberDecl.EnumMemberInitializer().IsNil() {
			initializer = b.f.DeepCloneNode(memberDecl.EnumMemberInitializer())
		} else {
			initializer = b.enumMemberInitializer(p)
		}
		b.ctx.approximateLength += 4 + len(p.Name)
		if !initializer.IsNil() {
			b.ctx.approximateLength += 5
		}
		members = append(members, b.f.NewEnumMember(b.f.NewIdentifier(p.Name), initializer))
	}
	constModifier := ast.ModifierFlagsNone
	if isConstEnumSymbol(symbol) {
		constModifier = ast.ModifierFlagsConst
	}
	var mods ast.ListRef
	if constModifier != 0 {
		mods = b.f.NewModifierList(ast.CreateModifiersFromModifierFlags(constModifier, b.f.NewModifier))
	}
	return b.f.NewEnumDeclaration(mods, b.f.NewIdentifier(name), b.f.NewList(members))
}
func (b *NodeBuilderImpl) enumMemberInitializer(p *ast.Symbol) ast.Handle {
	memberDecl := ast.FindSymbolDeclaration(p, ast.IsEnumMember)
	if memberDecl.IsNil() {
		return ast.Handle{}
	}
	val := b.ch.GetConstantValue(memberDecl)
	if val == nil {
		return ast.Handle{}
	}
	switch v := val.(type) {
	case string:
		return b.f.NewStringLiteral(v, 0)
	case jsnum.Number:
		return b.f.NewNumericLiteral(v.String(), 0)
	}
	return ast.Handle{}
}

func (b *NodeBuilderImpl) expandClassDecl(symbol *ast.Symbol) ast.Handle {
	name := ast.SymbolName(symbol)
	b.ctx.approximateLength += 9 + len(name)
	classLikeDeclarations := core.Filter(ast.DeclarationNodes(symbol), ast.IsClassLike)
	originalDecl := core.FirstOrNil(classLikeDeclarations)
	oldEnclosing := b.ctx.enclosingDeclaration
	if !originalDecl.IsNil() {
		b.ctx.enclosingDeclaration = originalDecl
	}
	defer func() {
		b.ctx.enclosingDeclaration = oldEnclosing
	}()
	localParams := b.ch.getLocalTypeParametersOfClassOrInterfaceOrTypeAlias(symbol)
	typeParamDecls := core.Map(localParams, func(p *Type) ast.Handle {
		return b.typeParameterToDeclaration(p)
	})
	declaredType := b.ch.getDeclaredTypeOfClassOrInterface(symbol)
	classType := b.ch.getTypeWithThisArgument(declaredType, nil, false)
	baseTypes := b.ch.getBaseTypes(b.ch.getTargetType(classType))
	staticType := b.ch.getTypeOfSymbol(symbol)
	isClass := staticType.symbol != nil && staticType.symbol.ValueDeclaration != 0 && ast.IsClassLike(ast.NodeOf(staticType.symbol.ValueDeclaration))
	var staticBaseType *Type
	if isClass {
		staticBaseType = b.ch.getBaseConstructorTypeOfClass(declaredType)
	} else {
		staticBaseType = b.ch.anyType
	}
	heritageClauses := b.hoverHeritageClauses(classLikeDeclarations)
	allProps := b.ch.getPropertiesOfType(classType)
	symbolProps := b.filterInheritedProperties(classType, baseTypes, allProps)
	publicProps := core.Filter(symbolProps, func(s *ast.Symbol) bool {
		return !isHashPrivate(s)
	})
	hasPrivate := core.Some(symbolProps, isHashPrivate)
	var instanceMembers []ast.Handle
	instanceMembers = b.serializePropertiesWithTruncation(publicProps, instanceMembers)
	instanceMembers = typeElementsToClassElements(b.f, instanceMembers)
	instanceMembers = b.addClassModifiers(instanceMembers, false)
	staticProps := core.Filter(b.ch.getPropertiesOfType(staticType), func(p *ast.Symbol) bool {
		return p.Flags&ast.SymbolFlagsPrototype == 0 && p.Name != "prototype" && !b.isNamespaceMember(p)
	})
	var staticMembers []ast.Handle
	staticMembers = b.serializePropertiesWithTruncation(staticProps, staticMembers)
	staticMembers = typeElementsToClassElements(b.f, staticMembers)
	staticMembers = b.addClassModifiers(staticMembers, true)
	var privateMembers []ast.Handle
	if hasPrivate {
		privateMembers = b.serializePropertiesWithTruncation(core.Filter(symbolProps, isHashPrivate), privateMembers)
		privateMembers = typeElementsToClassElements(b.f, privateMembers)
	}
	constructors := b.serializeConstructors(staticType, staticBaseType, isClass, symbol)
	indexSigs := b.serializeIndexSignaturesOfType(classType, core.FirstOrNil(baseTypes))
	allMembers := make([]ast.Handle, 0, len(indexSigs)+len(staticMembers)+len(constructors)+len(instanceMembers)+len(privateMembers))
	allMembers = append(allMembers, indexSigs...)
	allMembers = append(allMembers, staticMembers...)
	allMembers = append(allMembers, constructors...)
	allMembers = append(allMembers, instanceMembers...)
	allMembers = append(allMembers, privateMembers...)
	return b.f.NewClassDeclaration(0, b.f.NewIdentifier(name), b.f.NewList(typeParamDecls), b.f.NewList(heritageClauses), b.f.NewList(allMembers))
}

func (b *NodeBuilderImpl) addClassModifiers(members []ast.Handle, isStatic bool) []ast.Handle {
	for i, m := range members {
		var memberSymbol *ast.Symbol
		memberName := m.Name()
		if !memberName.IsNil() {
			if sym, ok := b.idToSymbol[memberName]; ok {
				memberSymbol = sym
			}
		}
		if memberSymbol == nil {
			continue
		}
		modFlags := getDeclarationModifierFlagsFromSymbol(memberSymbol) &^ ast.ModifierFlagsAsync
		if isStatic {
			modFlags |= ast.ModifierFlagsStatic
		}
		if modFlags != 0 && ast.CanHaveModifiers(m) {
			existing := m.ModifierFlags()
			if modFlags != existing {
				members[i] = ast.ReplaceHandleModifiers(b.f, m, b.f.NewModifierList(ast.CreateModifiersFromModifierFlags(modFlags|existing, b.f.NewModifier)))
			}
		}
	}
	return members
}

func typeElementsToClassElements(f ast.HandleFactory, members []ast.Handle) []ast.Handle {
	for i, m := range members {
		switch m.Kind() {
		case ast.KindPropertySignature:
			ps := m
			members[i] = f.NewPropertyDeclaration(m.Modifiers(), ps.Name(), ps.QuestionToken(), ps.Type(), ast.Handle{})
		case ast.KindMethodSignature:
			ms := m
			members[i] = f.NewMethodDeclaration(m.Modifiers(), ast.Handle{}, ms.Name(), ms.QuestionToken(), ms.TypeParameterList(), ms.ParameterList(), ms.Type(), ast.Handle{}, ast.Handle{})
		}
	}
	return members
}

func (b *NodeBuilderImpl) expandInterfaceDecl(symbol *ast.Symbol) ast.Handle {
	name := ast.SymbolName(symbol)
	b.ctx.approximateLength += 14 + len(name)
	interfaceType := b.ch.getDeclaredTypeOfClassOrInterface(symbol)
	interfaceDeclarations := core.Filter(ast.DeclarationNodes(symbol), ast.IsInterfaceDeclaration)
	localParams := b.ch.getLocalTypeParametersOfClassOrInterfaceOrTypeAlias(symbol)
	typeParamDecls := core.Map(localParams, func(p *Type) ast.Handle {
		return b.typeParameterToDeclaration(p)
	})
	baseTypes := b.ch.getBaseTypes(interfaceType)
	var baseType *Type
	if len(baseTypes) > 0 {
		baseType = b.ch.getIntersectionType(baseTypes)
	}
	resolved := b.ch.resolveStructuredTypeMembers(interfaceType)
	var members []ast.Handle
	members = append(members, b.serializeIndexSignaturesOfType(interfaceType, baseType)...)
	for _, sig := range resolved.ConstructSignatures() {
		if sig.flags&SignatureFlagsAbstract != 0 {
			continue
		}
		members = append(members, b.signatureToSignatureDeclarationHelper(sig, ast.KindConstructSignature, nil))
	}
	for _, sig := range resolved.CallSignatures() {
		members = append(members, b.signatureToSignatureDeclarationHelper(sig, ast.KindCallSignature, nil))
	}
	filteredProps := b.filterInheritedProperties(interfaceType, baseTypes, resolved.properties)
	members = b.serializePropertiesWithTruncation(filteredProps, members)
	heritageClauses := b.hoverHeritageClauses(interfaceDeclarations)
	return b.f.NewInterfaceDeclaration(0, b.f.NewIdentifier(name), b.f.NewList(typeParamDecls), b.f.NewList(heritageClauses), b.f.NewList(members))
}
func (b *NodeBuilderImpl) hoverHeritageClauses(declarations []ast.Handle) []ast.Handle {
	var extendsTypes []ast.Handle
	var implementsTypes []ast.Handle
	for _, declaration := range declarations {
		for _, heritageElement := range ast.GetExtendsHeritageClauseElements(declaration) {
			extendsTypes = append(extendsTypes, b.f.DeepCloneNode(heritageElement))
		}
		for _, heritageElement := range ast.GetImplementsHeritageClauseElements(declaration) {
			implementsTypes = append(implementsTypes, b.f.DeepCloneNode(heritageElement))
		}
	}
	var heritageClauses []ast.Handle
	if len(extendsTypes) > 0 {
		heritageClauses = append(heritageClauses, b.f.NewHeritageClause(ast.KindExtendsKeyword, b.f.NewList(extendsTypes)))
	}
	if len(implementsTypes) > 0 {
		heritageClauses = append(heritageClauses, b.f.NewHeritageClause(ast.KindImplementsKeyword, b.f.NewList(implementsTypes)))
	}
	return heritageClauses
}

func (b *NodeBuilderImpl) serializePropertiesWithTruncation(properties []*ast.Symbol, elements []ast.Handle) []ast.Handle {
	properties = core.Filter(properties, func(p *ast.Symbol) bool {
		return p.Flags&ast.SymbolFlagsPrototype == 0
	})
	for i, p := range properties {
		if b.checkTruncationLengthIfExpanding() && (i+3 < len(properties)-1) {
			b.ctx.expansionTruncated = true
			text := fmt.Sprintf("... %d more ...", len(properties)-i-1)
			elements = append(elements, b.f.NewPropertySignatureDeclaration(0, b.f.NewIdentifier(text), ast.Handle{}, ast.Handle{}, ast.Handle{}))
			elements = b.addPropertyToElementList(properties[len(properties)-1], elements)
			break
		}
		elements = b.addPropertyToElementList(p, elements)
	}
	return elements
}

func (b *NodeBuilderImpl) serializeConstructors(staticType *Type, staticBaseType *Type, isClass bool, symbol *ast.Symbol) []ast.Handle {
	isNonConstructable := !isClass && symbol.ValueDeclaration != 0 && ast.IsInJSFile(ast.NodeOf(symbol.ValueDeclaration)) && len(b.ch.getSignaturesOfType(staticType, SignatureKindConstruct)) == 0
	if isNonConstructable {
		b.ctx.approximateLength += 21
		modifiers := ast.CreateModifiersFromModifierFlags(ast.ModifierFlagsPrivate, b.f.NewModifier)
		return []ast.Handle{b.f.NewConstructorDeclaration(b.f.NewModifierList(modifiers), 0, b.f.NewList(nil), ast.Handle{}, ast.Handle{}, ast.Handle{})}
	}
	signatures := b.ch.getSignaturesOfType(staticType, SignatureKindConstruct)
	if staticBaseType != nil {
		baseSigs := b.ch.getSignaturesOfType(staticBaseType, SignatureKindConstruct)
		if len(baseSigs) == 0 && core.Every(signatures, func(sig *Signature) bool {
			return len(sig.parameters) == 0
		}) {
			return nil
		}
		if len(baseSigs) == len(signatures) {
			allMatch := true
			for i := range baseSigs {
				if b.ch.compareSignaturesIdentical(signatures[i], baseSigs[i], false, false, true, b.ch.compareTypesIdentical) != TernaryTrue {
					allMatch = false
					break
				}
			}
			if allMatch {
				return nil
			}
		}
		var privateProtected ast.ModifierFlags
		for _, sig := range signatures {
			if !sig.declaration.IsNil() {
				privateProtected |= sig.declaration.ModifierFlags() & (ast.ModifierFlagsPrivate | ast.ModifierFlagsProtected)
			}
		}
		if privateProtected != 0 {
			return []ast.Handle{b.f.NewConstructorDeclaration(b.f.NewModifierList(ast.CreateModifiersFromModifierFlags(privateProtected, b.f.NewModifier)), 0, b.f.NewList(nil), ast.Handle{}, ast.Handle{}, ast.Handle{})}
		}
	} else if core.Every(signatures, func(sig *Signature) bool {
		return len(sig.parameters) == 0
	}) {
		return nil
	}
	var result []ast.Handle
	for _, sig := range signatures {
		b.ctx.approximateLength++
		result = append(result, b.signatureToSignatureDeclarationHelper(sig, ast.KindConstructor, nil))
	}
	return result
}

func (b *NodeBuilderImpl) serializeIndexSignaturesOfType(input *Type, baseType *Type) []ast.Handle {
	var result []ast.Handle
	for _, info := range b.ch.getIndexInfosOfType(input) {
		if baseType != nil {
			baseInfo := b.ch.getIndexInfoOfType(baseType, info.keyType)
			if baseInfo != nil && b.ch.isTypeIdenticalTo(info.valueType, baseInfo.valueType) {
				continue
			}
		}
		result = append(result, b.indexInfoToIndexSignatureDeclarationHelper(info, ast.Handle{}))
	}
	return result
}

func (b *NodeBuilderImpl) serializeNamespaceMember(resolved *ast.Symbol, name string) ast.Handle {
	switch {
	case resolved.Flags&ast.SymbolFlagsTypeAlias != 0:
		return b.serializeTypeAliasForNamespace(resolved, name)
	case resolved.Flags&ast.SymbolFlagsEnum != 0:
		return b.expandEnumDecl(resolved)
	case resolved.Flags&ast.SymbolFlagsClass != 0:
		return b.expandClassDecl(resolved)
	case resolved.Flags&ast.SymbolFlagsInterface != 0:
		return b.expandInterfaceDecl(resolved)
	case resolved.Flags&(ast.SymbolFlagsValueModule|ast.SymbolFlagsNamespaceModule) != 0:
		return b.expandModuleDecl(resolved)
	default:
		t := b.ch.getWidenedType(b.ch.getTypeOfSymbol(resolved))
		b.ctx.approximateLength += len(name) + 5
		return b.f.NewVariableStatement(0, b.f.NewVariableDeclarationList(b.f.NewList([]ast.Handle{b.f.NewVariableDeclaration(b.f.NewIdentifier(name), ast.Handle{}, b.serializeTypeForDeclaration(ast.Handle{}, t, resolved, true), ast.Handle{})}), ast.NodeFlagsLet))
	}
}

func (b *NodeBuilderImpl) expandModuleDecl(symbol *ast.Symbol) ast.Handle {
	exports := b.ch.getExportsOfSymbol(symbol)
	var members []*ast.Symbol
	for _, sym := range exports {
		if !b.isNamespaceMember(sym) {
			continue
		}
		if !scanner.IsIdentifierText(sym.Name, core.LanguageVariantStandard) {
			continue
		}
		members = append(members, sym)
	}
	b.ch.sortSymbols(members)
	b.ctx.approximateLength += 14
	oldFlags := b.ctx.flags
	defer func() {
		b.ctx.flags = oldFlags
	}()
	b.ctx.flags |= nodebuilder.FlagsWriteTypeParametersInQualifiedName | nodebuilder.Flags(SymbolFormatFlagsUseOnlyExternalAliasing)
	localName := b.symbolToNode(symbol, ast.SymbolFlagsAll)
	b.ctx.flags = oldFlags
	type hoverStatement struct {
		node    ast.Handle
		isLocal bool
	}
	var bodyStmts []hoverStatement
	var emittedLocals collections.Set[*ast.Symbol]
	for i := 0; i < len(members); i++ {
		m := members[i]
		if b.checkTruncationLengthIfExpanding() && i+3 < len(members)-1 {
			b.ctx.expansionTruncated = true
			bodyStmts = append(bodyStmts, hoverStatement{node: b.f.NewExpressionStatement(b.f.NewIdentifier(fmt.Sprintf("... (%d more) ...", len(members)-i-1)))})
			i = len(members) - 2
			continue
		}
		if m.Flags&ast.SymbolFlagsAlias != 0 {
			aliasDecl := b.ch.getDeclarationOfAliasSymbol(m)
			target := b.ch.getMergedSymbol(b.ch.getTargetOfAliasDeclaration(aliasDecl))
			if target != nil {
				if target.Flags&(ast.SymbolFlagsBlockScopedVariable|ast.SymbolFlagsFunctionScopedVariable|ast.SymbolFlagsProperty) != 0 {
					if emittedLocals.AddIfAbsent(target) {
						localType := b.ch.getWidenedType(b.ch.getTypeOfSymbol(target))
						b.ctx.approximateLength += len(target.Name) + 5
						localStmt := b.f.NewVariableStatement(0, b.f.NewVariableDeclarationList(b.f.NewList([]ast.Handle{b.f.NewVariableDeclaration(b.f.NewIdentifier(target.Name), ast.Handle{}, b.serializeTypeForDeclaration(ast.Handle{}, localType, target, true), ast.Handle{})}), ast.NodeFlagsLet))
						bodyStmts = append(bodyStmts, hoverStatement{node: localStmt, isLocal: true})
					}
				}
				targetName := target.Name
				b.ctx.approximateLength += 16 + len(m.Name)
				var propertyName ast.Handle
				if m.Name != targetName {
					propertyName = b.f.NewIdentifier(targetName)
				}
				stmt := b.f.NewExportDeclaration(0, false, b.f.NewNamedExports(b.f.NewList([]ast.Handle{b.f.NewExportSpecifier(false, propertyName, b.f.NewIdentifier(m.Name))})), ast.Handle{}, ast.Handle{})
				bodyStmts = append(bodyStmts, hoverStatement{node: stmt})
				continue
			}
		}
		resolved := b.ch.resolveSymbol(m)
		if resolved.Flags&(ast.SymbolFlagsFunction|ast.SymbolFlagsMethod) != 0 {
			t := b.ch.getTypeOfSymbol(resolved)
			sigs := b.ch.getSignaturesOfType(t, SignatureKindCall)
			for _, sig := range sigs {
				b.ctx.approximateLength++
				decl := b.signatureToSignatureDeclarationHelper(sig, ast.KindFunctionDeclaration, &SignatureToSignatureDeclarationOptions{name: b.f.NewIdentifier(m.Name)})
				bodyStmts = append(bodyStmts, hoverStatement{node: decl})
			}
			merged := b.ch.getMergedSymbol(resolved)
			hasModuleExports := merged.Flags&(ast.SymbolFlagsValueModule|ast.SymbolFlagsNamespaceModule) != 0 && merged.Exports != nil && len(merged.Exports) != 0
			if !hasModuleExports {
				bodyStmts = append(bodyStmts, hoverStatement{node: b.f.NewModuleDeclaration(0, ast.KindNamespaceKeyword, b.f.NewIdentifier(m.Name), b.f.NewModuleBlock(b.f.NewList(nil)))})
			}
			continue
		}
		if node := b.serializeNamespaceMember(resolved, m.Name); !node.IsNil() {
			bodyStmts = append(bodyStmts, hoverStatement{node: node})
		}
	}
	for i := range bodyStmts {
		s := &bodyStmts[i]
		if s.isLocal || ast.IsExportDeclaration(s.node) {
			continue
		}
		if ast.CanHaveModifiers(s.node) {
			mf := s.node.ModifierFlags() | ast.ModifierFlagsExport
			s.node = ast.ReplaceHandleModifiers(b.f, s.node, b.f.NewModifierList(ast.CreateModifiersFromModifierFlags(mf, b.f.NewModifier)))
		}
	}
	bodyStatements := make([]ast.Handle, len(bodyStmts))
	for i := range bodyStmts {
		bodyStatements[i] = bodyStmts[i].node
	}
	allExported := len(bodyStatements) > 0 && core.Every(bodyStatements, func(d ast.Handle) bool {
		return ast.HasSyntacticModifier(d, ast.ModifierFlagsExport)
	})
	if allExported {
		for i, stmt := range bodyStatements {
			if ast.CanHaveModifiers(stmt) {
				mf := stmt.ModifierFlags() &^ ast.ModifierFlagsExport
				bodyStatements[i] = ast.ReplaceHandleModifiers(b.f, stmt, b.f.NewModifierList(ast.CreateModifiersFromModifierFlags(mf, b.f.NewModifier)))
			}
		}
	}
	keyword := ast.KindNamespaceKeyword
	if !ast.IsIdentifier(localName) {
		keyword = ast.KindModuleKeyword
	}
	return b.f.NewModuleDeclaration(0, keyword, localName, b.f.NewModuleBlock(b.f.NewList(bodyStatements)))
}

func (b *NodeBuilderImpl) serializeTypeAliasForNamespace(symbol *ast.Symbol, name string) ast.Handle {
	aliasType := b.ch.getDeclaredTypeOfTypeAlias(symbol)
	typeParams := b.ch.getLocalTypeParametersOfClassOrInterfaceOrTypeAlias(symbol)
	typeParamDecls := core.Map(typeParams, func(p *Type) ast.Handle {
		return b.typeParameterToDeclaration(p)
	})
	restoreFlags := b.saveRestoreFlags()
	b.ctx.flags |= nodebuilder.FlagsInTypeAlias
	typeNode := b.typeToTypeNode(aliasType)
	restoreFlags()
	b.ctx.approximateLength += 8 + len(name)
	return b.f.NewTypeAliasDeclaration(0, b.f.NewIdentifier(name), b.f.NewList(typeParamDecls), typeNode)
}

func (b *NodeBuilderImpl) filterInheritedProperties(t *Type, baseTypes []*Type, properties []*ast.Symbol) []*ast.Symbol {
	if len(baseTypes) == 0 {
		return properties
	}
	propsByName := make(map[string]*ast.Symbol, len(properties))
	for _, p := range properties {
		propsByName[p.Name] = p
	}
	var inherited collections.Set[string]
	for _, base := range baseTypes {
		baseWithThis := b.ch.getTypeWithThisArgument(base, b.ch.getTargetType(t).AsInterfaceType().thisType, false)
		for _, prop := range b.ch.getPropertiesOfType(baseWithThis) {
			if existing, ok := propsByName[prop.Name]; ok && prop.Parent == existing.Parent {
				inherited.Add(prop.Name)
			}
		}
	}
	if inherited.Len() == 0 {
		return properties
	}
	return core.Filter(properties, func(p *ast.Symbol) bool {
		return !inherited.Has(p.Name)
	})
}
func (b *NodeBuilderImpl) isNamespaceMember(p *ast.Symbol) bool {
	return p.Flags&(ast.SymbolFlagsType|ast.SymbolFlagsNamespace|ast.SymbolFlagsAlias) != 0 || !(p.Flags&ast.SymbolFlagsPrototype != 0 || p.Name == "prototype" || (p.ValueDeclaration != 0 && ast.HasStaticModifier(ast.NodeOf(p.ValueDeclaration)) && ast.IsClassLike(ast.NodeOf(p.ValueDeclaration).Parent())))
}
func isHashPrivate(s *ast.Symbol) bool {
	return s.ValueDeclaration != 0 && !ast.NodeOf(s.ValueDeclaration).Name().IsNil() && ast.IsPrivateIdentifier(ast.NodeOf(s.ValueDeclaration).Name())
}
