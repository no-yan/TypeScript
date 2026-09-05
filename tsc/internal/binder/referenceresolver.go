package binder

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
)

type ReferenceResolver interface {
	GetReferencedExportContainer(node /*nameNotFoundMessage*/ /*isUse*/ /*excludeGlobals*/ /*nameNotFoundMessage*/ /*isUse*/ /*excludeGlobals*/ /*SourceFile|ModuleDeclaration|EnumDeclaration*/ ast.Handle, prefixLocals bool) ast.Handle
	GetReferencedImportDeclaration(node ast.Handle) ast.Handle
	GetReferencedValueDeclaration(node ast.Handle) ast.Handle
	GetReferencedValueDeclarations(node ast.Handle) []ast.Handle
	GetElementAccessExpressionName(expression ast.Handle) string
	GetReferencedMemberValueDeclaration(node ast.Handle) ast.Handle
}
type ReferenceResolverHooks struct {
	ResolveName                            func(location ast.Handle, name string, meaning ast.SymbolFlags, nameNotFoundMessage *diagnostics.Message, isUse bool, excludeGlobals bool) *ast.Symbol
	GetResolvedSymbol                      func(ast.Handle) *ast.Symbol
	GetMergedSymbol                        func(*ast.Symbol) *ast.Symbol
	GetParentOfSymbol                      func(*ast.Symbol) *ast.Symbol
	GetSymbolOfDeclaration                 func(ast.Handle) *ast.Symbol
	GetTypeOnlyAliasDeclaration            func(symbol *ast.Symbol, include ast.SymbolFlags) ast.Handle
	GetExportSymbolOfValueSymbolIfExported func(*ast.Symbol) *ast.Symbol
	GetElementAccessExpressionName         func(ast.Handle) (string, bool)
}

var _ ReferenceResolver = &referenceResolver{}

type referenceResolver struct {
	resolver *NameResolver
	options  *core.CompilerOptions
	hooks    ReferenceResolverHooks
}

