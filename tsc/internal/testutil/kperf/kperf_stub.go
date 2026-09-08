//go:build !kperf || !darwin || !arm64

package kperf

import "errors"

// Counters is a snapshot of the calling thread's fixed PMCs.
type Counters struct {
	Cycles       uint64
	Instructions uint64
}

// Open is a no-op stub outside kperf+darwin+arm64 builds.
func Open() error {
	return errors.New("kperf: unavailable (need GOOS=darwin GOARCH=arm64 -tags=kperf CGO_ENABLED=1 and root)")
}

// Close is a no-op stub.
func Close() {}

// Read returns zeros in stub builds.
func Read() Counters { return Counters{} }
