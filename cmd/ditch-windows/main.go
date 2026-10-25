package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/module"
	"github.com/mithun-srinivasan/ditch-windows/internal/restic"
	"github.com/mithun-srinivasan/ditch-windows/internal/storage"
)

var version = "dev"
var commit = "unknown"
var date = "unknown"

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func execute(args []string) error {
	if len(args) == 0 || args[0] == "tui" {
		return menu()
	}
	switch args[0] {
	case "help", "-h", "--help":
		usage()
		return nil
	case "version":
		fmt.Printf("ditch-windows %s (commit %s, built %s)\n", version, commit, date)
		return nil
	case "config":
		return configCommand(args[1:])
	case "backup":
		return backupCommand(args[1:])
	case "restore":
		return restoreCommand(args[1:])
	case "list":
		return listCommand(args[1:])
	case "verify":
		return verifyCommand(args[1:])
	case "demo":
		return demoCommand(args[1:])
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Println(`ditch-windows - Windows backup and restore

Usage:
  ditch-windows [tui]
  ditch-windows config init --repo <path-or-restic-url>
  ditch-windows config show
  ditch-windows backup [--profile daily] [--repo <url>]
  ditch-windows list [--repo <url>]
  ditch-windows restore [backup-id] [--dry-run|--apply] [--repo <url>]
  ditch-windows verify [backup-id] [--read-data] [--repo <url>]
  ditch-windows demo [--interactive] [--apply]
  ditch-windows version

The demo command is an isolated, synthetic sandbox intended for Docker. It
never reads the real user profile; see README.md for the container workflow.

Set DITCH_RESTIC_BINARY to select a restic executable and RESTIC_PASSWORD to
supply the repository password non-interactively.`)
}

func configCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ditch-windows config init|show")
	}
	switch args[0] {
	case "show":
		cfg, err := core.LoadConfig()
		if err != nil {
			return err
		}
		fmt.Printf("Config: %s\nRepository: %s\nDefault profile: %s\nConflict mode: %s\n", core.ConfigPath(), cfg.RepoURL, cfg.DefaultProfile, cfg.ActiveConflictMode())
		profileNames := make([]string, 0, len(cfg.Profiles))
		for name := range cfg.Profiles {
			profileNames = append(profileNames, name)
		}
		sort.Strings(profileNames)
		for _, name := range profileNames {
			p := cfg.Profiles[name]
			fmt.Printf("  %s: %s\n", name, strings.Join(p.EnabledModules, ", "))
		}
		return nil
	case "init":
		fs := flag.NewFlagSet("config init", flag.ContinueOnError)
		repo := fs.String("repo", "", "restic repository path or URL")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *repo == "" {
			return errors.New("--repo is required")
		}
		_, url, err := storage.BackendFor(*repo)
		if err != nil {
			return err
		}
		cfg := core.DefaultConfig()
		cfg.RepoURL = url
		if err := core.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Configuration saved to %s\nRepository: %s\n", core.ConfigPath(), url)
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func backupCommand(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile to back up (defaults to configured profile)")
	repoURL := fs.String("repo", "", "override repository URL")
	headless := fs.Bool("headless", false, "run without interactive menus")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = headless
	cfg, runner, logger, err := runtimeFor(*repoURL)
	if err != nil {
		return err
	}
	defer logger.Close()
	name := *profileName
	if name == "" {
		name = cfg.DefaultProfile
	}
	p, err := cfg.Profile(name)
	if err != nil {
		return err
	}
	runner.SetProfile(name, p)
	for _, moduleName := range p.EnabledModules {
		for _, m := range module.All() {
			if m.Name() == moduleName && m.NeedsAdmin() && !core.IsElevated() {
				err := core.EnsureElevation(true)
				if errors.Is(err, core.ErrRelaunched) {
					return nil
				}
				if err != nil {
					return err
				}
			}
		}
	}
	fmt.Printf("Backing up profile %q\n", name)
	manifest, err := runner.Backup(context.Background(), p.EnabledModules)
	if manifest != nil {
		for _, s := range manifest.SortModuleStatuses() {
			fmt.Println(s.SummaryLine())
		}
		fmt.Printf("Backup ID: %s\n", manifest.BackupID)
	}
	return err
}

func listCommand(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	repoURL := fs.String("repo", "", "override repository URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, runner, logger, err := runtimeFor(*repoURL)
	if err != nil {
		return err
	}
	defer logger.Close()
	backups, err := runner.ListBackups(context.Background())
	if err != nil {
		return err
	}
	if len(backups) == 0 {
		fmt.Println("No backups found.")
		return nil
	}
	for _, b := range backups {
		fmt.Printf("%-24s  %s  %s\n", b.BackupID, b.Time.Local().Format(time.RFC3339), b.Hostname)
	}
	return nil
}

func restoreCommand(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	repoURL := fs.String("repo", "", "override repository URL")
	dryRun := fs.Bool("dry-run", true, "show changes without applying them (default)")
	apply := fs.Bool("apply", false, "apply the restore after showing the preview")
	profileName := fs.String("profile", "", "profile name (used for display only)")
	headless := fs.Bool("headless", false, "run without interactive menus")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = headless
	if fs.NArg() > 1 {
		return errors.New("restore accepts at most one backup ID")
	}
	backupID := ""
	if fs.NArg() == 1 {
		backupID = fs.Arg(0)
	}
	if *apply {
		*dryRun = false
	}
	cfg, runner, logger, err := runtimeFor(*repoURL)
	if err != nil {
		return err
	}
	defer logger.Close()
	if *profileName != "" {
		fmt.Printf("Profile: %s\n", *profileName)
	}
	// Always preview first. Applying is deliberately a second, explicit step.
	mode := cfg.ActiveConflictMode()
	preview, err := runner.Restore(context.Background(), backupID, core.RestoreOpts{DryRun: true, ConflictMode: mode, AskPerItem: mode == core.AskPerItem})
	if err != nil {
		return err
	}
	printPreview(preview.Preview)
	if *dryRun {
		fmt.Println("Dry run only; no changes were made. Use --apply to restore.")
		return nil
	}
	if !*apply {
		return errors.New("restore changes require --apply")
	}
	for _, m := range preview.Preview.Modules {
		if m.NeedsAdmin && !core.IsElevated() {
			err := core.EnsureElevation(true)
			if errors.Is(err, core.ErrRelaunched) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	if !confirm("Apply this restore? [y/N] ") {
		return errors.New("restore cancelled")
	}
	opts := core.RestoreOpts{ConflictMode: mode, AskPerItem: mode == core.AskPerItem}
	if opts.AskPerItem {
		reader := bufio.NewReader(os.Stdin)
		opts.Ask = func(path string) (core.ConflictMode, error) {
			fmt.Printf("Replace existing %s? [y/N] ", path)
			line, readErr := reader.ReadString('\n')
			if readErr != nil && len(line) == 0 {
				return core.Skip, readErr
			}
			if strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes") {
				return core.OverwriteKeepBak, nil
			}
			return core.Skip, nil
		}
	}
	report, err := runner.Restore(context.Background(), backupID, opts)
	if report != nil {
		for _, r := range report.Modules {
			state := "ok"
			if r.Skipped {
				state = "skipped"
			}
			if r.Error != "" {
				state = "FAILED"
			}
			fmt.Printf("%-14s %s %s\n", r.Name, state, r.Error)
		}
	}
	return err
}

func verifyCommand(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	repoURL := fs.String("repo", "", "override repository URL")
	readData := fs.Bool("read-data", false, "read and verify repository data blobs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("verify accepts at most one backup ID")
	}
	backupID := ""
	if fs.NArg() == 1 {
		backupID = fs.Arg(0)
	}
	_, runner, logger, err := runtimeFor(*repoURL)
	if err != nil {
		return err
	}
	defer logger.Close()
	if *readData {
		fmt.Println("Checking repository data; this may take a while...")
	}
	if err := runner.CheckRepository(context.Background(), *readData); err != nil {
		return err
	}
	report, err := runner.Verify(context.Background(), backupID)
	if report != nil {
		fmt.Printf("Backup %s: %d module(s) checked\n", report.BackupID, len(report.Modules))
	}
	return err
}

func runtimeFor(override string) (*core.Config, *core.Runner, core.Logger, error) {
	cfg, err := core.LoadConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	repoSpec := cfg.RepoURL
	if override != "" {
		repoSpec = override
	}
	if repoSpec == "" {
		return nil, nil, nil, fmt.Errorf("repository is not configured; run config init --repo <path>")
	}
	_, repoURL, err := storage.BackendFor(repoSpec)
	if err != nil {
		return nil, nil, nil, err
	}
	binary, err := restic.FindBinary(cfg.ResticPath)
	if err != nil {
		return nil, nil, nil, err
	}
	password, err := getPassphrase()
	if err != nil {
		return nil, nil, nil, err
	}
	repo := restic.New(repoURL, password, binary)
	if os.Getenv("RESTIC_PASSWORD") == "" {
		exists, existsErr := repo.Exists(context.Background())
		if existsErr != nil {
			return nil, nil, nil, existsErr
		}
		if !exists {
			confirmPassword, readErr := readSecret("Confirm new repository passphrase: ")
			if readErr != nil {
				return nil, nil, nil, readErr
			}
			if confirmPassword != password {
				return nil, nil, nil, errors.New("passphrases do not match")
			}
		}
	}
	logger, err := core.NewDailyFileLogger(core.LogsDir())
	if err != nil {
		return nil, nil, nil, err
	}
	params := &core.Params{Repo: repo, RepoInfo: storage.SanitizeRepoURL(repoURL), Config: cfg, Exec: core.NewDefaultExec(), Logger: logger, ToolVersion: version, Commit: commit}
	return cfg, core.NewRunner(module.All(), params), logger, nil
}

func printPreview(p *core.RestorePreview) {
	if p == nil {
		return
	}
	fmt.Printf("Restore preview: backup %s from %s (%s)\n", p.BackupID, p.Time.Local().Format(time.RFC3339), p.Hostname)
	for _, m := range p.Modules {
		state := "ready"
		if !m.Available {
			state = "unavailable"
		}
		if m.Error != "" {
			state += ": " + m.Error
		}
		fmt.Printf("  %-14s %-12s %4d items", m.Name, state, m.ItemCount)
		if m.Details != "" {
			fmt.Printf(" — %s", m.Details)
		}
		fmt.Println()
	}
	fmt.Printf("Files: %d, bytes: %d, conflicts: %d\n", p.FileCount, p.FileBytes, p.Conflicts)
}

func getPassphrase() (string, error) {
	if value := os.Getenv("RESTIC_PASSWORD"); value != "" {
		return value, nil
	}
	value, err := readSecret("Restic passphrase: ")
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	if value == "" {
		return "", errors.New("passphrase cannot be empty")
	}
	return value, nil
}

func readSecret(prompt string) (string, error) {
	fmt.Print(prompt)
	if term.IsTerminal(os.Stdin.Fd()) {
		secret, err := term.ReadPassword(os.Stdin.Fd())
		fmt.Println()
		return string(secret), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes")
}

func menu() error {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("\nditch-windows")
		fmt.Println("1) Back up  2) List backups  3) Preview restore  4) Verify repository  5) Show config  6) Quit")
		fmt.Print("Choose: ")
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return nil
		}
		var cmd []string
		switch strings.TrimSpace(line) {
		case "1":
			cmd = []string{"backup"}
		case "2":
			cmd = []string{"list"}
		case "3":
			cmd = []string{"restore", "--dry-run"}
		case "4":
			cmd = []string{"verify"}
		case "5":
			cmd = []string{"config", "show"}
		case "6", "q", "quit":
			return nil
		default:
			fmt.Println("Choose a number from 1 to 6.")
			continue
		}
		if err := execute(cmd); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
	}
}
