//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	modgdi32  = windows.NewLazySystemDLL("gdi32.dll")
	moduser32 = windows.NewLazySystemDLL("user32.dll")

	procAddFontResourceW      = modgdi32.NewProc("AddFontResourceW")
	procSystemParametersInfoW = moduser32.NewProc("SystemParametersInfoW")
	procSendMessageTimeoutW   = moduser32.NewProc("SendMessageTimeoutW")
)

// InstalledPrograms enumerates uninstall registry entries from HKLM (64- and
// 32-bit views) and HKCU. Entries without a display name are ignored.
func InstalledPrograms() ([]Program, error) {
	type source struct {
		root registry.Key
		path string
		view uint32
	}
	sources := []source{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`, registry.WOW64_64KEY},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, 0},
	}
	var programs []Program
	var errs []string
	for _, src := range sources {
		k, err := registry.OpenKey(src.root, src.path, registry.ENUMERATE_SUB_KEYS|src.view)
		if err != nil {
			continue
		}
		names, err := k.ReadSubKeyNames(-1)
		k.Close()
		if err != nil {
			continue
		}
		for _, name := range names {
			sub, err := registry.OpenKey(src.root, src.path+`\`+name, registry.QUERY_VALUE|src.view)
			if err != nil {
				continue
			}
			p, ok := readProgram(sub)
			sub.Close()
			if ok {
				programs = append(programs, p)
			}
		}
	}
	if len(programs) == 0 && len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return programs, nil
}

func readProgram(k registry.Key) (Program, bool) {
	name, _, err := k.GetStringValue("DisplayName")
	if err != nil || strings.TrimSpace(name) == "" {
		return Program{}, false
	}
	p := Program{DisplayName: strings.TrimSpace(name)}
	p.DisplayVersion, _, _ = k.GetStringValue("DisplayVersion")
	p.Publisher, _, _ = k.GetStringValue("Publisher")
	p.InstallLocation, _, _ = k.GetStringValue("InstallLocation")
	p.UninstallString, _, _ = k.GetStringValue("UninstallString")
	if v, _, err := k.GetIntegerValue("SystemComponent"); err == nil {
		p.SystemComponent = v == 1
	}
	if v, _, err := k.GetStringValue("ParentKeyName"); err == nil && v != "" {
		p.Hidden = true
	}
	if v, _, err := k.GetStringValue("ReleaseType"); err == nil && strings.EqualFold(v, "Update") {
		p.Hidden = true
	}
	return p, true
}

// SystemFonts reads HKLM's font registry.
func (SystemFonts) SystemFonts() ([]FontRecord, error) {
	return readFonts(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`, "system", systemFontsDir())
}

