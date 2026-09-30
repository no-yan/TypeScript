package storemodule

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The P2 declarations of module/util.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// GetResolutionDiagnostic takes the program's *module.ResolvedModule: its
// diagnostics are the resolver's (R11), so the Store has no ResolvedModule of
// its own to pass.
//
// storetranslate:pointer-hash 2d9ead960436ffa4 util.go:GetResolutionDiagnostic
func GetResolutionDiagnostic(options *core.CompilerOptions, resolvedModule *module.ResolvedModule, file *store.File) *diagnostics.Message {
	needJsx := func() *diagnostics.Message {
		if options.Jsx != core.JsxEmitNone {
			return nil
		}
		return diagnostics.Module_0_was_resolved_to_1_but_jsx_is_not_set
	}

	needAllowJs := func() *diagnostics.Message {
		if options.GetAllowJS() || !options.NoImplicitAny.DefaultIfUnknown(options.Strict).IsTrue() {
			return nil
		}
		return diagnostics.Could_not_find_a_declaration_file_for_module_0_1_implicitly_has_an_any_type
	}

	needResolveJsonModule := func() *diagnostics.Message {
		if options.GetResolveJsonModule() {
			return nil
		}
		return diagnostics.Module_0_was_resolved_to_1_but_resolveJsonModule_is_not_used
	}

	needAllowArbitraryExtensions := func() *diagnostics.Message {
		if file.IsDeclarationFile || options.AllowArbitraryExtensions.IsTrue() {
			return nil
		}
		return diagnostics.Module_0_was_resolved_to_1_but_allowArbitraryExtensions_is_not_set
	}

	if resolvedModule.ResolvedUsingExtraExtensions {
		return nil
	}

	switch resolvedModule.Extension {
	case tspath.ExtensionTs, tspath.ExtensionDts,
		tspath.ExtensionMts, tspath.ExtensionDmts,
		tspath.ExtensionCts, tspath.ExtensionDcts:
		// These are always allowed.
		return nil
	case tspath.ExtensionTsx:
		return needJsx()
	case tspath.ExtensionJsx:
		if message := needJsx(); message != nil {
			return message
		}
		return needAllowJs()
	case tspath.ExtensionJs, tspath.ExtensionMjs, tspath.ExtensionCjs:
		return needAllowJs()
	case tspath.ExtensionJson:
		return needResolveJsonModule()
	default:
		return needAllowArbitraryExtensions()
	}
}
