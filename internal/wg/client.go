// Package wg is a client for the Wargaming public API.
//
// It is hand-written rather than generated, because only about six endpoints
// matter here and because a generated wrapper would sit between wotctx and any
// field Wargaming adds - a new `extra` value, or a tier the tree did not have
// last patch.
//
// Two behaviours of this API shape the whole package. Errors arrive as HTTP 200
// with a {"status":"error"} body, so every response is inspected rather than
// trusted. And the rate limit is per IP rather than per key, so the limiter is
// a property of the client and is deliberately set below the documented ceiling.
package wg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/useragent"
)

// builtinApplicationID is the project's own standalone application id, set at
// release build time with -ldflags from a CI secret and empty in a local build
// (docs/spec-desktop.md section 12.1). It is deliberately not in the source.
var builtinApplicationID string

// BuiltinApplicationID returns the id compiled into this build, or "".
func BuiltinApplicationID() string { return builtinApplicationID }

// Documented limit is 10 requests/second per IP for a standalone application.
// Half of that leaves room for the game client and anything else sharing the
// connection, and nothing here is latency-sensitive enough to want the rest.
const defaultRequestsPerSecond = 5

// maxAttempts bounds retries of transient failures.
const maxAttempts = 3

// maxBodyBytes caps a response body. The largest real response is an
// encyclopedia page, which is well under this.
const maxBodyBytes = 32 << 20

// Client talks to one realm's API with one application_id.
type Client struct {
	baseURL string
	appID   string
	http    *http.Client
	limiter *limiter

	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithBaseURL overrides the realm base URL. Tests point this at an httptest
// server; production code should use the realm instead.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimSuffix(u, "/") }
}

// WithRequestsPerSecond overrides the self-imposed rate limit.
func WithRequestsPerSecond(rps float64) Option {
	return func(c *Client) { c.limiter = newLimiter(rps) }
}

// WithClock replaces the time source, for tests.
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		c.now = now
		c.limiter.now = now
	}
}

// WithSleeper replaces the backoff sleep, for tests that must not actually wait.
func WithSleeper(sleep func(context.Context, time.Duration) error) Option {
	return func(c *Client) {
		c.sleep = sleep
		c.limiter.sleep = sleep
	}
}

// BaseURLForRealm returns the API root for a realm.
func BaseURLForRealm(realm string) (string, error) {
	switch realm {
	case "eu", "com", "asia":
		return "https://api.worldoftanks." + realm + "/wot", nil
	default:
		return "", fmt.Errorf("unknown realm %q, want eu, com or asia", realm)
	}
}

// New returns a Client for a realm.
func New(realm, applicationID string, opts ...Option) (*Client, error) {
	if applicationID == "" {
		return nil, errors.New("wg: application_id is required")
	}
	base, err := BaseURLForRealm(realm)
	if err != nil {
		return nil, err
	}

	c := &Client{
		baseURL: base,
		appID:   applicationID,
		http:    &http.Client{Timeout: 30 * time.Second},
		limiter: newLimiter(defaultRequestsPerSecond),
		now:     time.Now,
		sleep:   sleepCtx,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Result is one response, in the form the store records it.
//
// Raw is kept verbatim so that a parser change never requires another request.
type Result struct {
	Endpoint    string
	RequestedAt time.Time
	HTTPStatus  int
	ETag        string
	NotModified bool
	// WGError is the Wargaming error message, if the body carried one.
	WGError string
	Raw     []byte
	// Data is the `data` member of a successful envelope.
	Data json.RawMessage
	Meta json.RawMessage
}

// OK reports whether the result carries usable data.
func (r Result) OK() bool {
	return r.WGError == "" && r.HTTPStatus >= 200 && r.HTTPStatus < 300 && len(r.Raw) > 0
}

// envelope is the shape every Wargaming response shares.
type envelope struct {
	Status string `json:"status"`
	Error  *struct {
		Field   string `json:"field"`
		Message string `json:"message"`
		Code    int    `json:"code"`
		Value   string `json:"value"`
	} `json:"error"`
	Meta json.RawMessage `json:"meta"`
	Data json.RawMessage `json:"data"`
}

// Get calls an endpoint such as "account/info".
//
// ifNoneMatch, when non-empty, makes this a conditional request; a 304 comes
// back as a Result with NotModified set and no body, which is a successful
// outcome rather than an error.
func (c *Client) Get(ctx context.Context, endpoint string, params url.Values, ifNoneMatch string) (Result, error) {
	return c.call(ctx, http.MethodGet, endpoint, params, ifNoneMatch)
}

// Post calls an endpoint with its parameters in a form body. Wargaming's
// auth/prolongate and auth/logout read access_token only from there: sent in a
// GET query it is ignored, and the call fails with ACCESS_TOKEN_NOT_SPECIFIED
// (checked against the live API on 2026-09-27). The body also keeps the token
// out of every URL.
func (c *Client) Post(ctx context.Context, endpoint string, params url.Values) (Result, error) {
	return c.call(ctx, http.MethodPost, endpoint, params, "")
}

func (c *Client) call(ctx context.Context, method, endpoint string, params url.Values, ifNoneMatch string) (Result, error) {
	form := url.Values{}
	for k, v := range params {
		form[k] = v
	}
	form.Set("application_id", c.appID)

	requestURL := fmt.Sprintf("%s/%s/", c.baseURL, strings.Trim(endpoint, "/"))
	body := ""
	if method == http.MethodGet {
		requestURL += "?" + form.Encode()
	} else {
		body = form.Encode()
	}

	// The last attempt's Result is returned even on failure, so the caller can
	// record the failed attempt as a snapshot. Evidence that a source was tried
	// and refused is part of the freshness story, not something to discard.
	var (
		lastErr    error
		lastResult Result
	)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			// Exponential backoff: 400ms, 800ms.
			delay := time.Duration(1<<(attempt-2)) * 400 * time.Millisecond
			if err := c.sleep(ctx, delay); err != nil {
				return lastResult, err
			}
		}
		if err := c.limiter.wait(ctx); err != nil {
			return lastResult, err
		}

		result, err := c.do(ctx, method, endpoint, requestURL, body, ifNoneMatch)
		if err == nil {
			return result, nil
		}
		lastErr, lastResult = err, result

		if !retryable(err) {
			return result, err
		}
	}
	return lastResult, fmt.Errorf("%s: giving up after %d attempts: %w", endpoint, maxAttempts, lastErr)
}

