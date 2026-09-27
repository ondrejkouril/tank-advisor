package config

import "github.com/ondrejkouril/tank-advisor/internal/yamledit"

// Update sets one key in config.yaml. Key is dotted ("account.account_id");
// a nil Value removes the key.
type Update = yamledit.Update

// Set applies updates to config.yaml at path, keeping every comment and the
// order of the keys it does not touch: config.yaml is written by people as
// well as by wotctx and the app (docs/spec-desktop.md section 10). A missing
// file is created. The result must still load, or nothing is written.
func Set(path string, updates ...Update) error {
	return yamledit.Set(path, func(tmp string) error {
		_, err := Load(tmp)
		return err
	}, updates...)
}
