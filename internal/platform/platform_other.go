//go:build !windows

package platform

import "time"

// InstalledPrograms returns no entries outside Windows.
func InstalledPrograms() ([]Program, error) { return nil, nil }

// SystemFonts provides empty font state outside Windows.
func (SystemFonts) SystemFonts() ([]FontRecord, error)            { return nil, nil }
func (SystemFonts) UserFonts() ([]FontRecord, error)              { return nil, nil }
func (SystemFonts) InstallTime() (int64, error)                   { return 0, ErrUnsupported }
func (SystemFonts) UserFontsDir() string                          { return "" }
func (SystemFonts) SystemFontsDir() string                        { return "" }
func (SystemFonts) RegisterFont(FontRecord, string, string) error { return ErrUnsupported }

// SystemTheme reports unavailable personalization outside Windows.
func (SystemTheme) ReadTheme() (ThemeState, error) { return ThemeState{}, ErrUnsupported }
func (SystemTheme) ApplyTheme(ThemeState) error    { return ErrUnsupported }

// FontFileAge returns a file's modification time.
func FontFileAge(path string) (time.Time, error) { return time.Time{}, ErrUnsupported }
