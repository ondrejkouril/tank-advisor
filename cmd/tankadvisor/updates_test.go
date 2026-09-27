package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"

	"github.com/ondrejkouril/tank-advisor/internal/release"
)

// fakeGitHub serves releases the way the API does, with their assets.
type fakeGitHub struct {
	releases []map[string]any
	files    map[string][]byte
}

func (g *fakeGitHub) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases") {
			json.NewEncoder(w).Encode(g.releases)
			return
		}
		body, ok := g.files[strings.TrimPrefix(r.URL.Path, "/files/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// publish adds a release with the given assets, served from the fake.
func (g *fakeGitHub) publish(srv *httptest.Server, tag string, pre bool, assets map[string][]byte) {
	var list []map[string]any
	for name, body := range assets {
		key := tag + "/" + name
		g.files[key] = body
		list = append(list, map[string]any{"name": name, "size": len(body), "browser_download_url": srv.URL + "/files/" + key})
	}
	g.releases = append(g.releases, map[string]any{"tag_name": tag, "prerelease": pre, "assets": list, "html_url": "https://example.test/" + tag})
}

// signedAssets is a release as the workflow publishes it: the app, its sums,
// and its signature.
func signedAssets(t *testing.T, app []byte, privateKey string) map[string][]byte {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, assetApp)
	os.WriteFile(exe, app, 0o644)
	sums, err := release.Sums([]string{exe})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := release.Sign(exe, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{assetApp: app, assetSums: []byte(sums), assetSig: []byte(sig)}
}

// testHost is the least an updater needs of an app with no window.
type testHost struct{}

func (testHost) Emit(string, ...any) bool                              { return true }
func (testHost) OnEvent(string, func(any)) func()                      { return func() {} }
func (testHost) OpenWindow(updater.WindowOptions) updater.WindowHandle { return nil }
func (testHost) Quit()                                                 {}

func newTestUpdater(t *testing.T, srv *httptest.Server, current, publicKey string) *updater.Updater {
	t.Helper()
	key, err := release.ParsePublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	u := updater.New(testHost{})
	provider := &signedReleases{repository: "o/r", baseURL: srv.URL, client: srv.Client()}
	if err := u.Init(updater.Config{CurrentVersion: current, Providers: []updater.Provider{provider}, PublicKey: key, Window: updater.WindowNone}); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestASignedReleaseIsVerifiedAndStaged(t *testing.T) {
	pub, priv, _ := release.GenerateKey()
	g := &fakeGitHub{files: map[string][]byte{}}
	srv := g.serve(t)
	g.publish(srv, "v1.1.0", false, signedAssets(t, []byte("new app"), priv))

	u := newTestUpdater(t, srv, "1.0.0", pub)
	rel, err := u.Check(context.Background())
	if err != nil || rel == nil || rel.Version != "1.1.0" {
		t.Fatalf("Check = %+v, %v", rel, err)
	}
	if err := u.DownloadAndInstall(context.Background()); err != nil {
		t.Fatalf("a properly signed release was refused: %v", err)
	}
	staged, err := os.ReadFile(u.DownloadedPath())
	if err != nil || string(staged) != "new app" {
		t.Errorf("staged %q, %v", staged, err)
	}
}

func TestABadSignatureIsRefused(t *testing.T) {
	pub, _, _ := release.GenerateKey()
	_, otherPriv, _ := release.GenerateKey() // not the key the app trusts
	g := &fakeGitHub{files: map[string][]byte{}}
	srv := g.serve(t)
	g.publish(srv, "v1.1.0", false, signedAssets(t, []byte("tampered app"), otherPriv))

	u := newTestUpdater(t, srv, "1.0.0", pub)
	if _, err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := u.DownloadAndInstall(context.Background())
	if err == nil {
		t.Fatal("a release signed with another key was installed")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Errorf("error = %v", err)
	}
}

func TestAMissingSignatureIsRefused(t *testing.T) {
	pub, priv, _ := release.GenerateKey()
	g := &fakeGitHub{files: map[string][]byte{}}
	srv := g.serve(t)
	assets := signedAssets(t, []byte("new app"), priv)
	delete(assets, assetSig)
	g.publish(srv, "v1.1.0", false, assets)

	u := newTestUpdater(t, srv, "1.0.0", pub)
	rel, err := u.Check(context.Background())
	if err == nil && rel != nil {
		t.Fatalf("an unsigned release was offered: %+v", rel)
	}
	if err == nil || !strings.Contains(err.Error(), assetSig) {
		t.Errorf("error = %v", err)
	}
}

func TestPreReleasesAreOnlyForPreReleaseBuilds(t *testing.T) {
	_, priv, _ := release.GenerateKey()
	g := &fakeGitHub{files: map[string][]byte{}}
	srv := g.serve(t)
	g.publish(srv, "v1.1.0-rc.1", true, signedAssets(t, []byte("rc"), priv))
	p := &signedReleases{repository: "o/r", baseURL: srv.URL, client: srv.Client()}

	for current, want := range map[string]string{"1.0.0": "", "1.1.0-rc.0": "1.1.0-rc.1", "1.1.0-rc.1": ""} {
		rel, err := p.Check(context.Background(), updater.CheckRequest{CurrentVersion: current})
		got := ""
		if rel != nil {
			got = rel.Version
		}
		if err != nil || got != want {
			t.Errorf("from %s: offered %q (%v), want %q", current, got, err, want)
		}
	}
}

func TestTheReleaseAssetNames(t *testing.T) {
	// The release workflow publishes exactly these.
	if got := fmt.Sprint(assetApp, " ", assetSums, " ", assetSig); got != "TankAdvisor.exe SHA256SUMS TankAdvisor.exe.sig" {
		t.Errorf("assets = %s", got)
	}
}
