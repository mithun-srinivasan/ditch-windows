package demo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

type fakeRepo struct {
	paths     []string
	opts      core.BackupOptions
	content   map[string]string
	snapshots []core.Snapshot
}

func (f *fakeRepo) RepoInfo() string                     { return "fake:demo" }
func (f *fakeRepo) Init(context.Context) error           { return nil }
func (f *fakeRepo) Exists(context.Context) (bool, error) { return true, nil }
func (f *fakeRepo) Check(context.Context, bool) error    { return nil }
func (f *fakeRepo) Restore(context.Context, string, string, core.RestoreOptions) error {
	return nil
}
func (f *fakeRepo) List(context.Context, []string) ([]core.Snapshot, error) {
	return f.snapshots, nil
}
func (f *fakeRepo) Ls(context.Context, string) ([]core.FileNode, error) { return nil, nil }
func (f *fakeRepo) Backup(_ context.Context, paths []string, opts core.BackupOptions) (core.Snapshot, error) {
	f.paths = append([]string(nil), paths...)
	f.opts = opts
	f.content = make(map[string]string)
	for _, root := range paths {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr == nil {
				f.content[path] = string(data)
			}
			return nil
		})
	}
	return core.Snapshot{ID: "snap-wifi", Tags: opts.Tags}, nil
}

func demoParams(t *testing.T) (context.Context, *core.Params, string) {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	p := &core.Params{HomeDir: home, TempDir: filepath.Join(base, "tmp"), BackupID: "demo-backup"}
	return core.WithParams(context.Background(), p), p, base
}

func TestSeedHome(t *testing.T) {
	home := t.TempDir()
	if err := SeedHome(home); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"Documents/welcome.txt",
		"Documents/notes/todo.md",
		"Desktop/readme.txt",
		"Pictures/photo-notes.txt",
		"Downloads/sample-download.txt",
		"Documents/node_modules/ignored.txt",
	} {
		if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected seeded file %s: %v", rel, err)
		}
	}
	// Re-seeding must not overwrite local edits.
	welcome := filepath.Join(home, "Documents", "welcome.txt")
	if err := os.WriteFile(welcome, []byte("local edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SeedHome(home); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(welcome)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "local edit" {
		t.Fatalf("SeedHome overwrote an existing file: %q", data)
	}
}

func TestSimulateLocalEdit(t *testing.T) {
	home := t.TempDir()
	if err := SeedHome(home); err != nil {
		t.Fatal(err)
	}
	if err := SimulateLocalEdit(home); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "Documents", "welcome.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Edited locally") {
		t.Fatalf("unexpected content: %q", data)
	}
}

func TestDemoAppsBackupVerifyRestore(t *testing.T) {
	ctx, p, _ := demoParams(t)
	dir := filepath.Join(t.TempDir(), "apps")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	m := NewApps()
	summary, err := m.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ItemCount != len(demoApps) {
		t.Fatalf("scan count = %d, want %d", summary.ItemCount, len(demoApps))
	}
	if err := m.Backup(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx, dir); err != nil {
		t.Fatal(err)
	}
	markdown, err := os.ReadFile(filepath.Join(dir, "manual-reinstall.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), "Vendor Laptop Utility") {
		t.Fatal("manual reinstall list should include the vendor-only app")
	}
	if err := m.Restore(ctx, dir, core.RestoreOpts{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.HomeDir, ".ditch-demo", "apps-restored.json")); !os.IsNotExist(err) {
		t.Fatal("dry-run restore must not write a marker")
	}
	if err := m.Restore(ctx, dir, core.RestoreOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.HomeDir, ".ditch-demo", "apps-restored.json")); err != nil {
		t.Fatalf("restore marker missing: %v", err)
	}
}

func TestDemoThemeBackupVerifyRestore(t *testing.T) {
	ctx, p, _ := demoParams(t)
	dir := filepath.Join(t.TempDir(), "theme")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	m := NewTheme()
	if err := m.Backup(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(ctx, dir, core.RestoreOpts{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.HomeDir, ".ditch-demo", "theme")); !os.IsNotExist(err) {
		t.Fatal("dry-run theme restore must not write assets")
	}
	if err := m.Restore(ctx, dir, core.RestoreOpts{ConflictMode: core.OverwriteKeepBak}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.HomeDir, ".ditch-demo", "theme", "wallpaper.txt")); err != nil {
		t.Fatalf("restored wallpaper missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.HomeDir, ".ditch-demo", "theme-restored.json")); err != nil {
		t.Fatalf("theme marker missing: %v", err)
	}
}

func TestDemoWifiKeepsSecretsOutOfMetadata(t *testing.T) {
	ctx, p, base := demoParams(t)
	repo := &fakeRepo{}
	p.Repo = repo
	if err := os.MkdirAll(p.TempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "wifi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	m := NewWifi()
	if err := m.Backup(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if len(repo.paths) != 1 {
		t.Fatalf("repo backup paths = %d, want 1 secure dir", len(repo.paths))
	}
	if _, err := os.Stat(repo.paths[0]); !os.IsNotExist(err) {
		t.Fatalf("secure Wi-Fi temp dir should be wiped, stat err=%v", err)
	}
	foundSecret := false
	for path, data := range repo.content {
		if strings.HasSuffix(path, ".xml") && strings.Contains(data, "DEMO-ONLY-NOT-A-REAL-PASSWORD") {
			foundSecret = true
		}
	}
	if !foundSecret {
		t.Fatal("synthetic Wi-Fi secret should exist only in the encrypted repo payload")
	}
	metadata, err := os.ReadFile(filepath.Join(dir, "wifi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metadata), "DEMO-ONLY") {
		t.Fatal("wifi.json leaked the secret")
	}
	if !strings.Contains(string(repo.opts.Tags[0]), "backup=demo-backup") {
		t.Fatalf("unexpected tags: %v", repo.opts.Tags)
	}
	if len(m.Snapshots()) != 1 || m.Snapshots()[0] != "snap-wifi" {
		t.Fatalf("snapshots = %v", m.Snapshots())
	}
	if err := m.Verify(ctx, dir); err != nil {
		t.Fatal(err)
	}
}
