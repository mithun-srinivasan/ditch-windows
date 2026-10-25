// Package demo provides safe, synthetic modules used by the Docker demo.
//
// The demo deliberately exercises the real restic repository implementation and
// the real files module, but replaces Windows-only integrations (winget, netsh,
// DISM, registry) with clearly labelled fixtures. Everything runs under one
// base directory so a container can throw the whole demo away.
package demo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/module"
)

// Modules returns the demo module set. files is the production module; it
// backs up and restores the synthetic demo home through restic exactly like a
// real machine would.
func Modules() []core.Module {
	return []core.Module{
		NewApps(),
		NewWifi(),
		module.NewFiles(),
		NewTheme(),
	}
}

// DemoProfile is the profile used by the container demo.
func DemoProfile() core.Profile {
	return core.Profile{
		EnabledModules: []string{"apps", "wifi", "files", "theme"},
		IncludeGlobs:   []string{"Documents", "Desktop", "Pictures", "Downloads"},
		ExcludeGlobs:   []string{"**/node_modules/**", "**/.cache/**", "**/*.tmp", "$RECYCLE.BIN", "System Volume Information"},
	}
}

// SeedHome creates a small, obviously fake home directory. Existing files are
// left alone so an interactive demo can be stopped and resumed.
func SeedHome(home string) error {
	files := map[string]string{
		"Documents/welcome.txt":              "Welcome to the ditch-windows Docker demo.\nThis file is synthetic. It is not taken from your computer.\n",
		"Documents/notes/todo.md":            "# Demo notes\n\n- [x] Install restic\n- [x] Back up the demo home\n- [ ] Preview a restore\n",
		"Desktop/readme.txt":                 "Pretend this is a desktop shortcut or note.\n",
		"Pictures/photo-notes.txt":           "The demo does not include real photos.\n",
		"Downloads/sample-download.txt":      "A sample download, safely generated inside the container.\n",
		"Documents/node_modules/ignored.txt": "This directory is excluded by the demo profile.\n",
		"Documents/scratch.tmp":              "Temporary files are excluded.\n",
	}
	for rel, contents := range files {
		path := filepath.Join(home, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// SimulateLocalEdit changes one demo file after a backup so a restore preview
// has a genuine conflict to report. It only ever writes inside home.
func SimulateLocalEdit(home string) error {
	path := filepath.Join(home, "Documents", "welcome.txt")
	return os.WriteFile(path, []byte("Edited locally after the demo backup.\nA restore should keep this as a .bak file.\n"), 0o600)
}

// DemoApps is a synthetic stand-in for the winget module.
type DemoApps struct{}

func NewApps() *DemoApps { return &DemoApps{} }

func (*DemoApps) Name() string        { return "apps" }
func (*DemoApps) Description() string { return "Demo app inventory (real build uses winget)" }
func (*DemoApps) NeedsAdmin() bool    { return false }

type appRecord struct {
	PackageID string `json:"package_id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Publisher string `json:"publisher"`
	Source    string `json:"source"`
}

var demoApps = []appRecord{
	{PackageID: "Git.Git", Name: "Git", Version: "2.45.2", Publisher: "The Git Development Community", Source: "winget"},
	{PackageID: "Microsoft.VisualStudioCode", Name: "Visual Studio Code", Version: "1.90.0", Publisher: "Microsoft Corporation", Source: "winget"},
	{PackageID: "Mozilla.Firefox", Name: "Firefox", Version: "127.0", Publisher: "Mozilla", Source: "winget"},
	{PackageID: "9NKSQGP7F2NH", Name: "WhatsApp", Version: "2.2410", Publisher: "WhatsApp Inc.", Source: "msstore"},
	{PackageID: "Scoop:ripgrep", Name: "ripgrep", Version: "14.1.0", Publisher: "BurntSushi", Source: "scoop"},
	{PackageID: "manual:vendor-driver", Name: "Vendor Laptop Utility", Version: "3.2.1", Publisher: "Example OEM", Source: "manual"},
}

func (*DemoApps) Scan(context.Context) (core.Summary, error) {
	return core.Summary{ItemCount: len(demoApps), Details: "3 winget, 2 store/scoop, 1 manual reinstall"}, nil
}

func (*DemoApps) Backup(ctx context.Context, dir string) error {
	p := core.ParamsFrom(ctx)
	if p == nil {
		return errors.New("demo apps: runner params missing")
	}
	if err := writeJSON(filepath.Join(dir, "apps.json"), map[string]any{
		"captured_at": time.Now().UTC(),
		"source":      "synthetic demo data",
		"apps":        demoApps,
	}); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "inventory.json"), map[string]any{
		"captured_at": time.Now().UTC(),
		"bucket_counts": map[string]int{
			"winget_reinstallable": 3,
			"store_scoop_choco":    2,
			"manual":               1,
		},
		"apps": demoApps,
	}); err != nil {
		return err
	}
	markdown := "# Manual reinstall list (demo)\n\nThese entries demonstrate apps winget cannot reinstall.\n\n| Name | Version | Publisher | Why manual |\n|---|---|---|---|\n| Vendor Laptop Utility | 3.2.1 | Example OEM | Vendor-only installer |\n"
	if err := os.WriteFile(filepath.Join(dir, "manual-reinstall.md"), []byte(markdown), 0o600); err != nil {
		return err
	}
	p.Reportf("demo app inventory: %d apps across 3 buckets", len(demoApps))
	return nil
}

func (*DemoApps) Restore(ctx context.Context, dir string, opts core.RestoreOpts) error {
	p := core.ParamsFrom(ctx)
	if p == nil {
		return errors.New("demo apps: runner params missing")
	}
	var payload struct {
		Apps []appRecord `json:"apps"`
	}
	if err := readJSON(filepath.Join(dir, "apps.json"), &payload); err != nil {
		return err
	}
	if opts.DryRun {
		p.Reportf("would reinstall %d demo apps via winget", len(payload.Apps))
		return nil
	}
	return writeDemoMarker(p, "apps-restored.json", map[string]any{
		"restored_at": time.Now().UTC(),
		"apps":        len(payload.Apps),
		"note":        "synthetic demo marker; no winget was invoked",
	})
}

func (*DemoApps) Verify(_ context.Context, dir string) error {
	var payload struct {
		Apps []appRecord `json:"apps"`
	}
	if err := readJSON(filepath.Join(dir, "apps.json"), &payload); err != nil {
		return err
	}
	if len(payload.Apps) == 0 {
		return errors.New("demo apps metadata contains no apps")
	}
	return nil
}

// DemoTheme is a synthetic stand-in for the Windows theme module.
type DemoTheme struct{}

func NewTheme() *DemoTheme { return &DemoTheme{} }

func (*DemoTheme) Name() string        { return "theme" }
func (*DemoTheme) Description() string { return "Demo wallpaper and accent (real build reads HKCU)" }
func (*DemoTheme) NeedsAdmin() bool    { return false }

type themeRecord struct {
	AccentColor string `json:"accent_color"`
	DarkMode    bool   `json:"dark_mode"`
	Wallpaper   string `json:"wallpaper_asset"`
}

func (*DemoTheme) Scan(context.Context) (core.Summary, error) {
	return core.Summary{ItemCount: 1, Details: "1 wallpaper, dark mode, blue accent"}, nil
}

func (*DemoTheme) Backup(ctx context.Context, dir string) error {
	p := core.ParamsFrom(ctx)
	if p == nil {
		return errors.New("demo theme: runner params missing")
	}
	assetDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetDir, 0o700); err != nil {
		return err
	}
	wallpaper := "# Synthetic demo wallpaper\n\nditch-windows Docker demo\n"
	if err := os.WriteFile(filepath.Join(assetDir, "wallpaper.txt"), []byte(wallpaper), 0o600); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "theme.json"), themeRecord{
		AccentColor: "#0A84FF",
		DarkMode:    true,
		Wallpaper:   "assets/wallpaper.txt",
	}); err != nil {
		return err
	}
	p.Reportf("captured demo wallpaper and accent colour")
	return nil
}

func (*DemoTheme) Restore(ctx context.Context, dir string, opts core.RestoreOpts) error {
	p := core.ParamsFrom(ctx)
	if p == nil {
		return errors.New("demo theme: runner params missing")
	}
	var t themeRecord
	if err := readJSON(filepath.Join(dir, "theme.json"), &t); err != nil {
		return err
	}
	dest := filepath.Join(p.HomeDir, ".ditch-demo", "theme")
	if opts.DryRun {
		p.Reportf("would restore wallpaper and dark mode (accent %s) to %s", t.AccentColor, dest)
		return nil
	}
	if _, err := core.ApplyTree(filepath.Join(dir, "assets"), dest, core.ApplyOptions{
		Mode: opts.EffectiveMode(),
		Ask:  opts.Ask,
	}); err != nil {
		return err
	}
	return writeDemoMarker(p, "theme-restored.json", map[string]any{
		"restored_at": time.Now().UTC(),
		"accent":      t.AccentColor,
		"dark_mode":   t.DarkMode,
		"note":        "synthetic marker; no registry was changed",
	})
}

func (*DemoTheme) Verify(_ context.Context, dir string) error {
	var t themeRecord
	if err := readJSON(filepath.Join(dir, "theme.json"), &t); err != nil {
		return err
	}
	if t.Wallpaper == "" {
		return errors.New("demo theme metadata has no wallpaper")
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(t.Wallpaper))); err != nil {
		return fmt.Errorf("demo theme wallpaper: %w", err)
	}
	return nil
}

func writeDemoMarker(p *core.Params, name string, value any) error {
	dest := filepath.Join(p.HomeDir, ".ditch-demo")
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dest, name), value)
}

func writeJSON(path string, value any) error {
	data, err := marshalJSON(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return unmarshalJSON(data, value)
}
