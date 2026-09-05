package ls

import (
	"context"
	"errors"
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/format"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
	"github.com/microsoft/TypeScript/tsc/internal/ls/autoimport"
	"github.com/microsoft/TypeScript/tsc/internal/ls/change"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsutil"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var ErrNeedsAutoImports = errors.New("completion list needs auto imports")

func (l *LanguageService) ProvideCompletion(ctx context.Context, documentURI lsproto.DocumentUri, LSPPosition lsproto.Position, context *lsproto.CompletionContext) (lsproto.CompletionResponse, error) {
	program, file := l.getProgramAndFile(documentURI)
	var triggerCharacter *string
	if context != nil {
		triggerCharacter = context.TriggerCharacter
	}
	ctx = format.WithFormatCodeSettings(ctx, l.FormatOptions(), l.FormatOptions().NewLineCharacter)
	positions := lsconv.FromLSPPositionForSourceFile(l.converters, file, LSPPosition, spanmap.FeatureCompletion)
	if len(positions) == 0 || !positions[0].Fidelity.IsExact() {
		return lsproto.CompletionItemsOrListOrNull{}, nil
	}
	file = positions[0].Script
	position := int(positions[0].Position)
	completionListInternal, err := l.getCompletionsAtPosition(ctx, file, position, triggerCharacter, false)
	if err != nil {
		return lsproto.CompletionItemsOrListOrNull{}, err
	}
	completionList := ensureItemData(file, position, completionListInternal.toLSP())
	if file.SpanMap() != nil {
		l.filterContentMappedAutoImports(ctx, program, file, completionList)
	}
	return lsproto.CompletionItemsOrListOrNull{List: completionList}, nil
}

func (l *LanguageService) filterContentMappedAutoImports(ctx context.Context, program *compiler.Program, file *ast.SourceFile, list *lsproto.CompletionList) {
	if list == nil {
		return
	}
	filtered := list.Items[: // In a content-mapped file the cursor is outside a verbatim span, so any completion committed here
	// could not be applied to the original text. Offer nothing rather than edits at a bogus location.
	/*includeSymbols*/ // filterContentMappedAutoImports eagerly resolves auto-import edits for a content-mapped file and drops any
	// completion whose import edit cannot be placed entirely within verbatim spans (it would otherwise insert
	// an import into synthesized virtual code with no counterpart in the original file). Surviving auto-imports carry
	// their additional edits directly so the client applies correct original-text positions on commit.
	// non-nil for symbol completions when IncludeSymbols is set; nil otherwise
	// *completionDataData | *completionDataKeyword | *completionDataJSDocTagName | *completionDataJSDocTag | *completionDataJSDocParameterName
	// Note that the presence of this alone doesn't mean that we need a conversion. Only do that if the completion is not an ordinary identifier.
	// In JSX tag name and attribute names, identifiers like "my-tag" or "aria-name" is valid identifier.
	// !!!
	// !!!
	// flags CompletionInfoFlags // !!!
	// TokenKind
	// If we're after the `=` sign but no identifier has been typed yet,
	// value will be `true` but initializer will be `nil`.
	// No keywords
	// Every possible kewyord
	// Keywords inside class body
	// Keywords inside interface body
	// Keywords at constructor parameter
	// Keywords at function like body
	// Literally just `type`
	// All commit characters, valid when `isNewIdentifierLocation` is false.
	// Commit characters valid at expression positions where we could be inside a parameter list.
	// Special values for `CompletionInfo['source']` used to disambiguate
	// completion items with the same `name`. (Each completion item must
	// have a unique name/source combination, because those two fields
	// comprise `CompletionEntryIdentifier` in `getCompletionEntryDetails`.
	//
	// When the completion item is an auto-import suggestion, the source
	// is the module specifier of the suggestion. To avoid collisions,
	// the values here should not be a module specifier we would ever
	// generate for an auto-import.
	// Completions that require `this.` insertion text.
	// Auto-import that comes attached to a class member snippet.
	// A type-only import that needs to be promoted in order to be used at the completion location.
	// Auto-import that comes attached to an object literal method snippet.
	// Case completions for switch statements.
	// Completions for an object literal expression.
	// Value is set to false for global variables or completions from external module exports,
	// true otherwise.
	// string | jsnum.Number | PseudoBigInt
	// `isValidTrigger` ensures we are at `import |`
	// !!! see if incomplete completion list and continue or clean
	/*forItemResolve*/ // If the current position is a jsDoc tag name, only tag names should be provided for completion
	/*tagNameOnly*/ // If the current position is a jsDoc tag, only tags should be provided for completion
	/*tagNameOnly*/ // The current position is next to the '@' sign, when no tag name being provided yet.
	// Provide a full list of tag names
	// When completion is requested without "@", we will have check to make sure that
	// there are no comments prefix the request position. We will only allow "*" and space.
	// e.g
	//   /** |c| /*
	//
	//   /**
	//     |c|
	//    */
	//
	//   /**
	//    * |c|
	//    */
	//
	//   /**
	//    *         |c|
	//    */
	// Completion should work inside certain JSDoc tags. For example:
	//     /** @type {number | string} */
	// Completion should work in the brackets
	// Use as type location if inside tag's type expression
	// Proceed if the current position is in JSDoc tag expression; otherwise it is a normal
	// comment or the plain text part of a JSDoc comment, so no completion should be available
	// The decision to provide completion depends on the contextToken, which is determined through the previousToken.
	// Note: 'previousToken' (and thus 'contextToken') can be undefined if we are the beginning of the file
	// Find the node where completion is requested on.
	// Also determine whether we are trying to complete with members of that node
	// or attributes of a JSX tag.
	// !!! flags := CompletionInfoFlagsNone
	// !!! flags |= CompletionInfoFlags.IsImportStatementCompletion;
	// Bail out if this is a known invalid completion location.
	// This is likely dot from incorrectly parsed expression and user is starting to write spread
	// eg: Math.min(./**/)
	// const x = function (./**/) {}
	// ({./**/})
	// There is nothing that precedes the dot, so this likely just a stray character
	// or leading into a '...' token. Just bail out instead.
	// <UI.Test /* completion position */ />
	// If the tagname is a property access expression, we will then walk up to the top most of property access expression.
	// Then, try to get a JSX container and its associated attributes type.
	// Fix location
	// First case is for `<div foo={true} [||] />` or `<div foo={true} [||] ></div>`,
	// `parent` will be `{true}` and `previousToken` will be `}`.
	// Second case is for `<div foo={true} t[||] ></div>`.
	// Second case must not match for `<div foo={undefine[||]}></div>`.
	// For `<div className="x" [||] ></div>`, `parent` will be JsxAttribute and `previousToken` will be its initializer.
	// For `<div x=[|f/**/|]`, `parent` will be `x` and `previousToken.parent` will be `f` (which is its own JsxAttribute).
	// Note for `<div someBool f>` we don't want to treat this as a jsx inializer, instead it's the attribute name.
	// This also gets mutated in nested-functions after the return
	// Keys are indexes of `symbols`.
	// For a computed property with an accessible name like `Symbol.iterator`,
	// we'll add a completion for the *name* `Symbol` instead of for the property.
	// If this is e.g. [Symbol.iterator], add a completion for `Symbol`.
	// The completion is for `Symbol`, not `iterator`.
	// If this is nested like for `namespace N { export const sym = Symbol(); }`, we'll add the completion for `N`.
	// !!! auto-import symbol
	// Only invalid commit character here would be `(`.
	/*insertAwait*/ // In javascript files, for union types, we don't just get the members that
	// the individual types have in common, we also include all the members that
	// each individual type has. This is because we're going to add all identifiers
	// anyways. So we might as well elevate the members that were at least part
	// of the individual types to a higher status since we know what they are.
	/*insertAwait*/ // Right of dot member completion list
	// Since this is qualified name check it's a type node location
	// Extract module or enum members
	// At `namespace N.M/**/`, if this is the only declaration of `M`, don't include `M` as a completion.
	// Any kind is allowed when dotting off namespace in internal import equals declaration
	// If the module is merged with a value, we must get the type of the class and add its properties (for inherited static methods).
	// microsoft/TypeScript#39946. Pulling on the type of a node inside of a function with a contextual `this` parameter can result in a circularity
	// if the `node` is part of the exprssion of a `yield` or `return`. This circularity doesn't exist at compile time because
	// we will check (and cache) the type of `this` *before* checking the type of the node.
	/*includeGlobalThis*/ /*insertAwait*/ /*insertQuestionDot*/ // Aggregates relevant symbols for completion in object literals in type argument positions.
	// Aggregates relevant symbols for completion in object literals and object binding patterns.
	// Relevant symbols are stored in the captured 'symbols' variable.
	// We're looking up possible property names from contextual/inferred/declared type.
	// Check completions for Object property value shorthand
	// Edge case: If NumberIndexType exists
	// We are *only* completing on properties from the type being destructured.
	// We don't want to complete using the type acquired by the shape
	// of the binding pattern; we are only interested in types acquired
	// through type declaration or inference.
	// Also proceed if rootDeclaration is a parameter and if its containing function expression/arrow function is contextually typed -
	// type of parameter will flow in from the contextual type of the function.
	/*isSuper*/ /*isWrite*/ // Add filtered items to the completion list.
	// Set sort texts.
	/*origin*/ /*isJsxIdentifierExpected*/ // If already typing an import statement, provide completions for it.
	// If not already a module, must have modules enabled.
	// Always using ES modules in 6.0+
	// Mutates `symbols`, `symbolToOriginInfoMap`, and `symbolToSortTextMap`
	// `completionItem/resolve` for auto-import completions should be resolved via the completion item data,
	// so we don't need to collect auto-import entries again.
	// import { type | -> token text should be blank
	/*includeJSDoc*/ // Aggregates relevant symbols for completion in import clauses and export clauses
	// whose declarations have a module specifier; for instance, symbols will be aggregated for
	//
	//      import { | } from "moduleName";
	//      export { a as foo, | } from "moduleName";
	//
	// but not for
	//
	//      export { | };
	//
	// Relevant symbols are stored in the captured 'symbols' variable.
	// `import { |` or `import { a as 0, | }` or `import { type | }`
	// We can at least offer `type` at `import { |`
	// try to show exported member for imported/re-exported module
	// If there's nothing else to import, don't offer `type` either.
	// import { x } from "foo" with { | }
	// Adds local declarations for completions in named exports:
	//   export { | };
	// Does not check for the absence of a module specifier (`export {} from "./other"`)
	// because `tryGetImportOrExportClauseCompletionSymbols` runs first and handles that,
	// preventing this function from running.
	// no members, only keywords
	// Declaring new property/method/accessor
	// Has keywords for constructor parameter
	// Aggregates relevant symbols for completion in class declaration
	// Relevant symbols are stored in the captured 'symbols' variable.
	// We're looking up possible property names from parent type.
	// Declaring new property/method/accessor
	// If you're in an interface you don't want to repeat things from super-interface. So just stop here.
	// If this is context token is not something we are editing now, consider if this would lead to be modifier.
	// No member list for private methods
	// List of property symbols of base type that are not private and already implemented
	// Cursor is inside a JSX self-closing element or opening element.
	// Set sort texts.
	// Get all entities in the current scope.
	// We need to find the node that will give us an appropriate scope to begin
	// aggregating completion candidates. This is achieved in 'getScopeNode'
	// by finding the first node that encompasses a position, accounting for whether a node
	// is "complete" to decide whether a position belongs to the node.
	//
	// However, at the end of an identifier, we are interested in the scope of the identifier
	// itself, but fall outside of the identifier. For instance:
	//
	//      xyz => x$
	//
	// the cursor is outside of both the 'x' and the arrow function 'xyz => x',
	// so 'xyz' is not returned in our results.
	//
	// We define 'adjustedPosition' so that we may appropriately account for
	// being at the end of an identifier. The intention is that if requesting completion
	// at the end of an identifier, it should be effectively equivalent to requesting completion
	// anywhere inside/at the beginning of the identifier. So in the previous case, the
	// 'adjustedPosition' will work as if requesting completion in the following:
	//
	//      xyz => $x
	//
	// If previousToken !== contextToken, then
	//   - 'contextToken' was adjusted to the token prior to 'previousToken'
	//      because we were at the end of an identifier.
	//   - 'previousToken' is defined.
	/*includeJSDoc*/ // Need to insert 'this.' before properties of `this` type.
	/*includeGlobalThis*/ // For JavaScript or TypeScript, if we're not after a dot, then just try to get the
	// global symbols in scope.  These results should be valid for either language as
	// the set of symbols that can be referenced from this location.
	// exclude literal suggestions after <input type="text" [||] /> microsoft/TypeScript#51667) and after closing quote (microsoft/TypeScript#52675)
	// for strings getStringLiteralCompletions handles completions
	// Verify if the file is JSX language variant
	// When the completion is for the expression of a case clause (e.g. `case |`),
	// filter literals & enum symbols whose values are already present in existing case clauses.
	/*replacementToken*/ // Tracks unique names.
	// Value is set to false for global variables or completions from external module exports, because we can have multiple of those;
	// true otherwise. Based on the order we add things we will always see locals first, then globals, then module exports.
	// So adding a completion for a local will prevent us from adding completions for external module exports sharing the same name.
	// When in a value location in a JS file, ignore symbols that definitely seem to be type-only.
	// True for locals; false for globals, module exports from other files, `this.` completions.
	// !!! check for type-only in JS
	// !!! deprecation
	// Non-contextual keywords (e.g., `function`, `class`, `const`) cannot be used as identifiers,
	// so auto-imports with these names should not shadow keyword completions.
	/*isMemberCompletion*/ /*hasAction*/ /*preselect*/ /*additionalTextEdits*/ /*detail*/ /*prefix*/ /*suffix*/ // We should only have needsConvertPropertyAccess if there's a property access to convert. But see microsoft/TypeScript#21790.
	// Somehow there was a global with a non-identifier name. Hopefully someone will complain about getting a "foo bar" global completion and provide a repro.
	// If the text after the '.' starts with this name, write over it. Else, add new text.
	/*includeJSDoc*/ /*includeJSDoc*/ // Provide object member completions when missing commas, and insert missing commas.
	// For example:
	//
	//    interface I {
	//        a: string;
	//        b: number
	//     }
	//
	//     const cc: I = { a: "red" | }
	//
	// Completion should add a comma after "red" and provide completions for b
	/*excludeJSDoc*/ // If is boolean like or undefined, don't return a snippet, we want to return just the completion.
	// If type is string-like or undefined, use quotes.
	// Use braces for everything else.
	// Check if it is `import { ^here as name } from '...'``.
	// We have to access the scanner here to check if it is `{ ^here as name }`` or `{ ^here, as, name }`.
	// Commit characters
	// Otherwise use the completion list default.
	/*autoImportFix*/ /*detail*/ /*emitContext*/ /*idToSymbol*/ /*modifiers*/ /*questionToken*/ /*typeNode*/ /*nodes*/ /*multiLine*/ /*modifiers*/ /*asteriskToken*/ /*postfixToken*/ /*typeParameters*/ /*typeNode*/ /*fullSignature*/ /*origin*/ /*isJsxIdentifierExpected*/ /*modifiers*/ /*emitContext*/ /*multiLine*/ /*includeJSDoc*/ /*includeJSDoc*/ /*includeJSDoc*/ /*multiLine*/ // Ported from vscode.
	// Finds the length and first rune of the word that ends at the given position.
	// e.g. for "abc def.ghi|jkl", the word length is 3 and the word start is 'g'.
	// !!! Port other case of vscode's `DEFAULT_WORD_REGEXP` that covers words that start like numbers, e.g. -123.456abcd.
	// If word starts with `@`, disregard this first character.
	// `["ab c"]` -> `ab c`
	// `['ab c']` -> `ab c`
	// `[123]` -> `123`
	// Ported from vscode ts extension: `getFilterText`.
	// Private field completion, e.g. label `#bar`.
	// `method() { this.#| }`
	// `method() { #| }`
	// `method() { this.| }`
	// `method() { | }`
	// `method() { this.#| }`
	// `method() { this.| }`
	// `method() { | }`
	// For `this.` completions, generally don't set the filter text since we don't want them to be overly deprioritized. microsoft/vscode#74164
	// Handle the case:
	// ```
	// const xyz = { 'ab c': 1 };
	// xyz.ab|
	// ```
	// In which case we want to insert a bracket accessor but should use `.abc` as the filter text instead of
	// the bracketed insert text.
	// Handle this case like the case above:
	// ```
	// const xyz = { 'ab c': 1 } | undefined;
	// xyz.ab|
	// ```
	// filterText should be `.ab c` instead of `?.['ab c']`.
	// ```
	// const xyz = { abc: 1 } | undefined;
	// xyz.ab|
	// ```
	// filterText should be `.abc` instead of `?.abc.
	// In all other cases, fall back to using the insertText.
	// Ported from vscode's `provideCompletionItems`.
	// export = /**/ here we want to get all meanings, so any symbol is ok
	// Filter out variables from their own initializers
	// `const a = /* no 'a' here */`
	// Filter out current and latter parameters from defaults
	// `function f(a = /* no 'a' and 'b' here */, b) { }` or
	// `function f<T = /* no 'T' and 'T2' here */>(a: T, b: T2) { }`
	// filter out the directly self-recursive type parameters
	// `type A<K extends /* no 'K' here*/> = K`
	// External modules can have global export declarations that will be
	// available as global keywords in all scopes. But if the external module
	// already has an explicit export and user only wants to use explicit
	// module imports then the global keywords will be filtered out so auto
	// import suggestions will win in the completion.
	// We only want to filter out the global keywords.
	// Auto Imports are not available for scripts so this conditional is always false.
	// import m = /**/ <-- It can only access namespace (if typing import = x. this would get member symbols and not namespace)
	// It's a type, but you can reach it by namespace.type as well.
	// expressions are value space (which includes the value namespaces)
	// If the symbol is external module, don't show it in the completion list
	// (i.e declare module "http" { const x; } | // <= request completion here, "http" should not be there)
	// If the symbol is the internal name of an ES symbol, it is not a valid entry. Internal names for ES symbols start with "__@"
	// name is a valid identifier or private identifier text
	// Allow non-identifier import/export aliases since we can insert them as string literals
	// TODO: microsoft/TypeScript#18169
	// For a 'this.' completion it will be in a global context, but may have a non-identifier name.
	// Don't add a completion for a name starting with a space. See https://github.com/Microsoft/TypeScript/pull/20547
	// !!! refactor symbolOriginInfo so that we can tell the difference between flags and the kind of data it has
	// In a scenarion such as `const x = 1 * |`, the context and previous tokens are both `*`.
	// In `const x = 1 * o|`, the context token is *, and the previous token is `o`.
	// `contextToken` and `previousToken` can both be nil if we are at the beginning of the file.
	// "." | '"' | "'" | "`" | "/" | "@" | "<" | "#" | " " | "*"
	// Only automatically bring up completions if this is an opening quote.
	/*includeJSDoc*/ // Opening JSX tag
	// True if symbol is a type or a module containing at least one type.
	// Since an alias can be merged with a local declaration, we need to test both the alias and its target.
	// This code used to just test the result of `skipAlias`, but that would ignore any locally introduced meanings.
	// Gets all properties on a type, but if that type is a union of several types,
	// excludes array-like types or callable/constructable types.
	// Given 'a.b.c', returns 'a'.
	/*meaning*/ /*useOnlyExternalAliasing*/ // getContextualTypeForConditionalExpression handles completion within a conditional expression
	// (ternary operator) by using the parent expression to find the contextual type.
	// Fall through to regular contextual type logic if not in an argument
	// When completing after `[` in an array literal (e.g., `[/*here*/]`),
	// we should provide contextual type for the first element
	// Get the type for the first element (index 0)
	// When completing after `]` (e.g., `[x]/*here*/`), we should not provide a contextual type
	// for the closing bracket token itself. Without this case, CloseBracketToken would fall through
	// to the default case, and if the parent is an array literal, GetContextualType would try to
	// find the token's index in the array elements (returning -1), leading to an out-of-bounds panic
	// in getContextualTypeForElementExpression.
	// When completing after `?` in a ternary conditional (e.g., `foo(a ? /*here*/)`),
	// we need to look at the parent conditional expression to find the contextual type.
	// When completing after `:` in a ternary conditional (e.g., `foo(a ? b : /*here*/)`),
	// we need to look at the parent conditional expression to find the contextual type.
	// Only handle this if parent is ConditionalExpression, otherwise fall through to default
	// (colons are used in other contexts like object literals, type annotations, etc.)
	// When completing after `,` in an array literal (e.g., `[x, /*here*/]`),
	// we should provide contextual type for the element after the comma.
	// Default case: see if we're in an argument position.
	// completion at `x ===/**/`
	// We disregard boolean literals for completion purposes.
	// For a union, return the first one with a recommended completion.
	// Don't make a recommended completion for an abstract class.
	// const a = () => /**/;
	// !!! ensure range is single line
	/*includeJSDoc*/ // we return no replacement range only if unterminated string is empty
	// Checks whether type is `string & {}`, which is semantically equivalent to string but
	// is not reduced by the checker as a special case used for supporting string literal completions
	// for string type.
	// Convert "(example, text)" into "_example_text_"
	// Default to "_" if the provided text was empty
	// Copied from vscode TS extension.
	// Editors will use the `sortText` and then fall back to `name` for sorting, but leave ties in response order.
	// So, it's important that we sort those ties in the order we want them displayed if it matters. We don't
	// strictly need to sort by name or SortText here since clients are going to do it anyway, but we have to
	// do the work of comparing them so we can sort those ties appropriately.
	// An `AssertClause` can come after an import declaration:
	//  import * from "foo" |
	//  import "foo" |
	// or after a re-export declaration that has a module specifier:
	//  export { foo } from "foo" |
	// Source: https://tc39.es/proposal-import-assertions/
	// Skip identifiers produced only from the current location
	// StringLiteralLike locations are handled separately in stringCompletions.ts
	/*includeJSDoc*/ // Previous token may have been a keyword that was converted to an identifier.
	// func( a, |
	// new C(a, |
	// func\n(a, |
	// const x = (a, |
	// constructor( a, | /* public, protected, private keywords are allowed here, so show completion */
	// var x: (s: string, list|
	// const obj = { x, |
	// [a, |
	// func( |
	// new C(a|
	// func\n( |
	// const x = (a|
	// constructor( |
	// function F(pred: (a| /* this can become an arrow function, where 'a' is the argument */
	// [ |
	// [ | : string ]
	// [ | : string ]
	// [ |    /* this can become an index signature */
	// module |
	// namespace |
	// import |
	// module A.|
	// class A { |
	// const obj = { |
	// const x = a|
	// x = a|
	// `aa ${|
	// `aa ${10} dd ${|
	// const obj = { async c|()
	// const obj = { async c|
	// const obj = { * c|
	// Finds the first node that "embraces" the position, so that one may
	// accurately aggregate locals from the closest containing scope.
	// Determines if a type is exactly the same type resolved by the global 'self', 'global', or 'globalThis'.
	// The type of `self` and `window` is the same in lib.dom.d.ts, but `window` does not exist in
	// lib.webworker.d.ts, so checking against `self` is also a check against `window` when it exists.
	/*diagnostic*/ /*diagnostic*/ /*diagnostic*/ // Try to get the reparsed node first - we may be in JSDoc.
	// In some cases, we won't have a corresponding symbol
	// (e.g. JSDoc types that never get re-attached) so we'll use
	// the name as declared by the property as a best-effort.
	// The cursor is at a property value location like `Foo<{ x: | }`.
	// `t` already refers to the appropriate property type.
	// const x = { |
	// const x = { a: 0, |
	// Object literal is assignment pattern: ({ | } = x)
	// f(() => (({ | })));
	// Filter out members whose only declaration is the object literal itself to avoid
	// self-fulfilling completions like:
	//
	// function f<T>(x: T) {}
	// f({ abc/**/: "" }) // `abc` is a member of `T` but only because it declares itself
	// Filters out members that are already declared in the object literal or binding pattern.
	// Also computes the set of existing members declared by spread assignment.
	// Ignore omitted expressions for missing members.
	// If this is the current item we are editing right now, do not filter it out.
	// include only identifiers in completion list
	// TODO: Account for computed property name
	// NOTE: if one only performs this step when m.name is an identifier,
	// things like '__proto__' are not filtered out.
	/*includeJSDoc*/ // Returns the immediate owning class declaration of a context token,
	// on the condition that one exists and that the context implies completion should be given.
	// Returns the immediate owning class declaration of a context token,
	// on the condition that one exists and that the context implies completion should be given.
	// class c { method() { } | method2() { } }
	// class c { public prop = c| }
	// class c extends React.Component { a: () => 1\n compon| }
	// class C { blah; constructor/**/ }
	// or
	// class C { blah \n constructor/**/ }
	// class c { public prop = | /* global completions */ }
	// class c {getValue(): number; | }
	// class c { method() { } | }
	// class c { method() { } b| }
	// class c { |
	// class c {getValue(): number, | }
	// class C extends React.Component { a: () => 1\n| }
	// class C { prop = ""\n | }
	// Filters out completion suggestions for class elements.
	// Ignore omitted expressions for missing members.
	// If this is the current item we are editing right now, do not filter it out
	// Don't filter member even if the name matches if it is declared private in the list.
	// Do not filter it out if the static presence doesn't match.
	// Currently we parse JsxOpeningLikeElement as:
	//      JsxOpeningLikeElement
	//          attributes: JsxAttributes
	//             properties: NodeArray<JsxAttributeLike>
	// The context token is the closing } or " of an attribute, which means
	// its parent is a JsxExpression, whose parent is a JsxAttribute,
	// whose parent is a JsxOpeningLikeElement
	// Currently we parse JsxOpeningLikeElement as:
	//      JsxOpeningLikeElement
	//          attributes: JsxAttributes
	//             properties: NodeArray<JsxAttributeLike>
	// Currently we parse JsxOpeningLikeElement as:
	//      JsxOpeningLikeElement
	//          attributes: JsxAttributes
	//             properties: NodeArray<JsxAttributeLike>
	//                  each JsxAttribute can have initializer as JsxExpression
	// Currently we parse JsxOpeningLikeElement as:
	//      JsxOpeningLikeElement
	//          attributes: JsxAttributes
	//             properties: NodeArray<JsxAttributeLike>
	// Filters out completion suggestions from 'symbols' according to existing JSX attributes.
	// @returns Symbols to be suggested in a JSX element, barring those whose attributes
	// do not occur at the current position and have not otherwise been typed.
	// If this is the item we are editing right now, do not filter it out.
	// Returns the item defaults for completion items, if that capability is supported.
	// Otherwise, if some item default is not supported by client, sets that property on each item.
	// Ported from vscode ts extension.
	// If `editRange` is set, `insertText` is ignored by the client, so we need to
	// provide `textEdit` instead.
	// We wanna walk up the tree till we find a JSX closing element.
	// In the TypeScript JSX element, if such element is not defined. When users query for completion at closing tag,
	// instead of simply giving unknown value, the completion will return the tag-name of an associated opening-element.
	// For example:
	//     var x = <div> </ /*1*/
	// The completion list at "1" will contain "div>" with type any
	// And at `<div> </ /*1*/ >` (with a closing `>`), the completion list will contain "div".
	// And at property access expressions `<MainComponent.Child> </MainComponent. /*1*/ >` the completion will
	// return full closing tag with an optional replacement span
	// For example:
	//     var x = <MainComponent.Child> </     MainComponent /*1*/  >
	//     var y = <MainComponent.Child> </   /*2*/   MainComponent >
	// the completion list at "1" and "2" will contain "MainComponent.Child" with a replacement span of closing tag name
	/*isNewIdentifierLocation*/ /*name*/ /*insertText*/ /*filterText*/ /*kindModifiers*/ /*replacementSpan*/ /*commitCharacters*/ /*labelDetails*/ /*isMemberCompletion*/ /*isSnippet*/ /*hasAction*/ /*preselect*/ /*source*/ /*autoImportEntryData*/ // !!! jsx autoimports
	/*additionalTextEdits*/ /*detail*/ // Text edit
	// Filter text
	// Ported from vscode ts extension.
	// Adjustements based on kind modifiers.
	// Copied from vscode ts extension: `MyCompletionItem.constructor`.
	// !!! adjust label like vscode does
	// Client assumes plain text by default.
	/*isNewIdentifierLocation*/ /*insertText*/ /*filterText*/ /*kindModifiers*/ /*replacementSpan*/ /*commitCharacters*/ /*labelDetails*/ /*isMemberCompletion*/ /*isSnippet*/ /*hasAction*/ /*preselect*/ /*source*/ /*autoImportEntryData*/ /*additionalTextEdits*/ /*detail*/ // To be "in" one of these literals, the position has to be:
	//   1. entirely within the token text.
	//   2. at the end position of an unterminated token.
	//   3. at the end of a regular expression (due to trailing flags like '/foo/g').
	// true if we are certain that the currently edited location must define a new location; false otherwise.
	// enum a { foo, |
	// interface A<T, |
	// var [x, y|
	// type Map, K, |
	// class A<T, |
	// var C = class D<T, |
	// var [.|
	// var {x :html|
	// var [x|
	// enum a { |
	// class A< |
	// var C = class D< |
	// interface A< |
	// type List< |
	// var [...z|
	// import { type | }
	// let a
	// |
	// import { type foo| }
	// If the previous token is keyword corresponding to class member completion keyword
	// there will be completion available here
	// constructor parameter completion is available only if
	// - its modifier of the constructor parameter or
	// - its name of the parameter and not being edited
	// eg. constructor(a |<- this shouldnt show completion
	// Previous token may have been a keyword that was converted to an identifier.
	// If we are inside a class declaration, and `constructor` is totally not present,
	// but we request a completion manually at a whitespace...
	// Don't block completions.
	// If we are inside a class declaration and typing `constructor` after property declaration...
	// And the cursor is at the token...
	// If we are sure that the previous property declaration is terminated according to newline or semicolon...
	// Don't block completions.
	// Should not block: `class C { blah = c/**/ }`
	// But should block: `class C { blah = somewhat c/**/ }` and `class C { blah: SomeType c/**/ }`
	// Don't block completions if we're in `class C /**/`, `interface I /**/` or `<T /**/>` ,
	// because we're *past* the end of the identifier and might want to complete `extends`.
	// If `contextToken !== previousToken`, this is `class C ex/**/`, `interface I ex/**/` or `<T ex/**/>`.
	// <Component<string> /**/ />
	// <Component<string> /**/ ><Component>
	// - contextToken: GreaterThanToken (before cursor)
	// - location: JsxSelfClosingElement or JsxOpeningElement
	// - contextToken.parent === location
	// <div>/**/
	// - contextToken: GreaterThanToken (before cursor)
	// - location: JSXElement
	// - different parents (JSXOpeningElement, JSXElement)
	// Special values for `CompletionInfo['source']` used to disambiguate
	// completion items with the same `name`. (Each completion item must
	// have a unique name/source combination, because those two fields
	// comprise `CompletionEntryIdentifier` in `getCompletionEntryDetails`.
	//
	// When the completion item is an auto-import suggestion, the source
	// is the module specifier of the suggestion. To avoid collisions,
	// the values here should not be a module specifier we would ever
	// generate for an auto-import.
	// Completions that require `this.` insertion text
	// Auto-import that comes attached to a class member snippet
	// A type-only import that needs to be promoted in order to be used at the completion location
	// Auto-import that comes attached to an object literal method snippet
	// Case completions for switch statements
	// Completions for an object literal expression
	// Auto-imports in content-mapped files are evaluated eagerly so edits outside
	// of verbatim spans can cause the completion item to be filtered out entirely.
	// Only real files take this code path, so the final Edits() is guaranteed ok.
	// Compute all the completion symbols again.
	// Didn't find a symbol with this name.  See if we can find a keyword instead.
	/*forItemResolve*/ // Find the symbol with the matching entry name.
	// We don't need to perform character checks here because we're only comparing the
	// name against 'entryName' (which is known to be good), not building a new
	// completion entry.
	/*documentation*/ // !!! fill in additionalTextEdits from code actions
	// Description of the code action to display in the UI of the editor
	// Text changes to apply to each file as part of the code action
	/*vsCapability*/ // import Foo |
	// import Foo f|
	// At `import { ... } |` or `import * as Foo |`, the only possible completion is `from`
	// A lone import keyword with nothing following it does not parse as a statement at all
	// `import s| from`
	// node is ImportDeclaration | ImportEqualsDeclaration | ImportSpecifier | JSDocImportTag | Token<SyntaxKind.ImportKeyword>
	// Use token position (excluding JSDoc/trivia) instead of node.Pos() to avoid including JSDoc comments
	/*includeJSDoc*/ // Guess which point in the import might actually be a later statement parsed as part of the import
	// during parser recovery - either in the middle of named imports, or the module specifier.
	// The module specifier/reference was previously found to be missing, empty, or
	// not a string literal - in this last case, it's likely that statement on a following
	// line was parsed as the module specifier of a partially-typed import, e.g.
	//   import Foo|
	//   interface Blah {}
	// This appears to be a multiline-import, and editors can't replace multiple lines.
	// But if everything but the "module specifier" is on one line, by this point we can
	// assume that the "module specifier" is actually just another statement, and return
	// the single-line range of the import excluding that probable statement.
	// We can only complete on named imports if there are no other named imports already,
	// but parser recovery sometimes puts later statements in the named imports list, so
	// we try to only consider the probably-valid ones.
	// Tries to identify the first named import that is not really a named import, but rather
	// just parser recovery for a situation like:
	//
	//	import { Foo|
	//	interface Bar {}
	//
	// in which `Foo`, `interface`, and `Bar` are all parsed as import specifiers. The caller
	// will also check if this token is on a separate line from the rest of the import.
	// Get the corresponding JSDocTag node if the position is in a JSDoc comment
	/*isNewIdentifierLocation*/ /*optionalReplacementSpan*/ // isSnippet := clientSupportsItemSnippet(clientOptions)
	// !!! need snippet printer
	/*includeJSDoc*/ // This parameter is already annotated.
	// Named parameter
	/*isObject*/ /*isSnippet*/ /*isObject*/ /*isSnippet*/ // Remove `@`
	// Destructuring parameter; do it positionally
	/*isSnippet*/ /*isSnippet*/ // Remove `@`
	/*idToSymbol*/ // !!! snippet p
	// !!!
	// Module: options.Module,
	// ModuleResolution: options.ModuleResolution,
	// Target: options.Target,
	/*isObject*/ /*isObject*/ /*isObject*/ // Assumes binding element is inside object binding pattern.
	// We can't deeply annotate an array binding pattern.
	// `{ b }` or `{ b: newB }`
	/*isObject*/ // `{ b: {...} }` or `{ b: [...] }`
	// Collect constant values in existing clauses.
	// Tolerate a nil import adder in untitled files.
	// Enums
	// Filter existing enums by their values
	// Literals
	/*emitContext*/ /*questionDotToken*/ /*questionDotToken*/ /** Snippet-escaping version of `printer.printNode`. */ /*sourceFile*/ /*sourceMapGenerator*/ /*initialIndentation*/ /*delta*/ // Override base writer methods to perform snippet escaping.
	// The formatter/scanner will have issues with snippet-escaped text,
	// so instead of writing the escaped text directly to the writer,
	// generate a set of changes that can be applied to the unescaped text
	// to escape it post-formatting.
	0]
	for _, item := range list.Items {
		if item.Data == nil || item.Data.AutoImport == nil {
			filtered = append(filtered, item)
			continue
		}
		edits, description, ok := (&autoimport.Fix{AutoImportFix: item.Data.AutoImport}).Edits(ctx, file, program.Options(), l.FormatOptions(), l.converters, l.UserPreferences())
		if !ok {
			continue
		}
		item.AdditionalTextEdits = &edits
		item.Detail = strPtrTo(description)
		filtered = append(filtered, item)
	}
	list.Items = filtered
}
func (l *LanguageService) GetCompletionsAtPosition(ctx context.Context, file *ast.SourceFile, position int, triggerCharacter *string, includeSymbols bool) (*CompletionList, error) {
	return l.getCompletionsAtPosition(ctx, file, position, triggerCharacter, includeSymbols)
}

type CompletionItem struct {
	*lsproto.CompletionItem
	Symbol *ast.Symbol
}
type CompletionList struct {
	IsIncomplete bool
	ItemDefaults *lsproto.CompletionItemDefaults
	ApplyKind    *lsproto.CompletionItemApplyKinds
	Items        []*CompletionItem
}

func ensureItemData(file *ast.SourceFile, pos int, list *lsproto.CompletionList) *lsproto.CompletionList {
	if list == nil {
		return nil
	}
	for _, item := range list.Items {
		if item.Data == nil {
			item.Data = &lsproto.CompletionItemData{FileName: file.OriginalFileName(), Position: int32(pos), SupplementalFileIndex: supplementalFileIndex(file), Name: item.Label}
		}
	}
	return list
}
func supplementalFileIndex(file *ast.SourceFile) *int32 {
	canonical := file.CanonicalSourceFile()
	if canonical == nil {
		return nil
	}
	for i, supplemental := range canonical.SupplementalSourceFiles() {
		if supplemental == file {
			return new(int32(i))
		}
	}
	panic("supplemental source file is not linked from its canonical source file")
}
func sourceFileForSupplementalFileIndex(file *ast.SourceFile, index *int32) *ast.SourceFile {
	if index == nil {
		return file
	}
	supplemental := file.SupplementalSourceFiles()
	if *index >= 0 && int(*index) < len(supplemental) {
		return supplemental[*index]
	}
	return nil
}

type completionData = any
type completionDataData struct {
	symbols                      []*ast.Symbol
	autoImports                  []*autoimport.FixAndExport
	completionKind               CompletionKind
	isInSnippetScope             bool
	propertyAccessToConvert      ast.Handle
	isNewIdentifierLocation      bool
	location                     ast.Handle
	keywordFilters               KeywordCompletionFilters
	literals                     []literalValue
	symbolToOriginInfoMap        map[int]*symbolOriginInfo
	symbolToSortTextMap          map[ast.SymbolId]SortText
	recommendedCompletion        *ast.Symbol
	previousToken                ast.Handle
	contextToken                 ast.Handle
	jsxInitializer               jsxInitializer
	insideJSDocTagTypeExpression bool
	isTypeOnlyLocation           bool
	isJsxIdentifierExpected      bool
	isRightOfOpenTag             bool
	isRightOfDotOrQuestionDot    bool
	importStatementCompletion    *importStatementCompletionInfo
	hasUnresolvedAutoImports     bool
	defaultCommitCharacters      []string
}
type completionDataKeyword struct {
	keywordCompletions      []*CompletionItem
	isNewIdentifierLocation bool
}
type completionDataJSDocTagName struct{}
type completionDataJSDocTag struct{}
type completionDataJSDocParameterName struct {
	tag ast.Handle
}
type importStatementCompletionInfo struct {
	isKeywordOnlyCompletion        bool
	keywordCompletion              ast.Kind
	isNewIdentifierLocation        bool
	isTopLevelTypeOnly             bool
	couldBeTypeOnlyImportSpecifier bool
	replacementSpan                *lsproto.Range
}

type jsxInitializer struct {
	isInitializer bool
	initializer   ast.Handle
}
type KeywordCompletionFilters int

const (
	KeywordCompletionFiltersNone KeywordCompletionFilters = iota
	KeywordCompletionFiltersAll
	KeywordCompletionFiltersClassElementKeywords
	KeywordCompletionFiltersInterfaceElementKeywords
	KeywordCompletionFiltersConstructorParameterKeywords
	KeywordCompletionFiltersFunctionLikeBodyKeywords
	KeywordCompletionFiltersTypeAssertionKeywords
	KeywordCompletionFiltersTypeKeywords
	KeywordCompletionFiltersTypeKeyword
	KeywordCompletionFiltersLast = KeywordCompletionFiltersTypeKeyword
)

func keywordFiltersFromSyntaxKind(keywordCompletion ast.Kind) KeywordCompletionFilters {
	switch keywordCompletion {
	case ast.KindTypeKeyword:
		return KeywordCompletionFiltersTypeKeyword
	default:
		panic("Unknown mapping from ast.Kind `" + keywordCompletion.String() + "` to KeywordCompletionFilters")
	}
}

type CompletionKind int

const (
	CompletionKindNone CompletionKind = iota
	CompletionKindObjectPropertyDeclaration
	CompletionKindGlobal
	CompletionKindPropertyAccess
	CompletionKindMemberLike
	CompletionKindString
)

var CompletionTriggerCharacters = []string{".", `"`, "'", "`", "/", "@", "<", "#", " ", "*"}

var allCommitCharacters = []string{".", ",", ";"}

var noCommaCommitCharacters = []string{".", ";"}
var emptyCommitCharacters = []string{}

type SortText string

const (
	SortTextLocalDeclarationPriority         SortText = "10"
	SortTextLocationPriority                 SortText = "11"
	SortTextOptionalMember                   SortText = "12"
	SortTextMemberDeclaredBySpreadAssignment SortText = "13"
	SortTextSuggestedClassMembers            SortText = "14"
	SortTextGlobalsOrKeywords                SortText = "15"
	SortTextAutoImportSuggestions            SortText = "16"
	SortTextClassMemberSnippets              SortText = "17"
	SortTextJavascriptIdentifiers            SortText = "18"
)

func DeprecateSortText(original SortText) SortText {
	return "z" + original
}
func ObjectLiteralPropertySortText(presetSortText SortText, symbolDisplayName string) SortText {
	return presetSortText + "\x00" + SortText(symbolDisplayName) + "\x00"
}
func SortBelow(original SortText) SortText {
	return original + "1"
}

type symbolOriginInfoKind int

const (
	symbolOriginInfoKindThisType symbolOriginInfoKind = 1 << iota
	symbolOriginInfoKindSymbolMember
	symbolOriginInfoKindPromise
	symbolOriginInfoKindNullable
	symbolOriginInfoKindTypeOnlyAlias
	symbolOriginInfoKindObjectLiteralMethod
	symbolOriginInfoKindIgnore
	symbolOriginInfoKindComputedPropertyName
)

type symbolOriginInfo struct {
	kind              symbolOriginInfoKind
	isDefaultExport   bool
	isFromPackageJson bool
	fileName          string
	data              any
}

func (origin *symbolOriginInfo) symbolName() string {
	switch origin.data.(type) {
	case *symbolOriginInfoComputedPropertyName:
		return origin.data.(*symbolOriginInfoComputedPropertyName).symbolName
	default:
		panic(fmt.Sprintf("symbolOriginInfo: unknown data type for symbolName(): %T", origin.data))
	}
}

type symbolOriginInfoObjectLiteralMethod struct {
	insertText   string
	labelDetails *lsproto.CompletionItemLabelDetails
	isSnippet    bool
}

func (s *symbolOriginInfo) asObjectLiteralMethod() *symbolOriginInfoObjectLiteralMethod {
	return s.data.(*symbolOriginInfoObjectLiteralMethod)
}

type symbolOriginInfoTypeOnlyAlias struct{ declaration ast.Handle }
type symbolOriginInfoComputedPropertyName struct{ symbolName string }

type completionSource string

const (
	completionSourceThisProperty                 completionSource = "ThisProperty/"
	completionSourceClassMemberSnippet           completionSource = "ClassMemberSnippet/"
	completionSourceTypeOnlyAlias                completionSource = "TypeOnlyAlias/"
	completionSourceObjectLiteralMethodSnippet   completionSource = "ObjectLiteralMethodSnippet/"
	completionSourceSwitchCases                  completionSource = "SwitchCases/"
	completionSourceObjectLiteralMemberWithComma completionSource = "ObjectLiteralMemberWithComma/"
)

type uniqueNamesMap = map[string]bool

type literalValue any
type globalsSearch int

const (
	globalsSearchContinue globalsSearch = iota
	globalsSearchSuccess
	globalsSearchFail
)

func (l *CompletionList) toLSP() *lsproto.CompletionList {
	if l == nil {
		return nil
	}
	items := make([]*lsproto.CompletionItem, 0, len(l.Items))
	for _, entry := range l.Items {
		if entry != nil && entry.CompletionItem != nil {
			items = append(items, entry.CompletionItem)
		}
	}
	return &lsproto.CompletionList{IsIncomplete: l.IsIncomplete, ItemDefaults: l.ItemDefaults, ApplyKind: l.ApplyKind, Items: items}
}
func (l *LanguageService) getCompletionsAtPosition(ctx context.Context, file *ast.SourceFile, position int, triggerCharacter *string, includeSymbols bool) (*CompletionList, error) {
	_, previousToken := getRelevantTokens(position, file)
	if triggerCharacter != nil && !IsInString(file, position, previousToken) && !isValidTrigger(file, *triggerCharacter, previousToken, position) {
		return nil, nil
	}
	if triggerCharacter != nil && *triggerCharacter == " " {
		if l.UserPreferences().IncludeCompletionsForImportStatements.IsTrue() {
			return &CompletionList{IsIncomplete: true}, nil
		}
		return nil, nil
	}
	if jsDocSnippetCompletion := l.getJSDocSnippetCompletion(ctx, file, position); jsDocSnippetCompletion != nil {
		return jsDocSnippetCompletion, nil
	}
	compilerOptions := l.GetProgram().Options()
	checker, done := l.GetProgram().GetTypeCheckerForFile(ctx, file)
	defer done()
	stringCompletions := l.getStringLiteralCompletions(ctx, file, position, previousToken, checker, compilerOptions, includeSymbols)
	if stringCompletions != nil {
		return stringCompletions, nil
	}
	if !previousToken.IsNil() && (previousToken.Kind == ast.KindBreakKeyword || previousToken.Kind == ast.KindContinueKeyword || previousToken.Kind == ast.KindIdentifier) && ast.IsBreakOrContinueStatement(previousToken.Parent()) {
		return l.getLabelCompletionsAtPosition(ctx, previousToken.Parent(), file, position, l.getOptionalReplacementSpan(previousToken, file)), nil
	}
	preferences := l.UserPreferences()
	data, err := l.getCompletionData(ctx, checker, file, position, preferences, false)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	switch data := data.(type) {
	case *completionDataData:
		optionalReplacementSpan := l.getOptionalReplacementSpan(data.location, file)
		response, err := l.completionInfoFromData(ctx, checker, file, compilerOptions, data, position, optionalReplacementSpan, includeSymbols)
		if err != nil {
			return nil, err
		}
		return response, nil
	case *completionDataKeyword:
		optionalReplacementSpan := l.getOptionalReplacementSpan(previousToken, file)
		return l.specificKeywordCompletionInfo(ctx, position, file, data.keywordCompletions, data.isNewIdentifierLocation, optionalReplacementSpan), nil
	case *completionDataJSDocTagName:
		items := getJSDocTagNameCompletions()
		items = append(items, getJSDocParameterCompletions(ctx, file, position, checker, compilerOptions, preferences, true)...)
		return l.jsDocCompletionInfo(ctx, position, file, items), nil
	case *completionDataJSDocTag:
		items := getJSDocTagCompletions()
		items = append(items, getJSDocParameterCompletions(ctx, file, position, checker, compilerOptions, preferences, false)...)
		return l.jsDocCompletionInfo(ctx, position, file, items), nil
	case *completionDataJSDocParameterName:
		return l.jsDocCompletionInfo(ctx, position, file, getJSDocParameterNameCompletions(data.tag)), nil
	default:
		panic("getCompletionData() returned unexpected type: " + fmt.Sprintf("%T", data))
	}
}
func (l *LanguageService) getCompletionData(ctx context.Context, typeChecker *checker.Checker, file *ast.SourceFile, position int, preferences lsutil.UserPreferences, forItemResolve bool) (completionData, error) {
	inCheckedFile := isCheckedFile(file, l.GetProgram().Options())
	currentToken := astnav.GetTokenAtPosition(file, position)
	insideComment := isInComment(file, position, currentToken)
	insideJSDocTagTypeExpression := false
	insideJsDocImportTag := false
	isInSnippetScope := false
	if insideComment != nil {
		if hasDocComment(file, position) {
			if position > 0 && file.Text()[position-1] == '@' {
				return &completionDataJSDocTagName{}, nil
			} else {
				lineStart := format.GetLineStartPositionForPosition(position, file)
				noCommentPrefix := true
				for _, r := range file.Text()[lineStart:position] {
					if !(stringutil.IsWhiteSpaceSingleLine(r) || r == '*' || r == '/' || r == '(' || r == ')' || r == '|') {
						noCommentPrefix = false
						break
					}
				}
				if noCommentPrefix {
					return &completionDataJSDocTag{}, nil
				}
			}
		}
		if tag := getJSDocTagAtPosition(currentToken, position); !tag.IsNil() {
			if tag.TagName().Pos() <= position && position <= tag.TagName().End() {
				return &completionDataJSDocTagName{}, nil
			}
			if ast.IsJSDocImportTag(tag) {
				insideJsDocImportTag = true
			} else {
				if typeExpression := tryGetTypeExpressionFromTag(tag); !typeExpression.IsNil() {
					currentToken = astnav.GetTokenAtPosition(file, position)
					if currentToken.IsNil() || (!ast.IsDeclarationName(currentToken) && (currentToken.Parent().Kind != ast.KindJSDocPropertyTag || currentToken.Parent().Name() != currentToken)) {
						insideJSDocTagTypeExpression = isCurrentlyEditingNode(typeExpression, file, position)
					}
				}
				if !insideJSDocTagTypeExpression && ast.IsJSDocParameterTag(tag) && (ast.NodeIsMissing(tag.Name()) || tag.Name().Pos() <= position && position <= tag.Name().End()) {
					return &completionDataJSDocParameterName{tag: tag}, nil
				}
			}
		}
		if !insideJSDocTagTypeExpression && !insideJsDocImportTag {
			return nil, nil
		}
	}
	isJSOnlyLocation := !insideJSDocTagTypeExpression && !insideJsDocImportTag && ast.IsSourceFileJS(file)
	contextToken, previousToken := getRelevantTokens(position, file)
	node := currentToken
	var propertyAccessToConvert ast.Handle
	isRightOfDot := false
	isRightOfQuestionDot := false
	isRightOfOpenTag := false
	isStartingCloseTag := false
	var jsxInitializer jsxInitializer
	isJsxIdentifierExpected := false
	var importStatementCompletion *importStatementCompletionInfo
	location := astnav.GetTouchingPropertyName(file, position)
	keywordFilters := KeywordCompletionFiltersNone
	isNewIdentifierLocation := false
	var defaultCommitCharacters []string
	if !contextToken.IsNil() {
		importStatementCompletionInfo := l.getImportStatementCompletionInfo(contextToken, file)
		if importStatementCompletionInfo.keywordCompletion != ast.KindUnknown {
			if importStatementCompletionInfo.isKeywordOnlyCompletion {
				return &completionDataKeyword{keywordCompletions: []*CompletionItem{{CompletionItem: &lsproto.CompletionItem{Label: scanner.TokenToString(importStatementCompletionInfo.keywordCompletion), Kind: new(lsproto.CompletionItemKindKeyword), SortText: new(string(SortTextGlobalsOrKeywords))}}}, isNewIdentifierLocation: importStatementCompletionInfo.isNewIdentifierLocation}, nil
			}
			keywordFilters = keywordFiltersFromSyntaxKind(importStatementCompletionInfo.keywordCompletion)
		}
		if importStatementCompletionInfo.replacementSpan != nil && preferences.IncludeCompletionsForImportStatements.IsTrue() {
			importStatementCompletion = &importStatementCompletionInfo
			isNewIdentifierLocation = importStatementCompletionInfo.isNewIdentifierLocation
		}
		if importStatementCompletionInfo.replacementSpan == nil && isCompletionListBlocker(contextToken, previousToken, location, file, position, typeChecker) {
			if keywordFilters != KeywordCompletionFiltersNone {
				isNewIdentifierLocation, _ := computeCommitCharactersAndIsNewIdentifier(contextToken, file, position)
				return keywordCompletionData(keywordFilters, isJSOnlyLocation, isNewIdentifierLocation), nil
			}
			return nil, nil
		}
		parent := contextToken.Parent()
		if contextToken.Kind == ast.KindDotToken || contextToken.Kind == ast.KindQuestionDotToken {
			isRightOfDot = contextToken.Kind == ast.KindDotToken
			isRightOfQuestionDot = contextToken.Kind == ast.KindQuestionDotToken
			switch parent.Kind {
			case ast.KindPropertyAccessExpression:
				propertyAccessToConvert = parent
				node = propertyAccessToConvert.Expression()
				leftMostAccessExpression := ast.GetLeftmostAccessExpression(parent)
				if ast.NodeIsMissing(leftMostAccessExpression) || ((ast.IsCallExpression(node) || ast.IsFunctionLike(node)) && node.End() == contextToken.Pos() && lsutil.GetLastChild(node, file).Kind != ast.KindCloseParenToken) {
					return nil, nil
				}
			case ast.KindQualifiedName:
				node = parent.QualifiedNameLeft()
			case ast.KindModuleDeclaration:
				node = parent.Name()
			case ast.KindImportType:
				node = parent
			case ast.KindMetaProperty:
				node = lsutil.GetFirstToken(parent, file)
				if node.Kind != ast.KindImportKeyword && node.Kind != ast.KindNewKeyword {
					panic("Unexpected token kind: " + node.Kind.String())
				}
			default:
				return nil, nil
			}
		} else if importStatementCompletion == nil {
			if !parent.IsNil() && parent.Kind == ast.KindPropertyAccessExpression {
				contextToken = parent
				parent = parent.Parent()
			}
			if parent == location {
				switch currentToken.Kind {
				case ast.KindGreaterThanToken:
					if parent.Kind == ast.KindJsxElement || parent.Kind == ast.KindJsxOpeningElement {
						location = currentToken
					}
				case ast.KindLessThanSlashToken:
					if parent.Kind == ast.KindJsxSelfClosingElement {
						location = currentToken
					}
				}
			}
			switch parent.Kind {
			case ast.KindJsxClosingElement:
				if contextToken.Kind == ast.KindLessThanSlashToken {
					isStartingCloseTag = true
					location = contextToken
				}
			case ast.KindBinaryExpression:
				if !binaryExpressionMayBeOpenTag(parent) {
					break
				}
				fallthrough
			case ast.KindJsxSelfClosingElement, ast.KindJsxElement, ast.KindJsxOpeningElement:
				isJsxIdentifierExpected = true
				if contextToken.Kind == ast.KindLessThanToken {
					isRightOfOpenTag = true
					location = contextToken
				}
			case ast.KindJsxExpression, ast.KindJsxSpreadAttribute:
				if previousToken.Kind == ast.KindCloseBraceToken || previousToken.Kind == ast.KindIdentifier && previousToken.Parent().Kind == ast.KindJsxAttribute {
					isJsxIdentifierExpected = true
				}
			case ast.KindJsxAttribute:
				if parent.Initializer() == previousToken && previousToken.End() < position {
					isJsxIdentifierExpected = true
				} else {
					switch previousToken.Kind {
					case ast.KindEqualsToken:
						jsxInitializer.isInitializer = true
					case ast.KindIdentifier:
						isJsxIdentifierExpected = true
						if parent != previousToken.Parent() && parent.Initializer().IsNil() && !astnav.FindChildOfKind(parent, ast.KindEqualsToken, file).IsNil() {
							jsxInitializer.initializer = previousToken
						}
					}
				}
			}
		}
	}
	completionKind := CompletionKindNone
	hasUnresolvedAutoImports := false
	var symbols []*ast.Symbol
	var autoImports []*autoimport.FixAndExport
	symbolToOriginInfoMap := map[int]*symbolOriginInfo{}
	symbolToSortTextMap := map[ast.SymbolId]SortText{}
	var seenPropertySymbols collections.Set[ast.SymbolId]
	isTypeOnlyLocation := insideJSDocTagTypeExpression || insideJsDocImportTag || importStatementCompletion != nil && !location.Parent().IsNil() && ast.IsTypeOnlyImportOrExportDeclaration(location.Parent()) || !isContextTokenValueLocation(contextToken) && (isPossiblyTypeArgumentPosition(contextToken, file, typeChecker) || ast.IsPartOfTypeNode(location) || isContextTokenTypeLocation(contextToken))
	addSymbolOriginInfo := func(symbol *ast.Symbol, insertQuestionDot bool, insertAwait bool) {
		symbolId := ast.GetSymbolId(symbol)
		if insertAwait && seenPropertySymbols.AddIfAbsent(symbolId) {
			symbolToOriginInfoMap[len(symbols)-1] = &symbolOriginInfo{kind: getNullableSymbolOriginInfoKind(symbolOriginInfoKindPromise, insertQuestionDot)}
		} else if insertQuestionDot {
			symbolToOriginInfoMap[len(symbols)-1] = &symbolOriginInfo{kind: symbolOriginInfoKindNullable}
		}
	}
	addSymbolSortInfo := func(symbol *ast.Symbol) {
		symbolId := ast.GetSymbolId(symbol)
		if isStaticProperty(symbol) {
			symbolToSortTextMap[symbolId] = SortTextLocalDeclarationPriority
		}
	}
	addPropertySymbol := func(symbol *ast.Symbol, insertAwait bool, insertQuestionDot bool) {
		computedPropertyName := core.FirstNonNil(ast.DeclarationNodes(symbol), func(decl ast.Handle) ast.Handle {
			name := ast.GetNameOfDeclaration(decl)
			if !name.IsNil() && name.Kind == ast.KindComputedPropertyName {
				return name
			}
			return ast.Handle{}
		})
		if !computedPropertyName.IsNil() {
			leftMostName := getLeftMostName(computedPropertyName.Expression())
			var nameSymbol *ast.Symbol
			if !leftMostName.IsNil() {
				nameSymbol = typeChecker.GetSymbolAtLocation(leftMostName)
			}
			var firstAccessibleSymbol *ast.Symbol
			if nameSymbol != nil {
				firstAccessibleSymbol = getFirstSymbolInChain(nameSymbol, contextToken, typeChecker)
			}
			var firstAccessibleSymbolId ast.SymbolId
			if firstAccessibleSymbol != nil {
				firstAccessibleSymbolId = ast.GetSymbolId(firstAccessibleSymbol)
			}
			if firstAccessibleSymbolId != 0 && seenPropertySymbols.AddIfAbsent(firstAccessibleSymbolId) {
				symbols = append(symbols, firstAccessibleSymbol)
				symbolToSortTextMap[firstAccessibleSymbolId] = SortTextGlobalsOrKeywords
				moduleSymbol := firstAccessibleSymbol.Parent
				if moduleSymbol == nil || !checker.IsExternalModuleSymbol(moduleSymbol) || typeChecker.TryGetMemberInModuleExportsAndProperties(firstAccessibleSymbol.Name, moduleSymbol) != firstAccessibleSymbol {
					symbolToOriginInfoMap[len(symbols)-1] = &symbolOriginInfo{kind: getNullableSymbolOriginInfoKind(symbolOriginInfoKindSymbolMember, insertQuestionDot)}
				} else {
				}
			} else if firstAccessibleSymbolId == 0 || !seenPropertySymbols.Has(firstAccessibleSymbolId) {
				symbols = append(symbols, symbol)
				addSymbolOriginInfo(symbol, insertQuestionDot, insertAwait)
				addSymbolSortInfo(symbol)
			}
		} else {
			symbols = append(symbols, symbol)
			addSymbolOriginInfo(symbol, insertQuestionDot, insertAwait)
			addSymbolSortInfo(symbol)
		}
	}
	addTypeProperties := func(t *checker.Type, insertAwait bool, insertQuestionDot bool) {
		if typeChecker.GetStringIndexType(t) != nil {
			isNewIdentifierLocation = true
			defaultCommitCharacters = []string{}
		}
		if isRightOfQuestionDot && len(typeChecker.GetCallSignatures(t)) != 0 {
			isNewIdentifierLocation = true
			if defaultCommitCharacters == nil {
				defaultCommitCharacters = slices.Clone(allCommitCharacters)
			}
		}
		var propertyAccess ast.Handle
		if node.Kind == ast.KindImportType {
			propertyAccess = node
		} else {
			propertyAccess = node.Parent()
		}
		if inCheckedFile {
			for _, symbol := range typeChecker.GetApparentProperties(t) {
				if typeChecker.IsValidPropertyAccessForCompletions(propertyAccess, t, symbol) {
					addPropertySymbol(symbol, false, insertQuestionDot)
				}
			}
		} else {
			for _, symbol := range getPropertiesForCompletion(t, typeChecker) {
				if typeChecker.IsValidPropertyAccessForCompletions(propertyAccess, t, symbol) {
					symbols = append(symbols, symbol)
				}
			}
		}
		if insertAwait {
			promiseType := typeChecker.GetPromisedTypeOfPromise(t)
			if promiseType != nil {
				for _, symbol := range typeChecker.GetApparentProperties(promiseType) {
					if typeChecker.IsValidPropertyAccessForCompletions(propertyAccess, promiseType, symbol) {
						addPropertySymbol(symbol, true, insertQuestionDot)
					}
				}
			}
		}
	}
	getTypeScriptMemberSymbols := func() {
		completionKind = CompletionKindPropertyAccess
		isImportType := ast.IsLiteralImportTypeNode(node)
		isTypeLocation := (isImportType && !node.ImportTypeNodeIsTypeOf()) || ast.IsPartOfTypeNode(node.Parent()) || isPossiblyTypeArgumentPosition(contextToken, file, typeChecker)
		isRhsOfImportDeclaration := isInRightSideOfInternalImportEqualsDeclaration(node)
		if ast.IsEntityName(node) || isImportType || ast.IsPropertyAccessExpression(node) {
			isNamespaceName := ast.IsModuleDeclaration(node.Parent())
			if isNamespaceName {
				isNewIdentifierLocation = true
				defaultCommitCharacters = []string{}
			}
			symbol := typeChecker.GetSymbolAtLocation(node)
			if symbol != nil {
				symbol := checker.SkipAlias(symbol, typeChecker)
				if symbol.Flags&(ast.SymbolFlagsModule|ast.SymbolFlagsEnum) != 0 {
					var valueAccessNode ast.Handle
					if isImportType {
						valueAccessNode = node
					} else {
						valueAccessNode = node.Parent()
					}
					exportedSymbols := typeChecker.GetExportsOfModule(symbol)
					for _, exportedSymbol := range exportedSymbols {
						if exportedSymbol == nil {
							panic("getExporsOfModule() should all be defined")
						}
						isValidValueAccess := func(s *ast.Symbol) bool {
							return typeChecker.IsValidPropertyAccess(valueAccessNode, s.Name)
						}
						isValidTypeAccess := func(s *ast.Symbol) bool {
							return symbolCanBeReferencedAtTypeLocation(s, typeChecker, collections.Set[ast.SymbolId]{})
						}
						var isValidAccess bool
						if isNamespaceName {
							isValidAccess = exportedSymbol.Flags&ast.SymbolFlagsNamespace != 0 && !ast.EveryDeclaration(exportedSymbol, func(declaration ast.Handle) bool {
								return declaration.Parent() == node.Parent()
							})
						} else if isRhsOfImportDeclaration {
							isValidAccess = isValidTypeAccess(exportedSymbol) || isValidValueAccess(exportedSymbol)
						} else if isTypeLocation || insideJSDocTagTypeExpression {
							isValidAccess = isValidTypeAccess(exportedSymbol)
						} else {
							isValidAccess = isValidValueAccess(exportedSymbol)
						}
						if isValidAccess {
							symbols = append(symbols, exportedSymbol)
						}
					}
					if !isTypeLocation && !insideJSDocTagTypeExpression && ast.SomeDeclaration(symbol, func(decl ast.Handle) bool {
						return decl.Kind != ast.KindSourceFile && decl.Kind != ast.KindModuleDeclaration && decl.Kind != ast.KindEnumDeclaration
					}) {
						t := typeChecker.GetNonOptionalType(typeChecker.GetTypeOfSymbolAtLocation(symbol, node))
						insertQuestionDot := false
						if typeChecker.IsNullableType(t) {
							canCorrectToQuestionDot := isRightOfDot && !isRightOfQuestionDot && !preferences.IncludeAutomaticOptionalChainCompletions.IsFalse()
							if canCorrectToQuestionDot || isRightOfQuestionDot {
								t = typeChecker.GetNonNullableType(t)
								if canCorrectToQuestionDot {
									insertQuestionDot = true
								}
							}
						}
						addTypeProperties(t, node.Flags()&ast.NodeFlagsAwaitContext != 0, insertQuestionDot)
					}
					return
				}
			}
		}
		if !isTypeLocation || checker.IsInTypeQuery(node) {
			typeChecker.TryGetThisTypeAtEx(node, false, ast.Handle{})
			t := typeChecker.GetNonOptionalType(typeChecker.GetTypeAtLocation(node))
			if !isTypeLocation {
				insertQuestionDot := false
				if typeChecker.IsNullableType(t) {
					canCorrectToQuestionDot := isRightOfDot && !isRightOfQuestionDot && !preferences.IncludeAutomaticOptionalChainCompletions.IsFalse()
					if canCorrectToQuestionDot || isRightOfQuestionDot {
						t = typeChecker.GetNonNullableType(t)
						if canCorrectToQuestionDot {
							insertQuestionDot = true
						}
					}
				}
				addTypeProperties(t, node.Flags()&ast.NodeFlagsAwaitContext != 0, insertQuestionDot)
			} else {
				addTypeProperties(typeChecker.GetNonNullableType(t), false, false)
			}
		}
	}
	tryGetObjectTypeLiteralInTypeArgumentCompletionSymbols := func() (globalsSearch, error) {
		typeLiteralNode := tryGetTypeLiteralNode(contextToken)
		if typeLiteralNode.IsNil() {
			return globalsSearchContinue, nil
		}
		intersectionTypeNode := core.IfElse(ast.IsIntersectionTypeNode(typeLiteralNode.Parent()), typeLiteralNode.Parent(), ast.Handle{})
		containerTypeNode := core.IfElse(!intersectionTypeNode.IsNil(), intersectionTypeNode, typeLiteralNode)
		containerExpectedType := getConstraintOfTypeArgumentProperty(containerTypeNode, typeChecker)
		if containerExpectedType == nil {
			return globalsSearchContinue, nil
		}
		containerActualType := typeChecker.GetTypeFromTypeNode(containerTypeNode)
		members := getPropertiesForCompletion(containerExpectedType, typeChecker)
		existingMembers := getPropertiesForCompletion(containerActualType, typeChecker)
		existingMemberNames := collections.Set[string]{}
		for _, member := range existingMembers {
			existingMemberNames.Add(member.Name)
		}
		symbols = append(symbols, core.Filter(members, func(member *ast.Symbol) bool {
			return !existingMemberNames.Has(member.Name)
		})...)
		completionKind = CompletionKindObjectPropertyDeclaration
		isNewIdentifierLocation = true
		return globalsSearchSuccess, nil
	}
	tryGetObjectLikeCompletionSymbols := func() (globalsSearch, error) {
		if !contextToken.IsNil() && contextToken.Kind == ast.KindDotDotDotToken {
			return globalsSearchContinue, nil
		}
		objectLikeContainer := tryGetObjectLikeCompletionContainer(contextToken, position, file)
		if objectLikeContainer.IsNil() {
			return globalsSearchContinue, nil
		}
		completionKind = CompletionKindObjectPropertyDeclaration
		var typeMembers []*ast.Symbol
		var existingMembers []ast.Handle
		if objectLikeContainer.Kind == ast.KindObjectLiteralExpression {
			instantiatedType := tryGetObjectLiteralContextualType(objectLikeContainer, typeChecker)
			if instantiatedType == nil {
				if objectLikeContainer.Flags()&ast.NodeFlagsInWithStatement != 0 {
					return globalsSearchFail, nil
				}
				return globalsSearchContinue, nil
			}
			completionsType := typeChecker.GetContextualType(objectLikeContainer, checker.ContextFlagsIgnoreNodeInferences)
			t := core.IfElse(completionsType != nil, completionsType, instantiatedType)
			stringIndexType := typeChecker.GetStringIndexType(t)
			numberIndexType := typeChecker.GetNumberIndexType(t)
			isNewIdentifierLocation = stringIndexType != nil || numberIndexType != nil
			typeMembers = getPropertiesForObjectExpression(instantiatedType, completionsType, objectLikeContainer, typeChecker)
			existingMembers = objectLikeContainer.Properties()
			if len(typeMembers) == 0 {
				if numberIndexType == nil {
					return globalsSearchContinue, nil
				}
			}
		} else {
			if objectLikeContainer.Kind != ast.KindObjectBindingPattern {
				panic("Expected 'objectLikeContainer' to be an object binding pattern.")
			}
			isNewIdentifierLocation = false
			rootDeclaration := ast.GetRootDeclaration(objectLikeContainer.Parent())
			if !ast.IsVariableLike(rootDeclaration) {
				panic("Root declaration is not variable-like.")
			}
			canGetType := ast.HasInitializer(rootDeclaration) || !ast.GetTypeAnnotationNode(rootDeclaration).IsNil() || rootDeclaration.Parent().Parent().Kind == ast.KindForOfStatement
			if !canGetType && rootDeclaration.Kind == ast.KindParameter {
				if ast.IsExpression(rootDeclaration.Parent()) {
					canGetType = typeChecker.GetContextualType(rootDeclaration.Parent(), checker.ContextFlagsNone) != nil
				} else if rootDeclaration.Parent().Kind == ast.KindMethodDeclaration || rootDeclaration.Parent().Kind == ast.KindSetAccessor {
					canGetType = ast.IsExpression(rootDeclaration.Parent().Parent()) && typeChecker.GetContextualType(rootDeclaration.Parent().Parent(), checker.ContextFlagsNone) != nil
				}
			}
			if canGetType {
				typeForObject := typeChecker.GetTypeAtLocation(objectLikeContainer)
				if typeForObject == nil {
					return globalsSearchFail, nil
				}
				typeMembers = core.Filter(typeChecker.GetPropertiesOfType(typeForObject), func(propertySymbol *ast.Symbol) bool {
					return typeChecker.IsPropertyAccessible(objectLikeContainer, false, false, typeForObject, propertySymbol)
				})
				existingMembers = objectLikeContainer.Elements()
			}
		}
		if len(typeMembers) > 0 {
			filteredMembers, spreadMemberNames := filterObjectMembersList(typeMembers, existingMembers, file, position, typeChecker)
			symbols = append(symbols, filteredMembers...)
			for _, member := range filteredMembers {
				symbolId := ast.GetSymbolId(member)
				if spreadMemberNames.Has(member.Name) {
					symbolToSortTextMap[symbolId] = SortTextMemberDeclaredBySpreadAssignment
				}
				if member.Flags&ast.SymbolFlagsOptional != 0 {
					_, ok := symbolToSortTextMap[symbolId]
					if !ok {
						symbolToSortTextMap[symbolId] = SortTextOptionalMember
					}
				}
				if objectLikeContainer.Kind == ast.KindObjectLiteralExpression && preferences.IncludeCompletionsWithObjectLiteralMethodSnippets.IsTrue() {
					displayName, _ := getCompletionEntryDisplayNameForSymbol(member, nil, CompletionKindObjectPropertyDeclaration, false)
					if displayName != "" {
						originalSortText := core.OrElse(symbolToSortTextMap[symbolId], SortTextLocationPriority)
						symbolToSortTextMap[symbolId] = ObjectLiteralPropertySortText(originalSortText, displayName)
					}
				}
			}
			if objectLikeContainer.Kind == ast.KindObjectLiteralExpression && preferences.IncludeCompletionsWithObjectLiteralMethodSnippets.IsTrue() {
				for _, entry := range l.collectObjectLiteralMethodSymbols(ctx, typeChecker, filteredMembers, objectLikeContainer, file) {
					symbolToOriginInfoMap[len(symbols)] = entry.origin
					symbols = append(symbols, entry.symbol)
				}
			}
		}
		return globalsSearchSuccess, nil
	}
	shouldOfferImportCompletions := func() bool {
		if tspath.IsDynamicFileName(file.FileName()) {
			return false
		}
		if importStatementCompletion != nil {
			return true
		}
		if preferences.IncludeCompletionsForModuleExports.IsFalse() {
			return false
		}
		return true
	}
	collectAutoImports := func() error {
		if forItemResolve {
			return nil
		}
		if !shouldOfferImportCompletions() {
			return nil
		}
		var lowerCaseTokenText string
		usagePosition, fidelity := l.createLspPosition(position, file)
		if !fidelity.IsExact() {
			return nil
		}
		if !previousToken.IsNil() && ast.IsIdentifier(previousToken) {
			usagePosition, fidelity = l.createLspPosition(scanner.GetTokenPosOfNode(previousToken, file, false), file)
			if !fidelity.IsExact() {
				return nil
			}
			if !(previousToken == contextToken && importStatementCompletion != nil) {
				lowerCaseTokenText = strings.ToLower(previousToken.Text())
			}
		}
		view, err := l.getPreparedAutoImportView(file)
		if err != nil {
			return err
		}
		if view == nil {
			return nil
		}
		autoImports = view.GetCompletions(ctx, lowerCaseTokenText, usagePosition, isRightOfOpenTag, isTypeOnlyLocation)
		return nil
	}
	tryGetImportCompletionSymbols := func() (globalsSearch, error) {
		if importStatementCompletion == nil {
			return globalsSearchContinue, nil
		}
		isNewIdentifierLocation = true
		if err := collectAutoImports(); err != nil {
			return globalsSearchFail, err
		}
		return globalsSearchSuccess, nil
	}
	tryGetImportOrExportClauseCompletionSymbols := func() (globalsSearch, error) {
		if contextToken.IsNil() {
			return globalsSearchContinue, nil
		}
		var namedImportsOrExports ast.Handle
		if contextToken.Kind == ast.KindOpenBraceToken || contextToken.Kind == ast.KindCommaToken {
			namedImportsOrExports = core.IfElse(isNamedImportsOrExports(contextToken.Parent()), contextToken.Parent(), ast.Handle{})
		} else if isTypeKeywordTokenOrIdentifier(contextToken) {
			namedImportsOrExports = core.IfElse(isNamedImportsOrExports(contextToken.Parent().Parent()), contextToken.Parent().Parent(), ast.Handle{})
		}
		if namedImportsOrExports.IsNil() {
			return globalsSearchContinue, nil
		}
		if !isTypeKeywordTokenOrIdentifier(contextToken) {
			keywordFilters = KeywordCompletionFiltersTypeKeyword
		}
		moduleSpecifier := core.IfElse(namedImportsOrExports.Kind == ast.KindNamedImports, namedImportsOrExports.Parent().Parent(), namedImportsOrExports.Parent()).ModuleSpecifier()
		if moduleSpecifier.IsNil() {
			isNewIdentifierLocation = true
			if namedImportsOrExports.Kind == ast.KindNamedImports {
				return globalsSearchFail, nil
			}
			return globalsSearchContinue, nil
		}
		moduleSpecifierSymbol := typeChecker.GetSymbolAtLocation(moduleSpecifier)
		if moduleSpecifierSymbol == nil {
			isNewIdentifierLocation = true
			return globalsSearchFail, nil
		}
		completionKind = CompletionKindMemberLike
		isNewIdentifierLocation = false
		exports := typeChecker.GetExportsAndPropertiesOfModule(moduleSpecifierSymbol)
		existing := collections.Set[string]{}
		for _, element := range namedImportsOrExports.Elements() {
			if isCurrentlyEditingNode(element, file, position) {
				continue
			}
			existing.Add(element.PropertyNameOrName().Text())
		}
		uniques := core.Filter(exports, func(symbol *ast.Symbol) bool {
			return ast.SymbolName(symbol) != ast.InternalSymbolNameDefault && !existing.Has(ast.SymbolName(symbol))
		})
		symbols = append(symbols, uniques...)
		if len(uniques) == 0 {
			keywordFilters = KeywordCompletionFiltersNone
		}
		return globalsSearchSuccess, nil
	}
	tryGetImportAttributesCompletionSymbols := func() (globalsSearch, error) {
		if contextToken.IsNil() {
			return globalsSearchContinue, nil
		}
		var importAttributes ast.Handle
		switch contextToken.Kind {
		case ast.KindOpenBraceToken, ast.KindCommaToken:
			importAttributes = contextToken.Parent()
		case ast.KindColonToken:
			importAttributes = contextToken.Parent().Parent()
		}
		if importAttributes.IsNil() || !ast.IsImportAttributes(importAttributes) {
			return globalsSearchContinue, nil
		}
		var elements []ast.Handle
		if importAttributes.ImportAttributesAttributes() != 0 {
			elements = importAttributes.Store().ListSlice(importAttributes.ImportAttributesAttributes())
		}
		attributeNames := core.Map(elements, func(el ast.Handle) string {
			return el.ImportAttributeName().Text()
		})
		existing := collections.NewSetFromItems(attributeNames...)
		uniques := core.Filter(typeChecker.GetApparentProperties(typeChecker.GetTypeAtLocation(importAttributes)), func(symbol *ast.Symbol) bool {
			return !existing.Has(ast.SymbolName(symbol))
		})
		symbols = append(symbols, uniques...)
		return globalsSearchSuccess, nil
	}
	tryGetLocalNamedExportCompletionSymbols := func() (globalsSearch, error) {
		if contextToken.IsNil() {
			return globalsSearchContinue, nil
		}
		var namedExports ast.Handle
		if contextToken.Kind == ast.KindOpenBraceToken || contextToken.Kind == ast.KindCommaToken {
			namedExports = core.IfElse(ast.IsNamedExports(contextToken.Parent()), contextToken.Parent(), ast.Handle{})
		}
		if namedExports.IsNil() {
			return globalsSearchContinue, nil
		}
		localsContainer := ast.FindAncestor(namedExports, func(node ast.Handle) bool {
			return ast.IsSourceFile(node) || ast.IsModuleDeclaration(node)
		})
		completionKind = CompletionKindNone
		isNewIdentifierLocation = false
		localSymbol := localsContainer.Symbol()
		var localExports ast.SymbolTable
		if localSymbol != nil {
			localExports = localSymbol.Exports
		}
		for name, symbol := range localsContainer.Locals() {
			symbols = append(symbols, symbol)
			if _, ok := localExports[name]; ok {
				symbolId := ast.GetSymbolId(symbol)
				symbolToSortTextMap[symbolId] = SortTextOptionalMember
			}
		}
		return globalsSearchSuccess, nil
	}
	tryGetConstructorCompletion := func() (globalsSearch, error) {
		if tryGetConstructorLikeCompletionContainer(contextToken).IsNil() {
			return globalsSearchContinue, nil
		}
		completionKind = CompletionKindNone
		isNewIdentifierLocation = true
		keywordFilters = KeywordCompletionFiltersConstructorParameterKeywords
		return globalsSearchSuccess, nil
	}
	tryGetClassLikeCompletionSymbols := func() (globalsSearch, error) {
		decl := tryGetObjectTypeDeclarationCompletionContainer(file, contextToken, location, position)
		if decl.IsNil() {
			return globalsSearchContinue, nil
		}
		completionKind = CompletionKindMemberLike
		isNewIdentifierLocation = true
		if contextToken.Kind == ast.KindAsteriskToken {
			keywordFilters = KeywordCompletionFiltersNone
		} else if ast.IsClassLike(decl) {
			keywordFilters = KeywordCompletionFiltersClassElementKeywords
		} else {
			keywordFilters = KeywordCompletionFiltersInterfaceElementKeywords
		}
		if !ast.IsClassLike(decl) {
			return globalsSearchSuccess, nil
		}
		var classElement ast.Handle
		if contextToken.Kind == ast.KindSemicolonToken {
			classElement = contextToken.Parent().Parent()
		} else {
			classElement = contextToken.Parent()
		}
		var classElementModifierFlags ast.ModifierFlags
		if ast.IsClassElement(classElement) {
			classElementModifierFlags = classElement.ModifierFlags()
		}
		if contextToken.Kind == ast.KindIdentifier && !isCurrentlyEditingNode(contextToken, file, position) {
			switch contextToken.Text() {
			case "private":
				classElementModifierFlags |= ast.ModifierFlagsPrivate
			case "static":
				classElementModifierFlags |= ast.ModifierFlagsStatic
			case "override":
				classElementModifierFlags |= ast.ModifierFlagsOverride
			}
		}
		if ast.IsClassStaticBlockDeclaration(classElement) {
			classElementModifierFlags |= ast.ModifierFlagsStatic
		}
		if classElementModifierFlags&ast.ModifierFlagsPrivate == 0 {
			var baseTypeNodes []ast.Handle
			if ast.IsClassLike(decl) && classElementModifierFlags&ast.ModifierFlagsOverride != 0 {
				if el := ast.GetClassExtendsHeritageElement(decl); !el.IsNil() {
					baseTypeNodes = []ast.Handle{el}
				}
			} else {
				baseTypeNodes = getAllSuperTypeNodes(decl)
			}
			var baseSymbols []*ast.Symbol
			for _, baseTypeNode := range baseTypeNodes {
				t := typeChecker.GetTypeAtLocation(baseTypeNode)
				if classElementModifierFlags&ast.ModifierFlagsStatic != 0 {
					if t.Symbol() != nil {
						baseSymbols = append(baseSymbols, typeChecker.GetPropertiesOfType(typeChecker.GetTypeOfSymbolAtLocation(t.Symbol(), decl))...)
					}
				} else if t != nil {
					baseSymbols = append(baseSymbols, typeChecker.GetPropertiesOfType(t)...)
				}
			}
			symbols = append(symbols, filterClassMembersList(baseSymbols, decl.Members(), classElementModifierFlags, file, position)...)
			for index, symbol := range symbols {
				declaration := ast.NodeOf(symbol.ValueDeclaration)
				if !declaration.IsNil() && ast.IsClassElement(declaration) && !declaration.Name().IsNil() && ast.IsComputedPropertyName(declaration.Name()) {
					origin := &symbolOriginInfo{kind: symbolOriginInfoKindComputedPropertyName, data: &symbolOriginInfoComputedPropertyName{symbolName: typeChecker.SymbolToString(symbol)}}
					symbolToOriginInfoMap[index] = origin
				}
			}
		}
		return globalsSearchSuccess, nil
	}
	tryGetJsxCompletionSymbols := func() (globalsSearch, error) {
		jsxContainer := tryGetContainingJsxElement(contextToken, file)
		if jsxContainer.IsNil() {
			return globalsSearchContinue, nil
		}
		attrsType := typeChecker.GetContextualType(jsxContainer.Attributes(), checker.ContextFlagsNone)
		if attrsType == nil {
			return globalsSearchContinue, nil
		}
		completionsType := typeChecker.GetContextualType(jsxContainer.Attributes(), checker.ContextFlagsIgnoreNodeInferences)
		filteredSymbols, spreadMemberNames := filterJsxAttributes(getPropertiesForObjectExpression(attrsType, completionsType, jsxContainer.Attributes(), typeChecker), jsxContainer.Attributes().Properties(), file, position, typeChecker)
		symbols = append(symbols, filteredSymbols...)
		for _, symbol := range filteredSymbols {
			symbolId := ast.GetSymbolId(symbol)
			if spreadMemberNames.Has(ast.SymbolName(symbol)) {
				symbolToSortTextMap[symbolId] = SortTextMemberDeclaredBySpreadAssignment
			}
			if symbol.Flags&ast.SymbolFlagsOptional != 0 {
				_, ok := symbolToSortTextMap[symbolId]
				if !ok {
					symbolToSortTextMap[symbolId] = SortTextOptionalMember
				}
			}
		}
		completionKind = CompletionKindMemberLike
		isNewIdentifierLocation = false
		return globalsSearchSuccess, nil
	}
	getGlobalCompletions := func() (globalsSearch, error) {
		if !tryGetFunctionLikeBodyCompletionContainer(contextToken).IsNil() {
			keywordFilters = KeywordCompletionFiltersFunctionLikeBodyKeywords
		} else {
			keywordFilters = KeywordCompletionFiltersAll
		}
		completionKind = CompletionKindGlobal
		isNewIdentifierLocation, defaultCommitCharacters = computeCommitCharactersAndIsNewIdentifier(contextToken, file, position)
		if previousToken != contextToken {
			if previousToken.IsNil() {
				panic("Expected 'contextToken' to be defined when different from 'previousToken'.")
			}
		}
		var adjustedPosition int
		if previousToken != contextToken {
			adjustedPosition = astnav.GetStartOfNode(previousToken, file, false)
		} else {
			adjustedPosition = position
		}
		scopeNode := getScopeNode(contextToken, adjustedPosition, file)
		if scopeNode.IsNil() {
			scopeNode = file.ParseRoot()
		}
		isInSnippetScope = isSnippetScope(scopeNode)
		symbolMeanings := core.IfElse(isTypeOnlyLocation, ast.SymbolFlagsNone, ast.SymbolFlagsValue) | ast.SymbolFlagsType | ast.SymbolFlagsNamespace | ast.SymbolFlagsAlias
		typeOnlyAliasNeedsPromotion := !previousToken.IsNil() && !ast.IsValidTypeOnlyAliasUseSite(previousToken)
		symbols = append(symbols, typeChecker.GetSymbolsInScope(scopeNode, symbolMeanings)...)
		core.CheckEachDefined(symbols, "getSymbolsInScope() should all be defined")
		for index, symbol := range symbols {
			symbolId := ast.GetSymbolId(symbol)
			if !typeChecker.IsArgumentsSymbol(symbol) && !ast.SomeDeclaration(symbol, func(decl ast.Handle) bool {
				return ast.GetSourceFileOfNode(decl) == file
			}) {
				symbolToSortTextMap[symbolId] = SortTextGlobalsOrKeywords
			}
			if typeOnlyAliasNeedsPromotion && symbol.Flags&ast.SymbolFlagsValue == 0 {
				typeOnlyAliasDeclaration := ast.FindSymbolDeclaration(symbol, ast.IsTypeOnlyImportDeclaration)
				if !typeOnlyAliasDeclaration.IsNil() {
					origin := &symbolOriginInfo{kind: symbolOriginInfoKindTypeOnlyAlias, data: &symbolOriginInfoTypeOnlyAlias{declaration: typeOnlyAliasDeclaration}}
					symbolToOriginInfoMap[index] = origin
				}
			}
		}
		if scopeNode.Kind != ast.KindSourceFile {
			thisType := typeChecker.TryGetThisTypeAtEx(scopeNode, false, core.IfElse(ast.IsClassLike(scopeNode.Parent()), scopeNode, ast.Handle{}))
			if thisType != nil && !isProbablyGlobalType(thisType, file, typeChecker) {
				for _, symbol := range getPropertiesForCompletion(thisType, typeChecker) {
					symbolId := ast.GetSymbolId(symbol)
					symbols = append(symbols, symbol)
					symbolToOriginInfoMap[len(symbols)-1] = &symbolOriginInfo{kind: symbolOriginInfoKindThisType}
					symbolToSortTextMap[symbolId] = SortTextSuggestedClassMembers
				}
			}
		}
		if err := collectAutoImports(); err != nil {
			return globalsSearchFail, err
		}
		if isTypeOnlyLocation {
			if !contextToken.IsNil() && ast.IsAssertionExpression(contextToken.Parent()) {
				keywordFilters = KeywordCompletionFiltersTypeAssertionKeywords
			} else {
				keywordFilters = KeywordCompletionFiltersTypeKeywords
			}
		}
		return globalsSearchSuccess, nil
	}
	tryGetGlobalSymbols := func() (bool, error) {
		var result globalsSearch
		var err error
		globalSearchFuncs := []func() (globalsSearch, error){tryGetObjectTypeLiteralInTypeArgumentCompletionSymbols, tryGetObjectLikeCompletionSymbols, tryGetImportCompletionSymbols, tryGetImportOrExportClauseCompletionSymbols, tryGetImportAttributesCompletionSymbols, tryGetLocalNamedExportCompletionSymbols, tryGetConstructorCompletion, tryGetClassLikeCompletionSymbols, tryGetJsxCompletionSymbols, getGlobalCompletions}
		for _, globalSearchFunc := range globalSearchFuncs {
			result, err = globalSearchFunc()
			if err != nil {
				return false, err
			}
			if result != globalsSearchContinue {
				break
			}
		}
		return result == globalsSearchSuccess, nil
	}
	if isRightOfDot || isRightOfQuestionDot {
		getTypeScriptMemberSymbols()
	} else if isRightOfOpenTag {
		symbols = typeChecker.GetJsxIntrinsicTagNamesAt(location)
		core.CheckEachDefined(symbols, "GetJsxIntrinsicTagNamesAt() should all be defined")
		if _, err := tryGetGlobalSymbols(); err != nil {
			return nil, err
		}
		completionKind = CompletionKindGlobal
		keywordFilters = KeywordCompletionFiltersNone
	} else if isStartingCloseTag {
		tagName := contextToken.Parent().Parent().JsxElementOpeningElement().TagName()
		tagSymbol := typeChecker.GetSymbolAtLocation(tagName)
		if tagSymbol != nil {
			symbols = []*ast.Symbol{tagSymbol}
		}
		completionKind = CompletionKindGlobal
		keywordFilters = KeywordCompletionFiltersNone
	} else {
		if ok, err := tryGetGlobalSymbols(); !ok {
			if err != nil {
				return nil, err
			}
			if keywordFilters != KeywordCompletionFiltersNone {
				return keywordCompletionData(keywordFilters, isJSOnlyLocation, isNewIdentifierLocation), nil
			}
			return nil, nil
		}
	}
	var contextualTypeOrConstraint *checker.Type
	if !previousToken.IsNil() {
		contextualTypeOrConstraint = getContextualType(previousToken, position, file, typeChecker)
		if contextualTypeOrConstraint == nil {
			contextualTypeOrConstraint = getConstraintOfTypeArgumentProperty(previousToken, typeChecker)
		}
	}
	isLiteralExpected := !(!previousToken.IsNil() && ast.IsStringLiteralLike(previousToken)) && !isJsxIdentifierExpected
	var literals []literalValue
	if isLiteralExpected {
		var types []*checker.Type
		if contextualTypeOrConstraint != nil && contextualTypeOrConstraint.IsUnion() {
			types = contextualTypeOrConstraint.Types()
		} else if contextualTypeOrConstraint != nil {
			types = []*checker.Type{contextualTypeOrConstraint}
		}
		literals = core.MapNonNil(types, func(t *checker.Type) literalValue {
			if isLiteral(t) && !t.IsEnumLiteral() {
				return t.AsLiteralType().Value()
			}
			return nil
		})
	}
	var recommendedCompletion *ast.Symbol
	if !previousToken.IsNil() && contextualTypeOrConstraint != nil {
		recommendedCompletion = getRecommendedCompletion(previousToken, contextualTypeOrConstraint, typeChecker)
	}
	if defaultCommitCharacters == nil {
		defaultCommitCharacters = getDefaultCommitCharacters(isNewIdentifierLocation)
	}
	return &completionDataData{symbols: symbols, autoImports: autoImports, completionKind: completionKind, isInSnippetScope: isInSnippetScope, propertyAccessToConvert: propertyAccessToConvert, isNewIdentifierLocation: isNewIdentifierLocation, location: location, keywordFilters: keywordFilters, literals: literals, symbolToOriginInfoMap: symbolToOriginInfoMap, symbolToSortTextMap: symbolToSortTextMap, recommendedCompletion: recommendedCompletion, previousToken: previousToken, contextToken: contextToken, jsxInitializer: jsxInitializer, insideJSDocTagTypeExpression: insideJSDocTagTypeExpression, isTypeOnlyLocation: isTypeOnlyLocation, isJsxIdentifierExpected: isJsxIdentifierExpected, isRightOfOpenTag: isRightOfOpenTag, isRightOfDotOrQuestionDot: isRightOfDot || isRightOfQuestionDot, importStatementCompletion: importStatementCompletion, hasUnresolvedAutoImports: hasUnresolvedAutoImports, defaultCommitCharacters: defaultCommitCharacters}, nil
}
func keywordCompletionData(keywordFilters KeywordCompletionFilters, filterOutTSOnlyKeywords bool, isNewIdentifierLocation bool) *completionDataKeyword {
	return &completionDataKeyword{keywordCompletions: getKeywordCompletions(keywordFilters, filterOutTSOnlyKeywords), isNewIdentifierLocation: isNewIdentifierLocation}
}
func getDefaultCommitCharacters(isNewIdentifierLocation bool) []string {
	if isNewIdentifierLocation {
		return []string{}
	}
	return slices.Clone(allCommitCharacters)
}
func (l *LanguageService) completionInfoFromData(ctx context.Context, typeChecker *checker.Checker, file *ast.SourceFile, compilerOptions *core.CompilerOptions, data *completionDataData, position int, optionalReplacementSpan *lsproto.Range, includeSymbols bool) (*CompletionList, error) {
	keywordFilters := data.keywordFilters
	isNewIdentifierLocation := data.isNewIdentifierLocation
	contextToken := data.contextToken
	literals := data.literals
	preferences := l.UserPreferences()
	if file.LanguageVariant == core.LanguageVariantJSX {
		list := l.getJsxClosingTagCompletion(ctx, data.location, file, position)
		if list != nil {
			return list, nil
		}
	}
	caseClause := ast.FindAncestor(contextToken, ast.IsCaseClause)
	if !caseClause.IsNil() && (contextToken.Kind == ast.KindCaseKeyword || ast.IsNodeDescendantOf(contextToken, caseClause.Expression())) {
		tracker := newCaseClauseTracker(typeChecker, caseClause.Store().ListSlice(caseClause.Parent().CaseBlockClauses()))
		literals = core.Filter(literals, func(literal literalValue) bool {
			return !tracker.hasValue(literal)
		})
		data.symbols = core.Filter(data.symbols, func(symbol *ast.Symbol) bool {
			if symbol.ValueDeclaration != 0 && ast.IsEnumMember(ast.NodeOf(symbol.ValueDeclaration)) {
				value := typeChecker.GetConstantValue(ast.NodeOf(symbol.ValueDeclaration))
				if value != nil && tracker.hasValue(value) {
					return false
				}
			}
			return true
		})
	}
	isChecked := isCheckedFile(file, compilerOptions)
	if isChecked && !isNewIdentifierLocation && len(data.symbols) == 0 && keywordFilters == KeywordCompletionFiltersNone {
		return nil, nil
	}
	uniqueNames, sortedEntries, err := l.getCompletionEntriesFromSymbols(ctx, typeChecker, data, ast.Handle{}, position, file, compilerOptions, includeSymbols)
	if err != nil {
		return nil, err
	}
	if data.keywordFilters != KeywordCompletionFiltersNone {
		keywordCompletions := getKeywordCompletions(data.keywordFilters, !data.insideJSDocTagTypeExpression && ast.IsSourceFileJS(file))
		for _, keywordEntry := range keywordCompletions {
			if data.isTypeOnlyLocation && isTypeKeyword(scanner.StringToToken(keywordEntry.Label)) || !data.isTypeOnlyLocation && isContextualKeywordInAutoImportableExpressionSpace(keywordEntry.Label) || !uniqueNames.Has(keywordEntry.Label) {
				uniqueNames.Add(keywordEntry.Label)
				sortedEntries = append(sortedEntries, keywordEntry)
			}
		}
	}
	for _, keywordEntry := range getContextualKeywords(file, contextToken, position) {
		if !uniqueNames.Has(keywordEntry.Label) {
			uniqueNames.Add(keywordEntry.Label)
			sortedEntries = append(sortedEntries, &CompletionItem{CompletionItem: keywordEntry})
		}
	}
	for _, literal := range literals {
		literalEntry := createCompletionItemForLiteral(file, preferences, literal)
		uniqueNames.Add(literalEntry.Label)
		sortedEntries = append(sortedEntries, &CompletionItem{CompletionItem: literalEntry})
	}
	if !isChecked {
		sortedEntries = l.getJSCompletionEntries(ctx, file, position, &uniqueNames, sortedEntries)
	}
	if !contextToken.IsNil() && !data.isRightOfOpenTag && !data.isRightOfDotOrQuestionDot {
		if caseBlock := ast.FindAncestorKind(contextToken, ast.KindCaseBlock); !caseBlock.IsNil() {
			casesItem, err := l.getExhaustiveCaseSnippets(ctx, caseBlock, file, position, compilerOptions, l.program, typeChecker)
			if err != nil {
				return nil, err
			}
			if casesItem != nil {
				sortedEntries = append(sortedEntries, &CompletionItem{CompletionItem: casesItem})
			}
		}
	}
	itemDefaults := l.setItemDefaults(ctx, position, file, sortedEntries, &data.defaultCommitCharacters, optionalReplacementSpan)
	return &CompletionList{IsIncomplete: data.hasUnresolvedAutoImports, ItemDefaults: itemDefaults, Items: sortedEntries}, nil
}
func (l *LanguageService) getCompletionEntriesFromSymbols(ctx context.Context, typeChecker *checker.Checker, data *completionDataData, replacementToken ast.Handle, position int, file *ast.SourceFile, compilerOptions *core.CompilerOptions, includeSymbols bool) (uniqueNames collections.Set[string], sortedEntries []*CompletionItem, err error) {
	closestSymbolDeclaration := getClosestSymbolDeclaration(data.contextToken, data.location)
	useSemicolons := lsutil.ProbablyUsesSemicolons(file)
	preferences := l.UserPreferences()
	isMemberCompletion := isMemberCompletionKind(data.completionKind)
	sortedEntries = slices.Grow(sortedEntries, len(data.symbols)+len(data.autoImports))
	uniques := make(uniqueNamesMap)
	for index, symbol := range data.symbols {
		origin := data.symbolToOriginInfoMap[index]
		name, needsConvertPropertyAccess := getCompletionEntryDisplayNameForSymbol(symbol, origin, data.completionKind, data.isJsxIdentifierExpected)
		if name == "" || uniques[name] && (origin == nil || !originIsObjectLiteralMethod(origin)) || data.completionKind == CompletionKindGlobal && !shouldIncludeSymbol(symbol, data, closestSymbolDeclaration, file, typeChecker, compilerOptions) {
			continue
		}
		if !data.isTypeOnlyLocation && ast.IsSourceFileJS(file) && symbolAppearsToBeTypeOnly(symbol, typeChecker) {
			continue
		}
		originalSortText := data.symbolToSortTextMap[ast.GetSymbolId(symbol)]
		if originalSortText == "" {
			originalSortText = SortTextLocationPriority
		}
		var sortText SortText
		if isDeprecated(symbol, typeChecker) {
			sortText = DeprecateSortText(originalSortText)
		} else {
			sortText = originalSortText
		}
		entry, err := l.createCompletionItem(ctx, typeChecker, symbol, sortText, replacementToken, data, position, file, name, needsConvertPropertyAccess, origin, useSemicolons, compilerOptions, isMemberCompletion)
		if err != nil {
			return uniqueNames, nil, err
		}
		if entry == nil {
			continue
		}
		shouldShadowLaterSymbols := (origin == nil || originIsTypeOnlyAlias(origin)) && !(symbol.Parent == nil && !ast.SomeDeclaration(symbol, func(d ast.Handle) bool {
			return ast.GetSourceFileOfNode(d) == file
		}))
		uniques[name] = shouldShadowLaterSymbols
		var sym *ast.Symbol
		if includeSymbols {
			sym = symbol
		}
		sortedEntries = append(sortedEntries, &CompletionItem{CompletionItem: entry, Symbol: sym})
	}
	for _, autoImport := range data.autoImports {
		replacementSpan := (*lsproto.Range)(nil)
		insertText := ""
		filterText := ""
		isSnippet := false
		sortText := SortTextAutoImportSuggestions
		if data.importStatementCompletion != nil {
			isSnippet = clientSupportsItemSnippet(ctx)
			insertText, replacementSpan = getInsertTextAndReplacementSpanForImportCompletion(autoImport.Fix, autoimport.GetImportKindForImportStatement(file, autoImport.Export, l.GetProgram()), data.importStatementCompletion, useSemicolons, file, preferences, isSnippet)
			filterText = autoImport.Fix.Name
			sortText = SortTextLocationPriority
		}
		if token := scanner.StringToToken(autoImport.Fix.Name); token != ast.KindUnknown && ast.IsNonContextualKeyword(token) {
			continue
		}
		if !autoImport.Export.IsUnresolvedAlias() {
			if data.isTypeOnlyLocation {
				if autoImport.Export.Flags&ast.SymbolFlagsType == 0 && autoImport.Export.Flags&ast.SymbolFlagsModule == 0 {
					continue
				}
			} else if data.importStatementCompletion == nil && autoImport.Export.Flags&ast.SymbolFlagsValue == 0 {
				continue
			}
		}
		entry := l.createLSPCompletionItem(ctx, autoImport.Fix.Name, insertText, filterText, sortText, autoImport.Export.ScriptElementKind, autoImport.Export.ScriptElementKindModifiers, replacementSpan, nil, &lsproto.CompletionItemLabelDetails{Description: new(autoImport.Fix.ModuleSpecifier)}, file, position, false, isSnippet, data.importStatementCompletion == nil, false, autoImport.Fix.ModuleSpecifier, autoImport.Fix.AutoImportFix, nil, nil)
		entry.Data.IsImportStatementCompletion = data.importStatementCompletion != nil
		if isShadowed, _ := uniques[autoImport.Fix.Name]; !isShadowed {
			uniques[autoImport.Fix.Name] = false
			sortedEntries = append(sortedEntries, &CompletionItem{CompletionItem: entry})
		}
	}
	uniqueSet := collections.NewSetWithSizeHint[string](len(uniques))
	for name := range uniques {
		uniqueSet.Add(name)
	}
	return *uniqueSet, sortedEntries, nil
}
func completionNameForLiteral(file *ast.SourceFile, preferences lsutil.UserPreferences, literal literalValue) string {
	switch literal := literal.(type) {
	case string:
		return quote(file, preferences, literal)
	case jsnum.Number:
		name, _ := core.StringifyJson(literal, "", "")
		return name
	case jsnum.PseudoBigInt:
		return literal.String() + "n"
	}
	panic(fmt.Sprintf("Unhandled literal value: %v", literal))
}
func getInsertTextAndReplacementSpanForImportCompletion(fix *autoimport.Fix, importKind lsproto.ImportKind, importStatementCompletion *importStatementCompletionInfo, useSemicolons bool, file *ast.SourceFile, preferences lsutil.UserPreferences, isSnippet bool) (insertText string, replacementSpan *lsproto.Range) {
	quotedModuleSpecifier := escapeSnippetText(quote(file, preferences, fix.ModuleSpecifier))
	tabStop := core.IfElse(isSnippet, "$1", "")
	suffix := core.IfElse(useSemicolons, ";", "")
	topLevelTypeOnlyText := core.IfElse(importStatementCompletion.isTopLevelTypeOnly, " "+scanner.TokenToString(ast.KindTypeKeyword)+" ", " ")
	name := escapeSnippetText(fix.Name)
	replacementSpan = importStatementCompletion.replacementSpan
	switch importKind {
	case lsproto.ImportKindCommonJS:
		return fmt.Sprintf("import%s%s%s = require(%s)%s", topLevelTypeOnlyText, name, tabStop, quotedModuleSpecifier, suffix), replacementSpan
	case lsproto.ImportKindDefault:
		return fmt.Sprintf("import%s%s%s from %s%s", topLevelTypeOnlyText, name, tabStop, quotedModuleSpecifier, suffix), replacementSpan
	case lsproto.ImportKindNamespace:
		return fmt.Sprintf("import%s* as %s from %s%s", topLevelTypeOnlyText, name, quotedModuleSpecifier, suffix), replacementSpan
	case lsproto.ImportKindNamed:
		return fmt.Sprintf("import%s{ %s%s%s } from %s%s", topLevelTypeOnlyText, core.IfElse(importStatementCompletion.couldBeTypeOnlyImportSpecifier, scanner.TokenToString(ast.KindTypeKeyword)+" ", ""), name, tabStop, quotedModuleSpecifier, suffix), replacementSpan
	default:
		panic("unhandled import kind: " + importKind.String())
	}
}
func createCompletionItemForLiteral(file *ast.SourceFile, preferences lsutil.UserPreferences, literal literalValue) *lsproto.CompletionItem {
	return &lsproto.CompletionItem{Label: completionNameForLiteral(file, preferences, literal), Kind: new(lsproto.CompletionItemKindConstant), SortText: new(string(SortTextLocationPriority)), CommitCharacters: new([]string{})}
}
func (l *LanguageService) createCompletionItem(ctx context.Context, typeChecker *checker.Checker, symbol *ast.Symbol, sortText SortText, replacementToken ast.Handle, data *completionDataData, position int, file *ast.SourceFile, name string, needsConvertPropertyAccess bool, origin *symbolOriginInfo, useSemicolons bool, compilerOptions *core.CompilerOptions, isMemberCompletion bool) (*lsproto.CompletionItem, error) {
	contextToken := data.contextToken
	var insertText string
	var filterText string
	replacementSpan := l.getReplacementRangeForContextToken(file, replacementToken, position)
	var isSnippet, hasAction bool
	source := getSourceFromOrigin(origin)
	var labelDetails *lsproto.CompletionItemLabelDetails
	preferences := l.UserPreferences()
	insertQuestionDot := originIsNullableMember(origin)
	useBraces := originIsSymbolMember(origin) || needsConvertPropertyAccess
	if originIsThisTypeNode(origin) {
		if needsConvertPropertyAccess {
			insertText = fmt.Sprintf("this%s[%s]", core.IfElse(insertQuestionDot, "?.", ""), quotePropertyName(file, preferences, name))
		} else {
			insertText = fmt.Sprintf("this%s%s", core.IfElse(insertQuestionDot, "?.", "."), name)
		}
	} else if !data.propertyAccessToConvert.IsNil() && (useBraces || insertQuestionDot) {
		if useBraces {
			if needsConvertPropertyAccess {
				insertText = fmt.Sprintf("[%s]", quotePropertyName(file, preferences, name))
			} else {
				insertText = fmt.Sprintf("[%s]", name)
			}
		} else {
			insertText = name
		}
		if insertQuestionDot || !data.propertyAccessToConvert.QuestionDotToken().IsNil() {
			insertText = "?." + insertText
		}
		dot := astnav.FindChildOfKind(data.propertyAccessToConvert, ast.KindDotToken, file)
		if dot.IsNil() {
			dot = astnav.FindChildOfKind(data.propertyAccessToConvert, ast.KindQuestionDotToken, file)
		}
		if dot.IsNil() {
			return nil, nil
		}
		var end int
		if strings.HasPrefix(name, data.propertyAccessToConvert.Name().Text()) {
			end = data.propertyAccessToConvert.End()
		} else {
			end = dot.End()
		}
		lspRange, fidelity := l.createLspRangeFromBounds(astnav.GetStartOfNode(dot, file, false), end, file)
		if !fidelity.IsExact() {
			return nil, nil
		}
		replacementSpan = &lspRange
	}
	if data.jsxInitializer.isInitializer {
		if insertText == "" {
			insertText = name
		}
		insertText = fmt.Sprintf("{%s}", insertText)
		if !data.jsxInitializer.initializer.IsNil() {
			lspRange, fidelity := l.createLspRangeFromNode(data.jsxInitializer.initializer, file)
			if !fidelity.IsExact() {
				return nil, nil
			}
			replacementSpan = &lspRange
		}
	}
	if originIsPromise(origin) && !data.propertyAccessToConvert.IsNil() {
		if insertText == "" {
			insertText = name
		}
		precedingToken := astnav.FindPrecedingToken(file, data.propertyAccessToConvert.Pos())
		var awaitText string
		if !precedingToken.IsNil() && lsutil.PositionIsASICandidate(precedingToken.End(), precedingToken.Parent(), file) {
			awaitText = ";"
		}
		awaitText += "(await " + scanner.GetTextOfNode(data.propertyAccessToConvert.Expression()) + ")"
		if needsConvertPropertyAccess {
			insertText = awaitText + insertText
		} else {
			dotStr := core.IfElse(insertQuestionDot, "?.", ".")
			insertText = awaitText + dotStr + insertText
		}
		isInAwaitExpression := ast.IsAwaitExpression(data.propertyAccessToConvert.Parent())
		wrapNode := core.IfElse(isInAwaitExpression, data.propertyAccessToConvert.Parent(), data.propertyAccessToConvert.Expression())
		lspRange, fidelity := l.createLspRangeFromBounds(astnav.GetStartOfNode(wrapNode, file, false), data.propertyAccessToConvert.End(), file)
		if !fidelity.IsExact() {
			return nil, nil
		}
		replacementSpan = &lspRange
	}
	if originIsTypeOnlyAlias(origin) {
		hasAction = true
	}
	if data.completionKind == CompletionKindObjectPropertyDeclaration && !contextToken.IsNil() && !ast.NodeHasKind(astnav.FindPrecedingTokenEx(file, contextToken.Pos(), contextToken, false), ast.KindCommaToken) {
		if ast.IsMethodDeclaration(contextToken.Parent().Parent()) || ast.IsGetAccessorDeclaration(contextToken.Parent().Parent()) || ast.IsSetAccessorDeclaration(contextToken.Parent().Parent()) || ast.IsSpreadAssignment(contextToken.Parent()) || lsutil.GetLastToken(ast.FindAncestor(contextToken.Parent(), ast.IsPropertyAssignment), file) == contextToken || ast.IsShorthandPropertyAssignment(contextToken.Parent()) && getLineOfPosition(file, contextToken.End()) != getLineOfPosition(file, position) {
			source = string(completionSourceObjectLiteralMemberWithComma)
			hasAction = true
		}
	}
	var additionalTextEdits *[]*lsproto.TextEdit
	if preferences.IncludeCompletionsWithClassMemberSnippets.IsTrue() && data.completionKind == CompletionKindMemberLike && isClassLikeMemberCompletion(symbol, data.location, file) {
		memberCompletionEntry, err := l.getEntryForMemberCompletion(ctx, typeChecker, symbol, name, data.location, position, contextToken, file)
		if err != nil {
			return nil, err
		}
		if memberCompletionEntry == nil {
			return nil, nil
		}
		insertText = memberCompletionEntry.insertText
		filterText = memberCompletionEntry.filterText
		isSnippet = memberCompletionEntry.isSnippet
		if len(memberCompletionEntry.additionalTextEdits) > 0 {
			additionalTextEdits = &memberCompletionEntry.additionalTextEdits
			hasAction = true
			source = string(completionSourceClassMemberSnippet)
		}
	}
	if originIsObjectLiteralMethod(origin) {
		insertText = origin.asObjectLiteralMethod().insertText
		isSnippet = origin.asObjectLiteralMethod().isSnippet
		labelDetails = origin.asObjectLiteralMethod().labelDetails
		if !clientSupportsItemLabelDetails(ctx) {
			name = name + *origin.asObjectLiteralMethod().labelDetails.Detail
			labelDetails = nil
		}
		source = string(completionSourceObjectLiteralMethodSnippet)
		sortText = SortBelow(sortText)
	}
	if data.isJsxIdentifierExpected && !data.isRightOfOpenTag && clientSupportsItemSnippet(ctx) && preferences.JsxAttributeCompletionStyle != lsutil.JsxAttributeCompletionStyleNone && !(!data.location.Parent().IsNil() && ast.IsJsxAttribute(data.location.Parent()) && !data.location.Parent().Initializer().IsNil()) {
		useBraces := preferences.JsxAttributeCompletionStyle == lsutil.JsxAttributeCompletionStyleBraces
		t := typeChecker.GetTypeOfSymbolAtLocation(symbol, data.location)
		if preferences.JsxAttributeCompletionStyle == lsutil.JsxAttributeCompletionStyleAuto && !t.IsBooleanLike() && !(t.IsUnion() && core.Some(t.Types(), (*checker.Type).IsBooleanLike)) {
			if t.IsStringLike() || t.IsUnion() && core.Every(t.Types(), func(t *checker.Type) bool {
				return t.Flags()&(checker.TypeFlagsStringLike|checker.TypeFlagsUndefined) != 0 || isStringAndEmptyAnonymousObjectIntersection(typeChecker, t)
			}) {
				insertText = fmt.Sprintf("%s=%s", escapeSnippetText(name), quote(file, preferences, "$1"))
				isSnippet = true
			} else {
				useBraces = true
			}
		}
		if useBraces {
			insertText = escapeSnippetText(name) + "={$1}"
			isSnippet = true
		}
	}
	parentNamedImportOrExport := ast.FindAncestor(data.location, isNamedImportsOrExports)
	if !parentNamedImportOrExport.IsNil() {
		if !scanner.IsIdentifierText(name, core.LanguageVariantStandard) {
			insertText = quotePropertyName(file, preferences, name)
			if parentNamedImportOrExport.Kind == ast.KindNamedImports {
				scanner := scanner.NewScanner()
				scanner.SetText(file.Text())
				scanner.ResetPos(position)
				if !(scanner.Scan() == ast.KindAsKeyword && scanner.Scan() == ast.KindIdentifier) {
					insertText += " as " + generateIdentifierForArbitraryString(name)
				}
			}
		} else if parentNamedImportOrExport.Kind == ast.KindNamedImports {
			possibleToken := scanner.StringToToken(name)
			if possibleToken != ast.KindUnknown && (possibleToken == ast.KindAwaitKeyword || lsutil.IsNonContextualKeyword(possibleToken)) {
				insertText = fmt.Sprintf("%s as %s_", name, name)
			}
		}
	}
	elementKind := lsutil.GetSymbolKind(typeChecker, symbol, data.location)
	var commitCharacters *[]string
	if clientSupportsItemCommitCharacters(ctx) {
		if elementKind == lsutil.ScriptElementKindWarning || elementKind == lsutil.ScriptElementKindString {
			commitCharacters = &[]string{}
		} else if !clientSupportsDefaultCommitCharacters(ctx) {
			commitCharacters = new(data.defaultCommitCharacters)
		}
	}
	preselect := isRecommendedCompletionMatch(symbol, data.recommendedCompletion, typeChecker)
	kindModifiers := lsutil.GetSymbolModifiers(typeChecker, symbol)
	return l.createLSPCompletionItem(ctx, name, insertText, filterText, sortText, elementKind, kindModifiers, replacementSpan, commitCharacters, labelDetails, file, position, isMemberCompletion, isSnippet, hasAction, preselect, source, nil, additionalTextEdits, nil), nil
}

type memberCompletionEntry struct {
	insertText          string
	filterText          string
	isSnippet           bool
	additionalTextEdits []*lsproto.TextEdit
}

func (l *LanguageService) getEntryForObjectLiteralMethodCompletion(ctx context.Context, typeChecker *checker.Checker, symbol *ast.Symbol, enclosingDeclaration ast.Handle, file *ast.SourceFile) *symbolOriginInfoObjectLiteralMethod {
	snippetPrinter := createSnippetPrinter(printer.PrinterOptions{RemoveComments: true, NewLine: core.GetNewLineKind(l.FormatOptions().NewLineCharacter), Target: l.GetProgram().Options().GetEmitScriptTarget()}, nil)
	isSnippet := clientSupportsItemSnippet(ctx)
	method := l.createObjectLiteralMethod(snippetPrinter, typeChecker, symbol, enclosingDeclaration, file, isSnippet)
	if method.IsNil() {
		return nil
	}
	insertText := snippetPrinter.printAndFormatNodeWithSettings(ctx, method, file, change.GetFormatCodeSettingsForWriting(l.FormatOptions(), file))
	insertText += ","
	return &symbolOriginInfoObjectLiteralMethod{insertText: insertText, labelDetails: &lsproto.CompletionItemLabelDetails{Detail: new(l.printObjectLiteralMethodLabelDetail(method, file, snippetPrinter.factory))}, isSnippet: isSnippet}
}
func (l *LanguageService) createObjectLiteralMethod(snippetPrinter *snippetPrinter, typeChecker *checker.Checker, symbol *ast.Symbol, enclosingDeclaration ast.Handle, file *ast.SourceFile, isSnippet bool) ast.Handle {
	factory := snippetPrinter.factory
	emitContext := snippetPrinter.emitContext
	declaration := core.FirstOrNil(ast.DeclarationNodes(symbol))
	if !isObjectLiteralMethodCompletionCandidateDeclaration(declaration) {
		return ast.Handle{}
	}
	effectiveType := typeChecker.GetWidenedType(typeChecker.GetTypeOfSymbolAtLocation(symbol, enclosingDeclaration))
	if effectiveType.Flags()&checker.TypeFlagsUnion != 0 && len(effectiveType.Types()) < 10 {
		effectiveType = typeChecker.GetUnionTypeEx(effectiveType.Types(), checker.UnionReductionSubtype)
	}
	if effectiveType.Flags()&checker.TypeFlagsUnion != 0 {
		var functionType *checker.Type
		for _, unionType := range effectiveType.Types() {
			if len(typeChecker.GetSignaturesOfType(unionType, checker.SignatureKindCall)) == 0 {
				continue
			}
			if functionType != nil {
				return ast.Handle{}
			}
			functionType = unionType
		}
		if functionType == nil {
			return ast.Handle{}
		}
		effectiveType = functionType
	}
	signatures := typeChecker.GetSignaturesOfType(effectiveType, checker.SignatureKindCall)
	if len(signatures) != 1 {
		return ast.Handle{}
	}
	flags := nodebuilder.FlagsOmitThisParameter
	if lsutil.GetQuotePreference(file, l.UserPreferences()) == lsutil.QuotePreferenceSingle {
		flags |= nodebuilder.FlagsUseSingleQuotesForStringLiteralType
	}
	typeNode := typeChecker.TypeToTypeNode(effectiveType, enclosingDeclaration, flags, nil)
	if typeNode.IsNil() || typeNode.Kind != ast.KindFunctionType {
		return ast.Handle{}
	}
	parameters := make([]ast.Handle, 0, len(typeNode.Store().ListSlice(typeNode.FunctionTypeNodeParameters())))
	for _, parameter := range typeNode.Store().ListSlice(typeNode.FunctionTypeNodeParameters()) {
		parameters = append(parameters, factory.NewParameterDeclaration(0, parameter.ParameterDeclarationDotDotDotToken(), factory.DeepCloneNode(parameter.Name()), ast.Handle{}, ast.Handle{}, parameter.ParameterDeclarationInitializer()))
	}
	body := factory.NewBlock(factory.NewList(nil), true)
	if isSnippet {
		body = createSnippetTabStopBody(factory, emitContext)
	}
	return factory.NewMethodDeclaration(0, ast.Handle{}, factory.DeepCloneNode(declaration.Name()), ast.Handle{}, 0, factory.NewList(parameters), ast.Handle{}, ast.Handle{}, body)
}
func isObjectLiteralMethodCompletionCandidateDeclaration(declaration ast.Handle) bool {
	if declaration.IsNil() {
		return false
	}
	switch declaration.Kind {
	case ast.KindPropertySignature, ast.KindPropertyDeclaration, ast.KindMethodSignature, ast.KindMethodDeclaration:
		return true
	default:
		return false
	}
}

type objectLiteralMethodSymbol struct {
	symbol *ast.Symbol
	origin *symbolOriginInfo
}

func (l *LanguageService) collectObjectLiteralMethodSymbols(ctx context.Context, typeChecker *checker.Checker, members []*ast.Symbol, enclosingDeclaration ast.Handle, file *ast.SourceFile) []objectLiteralMethodSymbol {
	if ast.IsSourceFileJS(file) {
		return nil
	}
	var methods []objectLiteralMethodSymbol
	for _, member := range members {
		if !isObjectLiteralMethodSymbol(member) {
			continue
		}
		displayName, _ := getCompletionEntryDisplayNameForSymbol(member, nil, CompletionKindObjectPropertyDeclaration, false)
		if displayName == "" {
			continue
		}
		entry := l.getEntryForObjectLiteralMethodCompletion(ctx, typeChecker, member, enclosingDeclaration, file)
		if entry == nil {
			continue
		}
		methods = append(methods, objectLiteralMethodSymbol{symbol: member, origin: &symbolOriginInfo{kind: symbolOriginInfoKindObjectLiteralMethod, data: entry}})
	}
	return methods
}
func isObjectLiteralMethodSymbol(symbol *ast.Symbol) bool {
	return symbol.Flags&(ast.SymbolFlagsProperty|ast.SymbolFlagsMethod) != 0
}
func (l *LanguageService) printObjectLiteralMethodLabelDetail(method ast.Handle, file *ast.SourceFile, factory ast.HandleFactory) string {
	methodDeclaration := method
	methodSignature := factory.NewMethodSignatureDeclaration(0, factory.NewIdentifier(""), methodDeclaration.PostfixToken(), methodDeclaration.TypeParameterList(), methodDeclaration.ParameterList(), methodDeclaration.Type())
	signaturePrinter := printer.NewPrinter(printer.PrinterOptions{RemoveComments: true, OmitTrailingSemicolon: true, NewLine: core.GetNewLineKind(l.FormatOptions().NewLineCharacter), Target: l.GetProgram().Options().GetEmitScriptTarget()}, printer.PrintHandlers{}, nil)
	return signaturePrinter.Emit(methodSignature, file)
}
func (l *LanguageService) getEntryForMemberCompletion(ctx context.Context, typeChecker *checker.Checker, symbol *ast.Symbol, name string, location ast.Handle, position int, contextToken ast.Handle, file *ast.SourceFile) (*memberCompletionEntry, error) {
	classLikeDeclaration := ast.FindAncestor(location, ast.IsClassLike)
	if classLikeDeclaration.IsNil() {
		return nil, nil
	}
	importAdder, err := l.createImportAdder(ctx, typeChecker, file)
	if err != nil {
		return nil, err
	}
	changeTracker := change.NewTracker(ctx, l.GetProgram().Options(), l.FormatOptions(), l.converters)
	fixer := newMissingMemberFixer(changeTracker, l.GetProgram(), typeChecker, l.UserPreferences(), importAdder, locale.FromContext(ctx))
	presentModifiers := l.getPresentMemberModifiers(contextToken, file, position)
	abstract := presentModifiers.modifiers&ast.ModifierFlagsAbstract != 0 && classLikeDeclaration.ModifierFlags()&ast.ModifierFlagsAbstract != 0
	isSnippet := clientSupportsItemSnippet(ctx)
	body := changeTracker.HandleFactory.NewBlock(changeTracker.HandleFactory.NewList(nil), true)
	if isSnippet {
		body = createSnippetTabStopBody(changeTracker.HandleFactory, changeTracker.EmitContext)
	}
	nodes := fixer.createMemberFromSymbol(symbol, classLikeDeclaration, file, body, preserveOptionalFlagsProperty, abstract)
	var additionalTextEdits []*lsproto.TextEdit
	if importAdder != nil && importAdder.HasFixes() {
		additionalTextEdits = importAdder.Edits()
	}
	if presentModifiers.eraseRange != nil {
		additionalTextEdits = append(additionalTextEdits, &lsproto.TextEdit{Range: *presentModifiers.eraseRange, NewText: ""})
	}
	modifiers := ast.ModifierFlagsNone
	completionNodes := make([]ast.Handle, 0, len(nodes))
	for _, node := range nodes {
		if node.IsNil() {
			continue
		}
		if len(completionNodes) == 0 {
			modifiers = node.ModifierFlags()
			if abstract {
				modifiers |= ast.ModifierFlagsAbstract
			}
			if ast.IsClassElement(node) && typeChecker.GetMemberOverrideModifierStatus(classLikeDeclaration, node, symbol) == checker.MemberOverrideStatusNeedsOverride {
				modifiers |= ast.ModifierFlagsOverride
			}
		}
		completionNodes = append(completionNodes, node)
	}
	if len(completionNodes) == 0 {
		return &memberCompletionEntry{insertText: name, filterText: name, isSnippet: isSnippet, additionalTextEdits: additionalTextEdits}, nil
	}
	allowedModifiers := modifiers | ast.ModifierFlagsOverride | ast.ModifierFlagsPublic
	if symbol.Flags&ast.SymbolFlagsMethod != 0 {
		allowedModifiers |= ast.ModifierFlagsAsync
	} else {
		allowedModifiers |= ast.ModifierFlagsAmbient | ast.ModifierFlagsReadonly
	}
	allowedAndPresent := presentModifiers.modifiers & allowedModifiers
	if presentModifiers.modifiers&^allowedModifiers != 0 {
		return nil, nil
	}
	if modifiers&ast.ModifierFlagsProtected != 0 && allowedAndPresent&ast.ModifierFlagsPublic != 0 {
		modifiers &^= ast.ModifierFlagsProtected
	}
	if allowedAndPresent != ast.ModifierFlagsNone && allowedAndPresent&ast.ModifierFlagsPublic == 0 {
		modifiers &^= ast.ModifierFlagsPublic
	}
	modifiers |= allowedAndPresent
	newLine := l.FormatOptions().NewLineCharacter
	snippetPrinter := createSnippetPrinter(printer.PrinterOptions{RemoveComments: true, NewLine: core.GetNewLineKind(newLine), Target: l.GetProgram().Options().GetEmitScriptTarget()}, changeTracker.EmitContext)
	var decoratedNode ast.Handle
	if len(presentModifiers.decorators) > 0 {
		lastNodeIndex := len(completionNodes) - 1
		if ast.CanHaveDecorators(completionNodes[lastNodeIndex]) {
			decoratedNode = completionNodes[lastNodeIndex]
		}
	}
	texts := make([]string, 0, len(completionNodes))
	for _, node := range completionNodes {
		node = ast.ReplaceHandleModifiers(changeTracker.HandleFactory, node, createModifierList(changeTracker.HandleFactory, modifiers, core.IfElse(node == decoratedNode, presentModifiers.decorators, nil)))
		text := snippetPrinter.printAndFormatNodeWithSettings(ctx, node, file, change.GetFormatCodeSettingsForWriting(l.FormatOptions(), file))
		texts = append(texts, text)
	}
	insertText := strings.Join(texts, newLine)
	if insertText == "" {
		return nil, nil
	}
	return &memberCompletionEntry{insertText: insertText, filterText: name, isSnippet: isSnippet, additionalTextEdits: additionalTextEdits}, nil
}

type presentMemberModifiers struct {
	modifiers  ast.ModifierFlags
	decorators []ast.Handle
	eraseRange *lsproto.Range
}

func (l *LanguageService) getPresentMemberModifiers(contextToken ast.Handle, file *ast.SourceFile, position int) presentMemberModifiers {
	if contextToken.IsNil() || getLineOfPosition(file, position) > getLineOfPosition(file, contextToken.End()) {
		return presentMemberModifiers{}
	}
	var modifiers ast.ModifierFlags
	var decorators []ast.Handle
	rangePos := position
	rangeEnd := position
	if ast.IsPropertyDeclaration(contextToken.Parent()) {
		contextModifierKind := modifierLikeKind(contextToken)
		if contextModifierKind == ast.KindUnknown {
			return presentMemberModifiers{}
		}
		modifierNodes := contextToken.Parent().ModifierNodes()
		if len(modifierNodes) > 0 {
			modifiers |= ast.ModifiersToFlags(modifierNodes) & ast.ModifierFlagsModifier
			for _, modifier := range modifierNodes {
				if ast.IsDecorator(modifier) {
					decorators = append(decorators, modifier)
				}
				rangePos = min(rangePos, scanner.GetTokenPosOfNode(modifier, file, false))
			}
		}
		contextModifierFlag := ast.ModifierToFlag(contextModifierKind)
		if modifiers&contextModifierFlag == 0 {
			modifiers |= contextModifierFlag
			rangePos = min(rangePos, astnav.GetStartOfNode(contextToken, file, false))
		}
		if contextToken.Parent().Name() != contextToken {
			rangeEnd = astnav.GetStartOfNode(contextToken.Parent().Name(), file, false)
		}
	}
	var eraseRange *lsproto.Range
	if rangePos < rangeEnd {
		lspRange, fidelity := l.createLspRangeFromBounds(rangePos, rangeEnd, file)
		if fidelity.IsExact() {
			eraseRange = &lspRange
		}
	}
	return presentMemberModifiers{modifiers: modifiers, decorators: decorators, eraseRange: eraseRange}
}
func modifierLikeKind(node ast.Handle) ast.Kind {
	if node.IsNil() {
		return ast.KindUnknown
	}
	if ast.IsModifier(node) {
		return node.Kind
	}
	if ast.IsIdentifier(node) {
		keywordKind := scanner.IdentifierToKeywordKind(node)
		if keywordKind != ast.KindUnknown && ast.IsModifierKind(keywordKind) {
			return keywordKind
		}
	}
	return ast.KindUnknown
}
func createModifierList(factory ast.HandleFactory, flags ast.ModifierFlags, decorators []ast.Handle) ast.ListRef {
	var nodes []ast.Handle
	for _, decorator := range decorators {
		nodes = append(nodes, factory.DeepCloneNode(decorator))
	}
	nodes = append(nodes, ast.CreateModifiersFromModifierFlags(flags, factory.NewModifier)...)
	if len(nodes) == 0 {
		return 0
	}
	return factory.NewModifierList(nodes)
}
func createSnippetTabStopBody(factory ast.HandleFactory, emitContext *printer.EmitContext) ast.Handle {
	emptyStatement := factory.NewEmptyStatement()
	emitContext.SetSnippetElement(emptyStatement, printer.SnippetElement{Kind: printer.SnippetKindTabStop, Order: 0})
	return factory.NewBlock(factory.NewList([]ast.Handle{emptyStatement}), true)
}
func (l *LanguageService) createImportAdder(ctx context.Context, typeChecker *checker.Checker, file *ast.SourceFile) (autoimport.ImportAdder, error) {
	if tspath.IsDynamicFileName(file.FileName()) {
		return nil, nil
	}
	view, err := l.getPreparedAutoImportView(file)
	if err != nil {
		return nil, err
	}
	if view == nil {
		return nil, nil
	}
	return autoimport.NewImportAdder(ctx, l.GetProgram(), typeChecker, file, view, l.FormatOptions(), l.converters, l.UserPreferences()), nil
}
func isRecommendedCompletionMatch(localSymbol *ast.Symbol, recommendedCompletion *ast.Symbol, typeChecker *checker.Checker) bool {
	return localSymbol == recommendedCompletion || localSymbol.Flags&ast.SymbolFlagsExportValue != 0 && typeChecker.GetExportSymbolOfSymbol(localSymbol) == recommendedCompletion
}

var wordSeparators = collections.NewSetFromItems('`', '~', '!', '@', '%', '^', '&', '*', '(', ')', '-', '=', '+', '[', '{', ']', '}', '\\', '|', ';', ':', '\'', '"', ',', '.', '<', '>', '/', '?')

func getWordLengthAndStart(sourceFile *ast.SourceFile, position int) (wordLength int, wordStart rune) {
	text := sourceFile.Text()[:position]
	totalSize := 0
	var firstRune rune
	for r, size := utf8.DecodeLastRuneInString(text); size != 0; r, size = utf8.DecodeLastRuneInString(text[:len(text)-totalSize]) {
		if wordSeparators.Has(r) || unicode.IsSpace(r) {
			break
		}
		totalSize += size
		firstRune = r
	}
	if firstRune == '@' {
		totalSize -= 1
		firstRune, _ = utf8.DecodeRuneInString(text[len(text)-totalSize:])
	}
	return totalSize, firstRune
}

func trimElementAccess(text string) string {
	text = strings.TrimPrefix(text, "[")
	text = strings.TrimSuffix(text, "]")
	if strings.HasPrefix(text, `'`) && strings.HasSuffix(text, `'`) {
		text = strings.TrimPrefix(strings.TrimSuffix(text, `'`), `'`)
	}
	if strings.HasPrefix(text, `"`) && strings.HasSuffix(text, `"`) {
		text = strings.TrimPrefix(strings.TrimSuffix(text, `"`), `"`)
	}
	return text
}

func getFilterText(file *ast.SourceFile, position int, insertText string, label string, wordStart rune, dotAccessor string) string {
	if after, ok := strings.CutPrefix(label, "#"); ok {
		if insertText != "" {
			if after, ok := strings.CutPrefix(insertText, "this.#"); ok {
				if wordStart == '#' {
					return ""
				} else {
					return after
				}
			}
		} else {
			if wordStart == '#' {
				return ""
			} else {
				return after
			}
		}
	}
	if strings.HasPrefix(insertText, "this.") {
		return ""
	}
	if strings.HasPrefix(insertText, "[") {
		return dotAccessor + trimElementAccess(insertText)
	}
	if strings.HasPrefix(insertText, "?.") {
		if strings.HasPrefix(insertText, "?.[") {
			return dotAccessor + trimElementAccess(insertText[2:])
		} else {
			return dotAccessor + insertText[2:]
		}
	}
	return insertText
}

func getDotAccessor(file *ast.SourceFile, position int) string {
	text := file.Text()[:position]
	totalSize := 0
	if strings.HasSuffix(text, "?.") {
		totalSize += 2
		return file.Text()[position-totalSize : position]
	}
	if strings.HasSuffix(text, ".") {
		totalSize += 1
		return file.Text()[position-totalSize : position]
	}
	return ""
}
func strPtrIsEmpty(ptr *string) bool {
	if ptr == nil {
		return true
	}
	return *ptr == ""
}
func strPtrTo(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
func boolToPtr(v bool) *bool {
	if v {
		return new(true)
	}
	return nil
}
func getLineOfPosition(file *ast.SourceFile, pos int) int {
	line := scanner.GetECMALineOfPosition(file, pos)
	return line
}
func getLineEndOfPosition(file *ast.SourceFile, pos int) int {
	line := getLineOfPosition(file, pos)
	lineStarts := scanner.GetECMALineStarts(file)
	var lastCharPos int
	if line+1 >= len(lineStarts) {
		lastCharPos = file.End()
	} else {
		lastCharPos = int(lineStarts[line+1]) - 1
	}
	fullText := file.Text()
	if lastCharPos > 0 && lastCharPos < len(fullText) && fullText[lastCharPos] == '\n' && fullText[lastCharPos-1] == '\r' {
		return lastCharPos - 1
	}
	return lastCharPos
}
func isClassLikeMemberCompletion(symbol *ast.Symbol, location ast.Handle, file *ast.SourceFile) bool {
	if ast.IsInJSFile(location) {
		return false
	}
	memberFlags := ast.SymbolFlagsClassMember & ast.SymbolFlagsEnumMemberExcludes
	return symbol.Flags&memberFlags != 0 && (ast.IsClassLike(location) || (!location.Parent().IsNil() && !location.Parent().Parent().IsNil() && ast.IsClassElement(location.Parent()) && location == location.Parent().Name() && lsutil.GetLastToken(location.Parent(), file) == location.Parent().Name() && ast.IsClassLike(location.Parent().Parent())) || (!location.Parent().IsNil() && ast.IsSyntaxList(location) && ast.IsClassLike(location.Parent())))
}
func symbolAppearsToBeTypeOnly(symbol *ast.Symbol, typeChecker *checker.Checker) bool {
	flags := checker.SkipAlias(symbol, typeChecker).CombinedLocalAndExportSymbolFlags()
	return flags&ast.SymbolFlagsValue == 0 && (len(symbol.Declarations) == 0 || !ast.IsInJSFile(ast.NodeOf(symbol.Declarations[0])) || flags&ast.SymbolFlagsType != 0)
}
func shouldIncludeSymbol(symbol *ast.Symbol, data *completionDataData, closestSymbolDeclaration ast.Handle, file *ast.SourceFile, typeChecker *checker.Checker, compilerOptions *core.CompilerOptions) bool {
	allFlags := symbol.Flags
	location := data.location
	if !location.Parent().IsNil() && ast.IsExportAssignment(location.Parent()) {
		return true
	}
	if !closestSymbolDeclaration.IsNil() && ast.IsVariableDeclaration(closestSymbolDeclaration) && ast.NodeOf(symbol.ValueDeclaration) == closestSymbolDeclaration {
		return false
	}
	var symbolDeclaration ast.Handle
	if symbol.ValueDeclaration != 0 {
		symbolDeclaration = ast.NodeOf(symbol.ValueDeclaration)
	} else if len(symbol.Declarations) > 0 {
		symbolDeclaration = ast.NodeOf(symbol.Declarations[0])
	}
	if !closestSymbolDeclaration.IsNil() && !symbolDeclaration.IsNil() {
		if ast.IsParameterDeclaration(closestSymbolDeclaration) && ast.IsParameterDeclaration(symbolDeclaration) {
			parameters := closestSymbolDeclaration.Parent().ParameterList()
			if symbolDeclaration.Pos() >= closestSymbolDeclaration.Pos() && symbolDeclaration.Pos() < closestSymbolDeclaration.Store().ListLoc(parameters).End() {
				return false
			}
		} else if ast.IsTypeParameterDeclaration(closestSymbolDeclaration) && ast.IsTypeParameterDeclaration(symbolDeclaration) {
			if closestSymbolDeclaration == symbolDeclaration && !data.contextToken.IsNil() && data.contextToken.Kind == ast.KindExtendsKeyword {
				return false
			}
			if isInTypeParameterDefault(data.contextToken) && !ast.IsInferTypeNode(closestSymbolDeclaration.Parent()) {
				typeParameters := closestSymbolDeclaration.Parent().TypeParameterList()
				if typeParameters != 0 && symbolDeclaration.Pos() >= closestSymbolDeclaration.Pos() && symbolDeclaration.Pos() < closestSymbolDeclaration.Store().ListLoc(typeParameters).End() {
					return false
				}
			}
		}
	}
	symbolOrigin := checker.SkipAlias(symbol, typeChecker)
	if !file.ExternalModuleIndicator.IsNil() && compilerOptions.AllowUmdGlobalAccess != core.TSTrue && symbol != symbolOrigin && data.symbolToSortTextMap[ast.GetSymbolId(symbol)] == SortTextGlobalsOrKeywords && symbol.Parent != nil && checker.IsExternalModuleSymbol(symbol.Parent) {
		return false
	}
	allFlags = allFlags | symbolOrigin.CombinedLocalAndExportSymbolFlags()
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		allFlags = allFlags | typeChecker.GetSymbolFlags(symbol)
	}
	if isInRightSideOfInternalImportEqualsDeclaration(data.location) {
		return allFlags&ast.SymbolFlagsNamespace != 0
	}
	if data.isTypeOnlyLocation {
		return symbolCanBeReferencedAtTypeLocation(symbol, typeChecker, collections.Set[ast.SymbolId]{})
	}
	return allFlags&ast.SymbolFlagsValue != 0
}
func getCompletionEntryDisplayNameForSymbol(symbol *ast.Symbol, origin *symbolOriginInfo, completionKind CompletionKind, isJsxIdentifierExpected bool) (displayName string, needsConvertPropertyAccess bool) {
	if originIsIgnore(origin) {
		return "", false
	}
	var name string
	if originIncludesSymbolName(origin) {
		name = origin.symbolName()
	} else {
		name = ast.SymbolName(symbol)
	}
	if name == "" || symbol.Flags&ast.SymbolFlagsModule != 0 && startsWithQuote(name) || checker.IsKnownSymbol(symbol) {
		return "", false
	}
	variant := core.IfElse(isJsxIdentifierExpected, core.LanguageVariantJSX, core.LanguageVariantStandard)
	if scanner.IsIdentifierText(name, variant) || symbol.ValueDeclaration != 0 && ast.IsPrivateIdentifierClassElementDeclaration(ast.NodeOf(symbol.ValueDeclaration)) {
		return name, false
	}
	if symbol.Flags&ast.SymbolFlagsAlias != 0 {
		return name, true
	}
	switch completionKind {
	case CompletionKindMemberLike:
		if originIsComputedPropertyName(origin) {
			return origin.symbolName(), false
		}
		return "", false
	case CompletionKindObjectPropertyDeclaration:
		escapedName, _ := core.StringifyJson(name, "", "")
		return escapedName, false
	case CompletionKindPropertyAccess, CompletionKindGlobal:
		ch, _ := utf8.DecodeRuneInString(name)
		if ch == ' ' {
			return "", false
		}
		return name, true
	case CompletionKindNone, CompletionKindString:
		return name, false
	default:
		panic(fmt.Sprintf("Unexpected completion kind: %v", completionKind))
	}
}

func originIsIgnore(origin *symbolOriginInfo) bool {
	return origin != nil && origin.kind&symbolOriginInfoKindIgnore != 0
}
func originIncludesSymbolName(origin *symbolOriginInfo) bool {
	return originIsComputedPropertyName(origin)
}
func originIsComputedPropertyName(origin *symbolOriginInfo) bool {
	return origin != nil && origin.kind&symbolOriginInfoKindComputedPropertyName != 0
}
func originIsObjectLiteralMethod(origin *symbolOriginInfo) bool {
	return origin != nil && origin.kind&symbolOriginInfoKindObjectLiteralMethod != 0
}
func originIsThisTypeNode(origin *symbolOriginInfo) bool {
	return origin != nil && origin.kind&symbolOriginInfoKindThisType != 0
}
func originIsTypeOnlyAlias(origin *symbolOriginInfo) bool {
	return origin != nil && origin.kind&symbolOriginInfoKindTypeOnlyAlias != 0
}
func originIsSymbolMember(origin *symbolOriginInfo) bool {
	return origin != nil && origin.kind&symbolOriginInfoKindSymbolMember != 0
}
func originIsNullableMember(origin *symbolOriginInfo) bool {
	return origin != nil && origin.kind&symbolOriginInfoKindNullable != 0
}
func originIsPromise(origin *symbolOriginInfo) bool {
	return origin != nil && origin.kind&symbolOriginInfoKindPromise != 0
}
func getSourceFromOrigin(origin *symbolOriginInfo) string {
	if originIsThisTypeNode(origin) {
		return string(completionSourceThisProperty)
	}
	if originIsTypeOnlyAlias(origin) {
		return string(completionSourceTypeOnlyAlias)
	}
	return ""
}

func getRelevantTokens(position int, file *ast.SourceFile) (contextToken ast.Handle, previousToken ast.Handle) {
	previousToken = astnav.FindPrecedingToken(file, position)
	if !previousToken.IsNil() && position <= previousToken.End() && (ast.IsMemberName(previousToken) || ast.IsKeywordKind(previousToken.Kind)) {
		contextToken := astnav.FindPrecedingToken(file, previousToken.Pos())
		return contextToken, previousToken
	}
	return previousToken, previousToken
}

type CompletionsTriggerCharacter = string

func isValidTrigger(file *ast.SourceFile, triggerCharacter CompletionsTriggerCharacter, contextToken ast.Handle, position int) bool {
	switch triggerCharacter {
	case ".", "@":
		return true
	case "\"", "'", "`":
		return !contextToken.IsNil() && isStringLiteralOrTemplate(contextToken) && position == astnav.GetStartOfNode(contextToken, file, false)+1
	case "#":
		return !contextToken.IsNil() && ast.IsPrivateIdentifier(contextToken) && !ast.GetContainingClass(contextToken).IsNil()
	case "<":
		return !contextToken.IsNil() && contextToken.Kind == ast.KindLessThanToken && (!ast.IsBinaryExpression(contextToken.Parent()) || binaryExpressionMayBeOpenTag(contextToken.Parent()))
	case "/":
		if contextToken.IsNil() {
			return false
		}
		if ast.IsStringLiteralLike(contextToken) {
			return !ast.TryGetImportFromModuleSpecifier(contextToken).IsNil()
		}
		return contextToken.Kind == ast.KindLessThanSlashToken && ast.IsJsxClosingElement(contextToken.Parent())
	case " ":
		return !contextToken.IsNil() && contextToken.Kind == ast.KindImportKeyword && contextToken.Parent().Kind == ast.KindSourceFile
	case "*":
		return isPotentiallyValidJSDocSnippetCompletionPosition(file, position)
	default:
		panic("Unknown trigger character: " + triggerCharacter)
	}
}
func isStringLiteralOrTemplate(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindTemplateExpression, ast.KindTaggedTemplateExpression:
		return true
	}
	return false
}
func binaryExpressionMayBeOpenTag(binaryExpression ast.Handle) bool {
	return ast.NodeIsMissing(binaryExpression.Left())
}
func isCheckedFile(file *ast.SourceFile, compilerOptions *core.CompilerOptions) bool {
	return !ast.IsSourceFileJS(file) || ast.IsCheckJSEnabledForFile(file, compilerOptions)
}
func isContextTokenValueLocation(contextToken ast.Handle) bool {
	return !contextToken.IsNil() && ((contextToken.Kind == ast.KindTypeOfKeyword && (contextToken.Parent().Kind == ast.KindTypeQuery || ast.IsTypeOfExpression(contextToken.Parent()))) || (contextToken.Kind == ast.KindAssertsKeyword && contextToken.Parent().Kind == ast.KindTypePredicate))
}
func isPossiblyTypeArgumentPosition(token ast.Handle, sourceFile *ast.SourceFile, typeChecker *checker.Checker) bool {
	info := getPossibleTypeArgumentsInfo(token, sourceFile)
	return info != nil && (ast.IsPartOfTypeNode(info.called) || len(getPossibleGenericSignatures(info.called, info.nTypeArguments, typeChecker)) != 0 || isPossiblyTypeArgumentPosition(info.called, sourceFile, typeChecker))
}
func isContextTokenTypeLocation(contextToken ast.Handle) bool {
	if !contextToken.IsNil() {
		parentKind := contextToken.Parent().Kind
		switch contextToken.Kind {
		case ast.KindColonToken:
			return parentKind == ast.KindPropertyDeclaration || parentKind == ast.KindPropertySignature || parentKind == ast.KindParameter || parentKind == ast.KindVariableDeclaration || ast.IsFunctionLikeKind(parentKind)
		case ast.KindEqualsToken:
			return parentKind == ast.KindTypeAliasDeclaration || parentKind == ast.KindTypeParameter
		case ast.KindAsKeyword:
			return parentKind == ast.KindAsExpression
		case ast.KindLessThanToken:
			return parentKind == ast.KindTypeReference || parentKind == ast.KindTypeAssertionExpression
		case ast.KindExtendsKeyword:
			return parentKind == ast.KindTypeParameter
		case ast.KindSatisfiesKeyword:
			return parentKind == ast.KindSatisfiesExpression
		}
	}
	return false
}

