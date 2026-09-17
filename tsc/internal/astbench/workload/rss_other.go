//go:build !darwin && !linux

package workload

func peakRSS() ByteEstimate {
	return ByteEstimate{Method: "unavailable", Reason: "process peak RSS collection is implemented only on Darwin and Linux"}
}
