package cli

import (
	"context"
	"strings"
	"testing"
)

// TestHookSessionStartIsSilentByDefault: the plugin is installed user-wide, so
// this runs at the start of sessions that have nothing to do with tanks.
func TestHookSessionStartIsSilentByDefault(t *testing.T) {
	t.Setenv(injectBriefOption, "")
	env, buf := seededEnv(t)
	if err := Run(context.Background(), env, []string{"hook", "session-start"}); err != nil {
		t.Fatalf("hook session-start = %v, want nil", err)
	}
	if buf.Len() != 0 {
		t.Errorf("hook session-start printed with inject_brief off:\n%s", buf.String())
	}
}

func TestHookSessionStartInjectsTheBrief(t *testing.T) {
	t.Setenv(injectBriefOption, "true")
	env, buf := seededEnv(t)
	if err := Run(context.Background(), env, []string{"brief"}); err != nil {
		t.Fatalf("brief: %v", err)
	}
	want := buf.String()
	buf.Reset()

	if err := Run(context.Background(), env, []string{"hook", "session-start"}); err != nil {
		t.Fatalf("hook session-start = %v, want nil", err)
	}
	got := buf.String()
	if !strings.HasSuffix(got, want) {
		t.Error("the hook's output does not end with the brief")
	}
	if !strings.Contains(got, "was not synced") {
		t.Error("the hook does not say the brief was not synced for this session")
	}
}

// TestHookSessionStartNeverFails: with no cache, a failing hook would show an
// error notice at the start of an unrelated session.
func TestHookSessionStartNeverFails(t *testing.T) {
	t.Setenv(injectBriefOption, "true")
	env, buf := newTestEnv(t) // no cache
	if err := Run(context.Background(), env, []string{"hook", "session-start"}); err != nil {
		t.Errorf("hook session-start with no cache = %v, want nil", err)
	}
	if buf.Len() != 0 {
		t.Errorf("hook session-start with no cache printed:\n%s", buf.String())
	}
}