func symbolCanBeReferencedAtTypeLocation(symbol *ast.Symbol, typeChecker *checker.Checker, seenModules collections.Set[ast.SymbolId]) bool {
	return nonAliasCanBeReferencedAtTypeLocation(symbol, typeChecker, seenModules) || nonAliasCanBeReferencedAtTypeLocation(checker.SkipAlias(core.IfElse(symbol.ExportSymbol != nil, symbol.ExportSymbol, symbol), typeChecker), typeChecker, seenModules)
}
func nonAliasCanBeReferencedAtTypeLocation(symbol *ast.Symbol, typeChecker *checker.Checker, seenModules collections.Set[ast.SymbolId]) bool {
	return symbol.Flags&ast.SymbolFlagsType != 0 || typeChecker.IsUnknownSymbol(symbol) || symbol.Flags&ast.SymbolFlagsModule != 0 && seenModules.AddIfAbsent(ast.GetSymbolId(symbol)) && core.Some(typeChecker.GetExportsOfModule(symbol), func(e *ast.Symbol) bool {
		return symbolCanBeReferencedAtTypeLocation(e, typeChecker, seenModules)
	})
}

func getPropertiesForCompletion(t *checker.Type, typeChecker *checker.Checker) []*ast.Symbol {
	if t.IsUnion() {
		return core.CheckEachDefined(typeChecker.GetAllPossiblePropertiesOfTypes(t.Types()), "getAllPossiblePropertiesOfTypes() should all be defined.")
	} else {
		return core.CheckEachDefined(typeChecker.GetApparentProperties(t), "getApparentProperties() should all be defined.")
	}
}

