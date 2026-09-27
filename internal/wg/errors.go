package wg

import (
	"errors"
	"fmt"
)

// Error is a Wargaming API error.
//
// These arrive as HTTP 200 with a body of {"status":"error", ...}, so the
// transport status code cannot be used to decide whether a response is usable.
// Every response therefore has its body inspected before it is trusted.
type Error struct {
	Code    int
	Message string
	Field   string
	Value   string
}

func (e *Error) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("wargaming: %s (code %d, field %s)", e.Message, e.Code, e.Field)
	}
	return fmt.Sprintf("wargaming: %s (code %d)", e.Message, e.Code)
}

// Documented message values worth reacting to differently.
const (
	msgInvalidApplicationID = "INVALID_APPLICATION_ID"
	msgApplicationIsBlocked = "APPLICATION_IS_BLOCKED"
	msgDemoIsBlocked        = "DEMO_APPLICATION_IS_BLOCKED"
	msgInvalidAccessToken   = "INVALID_ACCESS_TOKEN"
	msgAccessTokenExpired   = "ACCESS_TOKEN_EXPIRED"
	msgSourceNotAvailable   = "SOURCE_NOT_AVAILABLE"
	msgRequestLimitExceeded = "REQUEST_LIMIT_EXCEEDED"
)

// IsAppCredential reports an unusable application_id. Nothing can be fetched
// until it is fixed, so callers stop rather than retry.
func (e *Error) IsAppCredential() bool {
	switch e.Message {
	case msgInvalidApplicationID, msgApplicationIsBlocked, msgDemoIsBlocked:
		return true
	}
	return false
}

// IsAuth reports an access-token problem, which a fresh login fixes.
func (e *Error) IsAuth() bool {
	switch e.Message {
	case msgInvalidAccessToken, msgAccessTokenExpired:
		return true
	}
	return false
}

// Retryable reports a transient failure worth another attempt.
func (e *Error) Retryable() bool {
	switch e.Message {
	case msgSourceNotAvailable, msgRequestLimitExceeded:
		return true
	}
	return false
}

// AsError extracts a *Error from an error chain.
func AsError(err error) (*Error, bool) {
	var wgErr *Error
	if errors.As(err, &wgErr) {
		return wgErr, true
	}
	return nil, false
}

// IsAuthError reports whether err is an access-token failure anywhere in its
// chain.
func IsAuthError(err error) bool {
	wgErr, ok := AsError(err)
	return ok && wgErr.IsAuth()
}

// IsAppCredentialError reports whether err is an application_id failure.
func IsAppCredentialError(err error) bool {
	wgErr, ok := AsError(err)
	return ok && wgErr.IsAppCredential()
}
