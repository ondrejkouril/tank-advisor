package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/updater"

	"github.com/ondrejkouril/tank-advisor/internal/app"
	"github.com/ondrejkouril/tank-advisor/internal/release"
	"github.com/ondrejkouril/tank-advisor/internal/useragent"
)

// releasePublicKey is the release key's public half (docs/spec-desktop.md
// section 9), committed when the maintainer makes the key (plan step D0).
// Empty, the build updates nothing: it can only point at the release page.
//
//go:embed release.pub
var releasePublicKey string

// Release assets, as the release workflow names them.
const (
	assetApp  = "TankAdvisor.exe"
	assetSums = "SHA256SUMS"
	assetSig  = "TankAdvisor.exe.sig"
)

// signedReleases is the updater's source: GitHub Releases of this repository,
// where every release must carry SHA256SUMS and an Ed25519 signature of the
// app. Wails' own GitHub provider takes a checksum if it finds one and never
// a signature, so a release without one would install; this one refuses it.
type signedReleases struct {
	repository string
	baseURL    string // https://api.github.com, or a test server
	client     *http.Client
}

func (p *signedReleases) Name() string { return "github-signed" }

type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Name       string    `json:"name"`
	Body       string    `json:"body"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published_at"`
	HTMLURL    string    `json:"html_url"`
	Assets     []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Check finds the newest release newer than the running version. A
// pre-release build follows pre-releases too; a release build only releases.
func (p *signedReleases) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	var releases []ghRelease
	if err := p.getJSON(ctx, p.baseURL+"/repos/"+p.repository+"/releases?per_page=20", &releases); err != nil {
		return nil, err
	}
	withPre := app.IsPrerelease(req.CurrentVersion)
	var best *ghRelease
	for i := range releases {
		r := &releases[i]
		if r.Draft || (r.Prerelease && !withPre) || !app.NewerVersion(req.CurrentVersion, r.TagName) {
			continue
		}
		if best == nil || app.NewerVersion(best.TagName, r.TagName) {
			best = r
		}
	}
	if best == nil {
		return nil, nil
	}

	urls := map[string]string{}
	var size int64
	for _, a := range best.Assets {
		urls[a.Name] = a.URL
		if a.Name == assetApp {
			size = a.Size
		}
	}
	for _, need := range []string{assetApp, assetSums, assetSig} {
		if urls[need] == "" {
			return nil, fmt.Errorf("release %s has no %s, so it cannot be verified; not installing it", best.TagName, need)
		}
	}
	sums, err := p.getText(ctx, urls[assetSums])
	if err != nil {
		return nil, err
	}
	digest, err := release.SumFor(sums, assetApp)
	if err != nil {
		return nil, err
	}
	sigText, err := p.getText(ctx, urls[assetSig])
	if err != nil {
		return nil, err
	}
	sig, err := release.ParseSignature(sigText)
	if err != nil {
		return nil, err
	}
	return &updater.Release{
		Version:     strings.TrimPrefix(best.TagName, "v"),
		Name:        best.Name,
		Notes:       best.Body,
		PublishedAt: best.Published,
		Artifact:    updater.Artifact{Filename: assetApp, Filetype: "exe", Size: size, Platform: req.Platform, Arch: req.Arch},
		// The updater checks both: the file's SHA-256 against the sums, and
		// the signature over it against the key compiled in.
		Verification: &updater.Verification{DigestAlgo: "sha256", Digest: digest, SignatureAlgo: "ed25519", Signature: sig},
		Metadata:     map[string]any{"url": urls[assetApp], "page": best.HTMLURL},
	}, nil
}

func (p *signedReleases) Download(ctx context.Context, r *updater.Release, dst io.Writer, onProgress func(written, total int64)) error {
	url, _ := r.Metadata["url"].(string)
	resp, err := p.get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var written int64
	buf := make([]byte, 64<<10)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			written += int64(n)
			onProgress(written, r.Artifact.Size)
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (p *signedReleases) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", useragent.String)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, nil
}

func (p *signedReleases) getText(ctx context.Context, url string) (string, error) {
	resp, err := p.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(raw), err
}

func (p *signedReleases) getJSON(ctx context.Context, url string, out any) error {
	resp, err := p.get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// selfUpdate is app.UpdateInstaller over Wails' updater.
type selfUpdate struct {
	u       *updater.Updater
	current string
}

func (s selfUpdate) Latest(ctx context.Context) (app.Release, bool, error) {
	rel, err := s.u.Check(ctx)
	if err != nil {
		return app.Release{}, false, err
	}
	if rel == nil {
		// Up to date: the running version is the latest.
		return app.Release{Tag: s.current}, true, nil
	}
	page, _ := rel.Metadata["page"].(string)
	return app.Release{Tag: "v" + rel.Version, URL: page}, true, nil
}

func (s selfUpdate) Install(ctx context.Context) error {
	if err := s.u.DownloadAndInstall(ctx); err != nil {
		return err
	}
	return s.u.Restart(ctx)
}

// newSelfUpdate configures Wails' updater, or returns nil when this build
// carries no release key and so cannot verify anything.
func newSelfUpdate(u *updater.Updater, current string) (app.UpdateInstaller, error) {
	if strings.TrimSpace(releasePublicKey) == "" || !strings.HasPrefix(current, "v") {
		return nil, nil
	}
	key, err := release.ParsePublicKey(releasePublicKey)
	if err != nil {
		return nil, err
	}
	provider := &signedReleases{repository: repository, baseURL: "https://api.github.com", client: &http.Client{Timeout: 30 * time.Second}}
	if err := u.Init(updater.Config{
		CurrentVersion: strings.TrimPrefix(current, "v"),
		Providers:      []updater.Provider{provider},
		PublicKey:      key,
		Window:         updater.WindowNone,
	}); err != nil {
		return nil, err
	}
	return selfUpdate{u: u, current: current}, nil
}

// repository is where releases are published.
const repository = "ondrejkouril/tank-advisor"
