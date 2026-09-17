//go:build binderinvestigation && kperf && darwin && arm64 && cgo

package kperf

import "testing"

func TestReadEventsIntoRejectsNilDestination(t *testing.T) {
	if err := ReadEventsInto(nil); err == nil {
		t.Fatal("nil destination succeeded")
	}
}

func TestReadEventsIntoReportsClosedPMU(t *testing.T) {
	var dst [10]uint64
	if err := ReadEventsInto(&dst); err == nil {
		t.Fatal("closed PMU read succeeded")
	}
	if dst != ([10]uint64{}) {
		t.Fatalf("closed PMU wrote counters: %v", dst)
	}
}

func TestReadEventsCompatibilityWrapperReportsClosedPMU(t *testing.T) {
	got, err := ReadEvents()
	if err == nil {
		t.Fatal("closed PMU read succeeded")
	}
	if got != ([10]uint64{}) {
		t.Fatalf("closed PMU returned counters: %v", got)
	}
}
