package storecompiler

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/diagnosticwriter"
)

// writerDiagnostic is a Store diagnostic as the CLI's writer prints it
// (diagnosticwriter.ASTDiagnostic's counterpart, with no content mapper).
type writerDiagnostic struct {
	*store.Diagnostic
}

var _ diagnosticwriter.Diagnostic = writerDiagnostic{}

// WrapDiagnostics are ds as the writer prints them.
func WrapDiagnostics(ds []*store.Diagnostic) []diagnosticwriter.Diagnostic {
	out := make([]diagnosticwriter.Diagnostic, len(ds))
	for i, d := range ds {
		out[i] = writerDiagnostic{d}
	}
	return out
}

// CompareWrappedDiagnostics is store.CompareDiagnostics of two of
// WrapDiagnostics's diagnostics (diagnosticwriter.CompareASTDiagnostics's
// counterpart).
func CompareWrappedDiagnostics(a, b diagnosticwriter.Diagnostic) int {
	return store.CompareDiagnostics(a.(writerDiagnostic).Diagnostic, b.(writerDiagnostic).Diagnostic)
}

func (d writerDiagnostic) File() diagnosticwriter.FileLike {
	if f := d.Diagnostic.File(); f != nil {
		return f
	}
	return nil
}

func (d writerDiagnostic) MessageChain() []diagnosticwriter.Diagnostic {
	return WrapDiagnostics(d.Diagnostic.MessageChain())
}

func (d writerDiagnostic) RelatedInformation() []diagnosticwriter.Diagnostic {
	return WrapDiagnostics(d.Diagnostic.RelatedInformation())
}
