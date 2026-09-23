package storeparser

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// internal/parser/references.go on the Store, followed by the external module
// indicator of internal/ast/parseoptions.go. The references run on the
// finished Store; the indicator runs on the scratch through Builder.View
// before the top-level await reparse that depends on it.

func collectExternalModuleReferences(file *store.File) {
	for _, node := range file.Root().AsSourceFile().Statements().Refs() {
		collectModuleReferences(file, file.Store.Node(node), false /*inAmbientModule*/)
	}

	if file.Flags&ast.NodeFlagsPossiblyContainsDynamicImport != 0 || store.IsInJSFile(file.Root()) {
		forEachDynamicImportOrRequireCall(file /*includeTypeSpaceImports*/, true /*requireStringLiteralLikeArgument*/, true, func(node store.Node, moduleSpecifier store.Node) bool {
			file.Imports = append(file.Imports, moduleSpecifier.Ref())
			return false
		})
	}
}

func collectModuleReferences(file *store.File, node store.Node, inAmbientModule bool) {
	if store.IsAnyImportOrReExport(node) {
		moduleNameExpr := store.GetExternalModuleName(node)
		// TypeScript 1.0 spec (April 2014): 12.1.6
		// An ExternalImportDeclaration in an AmbientExternalModuleDeclaration may reference other external modules
		// only through top - level external module names. Relative external module names are not permitted.
		if !moduleNameExpr.IsNil() && store.IsStringLiteral(moduleNameExpr) {
			moduleName := moduleNameExpr.Text()
			if moduleName != "" && (!inAmbientModule || !tspath.IsExternalModuleNameRelative(moduleName)) {
				file.Imports = append(file.Imports, moduleNameExpr.Ref())
				// !!! removed `&& p.currentNodeModulesDepth == 0`
				if file.UsesUriStyleNodeCoreModules != core.TSTrue && !file.IsDeclarationFile {
					if strings.HasPrefix(moduleName, "node:") && !core.ExclusivelyPrefixedNodeCoreModules[moduleName] {
						// Presence of `node:` prefix takes precedence over unprefixed node core modules
						file.UsesUriStyleNodeCoreModules = core.TSTrue
					} else if file.UsesUriStyleNodeCoreModules == core.TSUnknown && core.UnprefixedNodeCoreModules[moduleName] {
						// Avoid `unprefixedNodeCoreModules.has` for every import
						file.UsesUriStyleNodeCoreModules = core.TSFalse
					}
				}
			}
		}
		return
	}
	if store.IsModuleDeclaration(node) && store.IsAmbientModule(node) && (inAmbientModule || store.HasSyntacticModifier(node, ast.ModifierFlagsAmbient) || file.IsDeclarationFile) {
		nameText := node.AsModuleDeclaration().Name().Text()
		// Ambient module declarations can be interpreted as augmentations for some existing external modules.
		// This will happen in two cases:
		// - if current file is external module then module augmentation is a ambient module declaration defined in the top level scope
		// - if current file is not external module then module augmentation is an ambient module declaration with non-relative module name
		//   immediately nested in top level ambient module declaration .
		if file.IsExternalModule() || (inAmbientModule && !tspath.IsExternalModuleNameRelative(nameText)) {
			file.ModuleAugmentations = append(file.ModuleAugmentations, node.AsModuleDeclaration().Name().Ref())
		} else if !inAmbientModule {
			file.AmbientModuleNames = append(file.AmbientModuleNames, nameText)
			// An AmbientExternalModuleDeclaration declares an external module.
			// This type of declaration is permitted only in the global module.
			// The StringLiteral must specify a top - level external module name.
			// Relative external module names are not permitted
			// NOTE: body of ambient module is always a module block, if it exists
			if !node.Body().IsNil() {
				for _, statement := range node.Body().Statements().Refs() {
					collectModuleReferences(file, file.Store.Node(statement), true /*inAmbientModule*/)
				}
			}
		}
	}
}

// findImportOrRequire and forEachDynamicImportOrRequireCall are
// ast.ForEachDynamicImportOrRequireCall's text scan and its descent, with
// store.GetNodeAtPosition (no JSDoc) in place of ast.GetNodeAtPosition.

func findImportOrRequire(text string, start int) (index int, size int) {
	index = max(start, 0)
	n := len(text)
	for index < n {
		next := strings.IndexAny(text[index:], "ir")
		if next < 0 {
			break
		}
		index += next

		var expected string
		if text[index] == 'i' {
			size = 6
			expected = "import"
		} else {
			size = 7
			expected = "require"
		}
		if index+size <= n && text[index:index+size] == expected {
			return index, size
		}
		index++
	}

	return -1, 0
}

