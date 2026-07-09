// Package config loads back-then's optional on-disk configuration.
//
// The config file lets a user set sensible personal defaults once — where the
// index lives, which folders to scan, how aggressively to split sessions, and
// how many results to show — instead of retyping flags on every invocation.
//
// It is deliberately dependency-free: the file is plain JSON so it parses with
// the standard library and stays readable. Every field is optional; a missing
// file is not an error, it just means "use the built-in defaults." Explicit
// command-line flags always win over config values, which in turn win over the
// compiled-in defaults.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileName is the config file's basename inside the back-then config dir.
const FileName = "config.json"

// Config holds user-tunable defaults. The zero value is valid and means
// "nothing configured"; callers should treat empty/zero fields as unset and
// fall back to their own defaults.
type Config struct {
	// DB is the index database path. Empty means use the per-user data dir.
	DB string
	// Roots are directories that `index`/`watch` scan when no paths are given
	// on the command line.
	Roots []string
	// Gap is the base inter-file gap that separates two sessions. Zero means
	// unset (use the sessions package default).
	Gap time.Duration
	// Limit caps how many results list-style commands show. Zero means unset
	// (use the per-command default); a negative value means "no limit."
	Limit int
	// LimitSet records whether Limit was present in the file, so that an
	// explicit 0 ("show none"/"all", depending on command) is distinguishable
	// from an omitted key.
	LimitSet bool
}

// fileShape is the JSON wire format. Durations are written as human strings
// ("2h", "90m") rather than raw nanoseconds so the file stays hand-editable.
type fileShape struct {
	DB    string   `json:"db"`
	Roots []string `json:"roots"`
	Gap   string   `json:"gap"`
	Limit *int     `json:"limit"`
}

// DefaultPath returns the resolved config file path
// (<user-config-dir>/back-then/config.json). It does not create anything and
// does not require the file to exist.
func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(base, "back-then", FileName), nil
}

// Load reads and parses the config file at path. A missing file yields the
// zero Config and no error, so callers can always call Load unconditionally.
// Malformed JSON or an unparseable duration is reported as an error so typos
// surface loudly rather than being silently ignored.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	return parse(data, path)
}

// parse decodes the JSON body into a Config, translating the string duration
// and pointer limit into the typed struct. src is only used for error context.
func parse(data []byte, src string) (Config, error) {
	var fs fileShape
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&fs); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", src, err)
	}

	cfg := Config{
		DB:    fs.DB,
		Roots: fs.Roots,
	}

	if fs.Gap != "" {
		d, err := time.ParseDuration(fs.Gap)
		if err != nil {
			return Config{}, fmt.Errorf("parse config %s: gap %q: %w", src, fs.Gap, err)
		}
		if d < 0 {
			return Config{}, fmt.Errorf("parse config %s: gap %q must not be negative", src, fs.Gap)
		}
		cfg.Gap = d
	}

	if fs.Limit != nil {
		cfg.Limit = *fs.Limit
		cfg.LimitSet = true
	}

	return cfg, nil
}
