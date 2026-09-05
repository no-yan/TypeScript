package declarations

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
)

type SymbolTrackerImpl struct {
	resolver                    printer.EmitResolver
	state                       *SymbolTrackerSharedState
	host                        DeclarationEmitHost
	fallbackStack               []ast.Handle
	watchedClassSymbol          *ast.Symbol
	classSymbolTracked          bool
	getIsolatedDeclarationError func(node ast.Handle) *ast.Diagnostic
}

func (s *SymbolTrackerImpl) PopErrorFallbackNode() {
	s.fallbackStack = s.fallbackStack[:len(s.fallbackStack)-1]
}

func (s *SymbolTrackerImpl) PushErrorFallbackNode(node ast.Handle) {
	s.fallbackStack = append(s.fallbackStack, node)
}

func (s *SymbolTrackerImpl) ReportCyclicStructureError() {
	location := s.errorLocation()
	if !location.IsNil() {
		s.state.addDiagnostic(createDiagnosticForNode(location, diagnostics.The_inferred_type_of_0_references_a_type_with_a_cyclic_structure_which_cannot_be_trivially_serialized_A_type_annotation_is_necessary, s.errorDeclarationNameWithFallback()))
	}
}

func (s *SymbolTrackerImpl) ReportInaccessibleThisError() {
	location := s.errorLocation()
	if !location.IsNil() {
		s.state.addDiagnostic(createDiagnosticForNode(location, diagnostics.The_inferred_type_of_0_references_an_inaccessible_1_type_A_type_annotation_is_necessary, s.errorDeclarationNameWithFallback(), "this"))
	}
}

func (s *SymbolTrackerImpl) ReportInaccessibleUniqueSymbolError() {
	location := s.errorLocation()
	if !location.IsNil() {
		s.state.addDiagnostic(createDiagnosticForNode(location, diagnostics.The_inferred_type_of_0_references_an_inaccessible_1_type_A_type_annotation_is_necessary, s.errorDeclarationNameWithFallback(), "unique symbol"))
	}
}
func (s *SymbolTrackerImpl) isBoundExpando(node ast.Handle) bool {
	if !(ast.IsExpandoPropertyDeclaration(node) && ast.IsPropertyAccessExpression(node.BinaryExpressionLeft())) {
		return false
	}
	ref := s.resolver.GetReferencedValueDeclarationUnsafe(ast.GetLeftmostExpression(node.BinaryExpressionLeft(), true))
	if ref.IsNil() {
		return false
	}
	return s.resolver.IsExpandoFunctionDeclarationUnsafe(ref)
}
func (s *SymbolTrackerImpl) isChildOfBoundExpando(node ast.Handle) bool {
	return !ast.FindAncestorOrQuit(node, func(n ast.Handle) ast.FindAncestorResult {
		if ast.IsSourceFile(n) || ast.IsBlock(n) {
			return ast.FindAncestorQuit
		}
		return ast.ToFindAncestorResult(s.isBoundExpando(n))
	}).IsNil()
}

func (s *SymbolTrackerImpl) ReportInferenceFallback(node ast.Handle) {
	if !s.state.isolatedDeclarations {
		return
	}
	if ast.GetSourceFileOfNode(node) != s.state.currentSourceFile {
		return
	}
	if s.state.resolver.IsExpandoFunctionDeclarationUnsafe(node) {
		s.state.reportExpandoFunctionErrors(node)
	}
	if !s.isChildOfBoundExpando(node) {
		s.state.addDiagnostic(s.getIsolatedDeclarationError(node))
	}
}

func (s *SymbolTrackerImpl) ReportLikelyUnsafeImportRequiredError(specifier string, symbolName string) {
	location := s.errorLocation()
	if !location.IsNil() {
		if symbolName != "" {
			s.state.addDiagnostic(createDiagnosticForNode(location, diagnostics.The_inferred_type_of_0_cannot_be_named_without_a_reference_to_2_from_1_This_is_likely_not_portable_A_type_annotation_is_necessary, s.errorDeclarationNameWithFallback(), specifier, symbolName))
		} else {
			s.state.addDiagnostic(createDiagnosticForNode(location, diagnostics.The_inferred_type_of_0_cannot_be_named_without_a_reference_to_1_This_is_likely_not_portable_A_type_annotation_is_necessary, s.errorDeclarationNameWithFallback(), specifier))
		}
	}
}

func (s *SymbolTrackerImpl) ReportNonSerializableProperty(propertyName string) {
	location := s.errorLocation()
	if !location.IsNil() {
		s.state.addDiagnostic(createDiagnosticForNode(location, diagnostics.The_type_of_this_node_cannot_be_serialized_because_its_property_0_cannot_be_serialized, propertyName))
	}
}

func (s *SymbolTrackerImpl) ReportNonlocalAugmentation(containingFile *ast.SourceFile, parentSymbol *ast.Symbol, augmentingSymbol *ast.Symbol) {
	primaryDeclaration := ast.FindSymbolDeclaration(parentSymbol, func(d ast.Handle) bool {
		return ast.GetSourceFileOfNode(d) == containingFile
	})
	if primaryDeclaration.IsNil() {
		return
	}
	for _, augmentations := range ast.DeclarationNodes(augmentingSymbol).All() {
		if ast.GetSourceFileOfNode(augmentations) == containingFile {
			continue
		}
		diag := createDiagnosticForNode(augmentations, diagnostics.Declaration_augments_declaration_in_another_file_This_cannot_be_serialized)
		related := createDiagnosticForNode(primaryDeclaration, diagnostics.This_is_the_declaration_being_augmented_Consider_moving_the_augmenting_declaration_into_the_same_file)
		diag.AddRelatedInfo(related)
		s.state.addDiagnostic(diag)
	}
}

