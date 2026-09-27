// Package secrets stores API credentials in the OS keychain and keeps them out
// of everything else wotctx writes.
//
// Two rules shape this package. Values are never returned by any function whose
// output reaches stdout, stderr or the database: callers get a value only when
// they are about to put it in an HTTP request. And every read reports where the
// value came from, so `wotctx doctor` can explain itself without printing
// anything sensitive.
package secrets

import (
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

// service is the keychain service name under which every key is stored.
const service = "wotctx"

// Key names one stored credential.
type Key string

const (
	WGApplicationID Key = "wg_application_id"
	WGAccessToken   Key = "wg_access_token"
	WGTokenExpiry   Key = "wg_token_expires_at"
)

// All returns every key, in the order doctor should report them.
func All() []Key {
	return []Key{WGApplicationID, WGAccessToken, WGTokenExpiry}
}

// envVar maps a key to the environment variable that can supply it. The env is
// a fallback for headless use and CI; the keychain wins when both are present.
func (k Key) envVar() string {
	switch k {
	case WGApplicationID:
		return "WOTCTX_WG_APPLICATION_ID"
	case WGAccessToken:
		return "WOTCTX_WG_ACCESS_TOKEN"
	case WGTokenExpiry:
		return "WOTCTX_WG_TOKEN_EXPIRES_AT"
	default:
		return ""
	}
}

// Source records where a value was found.
type Source string

const (
	SourceMissing Source = "missing"
	SourceKeyring Source = "keyring"
	SourceEnv     Source = "env"
)

// ErrNotFound reports that a key is set in neither the keychain nor the
// environment.
var ErrNotFound = errors.New("secret not found")

// Store reads and writes credentials.
type Store interface {
	// Get returns the value and where it came from. A missing key returns
	// ErrNotFound with SourceMissing.
	Get(k Key) (string, Source, error)
	Set(k Key, value string) error
	Delete(k Key) error
}

// Keyring is the production Store: the OS keychain, with the environment as a
// read-only fallback.
type Keyring struct{}

// New returns the default Store.
func New() Store { return Keyring{} }

// Get implements Store.
func (Keyring) Get(k Key) (string, Source, error) {
	value, err := keyring.Get(service, string(k))
	switch {
	case err == nil && value != "":
		return value, SourceKeyring, nil
	case err != nil && !errors.Is(err, keyring.ErrNotFound):
		// The keychain exists but is unusable: locked, or no secret-service on a
		// headless Linux box. Fall back to the environment rather than failing,
		// but say so if that turns up nothing either.
		if fromEnv, ok := lookupEnv(k); ok {
			return fromEnv, SourceEnv, nil
		}
		return "", SourceMissing, fmt.Errorf("reading %s from keychain: %w", k, err)
	}

	if fromEnv, ok := lookupEnv(k); ok {
		return fromEnv, SourceEnv, nil
	}
	return "", SourceMissing, fmt.Errorf("%s: %w", k, ErrNotFound)
}

// Set implements Store. It writes to the keychain only; the environment
// fallback is deliberately read-only.
func (Keyring) Set(k Key, value string) error {
	if value == "" {
		return fmt.Errorf("refusing to store an empty value for %s", k)
	}
	if err := keyring.Set(service, string(k), value); err != nil {
		return fmt.Errorf("writing %s to keychain: %w", k, err)
	}
	return nil
}

// Delete implements Store. A key that is only set in the environment cannot be
// deleted here, and Delete says so rather than reporting a false success.
func (Keyring) Delete(k Key) error {
	err := keyring.Delete(service, string(k))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("deleting %s from keychain: %w", k, err)
	}
	if _, ok := lookupEnv(k); ok {
		return fmt.Errorf("%s removed from the keychain but %s is still set in the environment", k, k.envVar())
	}
	return nil
}

func lookupEnv(k Key) (string, bool) {
	name := k.envVar()
	if name == "" {
		return "", false
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return "", false
	}
	return value, true
}

// Memory is an in-memory Store for tests. It does not consult the environment,
// so tests stay independent of the machine they run on.
type Memory struct {
	values map[Key]string
}

// NewMemory returns an empty in-memory Store.
func NewMemory() *Memory {
	return &Memory{values: map[Key]string{}}
}

// Get implements Store.
func (m *Memory) Get(k Key) (string, Source, error) {
	if v, ok := m.values[k]; ok && v != "" {
		return v, SourceKeyring, nil
	}
	return "", SourceMissing, fmt.Errorf("%s: %w", k, ErrNotFound)
}

// Set implements Store.
func (m *Memory) Set(k Key, value string) error {
	if value == "" {
		return fmt.Errorf("refusing to store an empty value for %s", k)
	}
	m.values[k] = value
	return nil
}

// Delete implements Store.
func (m *Memory) Delete(k Key) error {
	delete(m.values, k)
	return nil
}
