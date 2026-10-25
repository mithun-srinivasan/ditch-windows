package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EventType identifies a runner progress event.
type EventType string

const (
	EventBackupStart   EventType = "backup_start"
	EventModuleStart   EventType = "module_start"
	EventModuleScan    EventType = "module_scan"
	EventModuleDone    EventType = "module_done"
	EventBackupDone    EventType = "backup_done"
	EventRestoreStart  EventType = "restore_start"
	EventRestoreModule EventType = "restore_module"
	EventRestoreDone   EventType = "restore_done"
	EventMessage       EventType = "message"
	EventWarning       EventType = "warning"
)

// Event is emitted synchronously during a backup, restore, or verify.
type Event struct {
	Type     EventType
	Module   string
	Message  string
	Summary  *Summary
	Status   *ModuleStatus
	Manifest *Manifest
	Report   *RestoreReport
	Err      error
}

// SnapshotProvider is implemented by modules that create their own repository
// snapshots (files, Wi-Fi) instead of leaving payloads in the metadata stage.
type SnapshotProvider interface {
	Snapshots() []string
}

// PreviewResult describes the effect of restoring one module.
type PreviewResult struct {
	ItemCount int
	Details   string
	Files     int64
	Bytes     int64
	Conflicts int64
}

// Previewer is implemented by modules that can inspect a restored metadata
// directory without changing the system.
type Previewer interface {
	PreviewRestore(ctx context.Context, dir string, opts RestoreOpts) (PreviewResult, error)
}

// Runner orchestrates module backups and restores.
type Runner struct {
	modules []Module
	params  *Params
	onEvent func(Event)
}

// NewRunner creates a Runner. All available modules should be supplied; the
// enabled list selects which run during backup.
func NewRunner(modules []Module, params *Params) *Runner {
	return &Runner{modules: modules, params: params}
}

// SetEventHandler installs a progress callback.
func (r *Runner) SetEventHandler(fn func(Event)) { r.onEvent = fn }

// SetProfile supplies profile-specific file roots and exclusions to modules.
func (r *Runner) SetProfile(name string, profile Profile) {
	if r.params == nil {
		return
	}
	r.params.ProfileName = name
	r.params.Profile = profile
}

// Modules returns the runner's module list in display order.
func (r *Runner) Modules() []Module { return r.modules }

// CheckRepository verifies repository integrity without restoring a backup.
// Set readData to verify stored data blobs as well as repository structure.
func (r *Runner) CheckRepository(ctx context.Context, readData bool) error {
	if r.params == nil || r.params.Repo == nil {
		return errors.New("repository is not configured")
	}
	return r.params.Repo.Check(ctx, readData)
}

func (r *Runner) emit(e Event) {
	if r.onEvent != nil {
		r.onEvent(e)
	}
}

func (r *Runner) log() Logger {
	if r.params != nil && r.params.Logger != nil {
		return r.params.Logger
	}
	return NopLogger()
}

func (r *Runner) module(name string) (Module, bool) {
	for _, m := range r.modules {
		if m.Name() == name {
			return m, true
		}
	}
	return nil, false
}

