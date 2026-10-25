package module

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/platform"
)

// Theme captures the curated personalization subset: wallpaper, the active
// .theme file, accent color, and light/dark mode. It never reads or writes
// wholesale registry hives.
type Theme struct {
	provider platform.ThemeProvider
}

// NewTheme returns the production theme module.
func NewTheme() *Theme { return &Theme{provider: platform.SystemTheme{}} }

// ThemeManifest is written to theme.json.
type ThemeManifest struct {
	CapturedAt    time.Time           `json:"captured_at"`
	Theme         platform.ThemeState `json:"theme"`
	WallpaperFile string              `json:"wallpaper_file,omitempty"`
	ThemeFile     string              `json:"theme_file,omitempty"`
	OriginalTheme string              `json:"original_theme,omitempty"`
}

// Name implements core.Module.
func (t *Theme) Name() string { return "theme" }

// Description implements core.Module.
func (t *Theme) Description() string {
	return "Wallpaper, .theme file, accent color, and light/dark mode from curated HKCU keys"
}

// NeedsAdmin implements core.Module.
func (t *Theme) NeedsAdmin() bool { return false }

// Scan implements core.Module.
func (t *Theme) Scan(ctx context.Context) (core.Summary, error) {
	p, err := env(ctx)
	if err != nil {
		return core.Summary{}, err
	}
	state, err := t.readTheme()
	if err != nil {
		if errors.Is(err, platform.ErrUnsupported) {
			return core.Summary{Details: "theme capture requires Windows"}, nil
		}
		return core.Summary{}, err
	}
	count := 1
	if state.Wallpaper != "" && fileExists(state.Wallpaper) {
		count++
	}
	if state.CurrentTheme != "" && fileExists(state.CurrentTheme) {
		count++
	}
	mode := "light"
	if state.AppsUseLightTheme == 0 {
		mode = "dark"
	}
	details := fmt.Sprintf("%s apps, accent #%06X", mode, state.AccentColor)
	if state.Wallpaper != "" {
		details += ", wallpaper"
	}
	if state.CurrentTheme != "" {
		details += ", .theme file"
	}
	p.Reportf("theme: %s", details)
	return core.Summary{ItemCount: count, Details: details}, nil
}

// Backup implements core.Module.
func (t *Theme) Backup(ctx context.Context, dir string) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	state, err := t.readTheme()
	if err != nil {
		return err
	}
	manifest := ThemeManifest{
		CapturedAt:    time.Now().UTC(),
		Theme:         state,
		OriginalTheme: state.CurrentTheme,
	}
	assets := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assets, 0o700); err != nil {
		return err
	}
	if state.Wallpaper != "" && fileExists(state.Wallpaper) {
		ext := safeAssetExt(state.Wallpaper, ".wallpaper")
		name := "wallpaper" + ext
		if err := core.CopyFile(state.Wallpaper, filepath.Join(assets, name)); err != nil {
			return fmt.Errorf("copy wallpaper: %w", err)
		}
		manifest.WallpaperFile = filepath.ToSlash(filepath.Join("assets", name))
	}
	if state.CurrentTheme != "" && fileExists(state.CurrentTheme) {
		name := "theme" + safeAssetExt(state.CurrentTheme, ".theme")
		if err := core.CopyFile(state.CurrentTheme, filepath.Join(assets, name)); err != nil {
			return fmt.Errorf("copy .theme file: %w", err)
		}
		manifest.ThemeFile = filepath.ToSlash(filepath.Join("assets", name))
	}
	if err := writeJSON(filepath.Join(dir, "theme.json"), manifest); err != nil {
		return err
	}
	p.Reportf("captured theme (accent #%06X)", state.AccentColor)
	return nil
}