func getLeftMostName(e ast.Handle) ast.Handle {
	if ast.IsIdentifier(e) {
		return e
	} else if ast.IsPropertyAccessExpression(e) {
		return getLeftMostName(e.Expression())
	} else {
		return ast.Handle{}
	}
}
func getFirstSymbolInChain(symbol *ast.Symbol, enclosingDeclaration ast.Handle, typeChecker *checker.Checker) *ast.Symbol {
	chain := typeChecker.GetAccessibleSymbolChain(symbol, enclosingDeclaration, ast.SymbolFlagsAll, false)
	if len(chain) > 0 {
		return chain[0]
	}
	if symbol.Parent != nil {
		if isModuleSymbol(symbol.Parent) {
			return symbol
		}
		return getFirstSymbolInChain(symbol.Parent, enclosingDeclaration, typeChecker)
	}
	return nil
}
func isModuleSymbol(symbol *ast.Symbol) bool {
	return ast.SomeDeclaration(symbol, func(decl ast.Handle) bool {
		return decl.Kind == ast.KindSourceFile
	})
}
func getNullableSymbolOriginInfoKind(kind symbolOriginInfoKind, insertQuestionDot bool) symbolOriginInfoKind {
	if insertQuestionDot {
		kind |= symbolOriginInfoKindNullable
	}
	return kind
}
func isStaticProperty(symbol *ast.Symbol) bool {
	return symbol.ValueDeclaration != 0 && ast.NodeOf(symbol.ValueDeclaration).ModifierFlags()&ast.ModifierFlagsStatic != 0 && ast.IsClassLike(ast.NodeOf(symbol.ValueDeclaration).Parent())
}