// Backup runs the selected modules and stores one metadata snapshot plus any
// module-owned payload snapshots in the repository.
func (r *Runner) Backup(ctx context.Context, enabled []string) (*Manifest, error) {
	if r.params == nil || r.params.Repo == nil {
		return nil, errors.New("backup repository is not configured")
	}
	if len(enabled) == 0 {
		for _, m := range r.modules {
			enabled = append(enabled, m.Name())
		}
	}
	selected, err := r.selectedModules(enabled)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, errors.New("no modules selected for backup")
	}

	started := time.Now().UTC()
	if r.params.BackupID == "" {
		r.params.BackupID = NewBackupID(started)
	}
	backupID := r.params.BackupID

	tempRoot := r.params.TempDir
	if tempRoot == "" {
		tempRoot, err = TempRoot()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create staging directory: %w", err)
	}
	stage, err := os.MkdirTemp(tempRoot, "backup-"+backupID+"-")
	if err != nil {
		return nil, fmt.Errorf("create staging directory: %w", err)
	}
	defer removeStage(stage)

	log := r.log().WithContext("backup")
	exists, err := r.params.Repo.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}
	if !exists {
		log.Infof("initializing restic repository %s", r.params.RepoInfo)
		if err := r.params.Repo.Init(ctx); err != nil {
			return nil, fmt.Errorf("initialize repository: %w", err)
		}
	}

	host := CurrentHostInfo()
	manifest := &Manifest{
		SchemaVersion: SchemaVersion,
		BackupID:      backupID,
		ToolVersion:   r.params.ToolVersion,
		Commit:        r.params.Commit,
		GoVersion:     runtimeVersion(),
		OS:            runtimeOS(),
		Arch:          runtimeArch(),
		Hostname:      host.Hostname,
		Username:      host.Username,
		WindowsBuild:  host.WindowsBuild,
		StartedUTC:    started,
		RepoInfo:      r.params.RepoInfo,
		Modules:       make(map[string]ModuleStatus),
		Hashes:        make(map[string]string),
	}
	r.emit(Event{Type: EventBackupStart, Message: backupID})

	failed := 0
	for _, m := range selected {
		status := ModuleStatus{Name: m.Name(), NeedsAdmin: m.NeedsAdmin()}
		if m.NeedsAdmin() && !IsElevated() {
			status.Error = "administrator privileges required (run with UAC elevation)"
			status.Skipped = true
			manifest.Modules[m.Name()] = status
			log.Warnf("skipping %s: %s", m.Name(), status.Error)
			r.emit(Event{Type: EventModuleDone, Module: m.Name(), Status: &status, Err: ErrElevationNeeded})
			failed++
			continue
		}

		r.emit(Event{Type: EventModuleStart, Module: m.Name(), Message: "scanning"})
		scanCtx := WithParams(ctx, r.params)
		summary, err := m.Scan(scanCtx)
		if err != nil {
			status.Error = err.Error()
			status.DurationMS = time.Since(started).Milliseconds()
			manifest.Modules[m.Name()] = status
			log.Errorf("%s scan failed: %v", m.Name(), err)
			r.emit(Event{Type: EventModuleDone, Module: m.Name(), Status: &status, Err: err})
			failed++
			continue
		}
		status.ItemCount = summary.ItemCount
		status.Details = summary.Details
		r.emit(Event{Type: EventModuleScan, Module: m.Name(), Summary: &summary})

		moduleDir := filepath.Join(stage, "modules", m.Name())
		if err := os.MkdirAll(moduleDir, 0o700); err != nil {
			return nil, fmt.Errorf("create staging directory for %s: %w", m.Name(), err)
		}
		r.emit(Event{Type: EventModuleStart, Module: m.Name(), Message: "backing up"})
		moduleStart := time.Now()
		err = m.Backup(WithParams(ctx, r.params), moduleDir)
		status.DurationMS = time.Since(moduleStart).Milliseconds()
		if err != nil {
			status.Error = err.Error()
			manifest.Modules[m.Name()] = status
			log.Errorf("%s backup failed: %v", m.Name(), err)
			r.emit(Event{Type: EventModuleDone, Module: m.Name(), Status: &status, Err: err})
			failed++
			continue
		}
		hashes, err := HashDir(moduleDir)
		if err != nil {
			status.Error = err.Error()
			manifest.Modules[m.Name()] = status
			r.emit(Event{Type: EventModuleDone, Module: m.Name(), Status: &status, Err: err})
			failed++
			continue
		}
		MergeHashes(manifest.Hashes, prefixHashes("modules/"+m.Name()+"/", hashes))
		status.Hash = DigestHashes(hashes)
		if sp, ok := m.(SnapshotProvider); ok {
			status.Snapshots = append([]string(nil), sp.Snapshots()...)
			manifest.SnapshotIDs = append(manifest.SnapshotIDs, status.Snapshots...)
		}
		status.Success = true
		manifest.Modules[m.Name()] = status
		log.Infof("%s backed up: %d items", m.Name(), status.ItemCount)
		r.emit(Event{Type: EventModuleDone, Module: m.Name(), Status: &status})
	}

	manifest.CompletedUTC = time.Now().UTC()
	manifest.DurationMS = manifest.CompletedUTC.Sub(manifest.StartedUTC).Milliseconds()
	manifest.Partial = failed > 0

	// Persist the manifest locally for inspection and inside the encrypted
	// metadata snapshot.
	manifestPath := filepath.Join(stage, "manifest.json")
	if err := WriteManifest(manifestPath, manifest); err != nil {
		return manifest, err
	}
	localPath := filepath.Join(BackupDir(backupID), "manifest.json")
	if err := WriteManifest(localPath, manifest); err != nil {
		log.Warnf("could not write local manifest: %v", err)
	}

	snap, err := r.params.Repo.Backup(ctx, []string{stage}, BackupOptions{
		Tags: BackupTags(backupID, "metadata"),
		Host: host.Hostname,
	})
	if err != nil {
		return manifest, fmt.Errorf("store metadata snapshot: %w", err)
	}
	manifest.SnapshotIDs = append(manifest.SnapshotIDs, snap.ID)
	if err := WriteManifest(localPath, manifest); err != nil {
		log.Warnf("could not update local manifest: %v", err)
	}

	r.emit(Event{Type: EventBackupDone, Manifest: manifest})
	log.Infof("backup %s complete: %d snapshot(s), partial=%v", backupID, len(manifest.SnapshotIDs), manifest.Partial)

	if failed > 0 {
		return manifest, &PartialBackupError{Manifest: manifest}
	}
	return manifest, nil
}