func NewReferenceResolver(options *core.CompilerOptions, hooks ReferenceResolverHooks) ReferenceResolver {
	return &referenceResolver{options: options, hooks: hooks}
}
func (r *referenceResolver) getResolvedSymbol(node ast.Handle) *ast.Symbol {
	if !node.IsNil() {
		if r.hooks.GetResolvedSymbol != nil {
			return r.hooks.GetResolvedSymbol(node)
		}
	}
	return nil
}
func (r *referenceResolver) getMergedSymbol(symbol *ast.Symbol) *ast.Symbol {
	if symbol != nil {
		if r.hooks.GetMergedSymbol != nil {
			return r.hooks.GetMergedSymbol(symbol)
		}
		return symbol
	}
	return nil
}
func (r *referenceResolver) getParentOfSymbol(symbol *ast.Symbol) *ast.Symbol {
	if symbol != nil {
		if r.hooks.GetParentOfSymbol != nil {
			return r.hooks.GetParentOfSymbol(symbol)
		}
		return symbol.Parent
	}
	return nil
}
func (r *referenceResolver) getSymbolOfDeclaration(declaration ast.Handle) *ast.Symbol {
	if !declaration.IsNil() {
		if r.hooks.GetSymbolOfDeclaration != nil {
			return r.hooks.GetSymbolOfDeclaration(declaration)
		}
		return declaration.Symbol()
	}
	return nil
}
func (r *referenceResolver) getReferencedValueSymbol(reference ast.Handle, startInDeclarationContainer bool) *ast.Symbol {
	resolvedSymbol := r.getResolvedSymbol(reference)
	if resolvedSymbol != nil {
		return resolvedSymbol
	}
	location := reference
	if startInDeclarationContainer && !reference.Parent().IsNil() && ast.IsDeclaration(reference.Parent()) && reference.Parent().Name() == reference {
		location = ast.GetDeclarationContainer(reference.Parent())
	}
	if r.hooks.ResolveName != nil {
		return r.hooks.ResolveName(location, reference.Text(), ast.SymbolFlagsExportValue|ast.SymbolFlagsValue|ast.SymbolFlagsAlias, nil, false, false)
	}
	if r.resolver == nil {
		r.resolver = &NameResolver{CompilerOptions: r.options}
	}
	return r.resolver.Resolve(location, reference.Text(), ast.SymbolFlagsExportValue|ast.SymbolFlagsValue|ast.SymbolFlagsAlias, nil, false, false)
}
func (r *referenceResolver) isTypeOnlyAliasDeclaration(symbol *ast.Symbol) bool {
	if symbol != nil {
		if r.hooks.GetTypeOnlyAliasDeclaration != nil {
			return !r.hooks.GetTypeOnlyAliasDeclaration(symbol, ast.SymbolFlagsValue).IsNil()
		}
		node := r.getDeclarationOfAliasSymbol(symbol)
		for !node.IsNil() {
			switch node.Kind() {
			case ast.KindImportEqualsDeclaration, ast.KindExportDeclaration:
				return node.IsTypeOnly()
			case ast.KindImportClause, ast.KindImportSpecifier, ast.KindExportSpecifier:
				if node.IsTypeOnly() {
					return true
				}
				node = node.Parent()
				continue
			case ast.KindNamedImports, ast.KindNamedExports:
				node = node.Parent()
				continue
			}
			break
		}
	}
	return false
}
func (r *referenceResolver) getDeclarationOfAliasSymbol(symbol *ast.Symbol) ast.Handle {
	return ast.FindLastSymbolDeclaration(symbol, ast.IsAliasSymbolDeclaration)
}
func (r *referenceResolver) getExportSymbolOfValueSymbolIfExported(symbol *ast.Symbol) *ast.Symbol {
	if symbol != nil {
		if r.hooks.GetExportSymbolOfValueSymbolIfExported != nil {
			return r.hooks.GetExportSymbolOfValueSymbolIfExported(symbol)
		}
		if symbol.Flags&ast.SymbolFlagsExportValue != 0 && symbol.ExportSymbol != nil {
			symbol = symbol.ExportSymbol
		}
		return r.getMergedSymbol(symbol)
	}
	return nil
}
func (r *referenceResolver) GetReferencedExportContainer(node ast.Handle, prefixLocals bool) ast.Handle {
	startInDeclarationContainer := !node.Parent().IsNil() && (node.Parent().Kind() == ast.KindModuleDeclaration || node.Parent().Kind() == ast.KindEnumDeclaration) && node == node.Parent().Name()
	if symbol := r.getReferencedValueSymbol(node, startInDeclarationContainer); symbol != nil {
		if symbol.Flags&ast.SymbolFlagsExportValue != 0 {
			exportSymbol := r.getMergedSymbol(symbol.ExportSymbol)
			if !prefixLocals && exportSymbol.Flags&ast.SymbolFlagsExportHasLocal != 0 && exportSymbol.Flags&ast.SymbolFlagsVariable == 0 {
				return ast.Handle{}
			}
			symbol = exportSymbol
		}
		parentSymbol := r.getParentOfSymbol(symbol)
		if parentSymbol != nil {
			if parentSymbol.Flags&ast.SymbolFlagsValueModule != 0 {
				valueDecl := ast.NodeOf(parentSymbol.ValueDeclaration)
				if !valueDecl.IsNil() && valueDecl.Kind() == ast.KindSourceFile {
					symbolFile := ast.GetSourceFileOfNode(valueDecl)
					referenceFile := ast.GetSourceFileOfNode(node)
					symbolIsUmdExport := symbolFile != referenceFile
					if symbolIsUmdExport {
						return ast.Handle{}
					}
					return valueDecl
				}
			}
			isMatchingContainer := func(n ast.Handle) bool {
				return (n.Kind() == ast.KindModuleDeclaration || n.Kind() == ast.KindEnumDeclaration) && r.getSymbolOfDeclaration(n) == parentSymbol
			}
			return ast.FindAncestor(node.Parent(), isMatchingContainer)
		}
	}
	return ast.Handle{}
}
func (r *referenceResolver) GetReferencedImportDeclaration(node ast.Handle) ast.Handle {
	if symbol := r.getReferencedValueSymbol(node, false); symbol != nil {
		if ast.IsNonLocalAlias(symbol, ast.SymbolFlagsValue) && !r.isTypeOnlyAliasDeclaration(symbol) {
			return r.getDeclarationOfAliasSymbol(symbol)
		}
	}
	return ast.Handle{}
}
func (r *referenceResolver) GetReferencedValueDeclaration(node ast.Handle) ast.Handle {
	if symbol := r.getReferencedValueSymbol(node, false); symbol != nil {
		return ast.NodeOf(r.getExportSymbolOfValueSymbolIfExported(symbol).ValueDeclaration)
	}
	return ast.Handle{}
}
func (r *referenceResolver) GetReferencedValueDeclarations(node ast.Handle) []ast.Handle {
	var declarations []ast.Handle
	if symbol := r.getReferencedValueSymbol(node, false); symbol != nil {
		symbol = r.getExportSymbolOfValueSymbolIfExported(symbol)
		for _, declarationRef := range symbol.Declarations {
			declaration := ast.NodeOf(declarationRef)
			if declaration.IsNil() {
				continue
			}
			switch declaration.Kind() {
			case ast.KindVariableDeclaration, ast.KindParameter, ast.KindBindingElement, ast.KindPropertyDeclaration, ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment, ast.KindEnumMember, ast.KindObjectLiteralExpression, ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindClassDeclaration, ast.KindClassExpression, ast.KindEnumDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindModuleDeclaration:
				declarations = append(declarations, declaration)
			}
		}
	}
	return declarations
}
func (r *referenceResolver) GetElementAccessExpressionName(expression ast.Handle) string {
	if !expression.IsNil() {
		if r.hooks.GetElementAccessExpressionName != nil {
			if name, ok := r.hooks.GetElementAccessExpressionName(expression); ok {
				return name
			}
		}
	}
	return ""
}
func (r *referenceResolver) GetReferencedMemberValueDeclaration(node ast.Handle) ast.Handle {
	s := r.getResolvedSymbol(node)
	if s == nil && node.Symbol() != nil {
		s = r.getMergedSymbol(node.Symbol())
	}
	if s == nil {
		return ast.Handle{}
	}
	return ast.NodeOf(r.getExportSymbolOfValueSymbolIfExported(s).ValueDeclaration)
}
