package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/secrets"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

func TestAuthWGNeedsAnApplicationID(t *testing.T) {
	env, _ := newTestEnv(t) // empty secret store

	err := Run(context.Background(), env, []string{"auth", "wg"})
	if err == nil {
		t.Fatal("auth wg with no application_id = nil, want an error")
	}
	// The message has to be actionable: registering the app is a manual step
	// the user cannot guess.
	for _, want := range []string{"developers.wargaming.net", "--application-id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestAuthTomatoIsGone: the Tomato.gg API requires a paid subscription to
// issue a key, so the command was removed rather than left to send the user to
// a dead end. It must read as an unknown command, not as a broken one.
func TestAuthTomatoIsGone(t *testing.T) {
	env, buf := newTestEnv(t)

	err := Run(context.Background(), env, []string{"auth", "tomato"})
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("auth tomato = %v, want ErrUsage", err)
	}
	if !strings.Contains(buf.String(), "unknown command") {
		t.Errorf("expected an unknown-command message; got:\n%s", buf.String())
	}
}

func TestTokenRoundTripsThroughTheSecretStore(t *testing.T) {
	env, _ := newTestEnv(t)

	want := wg.Token{
		AccessToken: fakeToken,
		AccountID:   env.Config.Account.AccountID,
		Nickname:    env.Config.Account.Nickname,
		ExpiresAt:   time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
	if err := storeToken(env, want); err != nil {
		t.Fatalf("storeToken: %v", err)
	}

	got, err := loadToken(env)
	if err != nil {
		t.Fatalf("loadToken: %v", err)
	}
	if got.AccessToken != want.AccessToken {
		t.Errorf("access token did not round-trip")
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("expiry = %s, want %s", got.ExpiresAt, want.ExpiresAt)
	}
}

// TestLoadTokenWithoutExpiryIsTreatedAsUnverifiable: a token whose expiry is
// unknown cannot be reasoned about, so it must not be silently trusted.
func TestLoadTokenWithoutExpiryIsTreatedAsUnverifiable(t *testing.T) {
	env, _ := newTestEnv(t)
	if err := env.Secrets.Set(secrets.WGAccessToken, fakeToken); err != nil {
		t.Fatalf("Set: %v", err)
	}

	token, err := loadToken(env)
	if err != nil {
		t.Fatalf("loadToken: %v", err)
	}
	if !token.ExpiresAt.IsZero() {
		t.Error("an expiry appeared from nowhere")
	}
	if token.Valid(time.Now()) {
		t.Error("a token with no known expiry reported itself valid")
	}
}

func TestWGAuthCheckReportsTokenState(t *testing.T) {
	cases := []struct {
		name       string
		expiresIn  time.Duration
		storeToken bool
		want       checkStatus
		wantFix    string
	}{
		{name: "no token", storeToken: false, want: statusWarn, wantFix: "wotctx auth wg"},
		{name: "healthy", storeToken: true, expiresIn: 10 * 24 * time.Hour, want: statusOK},
		{name: "near expiry", storeToken: true, expiresIn: 2 * 24 * time.Hour, want: statusWarn, wantFix: "--prolong"},
		{name: "expired", storeToken: true, expiresIn: -time.Hour, want: statusFail, wantFix: "--relogin"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := newTestEnv(t)
			env.Clock = func() time.Time { return testNow }

			if tc.storeToken {
				if err := storeToken(env, wg.Token{
					AccessToken: fakeToken,
					ExpiresAt:   testNow.Add(tc.expiresIn),
				}); err != nil {
					t.Fatalf("storeToken: %v", err)
				}
			}

			got := checkWGAuth(env)
			if got.Status != tc.want {
				t.Errorf("status = %q, want %q (detail: %s)", got.Status, tc.want, got.Detail)
			}
			if tc.wantFix != "" && !strings.Contains(got.Fix, tc.wantFix) {
				t.Errorf("Fix = %q, want it to mention %q", got.Fix, tc.wantFix)
			}
			if strings.Contains(got.Detail, fakeToken) {
				t.Error("the wg-auth check leaked the token value")
			}
		})
	}
}

// TestProlongWindowIsShorterThanTokenLifetime guards the renewal arithmetic: a
// window at or beyond the lifetime would ask for renewal continuously.
func TestProlongWindowIsShorterThanTokenLifetime(t *testing.T) {
	if prolongWindow >= tokenLifetime {
		t.Fatalf("prolongWindow %s is not shorter than tokenLifetime %s", prolongWindow, tokenLifetime)
	}
}
