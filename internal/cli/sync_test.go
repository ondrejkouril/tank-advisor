package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestSyncDryRunCreatesNothing pins a bug this command actually had: the cache
// was opened before the dry-run branch, and opening creates the file, so
// --dry-run left a database behind on a clean machine.
func TestSyncDryRunCreatesNothing(t *testing.T) {
	env, buf := newTestEnv(t)

	if env.HasStore() {
		t.Fatal("cache exists before the test runs")
	}

	err := Run(context.Background(), env, []string{"sync", "--only", "wg", "--dry-run"})
	if err != nil {
		t.Fatalf("sync --dry-run = %v, want nil", err)
	}
	if env.HasStore() {
		t.Error("sync --dry-run created the cache database")
	}

	// It still has to say what it would do, or it is useless.
	out := buf.String()
	if !strings.Contains(out, "would sync") {
		t.Errorf("dry run did not report its intent:\n%s", out)
	}
	if !strings.Contains(out, "wg:encyclopedia/vehicles") {
		t.Errorf("dry run did not name the source:\n%s", out)
	}
}

// TestSyncDryRunNeedsNoCredentials: a dry run reports intent, so requiring an
// application_id would defeat its purpose on a machine that is not set up yet.
func TestSyncDryRunNeedsNoCredentials(t *testing.T) {
	env, _ := newTestEnv(t) // empty secret store

	if err := Run(context.Background(), env, []string{"sync", "--dry-run"}); err != nil {
		t.Errorf("sync --dry-run with no credentials = %v, want nil", err)
	}
}

// TestSyncWithoutCredentialsFails is the other side of it: a real sync must say
// what is missing rather than silently doing nothing.
func TestSyncWithoutCredentialsFails(t *testing.T) {
	env, _ := newTestEnv(t)

	err := Run(context.Background(), env, []string{"sync", "--only", "wg"})
	if err == nil {
		t.Fatal("sync with no application_id = nil, want an error")
	}
	if !strings.Contains(err.Error(), "auth wg") {
		t.Errorf("error %q does not say how to fix it", err)
	}
}

func TestSyncRejectsUnknownSource(t *testing.T) {
	env, buf := newTestEnv(t)

	err := Run(context.Background(), env, []string{"sync", "--only", "wows"})
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("sync --only wows = %v, want ErrUsage", err)
	}
	if !strings.Contains(buf.String(), "wows") {
		t.Errorf("message does not name the bad source:\n%s", buf.String())
	}
}

// TestSyncWN8DryRunNeedsNoWGCredentials: expected values are a public file, so
// neither a real nor a dry sync of them may demand a Wargaming application_id.
func TestSyncWN8DryRunNeedsNoWGCredentials(t *testing.T) {
	env, buf := newTestEnv(t)

	if err := Run(context.Background(), env, []string{"sync", "--only", "wn8", "--dry-run"}); err != nil {
		t.Fatalf("sync --only wn8 --dry-run = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "xvm:wn8exp") {
		t.Errorf("dry run did not name the source:\n%s", out)
	}
	if strings.Contains(out, "wg:") {
		t.Errorf("--only wn8 would also sync Wargaming sources:\n%s", out)
	}
}

// TestSyncRejectsTomato: the source no longer exists, so it must read as an
// unknown source rather than as something not yet built.
func TestSyncRejectsTomato(t *testing.T) {
	env, buf := newTestEnv(t)

	if err := Run(context.Background(), env, []string{"sync", "--only", "tomato"}); !errors.Is(err, ErrUsage) {
		t.Fatalf("sync --only tomato = %v, want ErrUsage", err)
	}
	if !strings.Contains(buf.String(), "unknown source") {
		t.Errorf("expected an unknown-source message; got:\n%s", buf.String())
	}
}

// TestSyncWhileAnotherRunsIsNotAFailure: the session-start hook and a sync by
// hand can overlap. The later one says so and exits 0, and with --quiet says
// nothing at all.
func TestSyncWhileAnotherRunsIsNotAFailure(t *testing.T) {
	defer func(d time.Duration) { syncLockWait = d }(syncLockWait)
	syncLockWait = 0

	env, buf := newTestEnv(t)
	db, err := env.OpenStore(context.Background())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	_, err = db.BeginSyncRun(context.Background(), time.Now())
	db.Close()
	if err != nil {
		t.Fatalf("BeginSyncRun: %v", err)
	}

	// --only wn8 needs no credentials, and the lock is taken before any
	// request, so nothing here reaches the network.
	if err := Run(context.Background(), env, []string{"sync", "--only", "wn8"}); err != nil {
		t.Fatalf("sync during another = %v, want nil", err)
	}
	if !strings.Contains(buf.String(), "another sync is still running") {
		t.Errorf("sync did not say another was running:\n%s", buf.String())
	}

	buf.Reset()
	if err := Run(context.Background(), env, []string{"sync", "--only", "wn8", "--quiet"}); err != nil {
		t.Fatalf("sync --quiet during another = %v, want nil", err)
	}
	if buf.Len() != 0 {
		t.Errorf("sync --quiet printed:\n%s", buf.String())
	}
}

// TestSyncQuietStillFails: --quiet hides the report, not a failure. The error
// is returned, and main prints it to the process's stderr.
func TestSyncQuietStillFails(t *testing.T) {
	env, buf := newTestEnv(t)
	err := Run(context.Background(), env, []string{"sync", "--only", "wg", "--quiet"})
	if err == nil || !strings.Contains(err.Error(), "auth wg") {
		t.Errorf("sync --quiet with no credentials = %v, want the auth error", err)
	}
	if buf.Len() != 0 {
		t.Errorf("sync --quiet printed:\n%s", buf.String())
	}
}
