package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ondrejkouril/tank-advisor/internal/secrets"
)

// The same invented credential shapes the secrets tests use.
const (
	fakeAppID = "deadbeefcafebabe0123456789abcdef"
	fakeToken = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4"
	fakeTmgg  = "tmgg_9zXq-pLm3_R7tKd2VbN8"
)

// runDoctorJSON runs `doctor --json` and returns the parsed report keyed by
// check name, along with the command's error.
func runDoctorJSON(t *testing.T, env *Env, buf interface{ Bytes() []byte }) (map[string]check, error) {
	t.Helper()

	err := Run(context.Background(), env, []string{"doctor", "--json"})

	var report doctorReport
	if jsonErr := json.Unmarshal(buf.Bytes(), &report); jsonErr != nil {
		t.Fatalf("doctor --json emitted invalid JSON: %v\n%s", jsonErr, buf.Bytes())
	}
	if report.Version != env.Version {
		t.Errorf("report.Version = %q, want %q", report.Version, env.Version)
	}

	byName := make(map[string]check, len(report.Checks))
	for _, c := range report.Checks {
		byName[c.Name] = c
	}
	return byName, err
}

// TestDoctorReportsEveryCheckByName keeps the report shape stable: the skill's
// staleness probe reads these names, so a check may change status but must not
// vanish.
func TestDoctorReportsEveryCheckByName(t *testing.T) {
	env, buf := newTestEnv(t)
	seedAllCredentials(t, env)

	checks, err := runDoctorJSON(t, env, buf)
	if err != nil {
		t.Fatalf("doctor with all credentials present = %v, want nil", err)
	}

	for _, name := range []string{
		"build", "config", "secrets", "wg-auth", "data-age", "vehicles", "overlay", "wn8", "mod-data",
	} {
		if _, ok := checks[name]; !ok {
			t.Errorf("doctor report is missing the %q check", name)
		}
	}
	if got := checks["build"].Status; got != statusOK {
		t.Errorf("build check = %q, want %q", got, statusOK)
	}
	if got := checks["secrets"].Status; got != statusOK {
		t.Errorf("secrets check = %q, want %q with everything present", got, statusOK)
	}
}

// TestDoctorConfigCheckNamesItsPaths is the plan step 2 acceptance criterion for
// the config side: doctor must say where config, database and overlay live.
func TestDoctorConfigCheckNamesItsPaths(t *testing.T) {
	env, buf := newTestEnv(t)
	seedAllCredentials(t, env)

	checks, err := runDoctorJSON(t, env, buf)
	if err != nil {
		t.Fatalf("doctor = %v, want nil", err)
	}

	cfg := checks["config"]
	if cfg.Status != statusOK {
		t.Errorf("config check = %q, want %q", cfg.Status, statusOK)
	}
	for _, want := range []string{
		env.Paths.ConfigFile,
		env.Paths.DBFile,
		env.Config.OverlayFile(env.Paths),
		env.Config.Account.Nickname,
	} {
		if !strings.Contains(cfg.Detail, want) {
			t.Errorf("config detail %q does not mention %q", cfg.Detail, want)
		}
	}
}

// TestDoctorNeverPrintsSecretValues is the other half of the plan step 2
// criterion: presence only, never the value.
func TestDoctorNeverPrintsSecretValues(t *testing.T) {
	for _, format := range [][]string{{"doctor"}, {"doctor", "--json"}} {
		t.Run(strings.Join(format, " "), func(t *testing.T) {
			env, buf := newTestEnv(t)
			seedAllCredentials(t, env)

			// An error is fine here; leaking is not.
			_ = Run(context.Background(), env, format)

			out := buf.String()
			for _, secret := range []string{fakeAppID, fakeToken, fakeTmgg} {
				if strings.Contains(out, secret) {
					t.Errorf("doctor output contains a credential value:\n%s", out)
				}
			}
			// It must still say that the credentials are configured, and where
			// each came from.
			for _, want := range []string{"wg_application_id", "wg_access_token", string(secrets.SourceKeyring)} {
				if !strings.Contains(out, want) {
					t.Errorf("doctor output does not mention %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestDoctorFailsWithoutAnApplicationID(t *testing.T) {
	env, buf := newTestEnv(t) // empty secret store

	checks, err := runDoctorJSON(t, env, buf)
	if err == nil {
		t.Error("doctor exited 0 with no application_id, want a failure")
	}

	got := checks["secrets"]
	if got.Status != statusFail {
		t.Errorf("secrets check = %q, want %q", got.Status, statusFail)
	}
	if !strings.Contains(got.Detail, "wg_application_id: missing") {
		t.Errorf("secrets detail %q does not report the missing key", got.Detail)
	}
	if got.Fix == "" {
		t.Error("secrets check has no Fix; the skill needs a command to suggest")
	}
}

// TestDoctorWarnsRatherThanFailsForARecoverableGap: a missing access token
// still leaves the public endpoints reachable and logging in fixes it, so it
// must not make doctor exit non-zero the way a missing application_id does.
func TestDoctorWarnsRatherThanFailsForARecoverableGap(t *testing.T) {
	env, buf := newTestEnv(t)
	if err := env.Secrets.Set(secrets.WGApplicationID, fakeAppID); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	checks, err := runDoctorJSON(t, env, buf)
	if err != nil {
		t.Errorf("doctor = %v, want nil for a recoverable gap", err)
	}

	got := checks["secrets"]
	if got.Status != statusWarn {
		t.Errorf("secrets check = %q, want %q", got.Status, statusWarn)
	}
	if !strings.Contains(got.Fix, "wotctx auth wg") {
		t.Errorf("Fix = %q, want it to suggest logging in", got.Fix)
	}
}

// TestDoctorNeverSuggestsTheRemovedProvider guards against the dropped
// integration coming back as a suggestion. Its API needs a paid subscription,
// so telling the user to go and get a key would send them to a dead end.
//
// It inspects the checks rather than the raw output, because the config check
// legitimately prints filesystem paths and a test's own name ends up inside
// t.TempDir() - which is how the first version of this test failed on itself.
func TestDoctorNeverSuggestsTheRemovedProvider(t *testing.T) {
	env, buf := newTestEnv(t)

	checks, _ := runDoctorJSON(t, env, buf)
	for name, c := range checks {
		if strings.Contains(strings.ToLower(c.Fix), "tomato") {
			t.Errorf("check %q suggests the removed provider: %q", name, c.Fix)
		}
		if name == "secrets" && strings.Contains(strings.ToLower(c.Detail), "tomato") {
			t.Errorf("the secrets check still tracks a Tomato.gg key: %q", c.Detail)
		}
	}
}

func TestDoctorTableIncludesTheFix(t *testing.T) {
	env, buf := newTestEnv(t) // empty store, so secrets fails and carries a Fix

	_ = Run(context.Background(), env, []string{"doctor"})

	if !strings.Contains(buf.String(), "developers.wargaming.net") {
		t.Errorf("table output omits the suggested fix:\n%s", buf.String())
	}
}

func seedAllCredentials(t *testing.T, env *Env) {
	t.Helper()
	for _, k := range secrets.All() {
		if err := env.Secrets.Set(k, valueFor(k)); err != nil {
			t.Fatalf("seeding %s: %v", k, err)
		}
	}
}

func valueFor(k secrets.Key) string {
	switch k {
	case secrets.WGApplicationID:
		return fakeAppID
	case secrets.WGAccessToken:
		return fakeToken
	default:
		return "2026-10-01T12:00:00Z"
	}
}