func getContextualTypeForConditionalExpression(conditionalExpr ast.Handle, position int, file *ast.SourceFile, typeChecker *checker.Checker) *checker.Type {
	argInfo := getArgumentInfoForCompletions(conditionalExpr, position, file, typeChecker)
	if argInfo != nil {
		return typeChecker.GetContextualTypeForArgumentAtIndex(argInfo.invocation, argInfo.argumentIndex)
	}
	contextualType := typeChecker.GetContextualType(conditionalExpr, checker.ContextFlagsIgnoreNodeInferences)
	if contextualType != nil {
		return contextualType
	}
	return typeChecker.GetContextualType(conditionalExpr, checker.ContextFlagsNone)
}
func getContextualType(previousToken ast.Handle, position int, file *ast.SourceFile, typeChecker *checker.Checker) *checker.Type {
	parent := previousToken.Parent()
	switch previousToken.Kind {
	case ast.KindIdentifier:
		return getContextualTypeFromParent(previousToken, typeChecker, checker.ContextFlagsNone)
	case ast.KindEqualsToken:
		switch parent.Kind {
		case ast.KindVariableDeclaration:
			return typeChecker.GetContextualType(parent.Initializer(), checker.ContextFlagsNone)
		case ast.KindBinaryExpression:
			return typeChecker.GetTypeAtLocation(parent.BinaryExpressionLeft())
		case ast.KindJsxAttribute:
			return typeChecker.GetContextualTypeForJsxAttribute(parent)
		default:
			return nil
		}
	case ast.KindNewKeyword:
		return typeChecker.GetContextualType(parent, checker.ContextFlagsNone)
	case ast.KindCaseKeyword:
		caseClause := core.IfElse(ast.IsCaseClause(parent), parent, ast.Handle{})
		if !caseClause.IsNil() {
			return getSwitchedType(caseClause, typeChecker)
		}
		return nil
	case ast.KindOpenBraceToken:
		if ast.IsJsxExpression(parent) && !ast.IsJsxElement(parent.Parent()) && !ast.IsJsxFragment(parent.Parent()) {
			return typeChecker.GetContextualTypeForJsxAttribute(parent.Parent())
		}
		return nil
	case ast.KindOpenBracketToken:
		if ast.IsArrayLiteralExpression(parent) {
			contextualArrayType := typeChecker.GetContextualType(parent, checker.ContextFlagsNone)
			if contextualArrayType != nil {
				return typeChecker.GetContextualTypeForArrayLiteralAtPosition(contextualArrayType, parent, position)
			}
		}
		return nil
	case ast.KindCloseBracketToken:
		return nil
	case ast.KindQuestionToken:
		if ast.IsConditionalExpression(parent) {
			return getContextualTypeForConditionalExpression(parent, position, file, typeChecker)
		}
		return nil
	case ast.KindColonToken:
		if ast.IsConditionalExpression(parent) {
			return getContextualTypeForConditionalExpression(parent, position, file, typeChecker)
		}
	case ast.KindCommaToken:
		if ast.IsArrayLiteralExpression(parent) {
			contextualArrayType := typeChecker.GetContextualType(parent, checker.ContextFlagsNone)
			if contextualArrayType != nil {
				return typeChecker.GetContextualTypeForArrayLiteralAtPosition(contextualArrayType, parent, position)
			}
			return nil
		}
	}
	argInfo := getArgumentInfoForCompletions(previousToken, position, file, typeChecker)
	if argInfo != nil {
		return typeChecker.GetContextualTypeForArgumentAtIndex(argInfo.invocation, argInfo.argumentIndex)
	} else if isEqualityOperatorKind(previousToken.Kind) && ast.IsBinaryExpression(parent) && isEqualityOperatorKind(parent.BinaryExpressionOperatorToken().Kind) {
		return typeChecker.GetTypeAtLocation(parent.BinaryExpressionLeft())
	} else {
		contextualType := typeChecker.GetContextualType(previousToken, checker.ContextFlagsIgnoreNodeInferences)
		if contextualType != nil {
			return contextualType
		}
		return typeChecker.GetContextualType(previousToken, checker.ContextFlagsNone)
	}
}
func getSwitchedType(caseClause ast.Handle, typeChecker *checker.Checker) *checker.Type {
	return typeChecker.GetTypeAtLocation(caseClause.Parent().Parent().Expression())
}
func isEqualityOperatorKind(kind ast.Kind) bool {
	switch kind {
	case ast.KindEqualsEqualsEqualsToken, ast.KindEqualsEqualsToken, ast.KindExclamationEqualsEqualsToken, ast.KindExclamationEqualsToken:
		return true
	default:
		return false
	}
}

