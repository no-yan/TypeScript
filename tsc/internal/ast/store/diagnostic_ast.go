package store

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// DiagnosticFromAST is d as a Store diagnostic, every field kept: the parser's
// and the binder's diagnostics of a Store file are *ast.Diagnostic with no
// file (the program attaches it), and the config's and the module resolver's
// are the Pointer's (store-cli-design-20261004.md 5). fileOf gives the Store
// file of d's file and of its chain and related information, nil included.
func DiagnosticFromAST(d *ast.Diagnostic, fileOf func(*ast.SourceFile) *File) *Diagnostic {
	if d == nil {
		return nil
	}
	var repopulateInfo *RepopulateDiagnosticInfo
	if r := d.RepopulateInfo(); r != nil {
		repopulateInfo = &RepopulateDiagnosticInfo{
			Kind:            RepopulateDiagnosticKind(r.Kind),
			ModuleReference: r.ModuleReference,
			Mode:            r.Mode,
			PackageName:     r.PackageName,
		}
	}
	return &Diagnostic{
		file:               fileOf(d.File()),
		loc:                d.Loc(),
		code:               d.Code(),
		category:           d.Category(),
		source:             d.Source(),
		message:            d.Message(),
		messageText:        d.MessageText(),
		messageKey:         d.MessageKey(),
		messageArgs:        d.MessageArgs(),
		messageChain:       DiagnosticsFromAST(d.MessageChain(), fileOf),
		relatedInformation: DiagnosticsFromAST(d.RelatedInformation(), fileOf),
		reportsUnnecessary: d.ReportsUnnecessary(),
		reportsDeprecated:  d.ReportsDeprecated(),
		skippedOnNoEmit:    d.SkippedOnNoEmit(),
		repopulateInfo:     repopulateInfo,
	}
}

// DiagnosticsFromAST converts each of ds; nil stays nil.
func DiagnosticsFromAST(ds []*ast.Diagnostic, fileOf func(*ast.SourceFile) *File) []*Diagnostic {
	if ds == nil {
		return nil
	}
	out := make([]*Diagnostic, len(ds))
	for i, d := range ds {
		out[i] = DiagnosticFromAST(d, fileOf)
	}
	return out
}

// InFile is a fileOf for the diagnostics of f that have no file (the parser's
// and the binder's): any other file is a mistake of the caller.
func InFile(f *File) func(*ast.SourceFile) *File {
	return func(sf *ast.SourceFile) *File {
		if sf != nil {
			panic("store: a diagnostic of a Store file has a Pointer file")
		}
		return f
	}
}
