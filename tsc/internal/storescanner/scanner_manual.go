package storescanner

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

// The P2 declarations of scanner/scanner.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// GetScannerForSourceFile sets the scanner up through its exported API: the
// Pointer version writes the fields of scanner.Scanner, which this package
// cannot. SetText, SetLanguageVariant and ResetPos write the same fields
// (text, end, languageVariant, pos), and Scan starts from pos as it does.
//
// storetranslate:pointer-hash 79c2d6bc1c53a79f scanner.go:GetScannerForSourceFile
func GetScannerForSourceFile(sourceFile *store.File, pos int) *scanner.Scanner {
	s := scanner.NewScanner()
	s.SetText(sourceFile.Text())
	s.ResetPos(pos)
	s.SetLanguageVariant(sourceFile.LanguageVariant)
	s.Scan()
	return s
}

// ScanTokenAtPosition reads the token through the scanner's exported API
// (Token is the token field).
//
// storetranslate:pointer-hash f6658dac6f33b211 scanner.go:ScanTokenAtPosition
func ScanTokenAtPosition(sourceFile *store.File, pos int) ast.Kind {
	s := GetScannerForSourceFile(sourceFile, pos)
	return s.Token()
}

// GetRangeOfTokenAtPosition reads the range through the scanner's exported
// API (TokenStart and TokenEnd are the tokenStart and pos fields).
//
// storetranslate:pointer-hash 66dc75483bc72875 scanner.go:GetRangeOfTokenAtPosition
func GetRangeOfTokenAtPosition(sourceFile *store.File, pos int) core.TextRange {
	s := GetScannerForSourceFile(sourceFile, pos)
	return core.NewTextRange(s.TokenStart(), s.TokenEnd())
}
