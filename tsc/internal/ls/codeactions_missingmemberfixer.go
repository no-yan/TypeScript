package ls

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
	"github.com/microsoft/TypeScript/tsc/internal/ls/autoimport"
	"github.com/microsoft/TypeScript/tsc/internal/ls/change"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"strconv"
)

type preserveOptionalFlags int

const (
	preserveOptionalFlagsMethod preserveOptionalFlags = /*initializer*/ /*typeParameters*/ /*parameters*/ /*fullSignature*/ /*typeParameters*/ /*type*/ /*fullSignature*/ /*body*/ /*signatureOnly*/ /*tracker*/ /*tracker*/ /*types*/ /*modifiers*/ /*initializer*/ /*asteriskToken*/ /*typeParameters*/ /*fullSignature*/ /*signatureOnly*/ /*typeArguments*/ /*isValidTypeOnlyUseSite*/ /*isValidTypeOnlyUseSite*/ /*typeArguments*/ /*multiLine*/ /*modifiers*/ /*dotDotDotToken*/ /*initializer*/ 1 << iota
	preserveOptionalFlagsProperty
	preserveOptionalFlagsAll = preserveOptionalFlagsMethod | preserveOptionalFlagsProperty
)

type missingMemberFixer struct {
	changeTracker *change.Tracker
	typeChecker   *checker.Checker
	program       *compiler.Program
	preferences   lsutil.UserPreferences
	importAdder   autoimport.ImportAdder
	locale        locale.Locale
}

