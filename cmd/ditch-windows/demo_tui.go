package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/demo"
	"github.com/mithun-srinivasan/ditch-windows/internal/tui"
)

func (s *demoSession) runTUI() error {
	// The automated tour streams runner events to stdout. A full-screen
	// bubbletea program owns the terminal, so the TUI adapter stays silent and
	// reports progress with its own view instead.
	s.runner.SetEventHandler(func(core.Event) {})
	return tui.RunDemo(tui.DemoActions{
		Backup:  s.tuiBackup,
		List:    s.tuiList,
		Preview: s.tuiPreview,
		Apply:   s.tuiApply,
		Verify:  s.tuiVerify,
		Files:   s.tuiFiles,
	})
}

func (s *demoSession) tuiBackup(ctx context.Context) (tui.BackupResult, error) {
	s.runner.SetProfile("demo", s.profile)
	s.params.BackupID = ""
	manifest, err := s.runner.Backup(ctx, s.profile.EnabledModules)
	if manifest == nil {
		return tui.BackupResult{}, err
	}
	rows := make([]tui.ModuleRow, 0, len(manifest.Modules))
	for _, status := range manifest.SortModuleStatuses() {
		rows = append(rows, tui.ModuleRow{
			Name:    status.Name,
			Status:  moduleStatusText(status),
			Items:   status.ItemCount,
			Details: status.Details,
		})
	}
	return tui.BackupResult{
		BackupID:  manifest.BackupID,
		Snapshots: len(manifest.SnapshotIDs),
		Modules:   rows,
	}, err
}

func (s *demoSession) tuiPreview(ctx context.Context) (tui.PreviewResult, error) {
	if err := demo.SimulateLocalEdit(s.home); err != nil {
		return tui.PreviewResult{}, err
	}
	report, err := s.runner.Restore(ctx, "", core.RestoreOpts{DryRun: true, ConflictMode: core.OverwriteKeepBak})
	if report == nil || report.Preview == nil {
		return tui.PreviewResult{}, err
	}
	rows := make([]tui.ModuleRow, 0, len(report.Preview.Modules))
	for _, m := range report.Preview.Modules {
		status := "ready"
		details := m.Details
		if !m.Available {
			status = "unavailable"
		}
		if m.Error != "" {
			status = "error"
			if details != "" {
				details += " — "
			}
			details += m.Error
		}
		rows = append(rows, tui.ModuleRow{
			Name:      m.Name,
			Status:    status,
			Items:     m.ItemCount,
			Conflicts: int(m.Conflicts),
			Details:   details,
		})
	}
	return tui.PreviewResult{
		BackupID:  report.Preview.BackupID,
		Modules:   rows,
		Files:     report.Preview.FileCount,
		Bytes:     report.Preview.FileBytes,
		Conflicts: report.Preview.Conflicts,
	}, err
}

func (s *demoSession) tuiApply(ctx context.Context) (tui.RestoreResult, error) {
	if err := demo.SimulateLocalEdit(s.home); err != nil {
		return tui.RestoreResult{}, err
	}
	report, err := s.runner.Restore(ctx, "", core.RestoreOpts{ConflictMode: core.OverwriteKeepBak})
	if report == nil {
		return tui.RestoreResult{}, err
	}
	rows := make([]tui.ModuleRow, 0, len(report.Modules))
	for _, m := range report.Modules {
		status := "ok"
		details := ""
		if m.Skipped {
			status = "skipped"
		}
		if m.Error != "" {
			status = "failed"
			details = m.Error
		}
		rows = append(rows, tui.ModuleRow{Name: m.Name, Status: status, Details: details})
	}
	baks, _ := findBakFiles(s.home)
	for i := range baks {
		if rel, relErr := filepath.Rel(s.home, baks[i]); relErr == nil {
			baks[i] = filepath.ToSlash(rel)
		}
	}
	return tui.RestoreResult{Modules: rows, Baks: baks}, err
}

func (s *demoSession) tuiVerify(ctx context.Context) (tui.VerifyResult, error) {
	report, err := s.runner.Verify(ctx, "")
	if report == nil {
		return tui.VerifyResult{Message: errorText(err)}, err
	}
	message := errorText(err)
	return tui.VerifyResult{
		BackupID: report.BackupID,
		RepoOK:   report.RepositoryOK,
		HashOK:   report.ManifestHashOK,
		Modules:  len(report.Modules),
		Message:  message,
	}, err
}

func (s *demoSession) tuiList(ctx context.Context) ([]tui.BackupRow, error) {
	backups, err := s.runner.ListBackups(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]tui.BackupRow, 0, len(backups))
	for _, b := range backups {
		rows = append(rows, tui.BackupRow{
			ID:   b.BackupID,
			Time: b.Time.Local().Format("2006-01-02 15:04:05"),
			Host: b.Hostname,
		})
	}
	return rows, nil
}

func (s *demoSession) tuiFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(s.home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(s.home, path)
		if relErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		files = append(files, fmt.Sprintf("%-48s %6d B", filepath.ToSlash(rel), info.Size()))
		return nil
	})
	return files, err
}

func moduleStatusText(s core.ModuleStatus) string {
	switch {
	case s.Success:
		return "ok"
	case s.Skipped:
		return "skipped"
	default:
		return "failed"
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
