package cli

import (
	"context"
	"time"
)

// What the Tank Advisor app reads from here (docs/spec-desktop.md section 5.2).
// The app is `doctor` with buttons: it shows these checks and runs the same
// commands through Run, so no logic lives twice.

// Check is one line of doctor's report.
type Check = check

// Check statuses, as doctor reports them.
const (
	StatusOK      = statusOK
	StatusWarn    = statusWarn
	StatusFail    = statusFail
	StatusUnknown = statusUnknown
)

// Checks runs every doctor check.
func Checks(ctx context.Context, env *Env) []Check {
	return collectChecks(ctx, env)
}

// Login is the stored Wargaming login, without the token itself.
type Login struct {
	Stored    bool
	ExpiresAt time.Time // zero when the expiry is unknown
	// Valid is true while the token has not expired, which is also when
	// auth/prolongate can still extend it.
	Valid bool
}

// StoredLogin reports the login in the keychain.
func StoredLogin(env *Env) Login {
	if env.Secrets == nil {
		return Login{}
	}
	token, err := loadToken(env)
	if err != nil {
		return Login{}
	}
	return Login{Stored: true, ExpiresAt: token.ExpiresAt, Valid: token.Valid(env.now())}
}

// ModDumpPath is where the client mod writes its dump.
func ModDumpPath(env *Env) string {
	return env.modDumpPath()
}
