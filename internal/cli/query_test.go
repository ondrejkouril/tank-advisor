package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestQueryNeedsACache: a query never creates the cache as a side effect.
func TestQueryNeedsACache(t *testing.T) {
	for _, args := range [][]string{
		{"query", "resources"}, {"query", "garage"}, {"query", "tank", "Leox"},
		{"query", "performance"}, {"query", "sessions"}, {"query", "candidates"},
	} {
		env, _ := newTestEnv(t)
		err := Run(context.Background(), env, args)
		if err == nil || !strings.Contains(err.Error(), "wotctx sync") {
			t.Errorf("%v = %v, want a no-cache error naming the fix", args, err)
		}
		if env.HasStore() {
			t.Errorf("%v created the cache", args)
		}
	}
}

func TestQueryRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{"query", "garage", "--class", "SPGs"},
		{"query", "garage", "--tier", "12"},
		{"query", "tank"},
		{"query", "performance", "--by", "crew"},
		{"query", "performance", "--window", "month"},
		{"query", "sessions", "--window", "lifetime"},
		{"query", "candidates", "--budget-credits", "-1"},
		{"query", "resources", "extra"},
	} {
		env, _ := envWithCache(t, map[string]time.Duration{"wg:account/info": time.Minute})
		if err := Run(context.Background(), env, args); !errors.Is(err, ErrUsage) {
			t.Errorf("%v = %v, want ErrUsage", args, err)
		}
	}
}

// TestQueryEmitsCompactJSONUnlessPretty: the skill reads these, and
// indentation is only tokens to it.
func TestQueryEmitsCompactJSONUnlessPretty(t *testing.T) {
	env, buf := envWithCache(t, map[string]time.Duration{"wg:account/info": time.Minute})
	if err := Run(context.Background(), env, []string{"query", "resources"}); err != nil {
		t.Fatalf("query resources = %v", err)
	}
	out := buf.String()
	if strings.Count(out, "\n") != 1 {
		t.Errorf("compact output spans %d lines:\n%s", strings.Count(out, "\n"), out)
	}
	var envelope struct {
		Meta struct {
			Sources []struct {
				Name string `json:"name"`
			} `json:"sources"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(envelope.Meta.Sources) == 0 {
		t.Error("meta.sources is empty")
	}

	buf.Reset()
	if err := Run(context.Background(), env, []string{"query", "resources", "--pretty"}); err != nil {
		t.Fatalf("query resources --pretty = %v", err)
	}
	if strings.Count(buf.String(), "\n") < 5 {
		t.Errorf("--pretty output is not indented:\n%s", buf.String())
	}
}

func TestQueryTankReportsUnknownNames(t *testing.T) {
	env, _ := envWithCache(t, map[string]time.Duration{"wg:account/info": time.Minute})
	err := Run(context.Background(), env, []string{"query", "tank", "Landkreuzer"})
	if err == nil || !strings.Contains(err.Error(), `unknown tank "Landkreuzer"`) {
		t.Errorf("query tank Landkreuzer = %v", err)
	}
}
