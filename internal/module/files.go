package module

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

// Files backs up user-selected folders with restic. restic performs all
// copying, deduplication, encryption, and (when elevated on Windows) the VSS
// snapshot for locked files.
type Files struct {
	snapshots []string
}

// NewFiles returns the personal-files module.
func NewFiles() *Files { return &Files{} }

// FilesManifest is written to files.json.
type FilesManifest struct {
	CapturedAt time.Time `json:"captured_at"`
	SourceHome string    `json:"source_home"`
	Roots      []string  `json:"roots"`
	Excludes   []string  `json:"excludes,omitempty"`
	VSS        bool      `json:"vss"`
}

// Name implements core.Module.
func (f *Files) Name() string { return "files" }

// Description implements core.Module.
func (f *Files) Description() string {
	return "Documents, Desktop, Pictures, Downloads and other selected folders via restic + VSS"
}

// NeedsAdmin implements core.Module. The VSS snapshot used for locked files
// requires an elevated token.
func (f *Files) NeedsAdmin() bool { return true }

// Scan implements core.Module.
func (f *Files) Scan(ctx context.Context) (core.Summary, error) {
	p, err := env(ctx)
	if err != nil {
		return core.Summary{}, err
	}
	home, err := homeDir(p)
	if err != nil {
		return core.Summary{}, err
	}
	roots, err := core.ResolveIncludeRoots(home, p.Profile.IncludeGlobs)
	if err != nil {
		return core.Summary{}, err
	}
	if len(roots) == 0 {
		return core.Summary{Details: "no configured include folders exist yet"}, nil
	}
	files, bytes, err := countTree(ctx, roots)
	if err != nil {
		return core.Summary{}, err
	}
	return core.Summary{
		ItemCount: int(files),
		Details:   fmt.Sprintf("%d file(s), %s across %d folder(s); VSS %s", files, humanBytes(bytes), len(roots), elevatedWord()),
	}, nil
}

// Backup implements core.Module.
func (f *Files) Backup(ctx context.Context, dir string) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	f.snapshots = nil
	home, err := homeDir(p)
	if err != nil {
		return err
	}
	roots, err := core.ResolveIncludeRoots(home, p.Profile.IncludeGlobs)
	if err != nil {
		return err
	}
	if len(roots) == 0 {
		return fmt.Errorf("files: none of the configured include folders exist (%s)", strings.Join(p.Profile.IncludeGlobs, ", "))
	}
	if p.Repo == nil {
		return fmt.Errorf("files: repository is not configured")
	}
	relativeRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		rel, err := filepath.Rel(home, root)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("files: include root %s escapes the home directory", root)
		}
		relativeRoots = append(relativeRoots, filepath.ToSlash(rel))
	}
	vss := runtime.GOOS == "windows" && core.IsElevated()
	manifest := FilesManifest{
		CapturedAt: time.Now().UTC(),
		SourceHome: home,
		Roots:      relativeRoots,
		Excludes:   append([]string(nil), p.Profile.ExcludeGlobs...),
		VSS:        vss,
	}
	if err := writeJSON(filepath.Join(dir, "files.json"), manifest); err != nil {
		return err
	}
	p.Reportf("backing up %d folder(s) with restic%s", len(roots), vssSuffix(vss))
	snap, err := p.Repo.Backup(ctx, roots, core.BackupOptions{
		Tags:     append(core.BackupTags(p.BackupID, "files"), "module=files"),
		Excludes: p.Profile.ExcludeGlobs,
		VSS:      vss,
	})
	if err != nil {
		return fmt.Errorf("restic files snapshot: %w", err)
	}
	f.snapshots = []string{snap.ID}
	p.Reportf("files snapshot stored: %s", snap.ID)
	return nil
}

// Snapshots implements core.SnapshotProvider.
func (f *Files) Snapshots() []string { return append([]string(nil), f.snapshots...) }