func isLiteral(t *checker.Type) bool {
	return t.IsStringLiteral() || t.IsNumberLiteral() || t.IsBigIntLiteral()
}
func getRecommendedCompletion(previousToken ast.Handle, contextualType *checker.Type, typeChecker *checker.Checker) *ast.Symbol {
	var types []*checker.Type
	if contextualType.IsUnion() {
		types = contextualType.Types()
	} else {
		types = []*checker.Type{contextualType}
	}
	return core.FirstNonNil(types, func(t *checker.Type) *ast.Symbol {
		symbol := t.Symbol()
		if symbol != nil && symbol.Flags&(ast.SymbolFlagsEnumMember|ast.SymbolFlagsEnum|ast.SymbolFlagsClass) != 0 && !isAbstractConstructorSymbol(symbol) {
			return getFirstSymbolInChain(symbol, previousToken, typeChecker)
		}
		return nil
	})
}
func isAbstractConstructorSymbol(symbol *ast.Symbol) bool {
	if symbol.Flags&ast.SymbolFlagsClass != 0 {
		declaration := ast.GetClassLikeDeclarationOfSymbol(symbol)
		return !declaration.IsNil() && ast.HasSyntacticModifier(declaration, ast.ModifierFlagsAbstract)
	}
	return false
}
func startsWithQuote(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return r == '"' || r == '\''
}
func getClosestSymbolDeclaration(contextToken ast.Handle, location ast.Handle) ast.Handle {
	if contextToken.IsNil() {
		return ast.Handle{}
	}
	closestDeclaration := ast.FindAncestorOrQuit(contextToken, func(node ast.Handle) ast.FindAncestorResult {
		if ast.IsFunctionBlock(node) || isArrowFunctionBody(node) || ast.IsBindingPattern(node) {
			return ast.FindAncestorQuit
		}
		if (ast.IsParameterDeclaration(node) || ast.IsTypeParameterDeclaration(node)) && !ast.IsIndexSignatureDeclaration(node.Parent()) {
			return ast.FindAncestorTrue
		}
		return ast.FindAncestorFalse
	})
	if closestDeclaration.IsNil() {
		closestDeclaration = ast.FindAncestorOrQuit(location, func(node ast.Handle) ast.FindAncestorResult {
			if ast.IsFunctionBlock(node) || isArrowFunctionBody(node) || ast.IsBindingPattern(node) {
				return ast.FindAncestorQuit
			}
			if ast.IsVariableDeclaration(node) {
				return ast.FindAncestorTrue
			}
			return ast.FindAncestorFalse
		})
	}
	return closestDeclaration
}
func isArrowFunctionBody(node ast.Handle) bool {
	return !node.Parent().IsNil() && ast.IsArrowFunction(node.Parent()) && (node.Parent().Body() == node || node.Kind == ast.KindEqualsGreaterThanToken)
}
func isInTypeParameterDefault(contextToken ast.Handle) bool {
	if contextToken.IsNil() {
		return false
	}
	node := contextToken
	parent := contextToken.Parent()
	for !parent.IsNil() {
		if ast.IsTypeParameterDeclaration(parent) {
			return parent.TypeParameterDeclarationDefaultType() == node || node.Kind == ast.KindEqualsToken
		}
		node = parent
		parent = parent.Parent()
	}
	return false
}
func isDeprecated(symbol *ast.Symbol, typeChecker *checker.Checker) bool {
	aliased := checker.SkipAlias(symbol, typeChecker)
	return len(aliased.Declarations) > 0 && ast.EveryDeclaration(aliased, typeChecker.IsDeprecatedDeclaration)
}
func (l *LanguageService) getReplacementRangeForContextToken(file *ast.SourceFile, contextToken ast.Handle, position int) *lsproto.Range {
	if contextToken.IsNil() {
		return nil
	}
	switch contextToken.Kind {
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return l.createRangeFromStringLiteralLikeContent(file, contextToken, position)
	default:
		lspRange, fidelity := l.createLspRangeFromNode(contextToken, file)
		if !fidelity.IsExact() {
			return nil
		}
		return &lspRange
	}
}
func (l *LanguageService) createRangeFromStringLiteralLikeContent(file *ast.SourceFile, node ast.Handle, position int) *lsproto.Range {
	replacementEnd := node.End() - 1
	nodeStart := astnav.GetStartOfNode(node, file, false)
	if ast.IsUnterminatedLiteral(node) {
		if nodeStart == replacementEnd {
			return nil
		}
		replacementEnd = min(position, node.End())
	}
	lspRange, fidelity := l.createLspRangeFromBounds(nodeStart+1, replacementEnd, file)
	if !fidelity.IsExact() {
		return nil
	}
	return &lspRange
}
func quotePropertyName(file *ast.SourceFile, preferences lsutil.UserPreferences, name string) string {
	r, _ := utf8.DecodeRuneInString(name)
	if unicode.IsDigit(r) {
		return name
	}
	return quote(file, preferences, name)
}

func isStringAndEmptyAnonymousObjectIntersection(typeChecker *checker.Checker, t *checker.Type) bool {
	if !t.IsIntersection() {
		return false
	}
	return len(t.Types()) == 2 && (areIntersectedTypesAvoidingStringReduction(typeChecker, t.Types()[0], t.Types()[1]) || areIntersectedTypesAvoidingStringReduction(typeChecker, t.Types()[1], t.Types()[0]))
}
func areIntersectedTypesAvoidingStringReduction(typeChecker *checker.Checker, t1 *checker.Type, t2 *checker.Type) bool {
	return t1.IsString() && typeChecker.IsEmptyAnonymousObjectType(t2)
}
func escapeSnippetText(text string) string {
	return strings.ReplaceAll(text, `$`, `\$`)
}
func isNamedImportsOrExports(node ast.Handle) bool {
	return ast.IsNamedImports(node) || ast.IsNamedExports(node)
}
func generateIdentifierForArbitraryString(text string) string {
	needsUnderscore := false
	var identifier strings.Builder
	var ch rune
	var size int
	for pos := 0; pos < len(text); pos += size {
		ch, size = utf8.DecodeRuneInString(text[pos:])
		var validChar bool
		if pos == 0 {
			validChar = scanner.IsIdentifierStart(ch)
		} else {
			validChar = scanner.IsIdentifierPart(ch)
		}
		if size > 0 && validChar {
			if needsUnderscore {
				identifier.WriteRune('_')
			}
			identifier.WriteRune(ch)
			needsUnderscore = false
		} else {
			needsUnderscore = true
		}
	}
	if needsUnderscore {
		identifier.WriteRune('_')
	}
	id := identifier.String()
	if id == "" {
		return "_"
	}
	return id
}

