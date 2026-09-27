package wg

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testAppID = "deadbeefcafebabe0123456789abcdef"

func fixture(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "wg", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return raw
}

// newTestClient wires a Client to a handler, with waiting disabled so tests do
// not spend real time in backoff or rate limiting.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New("eu", testAppID,
		WithBaseURL(server.URL),
		WithSleeper(func(context.Context, time.Duration) error { return nil }),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

// serveFixture answers every request with one fixture at HTTP 200, which is how
// Wargaming replies even to failures.
func serveFixture(t *testing.T, name string) http.HandlerFunc {
	body := fixture(t, name)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	if _, err := New("eu", ""); err == nil {
		t.Error("New with no application_id = nil, want an error")
	}
	if _, err := New("moon", testAppID); err == nil {
		t.Error("New with an unknown realm = nil, want an error")
	}
}

func TestBaseURLForRealm(t *testing.T) {
	for realm, want := range map[string]string{
		"eu":   "https://api.worldoftanks.eu/wot",
		"com":  "https://api.worldoftanks.com/wot",
		"asia": "https://api.worldoftanks.asia/wot",
	} {
		got, err := BaseURLForRealm(realm)
		if err != nil {
			t.Errorf("BaseURLForRealm(%q): %v", realm, err)
		}
		if got != want {
			t.Errorf("BaseURLForRealm(%q) = %q, want %q", realm, got, want)
		}
	}
}

func TestGetSendsApplicationIDAndParams(t *testing.T) {
	var got url.Values
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Write(fixture(t, "account-info-minimal.json"))
	})

	params := url.Values{}
	params.Set("account_id", "512345678")
	params.Set("extra", "private.garage,statistics.random")

	if _, err := client.Get(context.Background(), "account/info", params, ""); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Get("application_id") != testAppID {
		t.Errorf("application_id = %q, want it set from the client", got.Get("application_id"))
	}
	if got.Get("account_id") != "512345678" {
		t.Errorf("account_id = %q", got.Get("account_id"))
	}
	if got.Get("extra") != "private.garage,statistics.random" {
		t.Errorf("extra = %q", got.Get("extra"))
	}
}

// TestGetParsesSuccessfulEnvelope checks that Raw is kept verbatim: a parser
// change must never require another request.
func TestGetParsesSuccessfulEnvelope(t *testing.T) {
	body := fixture(t, "account-info-minimal.json")
	client := newTestClient(t, serveFixture(t, "account-info-minimal.json"))

	result, err := client.Get(context.Background(), "account/info", nil, "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !result.OK() {
		t.Error("OK() = false for a successful response")
	}
	if string(result.Raw) != string(body) {
		t.Error("Raw is not the verbatim body")
	}
	if len(result.Data) == 0 {
		t.Error("Data is empty")
	}

	var data map[string]struct {
		Nickname string `json:"nickname"`
		Private  struct {
			Credits int   `json:"credits"`
			Garage  []int `json:"garage"`
		} `json:"private"`
	}
	if err := json.Unmarshal(result.Data, &data); err != nil {
		t.Fatalf("parsing Data: %v", err)
	}
	account := data["512345678"]
	if account.Nickname != "example_player" {
		t.Errorf("nickname = %q", account.Nickname)
	}
	if account.Private.Credits != 42150000 {
		t.Errorf("credits = %d", account.Private.Credits)
	}
	if len(account.Private.Garage) != 3 {
		t.Errorf("garage = %v, want 3 entries", account.Private.Garage)
	}
}

