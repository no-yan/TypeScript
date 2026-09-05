package autoimport

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"slices"
	"sync/atomic"
)

type symbolExtractor struct {
	packageName       string
	stats             *extractorStats
	localNameResolver *binder.NameResolver
	checker           *checker.Checker
	toPath            func(fileName string) tspath.Path
	realpath          func(fileName string) string
}
type exportExtractor struct {
	*symbolExtractor
	moduleResolver *module.Resolver
}
type extractorStats struct {
	exports     atomic.Int32
	usedChecker atomic.Int32
}

func (e *exportExtractor) Stats( // realpath, if set, is used to resolve symlinks for ModuleID generation.
// This ensures that symlinked packages use their realpath as ModuleID,
// deduplicating exports from files that appear via multiple symlink paths.
// getModuleID returns the ModuleID for a file, using realpath if available.
// getModuleIDForSymbol returns the ModuleID for a module symbol, using realpath
// normalization when available for source files.
// If fileName is set, this is a source file that may need realpath normalization
// :shrug:
// allExports includes named exports from the file that will be processed separately;
// we want to add only the ones that come from the star
// what is actually desirable here? I think it would be reasonable to only treat these as exports
// if *every* property is a shorthand property or identifier: identifier
// At least, it would be sketchy if there were any methods, computed properties...
// createExport creates an Export for the given symbol, returning the Export and the target symbol if the export is an alias.
// !!! consider GetImmediateAliasedSymbol to go as far as we can
// Last resort: derive identifier from the file name. Use FileName() (original
// casing) rather than ModuleID/Path() which is lowercased on case-insensitive
// file systems, losing PascalCase.
// !!! check if module.exports = foo is marked as an alias
// fileNameForDefaultExportName returns the best file name to use when deriving
// a fallback identifier for a default-like export. It prefers the target symbol's
// source file (closest to the export origin), falls back to the module's original
// file name, and uses the lowercased moduleID only for ambient modules where no
// original file name is available.
) *extractorStats {
	return e.stats
}

type checkerLease struct {
	used    bool
	checker *checker.Checker
}

func (l *checkerLease) GetChecker() *checker.Checker {
	l.used = true
	return l.checker
}
func (l *checkerLease) TryChecker() *checker.Checker {
	if l.used {
		return l.checker
	}
	return nil
}
func newSymbolExtractor(packageName string, checker *checker.Checker, toPath func(string) tspath.Path, realpath func(string) string) *symbolExtractor {
	return &symbolExtractor{packageName: packageName, checker: checker, localNameResolver: &binder.NameResolver{CompilerOptions: core.EmptyCompilerOptions}, stats: &extractorStats{}, toPath: toPath, realpath: realpath}
}
func (b *registryBuilder) newExportExtractor(packageName string, checker *checker.Checker, moduleResolver *module.Resolver, realpath func(string) string) *exportExtractor {
	return &exportExtractor{symbolExtractor: newSymbolExtractor(packageName, checker, b.base.toPath, realpath), moduleResolver: moduleResolver}
}

func (e *symbolExtractor) getModuleID(file *ast.SourceFile) ModuleID {
	if e.realpath != nil && e.toPath != nil {
		realpath := e.realpath(file.FileName())
		return ModuleID(e.toPath(realpath))
	}
	return ModuleID(file.Path())
}

