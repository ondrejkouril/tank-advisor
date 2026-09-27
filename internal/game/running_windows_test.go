package game

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestWaitForExitReturnsWhenTheProcessEnds(t *testing.T) {
	// ping -n 3 takes about two seconds.
	cmd := exec.Command("ping", "-n", "3", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start ping:", err)
	}
	go cmd.Wait()
	start := time.Now()
	if err := WaitForExit(context.Background(), uint32(cmd.Process.Pid)); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); waited < time.Second || waited > 10*time.Second {
		t.Errorf("waited %s for a two-second process", waited)
	}
	// A process that is gone counts as exited at once.
	start = time.Now()
	WaitForExit(context.Background(), uint32(cmd.Process.Pid))
	if time.Since(start) > time.Second {
		t.Error("waited for a process that had already exited")
	}
}
