package ls

import (
	"cmp"
	"context"
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"slices"
)

var tokenTypes = []lsproto.SemanticTokenType{lsproto.SemanticTokenTypeNamespace, lsproto.SemanticTokenTypeClass, lsproto.SemanticTokenTypeEnum, lsproto.SemanticTokenTypeInterface, lsproto.SemanticTokenTypeStruct, lsproto.SemanticTokenTypeTypeParameter, lsproto.SemanticTokenTypeType, lsproto.SemanticTokenTypeParameter, lsproto.SemanticTokenTypeVariable, lsproto.SemanticTokenTypeProperty, lsproto.SemanticTokenTypeEnumMember, lsproto.SemanticTokenTypeDecorator, lsproto.SemanticTokenTypeEvent, lsproto.SemanticTokenTypeFunction, lsproto.SemanticTokenTypeMethod, lsproto.SemanticTokenTypeMacro, lsproto.SemanticTokenTypeLabel, lsproto.SemanticTokenTypeComment, lsproto.SemanticTokenTypeString, lsproto.SemanticTokenTypeKeyword, lsproto.SemanticTokenTypeNumber, lsproto.SemanticTokenTypeRegexp, lsproto.SemanticTokenTypeOperator}

var tokenModifiers = []lsproto.SemanticTokenModifier{lsproto.SemanticTokenModifierDeclaration, lsproto.SemanticTokenModifierDefinition, lsproto.SemanticTokenModifierReadonly, lsproto.SemanticTokenModifierStatic, lsproto.SemanticTokenModifierDeprecated, lsproto.SemanticTokenModifierAbstract, lsproto.SemanticTokenModifierAsync, lsproto.SemanticTokenModifierModification, lsproto.SemanticTokenModifierDocumentation, lsproto.SemanticTokenModifierDefaultLibrary, "local"}

type tokenType int

const (
	tokenTypeNamespace tokenType = iota
	tokenTypeClass
	tokenTypeEnum
	tokenTypeInterface
	tokenTypeStruct
	tokenTypeTypeParameter
	tokenTypeType
	tokenTypeParameter
	tokenTypeVariable
	tokenTypeProperty
	tokenTypeEnumMember
	tokenTypeDecorator
	tokenTypeEvent
	tokenTypeFunction
	tokenTypeMethod
	tokenTypeMacro
	tokenTypeLabel
	tokenTypeComment
	tokenTypeString
	tokenTypeKeyword
	tokenTypeNumber
	tokenTypeRegexp
	tokenTypeOperator
)

type tokenModifier int

const (
	tokenModifierDeclaration tokenModifier = 1 << iota
	tokenModifierDefinition
	tokenModifierReadonly
	tokenModifierStatic
	tokenModifierDeprecated
	tokenModifierAbstract
	tokenModifierAsync
	tokenModifierModification
	tokenModifierDocumentation
	tokenModifierDefaultLibrary
	tokenModifierLocal
)

