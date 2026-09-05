package parser

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

func collectExternalModuleReferences(file *ast.SourceFile) {
	root := file.ParseRoot()
	if root.IsNil() {
		return
	}
	for _, node := range root.Statements() {
		collectModuleReferences(file, node, false)
	}
	if file.Flags&ast.NodeFlagsPossiblyContainsDynamicImport != 0 || ast.IsSourceFileJS(file) {
		ast.ForEachDynamicImportOrRequireCall(file, true, true, func(_ ast.Handle, moduleSpecifier ast.Handle) bool {
			ast.SetImportsOfSourceFile(file, append(file.Imports(), moduleSpecifier))
			return false
		})
	}
}

func collectModuleReferences(file *ast.SourceFile, node ast.Handle, inAmbientModule bool) {
	if ast.IsAnyImportOrReExport(node) {
		moduleNameExpr := handleExternalModuleName(node)
		if !moduleNameExpr.IsNil() && moduleNameExpr.Kind == ast.KindStringLiteral {
			moduleName := handleText(moduleNameExpr)
			if moduleName != "" && (!inAmbientModule || !tspath.IsExternalModuleNameRelative(moduleName)) {
				ast.SetImportsOfSourceFile(file, append(file.Imports(), moduleNameExpr))
				if file.UsesUriStyleNodeCoreModules != core.TSTrue && !file.IsDeclarationFile {
					if strings.HasPrefix(moduleName, "node:") && !core.ExclusivelyPrefixedNodeCoreModules[moduleName] {
						file.UsesUriStyleNodeCoreModules = core.TSTrue
					} else if file.UsesUriStyleNodeCoreModules == core.TSUnknown && core.UnprefixedNodeCoreModules[moduleName] {
						file.UsesUriStyleNodeCoreModules = core.TSFalse
					}
				}
			}
		}
		return
	}
	if node.Kind == ast.KindModuleDeclaration && ast.IsAmbientModule(node) && (inAmbientModule || ast.HasSyntacticModifier(node, ast.ModifierFlagsAmbient) || file.IsDeclarationFile) {
		name := node.ModuleDeclarationName()
		nameText := handleText(name)
		if ast.IsExternalModule(file) || (inAmbientModule && !tspath.IsExternalModuleNameRelative(nameText)) {
			file.ModuleAugmentations = append(file.ModuleAugmentations, name)
			return
		}
		if !inAmbientModule {
			file.AmbientModuleNames = append(file.AmbientModuleNames, nameText)
			if !node.Body().IsNil() {
				for _, statement := range node.Body().Statements() {
					collectModuleReferences(file, statement, true)
				}
			}
		}
	}
}


func handleExternalModuleName(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration:
		return node.ImportDeclarationModuleSpecifier()
	case ast.KindExportDeclaration:
		return node.ExportDeclarationModuleSpecifier()
	case ast.KindImportEqualsDeclaration:
		ref := node.ImportEqualsDeclarationModuleReference()
		if !ref.IsNil() && ref.Kind == ast.KindExternalModuleReference {
			return ref.Expression()
		}
		return ast.Handle{}
	case ast.KindModuleDeclaration:
		name := node.ModuleDeclarationName()
		if !name.IsNil() && name.Kind == ast.KindStringLiteral {
			return name
		}
		return ast.Handle{}
	}
	return ast.Handle{}
}
