package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/secrets"
	"github.com/ondrejkouril/tank-advisor/internal/wg"
)

// callbackPort is where the login callback lands. It is fixed rather than
// chosen at random because Wargaming's application registration may pin the
// redirect URI, and a moving port could not be registered.
const callbackPort = 8731

// tokenLifetime is what we ask Wargaming for. Two weeks is the documented
// maximum.
const tokenLifetime = 14 * 24 * time.Hour

// prolongWindow is how close to expiry a token gets renewed. Renewing early is
// free; discovering an expired token mid-sync is not.
const prolongWindow = 3 * 24 * time.Hour

// newWGClient builds the login's Wargaming client; tests point it at a local
// server.
var newWGClient = func(realm, appID string) (*wg.Client, error) { return wg.New(realm, appID) }

// loginTimeout bounds how long the callback server waits for a human.
const loginTimeout = 5 * time.Minute

func runAuthWG(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "auth wg")
	relogin := fs.Bool("relogin", false, "force a fresh login even if a valid token is stored")
	prolongOnly := fs.Bool("prolong", false, "extend the stored token without logging in again")
	manual := fs.Bool("manual", false, "print the login URL and read the redirected URL from stdin")
	appIDFlag := fs.String("application-id", "", "use your own application_id instead of the built-in one (stored in the keychain)")
	realm := fs.String("realm", "", "the server to log in to: eu, com or asia; the first login records it")
	logout := fs.Bool("logout", false, "revoke the stored token and remove it from the keychain")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if env.ConfigErr != nil {
		return env.ConfigErr
	}

	if *realm != "" {
		if _, err := wg.BaseURLForRealm(*realm); err != nil {
			return usageErr(env, err.Error())
		}
		if env.Config.Configured() && *realm != env.Config.Account.Realm {
			return fmt.Errorf("this installation holds %s on %s; to switch, delete its data first: wotctx data delete",
				env.Config.Account.Nickname, env.Config.Account.Realm)
		}
		env.Config.Account.Realm = *realm
	}

	if *appIDFlag != "" {
		if err := env.Secrets.Set(secrets.WGApplicationID, strings.TrimSpace(*appIDFlag)); err != nil {
			return err
		}
		fmt.Fprintln(env.Stdout, "stored the Wargaming application_id")
	}

	appID, _, err := env.applicationID()
	if err != nil {
		return err
	}

	client, err := newWGClient(env.Config.Account.Realm, appID)
	if err != nil {
		return err
	}

	if *logout {
		return logoutStoredToken(ctx, env, client)
	}
	if *prolongOnly {
		return prolongStoredToken(ctx, env, client)
	}

	// A token with no recorded account (config.yaml replaced or lost) says
	// nothing about whose it is, so it cannot stand in for a login: the
	// login is what records the account.
	if !*relogin && env.Config.Configured() {
		if token, err := loadToken(env); err == nil {
			switch {
			case token.NeedsProlongation(env.now(), prolongWindow):
				fmt.Fprintln(env.Stdout, "stored token is near expiry; extending it")
				return prolongStoredToken(ctx, env, client)
			case token.Valid(env.now()):
				fmt.Fprintf(env.Stdout, "already logged in as %s; token valid until %s\n",
					token.Nickname, token.ExpiresAt.Format(time.RFC3339))
				fmt.Fprintln(env.Stdout, "use --relogin to replace it")
				return nil
			}
		}
	}

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", callbackPort)

	loginURL, err := client.LoginURL(ctx, redirectURI, tokenLifetime)
	if err != nil {
		return err
	}

	var token wg.Token
	if *manual {
		token, err = loginManually(env, loginURL)
	} else {
		token, err = loginViaCallback(ctx, env, loginURL, redirectURI)
	}
	if err != nil {
		return err
	}

	// One installation holds one account's history. A token for another
	// account would sync that account over this one's snapshots.
	if env.Config.Configured() && token.AccountID != env.Config.Account.AccountID {
		return fmt.Errorf("logged in as %s (account %d), but this installation holds %s (account %d); "+
			"the token was not stored. To switch accounts, delete this one's data first: wotctx data delete",
			token.Nickname, token.AccountID, env.Config.Account.Nickname, env.Config.Account.AccountID)
	}

	if err := storeToken(env, token); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "logged in as %s (account %d); token valid until %s\n",
		token.Nickname, token.AccountID, token.ExpiresAt.Format(time.RFC3339))

	if !env.Config.Configured() {
		if err := recordAccount(env, token); err != nil {
			return err
		}
		fmt.Fprintf(env.Stdout, "recorded %s on %s in %s\n", token.Nickname, env.Config.Account.Realm, env.Paths.ConfigFile)
	}
	return nil
}

// recordAccount writes the logged-in account into config.yaml, so nobody has
// to type an account id (docs/spec-desktop.md section 5.1, step 3).
func recordAccount(env *Env, token wg.Token) error {
	if token.AccountID <= 0 {
		return errors.New("the login returned no account id")
	}
	account := config.Account{Realm: env.Config.Account.Realm, AccountID: token.AccountID, Nickname: token.Nickname}
	if err := config.Set(env.Paths.ConfigFile,
		config.Update{Key: "account.realm", Value: account.Realm},
		config.Update{Key: "account.account_id", Value: account.AccountID},
		config.Update{Key: "account.nickname", Value: account.Nickname},
	); err != nil {
		return fmt.Errorf("recording the account: %w", err)
	}
	env.Config.Account = account
	return nil
}

