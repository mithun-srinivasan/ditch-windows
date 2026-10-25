package module

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/platform"
)

func TestThemeBackupRestoreCuratedState(t *testing.T) {
	home := t.TempDir()
	appHome := filepath.Join(home, "appdata")
	t.Setenv("DITCH_WINDOWS_HOME", appHome)
	wallpaper := filepath.Join(home, "wallpaper.jpg")
	themeFile := filepath.Join(home, "My Theme.theme")
	writeTestFile(t, wallpaper, "jpeg-data", time.Time{})
	writeTestFile(t, themeFile, "[Theme]\n", time.Time{})

	provider := &fakeThemeProvider{state: platform.ThemeState{
		Wallpaper:            wallpaper,
		WallpaperStyle:       "10",
		TileWallpaper:        "0",
		AppsUseLightTheme:    0,
		SystemUsesLightTheme: 0,
		ColorPrevalence:      1,
		AccentColor:          0x00d77800,
		CurrentTheme:         themeFile,
	}}
	th := &Theme{provider: provider}
	dir := filepath.Join(home, "module")
	p := testParams(newFakeExec(), &fakeRepo{}, home)
	ctx := ctxWith(p)

	if err := th.Backup(ctx, dir); err != nil {
		t.Fatalf("backup: %v", err)
	}
	requireFile(t, filepath.Join(dir, "theme.json"))
	requireFile(t, filepath.Join(dir, "assets", "wallpaper.jpg"))
	requireFile(t, filepath.Join(dir, "assets", "theme.theme"))

	preview, err := th.PreviewRestore(ctx, dir, core.RestoreOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Files != 2 || preview.Bytes == 0 {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	if err := th.Restore(ctx, dir, core.RestoreOpts{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if provider.applied != nil {
		t.Fatal("dry run applied the theme")
	}

	if err := th.Restore(ctx, dir, core.RestoreOpts{}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if provider.applied == nil {
		t.Fatal("theme was not applied")
	}
	if !strings.HasPrefix(provider.applied.Wallpaper, filepath.Join(appHome, "restored", "theme")) {
		t.Fatalf("wallpaper was not restored into app data: %s", provider.applied.Wallpaper)
	}
	if provider.applied.AccentColor != 0x00d77800 || provider.applied.AppsUseLightTheme != 0 {
		t.Fatalf("curated keys changed: %+v", provider.applied)
	}
	if err := th.Verify(ctx, dir); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestSafeAssetExt(t *testing.T) {
	cases := map[string]string{
		"/x/Wallpaper.JPG": ".jpg",
		`C:\x\theme.theme`: ".theme",
		"/x/noext":         ".fallback",
		"/x/bad.name!!!":   ".fallback",
	}
	for in, want := range cases {
		if got := safeAssetExt(in, ".fallback"); got != want {
			t.Errorf("safeAssetExt(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestThemeRestoreMissingManifestIsNoop(t *testing.T) {
	home := t.TempDir()
	p := testParams(newFakeExec(), &fakeRepo{}, home)
	th := &Theme{provider: &fakeThemeProvider{}}
	if err := th.Restore(ctxWith(p), filepath.Join(home, "module"), core.RestoreOpts{}); err != nil {
		t.Fatalf("missing manifest should be a no-op: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "module")); !os.IsNotExist(err) {
		t.Fatal("restore should not create a module directory")
	}
}