func newMissingMemberFixer(changeTracker *change.Tracker, program *compiler.Program, typeChecker *checker.Checker, preferences lsutil.UserPreferences, importAdder autoimport.ImportAdder, locale locale.Locale) *missingMemberFixer {
	return &missingMemberFixer{changeTracker: changeTracker, typeChecker: typeChecker, program: program, preferences: preferences, importAdder: importAdder, locale: locale}
}
func (f *missingMemberFixer) createNodeBuilder() (*checker.NodeBuilder, map[ast.Handle]*ast.Symbol) {
	idToSymbol := make(map[ast.Handle]*ast.Symbol)
	nodeBuilder := checker.NewNodeBuilderEx(f.typeChecker, f.changeTracker.EmitContext, idToSymbol)
	return nodeBuilder, idToSymbol
}
func (f *missingMemberFixer) createMemberFromSymbol(symbol *ast.Symbol, enclosingDeclaration ast.Handle, sourceFile *ast.SourceFile, body ast.Handle, preserveOptional preserveOptionalFlags, abstract bool) []ast.Handle {
	declarations := ast.DeclarationNodes(symbol)
	declaration := declarations.First()
	quotePreference := lsutil.GetQuotePreference(sourceFile, f.preferences)
	ambient := enclosingDeclaration.Flags()&ast.NodeFlagsAmbient != 0
	signatureOnly := ambient || abstract
	optional := symbol.Flags&ast.SymbolFlagsOptional != 0
	kind := ast.KindPropertySignature
	if !declaration.IsNil() {
		kind = declaration.Kind
	}
	declarationName := createDeclarationName(f.changeTracker.HandleFactory, f.typeChecker, symbol, declaration)
	modifiers := f.createModifiers(symbol, declaration)
	flags := nodebuilder.FlagsNoTruncation
	if quotePreference == lsutil.QuotePreferenceSingle {
		flags |= nodebuilder.FlagsUseSingleQuotesForStringLiteralType
	}
	t := f.typeChecker.GetWidenedType(f.typeChecker.GetTypeOfSymbolAtLocation(symbol, enclosingDeclaration))
	var nodes []ast.Handle
	switch kind {
	case ast.KindPropertySignature, ast.KindPropertyDeclaration:
		nodeBuilder, idToSymbol := f.createNodeBuilder()
		typeNode := f.createTypeNode(t, enclosingDeclaration, flags, nodeBuilder, idToSymbol)
		var questionToken ast.Handle
		if optional && preserveOptional&preserveOptionalFlagsProperty != 0 {
			questionToken = f.changeTracker.HandleFactory.NewToken(ast.KindQuestionToken)
		}
		return append(nodes, f.changeTracker.HandleFactory.NewPropertyDeclaration(modifiers, createPropertyName(f.changeTracker.HandleFactory, declarationName, quotePreference), questionToken, typeNode, ast.Handle{}))
	case ast.KindGetAccessor, ast.KindSetAccessor:
		nodeBuilder, idToSymbol := f.createNodeBuilder()
		accessors := ast.GetAllAccessorDeclarations(ast.DeclarationNodes(symbol).Slice(), declaration)
		var orderedAccessors []ast.Handle
		if accessors.SecondAccessor.IsNil() {
			orderedAccessors = append(orderedAccessors, accessors.FirstAccessor)
		} else {
			orderedAccessors = append(orderedAccessors, accessors.FirstAccessor, accessors.SecondAccessor)
		}
		for _, accessor := range orderedAccessors {
			if ast.IsGetAccessorDeclaration(accessor) {
				nodes = append(nodes, f.changeTracker.HandleFactory.NewGetAccessorDeclaration(modifiers, createPropertyName(f.changeTracker.HandleFactory, declarationName, quotePreference), 0, 0, f.createTypeNode(t, enclosingDeclaration, flags, nodeBuilder, idToSymbol), ast.Handle{}, f.createBody(body, quotePreference, signatureOnly)))
			}
			if ast.IsSetAccessorDeclaration(accessor) {
				parameter := checker.GetSetAccessorValueParameter(accessor)
				if parameter.IsNil() {
					panic("Expected set accessor to have a parameter.")
				}
				nodes = append(nodes, f.changeTracker.HandleFactory.NewSetAccessorDeclaration(modifiers, createPropertyName(f.changeTracker.HandleFactory, declarationName, quotePreference), 0, createDummyParameters(f.changeTracker.HandleFactory, 1, []string{parameter.Name().Text()}, []ast.Handle{f.createTypeNode(t, enclosingDeclaration, flags, nodeBuilder, idToSymbol)}, 1, ast.IsInJSFile(enclosingDeclaration)), ast.Handle{}, ast.Handle{}, f.createBody(body, quotePreference, signatureOnly)))
			}
		}
		return nodes
	case ast.KindMethodSignature, ast.KindMethodDeclaration:
		signatures := f.getCallSignatures(t)
		preserveOptional := optional && preserveOptional&preserveOptionalFlagsMethod != 0
		if len(signatures) == 0 {
			return nil
		}
		if len(declarations) == 1 {
			method := f.createSignatureDeclarationFromSignature(core.FirstOrNil(signatures), ast.KindMethodDeclaration, sourceFile, enclosingDeclaration, f.createBody(body, quotePreference, signatureOnly), modifiers, declarationName, preserveOptional)
			if !method.IsNil() {
				nodes = append(nodes, method)
			}
			return nodes
		}
		for _, signature := range signatures {
			if !signature.Declaration().IsNil() && signature.Declaration().Flags()&ast.NodeFlagsAmbient != 0 {
				continue
			}
			method := f.createSignatureDeclarationFromSignature(signature, ast.KindMethodDeclaration, sourceFile, enclosingDeclaration, ast.Handle{}, modifiers, declarationName, preserveOptional)
			if !method.IsNil() {
				nodes = append(nodes, method)
			}
		}
		if signatureOnly {
			return nodes
		}
		if len(declarations) > len(signatures) {
			signature := f.typeChecker.GetSignatureFromDeclaration(core.LastOrNil(declarations))
			method := f.createSignatureDeclarationFromSignature(signature, ast.KindMethodDeclaration, sourceFile, enclosingDeclaration, f.createBody(body, quotePreference, false), modifiers, declarationName, preserveOptional)
			if !method.IsNil() {
				nodes = append(nodes, method)
			}
		} else {
			method := f.createSignatureDeclarationFromSignatures(signatures, declarationName, preserveOptional, modifiers, quotePreference, body, enclosingDeclaration)
			if !method.IsNil() {
				nodes = append(nodes, method)
			}
		}
		return nodes
	}
	return nil
}
func (f *missingMemberFixer) getCallSignatures(t *checker.Type) []*checker.Signature {
	if t.IsUnion() {
		return core.FlatMap(t.Types(), f.typeChecker.GetCallSignatures)
	}
	return f.typeChecker.GetCallSignatures(t)
}
func (f *missingMemberFixer) createTypeNode(t *checker.Type, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, nodeBuilder *checker.NodeBuilder, idToSymbol map[ast.Handle]*ast.Symbol) ast.Handle {
	return f.importTypeNode(nodeBuilder.TypeToTypeNode(t, enclosingDeclaration, flags, nodebuilder.InternalFlagsNone, nil), idToSymbol)
}
func (f *missingMemberFixer) createModifiers(symbol *ast.Symbol, declaration ast.Handle) ast.ListRef {
	modifierFlags := ast.ModifierFlagsNone
	if !declaration.IsNil() {
		effective := checker.GetDeclarationModifierFlagsFromSymbol(symbol)
		modifierFlags = effective & ast.ModifierFlagsStatic
		if effective&ast.ModifierFlagsPublic != 0 {
			modifierFlags |= ast.ModifierFlagsPublic
		} else if effective&ast.ModifierFlagsProtected != 0 {
			modifierFlags |= ast.ModifierFlagsProtected
		}
		if ast.IsAutoAccessorPropertyDeclaration(declaration) {
			modifierFlags |= ast.ModifierFlagsAccessor
		}
	}
	if f.shouldAddOverrideKeyword(declaration) {
		modifierFlags |= ast.ModifierFlagsOverride
	}
	if modifierFlags == ast.ModifierFlagsNone {
		return 0
	}
	return f.changeTracker.HandleFactory.NewModifierList(ast.CreateModifiersFromModifierFlags(modifierFlags, f.changeTracker.HandleFactory.NewModifier))
}
func (f *missingMemberFixer) shouldAddOverrideKeyword(declaration ast.Handle) bool {
	return !declaration.IsNil() && f.program.Options().NoImplicitOverride.IsTrue() && ast.HasAbstractModifier(declaration)
}
func (f *missingMemberFixer) createSignatureDeclarationFromSignature(signature *checker.Signature, kind ast.Kind, sourceFile *ast.SourceFile, enclosingDeclaration ast.Handle, body ast.Handle, modifiers ast.ListRef, name ast.Handle, optional bool) ast.Handle {
	quotePreference := lsutil.GetQuotePreference(sourceFile, f.preferences)
	flags := nodebuilder.FlagsNoTruncation | nodebuilder.FlagsSuppressAnyReturnType | nodebuilder.FlagsAllowEmptyTuple
	if quotePreference == lsutil.QuotePreferenceSingle {
		flags |= nodebuilder.FlagsUseSingleQuotesForStringLiteralType
	}
	nodeBuilder, idToSymbol := f.createNodeBuilder()
	signatureDeclaration := nodeBuilder.SignatureToSignatureDeclaration(signature, kind, enclosingDeclaration, flags, nodebuilder.InternalFlagsAllowUnresolvedNames, nil)
	if signatureDeclaration.IsNil() {
		return ast.Handle{}
	}
	isJS := ast.IsInJSFile(enclosingDeclaration)
	parameters := signatureDeclaration.ParameterList()
	typeParameters := core.IfElse(isJS, 0, signatureDeclaration.TypeParameterList())
	typeNode := core.IfElse(isJS, ast.Handle{}, signatureDeclaration.Type())
	if typeParameters != 0 && enclosingDeclaration.Store().ListLen(typeParameters) > 0 {
		nodes := make([]ast.Handle, 0, enclosingDeclaration.Store().ListLen(typeParameters))
		for _, tp := range enclosingDeclaration.Store().ListSlice(typeParameters) {
			if tp.IsNil() {
				continue
			}
			if ast.IsTypeParameterDeclaration(tp) {
				typeParameter := tp
				constraint := typeParameter.Constraint()
				if !constraint.IsNil() {
					constraint = f.importTypeNode(constraint, idToSymbol)
				}
				defaultType := typeParameter.DefaultType()
				if !defaultType.IsNil() {
					defaultType = f.importTypeNode(defaultType, idToSymbol)
				}
				nodes = append(nodes, f.changeTracker.HandleFactory.UpdateTypeParameterDeclaration(typeParameter, typeParameter.Modifiers(), typeParameter.Name(), constraint, typeParameter.Expression(), defaultType))
			} else {
				nodes = append(nodes, tp)
			}
		}
		typeParameters = f.changeTracker.HandleFactory.NewList(nodes)
	}
	if parameters != 0 {
		nodes := make([]ast.Handle, 0, enclosingDeclaration.Store().ListLen(parameters))
		for _, p := range enclosingDeclaration.Store().ListSlice(parameters) {
			if p.IsNil() {
				continue
			}
			parameter := p
			parameterTypeNode := parameter.Type()
			if !parameterTypeNode.IsNil() {
				parameterTypeNode = f.importTypeNode(parameterTypeNode, idToSymbol)
			}
			nodes = append(nodes, f.changeTracker.HandleFactory.UpdateParameterDeclaration(parameter, parameter.Modifiers(), parameter.DotDotDotToken(), parameter.Name(), core.IfElse(isJS, ast.Handle{}, parameter.QuestionToken()), parameterTypeNode, parameter.Initializer()))
		}
		parameters = f.changeTracker.HandleFactory.NewList(nodes)
	}
	if !typeNode.IsNil() {
		typeNode = f.importTypeNode(typeNode, idToSymbol)
	}
	var questionToken ast.Handle
	if optional {
		questionToken = f.changeTracker.HandleFactory.NewToken(ast.KindQuestionToken)
	}
	switch kind {
	case ast.KindFunctionExpression:
		fn := signatureDeclaration
		return f.changeTracker.HandleFactory.UpdateFunctionExpression(fn, modifiers, fn.AsteriskToken(), core.IfElse(!name.IsNil() && ast.IsIdentifier(name), name, ast.Handle{}), typeParameters, parameters, typeNode, fn.FullSignature(), core.OrElse(body, fn.Body()))
	case ast.KindArrowFunction:
		fn := signatureDeclaration
		return f.changeTracker.HandleFactory.UpdateArrowFunction(fn, modifiers, typeParameters, parameters, typeNode, fn.FullSignature(), fn.EqualsGreaterThanToken(), core.OrElse(body, fn.Body()))
	case ast.KindMethodDeclaration:
		method := signatureDeclaration
		methodName := core.IfElse(name.IsNil(), f.changeTracker.HandleFactory.NewIdentifier(""), createPropertyName(f.changeTracker.HandleFactory, name, quotePreference))
		return f.changeTracker.HandleFactory.UpdateMethodDeclaration(method, modifiers, method.AsteriskToken(), methodName, questionToken, typeParameters, parameters, typeNode, method.FullSignature(), body)
	case ast.KindFunctionDeclaration:
		fn := signatureDeclaration
		return f.changeTracker.HandleFactory.UpdateFunctionDeclaration(fn, modifiers, fn.AsteriskToken(), core.IfElse(!name.IsNil() && ast.IsIdentifier(name), name, ast.Handle{}), typeParameters, parameters, typeNode, fn.FullSignature(), core.OrElse(body, fn.Body()))
	}
	return ast.Handle{}
}
func (f *missingMemberFixer) createSignatureDeclarationFromSignatures(signatures []*checker.Signature, name ast.Handle, optional bool, modifiers ast.ListRef, quotePreference lsutil.QuotePreference, body ast.Handle, enclosingDeclaration ast.Handle) ast.Handle {
	if len(signatures) == 0 {
		return ast.Handle{}
	}
	nodeBuilder, idToSymbol := f.createNodeBuilder()
	maxArgsSignature := signatures[0]
	minArgumentCount := signatures[0].MinArgumentCount()
	hasRestParameter := false
	for _, signature := range signatures {
		minArgumentCount = min(minArgumentCount, signature.MinArgumentCount())
		if signature.HasRestParameter() {
			hasRestParameter = true
		}
		if len(signature.Parameters()) >= len(maxArgsSignature.Parameters()) && (!signature.HasRestParameter() || maxArgsSignature.HasRestParameter()) {
			maxArgsSignature = signature
		}
	}
	maxNonRestArgs := len(maxArgsSignature.Parameters()) - core.IfElse(maxArgsSignature.HasRestParameter(), 1, 0)
	parameterNames := make([]string, 0, len(maxArgsSignature.Parameters()))
	for _, symbol := range maxArgsSignature.Parameters() {
		parameterNames = append(parameterNames, symbol.Name)
	}
	parameters := createDummyParameters(f.changeTracker.HandleFactory, maxNonRestArgs, parameterNames, nil, minArgumentCount, ast.IsInJSFile(enclosingDeclaration))
	if hasRestParameter {
		restParameterName := "rest"
		if maxNonRestArgs < len(parameterNames) && parameterNames[maxNonRestArgs] != "" {
			restParameterName = parameterNames[maxNonRestArgs]
		}
		var questionToken ast.Handle
		if maxNonRestArgs >= minArgumentCount {
			questionToken = f.changeTracker.HandleFactory.NewToken(ast.KindQuestionToken)
		}
		rest := f.changeTracker.HandleFactory.NewParameterDeclaration(0, f.changeTracker.HandleFactory.NewToken(ast.KindDotDotDotToken), f.changeTracker.HandleFactory.NewIdentifier(restParameterName), questionToken, f.changeTracker.HandleFactory.NewArrayTypeNode(f.changeTracker.HandleFactory.NewKeywordTypeNode(ast.KindUnknownKeyword)), ast.Handle{})
		parameters = f.changeTracker.HandleFactory.NewList(append(enclosingDeclaration.Store().ListSlice(parameters).Slice(), rest))
	}
	methodName := core.IfElse(name.IsNil(), f.changeTracker.HandleFactory.NewIdentifier(""), createPropertyName(f.changeTracker.HandleFactory, name, quotePreference))
	return f.changeTracker.HandleFactory.NewMethodDeclaration(modifiers, ast.Handle{}, methodName, core.IfElse(optional, f.changeTracker.HandleFactory.NewToken(ast.KindQuestionToken), ast.Handle{}), 0, parameters, f.getReturnTypeFromSignatures(signatures, enclosingDeclaration, nodeBuilder, idToSymbol), ast.Handle{}, f.createBody(body, quotePreference, false))
}
func (f *missingMemberFixer) getReturnTypeFromSignatures(signatures []*checker.Signature, enclosingDeclaration ast.Handle, nodeBuilder *checker.NodeBuilder, idToSymbol map[ast.Handle]*ast.Symbol) ast.Handle {
	if len(signatures) == 0 {
		return ast.Handle{}
	}
	returnTypes := make([]*checker.Type, 0, len(signatures))
	for _, signature := range signatures {
		returnTypes = append(returnTypes, f.typeChecker.GetReturnTypeOfSignature(signature))
	}
	unionType := f.typeChecker.GetUnionType(returnTypes)
	return f.importTypeNode(nodeBuilder.TypeToTypeNode(unionType, enclosingDeclaration, nodebuilder.FlagsNoTruncation, nodebuilder.InternalFlagsAllowUnresolvedNames, nil), idToSymbol)
}
func (f *missingMemberFixer) importTypeNode(typeNode ast.Handle, idToSymbol map[ast.Handle]*ast.Symbol) ast.Handle {
	if typeNode.IsNil() || f.importAdder == nil {
		return typeNode
	}
	importedTypeNode, symbols := autoimport.TryGetAutoImportableReferenceFromTypeNode(typeNode, idToSymbol)
	if !importedTypeNode.IsNil() {
		for _, symbol := range symbols {
			exportSymbol := f.getExportedSymbol(symbol)
			if exportSymbol == nil {
				continue
			}
			f.importAdder.AddImportFromExportedSymbol(exportSymbol, true)
		}
		return importedTypeNode
	}
	seen := make(map[*ast.Symbol]bool)
	for _, symbol := range idToSymbol {
		if symbol == nil || seen[symbol] {
			continue
		}
		seen[symbol] = true
		exportSymbol := f.getExportedSymbol(symbol)
		if exportSymbol == nil {
			continue
		}
		f.importAdder.AddImportFromExportedSymbol(exportSymbol, true)
	}
	return typeNode
}
func (f *missingMemberFixer) getExportedSymbol(symbol *ast.Symbol) *ast.Symbol {
	symbol = f.typeChecker.GetExportSymbolOfSymbol(symbol)
	if symbol == nil || symbol.Parent == nil {
		return nil
	}
	return symbol
}
func (f *missingMemberFixer) createIndexSignatureDeclarationFromType(classDeclaration ast.Handle, implementedType *checker.Type, keyType *checker.Type) ast.Handle {
	indexInfo := f.typeChecker.GetIndexInfoOfType(implementedType, keyType)
	if indexInfo == nil {
		return ast.Handle{}
	}
	builder := checker.NewNodeBuilder(f.typeChecker, f.changeTracker.EmitContext)
	return builder.IndexInfoToIndexSignatureDeclaration(indexInfo, classDeclaration, nodebuilder.FlagsNone, nodebuilder.InternalFlagsNone, nil)
}
func (f *missingMemberFixer) createBody(body ast.Handle, quotePreference lsutil.QuotePreference, signatureOnly bool) ast.Handle {
	if signatureOnly {
		return ast.Handle{}
	}
	body = f.changeTracker.HandleFactory.DeepCloneNode(body)
	if body.IsNil() {
		return f.createStubbedMethodBody(quotePreference)
	}
	return body
}
func (f *missingMemberFixer) createStubbedMethodBody(quotePreference lsutil.QuotePreference) ast.Handle {
	tokenFlags := ast.TokenFlagsNone
	if quotePreference == lsutil.QuotePreferenceSingle {
		tokenFlags = ast.TokenFlagsSingleQuote
	}
	return f.changeTracker.HandleFactory.NewBlock(f.changeTracker.HandleFactory.NewList([]ast.Handle{f.changeTracker.HandleFactory.NewThrowStatement(f.changeTracker.HandleFactory.NewNewExpression(f.changeTracker.HandleFactory.NewIdentifier("Error"), 0, f.changeTracker.HandleFactory.NewList([]ast.Handle{f.changeTracker.HandleFactory.NewStringLiteral(diagnostics.Method_not_implemented.Localize(f.locale), tokenFlags)})))}), true)
}
func createDummyParameters(factory ast.HandleFactory, argCount int, names []string, types []ast.Handle, minArgumentCount int, inJS bool) ast.ListRef {
	parameters := make([]ast.Handle, 0, argCount)
	parameterNameCounts := make(map[string]int)
	for i := range argCount {
		parameterName := ""
		if i < len(names) && names[i] != "" {
			parameterName = names[i]
		} else {
			parameterName = "arg" + strconv.Itoa(i)
		}
		count := parameterNameCounts[parameterName]
		parameterNameCounts[parameterName] = count + 1
		if count > 0 {
			parameterName += strconv.Itoa(count)
		}
		var questionToken ast.Handle
		if i >= minArgumentCount {
			questionToken = factory.NewToken(ast.KindQuestionToken)
		}
		var typeNode ast.Handle
		if inJS {
			typeNode = ast.Handle{}
		} else if i < len(types) && !types[i].IsNil() {
			typeNode = types[i]
		} else {
			typeNode = factory.NewKeywordTypeNode(ast.KindUnknownKeyword)
		}
		parameters = append(parameters, factory.NewParameterDeclaration(0, ast.Handle{}, factory.NewIdentifier(parameterName), questionToken, typeNode, ast.Handle{}))
	}
	return factory.NewList(parameters)
}
func createDeclarationName(factory ast.HandleFactory, typeChecker *checker.Checker, symbol *ast.Symbol, declaration ast.Handle) ast.Handle {
	if symbol != nil && symbol.CheckFlags&ast.CheckFlagsMapped != 0 {
		nameType := typeChecker.GetNameTypeOfSymbol(symbol)
		if nameType != nil && checker.IsTypeUsableAsPropertyName(nameType) {
			return factory.NewIdentifier(checker.GetPropertyNameFromType(nameType))
		}
	}
	if !declaration.IsNil() && !declaration.Name().IsNil() {
		return factory.DeepCloneNode(declaration.Name())
	}
	if symbol != nil {
		return factory.NewIdentifier(symbol.Name)
	}
	return ast.Handle{}
}
func createPropertyName(factory ast.HandleFactory, node ast.Handle, quotePreference lsutil.QuotePreference) ast.Handle {
	if ast.IsIdentifier(node) && node.Text() == "constructor" {
		tokenFlags := ast.TokenFlagsNone
		if quotePreference == lsutil.QuotePreferenceSingle {
			tokenFlags = ast.TokenFlagsSingleQuote
		}
		return factory.NewComputedPropertyName(factory.NewStringLiteral(node.Text(), tokenFlags))
	}
	return factory.DeepCloneNode(node)
}
