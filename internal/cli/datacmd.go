package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ondrejkouril/tank-advisor/internal/config"
	"github.com/ondrejkouril/tank-advisor/internal/secrets"
)

// runDataDelete removes everything wotctx holds about the account: the cache
// with its snapshot history, the client mod's dump, the login token, and the
// account recorded in config.yaml. It is the app's "Delete my data" and the
// uninstaller's option (docs/spec-desktop.md section 12.3). The overlay stays:
// it is the player's own settings file, not data fetched from Wargaming.
func runDataDelete(_ context.Context, env *Env, args []string) error {
	fs := newFlagSet(env, "data delete")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if env.ConfigErr != nil {
		return env.ConfigErr
	}

	fmt.Fprintln(env.Stdout, "This deletes:")
	fmt.Fprintf(env.Stdout, "  the cache and its snapshot history: %s\n", env.Paths.DBFile)
	fmt.Fprintf(env.Stdout, "  the client mod's dump: %s\n", filepath.Dir(env.modDumpPath()))
	fmt.Fprintln(env.Stdout, "  the Wargaming login token, from the keychain")
	fmt.Fprintf(env.Stdout, "  the account recorded in %s\n", env.Paths.ConfigFile)
	fmt.Fprintln(env.Stdout, "Snapshot history cannot be fetched again: recent form starts over from the next sync.")
	fmt.Fprintf(env.Stdout, "Kept: your settings in %s, and any application_id of your own.\n", env.Config.OverlayFile(env.Paths))

	if !*yes {
		fmt.Fprint(env.Stdout, "Type delete to confirm: ")
		line, err := bufio.NewReader(env.stdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if strings.TrimSpace(line) != "delete" {
			fmt.Fprintln(env.Stdout, "nothing deleted")
			return nil
		}
	}

	// The database first: if Claude Desktop holds it open, nothing else has
	// been deleted yet and the player can simply try again.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		err := os.Remove(env.Paths.DBFile + suffix)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("deleting %s: %w\nclose Claude Desktop and anything else using wotctx, then try again",
				env.Paths.DBFile+suffix, err)
		}
	}
	if dump := env.modDumpPath(); dump != "" {
		if err := os.RemoveAll(filepath.Dir(dump)); err != nil {
			return fmt.Errorf("deleting the mod dump: %w", err)
		}
	}
	for _, k := range []secrets.Key{secrets.WGAccessToken, secrets.WGTokenExpiry} {
		if err := env.Secrets.Delete(k); err != nil {
			return err
		}
	}
	if _, err := os.Stat(env.Paths.ConfigFile); err == nil {
		if err := config.Set(env.Paths.ConfigFile,
			config.Update{Key: "account.account_id"},
			config.Update{Key: "account.nickname"},
		); err != nil {
			return err
		}
	}
	env.Config.Account = config.Account{Realm: env.Config.Account.Realm}
	fmt.Fprintln(env.Stdout, "deleted")
	return nil
}
