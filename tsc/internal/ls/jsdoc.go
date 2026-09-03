package ls

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"slices"
	"strings"
)

type JSDocTagInfo struct {
	Name string
	Text string
}

func GetSymbolDocumentationComment(c *checker.Checker, symbol *ast.Symbol) string {
	if symbol == nil {
		return ""
	}
	var parts []string// JSDocTagInfo mirrors Strada's `JSDocTagInfo`, but renders the tag's text as a
	// plain string instead of `SymbolDisplayPart[]`.
	// GetSymbolDocumentationComment renders a symbol's documentation comment as plain text.
	// It backs the API's Symbol.getDocumentationComment and mirrors Strada's
	// getJsDocCommentsFromDeclarations: comments are gathered from each unique declaration,
	// deduplicated, and joined with line breaks. Like Strada, it does not resolve aliases —
	// consumers resolve aliases themselves (via getAliasedSymbol) and re-query if desired.
	/*commentOnly*/ // GetSymbolJSDocTags collects a symbol's JSDoc tags. It backs the API's Symbol.getJsDocTags
	// and mirrors Strada's getJsDocTagsFromDeclarations, except each tag's text is rendered as a
	// plain string rather than SymbolDisplayPart[]. Tags with no text have an empty Text field.
	// Skip comments containing @typedef/@callback since they're not associated with a
	// particular declaration, unless they also carry @param/@return (treated as local docs).
	// declarationJSDocTags returns the JSDoc tags associated with a declaration, walking the
	// JSDoc comment location chain like the checker's getAllJSDocTags.
	// getJSDocTagText renders the text of a single JSDoc tag as a plain string, mirroring
	// Strada's getCommentDisplayParts collapsed from SymbolDisplayPart[] to a string.
	// For binding patterns, match JSDoc @param tags by position rather than by name
	// For static members, use the checker's base constructor type resolution.
	// This correctly handles intersection constructor types from mixins
	// (e.g., typeof MixinClass & T) by preserving the full intersection.
	// getJSDocParameterTagByPosition finds a JSDoc @param tag for a binding pattern parameter by position.
	// Since binding patterns don't have a simple name, we match the @param tag at the same index as the parameter.
	// Find the parameter's index in the parent's parameters list
	// Get the JSDoc for the parent function/method
	// Collect all @param tags in order

	var seen collections.Set[ast.Handle]
	for _, decl := range ast.DeclarationNodes(symbol) {
		if decl.IsNil() {
			continue
		}
		if !seen.AddIfAbsent(decl) {
			continue
		}
		if doc := getDocumentationFromDeclaration(noMappedLocation, c, symbol, decl, decl, lsproto.MarkupKindPlainText, true); doc != "" && !slices.Contains(parts, doc) {
			parts = append(parts, doc)
		}
	}
	return strings.Join(parts, "\n")
}

func GetSymbolJSDocTags(symbol *ast.Symbol) []JSDocTagInfo {
	if symbol == nil {
		return nil
	}
	var infos []JSDocTagInfo
	var seen collections.Set[ast.Handle]
	for _, decl := range ast.DeclarationNodes(symbol) {
		if decl.IsNil() {
			continue
		}
		if !seen.AddIfAbsent(decl) {
			continue
		}
		tags := declarationJSDocTags(decl)
		hasTypedef := core.Some(tags, func(t ast.Handle) bool {
			return t.Kind == ast.KindJSDocTypedefTag || t.Kind == ast.KindJSDocCallbackTag
		})
		hasParamOrReturn := core.Some(tags, func(t ast.Handle) bool {
			return t.Kind == ast.KindJSDocParameterTag || t.Kind == ast.KindJSDocReturnTag
		})
		if hasTypedef && !hasParamOrReturn {
			continue
		}
		for _, tag := range tags {
			infos = append(infos, JSDocTagInfo{Name: tag.TagName().Text(), Text: getJSDocTagText(tag)})
		}
	}
	return infos
}

func declarationJSDocTags(node ast.Handle) []ast.Handle {
	if node.Flags()&ast.NodeFlagsJSDoc == 0 {
		for current := node; !current.IsNil(); current = ast.GetNextJSDocCommentLocation(current) {
			jsdocs := current.JSDoc(nil)
			if len(jsdocs) == 0 {
				continue
			}
			lastJSDoc := jsdocs[len(jsdocs)-1]
			if tags := lastJSDoc.Tags(); len(tags) > 0 {
				return tags
			}
		}
	}
	return nil
}