func (r *Runner) selectedModules(names []string) ([]Module, error) {
	requested := make(map[string]bool, len(names))
	for _, n := range names {
		requested[n] = true
	}
	var out []Module
	for _, m := range r.modules {
		if requested[m.Name()] {
			out = append(out, m)
			delete(requested, m.Name())
		}
	}
	if len(requested) > 0 {
		unknown := make([]string, 0, len(requested))
		for n := range requested {
			unknown = append(unknown, n)
		}
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown module(s): %s", strings.Join(unknown, ", "))
	}
	return out, nil
}

// PartialBackupError indicates that one or more modules failed, but the
// metadata snapshot was still stored.
type PartialBackupError struct{ Manifest *Manifest }

func (e *PartialBackupError) Error() string {
	failed := 0
	for _, s := range e.Manifest.Modules {
		if !s.Success && !s.Skipped {
			failed++
		}
	}
	return fmt.Sprintf("backup %s completed with %d failed module(s)", e.Manifest.BackupID, failed)
}

// BackupInfo summarizes one backup for the TUI and CLI.
type BackupInfo struct {
	BackupID   string
	SnapshotID string
	Time       time.Time
	Hostname   string
}

// ListBackups lists metadata snapshots, newest first.
func (r *Runner) ListBackups(ctx context.Context) ([]BackupInfo, error) {
	snaps, err := r.params.Repo.List(ctx, []string{"kind=metadata", "tool=ditch-windows"})
	if err != nil {
		return nil, err
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.After(snaps[j].Time) })
	seen := make(map[string]bool)
	out := make([]BackupInfo, 0, len(snaps))
	for _, s := range snaps {
		id := s.TagValue("backup")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, BackupInfo{BackupID: id, SnapshotID: s.ID, Time: s.Time, Hostname: s.Hostname})
	}
	return out, nil
}

// RestorePreview is a summary of what a restore would change.
type RestorePreview struct {
	BackupID   string
	SnapshotID string
	Time       time.Time
	Hostname   string
	Manifest   *Manifest
	Modules    []ModulePreview
	FileCount  int64
	FileBytes  int64
	Conflicts  int64
}

// ModulePreview is one module's restore preview.
type ModulePreview struct {
	Name       string
	Available  bool
	NeedsAdmin bool
	Elevated   bool
	ItemCount  int
	Details    string
	Files      int64
	Bytes      int64
	Conflicts  int64
	Error      string
}

// RestoreModuleResult is the outcome for one module.
type RestoreModuleResult struct {
	Name       string
	Success    bool
	Skipped    bool
	Error      string
	DurationMS int64
}

// RestoreReport is returned by Restore (including dry runs).
type RestoreReport struct {
	BackupID     string
	SnapshotID   string
	DryRun       bool
	StartedUTC   time.Time
	CompletedUTC time.Time
	DurationMS   int64
	Preview      *RestorePreview
	Modules      []RestoreModuleResult
}