func (e *symbolExtractor) getModuleIDForSymbol(symbol *ast.Symbol) (ModuleID, bool) {
	moduleID, fileName, ok := tryGetModuleIDAndFileNameOfModuleSymbol(symbol)
	if !ok {
		return "", false
	}
	if fileName != "" && e.realpath != nil {
		decl := ast.GetNonAugmentationDeclaration(symbol)
		if !decl.IsNil() && decl.Kind == ast.KindSourceFile {
			return e.getModuleID(ast.GetSourceFileOfNode(decl)), true
		}
	}
	return moduleID, true
}
func (e *exportExtractor) extractFromFile(file *ast.SourceFile) []*Export {
	if file.Symbol != nil {
		return e.extractFromModule(file)
	}
	if len(file.AmbientModuleNames) > 0 {
		var exportCount int
		for _, statement := range file.ParseRoot().Statements() {
			if ast.IsModuleWithStringLiteralName(statement) && isNonPatternAmbientModuleDeclaration(file, statement) {
				exportCount += len(statement.Symbol().Exports)
			}
		}
		exports := make([]*Export, 0, exportCount)
		for _, statement := range file.ParseRoot().Statements() {
			if ast.IsModuleWithStringLiteralName(statement) && isNonPatternAmbientModuleDeclaration(file, statement) {
				e.extractFromModuleDeclaration(statement, file, ModuleID(statement.Name().Text()), "", &exports)
			}
		}
		return exports
	}
	return nil
}
func isNonPatternAmbientModuleDeclaration(file *ast.SourceFile, decl ast.Handle) bool {
	for _, module := range file.PatternAmbientModules {
		if module.Symbol == decl.Symbol() {
			return false
		}
	}
	return true
}
func (e *exportExtractor) extractFromModule(file *ast.SourceFile) []*Export {
	moduleAugmentations := core.MapNonNil(file.ModuleAugmentations, func(name ast.Handle) ast.Handle {
		decl := name.Parent()
		if ast.IsGlobalScopeAugmentation(decl) {
			return ast.Handle{}
		}
		return decl
	})
	var augmentationExportCount int
	for _, decl := range moduleAugmentations {
		augmentationExportCount += len(decl.Symbol().Exports)
	}
	moduleID := e.getModuleID(file)
	exports := make([]*Export, 0, len(file.Symbol.Exports)+augmentationExportCount)
	for name, symbol := range file.Symbol.Exports {
		e.extractFromSymbol(name, symbol, moduleID, file.FileName(), file, &exports)
	}
	for _, decl := range moduleAugmentations {
		name := decl.Name().Text()
		moduleID := ModuleID(name)
		var moduleFileName string
		if tspath.IsExternalModuleNameRelative(name) {
			if resolved, _ := e.moduleResolver.ResolveModuleName(name, file.FileName(), core.ModuleKindCommonJS, nil); resolved.IsResolved() {
				moduleFileName = resolved.ResolvedFileName
				moduleID = ModuleID(e.toPath(moduleFileName))
			} else {
				moduleFileName = tspath.ResolvePath(tspath.GetDirectoryPath(file.FileName()), name)
				moduleID = ModuleID(e.toPath(moduleFileName))
			}
		}
		e.extractFromModuleDeclaration(decl, file, moduleID, moduleFileName, &exports)
	}
	return exports
}
func (e *exportExtractor) extractFromModuleDeclaration(decl ast.Handle, file *ast.SourceFile, moduleID ModuleID, moduleFileName string, exports *[]*Export) {
	for name, symbol := range decl.Symbol().Exports {
		e.extractFromSymbol(name, symbol, moduleID, moduleFileName, file, exports)
	}
}
func (e *symbolExtractor) extractFromSymbol(name string, symbol *ast.Symbol, moduleID ModuleID, moduleFileName string, file *ast.SourceFile, exports *[]*Export) {
	if shouldIgnoreSymbol(symbol) {
		return
	}
	if name == ast.InternalSymbolNameExportStar {
		checkerLease := &checkerLease{checker: e.checker}
		allExports := e.checker.GetExportsOfModule(symbol.Parent)
		for name, namedExport := range symbol.Parent.Exports {
			if name != ast.InternalSymbolNameExportStar {
				idx := slices.Index(allExports, namedExport)
				if idx >= 0 || shouldIgnoreSymbol(namedExport) {
					allExports = slices.Delete(allExports, idx, idx+1)
				}
			}
		}
		*exports = slices.Grow(*exports, len(allExports))
		for _, reexportedSymbol := range allExports {
			export, _ := e.createExport(reexportedSymbol, moduleID, moduleFileName, ExportSyntaxStar, file, checkerLease)
			if export != nil {
				parent := checkerLease.GetChecker().GetMergedSymbol(reexportedSymbol.Parent)
				if parent != nil && parent.IsExternalModule() {
					if targetModuleID, ok := e.getModuleIDForSymbol(parent); ok {
						export.Target = ExportID{ExportName: reexportedSymbol.Name, ModuleID: targetModuleID}
					}
				}
				export.through = ast.InternalSymbolNameExportStar
				*exports = append(*exports, export)
			}
		}
		return
	}
	syntax := getSyntax(symbol)
	checkerLease := &checkerLease{checker: e.checker}
	export, target := e.createExport(symbol, moduleID, moduleFileName, syntax, file, checkerLease)
	if export == nil {
		return
	}
	*exports = append(*exports, export)
	if target != nil {
		if syntax == ExportSyntaxEquals && target.Flags&ast.SymbolFlagsNamespace != 0 {
			*exports = slices.Grow(*exports, len(target.Exports))
			for innerName, namedExport := range target.Exports {
				if innerName != ast.InternalSymbolNameExportStar {
					export, _ := e.createExport(namedExport, moduleID, moduleFileName, syntax, file, checkerLease)
					if export != nil {
						export.through = name
						*exports = append(*exports, export)
					}
				}
			}
		}
	} else if syntax == ExportSyntaxCommonJSModuleExports {
		expression := ast.NodeOf(symbol.Declarations[0]).BinaryExpressionRight()
		if expression.Kind == ast.KindObjectLiteralExpression {
			*exports = slices.Grow(*exports, expression.Store().ListLen(expression.ObjectLiteralExpressionProperties()))
			for _, prop := range expression.Store().ListSlice(expression.ObjectLiteralExpressionProperties()).All() {
				if ast.IsShorthandPropertyAssignment(prop) || ast.IsPropertyAssignment(prop) && prop.PropertyAssignmentName().Kind == ast.KindIdentifier {
					export, _ := e.createExport(expression.Symbol().Members[prop.Name().Text()], moduleID, moduleFileName, syntax, file, checkerLease)
					if export != nil {
						export.through = name
						*exports = append(*exports, export)
					}
				}
			}
		}
	}
}