// logoutStoredToken revokes the token at Wargaming and removes it here. The
// policy requires a log-out function (docs/spec-desktop.md section 12.2). The
// local copy goes even if Wargaming cannot be reached: the player asked for it
// to be gone, and an unused token expires by itself.
func logoutStoredToken(ctx context.Context, env *Env, client *wg.Client) error {
	token, err := loadToken(env)
	if err != nil {
		fmt.Fprintln(env.Stdout, "not logged in")
		return nil
	}
	if err := client.Logout(ctx, token.AccessToken); err != nil {
		fmt.Fprintf(env.Stderr, "Wargaming did not confirm the logout (%s); removing the token here anyway\n",
			env.Redactor().RedactError(err))
	}
	for _, k := range []secrets.Key{secrets.WGAccessToken, secrets.WGTokenExpiry} {
		if err := env.Secrets.Delete(k); err != nil {
			return err
		}
	}
	fmt.Fprintln(env.Stdout, "logged out; the token is revoked and removed from the keychain")
	return nil
}

// loginViaCallback opens the browser and waits for Wargaming to redirect back
// to a one-shot local server.
func loginViaCallback(ctx context.Context, env *Env, loginURL, redirectURI string) (wg.Token, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", callbackPort))
	if err != nil {
		return wg.Token{}, fmt.Errorf("cannot listen on %s (is another wotctx login running?): %w", redirectURI, err)
	}
	defer listener.Close()

	type outcome struct {
		token wg.Token
		err   error
	}
	results := make(chan outcome, 1)

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			token, err := wg.TokenFromCallback(r.URL.Query())

			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, "Login failed. Return to the terminal for details.\n")
			} else {
				io.WriteString(w, "Logged in. You can close this tab and return to the terminal.\n")
			}

			select {
			case results <- outcome{token: token, err: err}:
			default:
			}
		}),
	}
	go server.Serve(listener)
	defer server.Close()

	fmt.Fprintln(env.Stdout, "opening the Wargaming login page in your browser")
	fmt.Fprintf(env.Stdout, "if it does not open, visit:\n\n  %s\n\n", loginURL)
	if err := openBrowser(loginURL); err != nil {
		fmt.Fprintf(env.Stderr, "could not open a browser automatically: %v\n", err)
	}
	fmt.Fprintf(env.Stdout, "waiting up to %s for the login to complete...\n", loginTimeout)

	waitCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	select {
	case res := <-results:
		return res.token, res.err
	case <-waitCtx.Done():
		return wg.Token{}, fmt.Errorf("timed out waiting for the login callback; "+
			"try again, or use: wotctx auth wg --manual (%w)", waitCtx.Err())
	}
}

// loginManually is the fallback for when a local callback cannot be used,
// because the application registration refused a localhost redirect URI or a
// browser cannot reach this machine.
func loginManually(env *Env, loginURL string) (wg.Token, error) {
	fmt.Fprintf(env.Stdout, "open this URL, log in, then paste the full URL you land on:\n\n  %s\n\n> ", loginURL)

	line, err := bufio.NewReader(env.stdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return wg.Token{}, fmt.Errorf("reading the redirected URL: %w", err)
	}

	redirected, err := url.Parse(strings.TrimSpace(line))
	if err != nil {
		return wg.Token{}, fmt.Errorf("that does not look like a URL: %w", err)
	}
	return wg.TokenFromCallback(redirected.Query())
}

func prolongStoredToken(ctx context.Context, env *Env, client *wg.Client) error {
	token, err := loadToken(env)
	if err != nil {
		return fmt.Errorf("no stored token to extend: run wotctx auth wg")
	}

	extended, err := client.Prolongate(ctx, token.AccessToken, tokenLifetime)
	if err != nil {
		if wg.IsAuthError(err) {
			return fmt.Errorf("the stored token can no longer be extended: run wotctx auth wg --relogin")
		}
		return err
	}
	if err := storeToken(env, extended); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "token extended; valid until %s\n", extended.ExpiresAt.Format(time.RFC3339))
	return nil
}

// loadToken reads the stored token. A token with no recorded expiry is treated
// as absent, since wotctx cannot then tell whether it is safe to use.
func loadToken(env *Env) (wg.Token, error) {
	access, _, err := env.Secrets.Get(secrets.WGAccessToken)
	if err != nil {
		return wg.Token{}, err
	}
	token := wg.Token{AccessToken: access, Nickname: env.Config.Account.Nickname, AccountID: env.Config.Account.AccountID}

	expiry, _, err := env.Secrets.Get(secrets.WGTokenExpiry)
	if err != nil {
		return token, nil
	}
	at, err := time.Parse(time.RFC3339, expiry)
	if err != nil {
		return token, nil
	}
	token.ExpiresAt = at
	return token, nil
}

func storeToken(env *Env, token wg.Token) error {
	if err := env.Secrets.Set(secrets.WGAccessToken, token.AccessToken); err != nil {
		return err
	}
	if !token.ExpiresAt.IsZero() {
		if err := env.Secrets.Set(secrets.WGTokenExpiry, token.ExpiresAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

// openBrowser asks the desktop to open a URL. Failure is not fatal: the URL is
// printed either way.
func openBrowser(target string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	case "darwin":
		return exec.Command("open", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}
