package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SchemaVersion is the manifest schema written by this release.
const SchemaVersion = 1

// Manifest is written once per backup. It is stored both inside the encrypted
// restic repository and in the local app data directory for inspection.
type Manifest struct {
	SchemaVersion int                     `json:"schema_version"`
	BackupID      string                  `json:"backup_id"`
	ToolVersion   string                  `json:"tool_version"`
	Commit        string                  `json:"commit,omitempty"`
	GoVersion     string                  `json:"go_version"`
	OS            string                  `json:"os"`
	Arch          string                  `json:"arch"`
	Hostname      string                  `json:"hostname"`
	Username      string                  `json:"username"`
	WindowsBuild  int                     `json:"windows_build,omitempty"`
	StartedUTC    time.Time               `json:"started_utc"`
	CompletedUTC  time.Time               `json:"completed_utc"`
	DurationMS    int64                   `json:"duration_ms"`
	RepoInfo      string                  `json:"repo_info"`
	Modules       map[string]ModuleStatus `json:"modules"`
	Hashes        map[string]string       `json:"hashes"`
	SnapshotIDs   []string                `json:"snapshot_ids,omitempty"`
	Partial       bool                    `json:"partial"`
}

// ModuleStatus describes one module's result.
type ModuleStatus struct {
	Name       string   `json:"name"`
	Success    bool     `json:"success"`
	Skipped    bool     `json:"skipped,omitempty"`
	NeedsAdmin bool     `json:"needs_admin,omitempty"`
	Error      string   `json:"error,omitempty"`
	ItemCount  int      `json:"item_count"`
	Details    string   `json:"details,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	Snapshots  []string `json:"snapshots,omitempty"`
	Hash       string   `json:"hash,omitempty"`
	Verified   *bool    `json:"verified,omitempty"`
}

// WriteManifest writes a manifest atomically as indented JSON.
func WriteManifest(path string, m *Manifest) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	if m.Modules == nil {
		m.Modules = make(map[string]ModuleStatus)
	}
	if m.Hashes == nil {
		m.Hashes = make(map[string]string)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create manifest directory: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace manifest: %w", err)
	}
	return nil
}

// ReadManifest reads and validates a manifest.
func ReadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("manifest schema %d is newer than supported schema %d", m.SchemaVersion, SchemaVersion)
	}
	if m.Modules == nil {
		m.Modules = make(map[string]ModuleStatus)
	}
	if m.Hashes == nil {
		m.Hashes = make(map[string]string)
	}
	return &m, nil
}

// FindManifest searches root for the shallowest manifest.json.
func FindManifest(root string) (string, *Manifest, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != "manifest.json" {
			return nil
		}
		found = path
		return filepath.SkipAll
	})
	if err != nil {
		return "", nil, err
	}
	if found == "" {
		return "", nil, fmt.Errorf("manifest.json not found in restored backup")
	}
	m, err := ReadManifest(found)
	return found, m, err
}

// SortModuleStatuses returns statuses sorted by name for stable output.
func (m *Manifest) SortModuleStatuses() []ModuleStatus {
	out := make([]ModuleStatus, 0, len(m.Modules))
	for _, s := range m.Modules {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SummaryLine returns a compact one-line status for CLI output.
func (s ModuleStatus) SummaryLine() string {
	state := "ok"
	switch {
	case s.Skipped:
		state = "skipped"
	case !s.Success:
		state = "FAILED"
	}
	line := fmt.Sprintf("%-14s %-8s %4d items", s.Name, state, s.ItemCount)
	if s.Error != "" {
		line += " — " + s.Error
	} else if s.Details != "" {
		line += " — " + s.Details
	}
	return line
}

// AllSucceeded reports whether every module succeeded (skips count as success).
func (m *Manifest) AllSucceeded() bool {
	for _, s := range m.Modules {
		if !s.Success && !s.Skipped {
			return false
		}
	}
	return true
}

// BackupTags returns the standard tags for a metadata snapshot.
func BackupTags(backupID, kind string) []string {
	return []string{"backup=" + backupID, "kind=" + kind, "tool=ditch-windows"}
}

// SanitizeLogText removes newlines from command output before logging.
func SanitizeLogText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > 2000 {
		s = strings.ToValidUTF8(s[:1997], "") + "…"
	}
	return s
}

// ContextLogger returns a logger annotated with module/backup context.
func ContextLogger(ctx context.Context, module string) Logger {
	p := ParamsFrom(ctx)
	if p == nil || p.Logger == nil {
		return NopLogger()
	}
	if module == "" {
		return p.Logger
	}
	return p.Logger.WithContext(module)
}
