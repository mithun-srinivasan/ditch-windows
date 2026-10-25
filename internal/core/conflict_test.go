package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func makeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestApplyTreeOverwriteKeepBak(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	makeTree(t, src, map[string]string{"a.txt": "new-a", "nested/b.txt": "new-b"})
	makeTree(t, dst, map[string]string{"a.txt": "old-a", "a.txt.bak": "older-a"})

	stats, err := ApplyTree(src, dst, ApplyOptions{Mode: OverwriteKeepBak})
	if err != nil {
		t.Fatal(err)
	}
	if readString(t, filepath.Join(dst, "a.txt")) != "new-a" {
		t.Fatal("file was not overwritten")
	}
	if readString(t, filepath.Join(dst, "a.txt.bak.1")) != "old-a" {
		t.Fatal("conflicting file was not preserved with a unique .bak name")
	}
	if readString(t, filepath.Join(dst, "nested", "b.txt")) != "new-b" {
		t.Fatal("nested file missing")
	}
	if stats.Conflicts != 1 || stats.BackedUp != 1 || stats.Files != 2 || stats.Dirs != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestApplyTreeSkip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	makeTree(t, src, map[string]string{"a.txt": "new", "b.txt": "b"})
	makeTree(t, dst, map[string]string{"a.txt": "old"})
	stats, err := ApplyTree(src, dst, ApplyOptions{Mode: Skip})
	if err != nil {
		t.Fatal(err)
	}
	if readString(t, filepath.Join(dst, "a.txt")) != "old" {
		t.Fatal("Skip overwrote a file")
	}
	if readString(t, filepath.Join(dst, "b.txt")) != "b" {
		t.Fatal("Skip did not copy a new file")
	}
	if stats.Skipped != 1 || stats.Conflicts != 1 || stats.BackedUp != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestApplyTreeDryRunWritesNothing(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	makeTree(t, src, map[string]string{"a.txt": "new", "b.txt": "b"})
	makeTree(t, dst, map[string]string{"a.txt": "old"})
	stats, err := ApplyTree(src, dst, ApplyOptions{Mode: OverwriteKeepBak, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if readString(t, filepath.Join(dst, "a.txt")) != "old" {
		t.Fatal("dry run modified an existing file")
	}
	if _, err := os.Stat(filepath.Join(dst, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("dry run copied a new file")
	}
	if stats.Conflicts != 1 || stats.BackedUp != 0 || stats.Files != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestApplyTreeAskPerItem(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	makeTree(t, src, map[string]string{"keep.txt": "new-keep", "skip.txt": "new-skip"})
	makeTree(t, dst, map[string]string{"keep.txt": "old-keep", "skip.txt": "old-skip"})
	asked := map[string]ConflictMode{}
	stats, err := ApplyTree(src, dst, ApplyOptions{
		Mode: AskPerItem,
		Ask: func(path string) (ConflictMode, error) {
			name := filepath.Base(path)
			asked[name] = Skip
			if name == "keep.txt" {
				asked[name] = OverwriteKeepBak
			}
			return asked[name], nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if readString(t, filepath.Join(dst, "skip.txt")) != "old-skip" {
		t.Fatal("per-item skip did not skip")
	}
	if readString(t, filepath.Join(dst, "keep.txt")) != "new-keep" {
		t.Fatal("per-item overwrite did not overwrite")
	}
	if len(asked) != 2 || stats.Skipped != 1 || stats.BackedUp != 1 {
		t.Fatalf("unexpected stats/asks: %+v %v", stats, asked)
	}
}

func TestApplyTreeAskErrorAborts(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	makeTree(t, src, map[string]string{"a.txt": "new"})
	makeTree(t, dst, map[string]string{"a.txt": "old"})
	boom := errors.New("user went away")
	_, err := ApplyTree(src, dst, ApplyOptions{Mode: AskPerItem, Ask: func(string) (ConflictMode, error) {
		return "", boom
	}})
	if !errors.Is(err, boom) {
		t.Fatalf("expected ask error, got %v", err)
	}
}

func TestBackupExistingUniqueNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	bak, err := BackupExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	if bak != path+".bak.1" {
		t.Fatalf("got %s", bak)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("original file still exists after backup")
	}
}

func TestResolveIncludeRoots(t *testing.T) {
	home := t.TempDir()
	makeTree(t, home, map[string]string{
		"Documents/a.txt":    "a",
		"Desktop/b.txt":      "b",
		"Pictures/c.txt":     "c",
		"secret/outside.txt": "x",
	})
	roots, err := ResolveIncludeRoots(home, []string{"Documents/**", "Desktop", "Missing", "Documents"})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Fatalf("expected 2 existing roots, got %v", roots)
	}
	if filepath.Base(roots[0]) != "Documents" || filepath.Base(roots[1]) != "Desktop" {
		t.Fatalf("unexpected roots: %v", roots)
	}
	if _, err := ResolveIncludeRoots(home, []string{"../outside"}); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if _, err := ResolveIncludeRoots(home, []string{home}); err == nil {
		t.Fatal("expected absolute path rejection")
	}
	if _, err := ResolveIncludeRoots(home, []string{"**"}); err == nil {
		t.Fatal("expected pattern without folder rejection")
	}
}
