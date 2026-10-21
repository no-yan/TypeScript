// Package storeclitest compares the native Store program and CLI with the
// Pointer ones built independently from the same input values
// (store-cli-design-20261004.md 8.2). Nothing here is on a production path.
package storeclitest

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// PointerGraph and StoreGraph describe a program's file graph in the same
// lines: the files in program order with their library flags and metadata,
// the synthetic import specifiers, every module and type reference
// resolution, the redirects of package deduplication, the include reasons
// and the missing files. A difference is a line.
func PointerGraph(p *compiler.Program) []string {
	var out []string
	for i, f := range p.SourceFiles() {
		out = append(out, fileLine(i, f.FileName().AsString(), f.PathKey(), p.IsSourceFileDefaultLibrary(f.PathKey()), p.IsSourceFileFromExternalLibrary(f), p.GetSourceFileMetaData(f.PathKey())))
		if s := p.GetImportHelpersImportSpecifier(f.PathKey()); s != nil {
			out = append(out, "  helpers "+syntheticPointer(s, f))
		}
		if ref, s := p.GetJSXRuntimeImportSpecifier(f.PathKey()); s != nil {
			out = append(out, "  jsx "+ref+" "+syntheticPointer(s, f))
		}
		if targets := p.GetRedirectTargets(f.PathKey()); len(targets) > 0 {
			out = append(out, fmt.Sprintf("  redirects %v", targets))
		}
	}
	out = append(out, resolutionLines(p.GetResolvedModules(), p.GetResolvedTypeReferenceDirectives())...)
	return append(out, reasonLines(p.GetIncludeReasons(), func(path tspath.PathKey) bool { return p.GetSourceFileByPath(path) != nil }, p.IsMissingPath)...)
}

func StoreGraph(p *storecompiler.Program) []string {
	var out []string
	for i, f := range p.SourceFiles() {
		out = append(out, fileLine(i, f.FileName().AsString(), f.PathKey(), p.IsSourceFileDefaultLibrary(f.PathKey()), p.IsSourceFileFromExternalLibrary(f), p.GetSourceFileMetaData(f.PathKey())))
		if s := p.GetImportHelpersImportSpecifier(f.PathKey()); !s.IsNil() {
			out = append(out, "  helpers "+syntheticStore(s, f))
		}
		if ref, s := p.GetJSXRuntimeImportSpecifier(f.PathKey()); !s.IsNil() {
			out = append(out, "  jsx "+ref+" "+syntheticStore(s, f))
		}
		if targets := p.GetRedirectTargets(f.PathKey()); len(targets) > 0 {
			out = append(out, fmt.Sprintf("  redirects %v", targets))
		}
	}
	out = append(out, resolutionLines(p.GetResolvedModules(), p.GetResolvedTypeReferenceDirectives())...)
	return append(out, reasonLines(p.GetIncludeReasons(), func(path tspath.PathKey) bool { return p.GetSourceFileByPath(path) != nil }, p.IsMissingPath)...)
}

func fileLine(i int, name string, path tspath.PathKey, lib, external bool, meta ast.SourceFileMetaData) string {
	return fmt.Sprintf("file %d %s path=%s lib=%v external=%v meta=%+v", i, name, path, lib, external, meta)
}

// syntheticPointer and syntheticStore: the specifier's text and shape, which
// the checker does not ask (the parent ImportDeclaration, whose parent is the
// file's root and not one of its statements).
func syntheticPointer(s *ast.Node, f *ast.SourceFile) string {
	decl := s.Parent
	inStatements := slices.Contains(f.Statements.Nodes, decl)
	return fmt.Sprintf("%q %v parent=%v root=%v statement=%v pos=%d,%d", s.Text(), s.Kind, decl.Kind, decl.Parent == f.AsNode(), inStatements, s.Pos(), s.End())
}

func syntheticStore(s store.Node, f *store.File) string {
	decl := s.Parent()
	inStatements := false
	statements := f.Root().Statements()
	for i := range statements.Len() {
		inStatements = inStatements || store.SameNode(statements.At(i), decl)
	}
	return fmt.Sprintf("%q %v parent=%v root=%v statement=%v pos=%d,%d", s.Text(), s.Kind(), decl.Kind(), store.SameNode(decl.Parent(), f.Root()), inStatements, s.Pos(), s.End())
}

