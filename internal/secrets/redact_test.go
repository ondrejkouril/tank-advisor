package secrets

import (
	"errors"
	"strings"
	"testing"
)

// Representative credential shapes. These are invented values, not real ones.
const (
	fakeAppID = "deadbeefcafebabe0123456789abcdef"
	fakeToken = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4"
	fakeTmgg  = "tmgg_9zXq-pLm3_R7tKd2VbN8"
)

// TestRedactorRemovesEveryCredentialShape pins the plan step 2 acceptance
// criterion: an application_id, an access token and a tmgg_ key must all be
// scrubbed from log output.
func TestRedactorRemovesEveryCredentialShape(t *testing.T) {
	r := NewRedactor(fakeAppID, fakeToken, fakeTmgg)

	cases := []struct {
		name string
		in   string
	}{
		{
			name: "wg request url",
			in:   "GET https://api.worldoftanks.eu/wot/account/info/?application_id=" + fakeAppID + "&access_token=" + fakeToken,
		},
		{
			name: "tomato header dump",
			in:   `x-api-key: ` + fakeTmgg,
		},
		{
			name: "bare values in prose",
			in:   "using " + fakeAppID + " with " + fakeToken + " and " + fakeTmgg,
		},
		{
			name: "json body",
			in:   `{"access_token":"` + fakeToken + `","application_id":"` + fakeAppID + `"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Redact(tc.in)
			for _, secret := range []string{fakeAppID, fakeToken, fakeTmgg} {
				if strings.Contains(got, secret) {
					t.Errorf("redacted output still contains a credential:\n%s", got)
				}
			}
			if !strings.Contains(got, Placeholder) {
				t.Errorf("nothing was redacted from %q; got %q", tc.in, got)
			}
		})
	}
}

// TestRedactorCatchesUnregisteredCredentials covers the case the exact-value
// path cannot: a credential this process never loaded, leaking through an error
// message from somewhere else.
func TestRedactorCatchesUnregisteredCredentials(t *testing.T) {
	r := NewRedactor() // knows no values at all

	for _, in := range []string{
		"application_id=" + fakeAppID,
		"access_token=" + fakeToken,
		"key " + fakeTmgg + " rejected",
		"id " + fakeAppID,
	} {
		got := r.Redact(in)
		if strings.Contains(got, fakeAppID) || strings.Contains(got, fakeToken) || strings.Contains(got, fakeTmgg) {
			t.Errorf("Redact(%q) = %q, still contains a credential", in, got)
		}
	}
}

// TestRedactedURLStaysDiagnosable checks that redaction does not destroy the
// information needed to debug a failed request.
func TestRedactedURLStaysDiagnosable(t *testing.T) {
	r := NewRedactor(fakeAppID)
	got := r.Redact("GET https://api.worldoftanks.eu/wot/tanks/stats/?application_id=" + fakeAppID + "&extra=random")

	for _, want := range []string{"api.worldoftanks.eu", "tanks/stats", "application_id=", "extra=random"} {
		if !strings.Contains(got, want) {
			t.Errorf("redaction removed %q, leaving %q", want, got)
		}
	}
}

func TestRedactorLeavesOrdinaryTextAlone(t *testing.T) {
	r := NewRedactor(fakeAppID)

	// Short hex, decimal ids and tank names must survive: they show up in every
	// line of real output.
	for _, in := range []string{
		"tank_id=19281 tier=11 nation=uk",
		"synced 742 vehicles in 1.4s",
		"commit 0b78bde",
		"TVP T 50/51",
	} {
		if got := r.Redact(in); got != in {
			t.Errorf("Redact(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestRedactorIgnoresShortValues(t *testing.T) {
	// A short secret would turn into a substring match that mangles unrelated
	// output, so NewRedactor drops it.
	r := NewRedactor("eu")
	if got := r.Redact("realm eu"); got != "realm eu" {
		t.Errorf("Redact = %q, want %q", got, "realm eu")
	}
}

func TestRedactErrorHandlesNil(t *testing.T) {
	r := NewRedactor()
	if got := r.RedactError(nil); got != "" {
		t.Errorf("RedactError(nil) = %q, want empty", got)
	}
	if got := r.RedactError(errors.New("boom " + fakeTmgg)); strings.Contains(got, fakeTmgg) {
		t.Errorf("RedactError leaked a credential: %q", got)
	}
}

func TestFromStorePreloadsStoredValues(t *testing.T) {
	m := NewMemory()
	if err := m.Set(WGApplicationID, fakeAppID); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := m.Set(WGAccessToken, fakeToken); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := FromStore(m).Redact("app " + fakeAppID + " token " + fakeToken)
	if strings.Contains(got, fakeAppID) || strings.Contains(got, fakeToken) {
		t.Errorf("FromStore redactor leaked a credential: %q", got)
	}
}

// TestTmggPatternIsRetained keeps the Tomato.gg key pattern in the redactor
// even though wotctx no longer stores one. It costs nothing, and a key could
// still reach a log through a pasted URL or an error from elsewhere - the
// pattern path exists precisely for credentials this process never loaded.
func TestTmggPatternIsRetained(t *testing.T) {
	if got := NewRedactor().Redact("x-api-key: " + fakeTmgg); strings.Contains(got, fakeTmgg) {
		t.Errorf("Redact leaked a tmgg_ key: %q", got)
	}
}
