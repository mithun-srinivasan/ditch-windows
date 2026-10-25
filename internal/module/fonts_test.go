package module

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/platform"
)

func TestSelectFontRecords(t *testing.T) {
	system := []platform.FontRecord{
		{DisplayName: "Arial", FileName: "arial.ttf", Path: "/fonts/arial.ttf", Scope: "system"},
		{DisplayName: "Acme Display", FileName: "acmedisplay.otf", Path: "/fonts/acmedisplay.otf", Scope: "system"},
		{DisplayName: "Old Custom", FileName: "oldcustom.ttf", Path: "/fonts/oldcustom.ttf", Scope: "system"},
	}
	user := []platform.FontRecord{
		{DisplayName: "My Font", FileName: "myfont.ttf", Path: "/userfonts/myfont.ttf"},
		{DisplayName: "My Font duplicate", FileName: "MyFont.ttf", Path: "/userfonts/MyFont.ttf"},
	}
	selected := selectFontRecords(system, user, 0)
	if len(selected) != 3 {
		t.Fatalf("expected 3 selected fonts, got %d: %+v", len(selected), selected)
	}
	var userCount, systemCount int
	for _, rec := range selected {
		switch rec.Scope {
		case "user":
			userCount++
		case "system":
			systemCount++
			if IsDefaultWindowsFont(rec.FileName) {
				t.Fatalf("default font %q should be excluded", rec.FileName)
			}
		}
	}
	if userCount != 1 || systemCount != 2 {
		t.Fatalf("user=%d system=%d, want 1/2: %+v", userCount, systemCount, selected)
	}
}

func TestIsDefaultWindowsFont(t *testing.T) {
	cases := map[string]bool{
		"arial.ttf":   true,
		"segoeui.ttf": true,
		"custom.otf":  false,
		"legacy.fon":  true,
	}
	for name, want := range cases {
		if got := IsDefaultWindowsFont(name); got != want {
			t.Errorf("IsDefaultWindowsFont(%q)=%v, want %v", name, got, want)
		}
	}
}

func TestFontsBackupRestore(t *testing.T) {
	home := t.TempDir()
	sourceDir := filepath.Join(home, "source")
	targetDir := filepath.Join(home, "target")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fontPath := filepath.Join(sourceDir, "myfont.ttf")
	writeTestFile(t, fontPath, "font-bytes", time.Time{})
	writeTestFile(t, filepath.Join(targetDir, "myfont.ttf"), "previous", time.Time{})

	provider := &fakeFontProvider{
		user:      []platform.FontRecord{{DisplayName: "My Font", FileName: "myfont.ttf", Path: fontPath}},
		userDir:   targetDir,
		systemDir: filepath.Join(home, "systemfonts"),
	}
	f := &Fonts{provider: provider}
	dir := filepath.Join(home, "module")
	p := testParams(newFakeExec(), &fakeRepo{}, home)
	ctx := ctxWith(p)

	if err := f.Backup(ctx, dir); err != nil {
		t.Fatalf("backup: %v", err)
	}
	requireFile(t, filepath.Join(dir, "fonts.json"))
	var manifest FontsManifest
	if err := readJSON(filepath.Join(dir, "fonts.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Fonts) != 1 {
		t.Fatalf("expected one font, got %+v", manifest.Fonts)
	}
	requireFile(t, filepath.Join(dir, filepath.FromSlash(manifest.Fonts[0].BackupFile)))
	if err := f.Verify(ctx, dir); err != nil {
		t.Fatalf("verify: %v", err)
	}

	if err := f.Restore(ctx, dir, core.RestoreOpts{DryRun: true}); err != nil {
		t.Fatalf("dry-run restore: %v", err)
	}
	if len(provider.registered) != 0 {
		t.Fatal("dry run registered a font")
	}

	if err := f.Restore(ctx, dir, core.RestoreOpts{ConflictMode: core.OverwriteKeepBak}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	requireFile(t, filepath.Join(targetDir, "myfont.ttf"))
	requireFile(t, filepath.Join(targetDir, "myfont.ttf.bak"))
	if len(provider.registered) != 1 {
		t.Fatalf("expected font registration, got %+v", provider.registered)
	}
}