// UserFonts reads HKCU's per-user font registry.
func (SystemFonts) UserFonts() ([]FontRecord, error) {
	return readFonts(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`, "user", userFontsDir())
}

func readFonts(root registry.Key, keyPath, scope, dir string) ([]FontRecord, error) {
	k, err := registry.OpenKey(root, keyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil, err
	}
	var out []FontRecord
	for _, name := range names {
		data, _, err := k.GetStringValue(name)
		if err != nil || strings.TrimSpace(data) == "" {
			continue
		}
		file := filepath.Base(strings.ReplaceAll(data, "/", `\`))
		path := data
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, file)
		} else {
			path = filepath.Clean(path)
		}
		out = append(out, FontRecord{DisplayName: name, FileName: file, Path: path, Scope: scope})
	}
	return out, nil
}

// InstallTime returns the Windows installation time as a Unix timestamp.
func (SystemFonts) InstallTime() (int64, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return 0, err
	}
	defer k.Close()
	if v, _, err := k.GetIntegerValue("InstallDate"); err == nil && v > 0 {
		return int64(v), nil
	}
	// Fall back to the kernel image's creation time.
	path := filepath.Join(os.Getenv("WINDIR"), "System32", "ntoskrnl.exe")
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.ModTime().Unix(), nil
}

func (SystemFonts) UserFontsDir() string   { return userFontsDir() }
func (SystemFonts) SystemFontsDir() string { return systemFontsDir() }

func userFontsDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	return filepath.Join(base, "Microsoft", "Windows", "Fonts")
}

func systemFontsDir() string {
	windir := os.Getenv("WINDIR")
	if windir == "" {
		windir = os.Getenv("SystemRoot")
	}
	return filepath.Join(windir, "Fonts")
}

// RegisterFont writes the font registry entry and tells Windows about it.
func (f SystemFonts) RegisterFont(rec FontRecord, destPath, scope string) error {
	display := rec.DisplayName
	if display == "" {
		display = rec.FileName
	}
	var root registry.Key
	var keyPath, data string
	if scope == "system" {
		root = registry.LOCAL_MACHINE
		keyPath = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`
		data = filepath.Base(destPath)
	} else {
		root = registry.CURRENT_USER
		keyPath = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`
		data = destPath
	}
	k, err := registry.OpenKey(root, keyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open font registry: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(display, data); err != nil {
		return fmt.Errorf("register font %s: %w", display, err)
	}
	// Activate the font for this session and notify applications.
	ptr, err := syscall.UTF16PtrFromString(destPath)
	if err == nil {
		_, _, _ = procAddFontResourceW.Call(uintptr(unsafe.Pointer(ptr)))
	}
	broadcastFontChange()
	return nil
}

func broadcastFontChange() {
	const (
		hwndBroadcast = 0xffff
		wmFontChange  = 0x001D
		smtoAbort     = 0x0002
	)
	var result uintptr
	_, _, _ = procSendMessageTimeoutW.Call(hwndBroadcast, wmFontChange, 0, 0, smtoAbort, 1000, uintptr(unsafe.Pointer(&result)))
}

// ReadTheme reads only curated HKCU personalization values.
func (SystemTheme) ReadTheme() (ThemeState, error) {
	var state ThemeState
	read := 0

	if k, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\Desktop`, registry.QUERY_VALUE); err == nil {
		state.Wallpaper, _, _ = k.GetStringValue("Wallpaper")
		state.WallpaperStyle, _, _ = k.GetStringValue("WallpaperStyle")
		state.TileWallpaper, _, _ = k.GetStringValue("TileWallpaper")
		read++
		k.Close()
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetIntegerValue("AppsUseLightTheme"); err == nil {
			state.AppsUseLightTheme = int(v)
		}
		if v, _, err := k.GetIntegerValue("SystemUsesLightTheme"); err == nil {
			state.SystemUsesLightTheme = int(v)
		}
		read++
		k.Close()
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\DWM`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetIntegerValue("ColorPrevalence"); err == nil {
			state.ColorPrevalence = int(v)
		}
		read++
		k.Close()
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\Accent`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetIntegerValue("AccentColorMenu"); err == nil {
			state.AccentColor = uint32(v)
		}
		read++
		k.Close()
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes`, registry.QUERY_VALUE); err == nil {
		state.CurrentTheme, _, _ = k.GetStringValue("CurrentTheme")
		read++
		k.Close()
	}
	if read == 0 {
		return state, errors.New("could not read any personalization keys")
	}
	return state, nil
}

// ApplyTheme writes the curated personalization values and refreshes the
// desktop. It never imports registry hives or touches unlisted keys.
func (SystemTheme) ApplyTheme(state ThemeState) error {
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\Desktop`, registry.SET_VALUE); err == nil {
		if state.WallpaperStyle != "" {
			_ = k.SetStringValue("WallpaperStyle", state.WallpaperStyle)
		}
		if state.TileWallpaper != "" {
			_ = k.SetStringValue("TileWallpaper", state.TileWallpaper)
		}
		if state.Wallpaper != "" {
			_ = k.SetStringValue("Wallpaper", state.Wallpaper)
		}
		k.Close()
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.SET_VALUE); err == nil {
		_ = k.SetDWordValue("AppsUseLightTheme", uint32(state.AppsUseLightTheme))
		_ = k.SetDWordValue("SystemUsesLightTheme", uint32(state.SystemUsesLightTheme))
		k.Close()
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\DWM`, registry.SET_VALUE); err == nil {
		_ = k.SetDWordValue("ColorPrevalence", uint32(state.ColorPrevalence))
		k.Close()
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\Accent`, registry.SET_VALUE); err == nil {
		if state.AccentColor != 0 {
			_ = k.SetDWordValue("AccentColorMenu", state.AccentColor)
		}
		k.Close()
	}
	refreshDesktop(state.Wallpaper)
	return nil
}

func refreshDesktop(wallpaper string) {
	const (
		spiSetDeskWallpaper = 0x0014
		spifUpdateIniFile   = 0x01
		spifSendChange      = 0x02
		hwndBroadcast       = 0xffff
		wmSettingChange     = 0x001A
		smtoAbort           = 0x0002
	)
	if wallpaper != "" {
		if ptr, err := syscall.UTF16PtrFromString(wallpaper); err == nil {
			_, _, _ = procSystemParametersInfoW.Call(spiSetDeskWallpaper, 0, uintptr(unsafe.Pointer(ptr)), spifUpdateIniFile|spifSendChange)
		}
	}
	var result uintptr
	param, err := syscall.UTF16PtrFromString("ImmersiveColorSet")
	if err == nil {
		_, _, _ = procSendMessageTimeoutW.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(param)), smtoAbort, 1000, uintptr(unsafe.Pointer(&result)))
	}
}

// FontFileAge returns a font file's modification time.
func FontFileAge(path string) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}