// Restore restores a backup. Empty backupID selects the newest backup.
func (r *Runner) Restore(ctx context.Context, backupID string, opts RestoreOpts) (*RestoreReport, error) {
	if r.params == nil || r.params.Repo == nil {
		return nil, errors.New("restore repository is not configured")
	}
	meta, err := r.metadataSnapshot(ctx, backupID)
	if err != nil {
		return nil, err
	}
	if backupID == "" {
		backupID = meta.TagValue("backup")
	}
	tempRoot := r.params.TempDir
	if tempRoot == "" {
		tempRoot, err = TempRoot()
		if err != nil {
			return nil, err
		}
	}
	stage, err := os.MkdirTemp(tempRoot, "restore-meta-"+backupID+"-")
	if err != nil {
		return nil, err
	}
	defer removeStage(stage)

	started := time.Now().UTC()
	r.emit(Event{Type: EventRestoreStart, Message: backupID})
	if err := r.params.Repo.Restore(ctx, meta.ID, stage, RestoreOptions{}); err != nil {
		return nil, fmt.Errorf("restore metadata: %w", err)
	}
	manifestPath, manifest, err := FindManifest(stage)
	if err != nil {
		return nil, err
	}
	r.params.Manifest = manifest
	report := &RestoreReport{BackupID: backupID, SnapshotID: meta.ID, DryRun: opts.DryRun, StartedUTC: started}

	preview, err := r.buildPreview(ctx, manifestPath, manifest, meta, opts)
	if err != nil {
		return nil, err
	}
	report.Preview = preview
	if opts.DryRun {
		report.CompletedUTC = time.Now().UTC()
		report.DurationMS = report.CompletedUTC.Sub(report.StartedUTC).Milliseconds()
		r.emit(Event{Type: EventRestoreDone, Report: report})
		return report, nil
	}

	selected := make(map[string]bool, len(opts.SelectedModules))
	for _, n := range opts.SelectedModules {
		selected[n] = true
	}
	modulesRoot := filepath.Dir(manifestPath)
	failed := 0
	priorReport := r.params.Report
	defer func() { r.params.Report = priorReport }()
	for _, status := range manifest.SortModuleStatuses() {
		if len(selected) > 0 && !selected[status.Name] {
			continue
		}
		m, ok := r.module(status.Name)
		if !ok {
			report.Modules = append(report.Modules, RestoreModuleResult{Name: status.Name, Error: "module not available in this build"})
			failed++
			continue
		}
		if !status.Success && !status.Skipped {
			report.Modules = append(report.Modules, RestoreModuleResult{Name: status.Name, Skipped: true, Error: "backup did not capture this module"})
			continue
		}
		if m.NeedsAdmin() && !IsElevated() {
			report.Modules = append(report.Modules, RestoreModuleResult{Name: status.Name, Error: "administrator privileges required"})
			failed++
			continue
		}
		moduleDir := filepath.Join(modulesRoot, "modules", status.Name)
		r.emit(Event{Type: EventRestoreModule, Module: status.Name, Message: "restoring"})
		r.params.Report = func(msg string) {
			r.emit(Event{Type: EventMessage, Module: status.Name, Message: msg})
			r.log().Infof("%s: %s", status.Name, msg)
		}
		moduleStart := time.Now()
		err := m.Restore(WithParams(ctx, r.params), moduleDir, opts)
		result := RestoreModuleResult{Name: status.Name, DurationMS: time.Since(moduleStart).Milliseconds()}
		if err != nil {
			result.Error = err.Error()
			failed++
			r.log().Errorf("%s restore failed: %v", status.Name, err)
		} else {
			result.Success = true
		}
		report.Modules = append(report.Modules, result)
		r.emit(Event{Type: EventRestoreModule, Module: status.Name, Message: "done", Err: err})
	}
	report.CompletedUTC = time.Now().UTC()
	report.DurationMS = report.CompletedUTC.Sub(report.StartedUTC).Milliseconds()
	r.emit(Event{Type: EventRestoreDone, Report: report})
	if failed > 0 {
		return report, fmt.Errorf("restore completed with %d failed module(s)", failed)
	}
	return report, nil
}

func (r *Runner) metadataSnapshot(ctx context.Context, backupID string) (Snapshot, error) {
	tags := []string{"kind=metadata", "tool=ditch-windows"}
	if backupID != "" {
		tags = append(tags, "backup="+backupID)
	}
	snaps, err := r.params.Repo.List(ctx, tags)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list backups: %w", err)
	}
	if len(snaps) == 0 {
		if backupID == "" {
			return Snapshot{}, errors.New("no ditch-windows backups found in this repository")
		}
		return Snapshot{}, fmt.Errorf("backup %q not found", backupID)
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.After(snaps[j].Time) })
	return snaps[0], nil
}

