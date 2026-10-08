package storeharness

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/baseline"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/harnessutil"
)

// The text of a type and symbol baseline
// (store-conformance-semantic-instructions-20261008.md 0.1): the functions of
// internal/testutil/tsbaseline/type_symbol_baseline.go and util.go, revision
// 28c68f49f8, written out with the walker's records for its typeWriterResult
// (tsbaseline is not exported, and is not changed). Its regular expressions
// and replacer that baseline_test.go already copies are those.

// bracketLineRegex is type_symbol_baseline.go's.
var bracketLineRegex = regexp.MustCompile(`^\s*[{|}]\s*$`)

// baselineResult is type_symbol_baseline.go's typeWriterResult: the line of
// the node, its text, and its type or symbol string. The underline is never
// written there.
type baselineResult struct {
	line       int
	sourceText string
	value      string
}

// baselineResults are the results of a walk of file: the records it gives a
// string, each at the line of its node's first token (writeTypeOrSymbol).
func baselineResults(file ast.SourceFileLike, records []walkRecord) []baselineResult {
	var results []baselineResult
	for _, r := range records {
		if r.present {
			line := scanner.GetECMALineOfPosition(file, scanner.SkipTrivia(file.Text(), r.pos))
			results = append(results, baselineResult{line: line, sourceText: r.text, value: r.value})
		}
	}
	return results
}

// generateBaseline is type_symbol_baseline.go's, without the performance
// lines it does not write.
func generateBaseline(allFiles []*harnessutil.TestFile, results [][]baselineResult, header string, isSymbolBaseline bool) string {
	var result strings.Builder
	for _, value := range iterateBaseline(allFiles, results, isSymbolBaseline) {
		result.WriteString(value)
	}
	if result.Len() > 0 {
		return fmt.Sprintf("//// [%s] ////\r\n\r\n%s", header, result.String())
	}
	return baseline.NoContent
}

// iterateBaseline is type_symbol_baseline.go's: results[i] are the walk's of
// allFiles[i].
func iterateBaseline(allFiles []*harnessutil.TestFile, results [][]baselineResult, isSymbolBaseline bool) []string {
	var baselines []string

	for i, file := range allFiles {
		unitName := file.UnitName
		var typeLines strings.Builder
		typeLines.WriteString("=== ")
		typeLines.WriteString(unitName)
		typeLines.WriteString(" ===\r\n")
		codeLines := codeLinesRegexp.Split(file.Content, -1)
		lastIndexWritten := -1
		for _, result := range results[i] {
			if isSymbolBaseline && result.value == "" {
				return baselines
			}
			if lastIndexWritten == -1 {
				typeLines.WriteString(strings.Join(codeLines[:result.line+1], "\r\n"))
				typeLines.WriteString("\r\n")
			} else if lastIndexWritten != result.line {
				if !(lastIndexWritten+1 < len(codeLines) &&
					(bracketLineRegex.MatchString(codeLines[lastIndexWritten+1]) || strings.TrimSpace(codeLines[lastIndexWritten+1]) == "")) {
					typeLines.WriteString("\r\n")
				}
				typeLines.WriteString(strings.Join(codeLines[lastIndexWritten+1:result.line+1], "\r\n"))
				typeLines.WriteString("\r\n")
			}
			lastIndexWritten = result.line
			lineText := lineDelimiter.ReplaceAllString(result.sourceText, "")
			typeLines.WriteString(">")
			fmt.Fprintf(&typeLines, "%s : %s", lineText, result.value)
			typeLines.WriteString("\r\n")
		}

		if lastIndexWritten+1 < len(codeLines) {
			if !(lastIndexWritten+1 < len(codeLines) &&
				(bracketLineRegex.MatchString(codeLines[lastIndexWritten+1]) || strings.TrimSpace(codeLines[lastIndexWritten+1]) == "")) {
				typeLines.WriteString("\r\n")
			}
			typeLines.WriteString(strings.Join(codeLines[lastIndexWritten+1:], "\r\n"))
		}
		typeLines.WriteString("\r\n")

		// util.go's removeTestPathPrefixes, the trailing separator not retained.
		baselines = append(baselines, testPathPrefixReplacer.Replace(typeLines.String()))
	}

	return baselines
}