func SemanticTokensLegend(clientCapabilities lsproto.ResolvedSemanticTokensClientCapabilities) *lsproto.SemanticTokensLegend {
	types := make([]string, 0, len(tokenTypes))
	for _, t := range tokenTypes {
		if slices.Contains(clientCapabilities.TokenTypes, string(t)) {
			types = append(types, string(t))
		}
	}
	modifiers := make([]string, 0, len(tokenModifiers))
	for _, m := range tokenModifiers {
		if slices.Contains(clientCapabilities.TokenModifiers, string(m)) {
			modifiers = append(modifiers, string(m))
		}
	}
	return &lsproto.SemanticTokensLegend{TokenTypes: types, TokenModifiers: modifiers}
}
func (l *LanguageService) ProvideSemanticTokens(ctx context.Context, documentURI lsproto.DocumentUri) (lsproto.SemanticTokensResponse, error) {
	program, file := l.getProgramAndFile(documentURI)
	supplemental := file.SupplementalSourceFiles()
	files := make([]*ast.SourceFile, 1, 1+len(supplemental))
	files[0] = file
	files = append(files, supplemental...)
	tokens := make([]semanticToken, 0, len(files))
	for _, projection := range files {
		c, done := program.GetTypeCheckerForFile(ctx, projection)
		for _, token := range l.collectSemanticTokens(ctx, c, projection, program) {
			token.file = projection
			tokens = append(tokens, token)
		}
		done()
	}
	sortSemanticTokens(tokens, l.converters)
	if len(tokens) == 0 {
		return lsproto.SemanticTokensOrNull{}, nil
	}
	encoded := encodeSemanticTokens(ctx, tokens, l.converters)
	return lsproto.SemanticTokensOrNull{SemanticTokens: &lsproto.SemanticTokens{Data: encoded}}, nil
}
func (l *LanguageService) ProvideSemanticTokensRange(ctx context.Context, documentURI lsproto.DocumentUri, rng lsproto.Range) (lsproto.SemanticTokensRangeResponse, error) {
	program, file := l.getProgramAndFile(documentURI)
	mappedRanges := lsconv.FromLSPRangeIntersectingForSourceFile(l.converters, file, rng, spanmap.FeatureSemanticTokens)
	tokens := make([]semanticToken, 0, len(mappedRanges))
	var seen collections.Set[semanticToken]
	for _, mapped := range mappedRanges {
		projection := mapped.Script
		c, done := program.GetTypeCheckerForFile(ctx, projection)
		for _, token := range l.collectSemanticTokensInRange(ctx, c, projection, program, mapped.Span.Pos(), mapped.Span.End()) {
			token.file = projection
			if seen.AddIfAbsent(token) {
				tokens = append(tokens, token)
			}
		}
		done()
	}
	sortSemanticTokens(tokens, l.converters)
	if len(tokens) == 0 {
		return lsproto.SemanticTokensOrNull{}, nil
	}
	encoded := encodeSemanticTokens(ctx, tokens, l.converters)
	return lsproto.SemanticTokensOrNull{SemanticTokens: &lsproto.SemanticTokens{Data: encoded}}, nil
}
func sortSemanticTokens(tokens []semanticToken, converters *lsconv.Converters) {
	slices.SortFunc(tokens, func(a, b semanticToken) int {
		aRange, _ := semanticTokenLSPRange(a, converters)
		bRange, _ := semanticTokenLSPRange(b, converters)
		if result := cmp.Compare(aRange.Start.Line, bRange.Start.Line); result != 0 {
			return result
		}
		if result := cmp.Compare(aRange.Start.Character, bRange.Start.Character); result != 0 {
			return result
		}
		if result := cmp.Compare(a.file.Path(), b.file.Path()); result != 0 {
			return result
		}
		return cmp.Compare(a.node.Pos(), b.node.Pos())
	})
}
func semanticTokenLSPRange(token semanticToken, converters *lsconv.Converters) (lsproto.Range, spanmap.Fidelity) {
	start := scanner.GetTokenPosOfNode(token.node, token.file, false)
	return converters.ToLSPRangeForFeature(token.file, core.NewTextRange(start, token.node.End()), spanmap.FeatureSemanticTokens)
}

type semanticToken struct {
	node          ast.Handle
	file          *ast.SourceFile
	tokenType     tokenType
	tokenModifier tokenModifier
}

