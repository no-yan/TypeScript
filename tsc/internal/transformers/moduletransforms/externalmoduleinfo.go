package moduletransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
	"slices"
)

type externalModuleInfo struct {
	externalImports []ast.Handle
	exportSpecifiers             collections.MultiMap[string, ast.Handle]
	exportedBindings             collections.MultiMap[ast.Handle, ast.Handle]
	exportedNames                []ast.Handle
	exportedFunctions            collections.OrderedSet[ast.Handle]
	exportEquals ast.Handle
	hasExportStarsToExportValues bool
}
type externalModuleInfoCollector struct {
	sourceFile       *ast.SourceFile
	compilerOptions  *core.CompilerOptions
	emitContext      *printer.EmitContext
	resolver         binder.ReferenceResolver
	uniqueExports    collections.Set[string]
	hasExportDefault bool
	output           *externalModuleInfo
}

func collectExternalModuleInfo(sourceFile *ast.SourceFile, compilerOptions *core.CompilerOptions, emitContext *printer.EmitContext, resolver binder.ReferenceResolver) *externalModuleInfo {
	c := externalModuleInfoCollector{sourceFile: sourceFile, compilerOptions: compilerOptions, emitContext: emitContext, resolver: resolver, output: &externalModuleInfo{}}
	return c.collect()
}
func (c *externalModuleInfoCollector) collect() *externalModuleInfo {
	hasImportStar := false
	hasImportDefault := false
	for _, node := range c.sourceFile.ParseRoot().Statements() {
		if ast.IsNotEmittedStatement(node) {
			original := c.emitContext.MostOriginal(node)
			if !original.IsNil() && ast.IsExportAssignment(original) {
				n := original
				if n.IsExportEquals() && c.output.exportEquals.IsNil() {
					c.output.exportEquals = n
				}
			}
			continue
		}
		switch node.Kind() {
		case ast.KindImportDeclaration:
			n := node
			c.addExternalImport(node)
			if !hasImportStar && getImportNeedsImportStarHelper(n) {
				hasImportStar = true
			}
			if !hasImportDefault && getImportNeedsImportDefaultHelper(n) {
				hasImportDefault = true
			}
		case ast.KindImportEqualsDeclaration:
			n := node
			if ast.IsExternalModuleReference(n.ModuleReference()) {
				c.addExternalImport(node)
			}
		case ast.KindExportDeclaration:
			n := node
			if !n.ModuleSpecifier().IsNil() {
				c.addExternalImport(node)
				if n.ExportClause().IsNil() {
					c.output.hasExportStarsToExportValues = true
				} else if ast.IsNamedExports(n.ExportClause()) {
					c.addExportedNamesForExportDeclaration(n)
					if !hasImportDefault {
						hasImportDefault = containsDefaultReference(n.ExportClause())
					}
				} else {
					name := n.ExportClause().NamespaceExportName()
					nameText := name.Text()
					if c.addUniqueExport(nameText) {
						c.addExportedBinding(node, name)
						c.addExportedName(name)
					}
					hasImportStar = true
				}
			} else {
				c.addExportedNamesForExportDeclaration(node)
			}
		case ast.KindExportAssignment:
			n := node
			if n.IsExportEquals() && c.output.exportEquals.IsNil() {
				c.output.exportEquals = n
			}
		case ast.KindVariableStatement:
			n := node
			if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
				for _, decl := range n.VariableStatementDeclarationList().Declarations() {
					c.collectExportedVariableInfo(decl)
				}
			}
		case ast.KindFunctionDeclaration:
			n := node
			if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
				c.addExportedFunctionDeclaration(n, ast.Handle{}, ast.HasSyntacticModifier(node, ast.ModifierFlagsDefault))
			}
		case ast.KindClassDeclaration:
			n := node
			if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
				if ast.HasSyntacticModifier(node, ast.ModifierFlagsDefault) {
					if !c.hasExportDefault {
						name := n.Name()
						if name.IsNil() {
							name = c.emitContext.Factory.NewGeneratedNameForNode(node)
						}
						c.addExportedBinding(node, name)
						c.hasExportDefault = true
					}
				} else {
					name := n.Name()
					if !name.IsNil() {
						if c.addUniqueExport(name.Text()) {
							c.addExportedBinding(node, name)
							c.addExportedName(name)
						}
					}
				}
			}
		}
	}
	return c.output
}
func (c *externalModuleInfoCollector) addUniqueExport(name string) bool {
	if !c.uniqueExports.Has(name) {
		c.uniqueExports.Add(name)
		return true
	}
	return false
}
func (c *externalModuleInfoCollector) addExportedBinding(decl ast.Handle, name ast.Handle) {
	c.output.exportedBindings.Add(c.emitContext.MostOriginal(decl), name)
}
func (c *externalModuleInfoCollector) addExternalImport(node ast.Handle) {
	c.output.externalImports = append(c.output.externalImports, node)
}
func (c *externalModuleInfoCollector) addExportedName(name ast.Handle) {
	c.output.exportedNames = append(c.output.exportedNames, name)
}
func (c *externalModuleInfoCollector) addExportedNamesForExportDeclaration(node ast.Handle) {
	for _, specifier := range node.ExportClause().Elements() {
		specifierNameText := specifier.Name().Text()
		if c.addUniqueExport(specifierNameText) {
			name := specifier.PropertyNameOrName()
			if name.Kind() != ast.KindStringLiteral {
				if node.ModuleSpecifier().IsNil() {
					c.output.exportSpecifiers.Add(name.Text(), specifier)
				}
				decl := c.resolver.GetReferencedImportDeclaration(c.emitContext.MostOriginal(name))
				if decl.IsNil() {
					decl = c.resolver.GetReferencedValueDeclaration(c.emitContext.MostOriginal(name))
				}
				if !decl.IsNil() {
					if decl.Kind() == ast.KindFunctionDeclaration {
						c.uniqueExports.Delete(specifierNameText)
						c.addExportedFunctionDeclaration(decl, specifier.Name(), ast.ModuleExportNameIsDefault(specifier.Name()))
						continue
					}
					c.addExportedBinding(decl, specifier.Name())
				}
			}
			c.addExportedName(specifier.Name())
		}
	}
}
func (c *externalModuleInfoCollector) addExportedFunctionDeclaration(node ast.Handle, name ast.Handle, isDefault bool) {
	c.output.exportedFunctions.Add(c.emitContext.MostOriginal(node))
	if isDefault {
		if !c.hasExportDefault {
			if name.IsNil() {
				name = c.emitContext.Factory.NewGeneratedNameForNode(node)
			}
			c.addExportedBinding(node, name)
			c.hasExportDefault = true
		}
	} else {
		if name.IsNil() {
			name = node.Name()
		}
		nameText := name.Text()
		if c.addUniqueExport(nameText) {
			c.addExportedBinding(node, name)
		}
	}
}
func (c *externalModuleInfoCollector) collectExportedVariableInfo(decl ast.Handle) {
	if ast.IsBindingPattern(decl.Name()) {
		for _, element := range decl.Name().Elements() {
			e := element
			if !e.Name().IsNil() {
				c.collectExportedVariableInfo(element)
			}
		}
	} else if !c.emitContext.HasAutoGenerateInfo(decl.Name()) {
		text := decl.Name().Text()
		if c.addUniqueExport(text) {
			c.addExportedName(decl.Name())
			if transformers.IsLocalName(c.emitContext, decl.Name()) {
				c.addExportedBinding(decl, decl.Name())
			}
		}
	}
}

