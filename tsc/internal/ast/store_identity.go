package ast

import (
	"sync"
	"sync/atomic"
)

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

// StoreSet is an identity domain. Readers only load atomic pointers; registering
// a Store never changes a slice or counter used by readers. IDs are not reused.
// The immutable directory grows geometrically; stable pages are never copied.
const storePageBits = 8
const storePageSize = 1 << storePageBits

type storeIdentityDomain struct{ marker byte }
type storeSlot struct {
	store atomic.Pointer[Store]
	file  atomic.Pointer[SourceFile]
}
type storePage struct{ slots [storePageSize]storeSlot }
type storeDirectory struct{ pages []*storePage }

type StoreSet struct {
	mu        sync.Mutex // writers only
	directory atomic.Pointer[storeDirectory]
	domain    *storeIdentityDomain // initialized under mu, independent of registry lifetime
	highWater uint64
	live      int
}

func NewStoreSet() *StoreSet { return &StoreSet{domain: &storeIdentityDomain{}} }

var identityStores = NewStoreSet()

func identitySet() *StoreSet { return identityStores }

func RegisterFile(file *SourceFile) { identitySet().BindFile(file) }

func RegisterStore(s *Store) StoreID {
	if s == nil {
		panic("ast: RegisterStore nil")
	}
	if id := s.ID(); id != 0 {
		// id is published last, so the immutable domain is initialized here.
		if s.identityDomain != identitySet().domain {
			panic("ast: foreign Store identity domain")
		}
		return id
	}
	return identitySet().register(s, false, false)
}

// UnregisterStore removes the lookup slot, not the Store's contents. Existing
// Handles remain valid and retain their owner. IDs and domains never change.
func UnregisterStore(s *Store)  { identitySet().Remove(s) }
func RegisteredStoreCount() int { return identitySet().liveCount() }
func NodeOf(g GlobalRef) Handle { return identitySet().At(g) }

// Add assigns a fresh identity. A Store belongs to at most one StoreSet.
func (ss *StoreSet) Add(s *Store) StoreID { return ss.register(s, true, false) }

// Lock order is StoreSet.mu -> Store.registrationMu, including competing sets.
func (ss *StoreSet) register(s *Store, exclusive, restore bool) StoreID {
	if s == nil {
		panic("ast: Add nil Store")
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	s.registrationMu.Lock()
	defer s.registrationMu.Unlock()
	if id := s.ID(); id != 0 {
		if exclusive {
			panic("ast: Store already registered")
		}
		if s.identityDomain != ss.domain {
			panic("ast: foreign Store identity domain")
		}
		if restore {
			slot := ss.slot(id)
			if slot.store.Load() == nil {
				slot.store.Store(s)
				ss.live++
			}
		}
		return id
	}
	if ss.highWater == uint64(^StoreID(0)) {
		panic("ast: StoreSet exhausted")
	}
	if ss.domain == nil {
		ss.domain = &storeIdentityDomain{}
	}
	id := StoreID(ss.highWater + 1)
	pageIndex := int(uint32(id-1) >> storePageBits)
	directory := ss.directory.Load()
	if directory == nil || pageIndex >= len(directory.pages) {
		size := 1
		if directory != nil {
			size = 2 * len(directory.pages)
		}
		next := &storeDirectory{pages: make([]*storePage, max(size, pageIndex+1))}
		start := 0
		if directory != nil {
			start = copy(next.pages, directory.pages)
		}
		for i := start; i < len(next.pages); i++ {
			next.pages[i] = &storePage{}
		}
		ss.directory.Store(next)
		directory = next
	}
	s.identityDomain = ss.domain
	directory.pages[pageIndex].slots[uint32(id-1)&(storePageSize-1)].store.Store(s)
	ss.highWater++
	ss.live++
	// Observing a nonzero ID guarantees directory, slot and domain publication.
	s.id.Store(uint32(id))
	return id
}

func (ss *StoreSet) BindFile(file *SourceFile) {
	if ss == nil || file == nil {
		return
	}
	if s := file.ParseStore(); s != nil {
		id := ss.register(s, false, true)
		ss.SetFile(id, file)
	}
}

func (ss *StoreSet) Remove(s *Store) {
	if ss == nil || s == nil || s.ID() == 0 {
		return
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if s.identityDomain != ss.domain {
		return
	}
	slot := ss.slot(s.ID())
	if slot != nil && slot.store.Load() == s {
		slot.file.Store(nil)
		slot.store.Store(nil)
		ss.live--
	}
}

func (ss *StoreSet) liveCount() int {
	if ss == nil {
		return 0
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.live
}

func (ss *StoreSet) SetFile(id StoreID, file *SourceFile) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if id == 0 || uint64(id) > ss.highWater {
		panic("ast: SetFile unknown StoreID")
	}
	ss.slot(id).file.Store(file)
}

func (ss *StoreSet) slot(id StoreID) *storeSlot {
	if ss == nil || id == 0 {
		return nil
	}
	directory := ss.directory.Load()
	index := uint32(id - 1)
	if directory == nil || int(index>>storePageBits) >= len(directory.pages) {
		return nil
	}
	return &directory.pages[index>>storePageBits].slots[index&(storePageSize-1)]
}

func (ss *StoreSet) File(id StoreID) *SourceFile {
	if slot := ss.slot(id); slot != nil {
		return slot.file.Load()
	}
	return nil
}
func (ss *StoreSet) Store(id StoreID) *Store {
	if slot := ss.slot(id); slot != nil {
		return slot.store.Load()
	}
	return nil
}
func (ss *StoreSet) At(g GlobalRef) Handle {
	if s := ss.Store(g.StoreID()); s != nil {
		return s.At(g.Ref())
	}
	return Handle{}
}

// ID reports the StoreID assigned by StoreSet.Add, or 0 before registration.
func (s *Store) ID() StoreID {
	if s == nil {
		return 0
	}
	return StoreID(s.id.Load())
}

// GlobalRef returns the process-wide identity for ref without constructing a
// Handle. It has the same registration requirement as Handle.Global.
func (s *Store) GlobalRef(ref NodeRef) GlobalRef {
	if s == nil || ref == 0 {
		return 0
	}
	id := StoreID(s.id.Load())
	if id == 0 {
		panic("ast: Global on unregistered Store")
	}
	return MakeGlobalRef(id, ref)
}

// Global returns the process-wide identity of the node. It panics on a
// Store that was never registered, because a silent 0 would corrupt any
// map keyed by GlobalRef.
func (h Handle) Global() GlobalRef {
	if h.id == 0 || h.s == nil {
		return 0
	}
	id := StoreID(h.s.id.Load())
	if id == 0 {
		panic("ast: Global on unregistered Store")
	}
	return MakeGlobalRef(id, h.id)
}
