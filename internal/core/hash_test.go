package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestHashDirDigestAndVerify(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "b.txt"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	hashes, err := HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes) != 2 {
		t.Fatalf("unexpected hashes: %v", hashes)
	}
	if _, ok := hashes["nested/b.txt"]; !ok {
		t.Fatalf("expected forward-slash path key: %v", hashes)
	}
	digest := DigestHashes(hashes)
	if digest == "" || digest != DigestHashes(hashes) {
		t.Fatal("digest is not stable")
	}
	if err := VerifyHashes(dir, hashes); err != nil {
		t.Fatalf("verify clean tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "b.txt"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHashes(dir, hashes); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHashes(dir, hashes); err == nil || !strings.Contains(err.Error(), "a.txt") {
		t.Fatalf("expected missing file error, got %v", err)
	}
}

func TestMergeHashes(t *testing.T) {
	dst := map[string]string{"a": "1"}
	got := MergeHashes(dst, map[string]string{"b": "2"})
	if got["a"] != "1" || got["b"] != "2" {
		t.Fatalf("unexpected merge: %v", got)
	}
	if MergeHashes(nil, nil) == nil {
		t.Fatal("MergeHashes returned nil")
	}
}
