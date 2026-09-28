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
