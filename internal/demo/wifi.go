package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/module"
)

// DemoWifi mirrors the production Wi-Fi module's secret handling: the XML with
// a password is written only to a private temp directory, backed up directly
// into the encrypted restic repository, and wiped immediately. Only profile
// names are left in plaintext metadata.
type DemoWifi struct {
	snapshots []string
}

func NewWifi() *DemoWifi { return &DemoWifi{} }

func (*DemoWifi) Name() string        { return "wifi" }
func (*DemoWifi) Description() string { return "Demo Wi-Fi profiles with encrypted-only secrets" }
func (*DemoWifi) NeedsAdmin() bool    { return false }

func (*DemoWifi) Scan(context.Context) (core.Summary, error) {
	return core.Summary{ItemCount: 2, Details: "2 synthetic Wi-Fi profiles; XML stays encrypted"}, nil
}

func (w *DemoWifi) Snapshots() []string { return append([]string(nil), w.snapshots...) }

const demoSecureWifiXML = `<?xml version="1.0" encoding="UTF-8"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1">
  <name>DitchDemoSecure</name>
  <SSIDConfig><SSID><name>DitchDemoSecure</name></SSID></SSIDConfig>
  <connectionType>ESS</connectionType>
  <MSM>
    <security>
      <authEncryption><authentication>WPA2PSK</authentication><encryption>AES</encryption></authEncryption>
      <sharedKey><keyType>passPhrase</keyType><protected>false</protected><keyMaterial>DEMO-ONLY-NOT-A-REAL-PASSWORD</keyMaterial></sharedKey>
    </security>
  </MSM>
</WLANProfile>
`

func (w *DemoWifi) Backup(ctx context.Context, dir string) error {
	p := core.ParamsFrom(ctx)
	if p == nil {
		return errors.New("demo wifi: runner params missing")
	}
	if p.Repo == nil {
		return errors.New("demo wifi: repository missing")
	}
	if err := os.MkdirAll(p.TempDir, 0o700); err != nil {
		return err
	}
	secureDir, err := os.MkdirTemp(p.TempDir, "demo-wifi-")
	if err != nil {
		return err
	}
	defer func() { _ = module.SecureWipeDir(secureDir) }()
	if err := os.WriteFile(filepath.Join(secureDir, "Wi-Fi-DitchDemoSecure.xml"), []byte(demoSecureWifiXML), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(secureDir, "Wi-Fi-DitchDemoGuest.xml"), []byte(strings.ReplaceAll(demoSecureWifiXML, "DitchDemoSecure", "DitchDemoGuest")), 0o600); err != nil {
		return err
	}
	snap, err := p.Repo.Backup(ctx, []string{secureDir}, core.BackupOptions{
		Tags: append(core.BackupTags(p.BackupID, "wifi"), "module=wifi"),
		Host: "ditch-demo",
	})
	if err != nil {
		return err
	}
	w.snapshots = []string{snap.ID}
	if err := writeJSON(filepath.Join(dir, "wifi.json"), map[string]any{
		"captured_at": time.Now().UTC(),
		"profiles":    []string{"DitchDemoSecure", "DitchDemoGuest"},
		"count":       2,
		"note":        "passwords exist only inside the encrypted restic snapshot",
	}); err != nil {
		return err
	}
	p.Reportf("exported 2 synthetic Wi-Fi profiles into encrypted storage; secure temp wiped")
	return nil
}

func (w *DemoWifi) Restore(ctx context.Context, dir string, opts core.RestoreOpts) error {
	p := core.ParamsFrom(ctx)
	if p == nil {
		return errors.New("demo wifi: runner params missing")
	}
	var meta struct {
		Profiles []string `json:"profiles"`
	}
	if err := readJSON(filepath.Join(dir, "wifi.json"), &meta); err != nil {
		return err
	}
	if opts.DryRun {
		p.Reportf("would add %d demo Wi-Fi profiles: %s", len(meta.Profiles), strings.Join(meta.Profiles, ", "))
		return nil
	}
	snapshot := ""
	if p.Manifest != nil {
		if status, ok := p.Manifest.Modules["wifi"]; ok && len(status.Snapshots) > 0 {
			snapshot = status.Snapshots[0]
		}
	}
	if snapshot == "" && p.BackupID != "" {
		snaps, err := p.Repo.List(ctx, []string{"module=wifi", "backup=" + p.BackupID})
		if err != nil {
			return err
		}
		if len(snaps) > 0 {
			snapshot = snaps[0].ID
		}
	}
	if snapshot == "" {
		return errors.New("demo wifi: snapshot not found")
	}
	if err := os.MkdirAll(p.TempDir, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(p.TempDir, "demo-wifi-restore-")
	if err != nil {
		return err
	}
	defer func() { _ = module.SecureWipeDir(stage) }()
	if err := p.Repo.Restore(ctx, snapshot, stage, core.RestoreOptions{}); err != nil {
		return err
	}
	xmls, err := filesWithSuffix(stage, ".xml")
	if err != nil {
		return err
	}
	if len(xmls) == 0 {
		return errors.New("demo wifi: no profiles in snapshot")
	}
	return writeDemoMarker(p, "wifi-restored.json", map[string]any{
		"restored_at": time.Now().UTC(),
		"profiles":    meta.Profiles,
		"xml_files":   len(xmls),
		"note":        "synthetic marker; netsh was not invoked and no secrets were written",
	})
}

func (*DemoWifi) Verify(_ context.Context, dir string) error {
	var meta struct {
		Profiles []string `json:"profiles"`
		Count    int      `json:"count"`
	}
	if err := readJSON(filepath.Join(dir, "wifi.json"), &meta); err != nil {
		return err
	}
	if meta.Count != len(meta.Profiles) {
		return fmt.Errorf("demo wifi: count %d does not match %d profiles", meta.Count, len(meta.Profiles))
	}
	return nil
}

func filesWithSuffix(root, suffix string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), suffix) {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func marshalJSON(value any) ([]byte, error) { return json.MarshalIndent(value, "", "  ") }

func unmarshalJSON(data []byte, value any) error { return json.Unmarshal(data, value) }
