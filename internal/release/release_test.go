package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "TankAdvisor.exe")
	os.WriteFile(exe, []byte("the app"), 0o644)

	sig, err := Sign(exe, priv)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ParsePublicKey(pub + "\n")
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := Digest(exe)
	if err := VerifyDigest(digest, sig, key); err != nil {
		t.Fatalf("a good signature failed: %v", err)
	}

	// A changed file, another key, and a mangled signature all fail.
	os.WriteFile(exe, []byte("the app, changed"), 0o644)
	changed, _ := Digest(exe)
	if VerifyDigest(changed, sig, key) == nil {
		t.Error("a changed file verified")
	}
	otherPub, _, _ := GenerateKey()
	other, _ := ParsePublicKey(otherPub)
	if VerifyDigest(digest, sig, other) == nil {
		t.Error("another key verified")
	}
	if VerifyDigest(digest, "not base64!", key) == nil || VerifyDigest(digest, "", key) == nil {
		t.Error("a mangled or missing signature verified")
	}
}

func TestSums(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "b.exe"), filepath.Join(dir, "a.exe")
	os.WriteFile(a, []byte("b"), 0o644)
	os.WriteFile(b, []byte("a"), 0o644)
	sums, err := Sums([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(sums), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "  a.exe") {
		t.Fatalf("sums = %q", sums)
	}
	d, err := SumFor(sums, "b.exe")
	want, _ := Digest(a)
	if err != nil || string(d) != string(want) {
		t.Errorf("SumFor = %x, %v", d, err)
	}
	if _, err := SumFor(sums, "c.exe"); err == nil {
		t.Error("a missing file was found")
	}
}

func TestBadKeys(t *testing.T) {
	if _, err := ParsePublicKey("AAAA"); err == nil {
		t.Error("a short public key parsed")
	}
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, nil, 0o644)
	if _, err := Sign(f, "AAAA"); err == nil {
		t.Error("a short private key signed")
	}
}
