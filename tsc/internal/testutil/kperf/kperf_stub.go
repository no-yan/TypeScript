//go:build !kperf || !darwin || !arm64 || !cgo

package kperf

import "errors"

const available = false

type Counters struct {
	Cycles       uint64
	Instructions uint64
}

func Open() error {
	return errors.New("kperf: unavailable (need GOOS=darwin GOARCH=arm64 -tags=kperf CGO_ENABLED=1 and root)")
}

func Close() {}

func Read() (Counters, error) { return Counters{}, errors.New("kperf: unavailable") }
