package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/secrets"
)

// TestBriefCarriesNoSecretMaterial is a plan step 10 acceptance criterion:
// with every credential present, none of them - nor the account's token -
// reaches the brief, which is meant to be pasted into a chat.
func TestBriefCarriesNoSecretMaterial(t *testing.T) {
	env, buf := envWithCache(t, map[string]time.Duration{
		"wg:account/info": time.Minute, "wg:tanks/stats": time.Minute,
	})
	if err := Run(context.Background(), env, []string{"brief"}); err != nil {
		t.Fatalf("brief = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "## Known gaps") {
		t.Fatalf("no brief was produced:\n%s", out)
	}
	for _, k := range secrets.All() {
		if v := valueFor(k); v != "" && strings.Contains(out, v) {
			t.Errorf("brief contains the %s", k)
		}
	}
	for _, v := range []string{fakeAppID, fakeToken, "access_token", "application_id"} {
		if strings.Contains(out, v) {
			t.Errorf("brief contains %q", v)
		}
	}
}

func TestBriefArguments(t *testing.T) {
	env, _ := newTestEnv(t)
	if err := Run(context.Background(), env, []string{"brief"}); err == nil || !strings.Contains(err.Error(), "wotctx sync") {
		t.Errorf("brief with no cache = %v, want a no-cache error", err)
	}
	if env.HasStore() {
		t.Error("brief created the cache")
	}

	env, _ = envWithCache(t, map[string]time.Duration{"wg:account/info": time.Minute})
	for _, args := range [][]string{{"brief", "--max-chars", "500"}, {"brief", "extra"}} {
		if err := Run(context.Background(), env, args); !errors.Is(err, ErrUsage) {
			t.Errorf("%v = %v, want ErrUsage", args, err)
		}
	}
}
