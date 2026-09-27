package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/cli"
	"github.com/ondrejkouril/tank-advisor/internal/overlay"
	"github.com/ondrejkouril/tank-advisor/internal/yamledit"
)

// The Goals and Advice pages edit wot-overlay.yaml, which stays a file people
// edit by hand (docs/spec-desktop.md section 5.5). Every write goes through
// yamledit.Apply, so only the lines a change touches are rewritten; a page
// saved unchanged writes nothing. A file that does not validate is never
// overwritten, and a file changed on disk since the page loaded it is not
// overwritten either: the page reloads instead.

// overlayFile is the overlay as a page loaded it.
type overlayFile struct {
	path    string
	raw     []byte // nil when there is no file yet
	version string // a hash of raw; "" for no file
	parsed  *overlay.Overlay
	// problem is why the file cannot be edited from the app, or "".
	problem string
}

func versionOf(raw []byte) string {
	if raw == nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

func (s *Service) loadOverlay() (*cli.Env, overlayFile) {
	env := s.opts.NewEnv(io.Discard, io.Discard)
	f := overlayFile{path: env.Config.OverlayFile(env.Paths)}
	raw, err := os.ReadFile(f.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		f.parsed = &overlay.Overlay{}
		return env, f
	case err != nil:
		f.problem = err.Error()
		return env, f
	}
	f.raw, f.version = raw, versionOf(raw)
	o, findings := overlay.Parse(raw)
	if errs := errorFindings(findings); len(errs) > 0 {
		f.problem = "Your settings file has errors, so Tank Advisor will not change it until they are fixed: " + strings.Join(errs, "; ")
	}
	if o == nil {
		o = &overlay.Overlay{}
	}
	f.parsed = o
	return env, f
}

func errorFindings(findings []overlay.Finding) []string {
	var errs []string
	for _, f := range findings {
		if f.Severity == overlay.SeverityError {
			errs = append(errs, f.String())
		}
	}
	return errs
}

// OverlayVersion is the settings file's current version, which a page polls
// to notice an edit made outside the app.
func (s *Service) OverlayVersion() string {
	_, f := s.loadOverlay()
	return f.version
}

// prepared is an overlay edit worked out but not written.
type prepared struct {
	file    overlayFile
	out     []byte
	changed bool
	parsed  *overlay.Overlay
	errs    []string
}

// prepareOverlay works out the file the updates produce, with its new
// updated_at, and whether it validates. Nothing is written.
func (s *Service) prepareOverlay(f overlayFile, updates []yamledit.Update) prepared {
	p := prepared{file: f}
	out, err := yamledit.Apply(f.raw, updates...)
	if err != nil {
		p.errs = []string{err.Error()}
		return p
	}
	p.changed = !bytes.Equal(out, f.raw)
	if p.changed {
		stamp := []yamledit.Update{{Key: "updated_at", Value: time.Now().UTC().Truncate(time.Second)}}
		if f.raw == nil || !hasKeyIn(out, "version") {
			stamp = append([]yamledit.Update{{Key: "version", Value: overlay.SchemaVersion}}, stamp...)
		}
		if out, err = yamledit.Apply(out, stamp...); err != nil {
			p.errs = []string{err.Error()}
			return p
		}
	}
	p.out = out
	o, findings := overlay.Parse(out)
	p.parsed = o
	p.errs = errorFindings(findings)
	return p
}

// saveOverlay writes a prepared edit, unless the file changed since the page
// loaded it (version), or the result does not validate, or extra (a check
// against the synced data, for tank names) refuses it.
func (s *Service) saveOverlay(ctx context.Context, version string, updates []yamledit.Update,
	extra func(ctx context.Context, o *overlay.Overlay) []string) Result {
	_, f := s.loadOverlay()
	switch {
	case f.version != version:
		return Result{Message: "The settings file was changed outside Tank Advisor since this page was opened. The page has been reloaded with it; make your change again."}
	case f.problem != "":
		return Result{Message: f.problem}
	}
	p := s.prepareOverlay(f, updates)
	if len(p.errs) > 0 {
		return Result{Message: "Not saved: " + strings.Join(p.errs, "; ")}
	}
	if !p.changed {
		return Result{OK: true, Message: "Nothing changed."}
	}
	if extra != nil {
		if errs := extra(ctx, p.parsed); len(errs) > 0 {
			return Result{Message: "Not saved: " + strings.Join(errs, "; ")}
		}
	}
	if err := yamledit.Write(f.path, p.out, nil); err != nil {
		return Result{Message: err.Error()}
	}
	return Result{OK: true, Message: "Saved. Claude reads it at its next answer; start a new conversation to be sure."}
}
