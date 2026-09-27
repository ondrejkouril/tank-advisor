package wg

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

var authNow = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

func TestLoginURLAsksForNoFollow(t *testing.T) {
	var query url.Values
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Write(fixture(t, "auth-login.json"))
	})

	got, err := client.LoginURL(context.Background(), "http://127.0.0.1:8731/callback", 14*24*time.Hour)
	if err != nil {
		t.Fatalf("LoginURL: %v", err)
	}

	// nofollow is what makes this flow work from a terminal: the API hands back
	// the destination instead of redirecting.
	if query.Get("nofollow") != "1" {
		t.Errorf("nofollow = %q, want 1", query.Get("nofollow"))
	}
	if query.Get("redirect_uri") != "http://127.0.0.1:8731/callback" {
		t.Errorf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Get("expires_at") != "1209600" {
		t.Errorf("expires_at = %q, want 1209600 seconds", query.Get("expires_at"))
	}
	if got == "" {
		t.Error("LoginURL returned an empty location")
	}
}

func TestLoginURLRejectsAnEmptyLocation(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":"ok","data":{}}`))
	})

	if _, err := client.LoginURL(context.Background(), "http://127.0.0.1:8731/callback", 0); err == nil {
		t.Error("LoginURL = nil, want an error when no location came back")
	}
}

// serveAuthLikeWargaming answers as the live auth/ methods do: access_token
// counts only in a POST body, and a token sent in the query is not there at
// all (ACCESS_TOKEN_NOT_SPECIFIED, checked 2026-09-27).
func serveAuthLikeWargaming(t *testing.T, wantToken string, answer []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Write([]byte(`{"status":"error","error":{"field":"access_token","message":"ACCESS_TOKEN_NOT_SPECIFIED","code":402,"value":null}}`))
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("access_token"); got != wantToken {
			t.Errorf("access_token in the body = %q, want %q", got, wantToken)
		}
		if r.PostForm.Get("application_id") != testAppID {
			t.Error("application_id missing from the body")
		}
		if r.URL.RawQuery != "" {
			t.Errorf("the token went into the URL: %s", r.URL.RawQuery)
		}
		w.Write(answer)
	}
}

func TestProlongateReturnsTheReplacementToken(t *testing.T) {
	client := newTestClient(t, serveAuthLikeWargaming(t, "old-token", fixture(t, "auth-prolongate.json")))

	token, err := client.Prolongate(context.Background(), "old-token", 14*24*time.Hour)
	if err != nil {
		t.Fatalf("Prolongate: %v", err)
	}
	if token.AccessToken == "" {
		t.Error("no access token in the result")
	}
	if token.AccountID != 512345678 {
		t.Errorf("account_id = %d", token.AccountID)
	}
	if token.Nickname != "example_player" {
		t.Errorf("nickname = %q", token.Nickname)
	}
	if token.ExpiresAt.IsZero() {
		t.Error("expires_at was not parsed")
	}
}

func TestProlongateFailsOnAnExpiredToken(t *testing.T) {
	client := newTestClient(t, serveFixture(t, "error-invalid-access-token.json"))

	_, err := client.Prolongate(context.Background(), "stale", 0)
	if err == nil {
		t.Fatal("Prolongate = nil, want an error")
	}
	if !IsAuthError(err) {
		t.Errorf("IsAuthError = false for %v; the caller needs to know to re-login", err)
	}
}

func TestTokenValidity(t *testing.T) {
	cases := []struct {
		name  string
		token Token
		want  bool
	}{
		{"fresh", Token{AccessToken: "t", ExpiresAt: authNow.Add(time.Hour)}, true},
		{"expired", Token{AccessToken: "t", ExpiresAt: authNow.Add(-time.Hour)}, false},
		{"no token", Token{ExpiresAt: authNow.Add(time.Hour)}, false},
		{"no expiry", Token{AccessToken: "t"}, false},
	}
	for _, tc := range cases {
		if got := tc.token.Valid(authNow); got != tc.want {
			t.Errorf("%s: Valid = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestNeedsProlongation covers the renew-early rule: discovering an expired
// token part-way through a sync is worse than renewing one that had days left.
func TestNeedsProlongation(t *testing.T) {
	const window = 3 * 24 * time.Hour

	cases := []struct {
		name  string
		token Token
		want  bool
	}{
		{"plenty of time", Token{AccessToken: "t", ExpiresAt: authNow.Add(10 * 24 * time.Hour)}, false},
		{"inside the window", Token{AccessToken: "t", ExpiresAt: authNow.Add(2 * 24 * time.Hour)}, true},
		{"already expired", Token{AccessToken: "t", ExpiresAt: authNow.Add(-time.Hour)}, true},
		{"no token at all", Token{}, false},
	}
	for _, tc := range cases {
		if got := tc.token.NeedsProlongation(authNow, window); got != tc.want {
			t.Errorf("%s: NeedsProlongation = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTokenFromCallback(t *testing.T) {
	query := url.Values{}
	query.Set("status", "ok")
	query.Set("access_token", "callback-token")
	query.Set("nickname", "example_player")
	query.Set("account_id", "512345678")
	query.Set("expires_at", "1790000000")

	token, err := TokenFromCallback(query)
	if err != nil {
		t.Fatalf("TokenFromCallback: %v", err)
	}
	if token.AccessToken != "callback-token" {
		t.Errorf("access token = %q", token.AccessToken)
	}
	if token.AccountID != 512345678 {
		t.Errorf("account_id = %d", token.AccountID)
	}
	if token.Nickname != "example_player" {
		t.Errorf("nickname = %q", token.Nickname)
	}
	if want := time.Unix(1790000000, 0).UTC(); !token.ExpiresAt.Equal(want) {
		t.Errorf("expires_at = %s, want %s", token.ExpiresAt, want)
	}
}

// TestTokenFromCallbackReportsLoginFailure: a declined login comes back through
// the same redirect, so this is where it becomes a user-facing error.
func TestTokenFromCallbackReportsLoginFailure(t *testing.T) {
	query := url.Values{}
	query.Set("status", "error")
	query.Set("code", "406")
	query.Set("message", "AUTH_CANCEL")

	_, err := TokenFromCallback(query)
	if err == nil {
		t.Fatal("TokenFromCallback = nil, want an error")
	}
	wgErr, ok := AsError(err)
	if !ok {
		t.Fatalf("error %v is not a *wg.Error", err)
	}
	if wgErr.Message != "AUTH_CANCEL" || wgErr.Code != 406 {
		t.Errorf("error = %+v, want AUTH_CANCEL/406", wgErr)
	}
}

func TestTokenFromCallbackRejectsMalformedInput(t *testing.T) {
	cases := map[string]url.Values{
		"no status":         {"access_token": {"t"}},
		"unknown status":    {"status": {"maybe"}},
		"no token":          {"status": {"ok"}, "nickname": {"example_player"}},
		"bad account_id":    {"status": {"ok"}, "access_token": {"t"}, "account_id": {"not-a-number"}},
		"bad expires_at":    {"status": {"ok"}, "access_token": {"t"}, "expires_at": {"soon"}},
		"error with no msg": {"status": {"error"}},
	}
	for name, query := range cases {
		if _, err := TokenFromCallback(query); err == nil {
			t.Errorf("TokenFromCallback(%s) = nil, want an error", name)
		}
	}
}

func TestLogoutSendsTheTokenInABody(t *testing.T) {
	client := newTestClient(t, serveAuthLikeWargaming(t, "the-token", []byte(`{"status":"ok","data":{}}`)))
	if err := client.Logout(context.Background(), "the-token"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
}
