package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HashFile returns the SHA-256 digest of a file as lowercase hex.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// HashDir hashes every regular file below root. Keys use forward slashes.
func HashDir(root string) (map[string]string, error) {
	hashes := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum, err := HashFile(path)
		if err != nil {
			return err
		}
		hashes[filepath.ToSlash(rel)] = sum
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("hash directory: %w", err)
	}
	return hashes, nil
}

// DigestHashes returns a stable aggregate digest for a set of file hashes.
func DigestHashes(hashes map[string]string) string {
	keys := make([]string, 0, len(hashes))
	for k := range hashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\n", k, hashes[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// MergeHashes merges src into dst and returns src.
func MergeHashes(dst map[string]string, src map[string]string) map[string]string {
	if dst == nil {
		dst = make(map[string]string)
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// VerifyHashes checks every expected file below root. Missing and mismatched
// files are reported together.
func VerifyHashes(root string, expected map[string]string) error {
	keys := make([]string, 0, len(expected))
	for k := range expected {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var problems []string
	for _, rel := range keys {
		path := filepath.Join(root, filepath.FromSlash(rel))
		got, err := HashFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", rel, err))
			continue
		}
		if !strings.EqualFold(got, expected[rel]) {
			problems = append(problems, fmt.Sprintf("%s: hash mismatch", rel))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("manifest hash verification failed: %s", strings.Join(problems, "; "))
	}
	return nil
}
