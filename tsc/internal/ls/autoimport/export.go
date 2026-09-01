package autoimport

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

type ModuleID string
type ExportID struct {
	ModuleID   ModuleID
	ExportName string
}
type ExportSyntax int

const (
	ExportSyntaxNone ExportSyntax = iota
	ExportSyntaxModifier
	ExportSyntaxNamed
	ExportSyntaxDefaultModifier
	ExportSyntaxDefaultDeclaration
	ExportSyntaxEquals
	ExportSyntaxUMD
	ExportSyntaxStar
	ExportSyntaxCommonJSModuleExports
	ExportSyntaxCommonJSExportsProperty
)

type Export struct {
	ExportID
	ModuleFileName             string
	Syntax                     ExportSyntax
	Flags                      ast.SymbolFlags
	localName                  string
	through                    string
	Target                     ExportID
	IsTypeOnly                 bool
	ScriptElementKind          lsutil.ScriptElementKind
	ScriptElementKindModifiers lsutil.ScriptElementKindModifier
	Path                       tspath.Path
	PackageName                string
}

func (e *Export) Name() string {
	if e.localName != "" {
		return e.localName
	}
	if e.ExportName == ast.InternalSymbolNameExportEquals {
		return e.Target.ExportName
	}
	return e.ExportName
}
func (e *Export) IsRenameable() bool {
	return e.ExportName == ast.InternalSymbolNameExportEquals || e.ExportName == ast.InternalSymbolNameDefault
}
func (e *Export) AmbientModuleName() string {
	if !tspath.IsExternalModuleNameRelative(string(e.ModuleID)) {
		return string(e.ModuleID)
	}
	return ""
}
func (e *Export) IsUnresolvedAlias() bool {
	return e.Flags == ast.SymbolFlagsAlias
}
func SymbolToExport(symbol *ast.Symbol, ch *checker.Checker) *Export {
	if symbol.Parent != nil && checker.IsExternalModuleSymbol(symbol.Parent) {
		if moduleID, moduleFileName, ok := tryGetModuleIDAndFileNameOfModuleSymbol(symbol.Parent); ok {
			return extractFirstExport(symbol, ch, moduleID, moduleFileName, ast.GetSourceFileOfModule(symbol.Parent))
		}
		return nil
	}
	declaration := core.FirstOrNil(ast.DeclarationNodes(symbol))
	if declaration.IsNil() {
		return nil
	}
	file := ast.GetSourceFileOfNode(declaration)
	if file.Symbol == nil {
		return nil
	}
	moduleSymbol := ch.GetMergedSymbol(file.Symbol)
	moduleID := ModuleID(file.Path())
	moduleFileName := file.FileName()
	target := ch.GetMergedSymbol(ch.SkipAlias(symbol))
	if export := tryGetModuleExport(ast.InternalSymbolNameDefault, target, moduleSymbol, ch, moduleID, moduleFileName, file); export != nil {
		return export
	}
	if export := tryGetModuleExport(ast.InternalSymbolNameExportEquals, target, moduleSymbol, ch, moduleID, moduleFileName, file); export != nil {
		return export
	}
	return tryGetModuleExport(symbol.Name, target, moduleSymbol, ch, moduleID, moduleFileName, file)
}
func tryGetModuleExport(exportName string, target *ast.Symbol, moduleSymbol *ast.Symbol, ch *checker.Checker, moduleID ModuleID, moduleFileName string, file *ast.SourceFile) *Export {
	exported := ch.TryGetMemberInModuleExportsAndProperties(exportName, moduleSymbol)
	if exported != nil && ch.GetMergedSymbol(ch.SkipAlias(exported)) == target {
		return extractFirstExport(exported, ch, moduleID, moduleFileName, file)
	}
	return nil
}
func extractFirstExport(symbol *ast.Symbol, ch *checker.Checker, moduleID ModuleID, moduleFileName string, file *ast.SourceFile) *Export {
	var exports []*Export
	extractor := newSymbolExtractor("", ch, nil, nil)
	extractor.extractFromSymbol(symbol.Name, symbol, moduleID, moduleFileName, file, &exports)
	return core.FirstOrNil(exports)
}
