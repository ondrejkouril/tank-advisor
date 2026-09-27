package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/secrets"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// notSetUp returns a test Env as a fresh install sees it: no account.
func notSetUp(t *testing.T) *Env {
	env, _ := newTestEnv(t)
	env.Config = config.Default()
	return env
}

func TestRecordAccountWritesTheConfig(t *testing.T) {
	env := notSetUp(t)
	os.WriteFile(env.Paths.ConfigFile, []byte("# mine\noverlay_path: C:/o.yaml\n"), 0o644)

	if err := recordAccount(env, wg.Token{AccountID: 42, Nickname: "someone"}); err != nil {
		t.Fatalf("recordAccount: %v", err)
	}
	cfg, err := config.Load(env.Paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Account != (config.Account{Realm: "eu", AccountID: 42, Nickname: "someone"}) || cfg.OverlayPath != "C:/o.yaml" {
		t.Errorf("config = %+v", cfg)
	}
	if !env.Config.Configured() {
		t.Error("the running Env still looks unconfigured")
	}
	raw, _ := os.ReadFile(env.Paths.ConfigFile)
	if !strings.Contains(string(raw), "# mine") {
		t.Errorf("the comment was lost:\n%s", raw)
	}
}

func TestLogoutRevokesAndForgetsTheToken(t *testing.T) {
	for name, status := range map[string]int{"wargaming answers": 200, "wargaming is down": 503} {
		t.Run(name, func(t *testing.T) {
			var revoked string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				revoked = r.PostForm.Get("access_token") // a body only, as Wargaming reads it
				w.WriteHeader(status)
				w.Write([]byte(`{"status":"ok","data":null}`))
			}))
			defer srv.Close()
			client, _ := wg.New("eu", "app", wg.WithBaseURL(srv.URL), wg.WithSleeper(func(context.Context, time.Duration) error { return nil }))

			env, _ := newTestEnv(t)
			storeToken(env, wg.Token{AccessToken: fakeToken, ExpiresAt: time.Now().Add(time.Hour)})
			if err := logoutStoredToken(context.Background(), env, client); err != nil {
				t.Fatalf("logout: %v", err)
			}
			if revoked != fakeToken {
				t.Errorf("auth/logout was not called with the token")
			}
			for _, k := range []secrets.Key{secrets.WGAccessToken, secrets.WGTokenExpiry} {
				if _, _, err := env.Secrets.Get(k); !errors.Is(err, secrets.ErrNotFound) {
					t.Errorf("%s still stored", k)
				}
			}
		})
	}
}

func TestAuthWGRefusesToSwitchRealmsOverExistingData(t *testing.T) {
	env, _ := newTestEnv(t)
	env.Secrets.Set(secrets.WGApplicationID, "app")
	err := Run(context.Background(), env, []string{"auth", "wg", "--realm", "com"})
	if err == nil || !strings.Contains(err.Error(), "data delete") {
		t.Errorf("auth wg --realm com over an eu account = %v, want a pointer to data delete", err)
	}
}

func TestDataDeleteRemovesTheAccountsData(t *testing.T) {
	env, buf := newTestEnv(t)
	os.WriteFile(env.Paths.DBFile, []byte("db"), 0o644)
	os.MkdirAll(filepath.Dir(env.modDumpPath()), 0o755)
	os.WriteFile(env.modDumpPath(), []byte("{}"), 0o644)
	storeToken(env, wg.Token{AccessToken: fakeToken, ExpiresAt: time.Now().Add(time.Hour)})
	env.Secrets.Set(secrets.WGApplicationID, "mine")
	config.Set(env.Paths.ConfigFile,
		config.Update{Key: "account.account_id", Value: 512345678},
		config.Update{Key: "account.nickname", Value: "example_player"},
		config.Update{Key: "overlay_path", Value: "C:/o.yaml"})

	if err := Run(context.Background(), env, []string{"data", "delete", "--yes"}); err != nil {
		t.Fatalf("data delete: %v\n%s", err, buf)
	}
	for _, path := range []string{env.Paths.DBFile, env.modDumpPath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists", path)
		}
	}
	if _, _, err := env.Secrets.Get(secrets.WGAccessToken); err == nil {
		t.Error("the token is still stored")
	}
	if _, _, err := env.Secrets.Get(secrets.WGApplicationID); err != nil {
		t.Error("the player's own application id was deleted; it is not account data")
	}
	cfg, _ := config.Load(env.Paths.ConfigFile)
	if cfg.Configured() || cfg.OverlayPath != "C:/o.yaml" {
		t.Errorf("config after delete = %+v, want no account and the overlay kept", cfg)
	}
}

func TestDataDeleteNeedsConfirmation(t *testing.T) {
	env, _ := newTestEnv(t)
	os.WriteFile(env.Paths.DBFile, []byte("db"), 0o644)
	env.Stdin = strings.NewReader("no\n")
	if err := Run(context.Background(), env, []string{"data", "delete"}); err != nil {
		t.Fatalf("data delete: %v", err)
	}
	if _, err := os.Stat(env.Paths.DBFile); err != nil {
		t.Error("the cache was deleted without confirmation")
	}
}

func TestANewInstallIsNotSetUp(t *testing.T) {
	env := notSetUp(t)
	if err := Run(context.Background(), env, []string{"sync"}); err == nil || !strings.Contains(err.Error(), "auth wg --realm") {
		t.Errorf("sync without an account = %v, want the setup command", err)
	}
	c := checkConfig(env)
	if c.Status != statusFail || !strings.Contains(c.Fix, "auth wg --realm") {
		t.Errorf("config check = %+v", c)
	}
}

// TestALoginWithNoAccountRecordedAlwaysLogsIn covers a keychain that still
// holds a token while config.yaml holds no account: the token cannot say
// whose it is, so the login runs, and records the account.
func TestALoginWithNoAccountRecordedAlwaysLogsIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","data":{"location":"https://eu.wargaming.net/id/openid/"}}`))
	}))
	defer srv.Close()
	newWGClient = func(realm, appID string) (*wg.Client, error) {
		return wg.New(realm, appID, wg.WithBaseURL(srv.URL))
	}
	t.Cleanup(func() { newWGClient = func(realm, appID string) (*wg.Client, error) { return wg.New(realm, appID) } })

	env := notSetUp(t)
	env.Secrets.Set(secrets.WGApplicationID, "app")
	storeToken(env, wg.Token{AccessToken: "left-over", ExpiresAt: time.Now().Add(10 * 24 * time.Hour)})
	expires := time.Now().Add(14 * 24 * time.Hour).Unix()
	env.Stdin = strings.NewReader("http://127.0.0.1:8731/callback?status=ok&access_token=fresh&nickname=someone&account_id=42&expires_at=" +
		strconv.FormatInt(expires, 10) + "\n")

	if err := Run(context.Background(), env, []string{"auth", "wg", "--manual"}); err != nil {
		t.Fatalf("auth wg: %v", err)
	}
	cfg, _ := config.Load(env.Paths.ConfigFile)
	if cfg.Account.AccountID != 42 || cfg.Account.Nickname != "someone" {
		t.Errorf("account = %+v, want the one that logged in", cfg.Account)
	}
	if token, _, _ := env.Secrets.Get(secrets.WGAccessToken); token != "fresh" {
		t.Errorf("token = %q, want the fresh one", token)
	}
}
