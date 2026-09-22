package store

// Internals that this package's own tests read.

func (s *Store) NodeAt(ref NodeRef) Node { return s.node(ref) }
