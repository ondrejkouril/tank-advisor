package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/store"
)

// dutyRig is a fixture with the duties on a fake clock: sleeping moves the
// clock, and a sync is recorded instead of run.
type dutyRig struct {
	*fixture
	now     time.Time
	syncs   []time.Time
	notices []Notice
	duties  *Duties
}

func newDutyRig(t *testing.T) *dutyRig {
	t.Helper()
	r := &dutyRig{fixture: newFixture(t, true), now: time.Now()}
	r.duties = r.svc.NewDuties(DutyHooks{
		Now:    func() time.Time { return r.now },
		Sleep:  func(_ context.Context, d time.Duration) error { r.now = r.now.Add(d); return nil },
		Notify: func(n Notice) { r.notices = append(r.notices, n) },
		Sync: func(context.Context) Result {
			r.syncs = append(r.syncs, r.now)
			return Result{OK: true, Message: "Synced."}
		},
	})
	return r
}

func (r *dutyRig) accountSyncedAt(t *testing.T, at time.Time) {
	t.Helper()
	env := r.svc.opts.NewEnv(nil, nil)
	db, err := env.OpenStore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.PutSnapshot(context.Background(), store.Snapshot{Source: "wg", Endpoint: "account/info", RequestedAt: at, HTTPStatus: 200, Raw: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
}

func TestASyncFollowsTheGameOnceTheDumpIsQuiet(t *testing.T) {
	r := newDutyRig(t)
	dump := filepath.Join(r.dir, "mod", "garage.json")
	write(t, dump, "{}")
	// Written just now: the mod may still be at it.
	os.Chtimes(dump, r.now, r.now)
	start := r.now

	r.duties.AfterGame(context.Background())
	if len(r.syncs) != 1 {
		t.Fatalf("%d syncs, want 1", len(r.syncs))
	}
	if waited := r.syncs[0].Sub(start); waited < settleQuiet || waited > settleQuiet+3*time.Second {
		t.Errorf("synced %s after the game, want just after %s of quiet", waited, settleQuiet)
	}
}

func TestNoDumpMeansNoWait(t *testing.T) {
	r := newDutyRig(t)
	start := r.now
	r.duties.AfterGame(context.Background())
	if len(r.syncs) != 1 || r.syncs[0] != start {
		t.Errorf("syncs = %v", r.syncs)
	}
}

func TestTheDailySyncRunsOnlyWhenADayHasPassed(t *testing.T) {
	r := newDutyRig(t)
	r.accountSyncedAt(t, r.now.Add(-3*time.Hour))
	r.duties.Periodic(context.Background())
	if len(r.syncs) != 0 {
		t.Fatal("synced data three hours old")
	}
	r.now = r.now.Add(22 * time.Hour)
	r.duties.Periodic(context.Background())
	if len(r.syncs) != 1 {
		t.Fatalf("%d syncs a day later, want 1", len(r.syncs))
	}
}

func TestPausedDutiesDoNothing(t *testing.T) {
	r := newDutyRig(t)
	for _, name := range []string{"sync", "login", "mod", "updates"} {
		if res := r.do(t, "duty-pause", name); !res.OK {
			t.Fatal(res.Message)
		}
	}
	r.login(r.now.Add(-time.Hour)) // lapsed
	r.duties.AfterGame(context.Background())
	r.duties.Periodic(context.Background())
	if len(r.syncs) != 0 || len(r.notices) != 0 {
		t.Errorf("syncs %v, notices %v", r.syncs, r.notices)
	}
	row := r.row(t, "duties")
	if row.State != stateWarn || !strings.Contains(row.Summary, "syncing") || primaryArg(row, "duty-resume") != 4 {
		t.Errorf("row = %+v", row)
	}
	if res := r.do(t, "duty-resume", "sync"); !res.OK {
		t.Fatal(res.Message)
	}
	r.duties.AfterGame(context.Background())
	if len(r.syncs) != 1 {
		t.Error("resumed syncing did not sync")
	}
	if res := r.do(t, "duty-pause", "everything"); res.OK {
		t.Error("an unknown duty was paused")
	}
}

func primaryArg(r Row, id string) int {
	n := 0
	for _, a := range r.Actions {
		if a.ID == id {
			n++
		}
	}
	return n
}

func TestALapsedLoginIsNoticedOnceADay(t *testing.T) {
	r := newDutyRig(t)
	r.accountSyncedAt(t, r.now)
	r.login(r.now.Add(-time.Hour))
	r.duties.Periodic(context.Background())
	r.duties.Periodic(context.Background())
	if len(r.notices) != 1 || r.notices[0].Action != "relogin" || r.notices[0].ActionLabel != "Log in again" {
		t.Fatalf("notices = %+v", r.notices)
	}
	r.now = r.now.Add(25 * time.Hour)
	r.accountSyncedAt(t, r.now)
	r.duties.Periodic(context.Background())
	if len(r.notices) != 2 {
		t.Errorf("%d notices a day later, want 2", len(r.notices))
	}
}

func TestLoggingOutIsNotALapse(t *testing.T) {
	r := newDutyRig(t)
	r.accountSyncedAt(t, r.now)
	r.duties.Periodic(context.Background())
	if len(r.notices) != 0 {
		t.Errorf("notices = %+v", r.notices)
	}
}

func TestTheModComesBackAfterAGameUpdate(t *testing.T) {
	r := newDutyRig(t)
	r.modPackage(t, "0.3.0")
	r.do(t, "mod-install", "")
	write(t, filepath.Join(r.game, "version.xml"), "<version.xml><version> v.2.5.0.0 #1 </version></version.xml>")
	r.duties.Mod(context.Background())
	if _, err := os.Stat(filepath.Join(r.game, "mods", "2.5.0.0", "ondrejkouril.wotctx_0.3.0.wotmod")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(r.row(t, "duties").Lines, "\n"), "2.5.0.0") {
		t.Error("the row does not say what the upkeep did")
	}
}
