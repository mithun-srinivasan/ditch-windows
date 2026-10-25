package module

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

// Wifi captures Wi-Fi profiles, including the key in clear text, but never
// writes a profile XML to unencrypted staging. The XML lives only in a private
// temporary directory and in the encrypted restic repository.
type Wifi struct {
	snapshots []string
}

// NewWifi returns the Wi-Fi module.
func NewWifi() *Wifi { return &Wifi{} }

// WifiManifest is the non-secret metadata stored in the manifest snapshot.
type WifiManifest struct {
	CapturedAt time.Time `json:"captured_at"`
	Profiles   []string  `json:"profiles"`
	Count      int       `json:"count"`
}

// Name implements core.Module.
func (w *Wifi) Name() string { return "wifi" }

// Description implements core.Module.
func (w *Wifi) Description() string {
	return "Wi-Fi profiles exported with keys, stored only in the encrypted repository"
}

// NeedsAdmin implements core.Module.
func (w *Wifi) NeedsAdmin() bool { return false }

// Scan implements core.Module.
func (w *Wifi) Scan(ctx context.Context) (core.Summary, error) {
	p, err := env(ctx)
	if err != nil {
		return core.Summary{}, err
	}
	if _, err := p.Exec.LookPath("netsh"); err != nil {
		return core.Summary{Details: "Wi-Fi profiles are only available on Windows"}, nil
	}
	res, err := p.Exec.Run(ctx, "netsh", "wlan", "show", "profiles")
	if err != nil {
		return core.Summary{}, fmt.Errorf("netsh wlan show profiles: %s", commandError(err, res))
	}
	profiles := ParseWifiProfiles(res.Stdout)
	return core.Summary{
		ItemCount: len(profiles),
		Details:   fmt.Sprintf("%d Wi-Fi profile(s); keys are stored encrypted", len(profiles)),
	}, nil
}

// Backup implements core.Module.
func (w *Wifi) Backup(ctx context.Context, dir string) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	w.snapshots = nil
	if _, err := p.Exec.LookPath("netsh"); err != nil {
		return fmt.Errorf("netsh is not available; Wi-Fi backup requires Windows")
	}
	if p.Repo == nil {
		return fmt.Errorf("wifi: repository is not configured")
	}

	secureDir, err := secureTempDir(p.TempDir, "wifi-export-")
	if err != nil {
		return err
	}
	defer func() { _ = SecureWipeDir(secureDir) }()

	p.Reportf("exporting Wi-Fi profiles with keys (encrypted at rest)")
	res, err := p.Exec.Run(ctx, "netsh", "wlan", "export", "profile", "key=clear", "folder="+secureDir)
	if err != nil {
		return fmt.Errorf("netsh wlan export failed: %s", commandError(err, res))
	}

	xmlFiles, err := findFilesByExtension(secureDir, ".xml")
	if err != nil {
		return err
	}
	profiles := make([]string, 0, len(xmlFiles))
	for _, file := range xmlFiles {
		name, err := ProfileNameFromXMLFile(file)
		if err != nil {
			continue
		}
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)
	manifest := WifiManifest{CapturedAt: time.Now().UTC(), Profiles: profiles, Count: len(profiles)}
	if err := writeJSON(filepath.Join(dir, "wifi.json"), manifest); err != nil {
		return err
	}
	if len(xmlFiles) == 0 {
		p.Reportf("no Wi-Fi profiles found")
		return nil
	}

	snap, err := p.Repo.Backup(ctx, []string{secureDir}, core.BackupOptions{
		Tags: append(core.BackupTags(p.BackupID, "wifi"), "module=wifi"),
	})
	if err != nil {
		return fmt.Errorf("store encrypted Wi-Fi snapshot: %w", err)
	}
	w.snapshots = []string{snap.ID}
	p.Reportf("%d Wi-Fi profile(s) stored in encrypted repository", len(profiles))
	return nil
}

// Snapshots implements core.SnapshotProvider.
func (w *Wifi) Snapshots() []string { return append([]string(nil), w.snapshots...) }

