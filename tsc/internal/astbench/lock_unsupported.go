//go:build !darwin && !linux

package astbench

import (
	"fmt"
	"os"
	"runtime"
)

func tryCampaignLock(*os.File) (bool, error) {
	return false, fmt.Errorf("campaign locking is unsupported on %s; Darwin or Linux is required", runtime.GOOS)
}