func (c *Client) do(ctx context.Context, method, endpoint, requestURL, form, ifNoneMatch string) (Result, error) {
	var reader io.Reader
	if form != "" {
		reader = strings.NewReader(form)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, reader)
	if err != nil {
		return Result{}, fmt.Errorf("%s: building request: %w", endpoint, err)
	}
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", useragent.String)
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}

	requestedAt := c.now().UTC()
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", endpoint, &transportError{err: err})
	}
	defer resp.Body.Close()

	result := Result{
		Endpoint:    endpoint,
		RequestedAt: requestedAt,
		HTTPStatus:  resp.StatusCode,
		ETag:        resp.Header.Get("ETag"),
	}

	if resp.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return result, fmt.Errorf("%s: reading body: %w", endpoint, &transportError{err: err})
	}
	result.Raw = body

	if resp.StatusCode >= 500 {
		return result, fmt.Errorf("%s: %w", endpoint, &transportError{status: resp.StatusCode})
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("%s: unexpected HTTP %d", endpoint, resp.StatusCode)
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return result, fmt.Errorf("%s: parsing response: %w", endpoint, err)
	}

	if env.Status == "error" && env.Error != nil {
		result.WGError = env.Error.Message
		return result, fmt.Errorf("%s: %w", endpoint, &Error{
			Code:    env.Error.Code,
			Message: env.Error.Message,
			Field:   env.Error.Field,
			Value:   env.Error.Value,
		})
	}
	if env.Status != "ok" {
		return result, fmt.Errorf("%s: unexpected status %q", endpoint, env.Status)
	}

	result.Data = env.Data
	result.Meta = env.Meta
	return result, nil
}

// transportError is a network or 5xx failure, which is worth retrying.
type transportError struct {
	err    error
	status int
}

func (e *transportError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return fmt.Sprintf("HTTP %d", e.status)
}

func (e *transportError) Unwrap() error { return e.err }

func retryable(err error) bool {
	var te *transportError
	if errors.As(err, &te) {
		return true
	}
	if wgErr, ok := AsError(err); ok {
		return wgErr.Retryable()
	}
	return false
}

// limiter spaces requests apart to stay under the per-IP budget.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time

	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

func newLimiter(rps float64) *limiter {
	interval := time.Duration(0)
	if rps > 0 {
		interval = time.Duration(float64(time.Second) / rps)
	}
	return &limiter{interval: interval, now: time.Now, sleep: sleepCtx}
}

// wait blocks until the next request may be sent.
func (l *limiter) wait(ctx context.Context) error {
	if l.interval <= 0 {
		return nil
	}

	l.mu.Lock()
	now := l.now()
	delay := time.Duration(0)
	if now.Before(l.next) {
		delay = l.next.Sub(now)
	}
	l.next = now.Add(delay + l.interval)
	l.mu.Unlock()

	if delay <= 0 {
		return nil
	}
	return l.sleep(ctx, delay)
}

// sleepCtx waits for d, or returns early if ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
