package module

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/platform"
)

const sampleWingetList = `Name                          Id                        Version      Available    Source
------------------------------------------------------------------------------------
7-Zip 23.01                   7zip.7zip                 23.01                     winget
Spotify Music                 SpotifyAB.SpotifyMusic    1.2.3                     winget
Microsoft Store               Microsoft.WindowsStore    2210                      msstore
`

func TestParseWingetList(t *testing.T) {
	entries := ParseWingetList([]byte(sampleWingetList))
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "7-Zip 23.01" || entries[0].ID != "7zip.7zip" || entries[0].Source != "winget" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
	if entries[2].Source != "msstore" {
		t.Fatalf("expected msstore source, got %+v", entries[2])
	}
}

func TestParseWingetListIgnoresNoise(t *testing.T) {
	output := `No installed package found matching input criteria.
Name    Id    Version
---------------------
Some App    Vendor.App    1.0.0
`
	entries := ParseWingetList([]byte(output))
	if len(entries) != 1 || entries[0].ID != "Vendor.App" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestParseWingetExport(t *testing.T) {
	raw := `{
	  "$schema": "https://aka.ms/winget-exportedpackages.schema.json",
	  "creationDate": "2026-01-01T00:00:00.0000000Z",
	  "sources": [{
	    "packages": [
	      {"packageIdentifier": "7zip.7zip", "version": "23.01"},
	      {"packageIdentifier": "Git.Git", "version": "2.44.0"}
	    ],
	    "sourceDetails": {"name": "winget", "type": "Microsoft.PreIndexed.Package"}
	  }]
	}`
	var export WingetExport
	if err := json.Unmarshal([]byte(raw), &export); err != nil {
		t.Fatal(err)
	}
	if got := len(export.Packages()); got != 2 {
		t.Fatalf("expected 2 packages, got %d", got)
	}
	if export.Packages()[1].PackageIdentifier != "Git.Git" {
		t.Fatalf("unexpected package: %+v", export.Packages()[1])
	}
}

func TestClassifyApps(t *testing.T) {
	programs := []platform.Program{
		{DisplayName: "7-Zip 23.01", DisplayVersion: "23.01", Publisher: "Igor Pavlov", UninstallString: `C:\Program Files\7-Zip\Uninstall.exe`},
		{DisplayName: "Spotify", InstallLocation: `C:\Program Files\WindowsApps\Spotify`},
		{DisplayName: "Steam", DisplayVersion: "2.10", Publisher: "Valve"},
		{DisplayName: "Notepad++", DisplayVersion: "8.6", Publisher: "Don Ho"},
	}
	exported := []WingetPackage{
		{PackageIdentifier: "7zip.7zip", Version: "23.01"},
		{PackageIdentifier: "Notepad++.Notepad++", Version: "8.6"},
	}
	inventory := ClassifyApps(programs, ParseWingetList([]byte(sampleWingetList)), exported, time.Unix(0, 0))
	winget, alternatives, manual := inventory.Counts()
	if winget < 2 {
		t.Fatalf("expected at least 2 winget apps, got %d: %+v", winget, inventory)
	}
	if alternatives != 2 { // Spotify (store) + Microsoft Store from winget list
		t.Fatalf("expected 2 alternative-source apps, got %d: %+v", alternatives, inventory)
	}
	if manual != 1 || inventory.Manual[0].Name != "Steam" {
		t.Fatalf("expected Steam as the only manual app, got %+v", inventory.Manual)
	}
	markdown := ManualReinstallMarkdown(inventory)
	if !strings.Contains(markdown, "Steam") || !strings.Contains(markdown, "winget-reinstallable") {
		t.Fatalf("unexpected markdown:\n%s", markdown)
	}
}

func TestWingetBackupRestoreWithFakeExec(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "module")
	exec := newFakeExec()
	exec.look["winget"] = true
	exec.run = func(name string, args []string) (core.Result, error) {
		switch {
		case name == "winget" && len(args) > 0 && args[0] == "export":
			out := argValue(args, "--output")
			raw := `{"sources":[{"packages":[{"packageIdentifier":"7zip.7zip","version":"23.01"},{"packageIdentifier":"Notepad++.Notepad++","version":"8.6"}]}]}`
			if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
				return core.Result{}, err
			}
			return core.Result{}, os.WriteFile(out, []byte(raw), 0o600)
		case name == "winget" && len(args) > 0 && args[0] == "list":
			return core.Result{Stdout: []byte(sampleWingetList)}, nil
		case name == "winget" && len(args) > 0 && args[0] == "import":
			return core.Result{Stdout: []byte("Successfully installed")}, nil
		}
		return core.Result{}, nil
	}
	repo := &fakeRepo{}
	p := testParams(exec, repo, home)
	p.Profile = core.Profile{EnabledModules: []string{"winget"}}
	w := &Winget{lister: func() ([]platform.Program, error) {
		return []platform.Program{
			{DisplayName: "7-Zip 23.01", DisplayVersion: "23.01", Publisher: "Igor Pavlov"},
			{DisplayName: "Steam", DisplayVersion: "2.10", Publisher: "Valve"},
			{DisplayName: "Spotify", InstallLocation: `C:\Program Files\WindowsApps\Spotify`},
		}, nil
	}}
	ctx := ctxWith(p)
	if err := w.Backup(ctx, dir); err != nil {
		t.Fatalf("backup: %v", err)
	}
	requireFile(t, filepath.Join(dir, "apps.json"))
	requireFile(t, filepath.Join(dir, "inventory.json"))
	requireFile(t, filepath.Join(dir, "manual-reinstall.md"))
	requireContains(t, filepath.Join(dir, "manual-reinstall.md"), "Steam")

	var inventory Inventory
	if err := readJSON(filepath.Join(dir, "inventory.json"), &inventory); err != nil {
		t.Fatal(err)
	}
	if len(inventory.Manual) != 1 {
		t.Fatalf("expected one manual app, got %+v", inventory.Manual)
	}
	if err := w.Verify(ctx, dir); err != nil {
		t.Fatalf("verify: %v", err)
	}

	importsBefore := len(exec.callsFor("winget"))
	if err := w.Restore(ctx, dir, core.RestoreOpts{DryRun: true}); err != nil {
		t.Fatalf("dry-run restore: %v", err)
	}
	for _, call := range exec.callsFor("winget")[importsBefore:] {
		if len(call.Args) > 0 && call.Args[0] == "import" {
			t.Fatalf("dry run must not import")
		}
	}
	if err := w.Restore(ctx, dir, core.RestoreOpts{}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	foundImport := false
	for _, call := range exec.callsFor("winget") {
		if len(call.Args) > 0 && call.Args[0] == "import" {
			foundImport = true
			if !containsArg(call.Args, "--accept-package-agreements") {
				t.Fatalf("import missing agreement flag: %v", call.Args)
			}
		}
	}
	if !foundImport {
		t.Fatal("expected winget import to run")
	}
}

func argValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestNormalizeAppName(t *testing.T) {
	if got := normalizeAppName("Microsoft® Visual Studio Code™"); got != "microsoft visual studio code" {
		t.Fatalf("unexpected normalization: %q", got)
	}
}
