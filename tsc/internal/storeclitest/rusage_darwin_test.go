//go:build darwin

package storeclitest

import (
	"os"
	"syscall"
	"unsafe"
)

// rusageInfoV4 is struct rusage_info_v4 of <sys/resource.h>.
type rusageInfoV4 struct {
	uuid                                   [16]byte
	userTime, systemTime                   uint64
	pkgIdleWkups, interruptWkups           uint64
	pageins, wiredSize, residentSize       uint64
	physFootprint                          uint64
	procStartAbstime, procExitAbstime      uint64
	childUserTime, childSystemTime         uint64
	childPkgIdleWkups, childInterruptWkups uint64
	childPageins, childElapsedAbstime      uint64
	diskioBytesread, diskioByteswritten    uint64
	cpuTimeQoS                             [7]uint64
	billedSystemTime, servicedSystemTime   uint64
	logicalWrites                          uint64
	lifetimeMaxPhysFootprint               uint64
	instructions, cycles                   uint64
	billedEnergy, servicedEnergy           uint64
	intervalMaxPhysFootprint, runnableTime uint64
}

// procRusage is proc_pid_rusage(getpid(), RUSAGE_INFO_V4) as libproc makes
// it, __proc_info(PROC_INFO_CALL_PIDRUSAGE, pid, flavor, 0, buffer, 0)
// (system call 336), with no cgo: the phys footprint (compressed memory
// included) and its peak, and the process's instructions and cycles.
func procRusage() (footprint, maxFootprint, instructions, cycles uint64) {
	var ri rusageInfoV4
	if _, _, e := syscall.Syscall6(336, 9, uintptr(os.Getpid()), 4, 0, uintptr(unsafe.Pointer(&ri)), 0); e != 0 {
		panic(e)
	}
	return ri.physFootprint, ri.lifetimeMaxPhysFootprint, ri.instructions, ri.cycles
}