func forEachDynamicImportOrRequireCall(
	file *store.File,
	includeTypeSpaceImports bool,
	requireStringLiteralLikeArgument bool,
	cb func(node store.Node, argument store.Node) bool,
) bool {
	isJavaScriptFile := store.IsInJSFile(file.Root())
	lastIndex, size := findImportOrRequire(file.Text, 0)
	for lastIndex >= 0 {
		node := store.GetNodeAtPosition(file, lastIndex)
		if isJavaScriptFile && store.IsRequireCall(node, requireStringLiteralLikeArgument) {
			if cb(node, node.Arguments().At(0)) {
				return true
			}
		} else if store.IsImportCall(node) && node.Arguments().Len() > 0 && (!requireStringLiteralLikeArgument || store.IsStringLiteralLike(node.Arguments().At(0))) {
			if cb(node, node.Arguments().At(0)) {
				return true
			}
		} else if includeTypeSpaceImports && store.IsLiteralImportTypeNode(node) {
			if cb(node, node.AsImportTypeNode().Argument().AsLiteralTypeNode().Literal()) {
				return true
			}
		}
		// skip past import/require
		lastIndex += size
		lastIndex, size = findImportOrRequire(file.Text, lastIndex)
	}
	return false
}

// The external module indicator, from internal/ast/parseoptions.go. It reads
// the scratch through Builder.View, before Finish: the top-level await reparse
// that depends on it rewrites the tree, so it cannot wait for the Store.

func (p *Parser) setExternalModuleIndicator(file *store.File, root store.NodeRef, opts ast.ExternalModuleIndicatorOptions) {
	file.ExternalModuleIndicator = p.getExternalModuleIndicator(file, root, opts)
}

func (p *Parser) getExternalModuleIndicator(file *store.File, root store.NodeRef, opts ast.ExternalModuleIndicatorOptions) store.NodeRef {
	if file.ScriptKind == core.ScriptKindJSON {
		return store.NoNodeRef
	}

	if node := p.isFileProbablyExternalModule(root); node != store.NoNodeRef {
		return node
	}

	if file.IsDeclarationFile {
		return store.NoNodeRef
	}

	if opts.JSX {
		if node := p.isFileModuleFromUsingJSXTag(root); node != store.NoNodeRef {
			return node
		}
	}

	if opts.Force {
		return root
	}

	return store.NoNodeRef
}

func (p *Parser) isFileProbablyExternalModule(sourceFile store.NodeRef) store.NodeRef {
	for _, statement := range p.b.View(sourceFile).AsSourceFile().Statements().Refs() {
		if isAnExternalModuleIndicatorNode(p.b.View(statement)) {
			return statement
		}
	}
	return p.getImportMetaIfNecessary(sourceFile)
}

func isAnExternalModuleIndicatorNode(node store.Node) bool {
	return store.HasSyntacticModifier(node, ast.ModifierFlagsExport) ||
		store.IsImportEqualsDeclaration(node) && store.IsExternalModuleReference(node.AsImportEqualsDeclaration().ModuleReference()) ||
		store.IsImportDeclaration(node) || store.IsExportAssignment(node) || store.IsExportDeclaration(node)
}

func (p *Parser) getImportMetaIfNecessary(sourceFile store.NodeRef) store.NodeRef {
	if p.b.View(sourceFile).Flags()&ast.NodeFlagsPossiblyContainsImportMeta != 0 {
		return findChildNode(p.b.View(sourceFile), store.IsImportMeta)
	}
	return store.NoNodeRef
}

func findChildNode(root store.Node, check func(store.Node) bool) store.NodeRef {
	var result store.NodeRef
	var visit func(store.Node) bool
	visit = func(node store.Node) bool {
		if check(node) {
			result = node.Ref()
			return true
		}
		return node.ForEachChild(visit)
	}
	visit(root)
	return result
}

func (p *Parser) isFileModuleFromUsingJSXTag(file store.NodeRef) store.NodeRef {
	return walkTreeForJSXTags(p.b.View(file))
}

// This is a somewhat unavoidable full tree walk to locate a JSX tag - `import.meta` requires the same,
// but we avoid that walk (or parts of it) if at all possible using the `PossiblyContainsImportMeta` node flag.
// Unfortunately, there's no `NodeFlag` space to do the same for JSX.
//
// The Pointer version prunes with SubtreeFacts (SubtreeContainsJsx); the Store
// has no subtree facts, so this walks the whole tree. It runs only for a TSX
// file with opts.JSX and no import or export statement.
func walkTreeForJSXTags(node store.Node) store.NodeRef {
	var found store.NodeRef

	var visitor func(node store.Node) bool
	visitor = func(node store.Node) bool {
		if found != store.NoNodeRef {
			return true
		}
		if store.IsJsxOpeningLikeElement(node) || store.IsJsxFragment(node) {
			found = node.Ref()
			return true
		}
		return node.ForEachChild(visitor)
	}
	visitor(node)

	return found
}
