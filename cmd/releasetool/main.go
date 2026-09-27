// Command releasetool makes the release key and signs releases
// (docs/spec-desktop.md section 9.1). The release workflow runs it; the
// maintainer runs keygen once.
//
//	releasetool keygen                       print a new key pair
//	releasetool sign <file>                  print the file's signature; the private key comes from RELEASE_SIGNING_KEY
//	releasetool verify <file> <sig> <pubkey> check a signature
//	releasetool sums <file>...               print SHA256SUMS
package main

import (
	"fmt"
	"os"

	"github.com/ondrejkouril/tank-advisor/internal/release"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "releasetool:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: releasetool keygen | sign <file> | verify <file> <sig-file> <public-key-file> | sums <file>...")
	}
	switch args[0] {
	case "keygen":
		pub, priv, err := release.GenerateKey()
		if err != nil {
			return err
		}
		fmt.Printf("public key (commit it as cmd/tankadvisor/release.pub):\n%s\n\n", pub)
		fmt.Printf("private key (a repository secret, RELEASE_SIGNING_KEY, and an offline backup; nowhere else):\n%s\n", priv)
		return nil
	case "sign":
		key := os.Getenv("RELEASE_SIGNING_KEY")
		if len(args) != 2 || key == "" {
			return fmt.Errorf("sign <file>, with RELEASE_SIGNING_KEY set")
		}
		sig, err := release.Sign(args[1], key)
		if err != nil {
			return err
		}
		fmt.Println(sig)
		return nil
	case "verify":
		if len(args) != 4 {
			return fmt.Errorf("verify <file> <sig-file> <public-key-file>")
		}
		sig, err := os.ReadFile(args[2])
		if err != nil {
			return err
		}
		pubRaw, err := os.ReadFile(args[3])
		if err != nil {
			return err
		}
		pub, err := release.ParsePublicKey(string(pubRaw))
		if err != nil {
			return err
		}
		digest, err := release.Digest(args[1])
		if err != nil {
			return err
		}
		if err := release.VerifyDigest(digest, string(sig), pub); err != nil {
			return err
		}
		fmt.Println("signature OK")
		return nil
	case "sums":
		sums, err := release.Sums(args[1:])
		if err != nil {
			return err
		}
		fmt.Print(sums)
		return nil
	}
	return fmt.Errorf("unknown command %s", args[0])
}
