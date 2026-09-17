package astbench

import (
	"context"
	"fmt"
	"os"
	"time"
)

// fileLock owns a descriptor whose kernel lock is released even if the process
// exits without cleanup. The path's inode must remain stable across owners.
type fileLock struct{ f *os.File }

func acquireLock(ctx context.Context, path string) (*fileLock, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("campaign lock: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	acquired := false
	defer func() {
		if !acquired {
			_ = f.Close()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("campaign lock: %w", err)
		}
		locked, err := tryCampaignLock(f)
		if err != nil {
			return nil, err
		}
		if locked {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("campaign lock: %w", err)
			}
			acquired = true
			return &fileLock{f: f}, nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("campaign lock timeout: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (l *fileLock) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = l.f.Close()
	// Do not unlink: waiters may already hold descriptors for this inode.
	l.f = nil
}
