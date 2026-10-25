package restic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

func TestParseSnapshotsJSON(t *testing.T) {
	out := []byte(`[
		{"id":"abc123","short_id":"abc","time":"2026-01-02T03:04:05.123456789Z","hostname":"pc","paths":["C:/Users/me/Documents"],"tags":["backup=x","kind=files"],"tree":"t1"},
		{"id":"def456","short_id":"def","time":"2026-01-03T03:04:05Z","hostname":"pc","paths":[],"tags":[],"tree":"t2"}
	]`)
	snaps, err := ParseSnapshotsJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 2 {
		t.Fatalf("got %d snapshots, want 2", len(snaps))
	}
	if snaps[0].ID != "abc123" || snaps[0].Hostname != "pc" || snaps[0].TagValue("kind") != "files" {
		t.Fatalf("unexpected first snapshot: %+v", snaps[0])
	}
	want := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	if !snaps[0].Time.Equal(want) {
		t.Fatalf("time = %v, want %v", snaps[0].Time, want)
	}
	if snaps[1].Time.Location() != time.UTC {
		t.Fatalf("parsed time should be UTC, got %v", snaps[1].Time.Location())
	}
}

func TestParseSnapshotsJSONEmpty(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("null"), []byte("  ")} {
		snaps, err := ParseSnapshotsJSON(in)
		if err != nil {
			t.Fatalf("ParseSnapshotsJSON(%q): %v", in, err)
		}
		if len(snaps) != 0 {
			t.Fatalf("got %d snapshots, want 0", len(snaps))
		}
	}
	if _, err := ParseSnapshotsJSON([]byte("{")); err == nil {
		t.Fatal("invalid JSON should return an error")
	}
}

func TestParseLsJSON(t *testing.T) {
	out := []byte(`{"time":"2026-01-02T03:04:05Z","tree":"abc","paths":["C:/x"]}
{"struct_type":"node","name":"a.txt","type":"file","path":"C:/x/a.txt","size":12,"mode":"0644"}
not json at all
{"struct_type":"node","name":"sub","type":"dir","path":"C:/x/sub","size":0}
{"struct_type":"snapshot","id":"abc"}
`)
	nodes, err := ParseLsJSON(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2: %+v", len(nodes), nodes)
	}
	if nodes[0].Path != "C:/x/a.txt" || nodes[0].Size != 12 || nodes[0].Type != "file" {
		t.Fatalf("unexpected first node: %+v", nodes[0])
	}
	if nodes[1].Type != "dir" {
		t.Fatalf("unexpected second node: %+v", nodes[1])
	}
}

func TestEnvFiltersSensitiveVariables(t *testing.T) {
	t.Setenv("RESTIC_PASSWORD", "stale")
	t.Setenv("RESTIC_REPOSITORY", "stale-repo")
	t.Setenv("RESTIC_CACHE_DIR", "stale-cache")
	t.Setenv("DITCH_TEST_KEEP", "yes")

	w := New("s3:s3.amazonaws.com/bucket", "fresh-secret", "restic")
	w.CacheDir = "C:/cache"
	env := w.env()

	passwords, repos, caches := 0, 0, 0
	for _, entry := range env {
		switch {
		case strings.HasPrefix(entry, "RESTIC_PASSWORD="):
			passwords++
			if entry != "RESTIC_PASSWORD=fresh-secret" {
				t.Fatalf("unexpected password entry %q", entry)
			}
		case strings.HasPrefix(entry, "RESTIC_REPOSITORY="):
			repos++
			if entry != "RESTIC_REPOSITORY=s3:s3.amazonaws.com/bucket" {
				t.Fatalf("unexpected repository entry %q", entry)
			}
		case strings.HasPrefix(entry, "RESTIC_CACHE_DIR="):
			caches++
			if entry != "RESTIC_CACHE_DIR=C:/cache" {
				t.Fatalf("unexpected cache entry %q", entry)
			}
		}
	}
	if passwords != 1 || repos != 1 || caches != 1 {
		t.Fatalf("env counts password=%d repo=%d cache=%d, want 1 each", passwords, repos, caches)
	}
	if os.Getenv("DITCH_TEST_KEEP") != "yes" {
		t.Fatal("unrelated environment variables should be preserved")
	}
}

func TestWrapErrorClassification(t *testing.T) {
	w := New("repo", "pw", "restic")
	err := w.wrapError([]string{"cat", "config"}, errors.New("exit status 1"), "Fatal: wrong password or no key found")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("error = %v, want ErrWrongPassword", err)
	}
	err = w.wrapError([]string{"cat", "config"}, errors.New("exit status 1"), "unable to open config file: no such file or directory")
	if !errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("error = %v, want ErrRepoNotFound", err)
	}
	err = w.wrapError([]string{"backup"}, errors.New("exit status 1"), "something else")
	if errors.Is(err, ErrWrongPassword) || errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("generic error was misclassified: %v", err)
	}
	if !strings.Contains(err.Error(), "restic backup failed") {
		t.Fatalf("unexpected generic error text: %v", err)
	}
}

func TestFindBinaryExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "restic-fake")
	if err := os.WriteFile(path, []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	found, err := FindBinary(path)
	if err != nil {
		t.Fatal(err)
	}
	if found != path {
		t.Fatalf("FindBinary = %q, want %q", found, path)
	}
}

func TestFindBinaryMissingHasInstallHint(t *testing.T) {
	t.Setenv("DITCH_RESTIC_BINARY", "")
	t.Setenv("PATH", t.TempDir())
	_, err := FindBinary(filepath.Join(t.TempDir(), "definitely-missing"))
	if err == nil {
		t.Fatal("expected an error when restic is not installed")
	}
	if !strings.Contains(err.Error(), "winget install restic.restic") {
		t.Fatalf("error should contain install hint: %v", err)
	}
}

func TestNewDefaultsBinary(t *testing.T) {
	if got := New("repo", "pw", "").Binary; got != "restic" {
		t.Fatalf("Binary = %q, want restic", got)
	}
}

func TestRestoreRequiresSnapshot(t *testing.T) {
	w := New("repo", "pw", "restic")
	err := w.Restore(context.Background(), "", t.TempDir(), core.RestoreOptions{})
	if err == nil {
		t.Fatal("empty snapshot id should fail")
	}
}