func (r *Runner) buildPreview(ctx context.Context, manifestPath string, manifest *Manifest, meta Snapshot, opts RestoreOpts) (*RestorePreview, error) {
	p := &RestorePreview{
		BackupID:   manifest.BackupID,
		SnapshotID: meta.ID,
		Time:       manifest.StartedUTC,
		Hostname:   manifest.Hostname,
		Manifest:   manifest,
	}
	modulesRoot := filepath.Dir(manifestPath)
	for _, status := range manifest.SortModuleStatuses() {
		mp := ModulePreview{
			Name:       status.Name,
			NeedsAdmin: status.NeedsAdmin,
			Elevated:   IsElevated(),
			ItemCount:  status.ItemCount,
			Details:    status.Details,
		}
		m, ok := r.module(status.Name)
		if !ok {
			mp.Error = "module not available in this build"
		} else {
			mp.Available = true
			if previewer, ok := m.(Previewer); ok {
				dir := filepath.Join(modulesRoot, "modules", status.Name)
				result, err := previewer.PreviewRestore(WithParams(ctx, r.params), dir, opts)
				if err != nil {
					mp.Error = err.Error()
				} else {
					mp.ItemCount = result.ItemCount
					mp.Details = result.Details
					mp.Files = result.Files
					mp.Bytes = result.Bytes
					mp.Conflicts = result.Conflicts
					p.FileCount += result.Files
					p.FileBytes += result.Bytes
					p.Conflicts += result.Conflicts
				}
			}
		}
		p.Modules = append(p.Modules, mp)
	}
	return p, nil
}

// VerifyReport is returned by Verify.
type VerifyReport struct {
	BackupID        string
	SnapshotID      string
	RepositoryOK    bool
	RepositoryError string
	ManifestHashOK  bool
	ManifestHashErr string
	Modules         []VerifyModuleResult
}

// VerifyModuleResult is one module's verification outcome.
type VerifyModuleResult struct {
	Name     string
	Verified bool
	Error    string
}

// Verify checks the repository, manifest hashes, and module metadata.
func (r *Runner) Verify(ctx context.Context, backupID string) (*VerifyReport, error) {
	meta, err := r.metadataSnapshot(ctx, backupID)
	if err != nil {
		return nil, err
	}
	if backupID == "" {
		backupID = meta.TagValue("backup")
	}
	report := &VerifyReport{BackupID: backupID, SnapshotID: meta.ID}
	if err := r.params.Repo.Check(ctx, false); err != nil {
		report.RepositoryError = err.Error()
	} else {
		report.RepositoryOK = true
	}

	tempRoot := r.params.TempDir
	if tempRoot == "" {
		tempRoot, err = TempRoot()
		if err != nil {
			return report, err
		}
	}
	stage, err := os.MkdirTemp(tempRoot, "verify-"+backupID+"-")
	if err != nil {
		return report, err
	}
	defer removeStage(stage)
	if err := r.params.Repo.Restore(ctx, meta.ID, stage, RestoreOptions{}); err != nil {
		return report, fmt.Errorf("restore metadata for verification: %w", err)
	}
	manifestPath, manifest, err := FindManifest(stage)
	if err != nil {
		return report, err
	}
	r.params.Manifest = manifest
	if err := VerifyHashes(filepath.Dir(manifestPath), manifest.Hashes); err != nil {
		report.ManifestHashErr = err.Error()
	} else {
		report.ManifestHashOK = true
	}
	modulesRoot := filepath.Dir(manifestPath)
	for _, status := range manifest.SortModuleStatuses() {
		if !status.Success {
			continue
		}
		m, ok := r.module(status.Name)
		if !ok {
			report.Modules = append(report.Modules, VerifyModuleResult{Name: status.Name, Error: "module not available"})
			continue
		}
		dir := filepath.Join(modulesRoot, "modules", status.Name)
		if err := m.Verify(WithParams(ctx, r.params), dir); err != nil {
			report.Modules = append(report.Modules, VerifyModuleResult{Name: status.Name, Error: err.Error()})
		} else {
			report.Modules = append(report.Modules, VerifyModuleResult{Name: status.Name, Verified: true})
		}
	}
	var problems []string
	if !report.RepositoryOK {
		problems = append(problems, "repository check failed: "+report.RepositoryError)
	}
	if !report.ManifestHashOK {
		problems = append(problems, "manifest hash verification failed: "+report.ManifestHashErr)
	}
	for _, m := range report.Modules {
		if !m.Verified {
			problems = append(problems, m.Name+": "+m.Error)
		}
	}
	if len(problems) > 0 {
		return report, errors.New(strings.Join(problems, "; "))
	}
	return report, nil
}

func prefixHashes(prefix string, hashes map[string]string) map[string]string {
	out := make(map[string]string, len(hashes))
	for k, v := range hashes {
		out[prefix+k] = v
	}
	return out
}

func removeStage(path string) {
	if path == "" {
		return
	}
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			_ = os.Chmod(p, 0o600)
		}
		return nil
	})
	_ = os.RemoveAll(path)
}
