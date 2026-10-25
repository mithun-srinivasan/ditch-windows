package module

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

func TestRelativeToSourceHome(t *testing.T) {
	cases := []struct {
		node, source, want string
		ok                 bool
	}{
		{`C:\Users\mithun\Documents\a.txt`, `C:\Users\mithun`, "Documents/a.txt", true},
		{`/C:/Users/mithun/Documents/a.txt`, `C:\Users\mithun`, "Documents/a.txt", true},
		{`/home/mithun/Documents/a.txt`, `/home/mithun`, "Documents/a.txt", true},
		{`D:\Backup\other\a.txt`, `C:\Users\mithun`, "", false},
	}
	for _, tc := range cases {
		got, ok := relativeToSourceHome(tc.node, tc.source)
		if ok != tc.ok || got != tc.want {
			t.Errorf("relativeToSourceHome(%q,%q)=(%q,%v), want (%q,%v)", tc.node, tc.source, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFindRestoredRootPicksShortestMatch(t *testing.T) {
	stage := t.TempDir()
	deep := filepath.Join(stage, "backup", "C", "Users", "old", "Documents")
	shallow := filepath.Join(stage, "C", "Users", "old", "Documents")
	for _, dir := range []string{deep, shallow} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got, err := findRestoredRoot(stage, `C:\Users\old`, "Documents")
	if err != nil {
		t.Fatal(err)
	}
	if got != shallow {
		t.Fatalf("got %s, want %s", got, shallow)
	}
}

func TestFilesScanAndBackup(t *testing.T) {
	home := t.TempDir()
	writeTestFile(t, filepath.Join(home, "Documents", "a.txt"), "a", time.Time{})
	writeTestFile(t, filepath.Join(home, "Desktop", "b.txt"), "b", time.Time{})
	writeTestFile(t, filepath.Join(home, "Documents", "node_modules", "skip.js"), "skip", time.Time{})
	writeTestFile(t, filepath.Join(home, "Documents", "junk.tmp"), "skip", time.Time{})

	repo := &fakeRepo{}
	p := testParams(newFakeExec(), repo, home)
	p.Profile = core.Profile{IncludeGlobs: []string{"Documents", "Desktop"}}
	f := NewFiles()
	ctx := ctxWith(p)
	summary, err := f.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ItemCount != 2 {
		t.Fatalf("expected 2 files, got %+v", summary)
	}

	dir := filepath.Join(home, "module")
	if err := f.Backup(ctx, dir); err != nil {
		t.Fatalf("backup: %v", err)
	}
	var manifest FilesManifest
	if err := readJSON(filepath.Join(dir, "files.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Roots) != 2 || manifest.Roots[0] != "Documents" || manifest.Roots[1] != "Desktop" {
		t.Fatalf("unexpected roots: %v", manifest.Roots)
	}
	if len(repo.BackupCalls) != 1 {
		t.Fatalf("expected one restic backup, got %d", len(repo.BackupCalls))
	}
	call := repo.BackupCalls[0]
	if len(call.Paths) != 2 {
		t.Fatalf("expected two absolute roots, got %v", call.Paths)
	}
	if !containsArg(call.Opts.Tags, "module=files") {
		t.Fatalf("missing module tag: %v", call.Opts.Tags)
	}
	wantVSS := runtime.GOOS == "windows" && core.IsElevated()
	if call.Opts.VSS != wantVSS || manifest.VSS != wantVSS {
		t.Fatalf("unexpected VSS setting: opts=%v manifest=%v", call.Opts.VSS, manifest.VSS)
	}
	if err := f.Verify(ctx, dir); err != nil {
		t.Fatal(err)
	}
}

func TestFilesPreviewRestore(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "module")
	writeTestFile(t, filepath.Join(dir, "files.json"), `{"source_home":"C:\\Users\\old","roots":["Documents"]}`, time.Time{})
	writeTestFile(t, filepath.Join(home, "Documents", "a.txt"), "existing", time.Time{})
	repo := &fakeRepo{LsNodes: []core.FileNode{
		{Path: `C:\Users\old\Documents\a.txt`, Type: "file", Size: 10},
		{Path: `C:\Users\old\Documents\b.txt`, Type: "file", Size: 20},
		{Path: `C:\Users\old\Documents`, Type: "dir"},
	}}
	p := testParams(newFakeExec(), repo, home)
	p.Manifest = &core.Manifest{Modules: map[string]core.ModuleStatus{
		"files": {Name: "files", Snapshots: []string{"snap-files"}},
	}}
	f := NewFiles()
	preview, err := f.PreviewRestore(ctxWith(p), dir, core.RestoreOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Files != 2 || preview.Bytes != 30 || preview.Conflicts != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
}

func TestFilesRestoreMapsSnapshotToCurrentHome(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "module")
	writeTestFile(t, filepath.Join(dir, "files.json"), `{"source_home":"C:\\Users\\old","roots":["Documents"]}`, time.Time{})
	writeTestFile(t, filepath.Join(home, "Documents", "a.txt"), "existing", time.Time{})

	repo := &fakeRepo{RestoreFn: func(snapshotID, target string) error {
		writeTestFile(t, filepath.Join(target, "C", "Users", "old", "Documents", "a.txt"), "restored", time.Time{})
		writeTestFile(t, filepath.Join(target, "C", "Users", "old", "Documents", "b.txt"), "new", time.Time{})
		return nil
	}}
	p := testParams(newFakeExec(), repo, home)
	p.Manifest = &core.Manifest{Modules: map[string]core.ModuleStatus{
		"files": {Name: "files", Snapshots: []string{"snap-files"}},
	}}
	f := NewFiles()
	if err := f.Restore(ctxWith(p), dir, core.RestoreOpts{ConflictMode: core.OverwriteKeepBak}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "Documents", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "restored" {
		t.Fatalf("expected restored content, got %q", data)
	}
	bak, err := os.ReadFile(filepath.Join(home, "Documents", "a.txt.bak"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != "existing" {
		t.Fatalf("expected original content in .bak, got %q", bak)
	}
	requireFile(t, filepath.Join(home, "Documents", "b.txt"))
}