func getJSDocTagText(tag ast.Handle) string {
	comment := scanner.GetTextOfJSDocComment(tag.Store(), tag.CommentList())
	addComment := func(s string) string {
		if comment == "" {
			return s
		}
		return s + " " + comment
	}
	switch tag.Kind {
	case ast.KindJSDocThrowsTag:
		if te := tag.JSDocThrowsTagTypeExpression(); !te.IsNil() {
			return addComment(scanner.GetTextOfNode(te))
		}
		return comment
	case ast.KindJSDocImplementsTag:
		return addComment(scanner.GetTextOfNode(tag.JSDocImplementsTagClassName()))
	case ast.KindJSDocAugmentsTag:
		return addComment(scanner.GetTextOfNode(tag.JSDocAugmentsTagClassName()))
	case ast.KindJSDocTemplateTag:
		templateTag := tag
		var b strings.Builder
		if !templateTag.Constraint().IsNil() {
			b.WriteString(scanner.GetTextOfNode(templateTag.Constraint()))
		}
		if tps := templateTag.TypeParameters(); len(tps) > 0 {
			for i, tp := range tps {
				if i == 0 && b.Len() != 0 {
					b.WriteString(" ")
				}
				if i != 0 {
					b.WriteString(", ")
				}
				b.WriteString(scanner.GetTextOfNode(tp))
			}
		}
		if comment != "" {
			if b.Len() != 0 {
				b.WriteString(" ")
			}
			b.WriteString(comment)
		}
		return b.String()
	case ast.KindJSDocTypeTag:
		return addComment(scanner.GetTextOfNode(tag.JSDocTypeTagTypeExpression()))
	case ast.KindJSDocSatisfiesTag:
		return addComment(scanner.GetTextOfNode(tag.JSDocSatisfiesTagTypeExpression()))
	case ast.KindJSDocSeeTag:
		if ne := tag.JSDocSeeTagNameExpression(); !ne.IsNil() {
			return addComment(scanner.GetTextOfNode(ne))
		}
		return comment
	case ast.KindJSDocParameterTag, ast.KindJSDocPropertyTag:
		if name := tag.Name(); !name.IsNil() {
			return addComment(scanner.GetTextOfNode(name))
		}
		return comment
	default:
		return comment
	}
}
func getJSDoc(node ast.Handle) ast.Handle {
	return core.LastOrNil(node.JSDoc(nil))
}
func getJSDocOrTag(c *checker.Checker, node ast.Handle, seenSymbols *collections.Set[*ast.Symbol]) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	if jsdoc := getJSDoc(node); !jsdoc.IsNil() {
		return jsdoc
	}
	switch {
	case ast.IsParameterDeclaration(node):
		name := node.Name()
		if ast.IsBindingPattern(name) {
			return getJSDocParameterTagByPosition(c, node)
		}
		return getMatchingJSDocTag(c, node.Parent(), name.Text(), isMatchingParameterTag, seenSymbols)
	case ast.IsTypeParameterDeclaration(node):
		return getMatchingJSDocTag(c, node.Parent(), node.Name().Text(), isMatchingTemplateTag, seenSymbols)
	case ast.IsVariableDeclaration(node) && ast.IsVariableDeclarationList(node.Parent()) && core.FirstOrNil(node.Store().ListSlice(node.Parent().VariableDeclarationListDeclarations())) == node:
		return getJSDocOrTag(c, node.Parent().Parent(), seenSymbols)
	case (ast.IsFunctionExpressionOrArrowFunction(node) || ast.IsClassExpression(node)) && (ast.IsVariableDeclaration(node.Parent()) || ast.IsPropertyDeclaration(node.Parent()) || ast.IsPropertyAssignment(node.Parent())) && node.Parent().Initializer() == node:
		return getJSDocOrTag(c, node.Parent(), seenSymbols)
	case ast.IsBindingElement(node) && ast.IsObjectBindingPattern(node.Parent()):
		if name := node.PropertyNameOrName(); ast.IsIdentifier(name) {
			if objectType := c.GetTypeAtLocation(node.Parent()); objectType != nil {
				if prop := c.GetPropertyOfType(objectType, name.Text()); prop != nil {
					for _, d := range ast.DeclarationNodes(prop) {
						if jsdoc := getJSDoc(d); !jsdoc.IsNil() {
							return jsdoc
						}
					}
				}
			}
		}
	}
	if symbol := node.Symbol(); symbol != nil && !node.Parent().IsNil() {
		if ast.IsFunctionDeclaration(node) || ast.IsMethodDeclaration(node) || ast.IsMethodSignatureDeclaration(node) || ast.IsConstructorDeclaration(node) || ast.IsConstructSignatureDeclaration(node) {
			firstSignature := ast.FindSymbolDeclaration(symbol, ast.IsFunctionLike)
			if !firstSignature.IsNil() && node != firstSignature {
				if jsDoc := getJSDocOrTag(c, firstSignature, seenSymbols); !jsDoc.IsNil() {
					return jsDoc
				}
			}
		}
		if ast.IsClassOrInterfaceLike(node.Parent()) {
			isStatic := ast.HasStaticModifier(node)
			classType := c.GetDeclaredTypeOfSymbol(node.Parent().Symbol())
			if isStatic {
				staticBaseType := c.GetApparentType(c.GetBaseConstructorTypeOfClass(classType))
				if prop := c.GetPropertyOfType(staticBaseType, symbol.Name); prop != nil && prop.ValueDeclaration != 0 && seenSymbols.AddIfAbsent(prop) {
					if jsDoc := getJSDocOrTag(c, ast.NodeOf(prop.ValueDeclaration), seenSymbols); !jsDoc.IsNil() {
						return jsDoc
					}
				}
			} else {
				for _, baseType := range c.GetBaseTypes(classType) {
					if prop := c.GetPropertyOfType(baseType, symbol.Name); prop != nil && prop.ValueDeclaration != 0 && seenSymbols.AddIfAbsent(prop) {
						if jsDoc := getJSDocOrTag(c, ast.NodeOf(prop.ValueDeclaration), seenSymbols); !jsDoc.IsNil() {
							return jsDoc
						}
					}
				}
			}
		}
	}
	return ast.Handle{}
}
func getMatchingJSDocTag(c *checker.Checker, node ast.Handle, name string, match func(ast.Handle, string) bool, seenSymbols *collections.Set[*ast.Symbol]) ast.Handle {
	if jsdoc := getJSDocOrTag(c, node, seenSymbols); !jsdoc.IsNil() && jsdoc.Kind == ast.KindJSDoc {
		if tags := jsdoc.JSDocTags(); tags != 0 {
			for _, tag := range node.Store().ListSlice(tags) {
				if match(tag, name) {
					return tag
				}
			}
		}
	}
	return ast.Handle{}
}

