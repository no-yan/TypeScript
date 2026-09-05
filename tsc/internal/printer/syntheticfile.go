package printer

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

func PrintAndPositionNode(factory ast.HandleFactory, node ast.Handle, sourceFile *ast.SourceFile, newLine string, indentSize int, emitContext *EmitContext) (text string, positioned ast.Handle) {
	writer := NewChangeTrackerWriter(newLine, indentSize)
	NewPrinter(PrinterOptions{NewLine: core.GetNewLineKind(newLine), NeverAsciiEscape: true, PreserveSourceNewlines: true, TerminateUnterminatedLiterals: true}, writer.GetPrintHandlers(), emitContext).Write(node, sourceFile, writer, nil)
	text = writer.String()
	text = strings.TrimSuffix(text, newLine)
	positioned = writer.AssignPositionsToNode(node, factory)
	return text, positioned
}

func CreateSyntheticSourceFile(factory ast.HandleFactory, node ast.Handle, text string, parseOptions ast.SourceFileParseOptions) *ast.SourceFile {
	eof := factory.NewToken(ast.KindEndOfFile)
	eof.SetLoc(core.NewTextRange(len(text), len(text)))
	statements := factory.List(core.NewTextRange(node.Pos(), node.End()), node)
	root := factory.NewSourceFile(statements, eof)
	root.SetLoc(core.NewTextRange(0, len(text)))
	root.SetParentsInChildren()
	file := ast.NewSourceFileMetadata(parseOptions, text)
	file.SetParseStore(factory.Store(), root)
	return file
}
