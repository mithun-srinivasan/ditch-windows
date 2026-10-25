package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/demo"
	"github.com/mithun-srinivasan/ditch-windows/internal/restic"
)

type demoSession struct {
	base    string
	home    string
	cfg     *core.Config
	params  *core.Params
	runner  *core.Runner
	logger  core.Logger
	profile core.Profile
}

func demoCommand(args []string) error {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	baseFlag := fs.String("base", "", "demo directory (default $DITCH_DEMO_DIR or a temp folder)")
	interactive := fs.Bool("interactive", false, "show an interactive demo menu")
	apply := fs.Bool("apply", false, "perform a demo restore inside the container-only demo home")
	if err := fs.Parse(args); err != nil {
		return err
	}

	base := *baseFlag
	if base == "" {
		base = os.Getenv("DITCH_DEMO_DIR")
	}
	if base == "" {
		base = filepath.Join(os.TempDir(), "ditch-windows-demo")
	}
	base, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	if base == filepath.VolumeName(base)+string(filepath.Separator) {
		return fmt.Errorf("refusing to use filesystem root %q as the demo directory", base)
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}

	// The demo never reads from or writes to the real user profile. Config,
	// logs, local manifests, fixtures, and the repository all live under base.
	stateDir := filepath.Join(base, "state")
	home := filepath.Join(base, "home")
	repoPath := filepath.Join(base, "repo")
	tempDir := filepath.Join(base, "tmp")
	for _, dir := range []string{stateDir, home, repoPath, tempDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := os.Setenv("DITCH_WINDOWS_HOME", stateDir); err != nil {
		return err
	}
	if err := demo.SeedHome(home); err != nil {
		return fmt.Errorf("seed demo home: %w", err)
	}

	cfg := core.DefaultConfig()
	cfg.RepoURL = repoPath
	cfg.DefaultProfile = "demo"
	profile := demo.DemoProfile()
	cfg.Profiles["demo"] = profile
	// The demo config intentionally uses synthetic module names, so it is kept
	// in memory rather than saved through the production config validator.

	password := os.Getenv("RESTIC_PASSWORD")
	if password == "" {
		password = "ditch-demo-only"
		if err := os.Setenv("RESTIC_PASSWORD", password); err != nil {
			return err
		}
	}
	binary, err := restic.FindBinary(cfg.ResticPath)
	if err != nil {
		return err
	}
	repo := restic.New(repoPath, password, binary)
	logger, err := core.NewDailyFileLogger(core.LogsDir())
	if err != nil {
		return err
	}
	defer logger.Close()
	params := &core.Params{
		Exec:        core.NewDefaultExec(),
		Repo:        repo,
		RepoInfo:    repoPath,
		Config:      cfg,
		Profile:     profile,
		ProfileName: "demo",
		HomeDir:     home,
		TempDir:     tempDir,
		ToolVersion: version,
		Commit:      commit,
		Logger:      logger,
	}
	runner := core.NewRunner(demo.Modules(), params)
	runner.SetEventHandler(func(e core.Event) {
		switch e.Type {
		case core.EventMessage:
			if e.Module != "" && e.Message != "" {
				fmt.Printf("    %s: %s\n", e.Module, e.Message)
			}
		case core.EventWarning, core.EventModuleStart:
			if e.Message != "" && e.Message != "backing up" && e.Message != "scanning" {
				fmt.Printf("    %s: %s\n", e.Module, e.Message)
			}
		}
	})
	s := &demoSession{base: base, home: home, cfg: cfg, params: params, runner: runner, logger: logger, profile: profile}

	fmt.Println("ditch-windows — isolated Docker demo")
	fmt.Printf("  demo root:     %s\n", base)
	fmt.Printf("  synthetic home: %s\n", home)
	fmt.Printf("  restic repo:   %s\n", repoPath)
	fmt.Println("  demo passphrase: ditch-demo-only (published; synthetic data only)")
	fmt.Println("  Your real files are not mounted or read. Everything above is disposable.")
	fmt.Println()

	if *interactive {
		return s.runMenu()
	}
	return s.runTour(*apply)
}

func (s *demoSession) runTour(apply bool) error {
	if err := s.backup(); err != nil {
		return err
	}
	if err := s.list(); err != nil {
		return err
	}
	fmt.Println("\nSimulating a local edit made after the backup...")
	if err := demo.SimulateLocalEdit(s.home); err != nil {
		return err
	}
	if err := s.preview(); err != nil {
		return err
	}
	if apply {
		fmt.Println("\nApplying the restore to the synthetic demo home (never the host)...")
		if err := s.applyRestore(); err != nil {
			return err
		}
	} else {
		fmt.Println("\nRestore was a dry run. Re-run with --apply to see .bak conflict handling inside the demo.")
	}
	if err := s.verify(); err != nil {
		return err
	}
	fmt.Println("\nDemo complete. This container can be removed with no effect on your machine.")
	fmt.Println("Interactive menu: ditch-windows demo --interactive")
	return nil
}

func (s *demoSession) runMenu() error {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("\nDocker demo menu")
		fmt.Println("1) Back up synthetic home   2) List backups   3) Preview restore")
		fmt.Println("4) Apply restore (demo only) 5) Verify          6) Show demo files   7) Quit")
		fmt.Print("Choose: ")
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return nil
		}
		switch strings.TrimSpace(line) {
		case "1":
			err = s.backup()
		case "2":
			err = s.list()
		case "3":
			err = demo.SimulateLocalEdit(s.home)
			if err == nil {
				err = s.preview()
			}
		case "4":
			err = s.applyRestore()
		case "5":
			err = s.verify()
		case "6":
			err = s.showFiles()
		case "7", "q", "quit":
			return nil
		default:
			fmt.Println("Choose a number from 1 to 7.")
			continue
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
	}
}