func getCompletionsSymbolKind(kind lsutil.ScriptElementKind) lsproto.CompletionItemKind {
	switch kind {
	case lsutil.ScriptElementKindPrimitiveType, lsutil.ScriptElementKindKeyword:
		return lsproto.CompletionItemKindKeyword
	case lsutil.ScriptElementKindConstElement, lsutil.ScriptElementKindLetElement, lsutil.ScriptElementKindVariableElement, lsutil.ScriptElementKindLocalVariableElement, lsutil.ScriptElementKindAlias, lsutil.ScriptElementKindParameterElement:
		return lsproto.CompletionItemKindVariable
	case lsutil.ScriptElementKindMemberVariableElement, lsutil.ScriptElementKindMemberGetAccessorElement, lsutil.ScriptElementKindMemberSetAccessorElement:
		return lsproto.CompletionItemKindField
	case lsutil.ScriptElementKindFunctionElement, lsutil.ScriptElementKindLocalFunctionElement:
		return lsproto.CompletionItemKindFunction
	case lsutil.ScriptElementKindMemberFunctionElement, lsutil.ScriptElementKindConstructSignatureElement, lsutil.ScriptElementKindCallSignatureElement, lsutil.ScriptElementKindIndexSignatureElement:
		return lsproto.CompletionItemKindMethod
	case lsutil.ScriptElementKindEnumElement:
		return lsproto.CompletionItemKindEnum
	case lsutil.ScriptElementKindEnumMemberElement:
		return lsproto.CompletionItemKindEnumMember
	case lsutil.ScriptElementKindModuleElement, lsutil.ScriptElementKindExternalModuleName:
		return lsproto.CompletionItemKindModule
	case lsutil.ScriptElementKindClassElement, lsutil.ScriptElementKindTypeElement:
		return lsproto.CompletionItemKindClass
	case lsutil.ScriptElementKindInterfaceElement:
		return lsproto.CompletionItemKindInterface
	case lsutil.ScriptElementKindWarning:
		return lsproto.CompletionItemKindText
	case lsutil.ScriptElementKindScriptElement:
		return lsproto.CompletionItemKindFile
	case lsutil.ScriptElementKindDirectory:
		return lsproto.CompletionItemKindFolder
	case lsutil.ScriptElementKindString:
		return lsproto.CompletionItemKindConstant
	default:
		return lsproto.CompletionItemKindProperty
	}
}

func CompareCompletionEntries(a, b *lsproto.CompletionItem) int {
	compareStrings := stringutil.CompareStringsCaseInsensitiveThenSensitive
	result := compareStrings(*a.SortText, *b.SortText)
	if result == stringutil.ComparisonEqual {
		result = compareStrings(a.Label, b.Label)
	}
	return result
}

var (
	keywordCompletionsCache = collections.SyncMap[KeywordCompletionFilters, []*lsproto.CompletionItem]{}
	allKeywordCompletions   = sync.OnceValue(func() []*lsproto.CompletionItem {
		result := make([]*lsproto.CompletionItem, 0, ast.KindLastKeyword-ast.KindFirstKeyword+1)
		for i := ast.KindFirstKeyword; i <= ast.KindLastKeyword; i++ {
			result = append(result, &lsproto.CompletionItem{Label: scanner.TokenToString(i), Kind: new(lsproto.CompletionItemKindKeyword), SortText: new(string(SortTextGlobalsOrKeywords))})
		}
		return result
	})
)

func cloneItems(items []*lsproto.CompletionItem) []*CompletionItem {
	if items == nil {
		return nil
	}
	entries := make([]*CompletionItem, len(items))
	for i, item := range items {
		itemClone := *item
		entries[i] = &CompletionItem{CompletionItem: &itemClone}
	}
	return entries
}
func getKeywordCompletions(keywordFilter KeywordCompletionFilters, filterOutTsOnlyKeywords bool) []*CompletionItem {
	if !filterOutTsOnlyKeywords {
		return cloneItems(getTypescriptKeywordCompletions(keywordFilter))
	}
	index := keywordFilter + KeywordCompletionFiltersLast + 1
	if cached, ok := keywordCompletionsCache.Load(index); ok {
		return cloneItems(cached)
	}
	result := core.Filter(getTypescriptKeywordCompletions(keywordFilter), func(ci *lsproto.CompletionItem) bool {
		return !isTypeScriptOnlyKeyword(scanner.StringToToken(ci.Label))
	})
	keywordCompletionsCache.Store(index, result)
	return cloneItems(result)
}
func getTypescriptKeywordCompletions(keywordFilter KeywordCompletionFilters) []*lsproto.CompletionItem {
	if cached, ok := keywordCompletionsCache.Load(keywordFilter); ok {
		return cached
	}
	result := core.Filter(allKeywordCompletions(), func(entry *lsproto.CompletionItem) bool {
		kind := scanner.StringToToken(entry.Label)
		switch keywordFilter {
		case KeywordCompletionFiltersNone:
			return false
		case KeywordCompletionFiltersAll:
			return isFunctionLikeBodyKeyword(kind) || kind == ast.KindDeclareKeyword || kind == ast.KindModuleKeyword || kind == ast.KindTypeKeyword || kind == ast.KindNamespaceKeyword || kind == ast.KindAbstractKeyword || isTypeKeyword(kind) && kind != ast.KindUndefinedKeyword
		case KeywordCompletionFiltersFunctionLikeBodyKeywords:
			return isFunctionLikeBodyKeyword(kind)
		case KeywordCompletionFiltersClassElementKeywords:
			return isClassMemberCompletionKeyword(kind)
		case KeywordCompletionFiltersInterfaceElementKeywords:
			return isInterfaceOrTypeLiteralCompletionKeyword(kind)
		case KeywordCompletionFiltersConstructorParameterKeywords:
			return ast.IsParameterPropertyModifier(kind)
		case KeywordCompletionFiltersTypeAssertionKeywords:
			return isTypeKeyword(kind) || kind == ast.KindConstKeyword
		case KeywordCompletionFiltersTypeKeywords:
			return isTypeKeyword(kind)
		case KeywordCompletionFiltersTypeKeyword:
			return kind == ast.KindTypeKeyword
		default:
			panic(fmt.Sprintf("Unknown keyword filter: %v", keywordFilter))
		}
	})
	keywordCompletionsCache.Store(keywordFilter, result)
	return result
}
func isTypeScriptOnlyKeyword(kind ast.Kind) bool {
	switch kind {
	case ast.KindAbstractKeyword, ast.KindAnyKeyword, ast.KindBigIntKeyword, ast.KindBooleanKeyword, ast.KindDeclareKeyword, ast.KindEnumKeyword, ast.KindGlobalKeyword, ast.KindImplementsKeyword, ast.KindInferKeyword, ast.KindInterfaceKeyword, ast.KindIsKeyword, ast.KindKeyOfKeyword, ast.KindModuleKeyword, ast.KindNamespaceKeyword, ast.KindNeverKeyword, ast.KindNumberKeyword, ast.KindObjectKeyword, ast.KindOverrideKeyword, ast.KindPrivateKeyword, ast.KindProtectedKeyword, ast.KindPublicKeyword, ast.KindReadonlyKeyword, ast.KindStringKeyword, ast.KindSymbolKeyword, ast.KindTypeKeyword, ast.KindUniqueKeyword, ast.KindUnknownKeyword:
		return true
	default:
		return false
	}
}
func isFunctionLikeBodyKeyword(kind ast.Kind) bool {
	return kind == ast.KindAsyncKeyword || kind == ast.KindAwaitKeyword || kind == ast.KindUsingKeyword || kind == ast.KindAsKeyword || kind == ast.KindSatisfiesKeyword || kind == ast.KindTypeKeyword || !ast.IsContextualKeyword(kind) && !isClassMemberCompletionKeyword(kind)
}
func isClassMemberCompletionKeyword(kind ast.Kind) bool {
	switch kind {
	case ast.KindAbstractKeyword, ast.KindAccessorKeyword, ast.KindConstructorKeyword, ast.KindGetKeyword, ast.KindSetKeyword, ast.KindAsyncKeyword, ast.KindDeclareKeyword, ast.KindOverrideKeyword:
		return true
	default:
		return ast.IsClassMemberModifier(kind)
	}
}
func isInterfaceOrTypeLiteralCompletionKeyword(kind ast.Kind) bool {
	return kind == ast.KindReadonlyKeyword
}
func isContextualKeywordInAutoImportableExpressionSpace(keyword string) bool {
	return keyword == "abstract" || keyword == "async" || keyword == "await" || keyword == "declare" || keyword == "module" || keyword == "namespace" || keyword == "type" || keyword == "satisfies" || keyword == "as"
}
func getContextualKeywords(file *ast.SourceFile, contextToken ast.Handle, position int) []*lsproto.CompletionItem {
	var entries []*lsproto.CompletionItem
	if !contextToken.IsNil() {
		parent := contextToken.Parent()
		tokenLine := scanner.GetECMALineOfPosition(file, contextToken.End())
		currentLine := scanner.GetECMALineOfPosition(file, position)
		if (ast.IsImportDeclaration(parent) || ast.IsExportDeclaration(parent) && !parent.ModuleSpecifier().IsNil()) && contextToken == parent.ModuleSpecifier() && tokenLine == currentLine {
			entries = append(entries, &lsproto.CompletionItem{Label: scanner.TokenToString(ast.KindAssertKeyword), Kind: new(lsproto.CompletionItemKindKeyword), SortText: new(string(SortTextGlobalsOrKeywords))})
		}
	}
	return entries
}
func (l *LanguageService) getJSCompletionEntries(ctx context.Context, file *ast.SourceFile, position int, uniqueNames *collections.Set[string], sortedEntries []*CompletionItem) []*CompletionItem {
	nameTable := file.GetNameTable()
	for name, pos := range nameTable {
		if pos == position {
			continue
		}
		if !uniqueNames.Has(name) && scanner.IsIdentifierText(name, core.LanguageVariantStandard) {
			uniqueNames.Add(name)
			sortedEntries = append(sortedEntries, &CompletionItem{CompletionItem: &lsproto.CompletionItem{Label: name, Kind: new(lsproto.CompletionItemKindText), SortText: new(string(SortTextJavascriptIdentifiers)), CommitCharacters: new([]string{})}})
		}
	}
	return sortedEntries
}
func (l *LanguageService) getOptionalReplacementSpan(location ast.Handle, file *ast.SourceFile) *lsproto.Range {
	if !location.IsNil() && (location.Kind == ast.KindIdentifier || location.Kind == ast.KindPrivateIdentifier) {
		start := astnav.GetStartOfNode(location, file, false)
		lspRange, fidelity := l.createLspRangeFromBounds(start, location.End(), file)
		if fidelity.IsExact() {
			return &lspRange
		}
	}
	return nil
}
func isMemberCompletionKind(kind CompletionKind) bool {
	return kind == CompletionKindObjectPropertyDeclaration || kind == CompletionKindMemberLike || kind == CompletionKindPropertyAccess
}
func tryGetFunctionLikeBodyCompletionContainer(contextToken ast.Handle) ast.Handle {
	if contextToken.IsNil() {
		return ast.Handle{}
	}
	var prev ast.Handle
	container := ast.FindAncestorOrQuit(contextToken, func(node ast.Handle) ast.FindAncestorResult {
		if ast.IsClassLike(node) {
			return ast.FindAncestorQuit
		}
		if ast.IsFunctionLikeDeclaration(node) && prev == node.Body() {
			return ast.FindAncestorTrue
		}
		prev = node
		return ast.FindAncestorFalse
	})
	return container
}
func computeCommitCharactersAndIsNewIdentifier(contextToken ast.Handle, file *ast.SourceFile, position int) (isNewIdentifierLocation bool, defaultCommitCharacters []string) {
	if contextToken.IsNil() {
		return false, allCommitCharacters
	}
	containingNodeKind := contextToken.Parent().Kind
	tokenKind := keywordForNode(contextToken)
	switch tokenKind {
	case ast.KindCommaToken:
		switch containingNodeKind {
		case ast.KindCallExpression, ast.KindNewExpression:
			expression := contextToken.Parent().Expression()
			if getLineOfPosition(file, expression.End()) != getLineOfPosition(file, position) {
				return true, noCommaCommitCharacters
			}
			return true, allCommitCharacters
		case ast.KindBinaryExpression:
			return true, noCommaCommitCharacters
		case ast.KindConstructor, ast.KindFunctionType, ast.KindObjectLiteralExpression:
			return true, emptyCommitCharacters
		case ast.KindArrayLiteralExpression:
			return true, allCommitCharacters
		default:
			return false, allCommitCharacters
		}
	case ast.KindOpenParenToken:
		switch containingNodeKind {
		case ast.KindCallExpression, ast.KindNewExpression:
			expression := contextToken.Parent().Expression()
			if getLineOfPosition(file, expression.End()) != getLineOfPosition(file, position) {
				return true, noCommaCommitCharacters
			}
			return true, allCommitCharacters
		case ast.KindParenthesizedExpression:
			return true, noCommaCommitCharacters
		case ast.KindConstructor, ast.KindParenthesizedType:
			return true, emptyCommitCharacters
		default:
			return false, allCommitCharacters
		}
	case ast.KindOpenBracketToken:
		switch containingNodeKind {
		case ast.KindArrayLiteralExpression, ast.KindIndexSignature, ast.KindTupleType, ast.KindComputedPropertyName:
			return true, allCommitCharacters
		default:
			return false, allCommitCharacters
		}
	case ast.KindModuleKeyword, ast.KindNamespaceKeyword, ast.KindImportKeyword:
		return true, emptyCommitCharacters
	case ast.KindDotToken:
		switch containingNodeKind {
		case ast.KindModuleDeclaration:
			return true, emptyCommitCharacters
		default:
			return false, allCommitCharacters
		}
	case ast.KindOpenBraceToken:
		switch containingNodeKind {
		case ast.KindClassDeclaration, ast.KindObjectLiteralExpression:
			return true, emptyCommitCharacters
		default:
			return false, allCommitCharacters
		}
	case ast.KindEqualsToken:
		switch containingNodeKind {
		case ast.KindVariableDeclaration, ast.KindBinaryExpression:
			return true, allCommitCharacters
		default:
			return false, allCommitCharacters
		}
	case ast.KindTemplateHead:
		return containingNodeKind == ast.KindTemplateExpression, allCommitCharacters
	case ast.KindTemplateMiddle:
		return containingNodeKind == ast.KindTemplateSpan, allCommitCharacters
	case ast.KindAsyncKeyword:
		if containingNodeKind == ast.KindMethodDeclaration || containingNodeKind == ast.KindShorthandPropertyAssignment {
			return true, emptyCommitCharacters
		}
		return false, allCommitCharacters
	case ast.KindAsteriskToken:
		if containingNodeKind == ast.KindMethodDeclaration {
			return true, emptyCommitCharacters
		}
		return false, allCommitCharacters
	}
	if isClassMemberCompletionKeyword(tokenKind) {
		return true, emptyCommitCharacters
	}
	return false, allCommitCharacters
}
func keywordForNode(node ast.Handle) ast.Kind {
	if ast.IsIdentifier(node) {
		return scanner.IdentifierToKeywordKind(node)
	}
	return node.Kind
}

func getScopeNode(initialToken ast.Handle, position int, file *ast.SourceFile) ast.Handle {
	scope := initialToken
	for !scope.IsNil() && !positionBelongsToNode(scope, position, file) {
		scope = scope.Parent()
	}
	return scope
}
func isSnippetScope(scopeNode ast.Handle) bool {
	switch scopeNode.Kind {
	case ast.KindSourceFile, ast.KindTemplateExpression, ast.KindJsxExpression, ast.KindBlock:
		return true
	default:
		return ast.IsStatement(scopeNode)
	}
}

func isProbablyGlobalType(t *checker.Type, file *ast.SourceFile, typeChecker *checker.Checker) bool {
	selfSymbol := typeChecker.GetGlobalSymbol("self", ast.SymbolFlagsValue, nil)
	if selfSymbol != nil && typeChecker.GetTypeOfSymbolAtLocation(selfSymbol, file.ParseRoot()) == t {
		return true
	}
	globalSymbol := typeChecker.GetGlobalSymbol("global", ast.SymbolFlagsValue, nil)
	if globalSymbol != nil && typeChecker.GetTypeOfSymbolAtLocation(globalSymbol, file.ParseRoot()) == t {
		return true
	}
	globalThisSymbol := typeChecker.GetGlobalSymbol("globalThis", ast.SymbolFlagsValue, nil)
	if globalThisSymbol != nil && typeChecker.GetTypeOfSymbolAtLocation(globalThisSymbol, file.ParseRoot()) == t {
		return true
	}
	return false
}
func tryGetTypeLiteralNode(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	parent := node.Parent()
	switch node.Kind {
	case ast.KindOpenBraceToken:
		if ast.IsTypeLiteralNode(parent) {
			return parent
		}
	case ast.KindSemicolonToken, ast.KindCommaToken, ast.KindIdentifier:
		if parent.Kind == ast.KindPropertySignature && ast.IsTypeLiteralNode(parent.Parent()) {
			return parent.Parent()
		}
	}
	return ast.Handle{}
}
func getConstraintOfTypeArgumentProperty(node ast.Handle, typeChecker *checker.Checker) *checker.Type {
	if node.IsNil() {
		return nil
	}
	if ast.IsTypeNode(node) {
		constraint := typeChecker.GetTypeArgumentConstraint(node)
		if constraint != nil {
			return constraint
		}
	}
	t := getConstraintOfTypeArgumentProperty(node.Parent(), typeChecker)
	if t == nil {
		return nil
	}
	switch node.Kind {
	case ast.KindPropertySignature:
		reparsed := ast.GetReparsedHandle(node)
		if symbol := reparsed.Symbol(); symbol != nil {
			return typeChecker.GetTypeOfPropertyOfContextualType(t, symbol.Name)
		}
		if name, ok := ast.TryGetTextOfPropertyName(reparsed.Name()); ok {
			return typeChecker.GetTypeOfPropertyOfContextualType(t, name)
		}
		return nil
	case ast.KindColonToken:
		if node.Parent().Kind == ast.KindPropertySignature {
			return t
		}
	case ast.KindIntersectionType, ast.KindTypeLiteral, ast.KindUnionType:
		return t
	case ast.KindOpenBracketToken:
		return typeChecker.GetElementTypeOfArrayType(t)
	}
	return nil
}
func tryGetObjectLikeCompletionContainer(contextToken ast.Handle, position int, file *ast.SourceFile) ast.Handle {
	if contextToken.IsNil() {
		return ast.Handle{}
	}
	parent := contextToken.Parent()
	switch contextToken.Kind {
	case ast.KindOpenBraceToken, ast.KindCommaToken:
		if ast.IsObjectLiteralExpression(parent) || ast.IsObjectBindingPattern(parent) {
			return parent
		}
	case ast.KindAsteriskToken:
		if ast.IsMethodDeclaration(parent) && ast.IsObjectLiteralExpression(parent.Parent()) {
			return parent.Parent()
		}
	case ast.KindAsyncKeyword:
		if ast.IsObjectLiteralExpression(parent.Parent()) {
			return parent.Parent()
		}
	case ast.KindIdentifier:
		if contextToken.Text() == "async" && ast.IsShorthandPropertyAssignment(parent) {
			return parent.Parent()
		} else {
			if ast.IsObjectLiteralExpression(parent.Parent()) && (ast.IsSpreadAssignment(parent) || ast.IsShorthandPropertyAssignment(parent) && getLineOfPosition(file, contextToken.End()) != getLineOfPosition(file, position)) {
				return parent.Parent()
			}
			ancestorNode := ast.FindAncestor(parent, ast.IsPropertyAssignment)
			if !ancestorNode.IsNil() && lsutil.GetLastToken(ancestorNode, file) == contextToken && ast.IsObjectLiteralExpression(ancestorNode.Parent()) {
				return ancestorNode.Parent()
			}
		}
	default:
		if !parent.Parent().IsNil() && !parent.Parent().Parent().IsNil() && (ast.IsMethodDeclaration(parent.Parent()) || ast.IsGetAccessorDeclaration(parent.Parent()) || ast.IsSetAccessorDeclaration(parent.Parent())) && ast.IsObjectLiteralExpression(parent.Parent().Parent()) {
			return parent.Parent().Parent()
		}
		if ast.IsSpreadAssignment(parent) && ast.IsObjectLiteralExpression(parent.Parent()) {
			return parent.Parent()
		}
		ancestorNode := ast.FindAncestor(parent, ast.IsPropertyAssignment)
		if contextToken.Kind != ast.KindColonToken && !ancestorNode.IsNil() && lsutil.GetLastToken(ancestorNode, file) == contextToken && ast.IsObjectLiteralExpression(ancestorNode.Parent()) {
			return ancestorNode.Parent()
		}
	}
	return ast.Handle{}
}
func tryGetObjectLiteralContextualType(node ast.Handle, typeChecker *checker.Checker) *checker.Type {
	t := typeChecker.GetContextualType(node, checker.ContextFlagsNone)
	if t != nil {
		return t
	}
	parent := ast.WalkUpParenthesizedExpressions(node.Parent())
	if ast.IsBinaryExpression(parent) && parent.BinaryExpressionOperatorToken().Kind == ast.KindEqualsToken && node == parent.BinaryExpressionLeft() {
		return typeChecker.GetTypeAtLocation(parent)
	}
	if ast.IsExpression(parent) {
		return typeChecker.GetContextualType(parent, checker.ContextFlagsNone)
	}
	return nil
}
func getPropertiesForObjectExpression(contextualType *checker.Type, completionsType *checker.Type, obj ast.Handle, typeChecker *checker.Checker) []*ast.Symbol {
	hasCompletionsType := completionsType != nil && completionsType != contextualType
	var types []*checker.Type
	if contextualType.IsUnion() {
		types = contextualType.Types()
	} else {
		types = []*checker.Type{contextualType}
	}
	promiseFilteredContextualType := typeChecker.GetUnionType(core.Filter(types, func(t *checker.Type) bool {
		return typeChecker.GetPromisedTypeOfPromise(t) == nil
	}))
	var t *checker.Type
	if hasCompletionsType && completionsType.Flags()&checker.TypeFlagsAnyOrUnknown == 0 {
		t = typeChecker.GetUnionType([]*checker.Type{promiseFilteredContextualType, completionsType})
	} else {
		t = promiseFilteredContextualType
	}
	hasDeclarationOtherThanSelf := func(member *ast.Symbol) bool {
		if len(member.Declarations) == 0 {
			return true
		}
		return ast.SomeDeclaration(member, func(decl ast.Handle) bool {
			return decl.Parent() != obj
		})
	}
	properties := getApparentProperties(t, obj, typeChecker)
	if t.IsClass() && containsNonPublicProperties(properties) {
		return nil
	} else if hasCompletionsType {
		return core.Filter(properties, hasDeclarationOtherThanSelf)
	} else {
		return properties
	}
}
func getApparentProperties(t *checker.Type, node ast.Handle, typeChecker *checker.Checker) []*ast.Symbol {
	if !t.IsUnion() {
		return typeChecker.GetApparentProperties(t)
	}
	return typeChecker.GetAllPossiblePropertiesOfTypes(core.Filter(t.Types(), func(memberType *checker.Type) bool {
		return !(memberType.Flags()&checker.TypeFlagsPrimitive != 0 || typeChecker.IsArrayLikeType(memberType) || typeChecker.IsTypeInvalidDueToUnionDiscriminant(memberType, node) || typeChecker.TypeHasCallOrConstructSignatures(memberType) || memberType.IsClass() && containsNonPublicProperties(typeChecker.GetApparentProperties(memberType)))
	}))
}
func containsNonPublicProperties(props []*ast.Symbol) bool {
	return core.Some(props, func(p *ast.Symbol) bool {
		return checker.GetDeclarationModifierFlagsFromSymbol(p)&ast.ModifierFlagsNonPublicAccessibilityModifier != 0
	})
}

func filterObjectMembersList(contextualMemberSymbols []*ast.Symbol, existingMembers []ast.Handle, file *ast.SourceFile, position int, typeChecker *checker.Checker) (filteredMembers []*ast.Symbol, spreadMemberNames collections.Set[string]) {
	if len(existingMembers) == 0 {
		return contextualMemberSymbols, collections.Set[string]{}
	}
	membersDeclaredBySpreadAssignment := collections.Set[string]{}
	existingMemberNames := collections.Set[string]{}
	for _, member := range existingMembers {
		if member.Kind != ast.KindPropertyAssignment && member.Kind != ast.KindShorthandPropertyAssignment && member.Kind != ast.KindBindingElement && member.Kind != ast.KindMethodDeclaration && member.Kind != ast.KindGetAccessor && member.Kind != ast.KindSetAccessor && member.Kind != ast.KindSpreadAssignment {
			continue
		}
		if isCurrentlyEditingNode(member, file, position) {
			continue
		}
		var existingName string
		if ast.IsSpreadAssignment(member) {
			setMemberDeclaredBySpreadAssignment(member, &membersDeclaredBySpreadAssignment, typeChecker)
		} else if ast.IsBindingElement(member) && !member.PropertyName().IsNil() {
			if member.PropertyName().Kind == ast.KindIdentifier {
				existingName = member.PropertyName().Text()
			}
		} else {
			name := ast.GetNameOfDeclaration(member)
			if !name.IsNil() && ast.IsPropertyNameLiteral(name) {
				existingName = name.Text()
			}
		}
		if existingName != "" {
			existingMemberNames.Add(existingName)
		}
	}
	filteredSymbols := core.Filter(contextualMemberSymbols, func(m *ast.Symbol) bool {
		return !existingMemberNames.Has(m.Name)
	})
	return filteredSymbols, membersDeclaredBySpreadAssignment
}
func isCurrentlyEditingNode(node ast.Handle, file *ast.SourceFile, position int) bool {
	start := astnav.GetStartOfNode(node, file, false)
	return start <= position && position <= node.End()
}
func setMemberDeclaredBySpreadAssignment(declaration ast.Handle, members *collections.Set[string], typeChecker *checker.Checker) {
	expression := declaration.Expression()
	symbol := typeChecker.GetSymbolAtLocation(expression)
	var t *checker.Type
	if symbol != nil {
		t = typeChecker.GetTypeOfSymbolAtLocation(symbol, expression)
	}
	var properties []*ast.Symbol
	if t != nil && t.Flags()&checker.TypeFlagsStructuredType != 0 {
		properties = t.AsStructuredType().Properties()
	}
	for _, property := range properties {
		members.Add(property.Name)
	}
}

func tryGetConstructorLikeCompletionContainer(contextToken ast.Handle) ast.Handle {
	if contextToken.IsNil() {
		return ast.Handle{}
	}
	parent := contextToken.Parent()
	switch contextToken.Kind {
	case ast.KindOpenParenToken, ast.KindCommaToken:
		if ast.IsConstructorDeclaration(parent) {
			return parent
		}
		return ast.Handle{}
	default:
		if isConstructorParameterCompletion(contextToken) {
			return parent.Parent()
		}
	}
	return ast.Handle{}
}
func isConstructorParameterCompletion(node ast.Handle) bool {
	return !node.Parent().IsNil() && ast.IsParameterDeclaration(node.Parent()) && ast.IsConstructorDeclaration(node.Parent().Parent()) && (ast.IsParameterPropertyModifier(node.Kind) || ast.IsDeclarationName(node))
}

func tryGetObjectTypeDeclarationCompletionContainer(file *ast.SourceFile, contextToken ast.Handle, location ast.Handle, position int) ast.Handle {
	switch location.Kind {
	case ast.KindSyntaxList:
		if ast.IsObjectTypeDeclaration(location.Parent()) {
			return location.Parent()
		}
		return ast.Handle{}
	case ast.KindEndOfFile:
		stmtList := location.Parent().StatementList()
		if stmtList != 0 && contextToken.Store().ListLen(stmtList) > 0 && ast.IsObjectTypeDeclaration(contextToken.Store().ListAt(stmtList, contextToken.Store().ListLen(stmtList)-1)) {
			cls := contextToken.Store().ListAt(stmtList, contextToken.Store().ListLen(stmtList)-1)
			if astnav.FindChildOfKind(cls, ast.KindCloseBraceToken, file).IsNil() {
				return cls
			}
		}
	case ast.KindPrivateIdentifier:
		if ast.IsPropertyDeclaration(location.Parent()) {
			return ast.FindAncestor(location, ast.IsClassLike)
		}
	case ast.KindIdentifier:
		originalKeywordKind := scanner.IdentifierToKeywordKind(location)
		if originalKeywordKind != ast.KindUnknown {
			return ast.Handle{}
		}
		if ast.IsPropertyDeclaration(location.Parent()) && location.Parent().Initializer() == location {
			return ast.Handle{}
		}
		if isFromObjectTypeDeclaration(location) {
			return ast.FindAncestor(location, ast.IsObjectTypeDeclaration)
		}
	}
	if contextToken.IsNil() {
		return ast.Handle{}
	}
	if location.Kind == ast.KindConstructorKeyword || (ast.IsIdentifier(contextToken) && ast.IsPropertyDeclaration(contextToken.Parent()) && ast.IsClassLike(location)) {
		return ast.FindAncestor(contextToken, ast.IsClassLike)
	}
	switch contextToken.Kind {
	case ast.KindEqualsToken:
		return ast.Handle{}
	case ast.KindSemicolonToken, ast.KindCloseBraceToken:
		if isFromObjectTypeDeclaration(location) && location.Parent().Name() == location {
			return location.Parent().Parent()
		}
		if ast.IsObjectTypeDeclaration(location) {
			return location
		}
		return ast.Handle{}
	case ast.KindOpenBraceToken, ast.KindCommaToken:
		if ast.IsObjectTypeDeclaration(contextToken.Parent()) {
			return contextToken.Parent()
		}
		return ast.Handle{}
	default:
		if ast.IsObjectTypeDeclaration(location) {
			if getLineOfPosition(file, contextToken.End()) != getLineOfPosition(file, position) {
				return location
			}
			isValidKeyword := core.IfElse(ast.IsClassLike(contextToken.Parent().Parent()), isClassMemberCompletionKeyword, isInterfaceOrTypeLiteralCompletionKeyword)
			if isValidKeyword(contextToken.Kind) || contextToken.Kind == ast.KindAsteriskToken || ast.IsIdentifier(contextToken) && isValidKeyword(scanner.IdentifierToKeywordKind(contextToken)) {
				return contextToken.Parent().Parent()
			}
		}
		return ast.Handle{}
	}
}
func isFromObjectTypeDeclaration(node ast.Handle) bool {
	return !node.Parent().IsNil() && ast.IsClassOrTypeElement(node.Parent()) && ast.IsObjectTypeDeclaration(node.Parent().Parent())
}

