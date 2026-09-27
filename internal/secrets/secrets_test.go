package secrets

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain swaps in go-keyring's in-memory provider for the whole package.
//
// Without it these tests read the developer's real keychain, which made them
// pass only until wotctx was actually used on the machine: once a credential
// was stored for real, the env-fallback test started failing because the
// keychain legitimately took priority. Tests must not depend on machine state,
// and must never write to a real keychain either.
func TestMain(m *testing.M) {
	keyring.MockInit()
	m.Run()
}

func TestEveryKeyHasAnEnvVar(t *testing.T) {
	seen := map[string]Key{}
	for _, k := range All() {
		name := k.envVar()
		if name == "" {
			t.Errorf("key %s has no environment variable", k)
			continue
		}
		if other, dup := seen[name]; dup {
			t.Errorf("keys %s and %s share the environment variable %s", other, k, name)
		}
		seen[name] = k
	}
}

// TestEnvVarNamesMatchSpec pins the names documented in docs/spec.md section 11.
// Renaming one silently would break a working setup.
func TestEnvVarNamesMatchSpec(t *testing.T) {
	want := map[Key]string{
		WGApplicationID: "WOTCTX_WG_APPLICATION_ID",
		WGAccessToken:   "WOTCTX_WG_ACCESS_TOKEN",
		WGTokenExpiry:   "WOTCTX_WG_TOKEN_EXPIRES_AT",
	}
	for k, name := range want {
		if got := k.envVar(); got != name {
			t.Errorf("%s env var = %q, want %q", k, got, name)
		}
	}
}

func TestKeyringFallsBackToEnv(t *testing.T) {
	// Setenv restores the previous value when the test ends.
	t.Setenv(WGApplicationID.envVar(), fakeAppID)

	// The mock keychain is empty, so Get must find the environment value and
	// say where it came from.
	value, source, err := Keyring{}.Get(WGApplicationID)
	if err != nil {
		t.Fatalf("Get = %v, want the env fallback to succeed", err)
	}
	// Compared rather than printed: a failure message must not echo a
	// credential, even a fake one, because the same message would run against
	// a real value.
	if value != fakeAppID {
		t.Error("Get did not return the env value")
	}
	if source != SourceEnv {
		t.Errorf("source = %q, want %q", source, SourceEnv)
	}
}

// TestKeyringPrefersTheKeychainOverEnv pins the precedence documented in
// docs/spec.md section 11, and is the case that the previous version of
// TestKeyringFallsBackToEnv was accidentally exercising.
func TestKeyringPrefersTheKeychainOverEnv(t *testing.T) {
	t.Setenv(WGAccessToken.envVar(), "token-from-the-environment")

	if err := (Keyring{}).Set(WGAccessToken, fakeToken); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Cleanup(func() { Keyring{}.Delete(WGAccessToken) })

	value, source, err := Keyring{}.Get(WGAccessToken)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if value != fakeToken {
		t.Error("Get returned the env value while the keychain had one")
	}
	if source != SourceKeyring {
		t.Errorf("source = %q, want %q", source, SourceKeyring)
	}
}

// TestKeyringDeleteReportsALingeringEnvValue: reporting success while the
// credential is still reachable would be a lie.
func TestKeyringDeleteReportsALingeringEnvValue(t *testing.T) {
	t.Setenv(WGAccessToken.envVar(), fakeToken)

	if err := (Keyring{}).Set(WGAccessToken, fakeToken); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := (Keyring{}).Delete(WGAccessToken); err == nil {
		t.Error("Delete = nil while the environment still supplies the value")
	}
}

func TestKeyringTreatsEmptyEnvAsMissing(t *testing.T) {
	// An empty value would read back as present and make doctor claim a
	// credential is configured when it is not.
	t.Setenv(WGApplicationID.envVar(), "")

	if _, source, err := (Keyring{}).Get(WGApplicationID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get = (%q, %v), want ErrNotFound", source, err)
	}
}

func TestMemoryStoreRoundTrips(t *testing.T) {
	m := NewMemory()

	if _, _, err := m.Get(WGAccessToken); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get on empty store = %v, want ErrNotFound", err)
	}
	if err := m.Set(WGAccessToken, fakeToken); err != nil {
		t.Fatalf("Set: %v", err)
	}
	value, source, err := m.Get(WGAccessToken)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if value != fakeToken {
		t.Errorf("Get = %q, want %q", value, fakeToken)
	}
	if source != SourceKeyring {
		t.Errorf("source = %q, want %q", source, SourceKeyring)
	}
	if err := m.Delete(WGAccessToken); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := m.Get(WGAccessToken); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestStoresRejectEmptyValues(t *testing.T) {
	// An empty value would read back as "missing" and make doctor lie about
	// whether a credential is configured.
	if err := NewMemory().Set(WGApplicationID, ""); err == nil {
		t.Error("Memory.Set(\"\") = nil, want an error")
	}
	if err := (Keyring{}).Set(WGApplicationID, ""); err == nil {
		t.Error("Keyring.Set(\"\") = nil, want an error")
	}
}

// TestMemoryStoreIgnoresEnv keeps tests independent of the developer machine.
func TestMemoryStoreIgnoresEnv(t *testing.T) {
	t.Setenv(WGApplicationID.envVar(), fakeAppID)

	if _, _, err := NewMemory().Get(WGApplicationID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Memory.Get consulted the environment: %v", err)
	}
}