func (s *SymbolTrackerImpl) ReportPrivateInBaseOfClassExpression(propertyName string) {
	location := s.errorLocation()
	if !location.IsNil() {
		diag := createDiagnosticForNode(location, diagnostics.Property_0_of_exported_anonymous_class_type_may_not_be_private_or_protected, propertyName)
		if ast.IsVariableDeclaration(location.Parent()) {
			related := createDiagnosticForNode(location, diagnostics.Add_a_type_annotation_to_the_variable_0, s.errorDeclarationNameWithFallback())
			diag.AddRelatedInfo(related)
		}
		s.state.addDiagnostic(diag)
	}
}

func (s *SymbolTrackerImpl) ReportTruncationError() {
	location := s.errorLocation()
	if !location.IsNil() {
		s.state.addDiagnostic(createDiagnosticForNode(location, diagnostics.The_inferred_type_of_this_node_exceeds_the_maximum_length_the_compiler_will_serialize_An_explicit_type_annotation_is_needed))
	}
}
func (s *SymbolTrackerImpl) errorFallbackNode() ast.Handle {
	if len(s.fallbackStack) >= 1 {
		return s.fallbackStack[len(s.fallbackStack)-1]
	}
	return ast.Handle{}
}
func (s *SymbolTrackerImpl) errorLocation() ast.Handle {
	location := s.state.errorNameNode
	if location.IsNil() {
		location = s.errorFallbackNode()
	}
	return location
}
func (s *SymbolTrackerImpl) errorDeclarationNameWithFallback() string {
	if !s.state.errorNameNode.IsNil() {
		return scanner.DeclarationNameToString(s.state.errorNameNode)
	}
	if !s.errorFallbackNode().IsNil() && !ast.GetNameOfDeclaration(s.errorFallbackNode()).IsNil() {
		return scanner.DeclarationNameToString(ast.GetNameOfDeclaration(s.errorFallbackNode()))
	}
	if !s.errorFallbackNode().IsNil() && ast.IsExportAssignment(s.errorFallbackNode()) {
		if s.errorFallbackNode().ExportAssignmentIsExportEquals() {
			return "export="
		}
		return "default"
	}
	return "(Missing)"
}

func (s *SymbolTrackerImpl) TrackSymbol(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags) bool {
	if symbol.Flags&ast.SymbolFlagsTypeParameter != 0 {
		return false
	}
	if s.watchedClassSymbol != nil && symbol == s.watchedClassSymbol {
		s.classSymbolTracked = true
		return false
	}
	issuedDiagnostic := s.handleSymbolAccessibilityError(s.resolver.IsSymbolAccessible(symbol, enclosingDeclaration, meaning, true))
	return issuedDiagnostic
}
func (s *SymbolTrackerImpl) handleSymbolAccessibilityError(symbolAccessibilityResult printer.SymbolAccessibilityResult) bool {
	if symbolAccessibilityResult.Accessibility == printer.SymbolAccessibilityAccessible {
		if len(symbolAccessibilityResult.AliasesToMakeVisible) > 0 {
			for _, ref := range symbolAccessibilityResult.AliasesToMakeVisible {
				s.state.lateMarkedStatements = core.AppendIfUnique(s.state.lateMarkedStatements, ref)
			}
		}
	} else if symbolAccessibilityResult.Accessibility != printer.SymbolAccessibilityNotResolved {
		errorInfo := s.state.getSymbolAccessibilityDiagnostic(symbolAccessibilityResult)
		if errorInfo != nil {
			info := *errorInfo
			diagNode := symbolAccessibilityResult.ErrorNode
			if diagNode.IsNil() {
				diagNode = errorInfo.errorNode
			}
			if !info.typeName.IsNil() {
				s.state.addDiagnostic(createDiagnosticForNode(diagNode, info.diagnosticMessage, scanner.GetTextOfNode(info.typeName), symbolAccessibilityResult.ErrorSymbolName, symbolAccessibilityResult.ErrorModuleName))
			} else {
				s.state.addDiagnostic(createDiagnosticForNode(diagNode, info.diagnosticMessage, symbolAccessibilityResult.ErrorSymbolName, symbolAccessibilityResult.ErrorModuleName))
			}
			return true
		}
	}
	return false
}
func createDiagnosticForNode(node ast.Handle, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	return checker.NewDiagnosticForNode(node, message, args...)
}

type SymbolTrackerSharedState struct {
	lateMarkedStatements             []ast.Handle
	diagnostics                      []*ast.Diagnostic
	getSymbolAccessibilityDiagnostic GetSymbolAccessibilityDiagnostic
	errorNameNode                    ast.Handle
	isolatedDeclarations             bool
	stripInternal                    bool
	currentSourceFile                *ast.SourceFile
	resolver                         printer.EmitResolver
	reportExpandoFunctionErrors      func(node ast.Handle)
}

func (s *SymbolTrackerSharedState) addDiagnostic(diag *ast.Diagnostic) {
	s.diagnostics = append(s.diagnostics, diag)
}
func NewSymbolTracker(host DeclarationEmitHost, resolver printer.EmitResolver, state *SymbolTrackerSharedState) *SymbolTrackerImpl {
	tracker := &SymbolTrackerImpl{host: host, resolver: resolver, state: state, getIsolatedDeclarationError: createGetIsolatedDeclarationErrors(resolver)}
	return tracker
}
