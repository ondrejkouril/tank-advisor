package wg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Token is a Wargaming access token and what it was issued for.
//
// Tokens last about two weeks and can be extended before they expire, so wotctx
// prolongs one that is close to the edge rather than making the user log in
// again.
type Token struct {
	AccessToken string
	AccountID   int
	Nickname    string
	ExpiresAt   time.Time
}

// Valid reports whether the token has not expired as of now.
func (t Token) Valid(now time.Time) bool {
	return t.AccessToken != "" && !t.ExpiresAt.IsZero() && now.Before(t.ExpiresAt)
}

// NeedsProlongation reports whether the token should be extended now. Renewing
// early is free; discovering an expired token mid-sync is not.
func (t Token) NeedsProlongation(now time.Time, within time.Duration) bool {
	if t.AccessToken == "" || t.ExpiresAt.IsZero() {
		return false
	}
	return now.Add(within).After(t.ExpiresAt)
}

// LoginURL asks Wargaming for the URL the user must open to log in.
//
// nofollow=1 makes the API return the destination as JSON instead of issuing a
// redirect, which is what lets a command-line tool drive this flow at all: the
// URL goes to a browser, and the browser comes back to redirectURI.
func (c *Client) LoginURL(ctx context.Context, redirectURI string, expiresIn time.Duration) (string, error) {
	params := url.Values{}
	params.Set("nofollow", "1")
	params.Set("redirect_uri", redirectURI)
	if expiresIn > 0 {
		params.Set("expires_at", strconv.FormatInt(int64(expiresIn.Seconds()), 10))
	}

	result, err := c.Get(ctx, "auth/login", params, "")
	if err != nil {
		return "", err
	}

	var payload struct {
		Location string `json:"location"`
	}
	if err := json.Unmarshal(result.Data, &payload); err != nil {
		return "", fmt.Errorf("auth/login: parsing response: %w", err)
	}
	if payload.Location == "" {
		return "", fmt.Errorf("auth/login: response carried no location")
	}
	return payload.Location, nil
}

// Prolongate extends an unexpired token and returns the replacement.
func (c *Client) Prolongate(ctx context.Context, token string, expiresIn time.Duration) (Token, error) {
	params := url.Values{}
	params.Set("access_token", token)
	if expiresIn > 0 {
		params.Set("expires_at", strconv.FormatInt(int64(expiresIn.Seconds()), 10))
	}

	result, err := c.Post(ctx, "auth/prolongate", params)
	if err != nil {
		return Token{}, err
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		AccountID   int    `json:"account_id"`
		Nickname    string `json:"nickname"`
		ExpiresAt   int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(result.Data, &payload); err != nil {
		return Token{}, fmt.Errorf("auth/prolongate: parsing response: %w", err)
	}
	if payload.AccessToken == "" {
		return Token{}, fmt.Errorf("auth/prolongate: response carried no access_token")
	}

	return Token{
		AccessToken: payload.AccessToken,
		AccountID:   payload.AccountID,
		Nickname:    payload.Nickname,
		ExpiresAt:   time.Unix(payload.ExpiresAt, 0).UTC(),
	}, nil
}

// Logout invalidates a token.
func (c *Client) Logout(ctx context.Context, token string) error {
	params := url.Values{}
	params.Set("access_token", token)

	_, err := c.Post(ctx, "auth/logout", params)
	return err
}

// TokenFromCallback reads the token out of the query string Wargaming redirects
// the browser to after a successful login.
//
// A failed login arrives the same way, with status=error, so this is also where
// a user-facing login failure is turned into an error.
func TokenFromCallback(query url.Values) (Token, error) {
	switch status := query.Get("status"); status {
	case "ok":
	case "error":
		message := query.Get("message")
		if message == "" {
			message = "login failed"
		}
		code, _ := strconv.Atoi(query.Get("code"))
		return Token{}, &Error{Code: code, Message: message}
	default:
		return Token{}, fmt.Errorf("login callback carried an unexpected status %q", status)
	}

	token := Token{
		AccessToken: query.Get("access_token"),
		Nickname:    query.Get("nickname"),
	}
	if token.AccessToken == "" {
		return Token{}, fmt.Errorf("login callback carried no access_token")
	}

	if raw := query.Get("account_id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil {
			return Token{}, fmt.Errorf("login callback account_id %q: %w", raw, err)
		}
		token.AccountID = id
	}
	if raw := query.Get("expires_at"); raw != "" {
		seconds, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Token{}, fmt.Errorf("login callback expires_at %q: %w", raw, err)
		}
		token.ExpiresAt = time.Unix(seconds, 0).UTC()
	}
	return token, nil
}
