package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ModuleNames lists the modules supported by this release.
var ModuleNames = []string{"winget", "default-apps", "wifi", "fonts", "theme", "files"}

// Config holds all user-configurable settings.
type Config struct {
	// RepoURL is the restic repository URL: a local path, USB drive letter,
	// s3:... URL, or rclone:remote:path.
	RepoURL string `yaml:"repo_url"`
	// ResticPath optionally pins the restic executable.
	ResticPath string `yaml:"restic_path,omitempty"`
	// DefaultProfile names the profile used when none is supplied.
	DefaultProfile string `yaml:"default_profile"`
	// Profiles are named module/path selections.
	Profiles map[string]Profile `yaml:"profiles"`
	// ConflictMode is the restore conflict policy.
	ConflictMode ConflictMode `yaml:"conflict_mode"`
	// AskPerItem enables per-file prompts during restore.
	AskPerItem bool `yaml:"ask_per_item"`
	// VerifyAfterBackup runs a repository check after a successful backup.
	VerifyAfterBackup bool `yaml:"verify_after_backup"`
}

// Profile is a named backup selection.
type Profile struct {
	// EnabledModules lists module names to run.
	EnabledModules []string `yaml:"enabled_modules"`
	// IncludeGlobs are paths relative to the user's home directory. A glob's
	// static prefix is used, so Documents/** selects the Documents folder.
	IncludeGlobs []string `yaml:"include_globs"`
	// ExcludeGlobs are restic exclude patterns.
	ExcludeGlobs []string `yaml:"exclude_globs"`
}

// ConfigError indicates an invalid configuration.
type ConfigError struct{ Err error }

func (e *ConfigError) Error() string { return "config error: " + e.Err.Error() }
func (e *ConfigError) Unwrap() error { return e.Err }

// DefaultConfig returns a conservative, useful configuration.
func DefaultConfig() *Config {
	return &Config{
		DefaultProfile: "daily",
		Profiles: map[string]Profile{
			"daily": {
				EnabledModules: []string{"winget", "wifi", "fonts", "theme", "files"},
				IncludeGlobs: []string{
					"Documents", "Desktop", "Pictures", "Downloads",
				},
				ExcludeGlobs: []string{
					"**/node_modules/**",
					"**/.cache/**",
					"**/*.tmp",
					"**/$RECYCLE.BIN/**",
					"**/System Volume Information/**",
				},
			},
			"full": {
				EnabledModules: []string{"winget", "default-apps", "wifi", "fonts", "theme", "files"},
				IncludeGlobs: []string{
					"Documents", "Desktop", "Pictures", "Downloads",
				},
				ExcludeGlobs: []string{
					"**/node_modules/**",
					"**/.cache/**",
					"**/*.tmp",
					"**/$RECYCLE.BIN/**",
					"**/System Volume Information/**",
				},
			},
		},
		ConflictMode: OverwriteKeepBak,
	}
}

// LoadConfig loads the default config file. A missing file yields defaults.
func LoadConfig() (*Config, error) { return LoadConfigFrom(ConfigPath()) }

// LoadConfigFrom loads a config from an explicit path.
func LoadConfigFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	c := DefaultConfig()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, &ConfigError{Err: fmt.Errorf("parse %s: %w", path, err)}
	}
	normalizeConfig(c)
	if err := c.Validate(); err != nil {
		return nil, &ConfigError{Err: fmt.Errorf("%s: %w", path, err)}
	}
	return c, nil
}

func normalizeConfig(c *Config) {
	if c.Profiles == nil {
		c.Profiles = make(map[string]Profile)
	}
	if c.DefaultProfile == "" {
		c.DefaultProfile = "daily"
	}
	if c.ConflictMode == "" {
		c.ConflictMode = OverwriteKeepBak
	}
	if c.AskPerItem {
		c.ConflictMode = AskPerItem
	}
}

// SaveConfig writes the default config path.
func SaveConfig(c *Config) error { return SaveConfigTo(ConfigPath(), c) }

// SaveConfigTo writes a config atomically with user-only permissions.
func SaveConfigTo(path string, c *Config) error {
	if err := c.Validate(); err != nil {
		return &ConfigError{Err: err}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// Validate checks module names, paths, and conflict policy.
func (c *Config) Validate() error {
	if c.DefaultProfile != "" {
		if _, ok := c.Profiles[c.DefaultProfile]; !ok {
			return fmt.Errorf("default_profile %q is not defined", c.DefaultProfile)
		}
	}
	if len(c.Profiles) == 0 {
		return errors.New("at least one profile is required")
	}
	for name, p := range c.Profiles {
		if name == "" {
			return errors.New("profile name cannot be empty")
		}
		if len(p.EnabledModules) == 0 {
			return fmt.Errorf("profile %q has no enabled modules", name)
		}
		seen := make(map[string]bool)
		for _, m := range p.EnabledModules {
			if !KnownModule(m) {
				return fmt.Errorf("profile %q references unknown module %q", name, m)
			}
			if seen[m] {
				return fmt.Errorf("profile %q lists module %q twice", name, m)
			}
			seen[m] = true
		}
		for _, pattern := range p.IncludeGlobs {
			if strings.TrimSpace(pattern) == "" {
				return fmt.Errorf("profile %q contains an empty include path", name)
			}
			if isAbsoluteLike(pattern) {
				return fmt.Errorf("profile %q include path %q must be relative to the home directory", name, pattern)
			}
			if strings.Contains(pattern, "..") {
				return fmt.Errorf("profile %q include path %q may not traverse upwards", name, pattern)
			}
		}
	}
	switch c.ConflictMode {
	case OverwriteKeepBak, Skip, AskPerItem:
	default:
		return fmt.Errorf("invalid conflict_mode %q", c.ConflictMode)
	}
	return nil
}

// isAbsoluteLike rejects absolute paths written for either Windows or POSIX so
// config validation behaves the same when tests run on Linux CI.
func isAbsoluteLike(path string) bool {
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return true
	}
	if len(path) >= 2 && path[1] == ':' {
		c := path[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return true
		}
	}
	return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//")
}

// KnownModule reports whether name is a supported module.
func KnownModule(name string) bool {
	for _, n := range ModuleNames {
		if n == name {
			return true
		}
	}
	return false
}

// Profile returns a named profile.
func (c *Config) Profile(name string) (Profile, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	p, ok := c.Profiles[name]
	if !ok {
		names := make([]string, 0, len(c.Profiles))
		for n := range c.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)
		return Profile{}, fmt.Errorf("profile %q does not exist (have: %s)", name, strings.Join(names, ", "))
	}
	return p, nil
}

// ActiveConflictMode resolves AskPerItem into the effective mode.
func (c *Config) ActiveConflictMode() ConflictMode {
	if c.AskPerItem {
		return AskPerItem
	}
	if c.ConflictMode == "" {
		return OverwriteKeepBak
	}
	return c.ConflictMode
}
