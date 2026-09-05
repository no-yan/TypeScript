package checker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"strings"
)

func createPrinterWithDefaults(emitContext *printer.EmitContext) *printer.Printer {
	return printer.NewPrinter(printer.PrinterOptions{}, printer.PrintHandlers{}, emitContext)
}
func createPrinterWithRemoveComments(emitContext *printer.EmitContext) *printer.Printer {
	return printer.NewPrinter(printer.PrinterOptions{RemoveComments: true}, printer.PrintHandlers{}, emitContext)
}
func createPrinterWithRemoveCommentsOmitTrailingSemicolon(emitContext *printer.EmitContext) *printer.Printer {
	return printer.NewPrinter(printer.PrinterOptions{RemoveComments: true, OmitTrailingSemicolon: true}, printer.PrintHandlers{}, emitContext)
}
func createPrinterWithRemoveCommentsOmitTrailingSemicolonNeverAsciiEscape(emitContext *printer.EmitContext) *printer.Printer {
	return printer.NewPrinter(printer.PrinterOptions{RemoveComments: true, OmitTrailingSemicolon: true, NeverAsciiEscape: true}, printer.PrintHandlers{}, emitContext)
}
func createPrinterWithRemoveCommentsNeverAsciiEscape(emitContext *printer.EmitContext) *printer.Printer {
	return printer.NewPrinter(printer.PrinterOptions{RemoveComments: true, NeverAsciiEscape: true}, printer.PrintHandlers{}, emitContext)
}
func (c *Checker) TypeToString(t *Type) string {
	return c.typeToString(t, // TODO: Memoize once per checker to retain threadsafety
		// Serialization of types can lead to (lazy) resolution of members, which can cause diagnostics that again require
		// serialization of types. This can potentially result in infinite recursion and stack overflows. To prevent that,
		// after a certain number of recursive invocations the function simply returns "?".
		// The unresolved type gets a synthesized comment on `any` to hint to users that it's not a plain `any`.
		// Otherwise, we always strip comments out.
		// hard cutoff matching Strada's absoluteMaximumLength
		// add neverAsciiEscape for GH#39027
		// TODO: GH#18217
		/*sourceFile*/ // TODO: GH#18217
		// TODO: GH#18217
		/*sourceFile*/ // ExpandSymbolForHover produces declaration strings for a symbol with verbosity support for expandable hover.
		// TypeParameterToStringEx renders a type parameter declaration (e.g. "T extends Foo") with optional verbosity support.
		ast.Handle{})
}
func (c *Checker) typeToString(t *Type, enclosingDeclaration ast.Handle) string {
	return c.typeToStringEx(t, enclosingDeclaration, TypeFormatFlagsAllowUniqueESSymbolType|TypeFormatFlagsUseAliasDefinedOutsideCurrentScope, nil)
}
func toNodeBuilderFlags(flags TypeFormatFlags) nodebuilder.Flags {
	return nodebuilder.Flags(flags & TypeFormatFlagsNodeBuilderFlagsMask)
}
func (c *Checker) TypeToStringEx(t *Type, enclosingDeclaration ast.Handle, flags TypeFormatFlags, vc *VerbosityContext) string {
	return c.typeToStringEx(t, enclosingDeclaration, flags, vc)
}
func (c *Checker) typeToStringEx(t *Type, enclosingDeclaration ast.Handle, flags TypeFormatFlags, vc *VerbosityContext) string {
	if c.serializationLevel >= maxSerializationLevel {
		return "?"
	}
	newLine := ""
	if flags&TypeFormatFlagsMultilineObjectLiterals != 0 {
		newLine = "\n"
	}
	writer := printer.NewTextWriter(newLine, 0)
	noTruncation := ((vc == nil || vc.MaxTruncationLength == 0) && c.compilerOptions.NoErrorTruncation == core.TSTrue) || (flags&TypeFormatFlagsNoTruncation != 0)
	combinedFlags := toNodeBuilderFlags(flags) | nodebuilder.FlagsIgnoreErrors
	if noTruncation {
		combinedFlags = combinedFlags | nodebuilder.FlagsNoTruncation
	}
	nodeBuilder, release := c.getNodeBuilder()
	defer release()
	oldVerbosity := nodeBuilder.verbosity
	nodeBuilder.verbosity = vc
	defer func() {
		nodeBuilder.verbosity = oldVerbosity
	}()
	c.serializationLevel++
	typeNode := nodeBuilder.TypeToTypeNode(t, enclosingDeclaration, combinedFlags, nodebuilder.InternalFlagsNone, nil)
	c.serializationLevel--
	if typeNode.IsNil() {
		panic("should always get typenode")
	}
	var p *printer.Printer
	if t == c.unresolvedType {
		p = createPrinterWithDefaults(nodeBuilder.EmitContext())
	} else {
		p = createPrinterWithRemoveComments(nodeBuilder.EmitContext())
	}
	var sourceFile *ast.SourceFile
	if !enclosingDeclaration.IsNil() {
		sourceFile = ast.GetSourceFileOfNode(enclosingDeclaration)
	}
	p.Write(typeNode, sourceFile, writer, nil)
	result := writer.String()
	maxLength := defaultMaximumTruncationLength * 2
	if vc != nil && vc.MaxTruncationLength > 0 {
		maxLength = vc.MaxTruncationLength * 10
	}
	if noTruncation {
		maxLength = noTruncationMaximumTruncationLength * 2
	}
	if maxLength > 0 && result != "" && len(result) >= maxLength {
		if vc != nil {
			vc.Truncated = true
		}
		return result[0:maxLength-len("...")] + "..."
	}
	return result
}
func (c *Checker) SymbolToString(s *ast.Symbol) string {
	return c.symbolToString(s)
}
func (c *Checker) symbolToString(symbol *ast.Symbol) string {
	return c.symbolToStringEx(symbol, ast.Handle{}, ast.SymbolFlagsAll, SymbolFormatFlagsAllowAnyNodeKind)
}
func (c *Checker) SymbolToStringEx(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags, flags SymbolFormatFlags) string {
	return c.symbolToStringEx(symbol, enclosingDeclaration, meaning, flags)
}
func (c *Checker) symbolToStringEx(symbol *ast.Symbol, enclosingDeclaration ast.Handle, meaning ast.SymbolFlags, flags SymbolFormatFlags) string {
	writer, putWriter := printer.GetSingleLineStringWriter()
	defer putWriter()
	nodeFlags := nodebuilder.FlagsIgnoreErrors
	internalNodeFlags := nodebuilder.InternalFlagsNone
	if flags&SymbolFormatFlagsUseOnlyExternalAliasing != 0 {
		nodeFlags |= nodebuilder.FlagsUseOnlyExternalAliasing
	}
	if flags&SymbolFormatFlagsWriteTypeParametersOrArguments != 0 {
		nodeFlags |= nodebuilder.FlagsWriteTypeParametersInQualifiedName
	}
	if flags&SymbolFormatFlagsUseAliasDefinedOutsideCurrentScope != 0 {
		nodeFlags |= nodebuilder.FlagsUseAliasDefinedOutsideCurrentScope
	}
	if flags&SymbolFormatFlagsDoNotIncludeSymbolChain != 0 {
		internalNodeFlags |= nodebuilder.InternalFlagsDoNotIncludeSymbolChain
	}
	if flags&SymbolFormatFlagsWriteComputedProps != 0 {
		internalNodeFlags |= nodebuilder.InternalFlagsWriteComputedProps
	}
	nodeBuilder, release := c.getNodeBuilder()
	defer release()
	var sourceFile *ast.SourceFile
	if !enclosingDeclaration.IsNil() {
		sourceFile = ast.GetSourceFileOfNode(enclosingDeclaration)
	}
	var printer_ *printer.Printer
	if !enclosingDeclaration.IsNil() && enclosingDeclaration.Kind == ast.KindSourceFile {
		printer_ = createPrinterWithRemoveCommentsOmitTrailingSemicolonNeverAsciiEscape(nodeBuilder.EmitContext())
	} else {
		printer_ = createPrinterWithRemoveCommentsOmitTrailingSemicolon(nodeBuilder.EmitContext())
	}
	var builder func(symbol *ast.Symbol, meaning ast.SymbolFlags, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, tracker nodebuilder.SymbolTracker) ast.Handle
	if flags&SymbolFormatFlagsAllowAnyNodeKind != 0 {
		builder = nodeBuilder.SymbolToNode
	} else {
		builder = nodeBuilder.SymbolToEntityName
	}
	entity := builder(symbol, meaning, enclosingDeclaration, nodeFlags, internalNodeFlags, nil)
	printer_.Write(entity, sourceFile, writer, nil)
	return writer.String()
}
func (c *Checker) signatureToString(signature *Signature) string {
	return c.signatureToStringEx(signature, ast.Handle{}, TypeFormatFlagsNone, nil)
}
func (c *Checker) SignatureToStringEx(signature *Signature, enclosingDeclaration ast.Handle, flags TypeFormatFlags, vc *VerbosityContext) string {
	return c.signatureToStringEx(signature, enclosingDeclaration, flags, vc)
}
func (c *Checker) signatureToStringEx(signature *Signature, enclosingDeclaration ast.Handle, flags TypeFormatFlags, vc *VerbosityContext) string {
	isConstructor := signature.flags&SignatureFlagsConstruct != 0 && flags&TypeFormatFlagsWriteCallStyleSignature == 0
	var sigOutput ast.Kind
	if flags&TypeFormatFlagsWriteArrowStyleSignature != 0 {
		if isConstructor {
			sigOutput = ast.KindConstructorType
		} else {
			sigOutput = ast.KindFunctionType
		}
	} else {
		if isConstructor {
			sigOutput = ast.KindConstructSignature
		} else {
			sigOutput = ast.KindCallSignature
		}
	}
	nodeBuilder, release := c.getNodeBuilder()
	defer release()
	oldVerbosity := nodeBuilder.verbosity
	nodeBuilder.verbosity = vc
	defer func() {
		nodeBuilder.verbosity = oldVerbosity
	}()
	combinedFlags := toNodeBuilderFlags(flags) | nodebuilder.FlagsIgnoreErrors | nodebuilder.FlagsWriteTypeParametersInQualifiedName
	sig := nodeBuilder.SignatureToSignatureDeclaration(signature, sigOutput, enclosingDeclaration, combinedFlags, nodebuilder.InternalFlagsNone, nil)
	p := createPrinterWithRemoveCommentsOmitTrailingSemicolonNeverAsciiEscape(nodeBuilder.EmitContext())
	var sourceFile *ast.SourceFile
	if !enclosingDeclaration.IsNil() {
		sourceFile = ast.GetSourceFileOfNode(enclosingDeclaration)
	}
	if flags&TypeFormatFlagsMultilineObjectLiterals != 0 {
		writer := printer.NewTextWriter("\n", 0)
		p.Write(sig, sourceFile, writer, nil)
		return writer.String()
	}
	writer, putWriter := printer.GetSingleLineStringWriter()
	defer putWriter()
	p.Write(sig, sourceFile, writer, nil)
	return writer.String()
}
func (c *Checker) typePredicateToString(typePredicate *TypePredicate) string {
	return c.typePredicateToStringEx(typePredicate, ast.Handle{}, TypeFormatFlagsUseAliasDefinedOutsideCurrentScope)
}
func (c *Checker) typePredicateToStringEx(typePredicate *TypePredicate, enclosingDeclaration ast.Handle, flags TypeFormatFlags) string {
	writer, putWriter := printer.GetSingleLineStringWriter()
	defer putWriter()
	nodeBuilder, release := c.getNodeBuilder()
	defer release()
	combinedFlags := toNodeBuilderFlags(flags) | nodebuilder.FlagsIgnoreErrors | nodebuilder.FlagsWriteTypeParametersInQualifiedName
	predicate := nodeBuilder.TypePredicateToTypePredicateNode(typePredicate, enclosingDeclaration, combinedFlags, nodebuilder.InternalFlagsNone, nil)
	printer_ := createPrinterWithRemoveComments(nodeBuilder.EmitContext())
	var sourceFile *ast.SourceFile
	if !enclosingDeclaration.IsNil() {
		sourceFile = ast.GetSourceFileOfNode(enclosingDeclaration)
	}
	printer_.Write(predicate, sourceFile, writer, nil)
	return writer.String()
}
func (c *Checker) valueToString(value any) string {
	return ValueToString(value)
}
func (c *Checker) formatUnionTypes(types []*Type, expandingEnum bool) []*Type {
	var result []*Type
	var flags TypeFlags
	for i := 0; i < len(types); i++ {
		t := types[i]
		flags |= t.flags
		if t.flags&TypeFlagsNullable == 0 {
			if t.flags&TypeFlagsBooleanLiteral != 0 || (!expandingEnum && t.flags&TypeFlagsEnumLike != 0) {
				var baseType *Type
				if t.flags&TypeFlagsBooleanLiteral != 0 {
					baseType = c.booleanType
				} else {
					baseType = c.getBaseTypeOfEnumLikeType(t)
				}
				if baseType.flags&TypeFlagsUnion != 0 {
					count := len(baseType.AsUnionType().types)
					if i+count <= len(types) && c.getRegularTypeOfLiteralType(types[i+count-1]) == c.getRegularTypeOfLiteralType(baseType.AsUnionType().types[count-1]) {
						result = append(result, baseType)
						i += count - 1
						continue
					}
				}
			}
			result = append(result, t)
		}
	}
	if flags&TypeFlagsNull != 0 {
		result = append(result, c.nullType)
	}
	if flags&TypeFlagsUndefined != 0 {
		result = append(result, c.undefinedType)
	}
	return result
}
func (c *Checker) TypeToTypeNode(t *Type, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, idToSymbol map[ast.Handle]*ast.Symbol) ast.Handle {
	nodeBuilder := c.getNodeBuilderEx(idToSymbol)
	return nodeBuilder.TypeToTypeNode(t, enclosingDeclaration, flags, nodebuilder.InternalFlagsNone, nil)
}
func (c *Checker) SignatureToSignatureDeclaration(signature *Signature, kind ast.Kind, enclosingDeclaration ast.Handle, flags nodebuilder.Flags) ast.Handle {
	nodeBuilder, release := c.getNodeBuilder()
	defer release()
	return nodeBuilder.SignatureToSignatureDeclaration(signature, kind, enclosingDeclaration, flags, nodebuilder.InternalFlagsNone, nil)
}