func (s *demoSession) backup() error {
	// Each run should get a fresh backup ID; the runner normally generates one
	// but keeps it for the lifetime of the params.
	s.runner.SetProfile("demo", s.profile)
	s.params.BackupID = ""
	fmt.Println("Backing up demo modules: apps, wifi, files, theme")
	manifest, err := s.runner.Backup(context.Background(), s.profile.EnabledModules)
	if manifest != nil {
		fmt.Println()
		for _, status := range manifest.SortModuleStatuses() {
			fmt.Printf("  %-8s %-10s %s\n", status.Name, statusState(status), status.Details)
			if status.Error != "" {
				fmt.Printf("           %s\n", status.Error)
			}
		}
		fmt.Printf("\nBackup ID: %s\n", manifest.BackupID)
		fmt.Printf("Snapshots: %d (metadata + module payloads)\n", len(manifest.SnapshotIDs))
	}
	return err
}

func (s *demoSession) list() error {
	backups, err := s.runner.ListBackups(context.Background())
	if err != nil {
		return err
	}
	if len(backups) == 0 {
		fmt.Println("No demo backups yet.")
		return nil
	}
	fmt.Println("\nBackups in the demo repository:")
	for _, b := range backups {
		fmt.Printf("  %-24s %s  %s\n", b.BackupID, b.Time.Local().Format(time.RFC3339), b.Hostname)
	}
	return nil
}

func (s *demoSession) preview() error {
	report, err := s.runner.Restore(context.Background(), "", core.RestoreOpts{
		DryRun:       true,
		ConflictMode: core.OverwriteKeepBak,
	})
	if err != nil {
		return err
	}
	printPreview(report.Preview)
	fmt.Println("Dry run only; the synthetic home was not changed.")
	return nil
}

func (s *demoSession) applyRestore() error {
	report, err := s.runner.Restore(context.Background(), "", core.RestoreOpts{
		ConflictMode: core.OverwriteKeepBak,
	})
	if err != nil {
		return err
	}
	fmt.Println("\nRestore results:")
	for _, m := range report.Modules {
		state := "ok"
		if m.Skipped {
			state = "skipped"
		}
		if m.Error != "" {
			state = "FAILED: " + m.Error
		}
		fmt.Printf("  %-8s %s\n", m.Name, state)
	}
	baks, _ := findBakFiles(s.home)
	if len(baks) > 0 {
		fmt.Println("\nConflict handling: existing files were preserved:")
		for _, bak := range baks {
			rel, _ := filepath.Rel(s.home, bak)
			fmt.Println("  " + filepath.ToSlash(rel))
		}
	}
	return nil
}

func findBakFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.Contains(filepath.Base(path), ".bak") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func (s *demoSession) verify() error {
	fmt.Println("\nVerifying repository and manifest hashes...")
	report, err := s.runner.Verify(context.Background(), "")
	if report != nil {
		fmt.Printf("Verified backup %s: repository_ok=%v manifest_hashes_ok=%v modules=%d\n",
			report.BackupID, report.RepositoryOK, report.ManifestHashOK, len(report.Modules))
	}
	return err
}

func (s *demoSession) showFiles() error {
	fmt.Println("\nSynthetic demo home:")
	err := filepath.WalkDir(s.home, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(s.home, path)
		if relErr != nil {
			return relErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		fmt.Printf("  %-50s %d bytes\n", filepath.ToSlash(rel), info.Size())
		return nil
	})
	return err
}

func statusState(s core.ModuleStatus) string {
	switch {
	case s.Success:
		return "ok"
	case s.Skipped:
		return "skipped"
	default:
		return "FAILED"
	}
}