func filterClassMembersList(baseSymbols []*ast.Symbol, existingMembers []ast.Handle, classElementModifierFlags ast.ModifierFlags, file *ast.SourceFile, position int) []*ast.Symbol {
	existingMemberNames := collections.Set[string]{}
	for _, member := range existingMembers {
		if member.Kind != ast.KindPropertyDeclaration && member.Kind != ast.KindMethodDeclaration && member.Kind != ast.KindGetAccessor && member.Kind != ast.KindSetAccessor {
			continue
		}
		if isCurrentlyEditingNode(member, file, position) {
			continue
		}
		if member.ModifierFlags()&ast.ModifierFlagsPrivate != 0 {
			continue
		}
		if ast.IsStatic(member) != (classElementModifierFlags&ast.ModifierFlagsStatic != 0) {
			continue
		}
		existingName := ast.GetPropertyNameForPropertyNameNode(member.Name())
		if existingName != "" {
			existingMemberNames.Add(existingName)
		}
	}
	return core.Filter(baseSymbols, func(propertySymbol *ast.Symbol) bool {
		return !existingMemberNames.Has(ast.SymbolName(propertySymbol)) && len(propertySymbol.Declarations) > 0 && checker.GetDeclarationModifierFlagsFromSymbol(propertySymbol)&ast.ModifierFlagsPrivate == 0 && !(propertySymbol.ValueDeclaration != 0 && ast.IsPrivateIdentifierClassElementDeclaration(ast.NodeOf(propertySymbol.ValueDeclaration)))
	})
}
func tryGetContainingJsxElement(contextToken ast.Handle, file *ast.SourceFile) ast.Handle {
	if contextToken.IsNil() {
		return ast.Handle{}
	}
	parent := contextToken.Parent()
	switch contextToken.Kind {
	case ast.KindGreaterThanToken, ast.KindLessThanSlashToken, ast.KindSlashToken, ast.KindIdentifier, ast.KindPropertyAccessExpression, ast.KindJsxNamespacedName, ast.KindJsxAttributes, ast.KindJsxAttribute, ast.KindJsxSpreadAttribute:
		if !parent.IsNil() && (parent.Kind == ast.KindJsxSelfClosingElement || parent.Kind == ast.KindJsxOpeningElement) {
			if contextToken.Kind == ast.KindGreaterThanToken {
				precedingToken := astnav.FindPrecedingToken(file, contextToken.Pos())
				if len(parent.TypeArguments()) == 0 || !precedingToken.IsNil() && precedingToken.Kind == ast.KindSlashToken {
					return ast.Handle{}
				}
			}
			return parent
		} else if !parent.IsNil() && ast.IsJsxNamespacedName(parent) && !parent.Parent().IsNil() && (parent.Parent().Kind == ast.KindJsxSelfClosingElement || parent.Parent().Kind == ast.KindJsxOpeningElement) {
			return parent.Parent()
		} else if !parent.IsNil() && parent.Kind == ast.KindJsxAttribute {
			return parent.Parent().Parent()
		}
	case ast.KindStringLiteral:
		if !parent.IsNil() && (parent.Kind == ast.KindJsxAttribute || parent.Kind == ast.KindJsxSpreadAttribute) {
			return parent.Parent().Parent()
		}
	case ast.KindCloseBraceToken:
		if !parent.IsNil() && parent.Kind == ast.KindJsxExpression && !parent.Parent().IsNil() && parent.Parent().Kind == ast.KindJsxAttribute {
			return parent.Parent().Parent().Parent()
		}
		if !parent.IsNil() && parent.Kind == ast.KindJsxSpreadAttribute {
			return parent.Parent().Parent()
		}
	}
	return ast.Handle{}
}

func filterJsxAttributes(symbols []*ast.Symbol, attributes []ast.Handle, file *ast.SourceFile, position int, typeChecker *checker.Checker) (filteredMembers []*ast.Symbol, spreadMemberNames *collections.Set[string]) {
	existingNames := collections.Set[string]{}
	membersDeclaredBySpreadAssignment := collections.Set[string]{}
	for _, attr := range attributes {
		if isCurrentlyEditingNode(attr, file, position) {
			continue
		}
		if attr.Kind == ast.KindJsxAttribute {
			existingNames.Add(attr.Name().Text())
		} else if ast.IsJsxSpreadAttribute(attr) {
			setMemberDeclaredBySpreadAssignment(attr, &membersDeclaredBySpreadAssignment, typeChecker)
		}
	}
	return core.Filter(symbols, func(a *ast.Symbol) bool {
		return !existingNames.Has(a.Name)
	}), &membersDeclaredBySpreadAssignment
}
func isTypeKeywordTokenOrIdentifier(node ast.Handle) bool {
	return ast.IsTypeKeywordToken(node) || ast.IsIdentifier(node) && scanner.IdentifierToKeywordKind(node) == ast.KindTypeKeyword
}

func (l *LanguageService) setItemDefaults(ctx context.Context, position int, file *ast.SourceFile, items []*CompletionItem, defaultCommitCharacters *[]string, optionalReplacementSpan *lsproto.Range) *lsproto.CompletionItemDefaults {
	var itemDefaults *lsproto.CompletionItemDefaults
	if defaultCommitCharacters != nil {
		supportsItemCommitCharacters := clientSupportsItemCommitCharacters(ctx)
		if clientSupportsDefaultCommitCharacters(ctx) && supportsItemCommitCharacters {
			itemDefaults = &lsproto.CompletionItemDefaults{CommitCharacters: defaultCommitCharacters}
		} else if supportsItemCommitCharacters {
			for _, item := range items {
				if item.CommitCharacters == nil {
					item.CommitCharacters = defaultCommitCharacters
				}
			}
		}
	}
	if optionalReplacementSpan != nil {
		end, fidelity := l.createLspPosition(position, file)
		if !fidelity.IsExact() {
			return itemDefaults
		}
		insertRange := lsproto.Range{Start: optionalReplacementSpan.Start, End: end}
		if clientSupportsDefaultEditRange(ctx) {
			itemDefaults = core.OrElse(itemDefaults, &lsproto.CompletionItemDefaults{})
			itemDefaults.EditRange = &lsproto.RangeOrEditRangeWithInsertReplace{EditRangeWithInsertReplace: &lsproto.EditRangeWithInsertReplace{Insert: insertRange, Replace: *optionalReplacementSpan}}
			for _, item := range items {
				if item.InsertText != nil && item.TextEdit == nil {
					item.TextEdit = &lsproto.TextEditOrInsertReplaceEdit{InsertReplaceEdit: &lsproto.InsertReplaceEdit{NewText: *item.InsertText, Insert: insertRange, Replace: *optionalReplacementSpan}}
					item.InsertText = nil
				}
			}
		} else if clientSupportsItemInsertReplace(ctx) {
			for _, item := range items {
				if item.TextEdit == nil {
					item.TextEdit = &lsproto.TextEditOrInsertReplaceEdit{InsertReplaceEdit: &lsproto.InsertReplaceEdit{NewText: *core.OrElse(item.InsertText, &item.Label), Insert: insertRange, Replace: *optionalReplacementSpan}}
				}
			}
		}
	}
	return itemDefaults
}
func (l *LanguageService) specificKeywordCompletionInfo(ctx context.Context, position int, file *ast.SourceFile, items []*CompletionItem, isNewIdentifierLocation bool, optionalReplacementSpan *lsproto.Range) *CompletionList {
	defaultCommitCharacters := getDefaultCommitCharacters(isNewIdentifierLocation)
	itemDefaults := l.setItemDefaults(ctx, position, file, items, &defaultCommitCharacters, optionalReplacementSpan)
	return &CompletionList{IsIncomplete: false, ItemDefaults: itemDefaults, Items: items}
}
func (l *LanguageService) getJsxClosingTagCompletion(ctx context.Context, location ast.Handle, file *ast.SourceFile, position int) *CompletionList {
	jsxClosingElement := ast.FindAncestorOrQuit(location, func(node ast.Handle) ast.FindAncestorResult {
		switch node.Kind {
		case ast.KindJsxClosingElement:
			return ast.FindAncestorTrue
		case ast.KindLessThanSlashToken, ast.KindGreaterThanToken, ast.KindIdentifier, ast.KindPropertyAccessExpression:
			return ast.FindAncestorFalse
		default:
			return ast.FindAncestorQuit
		}
	})
	if jsxClosingElement.IsNil() {
		return nil
	}
	hasClosingAngleBracket := !astnav.FindChildOfKind(jsxClosingElement, ast.KindGreaterThanToken, file).IsNil()
	tagName := jsxClosingElement.Parent().JsxElementOpeningElement().TagName()
	closingTag := scanner.GetTextOfNode(tagName)
	fullClosingTag := closingTag + core.IfElse(hasClosingAngleBracket, "", ">")
	optionalReplacementSpan, fidelity := l.createLspRangeFromNode(jsxClosingElement.TagName(), file)
	if !fidelity.IsExact() {
		return nil
	}
	defaultCommitCharacters := getDefaultCommitCharacters(false)
	lspItem := l.createLSPCompletionItem(ctx, fullClosingTag, "", "", SortTextLocationPriority, lsutil.ScriptElementKindClassElement, lsutil.ScriptElementKindModifierNone, nil, nil, nil, file, position, true, false, false, false, "", nil, nil, nil)
	item := &CompletionItem{CompletionItem: lspItem}
	items := []*CompletionItem{item}
	itemDefaults := l.setItemDefaults(ctx, position, file, items, &defaultCommitCharacters, &optionalReplacementSpan)
	return &CompletionList{IsIncomplete: false, ItemDefaults: itemDefaults, Items: items}
}
func (l *LanguageService) createLSPCompletionItem(ctx context.Context, name string, insertText string, filterText string, sortText SortText, elementKind lsutil.ScriptElementKind, kindModifiers lsutil.ScriptElementKindModifier, replacementSpan *lsproto.Range, commitCharacters *[]string, labelDetails *lsproto.CompletionItemLabelDetails, file *ast.SourceFile, position int, isMemberCompletion bool, isSnippet bool, hasAction bool, preselect bool, source string, autoImportFix *lsproto.AutoImportFix, additionalTextEdits *[]*lsproto.TextEdit, detail *string) *lsproto.CompletionItem {
	kind := getCompletionsSymbolKind(elementKind)
	data := &lsproto.CompletionItemData{FileName: file.OriginalFileName(), Position: int32(position), SupplementalFileIndex: supplementalFileIndex(file), Source: source, Name: name, AutoImport: autoImportFix}
	var textEdit *lsproto.TextEditOrInsertReplaceEdit
	if replacementSpan != nil {
		textEdit = &lsproto.TextEditOrInsertReplaceEdit{TextEdit: &lsproto.TextEdit{NewText: core.IfElse(insertText == "", name, insertText), Range: *replacementSpan}}
	}
	wordSize, wordStart := getWordLengthAndStart(file, position)
	dotAccessor := getDotAccessor(file, position-wordSize)
	if filterText == "" {
		filterText = getFilterText(file, position, insertText, name, wordStart, dotAccessor)
	}
	var tags *[]lsproto.CompletionItemTag
	if isMemberCompletion && kindModifiers&lsutil.ScriptElementKindModifierOptional != 0 {
		if insertText == "" {
			insertText = name
		}
		if filterText == "" || isSnippet {
			filterText = name
		}
		name = name + "?"
	}
	if kindModifiers&lsutil.ScriptElementKindModifierDeprecated != 0 {
		tags = &[]lsproto.CompletionItemTag{lsproto.CompletionItemTagDeprecated}
	}
	if hasAction && source != "" {
	}
	var insertTextFormat *lsproto.InsertTextFormat
	if isSnippet {
		insertTextFormat = new(lsproto.InsertTextFormatSnippet)
	}
	return &lsproto.CompletionItem{Label: name, LabelDetails: labelDetails, Kind: &kind, Tags: tags, Detail: detail, Preselect: boolToPtr(preselect), SortText: new(string(sortText)), FilterText: strPtrTo(filterText), InsertText: strPtrTo(insertText), InsertTextFormat: insertTextFormat, TextEdit: textEdit, CommitCharacters: commitCharacters, AdditionalTextEdits: additionalTextEdits, Data: data}
}
func (l *LanguageService) getLabelCompletionsAtPosition(ctx context.Context, node ast.Handle, file *ast.SourceFile, position int, optionalReplacementSpan *lsproto.Range) *CompletionList {
	items := l.getLabelStatementCompletions(ctx, node, file, position)
	if len(items) == 0 {
		return nil
	}
	defaultCommitCharacters := getDefaultCommitCharacters(false)
	itemDefaults := l.setItemDefaults(ctx, position, file, items, &defaultCommitCharacters, optionalReplacementSpan)
	return &CompletionList{IsIncomplete: false, ItemDefaults: itemDefaults, Items: items}
}
func (l *LanguageService) getLabelStatementCompletions(ctx context.Context, node ast.Handle, file *ast.SourceFile, position int) []*CompletionItem {
	var uniques collections.Set[string]
	var items []*CompletionItem
	current := node
	for !current.IsNil() {
		if ast.IsFunctionLike(current) {
			break
		}
		if ast.IsLabeledStatement(current) {
			name := current.Label().Text()
			if !uniques.Has(name) {
				uniques.Add(name)
				lspItem := l.createLSPCompletionItem(ctx, name, "", "", SortTextLocationPriority, lsutil.ScriptElementKindLabel, lsutil.ScriptElementKindModifierNone, nil, nil, nil, file, position, false, false, false, false, "", nil, nil, nil)
				items = append(items, &CompletionItem{CompletionItem: lspItem})
			}
		}
		current = current.Parent()
	}
	return items
}
func isCompletionListBlocker(contextToken ast.Handle, previousToken ast.Handle, location ast.Handle, file *ast.SourceFile, position int, typeChecker *checker.Checker) bool {
	return isInStringOrRegularExpressionOrTemplateLiteral(contextToken, position) || isSolelyIdentifierDefinitionLocation(contextToken, previousToken, file, position, typeChecker) || isDotOfNumericLiteral(contextToken, file) || isInJsxText(contextToken, location) || ast.IsBigIntLiteral(contextToken)
}
func isInStringOrRegularExpressionOrTemplateLiteral(contextToken ast.Handle, position int) bool {
	return (ast.IsRegularExpressionLiteral(contextToken) || ast.IsStringTextContainingNode(contextToken)) && contextToken.Loc().ContainsExclusive(position) || position == contextToken.End() && (ast.IsUnterminatedLiteral(contextToken) || ast.IsRegularExpressionLiteral(contextToken))
}

func isSolelyIdentifierDefinitionLocation(contextToken ast.Handle, previousToken ast.Handle, file *ast.SourceFile, position int, typeChecker *checker.Checker) bool {
	parent := contextToken.Parent()
	containingNodeKind := parent.Kind
	switch contextToken.Kind {
	case ast.KindCommaToken:
		return containingNodeKind == ast.KindVariableDeclaration || isVariableDeclarationListButNotTypeArgument(contextToken, file, typeChecker) || containingNodeKind == ast.KindVariableStatement || containingNodeKind == ast.KindEnumDeclaration || isFunctionLikeButNotConstructor(containingNodeKind) || containingNodeKind == ast.KindInterfaceDeclaration || containingNodeKind == ast.KindArrayBindingPattern || containingNodeKind == ast.KindTypeAliasDeclaration || (ast.IsClassLike(parent) && parent.TypeParameterList() != 0 && parent.Store().ListLoc(parent.TypeParameterList()).End() >= contextToken.Pos())
	case ast.KindDotToken:
		return containingNodeKind == ast.KindArrayBindingPattern
	case ast.KindColonToken:
		return containingNodeKind == ast.KindBindingElement
	case ast.KindOpenBracketToken:
		return containingNodeKind == ast.KindArrayBindingPattern
	case ast.KindOpenParenToken:
		return containingNodeKind == ast.KindCatchClause || isFunctionLikeButNotConstructor(containingNodeKind)
	case ast.KindOpenBraceToken:
		return containingNodeKind == ast.KindEnumDeclaration
	case ast.KindLessThanToken:
		return containingNodeKind == ast.KindClassDeclaration || containingNodeKind == ast.KindClassExpression || containingNodeKind == ast.KindInterfaceDeclaration || containingNodeKind == ast.KindTypeAliasDeclaration || ast.IsFunctionLikeKind(containingNodeKind)
	case ast.KindStaticKeyword:
		return containingNodeKind == ast.KindPropertyDeclaration && !ast.IsClassLike(parent.Parent())
	case ast.KindDotDotDotToken:
		return containingNodeKind == ast.KindParameter || (!parent.Parent().IsNil() && parent.Parent().Kind == ast.KindArrayBindingPattern)
	case ast.KindPublicKeyword, ast.KindPrivateKeyword, ast.KindProtectedKeyword:
		return containingNodeKind == ast.KindParameter && !ast.IsConstructorDeclaration(parent.Parent())
	case ast.KindAsKeyword:
		return containingNodeKind == ast.KindImportSpecifier || containingNodeKind == ast.KindExportSpecifier || containingNodeKind == ast.KindNamespaceImport
	case ast.KindGetKeyword, ast.KindSetKeyword:
		return !isFromObjectTypeDeclaration(contextToken)
	case ast.KindIdentifier:
		if (containingNodeKind == ast.KindImportSpecifier || containingNodeKind == ast.KindExportSpecifier) && contextToken == parent.Name() && contextToken.Text() == "type" {
			return false
		}
		ancestorVariableDeclaration := ast.FindAncestor(parent, ast.IsVariableDeclaration)
		if !ancestorVariableDeclaration.IsNil() && getLineEndOfPosition(file, contextToken.End()) < position {
			return false
		}
	case ast.KindClassKeyword, ast.KindEnumKeyword, ast.KindInterfaceKeyword, ast.KindFunctionKeyword, ast.KindVarKeyword, ast.KindImportKeyword, ast.KindLetKeyword, ast.KindConstKeyword, ast.KindInferKeyword:
		return true
	case ast.KindTypeKeyword:
		return containingNodeKind != ast.KindImportSpecifier
	case ast.KindAsteriskToken:
		return ast.IsFunctionLike(parent) && !ast.IsMethodDeclaration(parent)
	}
	tokenKind := keywordForNode(contextToken)
	if isClassMemberCompletionKeyword(tokenKind) && isFromObjectTypeDeclaration(contextToken) {
		return false
	}
	if isConstructorParameterCompletion(contextToken) {
		if !ast.IsIdentifier(contextToken) || ast.IsParameterPropertyModifier(tokenKind) || isCurrentlyEditingNode(contextToken, file, position) {
			return false
		}
	}
	switch keywordForNode(contextToken) {
	case ast.KindAbstractKeyword, ast.KindClassKeyword, ast.KindDeclareKeyword, ast.KindEnumKeyword, ast.KindFunctionKeyword, ast.KindInterfaceKeyword, ast.KindLetKeyword, ast.KindPrivateKeyword, ast.KindProtectedKeyword, ast.KindPublicKeyword, ast.KindStaticKeyword, ast.KindVarKeyword:
		return true
	case ast.KindAsyncKeyword:
		return ast.IsPropertyDeclaration(contextToken.Parent())
	}
	ancestorClassLike := ast.FindAncestor(parent, ast.IsClassLike)
	if !ancestorClassLike.IsNil() && contextToken == previousToken && isPreviousPropertyDeclarationTerminated(contextToken, file, position) {
		return false
	}
	ancestorPropertyDeclaration := ast.FindAncestor(parent, ast.IsPropertyDeclaration)
	if !ancestorPropertyDeclaration.IsNil() && contextToken != previousToken && ast.IsClassLike(previousToken.Parent().Parent()) && position <= previousToken.End() {
		if isPreviousPropertyDeclarationTerminated(contextToken, file, previousToken.End()) {
			return false
		} else if contextToken.Kind != ast.KindEqualsToken && (ast.IsInitializedProperty(ancestorPropertyDeclaration) || !ancestorPropertyDeclaration.Type().IsNil()) {
			return true
		}
	}
	if tokenKind == ast.KindConstKeyword {
		return true
	}
	return ast.IsDeclarationName(contextToken) && !ast.IsShorthandPropertyAssignment(parent) && !ast.IsJsxAttribute(parent) && !((ast.IsClassLike(parent) || ast.IsInterfaceDeclaration(parent) || ast.IsTypeParameterDeclaration(parent)) && (contextToken != previousToken || position > previousToken.End()))
}
func isVariableDeclarationListButNotTypeArgument(node ast.Handle, file *ast.SourceFile, typeChecker *checker.Checker) bool {
	return node.Parent().Kind == ast.KindVariableDeclarationList && !isPossiblyTypeArgumentPosition(node, file, typeChecker)
}
func isFunctionLikeButNotConstructor(kind ast.Kind) bool {
	return ast.IsFunctionLikeKind(kind) && kind != ast.KindConstructor
}
func isPreviousPropertyDeclarationTerminated(contextToken ast.Handle, file *ast.SourceFile, position int) bool {
	return contextToken.Kind != ast.KindEqualsToken && (contextToken.Kind == ast.KindSemicolonToken || getLineOfPosition(file, contextToken.End()) != getLineOfPosition(file, position))
}
func isDotOfNumericLiteral(contextToken ast.Handle, file *ast.SourceFile) bool {
	if contextToken.Kind == ast.KindNumericLiteral {
		text := file.Text()[contextToken.Pos():contextToken.End()]
		r, _ := utf8.DecodeLastRuneInString(text)
		return r == '.'
	}
	return false
}
func isInJsxText(contextToken ast.Handle, location ast.Handle) bool {
	if contextToken.Kind == ast.KindJsxText {
		return true
	}
	if contextToken.Kind == ast.KindGreaterThanToken && !contextToken.Parent().IsNil() {
		if location == contextToken.Parent() && ast.IsJsxOpeningLikeElement(location) {
			return false
		}
		if contextToken.Parent().Kind == ast.KindJsxOpeningElement {
			return location.Parent().Kind != ast.KindJsxOpeningElement
		}
		if contextToken.Parent().Kind == ast.KindJsxClosingElement || contextToken.Parent().Kind == ast.KindJsxSelfClosingElement {
			return !contextToken.Parent().Parent().IsNil() && contextToken.Parent().Parent().Kind == ast.KindJsxElement
		}
	}
	return false
}
func clientSupportsItemLabelDetails(ctx context.Context) bool {
	return lsproto.GetClientCapabilities(ctx).TextDocument.Completion.CompletionItem.LabelDetailsSupport
}
func clientSupportsItemSnippet(ctx context.Context) bool {
	return lsproto.GetClientCapabilities(ctx).TextDocument.Completion.CompletionItem.SnippetSupport
}
func clientSupportsItemCommitCharacters(ctx context.Context) bool {
	return lsproto.GetClientCapabilities(ctx).TextDocument.Completion.CompletionItem.CommitCharactersSupport
}
func clientSupportsItemInsertReplace(ctx context.Context) bool {
	return lsproto.GetClientCapabilities(ctx).TextDocument.Completion.CompletionItem.InsertReplaceSupport
}
func clientSupportsDefaultCommitCharacters(ctx context.Context) bool {
	return slices.Contains(lsproto.GetClientCapabilities(ctx).TextDocument.Completion.CompletionList.ItemDefaults, "commitCharacters")
}
func clientSupportsDefaultEditRange(ctx context.Context) bool {
	return slices.Contains(lsproto.GetClientCapabilities(ctx).TextDocument.Completion.CompletionList.ItemDefaults, "editRange")
}

type argumentInfoForCompletions struct {
	invocation    ast.Handle
	argumentIndex int
	argumentCount int
}

func getArgumentInfoForCompletions(node ast.Handle, position int, file *ast.SourceFile, typeChecker *checker.Checker) *argumentInfoForCompletions {
	info := getImmediatelyContainingArgumentInfo(node, position, file, typeChecker)
	if info == nil || info.isTypeParameterList || info.invocation.callInvocation == nil {
		return nil
	}
	return &argumentInfoForCompletions{invocation: info.invocation.callInvocation.node, argumentIndex: info.argumentIndex, argumentCount: info.argumentCount}
}

const (
	SourceThisProperty                 = "ThisProperty/"
	SourceClassMemberSnippet           = "ClassMemberSnippet/"
	SourceTypeOnlyAlias                = "TypeOnlyAlias/"
	SourceObjectLiteralMethodSnippet   = "ObjectLiteralMethodSnippet/"
	SourceSwitchCases                  = "SwitchCases/"
	SourceObjectLiteralMemberWithComma = "ObjectLiteralMemberWithComma/"
)

func (l *LanguageService) ResolveCompletionItem(ctx context.Context, item *lsproto.CompletionItem, data *lsproto.CompletionItemData) (*lsproto.CompletionItem, error) {
	if data == nil {
		return nil, errors.New("completion item data is nil")
	}
	program, file := l.tryGetProgramAndFile(data.FileName)
	if file == nil {
		return nil, fmt.Errorf("file not found: %s", data.FileName)
	}
	file = sourceFileForSupplementalFileIndex(file, data.SupplementalFileIndex)
	if file == nil {
		return nil, fmt.Errorf("supplemental source file index not found: %d", *data.SupplementalFileIndex)
	}
	checker, done := program.GetTypeCheckerForFile(ctx, file)
	defer done()
	return l.getCompletionItemDetails(ctx, program, checker, int(data.Position), file, item, data), nil
}
func getCompletionDocumentationFormat(ctx context.Context) lsproto.MarkupKind {
	return lsproto.PreferredMarkupKind(lsproto.GetClientCapabilities(ctx).TextDocument.Completion.CompletionItem.DocumentationFormat)
}
func (l *LanguageService) getCompletionItemDetails(ctx context.Context, program *compiler.Program, checker *checker.Checker, position int, file *ast.SourceFile, item *lsproto.CompletionItem, data *lsproto.CompletionItemData) *lsproto.CompletionItem {
	docFormat := getCompletionDocumentationFormat(ctx)
	contextToken, previousToken := getRelevantTokens(position, file)
	if IsInString(file, position, previousToken) {
		return l.getStringLiteralCompletionDetails(ctx, checker, item, data.Name, file, position, contextToken, docFormat)
	}
	if data.AutoImport != nil {
		if data.IsImportStatementCompletion {
			return item
		}
		edits, description, _ := (&autoimport.Fix{AutoImportFix: data.AutoImport}).Edits(ctx, file, program.Options(), l.FormatOptions(), l.converters, l.UserPreferences())
		item.AdditionalTextEdits = &edits
		item.Detail = strPtrTo(description)
		return item
	}
	symbolCompletion := l.getSymbolCompletionFromItemData(ctx, checker, file, position, data)
	preferences := l.UserPreferences()
	switch {
	case symbolCompletion.request != nil:
		request := *symbolCompletion.request
		switch request := request.(type) {
		case *completionDataJSDocTagName:
			return createSimpleDetails(item, data.Name, docFormat)
		case *completionDataJSDocTag:
			return createSimpleDetails(item, data.Name, docFormat)
		case *completionDataJSDocParameterName:
			return createSimpleDetails(item, data.Name, docFormat)
		case *completionDataKeyword:
			if core.Some(request.keywordCompletions, func(c *CompletionItem) bool {
				return c.Label == data.Name
			}) {
				return createSimpleDetails(item, data.Name, docFormat)
			}
			return item
		default:
			panic(fmt.Sprintf("Unexpected completion data type: %T", request))
		}
	case symbolCompletion.symbol != nil:
		symbolDetails := symbolCompletion.symbol
		return l.createCompletionDetailsForSymbol(item, symbolDetails.symbol, checker, symbolDetails.location, position, docFormat)
	case symbolCompletion.literal != nil:
		literal := symbolCompletion.literal
		return createSimpleDetails(item, completionNameForLiteral(file, preferences, *literal), docFormat)
	case symbolCompletion.cases != nil:
		return item
	default:
		if core.Some(allKeywordCompletions(), func(c *lsproto.CompletionItem) bool {
			return c.Label == data.Name
		}) {
			return createSimpleDetails(item, data.Name, docFormat)
		}
		return item
	}
}

type detailsData struct {
	symbol  *symbolDetails
	request *completionData
	literal *literalValue
	cases   *struct{}
}
type symbolDetails struct {
	symbol             *ast.Symbol
	location           ast.Handle
	origin             *symbolOriginInfo
	previousToken      ast.Handle
	contextToken       ast.Handle
	jsxInitializer     jsxInitializer
	isTypeOnlyLocation bool
}

func (l *LanguageService) getSymbolCompletionFromItemData(ctx context.Context, ch *checker.Checker, file *ast.SourceFile, position int, itemData *lsproto.CompletionItemData) detailsData {
	if itemData.Source == SourceSwitchCases {
		return detailsData{cases: &struct{}{}}
	}
	completionData, err := l.getCompletionData(ctx, ch, file, position, l.UserPreferences(), true)
	if err != nil {
		panic(err)
	}
	if completionData == nil {
		return detailsData{}
	}
	if _, ok := completionData.(*completionDataData); !ok {
		return detailsData{request: &completionData}
	}
	data := completionData.(*completionDataData)
	preferences := l.UserPreferences()
	var literal literalValue
	for _, l := range data.literals {
		if completionNameForLiteral(file, preferences, l) == itemData.Name {
			literal = l
			break
		}
	}
	if literal != nil {
		return detailsData{literal: &literal}
	}
	for index, symbol := range data.symbols {
		origin := data.symbolToOriginInfoMap[index]
		displayName, _ := getCompletionEntryDisplayNameForSymbol(symbol, origin, data.completionKind, data.isJsxIdentifierExpected)
		if displayName == itemData.Name && (itemData.Source == string(completionSourceClassMemberSnippet) && symbol.Flags&ast.SymbolFlagsClassMember != 0 || itemData.Source == string(completionSourceObjectLiteralMethodSnippet) && symbol.Flags&(ast.SymbolFlagsProperty|ast.SymbolFlagsMethod) != 0 || getSourceFromOrigin(origin) == itemData.Source || itemData.Source == string(completionSourceObjectLiteralMemberWithComma)) {
			return detailsData{symbol: &symbolDetails{symbol: symbol, location: data.location, origin: origin, previousToken: data.previousToken, contextToken: data.contextToken, jsxInitializer: data.jsxInitializer, isTypeOnlyLocation: data.isTypeOnlyLocation}}
		}
	}
	return detailsData{}
}
func createSimpleDetails(item *lsproto.CompletionItem, name string, docFormat lsproto.MarkupKind) *lsproto.CompletionItem {
	return createCompletionDetails(item, name, "", docFormat)
}
func createCompletionDetails(item *lsproto.CompletionItem, detail string, documentation string, docFormat lsproto.MarkupKind) *lsproto.CompletionItem {
	if item.Detail == nil && detail != "" {
		item.Detail = &detail
	}
	if documentation != "" {
		item.Documentation = &lsproto.StringOrMarkupContent{MarkupContent: &lsproto.MarkupContent{Kind: docFormat, Value: documentation}}
	}
	return item
}

type codeAction struct {
	description string
	changes     []*lsproto.TextEdit
}