// TestErrorBodiesArriveAsHTTP200 is the plan step 3 acceptance criterion, and
// the single most important behaviour in this package: Wargaming reports
// failures with a 200, so the body decides.
func TestErrorBodiesArriveAsHTTP200(t *testing.T) {
	cases := []struct {
		fixture       string
		wantMessage   string
		wantCode      int
		isAppCred     bool
		isAuth        bool
		wantRetryable bool
	}{
		{"error-invalid-application-id.json", msgInvalidApplicationID, 407, true, false, false},
		{"error-demo-blocked.json", msgDemoIsBlocked, 407, true, false, false},
		{"error-invalid-access-token.json", msgInvalidAccessToken, 407, false, true, false},
		{"error-source-not-available.json", msgSourceNotAvailable, 504, false, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			client := newTestClient(t, serveFixture(t, tc.fixture))

			result, err := client.Get(context.Background(), "account/info", nil, "")
			if err == nil {
				t.Fatal("Get = nil, want an error from the 200 body")
			}

			wgErr, ok := AsError(err)
			if !ok {
				t.Fatalf("error %v is not a *wg.Error", err)
			}
			if wgErr.Message != tc.wantMessage {
				t.Errorf("message = %q, want %q", wgErr.Message, tc.wantMessage)
			}
			if wgErr.Code != tc.wantCode {
				t.Errorf("code = %d, want %d", wgErr.Code, tc.wantCode)
			}
			if wgErr.IsAppCredential() != tc.isAppCred {
				t.Errorf("IsAppCredential = %v, want %v", wgErr.IsAppCredential(), tc.isAppCred)
			}
			if wgErr.IsAuth() != tc.isAuth {
				t.Errorf("IsAuth = %v, want %v", wgErr.IsAuth(), tc.isAuth)
			}
			if wgErr.Retryable() != tc.wantRetryable {
				t.Errorf("Retryable = %v, want %v", wgErr.Retryable(), tc.wantRetryable)
			}

			// The result still carries the transport facts, so the caller can
			// record the failed attempt as a snapshot.
			if result.HTTPStatus != http.StatusOK {
				t.Errorf("HTTPStatus = %d, want 200 - the failure was in the body", result.HTTPStatus)
			}
			if result.WGError != tc.wantMessage {
				t.Errorf("result.WGError = %q, want %q", result.WGError, tc.wantMessage)
			}
			if result.OK() {
				t.Error("OK() = true for an error body")
			}
		})
	}
}

// TestNotModifiedIsSuccess: a 304 means the cached copy is still good, which is
// the point of sending the ETag.
func TestNotModifiedIsSuccess(t *testing.T) {
	const etag = `W/"vehicles-v7"`

	var sent string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		sent = r.Header.Get("If-None-Match")
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
	})

	result, err := client.Get(context.Background(), "encyclopedia/vehicles", nil, etag)
	if err != nil {
		t.Fatalf("Get = %v, want nil for a 304", err)
	}
	if sent != etag {
		t.Errorf("If-None-Match = %q, want %q", sent, etag)
	}
	if !result.NotModified {
		t.Error("NotModified = false for a 304")
	}
	if result.HTTPStatus != http.StatusNotModified {
		t.Errorf("HTTPStatus = %d, want 304", result.HTTPStatus)
	}
	if len(result.Raw) != 0 {
		t.Error("a 304 carried a body")
	}
	if result.OK() {
		t.Error("OK() = true for a 304; there is no payload to use")
	}
	if result.ETag != etag {
		t.Errorf("ETag = %q, want it preserved", result.ETag)
	}
}

func TestETagIsCapturedFromSuccess(t *testing.T) {
	const etag = `W/"abc"`
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", etag)
		w.Write(fixture(t, "account-info-minimal.json"))
	})

	result, err := client.Get(context.Background(), "account/info", nil, "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if result.ETag != etag {
		t.Errorf("ETag = %q, want %q", result.ETag, etag)
	}
}

// TestRetriesTransientFailures covers both retryable shapes: a 5xx, and a
// Wargaming error body that says the source is temporarily unavailable.
func TestRetriesTransientFailures(t *testing.T) {
	t.Run("http 500 then success", func(t *testing.T) {
		var calls atomic.Int32
		client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("upstream is unhappy"))
				return
			}
			w.Write(fixture(t, "account-info-minimal.json"))
		})

		result, err := client.Get(context.Background(), "account/info", nil, "")
		if err != nil {
			t.Fatalf("Get = %v, want the retry to succeed", err)
		}
		if !result.OK() {
			t.Error("OK() = false after a successful retry")
		}
		if got := calls.Load(); got != 2 {
			t.Errorf("made %d calls, want 2", got)
		}
	})

	t.Run("source not available exhausts attempts", func(t *testing.T) {
		var calls atomic.Int32
		client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Write(fixture(t, "error-source-not-available.json"))
		})

		if _, err := client.Get(context.Background(), "account/info", nil, ""); err == nil {
			t.Fatal("Get = nil, want an error after exhausting attempts")
		}
		if got := calls.Load(); got != maxAttempts {
			t.Errorf("made %d calls, want %d", got, maxAttempts)
		}
	})
}

// TestDoesNotRetryPermanentFailures: retrying a bad application_id just burns
// the rate limit.
func TestDoesNotRetryPermanentFailures(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Write(fixture(t, "error-invalid-application-id.json"))
	})

	if _, err := client.Get(context.Background(), "account/info", nil, ""); err == nil {
		t.Fatal("Get = nil, want an error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("made %d calls, want 1 - a permanent failure must not be retried", got)
	}
}

