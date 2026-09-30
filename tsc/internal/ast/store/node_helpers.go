package store

import "slices"

// The generic helpers of internal/core and slices whose comparison, for Node,
// must be IsNil or SameNode (Z1, internal/ast/docs/
// store-node-zero-design-20260926.md 3, and the translation rule of
// store-checker-7d0-implementation-instructions-20260924.md 5.1). Node is not
// comparable, so the core versions do not compile on it; the translator
// rewrites a call whose actual type argument is Node to the function here.
// Each keeps the core version's order of evaluation, number and order of
// callback calls, early exit, element order and reuse of the input slice.

// FirstNonNil is core.FirstNonNil with U = Node: the first non-nil result.
func FirstNonNil[T any](slice []T, f func(T) Node) Node {
	for _, value := range slice {
		mapped := f(value)
		if !mapped.IsNil() {
			return mapped
		}
	}
	return Node{}
}

// MapNonNil is core.MapNonNil with U = Node.
func MapNonNil[T any](slice []T, f func(T) Node) []Node {
	var result []Node
	for _, value := range slice {
		mapped := f(value)
		if !mapped.IsNil() {
			result = append(result, mapped)
		}
	}
	return result
}

// OrElse is core.OrElse with T = Node. Both arguments are evaluated by the
// caller, as in core.
func OrElse(value Node, defaultValue Node) Node {
	if !value.IsNil() {
		return value
	}
	return defaultValue
}

// SameMap is core.SameMap with T = Node: the input slice when f changes
// nothing.
func SameMap(slice []Node, f func(Node) Node) []Node {
	for i, value := range slice {
		mapped := f(value)
		if !SameNode(mapped, value) {
			result := make([]Node, len(slice))
			copy(result, slice[:i])
			result[i] = mapped
			for j := i + 1; j < len(slice); j++ {
				result[j] = f(slice[j])
			}
			return result
		}
	}
	return slice
}

// SameMapIndex is core.SameMapIndex with T = Node.
func SameMapIndex(slice []Node, f func(Node, int) Node) []Node {
	for i, value := range slice {
		mapped := f(value, i)
		if !SameNode(mapped, value) {
			result := make([]Node, len(slice))
			copy(result, slice[:i])
			result[i] = mapped
			for j := i + 1; j < len(slice); j++ {
				result[j] = f(slice[j], j)
			}
			return result
		}
	}
	return slice
}

// Deduplicate is core.Deduplicate with T = Node: the input slice when it has
// no duplicate.
func Deduplicate(slice []Node) []Node {
	if len(slice) > 1 {
		for i, value := range slice {
			if Contains(slice[:i], value) {
				result := slices.Clone(slice[:i])
				for i++; i < len(slice); i++ {
					value = slice[i]
					if !Contains(result, value) {
						result = append(result, value)
					}
				}
				return result
			}
		}
	}
	return slice
}

// AppendIfUnique is core.AppendIfUnique with T = Node.
func AppendIfUnique(slice []Node, element Node) []Node {
	if Contains(slice, element) {
		return slice
	}
	return append(slice, element)
}

// Contains is slices.Contains with SameNode.
func Contains(slice []Node, n Node) bool {
	return Index(slice, n) >= 0
}

// Index is slices.Index with SameNode.
func Index(slice []Node, n Node) int {
	for i, value := range slice {
		if SameNode(value, n) {
			return i
		}
	}
	return -1
}

// The List versions of the helpers of core and slices (R4 of
// store-checker-7d2-design-remainder-20260929.md 2.1): the translation calls
// <F>List where the Pointer passes list.Nodes to F. Each reads the list with
// Len and At, in the order of the core version, with its early exits. A List
// that an accessor returns for an absent member belongs to its Store (block 0)
// and reads as empty, as the Pointer's nil slice from the accessor does. The
// zero List{} is the Pointer's nil *ast.NodeList: like nil.Nodes, it cannot be
// read, and these helpers panic on it.

func SomeList(l List, f func(Node) bool) bool {
	for i := range l.Len() {
		if f(l.At(i)) {
			return true
		}
	}
	return false
}

func EveryList(l List, f func(Node) bool) bool {
	for i := range l.Len() {
		if !f(l.At(i)) {
			return false
		}
	}
	return true
}

func FindList(l List, f func(Node) bool) Node {
	for i := range l.Len() {
		if value := l.At(i); f(value) {
			return value
		}
	}
	return Node{}
}

func FindIndexList(l List, f func(Node) bool) int {
	for i := range l.Len() {
		if f(l.At(i)) {
			return i
		}
	}
	return -1
}

func FindLastIndexList(l List, f func(Node) bool) int {
	for i := l.Len() - 1; i >= 0; i-- {
		if f(l.At(i)) {
			return i
		}
	}
	return -1
}

// MapList is core.Map: nil for the nil List.
func MapList[U any](l List, f func(Node) U) []U {
	if l.IsNil() {
		return nil
	}
	result := make([]U, l.Len())
	for i := range result {
		result[i] = f(l.At(i))
	}
	return result
}

// FilterList is core.Filter. The core version returns its input when nothing
// is dropped; a List is not a slice, so this one returns a copy then.
func FilterList(l List, f func(Node) bool) []Node {
	var result []Node
	for i := range l.Len() {
		if value := l.At(i); f(value) {
			result = append(result, value)
		}
	}
	if result == nil && !l.IsNil() && l.Len() == 0 {
		return []Node{}
	}
	return result
}

func FirstOrNilList(l List) Node {
	if l.Len() != 0 {
		return l.At(0)
	}
	return Node{}
}

func LastOrNilList(l List) Node {
	if n := l.Len(); n != 0 {
		return l.At(n - 1)
	}
	return Node{}
}

func ElementOrNilList(l List, index int) Node {
	if index < l.Len() {
		return l.At(index)
	}
	return Node{}
}

// IndexList is slices.Index with SameNode.
func IndexList(l List, n Node) int {
	for i := range l.Len() {
		if SameNode(l.At(i), n) {
			return i
		}
	}
	return -1
}

// ContainsList is slices.Contains with SameNode.
func ContainsList(l List, n Node) bool {
	return IndexList(l, n) >= 0
}

// BinarySearchFuncList is slices.BinarySearchFunc on a list.
func BinarySearchFuncList[T any](l List, target T, cmp func(Node, T) int) (int, bool) {
	n := l.Len()
	i, j := 0, n
	for i < j {
		h := int(uint(i+j) >> 1)
		if cmp(l.At(h), target) < 0 {
			i = h + 1
		} else {
			j = h
		}
	}
	return i, i < n && cmp(l.At(i), target) == 0
}