func (l *LanguageService) createCompletionDetailsForSymbol(item *lsproto.CompletionItem, symbol *ast.Symbol, checker *checker.Checker, location ast.Handle, position int, docFormat lsproto.MarkupKind) *lsproto.CompletionItem {
	quickInfo, documentation, _, _ := l.getQuickInfoAndDocumentationForSymbol(checker, symbol, location, docFormat, nil, false)
	return createCompletionDetails(item, quickInfo, documentation, docFormat)
}
func (l *LanguageService) getImportStatementCompletionInfo(contextToken ast.Handle, sourceFile *ast.SourceFile) importStatementCompletionInfo {
	result := importStatementCompletionInfo{}
	var candidate ast.Handle
	parent := contextToken.Parent()
	switch {
	case ast.IsImportEqualsDeclaration(parent):
		lastToken := lsutil.GetLastToken(parent, sourceFile)
		if contextToken.Kind == ast.KindIdentifier && lastToken != contextToken {
			result.keywordCompletion = ast.KindFromKeyword
			result.isKeywordOnlyCompletion = true
		} else {
			if contextToken.Kind != ast.KindTypeKeyword {
				result.keywordCompletion = ast.KindTypeKeyword
			}
			if isModuleSpecifierMissingOrEmpty(parent.ImportEqualsDeclarationModuleReference()) {
				candidate = parent
			}
		}
	case couldBeTypeOnlyImportSpecifier(parent, contextToken) && canCompleteFromNamedBindings(parent.Parent()):
		candidate = parent
	case ast.IsNamedImports(parent) || ast.IsNamespaceImport(parent):
		if !parent.Parent().IsTypeOnly() && (contextToken.Kind == ast.KindOpenBraceToken || contextToken.Kind == ast.KindImportKeyword || contextToken.Kind == ast.KindCommaToken) {
			result.keywordCompletion = ast.KindTypeKeyword
		}
		if canCompleteFromNamedBindings(parent) {
			if contextToken.Kind == ast.KindCloseBraceToken || contextToken.Kind == ast.KindIdentifier {
				result.isKeywordOnlyCompletion = true
				result.keywordCompletion = ast.KindFromKeyword
			} else {
				candidate = parent.Parent().Parent()
			}
		}
	case ast.IsExportDeclaration(parent) && contextToken.Kind == ast.KindAsteriskToken, ast.IsNamedExports(parent) && contextToken.Kind == ast.KindCloseBraceToken:
		result.isKeywordOnlyCompletion = true
		result.keywordCompletion = ast.KindFromKeyword
	case contextToken.Kind == ast.KindImportKeyword:
		if ast.IsSourceFile(parent) {
			result.keywordCompletion = ast.KindTypeKeyword
			candidate = contextToken
		} else if ast.IsImportDeclaration(parent) {
			result.keywordCompletion = ast.KindTypeKeyword
			if isModuleSpecifierMissingOrEmpty(parent.ModuleSpecifier()) {
				candidate = parent
			}
		}
	}
	if !candidate.IsNil() {
		result.isNewIdentifierLocation = true
		result.replacementSpan = l.getSingleLineReplacementSpanForImportCompletionNode(candidate)
		result.couldBeTypeOnlyImportSpecifier = couldBeTypeOnlyImportSpecifier(candidate, contextToken)
		if ast.IsImportDeclaration(candidate) {
			if importClause := candidate.ImportClause(); !importClause.IsNil() {
				result.isTopLevelTypeOnly = importClause.IsTypeOnly()
			}
		} else if candidate.Kind == ast.KindImportEqualsDeclaration {
			result.isTopLevelTypeOnly = candidate.IsTypeOnly()
		}
	} else {
		result.isNewIdentifierLocation = result.keywordCompletion == ast.KindTypeKeyword
	}
	return result
}
func (l *LanguageService) getSingleLineReplacementSpanForImportCompletionNode(node ast.Handle) *lsproto.Range {
	if ancestor := ast.FindAncestor(node, core.Or(ast.IsImportDeclaration, ast.IsImportEqualsDeclaration, ast.IsJSDocImportTag)); !ancestor.IsNil() {
		node = ancestor
	}
	sourceFile := ast.GetSourceFileOfNode(node)
	tokenPos := scanner.GetTokenPosOfNode(node, sourceFile, false)
	if printer.GetLinesBetweenPositions(sourceFile, tokenPos, node.End()) == 0 {
		lspRange, fidelity := l.createLspRangeFromNode(node, sourceFile)
		if !fidelity.IsExact() {
			return nil
		}
		return &lspRange
	}
	if node.Kind == ast.KindImportKeyword || node.Kind == ast.KindImportSpecifier {
		panic("ImportKeyword was necessarily on one line; ImportSpecifier was necessarily parented in an ImportDeclaration")
	}
	var potentialSplitPoint ast.Handle
	if node.Kind == ast.KindImportDeclaration || node.Kind == ast.KindJSDocImportTag {
		var specifier ast.Handle
		if importClause := node.ImportClause(); !importClause.IsNil() {
			specifier = getPotentiallyInvalidImportSpecifier(importClause.ImportClauseNamedBindings())
		}
		if !specifier.IsNil() {
			potentialSplitPoint = specifier
		} else {
			potentialSplitPoint = node.ModuleSpecifier()
		}
	} else {
		potentialSplitPoint = node.ImportEqualsDeclarationModuleReference()
	}
	withoutModuleSpecifier := core.NewTextRange(scanner.GetTokenPosOfNode(lsutil.GetFirstToken(node, sourceFile), sourceFile, false), potentialSplitPoint.Pos())
	if printer.GetLinesBetweenPositions(sourceFile, withoutModuleSpecifier.Pos(), withoutModuleSpecifier.End()) == 0 {
		lspRange, fidelity := l.createLspRangeFromBounds(withoutModuleSpecifier.Pos(), withoutModuleSpecifier.End(), sourceFile)
		if !fidelity.IsExact() {
			return nil
		}
		return &lspRange
	}
	return nil
}
func couldBeTypeOnlyImportSpecifier(importSpecifier ast.Handle, contextToken ast.Handle) bool {
	return ast.IsImportSpecifier(importSpecifier) && (importSpecifier.IsTypeOnly() || contextToken == importSpecifier.Name() && isTypeKeywordTokenOrIdentifier(contextToken))
}
func canCompleteFromNamedBindings(namedBindings ast.Handle) bool {
	if !isModuleSpecifierMissingOrEmpty(namedBindings.Parent().Parent().ModuleSpecifier()) || !namedBindings.Parent().Name().IsNil() {
		return false
	}
	if ast.IsNamedImports(namedBindings) {
		invalidNamedImport := getPotentiallyInvalidImportSpecifier(namedBindings)
		elements := namedBindings.Elements()
		validImports := len(elements)
		if !invalidNamedImport.IsNil() {
			validImports = slices.Index(elements, invalidNamedImport)
		}
		return validImports < 2 && validImports > -1
	}
	return true
}

func getPotentiallyInvalidImportSpecifier(namedBindings ast.Handle) ast.Handle {
	if namedBindings.IsNil() || namedBindings.Kind != ast.KindNamedImports {
		return ast.Handle{}
	}
	return core.Find(namedBindings.Elements(), func(e ast.Handle) bool {
		return e.PropertyName().IsNil() && lsutil.IsNonContextualKeyword(scanner.StringToToken(e.Name().Text())) && astnav.FindPrecedingToken(ast.GetSourceFileOfNode(namedBindings), e.Name().Pos()).Kind != ast.KindCommaToken
	})
}
func isModuleSpecifierMissingOrEmpty(specifier ast.Handle) bool {
	if ast.NodeIsMissing(specifier) {
		return true
	}
	node := specifier
	if ast.IsExternalModuleReference(node) {
		node = node.Expression()
	}
	if !ast.IsStringLiteralLike(node) {
		return true
	}
	return node.Text() == ""
}
func hasDocComment(file *ast.SourceFile, position int) bool {
	token := astnav.GetTokenAtPosition(file, position)
	return !ast.FindAncestor(token, ast.IsJSDoc).IsNil()
}

func getJSDocTagAtPosition(node ast.Handle, position int) ast.Handle {
	return ast.FindAncestorOrQuit(node, func(n ast.Handle) ast.FindAncestorResult {
		if ast.IsJSDocTag(n) && n.Loc().ContainsInclusive(position) {
			return ast.FindAncestorTrue
		}
		if ast.IsJSDoc(n) {
			return ast.FindAncestorQuit
		}
		return ast.FindAncestorFalse
	})
}
func tryGetTypeExpressionFromTag(tag ast.Handle) ast.Handle {
	if isTagWithTypeExpression(tag) {
		var typeExpression ast.Handle
		if ast.IsJSDocTemplateTag(tag) {
			typeExpression = tag.JSDocTemplateTagConstraint()
		} else {
			typeExpression = tag.TypeExpression()
		}
		if !typeExpression.IsNil() && typeExpression.Kind == ast.KindJSDocTypeExpression {
			return typeExpression
		}
	}
	if ast.IsJSDocAugmentsTag(tag) || ast.IsJSDocImplementsTag(tag) {
		return tag.ClassName()
	}
	return ast.Handle{}
}
func isTagWithTypeExpression(tag ast.Handle) bool {
	switch tag.Kind {
	case ast.KindJSDocParameterTag, ast.KindJSDocPropertyTag, ast.KindJSDocReturnTag, ast.KindJSDocTypeTag, ast.KindJSDocTypedefTag, ast.KindJSDocThrowsTag, ast.KindJSDocSatisfiesTag:
		return true
	case ast.KindJSDocTemplateTag:
		return !tag.JSDocTemplateTagConstraint().IsNil()
	default:
		return false
	}
}
func (l *LanguageService) jsDocCompletionInfo(ctx context.Context, position int, file *ast.SourceFile, items []*CompletionItem) *CompletionList {
	defaultCommitCharacters := getDefaultCommitCharacters(false)
	itemDefaults := l.setItemDefaults(ctx, position, file, items, &defaultCommitCharacters, nil)
	return &CompletionList{IsIncomplete: false, ItemDefaults: itemDefaults, Items: items}
}

var jsDocTagNames = []string{"abstract", "access", "alias", "argument", "async", "augments", "author", "borrows", "callback", "class", "classdesc", "constant", "constructor", "constructs", "copyright", "default", "deprecated", "description", "emits", "enum", "event", "example", "exports", "extends", "external", "field", "file", "fileoverview", "fires", "function", "generator", "global", "hideconstructor", "host", "ignore", "implements", "import", "inheritdoc", "inner", "instance", "interface", "kind", "lends", "license", "link", "linkcode", "linkplain", "listens", "member", "memberof", "method", "mixes", "module", "name", "namespace", "overload", "override", "package", "param", "private", "prop", "property", "protected", "public", "readonly", "requires", "returns", "satisfies", "see", "since", "static", "summary", "template", "this", "throws", "todo", "tutorial", "type", "typedef", "var", "variation", "version", "virtual", "yields"}
var jsDocTagNameCompletionItems = sync.OnceValue(func() []*lsproto.CompletionItem {
	items := make([]*lsproto.CompletionItem, 0, len(jsDocTagNames))
	for _, tagName := range jsDocTagNames {
		item := &lsproto.CompletionItem{Label: tagName, Kind: new(lsproto.CompletionItemKindKeyword), SortText: new(string(SortTextLocationPriority))}
		items = append(items, item)
	}
	return items
})
var jsDocTagCompletionItems = sync.OnceValue(func() []*lsproto.CompletionItem {
	items := make([]*lsproto.CompletionItem, 0, len(jsDocTagNames))
	for _, tagName := range jsDocTagNames {
		item := &lsproto.CompletionItem{Label: "@" + tagName, Kind: new(lsproto.CompletionItemKindKeyword), SortText: new(string(SortTextLocationPriority))}
		items = append(items, item)
	}
	return items
})

func getJSDocTagNameCompletions() []*CompletionItem {
	return cloneItems(jsDocTagNameCompletionItems())
}
func getJSDocTagCompletions() []*CompletionItem {
	return cloneItems(jsDocTagCompletionItems())
}
func getJSDocParameterCompletions(ctx context.Context, file *ast.SourceFile, position int, typeChecker *checker.Checker, options *core.CompilerOptions, preferences lsutil.UserPreferences, tagNameOnly bool) []*CompletionItem {
	currentToken := astnav.GetTokenAtPosition(file, position)
	if !ast.IsJSDocTag(currentToken) && !ast.IsJSDoc(currentToken) {
		return nil
	}
	var jsDoc ast.Handle
	if ast.IsJSDoc(currentToken) {
		jsDoc = currentToken
	} else {
		jsDoc = currentToken.Parent()
	}
	if !ast.IsJSDoc(jsDoc) {
		return nil
	}
	fun := jsDoc.Parent()
	if !ast.IsFunctionLike(fun) {
		return nil
	}
	isJS := ast.IsSourceFileJS(file)
	isSnippet := false
	paramTagCount := 0
	var tags []ast.Handle
	if jsDoc.JSDocTags() != 0 {
		tags = jsDoc.Store().ListSlice(jsDoc.JSDocTags())
	}
	for _, tag := range tags {
		if ast.IsJSDocParameterTag(tag) && astnav.GetStartOfNode(tag, file, false) < position && ast.IsIdentifier(tag.Name()) {
			paramTagCount++
		}
	}
	paramIndex := -1
	return core.MapNonNil(fun.Parameters(), func(param ast.Handle) *CompletionItem {
		paramIndex++
		if paramIndex < paramTagCount {
			return nil
		}
		if ast.IsIdentifier(param.Name()) {
			tabstopCounter := 1
			paramName := param.Name().Text()
			displayText := getJSDocParamAnnotation(paramName, param.Initializer(), param.ParameterDeclarationDotDotDotToken(), isJS, false, false, typeChecker, options, preferences, &tabstopCounter)
			var snippetText string
			if isSnippet {
				snippetText = getJSDocParamAnnotation(paramName, param.Initializer(), param.ParameterDeclarationDotDotDotToken(), isJS, false, true, typeChecker, options, preferences, &tabstopCounter)
			}
			if tagNameOnly {
				displayText = displayText[1:]
				if snippetText != "" {
					snippetText = snippetText[1:]
				}
			}
			return &CompletionItem{CompletionItem: &lsproto.CompletionItem{Label: displayText, Kind: new(lsproto.CompletionItemKindVariable), SortText: new(string(SortTextLocationPriority)), InsertText: strPtrTo(snippetText), InsertTextFormat: core.IfElse(isSnippet, new(lsproto.InsertTextFormatSnippet), nil)}}
		} else if paramIndex == paramTagCount {
			paramPath := fmt.Sprintf("param%d", paramIndex)
			displayTextResult := generateJSDocParamTagsForDestructuring(paramPath, param.Name(), param.Initializer(), param.ParameterDeclarationDotDotDotToken(), isJS, false, typeChecker, options, preferences)
			var snippetText string
			if isSnippet {
				snippetTextResult := generateJSDocParamTagsForDestructuring(paramPath, param.Name(), param.Initializer(), param.ParameterDeclarationDotDotDotToken(), isJS, true, typeChecker, options, preferences)
				snippetText = strings.Join(snippetTextResult, options.NewLine.GetNewLineCharacter()+"* ")
			}
			displayText := strings.Join(displayTextResult, options.NewLine.GetNewLineCharacter()+"* ")
			if tagNameOnly {
				displayText = strings.TrimPrefix(displayText, "@")
				snippetText = strings.TrimPrefix(snippetText, "@")
			}
			return &CompletionItem{CompletionItem: &lsproto.CompletionItem{Label: displayText, Kind: new(lsproto.CompletionItemKindVariable), SortText: new(string(SortTextLocationPriority)), InsertText: strPtrTo(snippetText), InsertTextFormat: core.IfElse(isSnippet, new(lsproto.InsertTextFormatSnippet), nil)}}
		}
		return nil
	})
}
func getJSDocParamAnnotation(paramName string, initializer ast.Handle, dotDotDotToken ast.Handle, isJS bool, isObject bool, isSnippet bool, typeChecker *checker.Checker, options *core.CompilerOptions, preferences lsutil.UserPreferences, tabstopCounter *int) string {
	if isSnippet {
		debug.Assert(tabstopCounter != nil)
	}
	if !initializer.IsNil() {
		paramName = getJSDocParamNameWithInitializer(paramName, initializer)
	}
	if isSnippet {
		paramName = escapeSnippetText(paramName)
	}
	if isJS {
		t := "*"
		if isObject {
			debug.Assert(dotDotDotToken.IsNil(), `Cannot annotate a rest parameter with type 'object'.`)
			t = "object"
		} else {
			if !initializer.IsNil() {
				inferredType := typeChecker.GetTypeAtLocation(initializer.Parent())
				if inferredType.Flags()&(checker.TypeFlagsAny|checker.TypeFlagsVoid) == 0 {
					file := ast.GetSourceFileOfNode(initializer)
					quotePreference := lsutil.GetQuotePreference(file, preferences)
					builderFlags := core.IfElse(quotePreference == lsutil.QuotePreferenceSingle, nodebuilder.FlagsUseSingleQuotesForStringLiteralType, nodebuilder.FlagsNone)
					typeNode := typeChecker.TypeToTypeNode(inferredType, ast.FindAncestor(initializer, ast.IsFunctionLike), builderFlags, nil)
					if !typeNode.IsNil() {
						emitContext := printer.NewEmitContext()
						p := printer.NewPrinter(printer.PrinterOptions{RemoveComments: true}, printer.PrintHandlers{}, emitContext)
						emitContext.SetEmitFlags(typeNode, printer.EFSingleLine)
						t = p.Emit(typeNode, file)
					}
				}
			}
			if isSnippet && t == "*" {
				tabstop := *tabstopCounter
				*tabstopCounter++
				t = fmt.Sprintf("${%d:%s}", tabstop, t)
			}
		}
		dotDotDot := core.IfElse(!isObject && !dotDotDotToken.IsNil(), "...", "")
		var description string
		if isSnippet {
			tabstop := *tabstopCounter
			*tabstopCounter++
			description = fmt.Sprintf("${%d}", tabstop)
		}
		return fmt.Sprintf("@param {%s%s} %s %s", dotDotDot, t, paramName, description)
	} else {
		var description string
		if isSnippet {
			tabstop := *tabstopCounter
			*tabstopCounter++
			description = fmt.Sprintf("${%d}", tabstop)
		}
		return fmt.Sprintf("@param %s %s", paramName, description)
	}
}
func getJSDocParamNameWithInitializer(paramName string, initializer ast.Handle) string {
	initializerText := strings.TrimSpace(scanner.GetTextOfNode(initializer))
	if strings.Contains(initializerText, "\n") || len(initializerText) > 80 {
		return fmt.Sprintf("[%s]", paramName)
	}
	return fmt.Sprintf("[%s=%s]", paramName, initializerText)
}
func generateJSDocParamTagsForDestructuring(path string, pattern ast.Handle, initializer ast.Handle, dotDotDotToken ast.Handle, isJS bool, isSnippet bool, typeChecker *checker.Checker, options *core.CompilerOptions, preferences lsutil.UserPreferences) []string {
	tabstopCounter := 1
	if !isJS {
		return []string{getJSDocParamAnnotation(path, initializer, dotDotDotToken, isJS, false, isSnippet, typeChecker, options, preferences, &tabstopCounter)}
	}
	return jsDocParamPatternWorker(path, pattern, initializer, dotDotDotToken, isJS, isSnippet, typeChecker, options, preferences, &tabstopCounter)
}
func jsDocParamPatternWorker(path string, pattern ast.Handle, initializer ast.Handle, dotDotDotToken ast.Handle, isJS bool, isSnippet bool, typeChecker *checker.Checker, options *core.CompilerOptions, preferences lsutil.UserPreferences, counter *int) []string {
	if ast.IsObjectBindingPattern(pattern) && dotDotDotToken.IsNil() {
		childCounter := *counter
		rootParam := getJSDocParamAnnotation(path, initializer, dotDotDotToken, isJS, true, isSnippet, typeChecker, options, preferences, &childCounter)
		var childTags []string
		for _, element := range pattern.Elements() {
			elementTags := jsDocParamElementWorker(path, element, initializer, dotDotDotToken, isJS, isSnippet, typeChecker, options, preferences, &childCounter)
			if len(elementTags) == 0 {
				childTags = nil
				break
			}
			childTags = append(childTags, elementTags...)
		}
		if len(childTags) > 0 {
			*counter = childCounter
			return append([]string{rootParam}, childTags...)
		}
	}
	return []string{getJSDocParamAnnotation(path, initializer, dotDotDotToken, isJS, false, isSnippet, typeChecker, options, preferences, counter)}
}

func jsDocParamElementWorker(path string, element ast.Handle, initializer ast.Handle, dotDotDotToken ast.Handle, isJS bool, isSnippet bool, typeChecker *checker.Checker, options *core.CompilerOptions, preferences lsutil.UserPreferences, counter *int) []string {
	if ast.IsIdentifier(element.Name()) {
		var propertyName string
		if !element.PropertyName().IsNil() {
			propertyName, _ = ast.TryGetTextOfPropertyName(element.PropertyName())
		} else {
			propertyName = element.Name().Text()
		}
		if propertyName == "" {
			return nil
		}
		paramName := fmt.Sprintf("%s.%s", path, propertyName)
		return []string{getJSDocParamAnnotation(paramName, element.Initializer(), element.BindingElementDotDotDotToken(), isJS, false, isSnippet, typeChecker, options, preferences, counter)}
	} else if !element.PropertyName().IsNil() {
		propertyName, _ := ast.TryGetTextOfPropertyName(element.PropertyName())
		if propertyName == "" {
			return nil
		}
		return jsDocParamPatternWorker(fmt.Sprintf("%s.%s", path, propertyName), element.Name(), element.Initializer(), element.BindingElementDotDotDotToken(), isJS, isSnippet, typeChecker, options, preferences, counter)
	}
	return nil
}
func getJSDocParameterNameCompletions(tag ast.Handle) []*CompletionItem {
	if !ast.IsIdentifier(tag.Name()) {
		return nil
	}
	nameThusFar := tag.Name().Text()
	jsDoc := tag.Parent()
	fn := jsDoc.Parent()
	if !ast.IsFunctionLike(fn) {
		return nil
	}
	var tags []ast.Handle
	if jsDoc.JSDocTags() != 0 {
		tags = jsDoc.Store().ListSlice(jsDoc.JSDocTags())
	}
	return core.MapNonNil(fn.Parameters(), func(param ast.Handle) *CompletionItem {
		if !ast.IsIdentifier(param.Name()) {
			return nil
		}
		name := param.Name().Text()
		if core.Some(tags, func(t ast.Handle) bool {
			return t != tag && ast.IsJSDocParameterTag(t) && ast.IsIdentifier(t.Name()) && t.Name().Text() == name
		}) || nameThusFar != "" && !strings.HasPrefix(name, nameThusFar) {
			return nil
		}
		return &CompletionItem{CompletionItem: &lsproto.CompletionItem{Label: name, Kind: new(lsproto.CompletionItemKindVariable), SortText: new(string(SortTextLocationPriority))}}
	})
}
func (l *LanguageService) getExhaustiveCaseSnippets(ctx context.Context, caseBlock ast.Handle, file *ast.SourceFile, position int, options *core.CompilerOptions, program *compiler.Program, c *checker.Checker) (*lsproto.CompletionItem, error) {
	clauses := caseBlock.Clauses()
	switchType := c.GetTypeAtLocation(caseBlock.Parent().Expression())
	if switchType != nil && switchType.IsUnion() && core.Every(switchType.Types(), isLiteral) {
		tracker := newCaseClauseTracker(c, clauses)
		target := options.GetEmitScriptTarget()
		quotePreference := lsutil.GetQuotePreference(file, l.UserPreferences())
		var importAdder autoimport.ImportAdder
		if !tspath.IsDynamicFileName(file.FileName()) {
			view, err := l.getPreparedAutoImportView(file)
			if err != nil {
				return nil, err
			}
			if view != nil {
				importAdder = autoimport.NewImportAdder(ctx, program, c, file, view, l.FormatOptions(), l.converters, l.UserPreferences())
			}
		}
		var elements []ast.Handle
		factory := ast.NewFactory(ast.FactoryHooks{})
		for _, t := range switchType.Types() {
			if t.IsEnumLiteral() {
				debug.Assert(t.Symbol() != nil, "An enum member type should have a symbol")
				debug.Assert(t.Symbol().Parent != nil, "An enum member type should have a parent symbol (the enum symbol)")
				var enumValue any
				if t.Symbol().ValueDeclaration != 0 {
					enumValue = c.GetConstantValue(ast.NodeOf(t.Symbol().ValueDeclaration))
				}
				if enumValue != nil {
					if tracker.hasValue(enumValue) {
						continue
					}
					tracker.addValue(enumValue)
				}
				typeNode := autoimport.TypeToAutoImportableTypeNode(c, importAdder, t, caseBlock)
				if typeNode.IsNil() {
					return nil, nil
				}
				expr := typeNodeToExpression(typeNode, target, quotePreference, factory)
				if expr.IsNil() {
					return nil, nil
				}
				elements = append(elements, expr)
			} else if value := t.AsLiteralType().Value(); !tracker.hasValue(value) {
				switch v := value.(type) {
				case jsnum.PseudoBigInt:
					var bigInt ast.Handle
					if v.Negative {
						v.Negative = false
						bigInt = factory.NewPrefixUnaryExpression(ast.KindMinusToken, factory.NewBigIntLiteral(v.String()+"n", ast.TokenFlagsNone))
					} else {
						bigInt = factory.NewBigIntLiteral(v.String()+"n", ast.TokenFlagsNone)
					}
					elements = append(elements, bigInt)
				case jsnum.Number:
					var number ast.Handle
					if v < 0 {
						number = factory.NewPrefixUnaryExpression(ast.KindMinusToken, factory.NewNumericLiteral(v.Abs().String(), ast.TokenFlagsNone))
					} else {
						number = factory.NewNumericLiteral(v.String(), ast.TokenFlagsNone)
					}
					elements = append(elements, number)
				case string:
					literal := factory.NewStringLiteral(v, core.IfElse(quotePreference == lsutil.QuotePreferenceSingle, ast.TokenFlagsSingleQuote, ast.TokenFlagsNone))
					elements = append(elements, literal)
				}
			}
		}
		if len(elements) == 0 {
			return nil, nil
		}
		newClauses := core.Map(elements, func(element ast.Handle) ast.Handle {
			return factory.NewCaseOrDefaultClause(ast.KindCaseClause, element, factory.NewList(nil))
		})
		newLineChar := l.FormatOptions().NewLineCharacter
		printer := createSnippetPrinter(printer.PrinterOptions{RemoveComments: true, NewLine: core.GetNewLineKind(newLineChar)}, nil)
		printNode := func(node ast.Handle) string {
			return printer.printAndFormatNode(ctx, node, file)
		}
		insertText := strings.Join(core.MapIndex(newClauses, func(clause ast.Handle, i int) string {
			if clientSupportsItemSnippet(ctx) {
				return fmt.Sprintf("%s$%d", printNode(clause), i+1)
			}
			return printer.printUnescapedNode(clause)
		}), newLineChar)
		firstClause := printer.printUnescapedNode(newClauses[0])
		name := firstClause + " ..."
		var additionalTextEdits *[]*lsproto.TextEdit
		if importAdder != nil {
			if edits := importAdder.Edits(); len(edits) != 0 {
				additionalTextEdits = &edits
			}
		}
		return &lsproto.CompletionItem{Label: name, Kind: new(lsproto.CompletionItemKindSnippet), SortText: new(string(SortTextGlobalsOrKeywords)), InsertText: strPtrTo(insertText), AdditionalTextEdits: additionalTextEdits, InsertTextFormat: core.IfElse(clientSupportsItemSnippet(ctx), new(lsproto.InsertTextFormatSnippet), nil), Data: &lsproto.CompletionItemData{FileName: file.OriginalFileName(), Position: int32(position), SupplementalFileIndex: supplementalFileIndex(file), Name: name, Source: string(completionSourceSwitchCases)}}, nil
	}
	return nil, nil
}
func typeNodeToExpression(typeNode ast.Handle, target core.ScriptTarget, quotePreference lsutil.QuotePreference, factory ast.HandleFactory) ast.Handle {
	switch typeNode.Kind {
	case ast.KindTypeReference:
		typeName := typeNode.TypeReferenceNodeTypeName()
		return entityNameToExpression(typeName, target, quotePreference, factory)
	case ast.KindIndexedAccessType:
		objectExpression := typeNodeToExpression(typeNode.IndexedAccessTypeNodeObjectType(), target, quotePreference, factory)
		indexExpression := typeNodeToExpression(typeNode.IndexedAccessTypeNodeIndexType(), target, quotePreference, factory)
		if !objectExpression.IsNil() && !indexExpression.IsNil() {
			return factory.NewElementAccessExpression(objectExpression, ast.Handle{}, indexExpression, ast.NodeFlagsNone)
		}
		return ast.Handle{}
	case ast.KindLiteralType:
		literal := typeNode.LiteralTypeNodeLiteral()
		switch literal.Kind {
		case ast.KindStringLiteral:
			expr := factory.NewStringLiteral(literal.Text(), core.IfElse(quotePreference == lsutil.QuotePreferenceSingle, ast.TokenFlagsSingleQuote, ast.TokenFlagsNone))
			return expr
		case ast.KindNumericLiteral:
			expr := factory.NewNumericLiteral(literal.Text(), literal.NumericLiteralTokenFlags())
			return expr
		default:
			return ast.Handle{}
		}
	case ast.KindParenthesizedType:
		expr := typeNodeToExpression(typeNode.ParenthesizedTypeNodeType(), target, quotePreference, factory)
		if expr.IsNil() {
			return ast.Handle{}
		}
		if ast.IsIdentifier(expr) {
			return expr
		}
		return factory.NewParenthesizedExpression(expr)
	case ast.KindTypeQuery:
		return entityNameToExpression(typeNode.TypeQueryNodeExprName(), target, quotePreference, factory)
	case ast.KindImportType:
		debug.Fail(`We should not get an import type after calling 'typeToAutoImportableTypeNode'.`)
		return ast.Handle{}
	}
	return ast.Handle{}
}
func entityNameToExpression(entityName ast.Handle, target core.ScriptTarget, quotePreference lsutil.QuotePreference, factory ast.HandleFactory) ast.Handle {
	if ast.IsIdentifier(entityName) {
		return entityName
	}
	return factory.NewPropertyAccessExpression(entityNameToExpression(entityName.QualifiedNameLeft(), target, quotePreference, factory), ast.Handle{}, entityName.QualifiedNameRight(), ast.NodeFlagsNone)
}

type snippetPrinter struct {
	baseWriter  *printer.ChangeTrackerWriter
	emitContext *printer.EmitContext
	printer     *printer.Printer
	writer      *snippetEmitTextWriter
	factory     ast.HandleFactory
}

func (p *snippetPrinter) printNode(node ast.Handle) string {
	unescaped := p.printUnescapedNode(node)
	if len(p.writer.escapes) > 0 {
		return core.ApplyBulkEdits(unescaped, p.writer.escapes)
	}
	return unescaped
}
func (p *snippetPrinter) printUnescapedNode(node ast.Handle) string {
	p.writer.escapes = nil
	p.writer.Clear()
	p.printer.Write(node, nil, p.writer, nil)
	return p.writer.String()
}
func (p *snippetPrinter) printAndFormatNode(ctx context.Context, node ast.Handle, sourceFile *ast.SourceFile) string {
	return p.printAndFormatNodeWithSettings(ctx, node, sourceFile, format.GetFormatCodeSettingsFromContext(ctx))
}
func (p *snippetPrinter) printAndFormatNodeWithSettings(ctx context.Context, node ast.Handle, sourceFile *ast.SourceFile, formatOptions lsutil.FormatCodeSettings) string {
	text := p.printUnescapedNode(node)
	nodeWithPos := p.baseWriter.AssignPositionsToNode(node, p.factory)
	syntheticFile := printer.CreateSyntheticSourceFile(p.factory, nodeWithPos, text, sourceFile.ParseOptions())
	ctx = format.WithFormatCodeSettings(ctx, formatOptions, formatOptions.NewLineCharacter)
	changes := format.FormatNodeGivenIndentation(ctx, nodeWithPos, syntheticFile, sourceFile.LanguageVariant, 0, 0)
	allChanges := changes
	if len(p.writer.escapes) > 0 {
		allChanges = append(changes, p.writer.escapes...)
		slices.SortFunc(allChanges, func(a, b core.TextChange) int {
			return core.CompareTextRanges(a.TextRange, b.TextRange)
		})
	}
	return core.ApplyBulkEdits(syntheticFile.Text(), allChanges)
}
func createSnippetPrinter(options printer.PrinterOptions, emitContext *printer.EmitContext) *snippetPrinter {
	if emitContext == nil {
		emitContext = printer.NewEmitContext()
	}
	baseWriter := printer.NewChangeTrackerWriter(options.NewLine.GetNewLineCharacter(), -1)
	printer := printer.NewPrinter(options, baseWriter.GetPrintHandlers(), emitContext)
	writer := &snippetEmitTextWriter{ChangeTrackerWriter: baseWriter}
	return &snippetPrinter{baseWriter: baseWriter, emitContext: emitContext, printer: printer, writer: writer, factory: emitContext.StoreFactory()}
}

type snippetEmitTextWriter struct {
	*printer.ChangeTrackerWriter
	escapes []core.TextChange
}

func (w *snippetEmitTextWriter) Write(s string) {
	w.escapingWrite(s, func() {
		w.ChangeTrackerWriter.Write(s)
	})
}
func (w *snippetEmitTextWriter) WriteComment(text string) {
	w.escapingWrite(text, func() {
		w.ChangeTrackerWriter.WriteComment(text)
	})
}
func (w *snippetEmitTextWriter) WriteStringLiteral(text string) {
	w.escapingWrite(text, func() {
		w.ChangeTrackerWriter.WriteStringLiteral(text)
	})
}
func (w *snippetEmitTextWriter) WriteParameter(text string) {
	w.escapingWrite(text, func() {
		w.ChangeTrackerWriter.WriteParameter(text)
	})
}
func (w *snippetEmitTextWriter) WriteProperty(text string) {
	w.escapingWrite(text, func() {
		w.ChangeTrackerWriter.WriteProperty(text)
	})
}
func (w *snippetEmitTextWriter) WriteSymbol(text string, symbol *ast.Symbol) {
	w.escapingWrite(text, func() {
		w.ChangeTrackerWriter.WriteSymbol(text, symbol)
	})
}

func (w *snippetEmitTextWriter) escapingWrite(s string, write func()) {
	escaped := escapeSnippetText(s)
	if escaped != s {
		start := w.GetTextPos()
		write()
		end := w.GetTextPos()
		w.escapes = append(w.escapes, core.TextChange{NewText: escaped, TextRange: core.NewTextRange(start, end)})
	} else {
		write()
	}
}