func (e *symbolExtractor) createExport(symbol *ast.Symbol, moduleID ModuleID, moduleFileName string, syntax ExportSyntax, file *ast.SourceFile, checkerLease *checkerLease) (*Export, *ast.Symbol) {
	if shouldIgnoreSymbol(symbol) {
		return nil, nil
	}
	export := &Export{ExportID: ExportID{ExportName: symbol.Name, ModuleID: moduleID}, ModuleFileName: moduleFileName, Syntax: syntax, Flags: symbol.CombinedLocalAndExportSymbolFlags(), Path: file.Path(), PackageName: e.packageName}
	if syntax == ExportSyntaxUMD {
		export.ExportName = ast.InternalSymbolNameExportEquals
		export.localName = symbol.Name
	}
	var targetSymbol *ast.Symbol
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		targetSymbol = e.tryResolveSymbol(symbol, syntax, checkerLease)
		if targetSymbol != nil {
			var decl ast.Handle
			if len(targetSymbol.Declarations) > 0 {
				decl = ast.NodeOf(targetSymbol.Declarations[0])
			} else if targetSymbol.CheckFlags&ast.CheckFlagsMapped != 0 {
				if mappedDecl := checkerLease.GetChecker().GetMappedTypeSymbolOfProperty(targetSymbol); mappedDecl != nil && len(mappedDecl.Declarations) > 0 {
					decl = ast.NodeOf(mappedDecl.Declarations[0])
				}
			}
			if decl.IsNil() {
				decl = ast.NodeOf(symbol.Declarations[0])
			}
			if decl.IsNil() {
				panic("no declaration for aliased symbol")
			}
			parent := targetSymbol.Parent
			if checker := checkerLease.TryChecker(); checker != nil {
				export.Flags = checker.GetSymbolFlags(targetSymbol)
				export.IsTypeOnly = !checker.GetTypeOnlyAliasDeclaration(symbol).IsNil()
				parent = checker.GetMergedSymbol(parent)
			} else {
				export.Flags = targetSymbol.Flags
				export.IsTypeOnly = ast.SomeDeclaration(symbol, ast.IsPartOfTypeOnlyImportOrExportDeclaration)
			}
			export.ScriptElementKind = lsutil.GetSymbolKind(checkerLease.TryChecker(), targetSymbol, decl)
			export.ScriptElementKindModifiers = lsutil.GetSymbolModifiers(checkerLease.TryChecker(), targetSymbol)
			targetModuleID := ModuleID(ast.GetSourceFileOfNode(decl).Path())
			if parent != nil && parent.IsExternalModule() {
				if id, ok := e.getModuleIDForSymbol(parent); ok {
					targetModuleID = id
				}
			}
			export.Target = ExportID{ExportName: targetSymbol.Name, ModuleID: targetModuleID}
		}
	} else {
		export.ScriptElementKind = lsutil.GetSymbolKind(checkerLease.TryChecker(), symbol, ast.NodeOf(symbol.Declarations[0]))
		export.ScriptElementKindModifiers = lsutil.GetSymbolModifiers(checkerLease.TryChecker(), symbol)
	}
	if symbol.Name == ast.InternalSymbolNameDefault || symbol.Name == ast.InternalSymbolNameExportEquals {
		namedSymbol := symbol
		if s := binder.GetLocalSymbolForExportDefault(symbol); s != nil {
			namedSymbol = s
		}
		export.localName = getDefaultLikeExportNameFromDeclaration(namedSymbol)
		if isUnusableName(export.localName) {
			export.localName = export.Target.ExportName
		}
		if isUnusableName(export.localName) {
			if targetSymbol != nil {
				namedSymbol = targetSymbol
				if s := binder.GetLocalSymbolForExportDefault(targetSymbol); s != nil {
					namedSymbol = s
				}
				export.localName = getDefaultLikeExportNameFromDeclaration(namedSymbol)
			}
		}
		if isUnusableName(export.localName) {
			export.localName = lsutil.ModuleSpecifierToValidIdentifier(fileNameForDefaultExportName(targetSymbol, moduleFileName, moduleID), false)
		}
	}
	if isUnusableName(export.Name()) {
		return nil, nil
	}
	e.stats.exports.Add(1)
	if checkerLease.TryChecker() != nil {
		e.stats.usedChecker.Add(1)
	}
	return export, targetSymbol
}
func (e *symbolExtractor) tryResolveSymbol(symbol *ast.Symbol, syntax ExportSyntax, checkerLease *checkerLease) *ast.Symbol {
	if !ast.IsNonLocalAlias(symbol, ast.SymbolFlagsNone) {
		return symbol
	}
	var loc ast.Handle
	var name string
	switch syntax {
	case ExportSyntaxNamed:
		decl := ast.GetDeclarationOfKind(symbol, ast.KindExportSpecifier)
		if decl.Parent().Parent().ExportDeclarationModuleSpecifier().IsNil() {
			if n := core.FirstNonZero(decl.Name(), decl.PropertyName()); n.Kind == ast.KindIdentifier {
				loc = n
				name = n.Text()
			}
		}
	case ExportSyntaxEquals:
		if symbol.Name != ast.InternalSymbolNameExportEquals {
			break
		}
		fallthrough
	case ExportSyntaxDefaultDeclaration:
		decl := ast.GetDeclarationOfKind(symbol, ast.KindExportAssignment)
		if decl.Expression().Kind == ast.KindIdentifier {
			loc = decl.Expression()
			name = loc.Text()
		}
	}
	if !loc.IsNil() {
		local := e.localNameResolver.Resolve(loc, name, ast.SymbolFlagsAll, nil, false, false)
		if local != nil && !ast.IsNonLocalAlias(local, ast.SymbolFlagsNone) {
			return local
		}
	}
	checker := checkerLease.GetChecker()
	if resolved := checker.GetAliasedSymbol(symbol); !checker.IsUnknownSymbol(resolved) {
		return resolved
	}
	return nil
}
func shouldIgnoreSymbol(symbol *ast.Symbol) bool {
	if symbol.Flags&ast.SymbolFlagsPrototype != 0 {
		return true
	}
	return false
}
func getSyntax(symbol *ast.Symbol) ExportSyntax {
	for _, decl := range ast.DeclarationNodes(symbol).All() {
		switch decl.Kind {
		case ast.KindExportSpecifier:
			return ExportSyntaxNamed
		case ast.KindExportAssignment:
			return core.IfElse(decl.ExportAssignmentIsExportEquals(), ExportSyntaxEquals, ExportSyntaxDefaultDeclaration)
		case ast.KindNamespaceExportDeclaration:
			return ExportSyntaxUMD
		case ast.KindBinaryExpression:
			switch ast.GetAssignmentDeclarationKind(decl) {
			case ast.JSDeclarationKindModuleExports:
				return ExportSyntaxCommonJSModuleExports
			case ast.JSDeclarationKindExportsProperty:
				return ExportSyntaxCommonJSExportsProperty
			}
		default:
			if ast.GetCombinedModifierFlags(decl)&ast.ModifierFlagsDefault != 0 {
				return ExportSyntaxDefaultModifier
			} else {
				return ExportSyntaxModifier
			}
		}
	}
	return ExportSyntaxNone
}
func isUnusableName(name string) bool {
	return name == "" || name == "_default" || name == ast.InternalSymbolNameExportStar || name == ast.InternalSymbolNameDefault || name == ast.InternalSymbolNameExportEquals
}

func fileNameForDefaultExportName(targetSymbol *ast.Symbol, moduleFileName string, moduleID ModuleID) string {
	if targetSymbol != nil && len(targetSymbol.Declarations) > 0 {
		if fn := ast.GetSourceFileOfNode(ast.NodeOf(targetSymbol.Declarations[0])).FileName(); fn != "" {
			return fn
		}
	}
	if moduleFileName != "" {
		return moduleFileName
	}
	return string(moduleID)
}