func TestRejectsMalformedJSON(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":"ok","data":`))
	})

	if _, err := client.Get(context.Background(), "account/info", nil, ""); err == nil {
		t.Error("Get on truncated JSON = nil, want an error")
	}
}

func TestRejectsUnexpectedStatus(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":"something-new"}`))
	})

	if _, err := client.Get(context.Background(), "account/info", nil, ""); err == nil {
		t.Error("Get on an unknown status = nil, want an error")
	}
}

func TestContextCancellationStops(t *testing.T) {
	client := newTestClient(t, serveFixture(t, "account-info-minimal.json"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := client.Get(ctx, "account/info", nil, ""); err == nil {
		t.Error("Get with a cancelled context = nil, want an error")
	}
}

// TestLimiterSpacesRequests drives the limiter against a fake clock, so the
// spacing is asserted rather than timed.
func TestLimiterSpacesRequests(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	var slept []time.Duration

	l := newLimiter(5) // 200ms apart
	l.now = func() time.Time { return now }
	l.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}

	ctx := context.Background()
	for range 3 {
		if err := l.wait(ctx); err != nil {
			t.Fatalf("wait: %v", err)
		}
	}

	// The first request goes immediately; the next two wait a widening gap,
	// because the clock is frozen and the limiter is booking future slots.
	if len(slept) != 2 {
		t.Fatalf("slept %d times, want 2: %v", len(slept), slept)
	}
	if slept[0] != 200*time.Millisecond {
		t.Errorf("second request waited %s, want 200ms", slept[0])
	}
	if slept[1] != 400*time.Millisecond {
		t.Errorf("third request waited %s, want 400ms", slept[1])
	}
}

func TestLimiterIsDisabledAtZero(t *testing.T) {
	l := newLimiter(0)
	l.sleep = func(context.Context, time.Duration) error {
		t.Error("limiter slept when disabled")
		return nil
	}
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestDefaultRateLimitStaysUnderTheDocumentedCeiling(t *testing.T) {
	// Wargaming documents 10 requests/second per IP for a standalone app.
	if defaultRequestsPerSecond > 10 {
		t.Fatalf("default rate %v exceeds the documented limit", defaultRequestsPerSecond)
	}
	if defaultRequestsPerSecond > 5 {
		t.Errorf("default rate %v leaves no headroom for the game client", defaultRequestsPerSecond)
	}
}

// TestEndpointPathIsBuiltCorrectly guards the trailing slash Wargaming expects.
func TestEndpointPathIsBuiltCorrectly(t *testing.T) {
	var path string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Write(fixture(t, "account-info-minimal.json"))
	})

	if _, err := client.Get(context.Background(), "encyclopedia/vehicles", nil, ""); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.HasSuffix(path, "/encyclopedia/vehicles/") {
		t.Errorf("request path = %q, want it to end with /encyclopedia/vehicles/", path)
	}
}

// TestAccountInfoAsksOnlyForWhatIsRead holds the data-minimisation rule of
// docs/spec-desktop.md section 12.3: named fields only, the extra blocks among
// them, and never another player's data.
func TestAccountInfoAsksOnlyForWhatIsRead(t *testing.T) {
	var got url.Values
	var agent string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got, agent = r.URL.Query(), r.Header.Get("User-Agent")
		w.Write(fixture(t, "account-info-minimal.json"))
	})
	if _, err := client.AccountInfo(context.Background(), 512345678, ""); err != nil {
		t.Fatalf("AccountInfo: %v", err)
	}

	fields := strings.Split(got.Get("fields"), ",")
	if len(fields) < 2 {
		t.Fatalf("fields = %q, want a list", got.Get("fields"))
	}
	have := map[string]bool{}
	for _, f := range fields {
		have[f] = true
	}
	for _, extra := range accountExtras {
		if !have[extra] {
			t.Errorf("extra %s is requested but not in fields, so it would be dropped", extra)
		}
	}
	for _, banned := range []string{"private.grouped_contacts", "private.ban_info", "private.is_bound_to_phone", "private.restrictions"} {
		if have[banned] || strings.Contains(got.Get("extra"), banned) {
			t.Errorf("%s is requested", banned)
		}
	}
	if !strings.Contains(agent, "github.com/ondrejkouril/tank-advisor") {
		t.Errorf("User-Agent %q does not name the project", agent)
	}
}
