//go:build !darwin

package storeclitest

func procRusage() (footprint, maxFootprint, instructions, cycles uint64) { return 0, 0, 0, 0 }
