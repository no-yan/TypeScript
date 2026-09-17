package workload

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

// repeatedStorage counts only this generator's known representation. It does
// not estimate allocator rounding, GC metadata, or registry ownership.
func repeatedStorage(c Config, t *tree) (ByteEstimate, ByteEstimate) {
	unavailable := func(reason string) ByteEstimate { return ByteEstimate{Method: "unavailable", Reason: reason} }
	if c.Shape != ShapeRepeated {
		return unavailable("storage census currently covers only repeated synthetic trees"), unavailable("storage census currently covers only repeated synthetic trees")
	}
	if t.pointer != nil {
		var bytes uint64
		var walk func(*ast.Node)
		walk = func(n *ast.Node) {
			switch n.Kind {
			case ast.KindNumericLiteral:
				bytes += uint64(unsafe.Sizeof(ast.NumericLiteral{}))
				// Each repeated leaf gets its own FormatUint allocation.
				bytes += uint64(len(n.Text()))
			case ast.KindPlusToken:
				bytes += uint64(unsafe.Sizeof(ast.Token{}))
			case ast.KindParenthesizedExpression:
				bytes += uint64(unsafe.Sizeof(ast.ParenthesizedExpression{}))
			case ast.KindBinaryExpression:
				bytes += uint64(unsafe.Sizeof(ast.BinaryExpression{}))
			case ast.KindArrayLiteralExpression:
				bytes += uint64(unsafe.Sizeof(ast.ArrayLiteralExpression{})) + uint64(unsafe.Sizeof(ast.NodeList{})) + uint64(len(n.Elements()))*uint64(unsafe.Sizeof((*ast.Node)(nil)))
			}
			n.ForEachChild(func(k *ast.Node) bool { walk(k); return false })
		}
		walk(t.pointer)
		return ByteEstimate{Bytes: &bytes, Method: "repeated synthetic logical storage: sizeof concrete AST structs (embedded Node counted once), NodeList, used list pointer elements, numeric text lengths; no allocator rounding, GC overhead, arena spare capacity"}, unavailable("pointer factory arenas are no longer retained as census objects; spare capacity cannot be recovered from node pointers")
	}
	v := reflect.ValueOf(t.owner).Elem()
	used, capacity := uint64(v.Type().Size()), uint64(v.Type().Size())
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		name := v.Type().Field(i).Name
		switch f.Kind() {
		case reflect.Slice:
			elem := f.Type().Elem()
			if elem.Kind() != reflect.Uint8 && elem.Kind() != reflect.Uint32 && name != "nodes" && name != "lists" {
				if f.Cap() != 0 {
					reason := fmt.Sprintf("Store storage census does not cover nonempty %s", name)
					return unavailable(reason), unavailable(reason)
				}
			}
			used += uint64(f.Len()) * uint64(elem.Size())
			capacity += uint64(f.Cap()) * uint64(elem.Size())
		case reflect.Map:
			if !f.IsNil() {
				reason := fmt.Sprintf("Store storage census cannot account for allocated map %s", name)
				return unavailable(reason), unavailable(reason)
			}
		case reflect.Pointer:
			// identityDomain belongs to the shared Store registry, not the AST.
			if name != "identityDomain" && !f.IsNil() {
				reason := fmt.Sprintf("Store storage census does not cover pointer %s", name)
				return unavailable(reason), unavailable(reason)
			}
		}
	}
	method := "repeated synthetic storage: sizeof(Store) plus reflected scalar node/list/edge/intern backing slices; requires nil maps and no external payload pointers; includes sentinels; excludes shared registry, allocator rounding and GC metadata"
	return ByteEstimate{Bytes: &used, Method: method + "; len used"}, ByteEstimate{Bytes: &capacity, Method: method + "; cap reserved"}
}
