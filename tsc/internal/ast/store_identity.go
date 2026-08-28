package ast

import "sync"

// StoreID identifies a Store within one StoreSet. 0 is missing.
type StoreID uint32

// GlobalRef is a process-wide node identity: StoreID in the high 32 bits,
// NodeRef in the low 32. 0 is missing. Unlike *Node or NodeId, it is
// pointer-free, deterministic for a fixed registration order, and usable
// as a map key without making the map scannable.
type GlobalRef uint64

func MakeGlobalRef(store StoreID, ref NodeRef) GlobalRef {
	if store == 0 || ref == 0 {
		return 0
	}
	return GlobalRef(store)<<32 | GlobalRef(ref)
}

func (g GlobalRef) StoreID() StoreID { return StoreID(g >> 32) }
func (g GlobalRef) Ref() NodeRef     { return NodeRef(g) }

// StoreSet is the identity domain: it assigns StoreIDs and resolves
// GlobalRefs back to Handles. Register stores in file order to get
// deterministic ids across runs.
type StoreSet struct {
	mu     sync.RWMutex
	stores []*Store      // index i holds the Store with StoreID i+1
	files  []*SourceFile // parallel to stores; nil until SetFile
}

func NewStoreSet() *StoreSet { return &StoreSet{} }

var (
	identityOnce   sync.Once
	identityStores *StoreSet
)

func identitySet() *StoreSet {
	identityOnce.Do(func() { identityStores = NewStoreSet() })
	return identityStores
}

func RegisterFile(file *SourceFile) {
	identitySet().BindFile(file)
}

func NodeOf(g GlobalRef) *Node {
	return identitySet().NodeOf(g)
}

// Add registers a Store and assigns its StoreID. A Store belongs to at
// most one StoreSet; registering it twice panics.
func (ss *StoreSet) Add(s *Store) StoreID {
	if s == nil {
		panic("ast: Add nil Store")
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if s.id != 0 {
		panic("ast: Store already registered")
	}
	id := StoreID(len(ss.stores) + 1)
	s.id = id
	ss.stores = append(ss.stores, s)
	ss.files = append(ss.files, nil)
	return id
}

func (ss *StoreSet) BindFile(file *SourceFile) {
	if ss == nil || file == nil {
		return
	}
	s := file.ParseStore()
	if s == nil {
		return
	}
	if s.ID() == 0 {
		ss.Add(s)
	} else {
		ss.adopt(s)
	}
	ss.SetFile(s.ID(), file)
}

func (ss *StoreSet) adopt(s *Store) {
	if s == nil || s.id == 0 {
		return
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	idx := int(s.id - 1)
	for len(ss.stores) <= idx {
		ss.stores = append(ss.stores, nil)
		ss.files = append(ss.files, nil)
	}
	ss.stores[idx] = s
}

func (ss *StoreSet) NodeOf(g GlobalRef) *Node {
	if ss == nil || g == 0 {
		return nil
	}
	file := ss.File(g.StoreID())
	if file == nil {
		return nil
	}
	return file.NodeFor(g.Ref())
}

func (ss *StoreSet) SetFile(id StoreID, file *SourceFile) {
	if id == 0 {
		panic("ast: SetFile missing StoreID")
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if int(id) > len(ss.stores) {
		panic("ast: SetFile unknown StoreID")
	}
	ss.files[id-1] = file
}

func (ss *StoreSet) File(id StoreID) *SourceFile {
	if id == 0 {
		return nil
	}
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	if int(id) > len(ss.files) {
		return nil
	}
	return ss.files[id-1]
}

func (ss *StoreSet) Store(id StoreID) *Store {
	if id == 0 {
		return nil
	}
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	if int(id) > len(ss.stores) {
		return nil
	}
	return ss.stores[id-1]
}

func (ss *StoreSet) At(g GlobalRef) Handle {
	s := ss.Store(g.StoreID())
	if s == nil {
		return Handle{}
	}
	return s.At(g.Ref())
}

// ID reports the StoreID assigned by StoreSet.Add, or 0 before registration.
func (s *Store) ID() StoreID {
	if s == nil {
		return 0
	}
	return s.id
}

// Global returns the process-wide identity of the node. It panics on a
// Store that was never registered, because a silent 0 would corrupt any
// map keyed by GlobalRef.
func (h Handle) Global() GlobalRef {
	if h.id == 0 || h.s == nil {
		return 0
	}
	if h.s.id == 0 {
		panic("ast: Global on unregistered Store")
	}
	return MakeGlobalRef(h.s.id, h.id)
}
