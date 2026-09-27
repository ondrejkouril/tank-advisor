package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ondrejkouril/tank-advisor/internal/useragent"
)

// repository is where releases are published (docs/spec-desktop.md section 9).
const repository = "ondrejkouril/tank-advisor"

// Release is the newest published release.
type Release struct {
	Tag string
	URL string
}

// UpdateChecker finds the newest release. found is false when none is
// published.
type UpdateChecker interface {
	Latest(ctx context.Context) (r Release, found bool, err error)
}

// UpdateResult is the last check's answer.
type UpdateResult struct {
	CheckedAt time.Time
	Latest    string
	URL       string
	Newer     bool
	// Note is the line shown when there is nothing to install.
	Note string
	Err  string
}

// GitHubUpdates asks GitHub's API for the latest release. The request
// carries no account data.
type GitHubUpdates struct {
	Client *http.Client
}

// Latest implements UpdateChecker.
func (g GitHubUpdates) Latest(ctx context.Context) (Release, bool, error) {
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+repository+"/releases/latest", nil)
	if err != nil {
		return Release{}, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", useragent.String)
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Release{}, false, nil
	default:
		return Release{}, false, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Release{}, false, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	return Release{Tag: body.TagName, URL: body.HTMLURL}, true, nil
}

func (s *Service) checkUpdates(ctx context.Context) Result {
	rel, found, err := s.opts.Updates.Latest(ctx)
	u := UpdateResult{CheckedAt: time.Now()}
	switch {
	case err != nil:
		u.Err = err.Error()
	case !found:
		u.Note = "No release is published yet."
	default:
		u.Latest, u.URL = rel.Tag, rel.URL
		u.Newer, u.Note = compareRelease(s.opts.Version, rel.Tag)
	}
	s.mu.Lock()
	s.update = u
	s.mu.Unlock()

	switch {
	case u.Err != "":
		return Result{Message: "The update check failed: " + u.Err}
	case u.Newer:
		return Result{OK: true, Message: "Version " + u.Latest + " is available."}
	}
	return Result{OK: true, Message: u.Note}
}

func (s *Service) openRelease() Result {
	s.mu.Lock()
	url := s.update.URL
	s.mu.Unlock()
	if url == "" {
		url = "https://github.com/" + repository + "/releases"
	}
	return s.open(url)
}

// compareRelease says whether tag is newer than the running version, and
// what to show otherwise. A development build, whose version is a commit
// rather than a release number, is never offered an update over itself.
func compareRelease(current, tag string) (newer bool, note string) {
	cur, okCur := parseVersion(current)
	latest, okLatest := parseVersion(tag)
	switch {
	case !okLatest:
		return false, "The latest release, " + tag + ", has a version this app cannot compare."
	case !okCur:
		return false, "This is a development build; the latest release is " + tag + "."
	case compareVersions(latest, cur) > 0:
		return true, ""
	}
	return false, "Up to date: " + tag + " is the latest release."
}

// version is a release number: major, minor, patch, and whether it is a
// pre-release (v1.0.0-rc.1), which sorts before the release itself.
type version struct {
	parts [3]int
	pre   string
}

func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	core, pre, _ := strings.Cut(s, "-")
	fields := strings.Split(core, ".")
	if len(fields) != 3 {
		return version{}, false
	}
	var v version
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return version{}, false
		}
		v.parts[i] = n
	}
	v.pre = pre
	return v, true
}

func compareVersions(a, b version) int {
	for i := range a.parts {
		if a.parts[i] != b.parts[i] {
			if a.parts[i] > b.parts[i] {
				return 1
			}
			return -1
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	case a.pre > b.pre:
		return 1
	}
	return -1
}

// NewerVersion reports whether tag is a newer release than current. A
// development build, whose version is not a release number, is never
// offered one.
func NewerVersion(current, tag string) bool {
	newer, _ := compareRelease(current, tag)
	return newer
}

// IsPrerelease reports whether a version is a pre-release (v1.0.0-rc.1). A
// pre-release build follows pre-releases; a release follows releases only.
func IsPrerelease(v string) bool {
	parsed, ok := parseVersion(v)
	return ok && parsed.pre != ""
}

// UpdateInstaller is an UpdateChecker that can also install what it found:
// Wails' updater, in a build that carries the release key.
type UpdateInstaller interface {
	UpdateChecker
	Install(ctx context.Context) error
}

func (s *Service) installUpdate(ctx context.Context) Result {
	inst, ok := s.opts.Updates.(UpdateInstaller)
	if !ok {
		return s.openRelease()
	}
	if err := inst.Install(ctx); err != nil {
		return Result{Message: "The update did not install: " + err.Error()}
	}
	return Result{OK: true, Message: "Updated. Tank Advisor restarts now."}
}

// SetUpdates replaces the update source, for a source that exists only once
// the window toolkit has started (Wails' updater).
func (s *Service) SetUpdates(u UpdateChecker) {
	s.mu.Lock()
	s.opts.Updates = u
	s.mu.Unlock()
}