func getJSDocParameterTagByPosition(c *checker.Checker, param ast.Handle) ast.Handle {
	parent := param.Parent()
	if parent.IsNil() {
		return ast.Handle{}
	}
	params := parent.Parameters()
	paramIndex := -1
	for i, p := range params {
		if p == param {
			paramIndex = i
			break
		}
	}
	if paramIndex < 0 {
		return ast.Handle{}
	}
	jsdoc := getJSDocOrTag(c, parent, &collections.Set[*ast.Symbol]{})
	if jsdoc.IsNil() || jsdoc.Kind != ast.KindJSDoc {
		return ast.Handle{}
	}
	tags := jsdoc.JSDocTags()
	if tags == 0 {
		return ast.Handle{}
	}
	paramTagIndex := 0
	for _, tag := range param.Store().ListSlice(tags) {
		if tag.Kind == ast.KindJSDocParameterTag {
			if paramTagIndex == paramIndex {
				return tag
			}
			paramTagIndex++
		}
	}
	return ast.Handle{}
}
func isMatchingParameterTag(tag ast.Handle, name string) bool {
	return tag.Kind == ast.KindJSDocParameterTag && isNodeWithName(tag, name)
}
func isMatchingTemplateTag(tag ast.Handle, name string) bool {
	return tag.Kind == ast.KindJSDocTemplateTag && core.Some(tag.TypeParameters(), func(tp ast.Handle) bool {
		return isNodeWithName(tp, name)
	})
}
func isNodeWithName(node ast.Handle, name string) bool {
	nodeName := node.Name()
	return ast.IsIdentifier(nodeName) && nodeName.Text() == name
}
func noMappedLocation(*ast.SourceFile, core.TextRange) (lsproto.Location, spanmap.Fidelity) {
	return lsproto.Location{}, spanmap.FidelityNone
}