// Restore implements core.Module.
func (w *Wifi) Restore(ctx context.Context, dir string, opts core.RestoreOpts) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	var manifest WifiManifest
	if err := readJSON(filepath.Join(dir, "wifi.json"), &manifest); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read Wi-Fi manifest: %w", err)
	}
	if opts.DryRun {
		p.Reportf("would restore %d Wi-Fi profile(s)", manifest.Count)
		return nil
	}
	if manifest.Count == 0 {
		return nil
	}
	if p.Manifest == nil {
		return fmt.Errorf("wifi: backup manifest is unavailable")
	}
	status, ok := p.Manifest.Modules[w.Name()]
	if !ok || len(status.Snapshots) == 0 {
		return fmt.Errorf("wifi: encrypted profile snapshot is missing from the backup")
	}
	if _, err := p.Exec.LookPath("netsh"); err != nil {
		return fmt.Errorf("netsh is not available; Wi-Fi restore requires Windows")
	}

	secureDir, err := secureTempDir(p.TempDir, "wifi-restore-")
	if err != nil {
		return err
	}
	defer func() { _ = SecureWipeDir(secureDir) }()

	if err := p.Repo.Restore(ctx, status.Snapshots[0], secureDir, core.RestoreOptions{}); err != nil {
		return fmt.Errorf("restore encrypted Wi-Fi snapshot: %w", err)
	}
	xmlFiles, err := findFilesByExtension(secureDir, ".xml")
	if err != nil {
		return err
	}
	if len(xmlFiles) == 0 {
		return fmt.Errorf("wifi: snapshot contains no profile XML files")
	}

	scope := "user=current"
	if core.IsElevated() {
		scope = "user=all"
	}
	var failures []error
	for _, file := range xmlFiles {
		name, _ := ProfileNameFromXMLFile(file)
		if name != "" {
			p.Reportf("adding Wi-Fi profile %q", name)
		}
		res, err := p.Exec.Run(ctx, "netsh", "wlan", "add", "profile", "filename="+file, scope)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %s", filepath.Base(file), commandError(err, res)))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("could not add %d Wi-Fi profile(s): %w", len(failures), errors.Join(failures...))
	}
	return nil
}

// Verify implements core.Module.
func (w *Wifi) Verify(ctx context.Context, dir string) error {
	var manifest WifiManifest
	if err := readJSON(filepath.Join(dir, "wifi.json"), &manifest); err != nil {
		return fmt.Errorf("wifi manifest: %w", err)
	}
	if manifest.Count != len(manifest.Profiles) {
		return fmt.Errorf("wifi manifest count %d does not match %d profiles", manifest.Count, len(manifest.Profiles))
	}
	return nil
}

// ParseWifiProfiles extracts profile names from "netsh wlan show profiles".
func ParseWifiProfiles(data []byte) []string {
	var out []string
	seen := make(map[string]bool)
	for _, line := range splitLines(data) {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "profile") {
			continue
		}
		name := lastAfterColon(line)
		if name == "" || strings.Contains(strings.ToLower(name), "profile") {
			continue
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

type wlanProfileXML struct {
	Name string `xml:"name"`
}

// ProfileNameFromXMLFile reads just the profile name from an exported XML.
// The file may contain a plaintext key; it is never logged.
func ProfileNameFromXMLFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	decoder := xml.NewDecoder(io.LimitReader(f, 1<<20))
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "name" {
			continue
		}
		var name string
		if err := decoder.DecodeElement(&name, &start); err != nil {
			return "", err
		}
		name = strings.TrimSpace(name)
		if name != "" {
			return name, nil
		}
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.TrimPrefix(base, "Wi-Fi-")
	return strings.TrimSpace(base), nil
}

func findFilesByExtension(root, ext string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ext) {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func secureTempDir(parent, pattern string) (string, error) {
	if parent != "" {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return "", err
		}
	}
	dir, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", fmt.Errorf("create secure temporary directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// SecureWipeDir overwrites regular files with zeros before deleting them. On
// SSDs and copy-on-write filesystems this is best effort; the primary defense
// is that profile XML never leaves this directory unencrypted.
func SecureWipeDir(dir string) error {
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			_ = wipeFile(path, info.Size())
		}
		return nil
	})
	removeErr := os.RemoveAll(dir)
	if err != nil {
		return err
	}
	return removeErr
}

func wipeFile(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	defer f.Close()
	zero := make([]byte, 32*1024)
	remaining := size
	for remaining > 0 {
		n := int64(len(zero))
		if remaining < n {
			n = remaining
		}
		if _, err := f.Write(zero[:n]); err != nil {
			break
		}
		remaining -= n
	}
	_ = f.Sync()
	return nil
}
