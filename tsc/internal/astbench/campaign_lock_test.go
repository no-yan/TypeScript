package astbench

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCampaignLockHelperProcess(t *testing.T) {
	path := os.Getenv("ASTBENCH_TEST_LOCK_PATH")
	if path == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lock, err := acquireLock(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	fmt.Fprintln(os.Stdout, "lock-acquired")
	for {
		time.Sleep(time.Hour)
	}
}

func TestCampaignLockRecoversAfterKilledOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCampaignLockHelperProcess$")
	cmd.Env = append(os.Environ(), "ASTBENCH_TEST_LOCK_PATH="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() && scanner.Text() == "lock-acquired" {
			ready <- nil
			return
		}
		ready <- fmt.Errorf("lock helper failed handshake: %q (%v)", scanner.Text(), scanner.Err())
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock helper did not acquire lock")
	}

	heldCtx, heldCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	contender, err := acquireLock(heldCtx, path)
	heldCancel()
	if contender != nil {
		contender.release()
		t.Fatal("campaign lock allowed another owner while helper was alive")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded exclusion while owner alive, got %v", err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	waited = true
	if err == nil {
		t.Fatal("helper unexpectedly exited successfully after kill")
	}

	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), time.Second)
	defer recoveryCancel()
	recovered, err := acquireLock(recoveryCtx, path)
	if err != nil {
		t.Fatalf("campaign lock remained stale after killed owner exited: %v", err)
	}
	defer recovered.release()
}

func TestCampaignLockIgnoresUnownedLegacyPIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.lock")
	// A legacy PID file has no kernel lock owner, regardless of its contents.
	if err := os.WriteFile(path, []byte("999999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lock, err := acquireLock(ctx, path)
	if err != nil {
		t.Fatalf("unowned legacy PID file prevented acquisition: %v", err)
	}
	defer lock.release()
}

func TestCampaignLockRejectsCanceledContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "campaign.lock")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lock, err := acquireLock(ctx, path)
	if lock != nil {
		lock.release()
		t.Fatal("canceled context acquired vacant campaign lock")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