// Restore implements core.Module.
func (t *Theme) Restore(ctx context.Context, dir string, opts core.RestoreOpts) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	var manifest ThemeManifest
	if err := readJSON(filepath.Join(dir, "theme.json"), &manifest); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read theme manifest: %w", err)
	}
	if opts.DryRun {
		p.Reportf("would restore wallpaper, accent color, and light/dark mode")
		return nil
	}
	restoreDir := filepath.Join(core.AppDataDir(), "restored", "theme")
	if err := os.MkdirAll(restoreDir, 0o700); err != nil {
		return err
	}
	state := manifest.Theme
	if manifest.WallpaperFile != "" {
		src := filepath.Join(dir, filepath.FromSlash(manifest.WallpaperFile))
		if !fileExists(src) {
			return fmt.Errorf("wallpaper file is missing from the backup")
		}
		dst := filepath.Join(restoreDir, "wallpaper"+safeAssetExt(manifest.WallpaperFile, ".wallpaper"))
		if err := core.CopyFile(src, dst); err != nil {
			return fmt.Errorf("restore wallpaper: %w", err)
		}
		state.Wallpaper = dst
	} else if state.Wallpaper != "" && !fileExists(state.Wallpaper) {
		state.Wallpaper = ""
	}
	if manifest.ThemeFile != "" {
		src := filepath.Join(dir, filepath.FromSlash(manifest.ThemeFile))
		if !fileExists(src) {
			return fmt.Errorf(".theme file is missing from the backup")
		}
		dst := filepath.Join(restoreDir, "theme"+safeAssetExt(manifest.ThemeFile, ".theme"))
		if err := core.CopyFile(src, dst); err != nil {
			return fmt.Errorf("restore .theme file: %w", err)
		}
		state.CurrentTheme = dst
	} else if state.CurrentTheme != "" && !fileExists(state.CurrentTheme) {
		state.CurrentTheme = ""
	}
	if t.provider == nil {
		t.provider = platform.SystemTheme{}
	}
	if err := t.provider.ApplyTheme(state); err != nil {
		return fmt.Errorf("apply theme: %w", err)
	}
	p.Reportf("restored wallpaper, accent color, and light/dark mode")
	return nil
}

// PreviewRestore implements core.Previewer.
func (t *Theme) PreviewRestore(ctx context.Context, dir string, opts core.RestoreOpts) (core.PreviewResult, error) {
	var manifest ThemeManifest
	if err := readJSON(filepath.Join(dir, "theme.json"), &manifest); err != nil {
		return core.PreviewResult{}, err
	}
	result := core.PreviewResult{
		ItemCount: 1,
		Details:   fmt.Sprintf("accent #%06X, dark=%v", manifest.Theme.AccentColor, manifest.Theme.AppsUseLightTheme == 0),
	}
	for _, rel := range []string{manifest.WallpaperFile, manifest.ThemeFile} {
		if rel == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
			result.Files++
			result.Bytes += info.Size()
		}
	}
	return result, nil
}

// Verify implements core.Module.
func (t *Theme) Verify(ctx context.Context, dir string) error {
	var manifest ThemeManifest
	if err := readJSON(filepath.Join(dir, "theme.json"), &manifest); err != nil {
		return fmt.Errorf("theme manifest: %w", err)
	}
	for _, rel := range []string{manifest.WallpaperFile, manifest.ThemeFile} {
		if rel != "" && !fileExists(filepath.Join(dir, filepath.FromSlash(rel))) {
			return fmt.Errorf("theme asset %s is missing", rel)
		}
	}
	return nil
}

func (t *Theme) readTheme() (platform.ThemeState, error) {
	if t.provider == nil {
		t.provider = platform.SystemTheme{}
	}
	state, err := t.provider.ReadTheme()
	if err != nil {
		return platform.ThemeState{}, fmt.Errorf("read theme settings: %w", err)
	}
	return state, nil
}

func safeAssetExt(path, fallback string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" || len(ext) > 12 || strings.ContainsAny(ext, `/\`) {
		return fallback
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return fallback
		}
	}
	return ext
}