// Restore implements core.Module.
func (f *Files) Restore(ctx context.Context, dir string, opts core.RestoreOpts) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	var manifest FilesManifest
	if err := readJSON(filepath.Join(dir, "files.json"), &manifest); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read files manifest: %w", err)
	}
	if opts.DryRun {
		p.Reportf("would restore %d folder(s)", len(manifest.Roots))
		return nil
	}
	snapshotID, err := f.snapshotID(ctx, p)
	if err != nil {
		return err
	}
	home, err := homeDir(p)
	if err != nil {
		return err
	}
	tempRoot := p.TempDir
	if tempRoot == "" {
		tempRoot, err = core.TempRoot()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return fmt.Errorf("create restore temp directory: %w", err)
	}
	stage, err := os.MkdirTemp(tempRoot, "files-restore-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	p.Reportf("restoring files snapshot (this may take a while)")
	if err := p.Repo.Restore(ctx, snapshotID, stage, core.RestoreOptions{}); err != nil {
		return fmt.Errorf("restore files snapshot: %w", err)
	}
	var failures []error
	mode := opts.EffectiveMode()
	ask := opts.Ask
	for _, rel := range manifest.Roots {
		restored, err := findRestoredRoot(stage, manifest.SourceHome, rel)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		target := filepath.Join(home, filepath.FromSlash(rel))
		p.Reportf("merging %s", rel)
		stats, err := core.ApplyTree(restored, target, core.ApplyOptions{
			Mode: mode,
			Ask:  ask,
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", rel, err))
			continue
		}
		p.Reportf("%s: %d file(s), %d conflict(s), %d .bak file(s)", rel, stats.Files, stats.Conflicts, stats.BackedUp)
	}
	if len(failures) > 0 {
		return fmt.Errorf("files restore: %w", errors.Join(failures...))
	}
	return nil
}

// PreviewRestore implements core.Previewer without writing anything.
func (f *Files) PreviewRestore(ctx context.Context, dir string, opts core.RestoreOpts) (core.PreviewResult, error) {
	p, err := env(ctx)
	if err != nil {
		return core.PreviewResult{}, err
	}
	var manifest FilesManifest
	if err := readJSON(filepath.Join(dir, "files.json"), &manifest); err != nil {
		return core.PreviewResult{}, err
	}
	snapshotID, err := f.snapshotID(ctx, p)
	if err != nil {
		return core.PreviewResult{}, err
	}
	home, err := homeDir(p)
	if err != nil {
		return core.PreviewResult{}, err
	}
	nodes, err := p.Repo.Ls(ctx, snapshotID)
	if err != nil {
		return core.PreviewResult{}, fmt.Errorf("list files snapshot: %w", err)
	}
	result := core.PreviewResult{Details: fmt.Sprintf("%d folder(s)", len(manifest.Roots))}
	for _, node := range nodes {
		if node.Type == "dir" {
			continue
		}
		rel, ok := relativeToSourceHome(node.Path, manifest.SourceHome)
		if !ok {
			continue
		}
		result.Files++
		result.Bytes += node.Size
		if fileExists(filepath.Join(home, filepath.FromSlash(rel))) {
			result.Conflicts++
		}
	}
	result.ItemCount = int(result.Files)
	return result, nil
}

// Verify implements core.Module.
func (f *Files) Verify(ctx context.Context, dir string) error {
	var manifest FilesManifest
	if err := readJSON(filepath.Join(dir, "files.json"), &manifest); err != nil {
		return fmt.Errorf("files manifest: %w", err)
	}
	if manifest.SourceHome == "" {
		return fmt.Errorf("files manifest has no source home")
	}
	if len(manifest.Roots) == 0 {
		return fmt.Errorf("files manifest has no roots")
	}
	return nil
}

func (f *Files) snapshotID(ctx context.Context, p *core.Params) (string, error) {
	if p.Manifest != nil {
		if status, ok := p.Manifest.Modules[f.Name()]; ok && len(status.Snapshots) > 0 {
			return status.Snapshots[0], nil
		}
	}
	tags := append(core.BackupTags(p.BackupID, "files"), "module=files")
	snaps, err := p.Repo.List(ctx, tags)
	if err != nil {
		return "", fmt.Errorf("locate files snapshot: %w", err)
	}
	if len(snaps) == 0 {
		return "", fmt.Errorf("files: backup contains no files snapshot")
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.After(snaps[j].Time) })
	return snaps[0].ID, nil
}

func homeDir(p *core.Params) (string, error) {
	if p.HomeDir != "" {
		return p.HomeDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

func countTree(ctx context.Context, roots []string) (int64, int64, error) {
	var files, bytes int64
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrPermission) {
					return nil
				}
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if d.IsDir() {
				if skipDirName(d.Name(), root == path) {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".tmp") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			files++
			bytes += info.Size()
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			return files, bytes, err
		}
	}
	return files, bytes, nil
}

func skipDirName(name string, isRoot bool) bool {
	if isRoot {
		return false
	}
	switch strings.ToLower(name) {
	case "node_modules", ".cache", "$recycle.bin", "system volume information":
		return true
	}
	return false
}

func elevatedWord() string {
	if core.IsElevated() {
		return "enabled"
	}
	return "unavailable (not elevated)"
}

func vssSuffix(vss bool) string {
	if vss {
		return " using a VSS snapshot"
	}
	return " (VSS requires elevation)"
}

// relativeToSourceHome maps a restic node path back to a path relative to the
// backed-up home directory. Restic stores Windows paths with or without a
// volume prefix depending on the restic build, so compare path components.
func relativeToSourceHome(nodePath, sourceHome string) (string, bool) {
	nodeParts := pathComponents(nodePath)
	sourceParts := homeComponents(sourceHome)
	if len(sourceParts) == 0 || len(nodeParts) <= len(sourceParts) {
		return "", false
	}
	for start := 0; start+len(sourceParts) <= len(nodeParts); start++ {
		match := true
		for i := range sourceParts {
			if !strings.EqualFold(nodeParts[start+i], sourceParts[i]) {
				match = false
				break
			}
		}
		if match {
			rel := nodeParts[start+len(sourceParts):]
			return filepath.ToSlash(filepath.Join(rel...)), true
		}
	}
	return "", false
}

func findRestoredRoot(root, sourceHome, relRoot string) (string, error) {
	suffix := append(homeComponents(sourceHome), pathComponents(relRoot)...)
	if len(suffix) == 0 {
		return "", fmt.Errorf("cannot map restored folder %q", relRoot)
	}
	var candidates []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() || path == root {
			return nil
		}
		parts := pathComponents(mustRel(root, path))
		if len(parts) < len(suffix) {
			return nil
		}
		match := true
		for i := range suffix {
			if !strings.EqualFold(parts[len(parts)-len(suffix)+i], suffix[i]) {
				match = false
				break
			}
		}
		if match {
			candidates = append(candidates, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("could not find restored folder %q under snapshot", relRoot)
	}
	sort.Slice(candidates, func(i, j int) bool { return len(candidates[i]) < len(candidates[j]) })
	return candidates[0], nil
}

func pathComponents(path string) []string {
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "/")
	var out []string
	for _, part := range strings.Split(path, "/") {
		part = strings.TrimSpace(part)
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return out
}

func homeComponents(home string) []string {
	parts := pathComponents(home)
	if len(parts) > 0 && len(parts[0]) == 2 && parts[0][1] == ':' {
		parts = parts[1:]
	}
	return parts
}

func mustRel(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