func (l *LanguageService) collectSemanticTokens(ctx context.Context, c *checker.Checker, file *ast.SourceFile, program *compiler.Program) []semanticToken {
	return l.collectSemanticTokensInRange(ctx, c, file, program, file.Pos(), file.End())
}
func (l *LanguageService) collectSemanticTokensInRange(ctx context.Context, c *checker.Checker, file *ast.SourceFile, program *compiler.Program, spanStart, spanEnd int) []semanticToken {
	tokens := []semanticToken{}
	inJSXElement := false
	var visit func(ast.Handle) bool
	visit = func(node ast.Handle) bool {
		if ctx.Err() != nil {
			return false
		}
		if node.IsNil() {
			return false
		}
		if node.Flags()&ast.NodeFlagsReparsed != 0 {
			return false
		}
		nodeEnd := node.End()
		if node.Pos() >= spanEnd || nodeEnd <= spanStart {
			return false
		}
		prevInJSXElement := inJSXElement
		if ast.IsJsxElement(node) || ast.IsJsxSelfClosingElement(node) {
			inJSXElement = true
		} else if ast.IsJsxExpression(node) {
			inJSXElement = false
		}
		if ast.IsIdentifier(node) && node.Text() != "" && !inJSXElement && !isInImportClause(node) && !isInfinityOrNaNString(node.Text()) {
			symbol := c.GetSymbolAtLocation(node)
			if symbol != nil {
				if symbol.Flags&ast.SymbolFlagsAlias != 0 {
					symbol = c.GetAliasedSymbol(symbol)
				}
				tokenType, ok := classifySymbol(symbol, getMeaningFromLocation(node))
				if ok {
					tokenModifier := tokenModifier(0)
					parent := node.Parent()
					if !parent.IsNil() {
						parentIsDeclaration := ast.IsBindingElement(parent) || tokenFromDeclarationMapping(parent.Kind) == tokenType
						if parentIsDeclaration && parent.Name() == node {
							tokenModifier |= tokenModifierDeclaration
						}
					}
					if tokenType == tokenTypeParameter && ast.IsRightSideOfQualifiedNameOrPropertyAccess(node) {
						tokenType = tokenTypeProperty
					}
					tokenType = reclassifyByType(c, node, tokenType)
					if decl := ast.NodeOf(symbol.ValueDeclaration); !decl.IsNil() {
						modifiers := ast.GetCombinedModifierFlags(decl)
						nodeFlags := ast.GetCombinedNodeFlags(decl)
						if modifiers&ast.ModifierFlagsStatic != 0 {
							tokenModifier |= tokenModifierStatic
						}
						if modifiers&ast.ModifierFlagsAsync != 0 {
							tokenModifier |= tokenModifierAsync
						}
						if tokenType != tokenTypeClass && tokenType != tokenTypeInterface {
							if (modifiers&ast.ModifierFlagsReadonly != 0) || (nodeFlags&ast.NodeFlagsConst != 0) || (symbol.Flags&ast.SymbolFlagsEnumMember != 0) {
								tokenModifier |= tokenModifierReadonly
							}
						}
						if (tokenType == tokenTypeVariable || tokenType == tokenTypeFunction) && isLocalDeclaration(decl, file) {
							tokenModifier |= tokenModifierLocal
						}
						declSourceFile := ast.GetSourceFileOfNode(decl)
						if declSourceFile != nil && program.IsSourceFileDefaultLibrary(declSourceFile.Path()) {
							tokenModifier |= tokenModifierDefaultLibrary
						}
					} else if symbol.Declarations != nil {
						for _, decl := range ast.DeclarationNodes(symbol).All() {
							declSourceFile := ast.GetSourceFileOfNode(decl)
							if declSourceFile != nil && program.IsSourceFileDefaultLibrary(declSourceFile.Path()) {
								tokenModifier |= tokenModifierDefaultLibrary
								break
							}
						}
					}
					tokens = append(tokens, semanticToken{node: node, tokenType: tokenType, tokenModifier: tokenModifier})
				}
			}
		}
		node.ForEachChild(visit)
		inJSXElement = prevInJSXElement
		return false
	}
	visit(file.ParseRoot())
	if ctx.Err() != nil {
		return nil
	}
	return tokens
}
func classifySymbol(symbol *ast.Symbol, meaning ast.SemanticMeaning) (tokenType, bool) {
	flags := symbol.Flags
	if flags&ast.SymbolFlagsClass != 0 {
		return tokenTypeClass, true
	}
	if flags&ast.SymbolFlagsEnum != 0 {
		return tokenTypeEnum, true
	}
	if flags&ast.SymbolFlagsTypeAlias != 0 {
		return tokenTypeType, true
	}
	if flags&ast.SymbolFlagsInterface != 0 {
		if meaning&ast.SemanticMeaningType != 0 {
			return tokenTypeInterface, true
		}
	}
	if flags&ast.SymbolFlagsTypeParameter != 0 {
		return tokenTypeTypeParameter, true
	}
	decl := ast.NodeOf(symbol.ValueDeclaration)
	if decl.IsNil() && len(symbol.Declarations) > 0 {
		decl = ast.NodeOf(symbol.Declarations[0])
	}
	if !decl.IsNil() {
		if ast.IsBindingElement(decl) {
			decl = getDeclarationForBindingElement(decl)
		}
		if tokenType := tokenFromDeclarationMapping(decl.Kind); tokenType >= 0 {
			return tokenType, true
		}
	}
	return 0, false
}
func tokenFromDeclarationMapping(kind ast.Kind) tokenType {
	switch kind {
	case ast.KindVariableDeclaration:
		return tokenTypeVariable
	case ast.KindParameter:
		return tokenTypeParameter
	case ast.KindPropertyDeclaration:
		return tokenTypeProperty
	case ast.KindModuleDeclaration:
		return tokenTypeNamespace
	case ast.KindEnumDeclaration:
		return tokenTypeEnum
	case ast.KindEnumMember:
		return tokenTypeEnumMember
	case ast.KindClassDeclaration, ast.KindClassExpression:
		return tokenTypeClass
	case ast.KindMethodDeclaration:
		return tokenTypeMethod
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression:
		return tokenTypeFunction
	case ast.KindMethodSignature:
		return tokenTypeMethod
	case ast.KindGetAccessor, ast.KindSetAccessor:
		return tokenTypeProperty
	case ast.KindPropertySignature:
		return tokenTypeProperty
	case ast.KindInterfaceDeclaration:
		return tokenTypeInterface
	case ast.KindTypeAliasDeclaration:
		return tokenTypeType
	case ast.KindTypeParameter:
		return tokenTypeTypeParameter
	case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment:
		return tokenTypeProperty
	default:
		return -1
	}
}
func reclassifyByType(c *checker.Checker, node ast.Handle, tt tokenType) tokenType {
	if tt == tokenTypeVariable || tt == tokenTypeProperty || tt == tokenTypeParameter {
		typ := c.GetTypeAtLocation(node)
		if typ != nil {
			test := func(condition func(*checker.Type) bool) bool {
				if condition(typ) {
					return true
				}
				if typ.Flags()&checker.TypeFlagsUnion != 0 {
					if slices.ContainsFunc(typ.AsUnionType().Types(), condition) {
						return true
					}
				}
				return false
			}
			if tt != tokenTypeParameter && test(func(t *checker.Type) bool {
				return len(c.GetSignaturesOfType(t, checker.SignatureKindConstruct)) > 0
			}) {
				return tokenTypeClass
			}
			hasCallSignatures := test(func(t *checker.Type) bool {
				return len(c.GetSignaturesOfType(t, checker.SignatureKindCall)) > 0
			})
			if hasCallSignatures {
				hasNoProperties := !test(func(t *checker.Type) bool {
					objType := t.AsObjectType()
					return objType != nil && len(objType.Properties()) > 0
				})
				if hasNoProperties || isExpressionInCallExpression(node) {
					if tt == tokenTypeProperty {
						return tokenTypeMethod
					}
					return tokenTypeFunction
				}
			}
		}
	}
	return tt
}
func isLocalDeclaration(decl ast.Handle, sourceFile *ast.SourceFile) bool {
	if ast.IsBindingElement(decl) {
		decl = getDeclarationForBindingElement(decl)
	}
	if ast.IsVariableDeclaration(decl) {
		parent := decl.Parent()
		if !parent.IsNil() && ast.IsCatchClause(parent) {
			return ast.GetSourceFileOfNode(decl) == sourceFile
		}
		if !parent.IsNil() && ast.IsVariableDeclarationList(parent) {
			grandparent := parent.Parent()
			if !grandparent.IsNil() {
				greatGrandparent := grandparent.Parent()
				return (!ast.IsSourceFile(greatGrandparent) || ast.IsCatchClause(grandparent)) && ast.GetSourceFileOfNode(decl) == sourceFile
			}
		}
	} else if ast.IsFunctionDeclaration(decl) {
		parent := decl.Parent()
		return !parent.IsNil() && !ast.IsSourceFile(parent) && ast.GetSourceFileOfNode(decl) == sourceFile
	}
	return false
}
func getDeclarationForBindingElement(element ast.Handle) ast.Handle {
	for {
		parent := element.Parent()
		if !parent.IsNil() && ast.IsBindingPattern(parent) {
			grandparent := parent.Parent()
			if !grandparent.IsNil() && ast.IsBindingElement(grandparent) {
				element = grandparent
				continue
			}
			return parent.Parent()
		}
		return element
	}
}
func isInImportClause(node ast.Handle) bool {
	parent := node.Parent()
	return !parent.IsNil() && (ast.IsImportClause(parent) || ast.IsImportSpecifier(parent) || ast.IsNamespaceImport(parent))
}
func isExpressionInCallExpression(node ast.Handle) bool {
	for ast.IsRightSideOfQualifiedNameOrPropertyAccess(node) {
		node = node.Parent()
	}
	parent := node.Parent()
	return !parent.IsNil() && ast.IsCallExpression(parent) && parent.Expression() == node
}
func isInfinityOrNaNString(text string) bool {
	return text == "Infinity" || text == "NaN"
}