func (c *Checker) ExpandSymbolForHover(symbol *ast.Symbol, meaning ast.SymbolFlags, vc *VerbosityContext) string {
	nodeBuilder, release := c.getNodeBuilder()
	defer release()
	oldVerbosity := nodeBuilder.verbosity
	nodeBuilder.verbosity = vc
	defer func() {
		nodeBuilder.verbosity = oldVerbosity
	}()
	nodes := nodeBuilder.ExpandSymbolForHover(symbol, meaning)
	if len(nodes) == 0 {
		return ""
	}
	p := createPrinterWithRemoveComments(nodeBuilder.EmitContext())
	var sourceFile *ast.SourceFile
	if symbol.ValueDeclaration != 0 {
		sourceFile = ast.GetSourceFileOfNode(ast.NodeOf(symbol.ValueDeclaration))
	}
	var b strings.Builder
	for i, node := range nodes {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(p.Emit(node, sourceFile))
	}
	return b.String()
}

func (c *Checker) TypeParameterToStringEx(t *Type, enclosingDeclaration ast.Handle, vc *VerbosityContext) string {
	nodeBuilder, release := c.getNodeBuilder()
	defer release()
	oldVerbosity := nodeBuilder.verbosity
	nodeBuilder.verbosity = vc
	defer func() {
		nodeBuilder.verbosity = oldVerbosity
	}()
	typeParamNode := nodeBuilder.TypeParameterToDeclaration(t, enclosingDeclaration, nodebuilder.FlagsIgnoreErrors, nodebuilder.InternalFlagsNone, nil)
	if typeParamNode.IsNil() {
		return c.TypeToString(t)
	}
	p := createPrinterWithRemoveComments(nodeBuilder.EmitContext())
	var sourceFile *ast.SourceFile
	if !enclosingDeclaration.IsNil() {
		sourceFile = ast.GetSourceFileOfNode(enclosingDeclaration)
	}
	return p.Emit(typeParamNode, sourceFile)
}
func (c *Checker) TypeToTypeNodeEx(t *Type, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, internalFlags nodebuilder.InternalFlags, idToSymbol map[ast.Handle]*ast.Symbol) ast.Handle {
	nodeBuilder := c.getNodeBuilderEx(idToSymbol)
	return nodeBuilder.TypeToTypeNode(t, enclosingDeclaration, flags, internalFlags, nil)
}
func (c *Checker) TypePredicateToTypePredicateNode(t *TypePredicate, enclosingDeclaration ast.Handle, flags nodebuilder.Flags, idToSymbol map[ast.Handle]*ast.Symbol) ast.Handle {
	nodeBuilder := c.getNodeBuilderEx(idToSymbol)
	return nodeBuilder.TypePredicateToTypePredicateNode(t, enclosingDeclaration, flags, nodebuilder.InternalFlagsNone, nil)
}
