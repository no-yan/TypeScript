//go:build darwin || linux

package workload

import (
	"runtime"
	"syscall"
)

func peakRSS() ByteEstimate {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return ByteEstimate{Method: "getrusage(RUSAGE_SELF)", Reason: err.Error()}
	}
	bytes := uint64(usage.Maxrss)
	if runtime.GOOS == "linux" {
		bytes *= 1024
	}
	return ByteEstimate{Bytes: &bytes, Method: "getrusage(RUSAGE_SELF).Maxrss after build and metadata; process-wide lifetime peak including setup, not AST bytes"}
}