func resolutionLines(modules map[tspath.PathKey]module.ModeAwareCache[*module.ResolvedModule], types map[tspath.PathKey]module.ModeAwareCache[*module.ResolvedTypeReferenceDirective]) []string {
	var out []string
	for _, path := range slices.Sorted(maps.Keys(modules)) {
		for _, key := range sortedKeys(modules[path]) {
			r := modules[path][key]
			if r == nil {
				out = append(out, fmt.Sprintf("module %s %q mode=%v -> nil", path, key.Name, key.Mode))
				continue
			}
			out = append(out, fmt.Sprintf("module %s %q mode=%v -> %q ext=%s external=%v pkg=%+v original=%q tsext=%v extra=%v diags=%d",
				path, key.Name, key.Mode, r.ResolvedFileName, r.Extension, r.IsExternalLibraryImport, r.PackageId, r.OriginalPath, r.ResolvedUsingTsExtension, r.ResolvedUsingExtraExtensions, len(r.ResolutionDiagnostics)))
		}
	}
	for _, path := range slices.Sorted(maps.Keys(types)) {
		for _, key := range sortedKeys(types[path]) {
			r := types[path][key]
			if r == nil {
				out = append(out, fmt.Sprintf("type %s %q mode=%v -> nil", path, key.Name, key.Mode))
				continue
			}
			out = append(out, fmt.Sprintf("type %s %q mode=%v -> %q primary=%v external=%v pkg=%+v original=%q diags=%d",
				path, key.Name, key.Mode, r.ResolvedFileName, r.Primary, r.IsExternalLibraryImport, r.PackageId, r.OriginalPath, len(r.ResolutionDiagnostics)))
		}
	}
	return out
}

func sortedKeys[T any](cache module.ModeAwareCache[T]) []module.ModeAwareCacheKey {
	return slices.SortedFunc(maps.Keys(cache), func(a, b module.ModeAwareCacheKey) int {
		if a.Name != b.Name {
			if a.Name < b.Name {
				return -1
			}
			return 1
		}
		return int(a.Mode) - int(b.Mode)
	})
}

// reasonLines are the include reasons of every path, sorted by path, each
// path's in their order, and whether a path without a file is missing.
func reasonLines[R any](reasons map[tspath.PathKey][]R, isFile func(tspath.PathKey) bool, isMissing func(tspath.PathKey) bool) []string {
	var out []string
	for _, path := range slices.Sorted(maps.Keys(reasons)) {
		line := fmt.Sprintf("reasons %s", path)
		if !isFile(path) {
			line += fmt.Sprintf(" nofile missing=%v", isMissing(path))
		}
		out = append(out, line)
		for _, r := range reasons[path] {
			out = append(out, "  "+describeReason(reflect.ValueOf(r).Elem()))
		}
	}
	return out
}

// describeReason prints a FileIncludeReason of either package, field by
// field: the fields are unexported, and the same in both (storecompiler's are
// translated). A synthetic import node is printed by its kind and text.
func describeReason(v reflect.Value) string {
	if v.CanAddr() {
		v = reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
	}
	if v.CanInterface() {
		switch x := v.Interface().(type) {
		case *ast.Node:
			if x == nil {
				return "nil"
			}
			return fmt.Sprintf("node %v %q", x.Kind, x.Text())
		case store.Node:
			if x.IsNil() {
				return "nil"
			}
			return fmt.Sprintf("node %v %q", x.Kind(), x.Text())
		case module.PackageId:
			return fmt.Sprintf("%+v", x)
		}
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return "nil"
		}
		return describeReason(v.Elem())
	case reflect.Struct:
		s := "{"
		for i := range v.NumField() {
			if i > 0 {
				s += " "
			}
			s += v.Type().Field(i).Name + "=" + describeReason(v.Field(i))
		}
		return s + "}"
	}
	return fmt.Sprintf("%v", v.Interface())
}
