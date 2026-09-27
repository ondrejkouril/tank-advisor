// Package release signs and checks Tank Advisor's releases
// (docs/spec-desktop.md section 9). A release carries SHA256SUMS and, for the
// app's executable, an Ed25519 signature over its SHA-256 digest: the scheme
// Wails' updater verifies ("ed25519" over the digest), with the public key
// compiled into the app. The private key exists only in a repository secret
// and the maintainer's offline backup.
package release

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Keys are encoded as standard base64: the private key as its 32-byte seed,
// the public key as its 32 bytes.

// GenerateKey makes a new key pair, returned encoded.
func GenerateKey() (public, private string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv.Seed()), nil
}

// ParsePublicKey decodes a public key. Surrounding space is ignored, so a key
// file can end with a newline.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key: %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

func parsePrivateKey(s string) (ed25519.PrivateKey, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("private key: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("private key: %d bytes, want a %d-byte seed", len(seed), ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// Digest is a file's SHA-256.
func Digest(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// Sign signs a file's digest and returns the signature, base64-encoded: the
// contents of its .sig file.
func Sign(path, privateKey string) (string, error) {
	priv, err := parsePrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	digest, err := Digest(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, digest)), nil
}

// ParseSignature decodes a .sig file's contents.
func ParseSignature(s string) ([]byte, error) {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("signature: %d bytes, want %d", len(sig), ed25519.SignatureSize)
	}
	return sig, nil
}

// VerifyDigest checks a signature over a digest.
func VerifyDigest(digest []byte, signature string, publicKey ed25519.PublicKey) error {
	sig, err := ParseSignature(signature)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, digest, sig) {
		return errors.New("the signature does not match: this file was not signed by the Tank Advisor release key")
	}
	return nil
}

// Sums writes SHA256SUMS lines ("<hex>  <name>") for files, sorted by name.
func Sums(files []string) (string, error) {
	var lines []string
	for _, f := range files {
		d, err := Digest(f)
		if err != nil {
			return "", err
		}
		lines = append(lines, hex.EncodeToString(d)+"  "+filepath.Base(f))
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i][66:] < lines[j][66:] })
	return strings.Join(lines, "\n") + "\n", nil
}

// SumFor finds a file's digest in SHA256SUMS contents.
func SumFor(sums, name string) ([]byte, error) {
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			d, err := hex.DecodeString(fields[0])
			if err != nil || len(d) != sha256.Size {
				return nil, fmt.Errorf("SHA256SUMS: a bad digest for %s", name)
			}
			return d, nil
		}
	}
	return nil, fmt.Errorf("SHA256SUMS has no line for %s", name)
}
