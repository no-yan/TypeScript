package store

// Internals that this package's own tests read and set.

func (s *Store) NodeAt(ref NodeRef) Node { return s.node(ref) }

// StoreChecks is whether the test binary has the storechecks tag.
const StoreChecks = storeChecks

// ForeignBit is the mark of a foreign edge.
const ForeignBit = foreignBit

// SetInitialCaps sets the initial capacity of extra and texts of the next
// Stores of f, to force their reallocation.
func SetInitialCaps(f *Factory, extra, texts int) {
	f.extraCap, f.textsCap = extra, texts
}

func StoreOf(n Node) *Store         { return n.s }
func ListStoreOf(l List) *Store     { return l.s }
func NodeCapacity(s *Store) int     { return cap(s.nodes) - 1 }
func ExtraCap(s *Store) int         { return cap(s.extra) }
func TextsCap(s *Store) int         { return s.private.texts.Cap() }
func IsSynthetic(n Node) bool       { return n.s.owner != nil }
func ForeignNodeCount(s *Store) int { return len(s.foreignNodes) }

// SetRawParent writes an edge value into the parent word, as a corrupt
// Store would hold.
func SetRawParent(n Node, e NodeRef) { n.h.parent = e }

// SetNextSymbolId moves the process-wide counter of GetSymbolId. Only the
// isolated subprocess of TestSymbolIdLimit calls it.
func SetNextSymbolId(v uint64) { nextSymbolId.Store(v) }

// FlowOwner is the Store of the Bound that a Flow carries.
func FlowOwner(f Flow) *Store { return f.bound.store }

// ParseJSDocForNodeFunc is the registered lazy JSDoc parser, for a test that
// wraps it to count the parses.
func ParseJSDocForNodeFunc() func(*File, Node) []Node { return parseJSDocForNode }

func IsSealed(s *Store) bool { return s.sealed }
