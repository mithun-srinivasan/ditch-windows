package core

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ApplyOptions controls how a restored tree is copied into place.
type ApplyOptions struct {
	Mode ConflictMode
	Ask  func(path string) (ConflictMode, error)
	// DryRun reports conflicts without writing.
	DryRun bool
}

// ApplyStats reports the result of ApplyTree.
type ApplyStats struct {
	Files      int
	Dirs       int
	Conflicts  int
	Skipped    int
	BackedUp   int
	Bytes      int64
	BackupPath []string
}

// ApplyTree copies srcRoot into dstRoot according to opts. Existing files are
// handled per conflict policy. It is only used after restic has decrypted and
// restored a snapshot; it performs no deduplication or cryptography.
func ApplyTree(srcRoot, dstRoot string, opts ApplyOptions) (ApplyStats, error) {
	var stats ApplyStats
	mode := opts.Mode
	if mode == "" {
		mode = OverwriteKeepBak
	}
	ask := opts.Ask

	err := filepath.WalkDir(srcRoot, func(src string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, src)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dst := filepath.Join(dstRoot, rel)

		if d.IsDir() {
			stats.Dirs++
			if opts.DryRun {
				return nil
			}
			if info, err := os.Lstat(dst); err == nil && !info.IsDir() {
				// A file is in the way of a directory: preserve it as .bak.
				if err := preserveExisting(dst, &stats); err != nil {
					return err
				}
			}
			if err := os.MkdirAll(dst, 0o700); err != nil {
				return fmt.Errorf("create directory %s: %w", dst, err)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			// Symlinks and special files are intentionally not restored.
			return nil
		}
		stats.Files++

		info, statErr := os.Lstat(dst)
		exists := statErr == nil
		if exists && info.IsDir() {
			return fmt.Errorf("cannot restore file %s: a directory exists at the destination", dst)
		}
		if exists {
			stats.Conflicts++
			if opts.DryRun {
				return nil
			}
			effective := mode
			if mode == AskPerItem {
				effective = OverwriteKeepBak
				if ask != nil {
					choice, err := ask(dst)
					if err != nil {
						return err
					}
					if choice == Skip || choice == OverwriteKeepBak {
						effective = choice
					}
				}
			}
			switch effective {
			case Skip:
				stats.Skipped++
				return nil
			case OverwriteKeepBak:
				bak, err := BackupExisting(dst)
				if err != nil {
					return err
				}
				stats.BackedUp++
				stats.BackupPath = append(stats.BackupPath, bak)
			default:
				return fmt.Errorf("unsupported conflict mode %q", effective)
			}
		}
		if opts.DryRun {
			if fi, err := d.Info(); err == nil {
				stats.Bytes += fi.Size()
			}
			return nil
		}
		if err := CopyFile(src, dst); err != nil {
			return err
		}
		if fi, err := d.Info(); err == nil {
			stats.Bytes += fi.Size()
		}
		return nil
	})
	if err != nil {
		return stats, err
	}
	return stats, nil
}

func preserveExisting(path string, stats *ApplyStats) error {
	bak, err := BackupExisting(path)
	if err != nil {
		return err
	}
	stats.BackedUp++
	stats.BackupPath = append(stats.BackupPath, bak)
	return nil
}

// BackupExisting renames path to a unique .bak name and returns that name.
func BackupExisting(path string) (string, error) {
	candidate := path + ".bak"
	for i := 1; ; i++ {
		_, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("inspect backup path %s: %w", candidate, err)
		}
		candidate = fmt.Sprintf("%s.bak.%d", path, i)
	}
	if err := os.Rename(path, candidate); err != nil {
		return "", fmt.Errorf("preserve existing file as %s: %w", candidate, err)
	}
	return candidate, nil
}

// CopyFile copies src to dst atomically, preserving permissions where the
// platform supports it.
func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("create parent of %s: %w", dst, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".ditch-copy-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := io.Copy(tmp, in); err != nil {
		cleanup()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chtimes(tmpName, info.ModTime(), info.ModTime()); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace %s: %w", dst, err)
	}
	return nil
}

// ResolveIncludeRoots converts include patterns such as "Documents/**" into
// existing directories below home. The static prefix before the first glob
// metacharacter is used; this avoids walking entire trees during planning.
func ResolveIncludeRoots(home string, patterns []string) ([]string, error) {
	seen := make(map[string]bool)
	var roots []string
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if isAbsoluteLike(pattern) {
			return nil, fmt.Errorf("include path %q must be relative to home", pattern)
		}
		prefix := pattern
		if idx := strings.IndexAny(prefix, "*?["); idx >= 0 {
			prefix = prefix[:idx]
		}
		prefix = strings.TrimRight(prefix, `/\`)
		if prefix == "" {
			return nil, fmt.Errorf("include pattern %q must name a folder", pattern)
		}
		root := filepath.Join(home, filepath.FromSlash(prefix))
		root = filepath.Clean(root)
		rel, err := filepath.Rel(home, root)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("include pattern %q escapes the home directory", pattern)
		}
		info, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			continue
		}
		key := strings.ToLower(root)
		if !seen[key] {
			seen[key] = true
			roots = append(roots, root)
		}
	}
	return roots, nil
}
