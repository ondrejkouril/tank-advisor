package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/game"
	"github.com/ondrejkouril/tank-advisor/internal/secrets"
)

// TestMain isolates the package from the developer's real keychain. Most tests
// use an in-memory store, but Bootstrap deliberately builds a real one, and a
// test must not read or write the machine's credentials to exercise it.
func TestMain(m *testing.M) {
	keyring.MockInit()
	m.Run()
}

// newTestEnv returns an Env writing into a single buffer, so tests can assert on
// whatever the command produced without caring which stream it chose. It uses an
// in-memory secret store and temporary paths, so tests never read the developer
// machine's keychain or config.
func newTestEnv(t *testing.T) (*Env, *bytes.Buffer) {
	t.Helper()

	dir := t.TempDir()
	var buf bytes.Buffer
	cfg := config.Default()
	// The account the recorded fixtures (and the mod dump fixture) belong to.
	cfg.Account = config.Account{Realm: "eu", AccountID: 512345678, Nickname: "example_player"}
	return &Env{
		Stdout:  &buf,
		Stderr:  &buf,
		Version: "test",
		Config:  cfg,
		Paths: config.Paths{
			ConfigDir:  dir,
			ConfigFile: filepath.Join(dir, "config.yaml"),
			DataDir:    dir,
			DBFile:     filepath.Join(dir, "wotctx.db"),
		},
		Secrets: secrets.NewMemory(),
	}, &buf
}

// TestRootHelpListsEveryCommand pins the acceptance criterion for plan step 1:
// `wotctx --help` must describe the whole surface in docs/spec.md section 7.
func TestRootHelpListsEveryCommand(t *testing.T) {
	want := []string{
		"auth wg",
		"data delete",
		"sync",
		"brief",
		"query garage",
		"query tank",
		"query performance",
		"query resources",
		"query sessions",
		"query candidates",
		"doctor",
		"overlay validate",
		"overlay path",
		"meta moe",
		"mcp",
		"version",
	}

	env, buf := newTestEnv(t)
	if err := Run(context.Background(), env, []string{"--help"}); err != nil {
		t.Fatalf("Run(--help) = %v, want nil", err)
	}

	got := buf.String()
	for _, cmd := range want {
		if !strings.Contains(got, cmd) {
			t.Errorf("root help does not mention %q; got:\n%s", cmd, got)
		}
	}
}

func TestVersionPrintsBuildVersion(t *testing.T) {
	env, buf := newTestEnv(t)
	if err := Run(context.Background(), env, []string{"version"}); err != nil {
		t.Fatalf("Run(version) = %v, want nil", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "test" {
		t.Errorf("version printed %q, want %q", got, "test")
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	env, buf := newTestEnv(t)
	err := Run(context.Background(), env, []string{"garage"}) // valid only under `query`
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("Run(garage) = %v, want ErrUsage", err)
	}
	if !strings.Contains(buf.String(), `unknown command "garage"`) {
		t.Errorf("expected the offending name in the message; got:\n%s", buf.String())
	}
}

func TestGroupWithoutSubcommandIsUsageError(t *testing.T) {
	env, _ := newTestEnv(t)
	if err := Run(context.Background(), env, []string{"query"}); !errors.Is(err, ErrUsage) {
		t.Fatalf("Run(query) = %v, want ErrUsage", err)
	}
}

// TestBootstrapSurvivesABrokenConfig checks that a bad config file does not stop
// the commands needed to diagnose it.
func TestBootstrapSurvivesABrokenConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)

	paths, err := config.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("account:\n  realm: nonsense\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var buf bytes.Buffer
	env := Bootstrap(&buf, &buf, "test")
	if env.ConfigErr == nil {
		t.Fatal("Bootstrap did not record the config error")
	}
	// version must still work, because being unable to report your own version
	// while the config is broken is exactly the wrong failure mode.
	if err := Run(context.Background(), env, []string{"version"}); err != nil {
		t.Errorf("Run(version) with a broken config = %v, want nil", err)
	}
}

// Tests never look for this machine's game: a test that needs one sets
// Config.GameDir.
func init() {
	gameLocator = func() game.Locator { return game.Locator{} }
}
