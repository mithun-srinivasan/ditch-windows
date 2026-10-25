// Package platform isolates the small amount of Windows-specific state that
// ditch-windows reads and writes. Non-Windows stubs keep the rest of the
// application testable on Linux CI.
package platform

import "errors"

// ErrUnsupported is returned by Windows-only operations on other platforms.
var ErrUnsupported = errors.New("operation is only supported on Windows")

// Program is an entry from the Windows Uninstall registry keys.
type Program struct {
	DisplayName     string
	DisplayVersion  string
	Publisher       string
	InstallLocation string
	UninstallString string
	SystemComponent bool
	Hidden          bool
}

// FontRecord is a registered font.
type FontRecord struct {
	DisplayName string
	FileName    string
	Path        string
	Scope       string // "user" or "system"
}

// ThemeState is the curated subset of personalization that ditch-windows
// backs up. It intentionally excludes Explorer and policy keys.
type ThemeState struct {
	Wallpaper            string `json:"wallpaper"`
	WallpaperStyle       string `json:"wallpaper_style"`
	TileWallpaper        string `json:"tile_wallpaper"`
	AppsUseLightTheme    int    `json:"apps_use_light_theme"`
	SystemUsesLightTheme int    `json:"system_uses_light_theme"`
	ColorPrevalence      int    `json:"color_prevalence"`
	AccentColor          uint32 `json:"accent_color"`
	CurrentTheme         string `json:"current_theme"`
}

// FontProvider abstracts font registry access for tests.
type FontProvider interface {
	SystemFonts() ([]FontRecord, error)
	UserFonts() ([]FontRecord, error)
	InstallTime() (int64, error)
	UserFontsDir() string
	SystemFontsDir() string
	RegisterFont(rec FontRecord, destPath, scope string) error
}

// ThemeProvider abstracts theme registry access for tests.
type ThemeProvider interface {
	ReadTheme() (ThemeState, error)
	ApplyTheme(state ThemeState) error
}

// SystemFonts is the production FontProvider.
type SystemFonts struct{}

// SystemTheme is the production ThemeProvider.
type SystemTheme struct{}