func encodeSemanticTokens(ctx context.Context, tokens []semanticToken, converters *lsconv.Converters) []uint32 {
	typeMapping := make(map[tokenType]uint32)
	modifierMapping := make(map[lsproto.SemanticTokenModifier]uint32)
	clientCapabilities := lsproto.GetClientCapabilities(ctx).TextDocument.SemanticTokens
	clientIdx := uint32(0)
	for i, serverType := range tokenTypes {
		if slices.Contains(clientCapabilities.TokenTypes, string(serverType)) {
			typeMapping[tokenType(i)] = clientIdx
			clientIdx++
		}
	}
	clientBit := uint32(0)
	for _, serverModifier := range tokenModifiers {
		if slices.Contains(clientCapabilities.TokenModifiers, string(serverModifier)) {
			modifierMapping[serverModifier] = clientBit
			clientBit++
		}
	}
	encoded := make([]uint32, 0, len(tokens)*5)
	prevLine := uint32(0)
	prevChar := uint32(0)
	for _, token := range tokens {
		clientTypeIdx, typeSupported := typeMapping[token.tokenType]
		if !typeSupported {
			continue
		}
		clientModifierMask := uint32(0)
		for i, serverModifier := range tokenModifiers {
			if token.tokenModifier&(1<<i) != 0 {
				if clientBit, ok := modifierMapping[serverModifier]; ok {
					clientModifierMask |= 1 << clientBit
				}
			}
		}
		lspRange, fidelity := semanticTokenLSPRange(token, converters)
		if !fidelity.IsExact() {
			continue
		}
		startPos := lspRange.Start
		endPos := lspRange.End
		var tokenLength uint32
		if startPos.Line == endPos.Line {
			tokenLength = endPos.Character - startPos.Character
		} else {
			panic(fmt.Sprintf("semantic tokens: token spans multiple lines: start=(%d,%d) end=(%d,%d) for token at offset %d", startPos.Line, startPos.Character, endPos.Line, endPos.Character, token.node.Pos()))
		}
		line := startPos.Line
		char := startPos.Character
		if len(encoded) > 0 && line == prevLine && char == prevChar {
			continue
		}
		if len(encoded) > 0 && (line < prevLine || line == prevLine && char < prevChar) {
			panic(fmt.Sprintf("semantic tokens: positions must be strictly increasing: prev=(%d,%d) current=(%d,%d) for token at offset %d", prevLine, prevChar, line, char, token.node.Pos()))
		}
		deltaLine := line - prevLine
		var deltaChar uint32
		if deltaLine == 0 {
			deltaChar = char - prevChar
		} else {
			deltaChar = char
		}
		encoded = append(encoded, deltaLine, deltaChar, tokenLength, clientTypeIdx, clientModifierMask)
		prevLine = line
		prevChar = char
	}
	return encoded
}