const externalHelpersModuleNameText = "tslib"

func createExternalHelpersImportDeclarationIfNeeded(emitContext *printer.EmitContext, sourceFile *ast.SourceFile, compilerOptions *core.CompilerOptions, fileModuleKind core.ModuleKind, hasExportStarsToExportValues bool, hasImportStar bool, hasImportDefault bool) ast.Handle {
	if compilerOptions.ImportHelpers.IsTrue() && ast.IsEffectiveExternalModule(sourceFile, compilerOptions) {
		moduleKind := compilerOptions.GetEmitModuleKind()
		helpers := getImportedHelpers(emitContext, sourceFile)
		if fileModuleKind == core.ModuleKindCommonJS || fileModuleKind == core.ModuleKindNone && moduleKind == core.ModuleKindCommonJS {
			externalHelpersModuleName := getOrCreateExternalHelpersModuleNameIfNeeded(emitContext, sourceFile, compilerOptions, helpers, hasExportStarsToExportValues, hasImportStar || hasImportDefault, fileModuleKind)
			if !externalHelpersModuleName.IsNil() {
				externalHelpersImportDeclaration := emitContext.Factory.NewImportEqualsDeclaration(0, false, externalHelpersModuleName, emitContext.Factory.NewExternalModuleReference(emitContext.Factory.NewStringLiteral(externalHelpersModuleNameText, ast.TokenFlagsNone)))
				emitContext.AddEmitFlags(externalHelpersImportDeclaration, printer.EFCustomPrologue)
				return externalHelpersImportDeclaration
			}
		} else {
			var helperNames []string
			for _, helper := range helpers {
				importName := helper.ImportName
				if len(importName) > 0 {
					helperNames = core.AppendIfUnique(helperNames, importName)
				}
			}
			if len(helperNames) > 0 {
				slices.SortFunc(helperNames, stringutil.CompareStringsCaseSensitive)
				importSpecifiers := core.Map(helperNames, func(name string) ast.Handle {
					if emitContext.IsFileLevelUniqueName(sourceFile, name, nil) {
						return emitContext.Factory.NewImportSpecifier(false, ast.Handle{}, emitContext.Factory.NewIdentifier(name))
					} else {
						return emitContext.Factory.NewImportSpecifier(false, emitContext.Factory.NewIdentifier(name), emitContext.Factory.NewUnscopedHelperName(name))
					}
				})
				namedBindings := emitContext.Factory.NewNamedImports(emitContext.Factory.NewList(importSpecifiers))
				parseNode := emitContext.MostOriginal(sourceFile.ParseRoot())
				emitContext.AddEmitFlags(parseNode, printer.EFExternalHelpers)
				externalHelpersImportDeclaration := emitContext.Factory.NewImportDeclaration(0, emitContext.Factory.NewImportClause(ast.KindUnknown, ast.Handle{}, namedBindings), emitContext.Factory.NewStringLiteral(externalHelpersModuleNameText, ast.TokenFlagsNone), ast.Handle{})
				emitContext.AddEmitFlags(externalHelpersImportDeclaration, printer.EFCustomPrologue)
				return externalHelpersImportDeclaration
			}
		}
	}
	return ast.Handle{}
}
func getImportedHelpers(emitContext *printer.EmitContext, sourceFile *ast.SourceFile) []*printer.EmitHelper {
	var helpers []*printer.EmitHelper
	for _, helper := range emitContext.GetEmitHelpers(sourceFile.ParseRoot()) {
		if !helper.Scoped {
			helpers = append(helpers, helper)
		}
	}
	return helpers
}
func getOrCreateExternalHelpersModuleNameIfNeeded(emitContext *printer.EmitContext, node *ast.SourceFile, compilerOptions *core.CompilerOptions, helpers []*printer.EmitHelper, hasExportStarsToExportValues bool, hasImportStarOrImportDefault bool, fileModuleKind core.ModuleKind) ast.Handle {
	externalHelpersModuleName := emitContext.GetExternalHelpersModuleName(node)
	if !externalHelpersModuleName.IsNil() {
		return externalHelpersModuleName
	}
	create := len(helpers) > 0 || (hasExportStarsToExportValues || hasImportStarOrImportDefault) && fileModuleKind < core.ModuleKindSystem
	if create {
		externalHelpersModuleName = emitContext.Factory.NewUniqueName(externalHelpersModuleNameText)
		emitContext.SetExternalHelpersModuleName(node, externalHelpersModuleName)
	}
	return externalHelpersModuleName
}
func isNamedDefaultReference(e ast.Handle) bool {
	return ast.ModuleExportNameIsDefault(e.PropertyNameOrName())
}
func containsDefaultReference(node ast.Handle) bool {
	return !node.IsNil() && (ast.IsNamedImports(node) || ast.IsNamedExports(node)) && core.Some(node.Elements(), isNamedDefaultReference)
}
func getExportNeedsImportStarHelper(node ast.Handle) bool {
	return !ast.GetNamespaceDeclarationNode(node).IsNil()
}
func getImportNeedsImportStarHelper(node ast.Handle) bool {
	if !ast.GetNamespaceDeclarationNode(node).IsNil() {
		return true
	}
	if node.ImportClause().IsNil() {
		return false
	}
	bindings := node.ImportClause().ImportClauseNamedBindings()
	if bindings.IsNil() {
		return false
	}
	if !ast.IsNamedImports(bindings) {
		return false
	}
	namedImports := bindings
	defaultRefCount := 0
	for _, binding := range namedImports.Elements() {
		if isNamedDefaultReference(binding) {
			defaultRefCount++
		}
	}
	return (defaultRefCount > 0 && defaultRefCount != len(namedImports.Elements())) || ((len(namedImports.Elements())-defaultRefCount) != 0 && ast.IsDefaultImport(node))
}
func getImportNeedsImportDefaultHelper(node ast.Handle) bool {
	return !getImportNeedsImportStarHelper(node) && (ast.IsDefaultImport(node) || (!node.ImportClause().IsNil() && ast.IsNamedImports(node.ImportClause().ImportClauseNamedBindings()) && containsDefaultReference(node.ImportClause().ImportClauseNamedBindings())))
}
