package core

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const appDirName = "ditch-windows"

// AppDataDir returns ditch-windows' per-user data directory. Set
// DITCH_WINDOWS_HOME to override it (used by tests and portable installs).
func AppDataDir() string {
	if v := os.Getenv("DITCH_WINDOWS_HOME"); v != "" {
		return v
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, appDirName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "."+appDirName)
	}
	return filepath.Join(home, ".config", appDirName)
}

// ConfigPath returns the default configuration file path.
func ConfigPath() string { return filepath.Join(AppDataDir(), "config.yaml") }

// LogsDir returns the directory that holds structured log files.
func LogsDir() string { return filepath.Join(AppDataDir(), "logs") }

// BackupsDir returns the directory that holds locally inspectable manifests.
func BackupsDir() string { return filepath.Join(AppDataDir(), "backups") }

// TempRoot returns the application-private temporary directory. It is created
// with restrictive permissions on first use.
func TempRoot() (string, error) {
	dir := filepath.Join(AppDataDir(), "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create temp directory: %w", err)
	}
	return dir, nil
}

// DefaultRepoPath is the suggested local restic repository location.
func DefaultRepoPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "ditch-windows-repo")
	}
	return filepath.Join(home, "ditch-windows-repo")
}

// BackupDir returns the local directory for one backup's manifest.
func BackupDir(id string) string { return filepath.Join(BackupsDir(), id) }

// NewBackupID creates a sortable, human-readable backup identifier.
func NewBackupID(t time.Time) string {
	return t.UTC().Format("20060102T150405Z") + "-" + randomHex(3)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := readRandom(buf); err != nil {
		return fmt.Sprintf("%06x", time.Now().UnixNano()&0xffffff)
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, b := range buf {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0xf]
	}
	return string(out)
}
