package lsutil

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"strings"
	"unicode"
)

func ProbablyUsesSemicolons(file *ast.SourceFile) bool {
	withSemicolon := 0
	withoutSemicolon := 0
	nStatementsToObserve := 5
	var visit ast.StoreVisitor
	visit = func(node ast.Handle) bool {
		if node.Flags()&ast.NodeFlagsReparsed != 0 {
			return false
		}
		if SyntaxRequiresTrailingSemicolonOrASI(node.Kind()) {
			lastToken := GetLastToken(node, file)
			if !lastToken.IsNil() && lastToken.Kind() == ast.KindSemicolonToken {
				withSemicolon++
			} else {
				withoutSemicolon++
			}
		} else if SyntaxRequiresTrailingCommaOrSemicolonOrASI(node.Kind()) {
			lastToken := GetLastToken(node, file)
			if !lastToken.IsNil() && lastToken.Kind() == ast.KindSemicolonToken {
				withSemicolon++
			} else if !lastToken.IsNil() && lastToken.Kind() != ast.KindCommaToken {
				lastTokenLine := scanner.GetECMALineOfPosition(file, astnav.GetStartOfNode(lastToken, file, false))
				nextTokenLine := scanner.GetECMALineOfPosition(file, scanner.SkipTrivia(file.Text(), lastToken.End()))
				if lastTokenLine != nextTokenLine {
					withoutSemicolon++
				}
			}
		}
		if withSemicolon+withoutSemicolon >= nStatementsToObserve {
			return true
		}
		return node.ForEachChild(visit)
	}
	file.ParseRoot().ForEachChild(visit)
	if withSemicolon == 0 && withoutSemicolon <= 1 {
		return true
	}
	if withoutSemicolon == 0 {
		return true
	}
	return withSemicolon*nStatementsToObserve > withoutSemicolon
}
func ShouldUseUriStyleNodeCoreModules(file *ast.SourceFile, program *compiler.Program) core.Tristate {
	for _, node := range file.Imports() {
		if core.NodeCoreModules()[node.Text()] && !core.ExclusivelyPrefixedNodeCoreModules[node.Text()] {
			if strings.HasPrefix(node.Text(), "node:") {
				return core.TSTrue
			} else {
				return core.TSFalse
			}
		}
	}
	return program.UsesUriStyleNodeCoreModules()
}
func QuotePreferenceFromString(str ast.Handle) QuotePreference {
	if str.TokenFlags()&ast.TokenFlagsSingleQuote != 0 {
		return QuotePreferenceSingle
	}
	return QuotePreferenceDouble
}
func GetQuotePreference(sourceFile *ast.SourceFile, preferences UserPreferences) QuotePreference {
	if preferences.QuotePreference != "" && preferences.QuotePreference != "auto" {
		if preferences.QuotePreference == "single" {
			return QuotePreferenceSingle
		}
		return QuotePreferenceDouble
	}
	firstModuleSpecifier := core.Find(sourceFile.Imports(), func(n ast.Handle) bool {
		return ast.IsStringLiteral(n) && !ast.NodeIsSynthesized(n.Parent())
	})
	if !firstModuleSpecifier.IsNil() {
		return QuotePreferenceFromString(firstModuleSpecifier)
	}
	return QuotePreferenceDouble
}
func ModuleSymbolToValidIdentifier(moduleSymbol *ast.Symbol, forceCapitalize bool) string {
	return ModuleSpecifierToValidIdentifier(stringutil.StripQuotes(moduleSymbol.Name), forceCapitalize)
}
func ModuleSpecifierToValidIdentifier(moduleSpecifier string, forceCapitalize bool) string {
	baseName := tspath.GetBaseFileName(strings.TrimSuffix(tspath.RemoveAnyFileExtension(moduleSpecifier), "/index"))
	res := []rune{}
	lastCharWasValid := true
	baseNameRunes := []rune(baseName)
	if len(baseNameRunes) > 0 && scanner.IsIdentifierStart(baseNameRunes[0]) {
		if forceCapitalize {
			res = append(res, unicode.ToUpper(baseNameRunes[0]))
		} else {
			res = append(res, baseNameRunes[0])
		}
	} else {
		lastCharWasValid = false
	}
	for i := 1; i < len(baseNameRunes); i++ {
		isValid := scanner.IsIdentifierPart(baseNameRunes[i])
		if isValid {
			if !lastCharWasValid {
				res = append(res, unicode.ToUpper(baseNameRunes[i]))
			} else {
				res = append(res, baseNameRunes[i])
			}
		}
		lastCharWasValid = isValid
	}
	resString := string(res)
	if resString != "" && !IsNonContextualKeyword(scanner.StringToToken(resString)) {
		return resString
	}
	return "_" + resString
}
func IsNonContextualKeyword(token ast.Kind) bool {
	return ast.IsKeywordKind(token) && !ast.IsContextualKeyword(token)
}
