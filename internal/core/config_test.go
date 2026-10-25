package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultConfigValid(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatalf("default config is invalid: %v", err)
	}
	if _, err := c.Profile("daily"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Profile("full"); err != nil {
		t.Fatal(err)
	}
	if c.ActiveConflictMode() != OverwriteKeepBak {
		t.Fatalf("unexpected default conflict mode: %s", c.ActiveConflictMode())
	}
}

func TestConfigLoadMissingUsesDefaults(t *testing.T) {
	c, err := LoadConfigFrom(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultProfile != "daily" || len(c.Profiles) != 2 {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestConfigSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	c := DefaultConfig()
	c.RepoURL = "s3:https://s3.amazonaws.com/my-bucket/ditch"
	c.ResticPath = `C:\tools\restic.exe`
	c.AskPerItem = true
	if err := SaveConfigTo(path, c); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config permissions are too broad: %v", info.Mode())
	}
	loaded, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RepoURL != c.RepoURL || loaded.ResticPath != c.ResticPath {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}
	if loaded.ActiveConflictMode() != AskPerItem {
		t.Fatalf("AskPerItem did not resolve: %s", loaded.ActiveConflictMode())
	}
}

func TestConfigValidation(t *testing.T) {
	base := func() *Config {
		c := DefaultConfig()
		return c
	}
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"unknown module", func(c *Config) {
			c.Profiles["daily"] = Profile{EnabledModules: []string{"nope"}}
		}, "unknown module"},
		{"duplicate module", func(c *Config) {
			c.Profiles["daily"] = Profile{EnabledModules: []string{"winget", "winget"}}
		}, "twice"},
		{"absolute include", func(c *Config) {
			c.Profiles["daily"] = Profile{EnabledModules: []string{"files"}, IncludeGlobs: []string{`C:\Users\me\Documents`}}
		}, "relative"},
		{"parent traversal", func(c *Config) {
			c.Profiles["daily"] = Profile{EnabledModules: []string{"files"}, IncludeGlobs: []string{"../secret"}}
		}, "traverse"},
		{"no modules", func(c *Config) {
			c.Profiles["daily"] = Profile{}
		}, "no enabled modules"},
		{"bad conflict", func(c *Config) { c.ConflictMode = "explode" }, "conflict_mode"},
		{"missing default", func(c *Config) { c.DefaultProfile = "weekend" }, "not defined"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mutate(c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestConfigBadYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("profiles: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfigFrom(path)
	if err == nil {
		t.Fatal("expected a parse error")
	}
	var cfgErr *ConfigError
	if !asConfigError(err, &cfgErr) {
		t.Fatalf("expected ConfigError, got %T", err)
	}
}

func asConfigError(err error, target **ConfigError) bool {
	for err != nil {
		if ce, ok := err.(*ConfigError); ok {
			*target = ce
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
