package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/restic"
	"github.com/mithun-srinivasan/ditch-windows/internal/storage"
)

// setupOptions controls the one-command bootstrap.
type setupOptions struct {
	// Repo overrides the repository path or URL.
	Repo string
	// NoDownload disables the automatic restic download.
	NoDownload bool
	// SkipUI disables building the terminal UI from ui/.
	SkipUI bool
}

// setupCommand makes a checkout or release usable: restic present, a
// repository configured, and the terminal UI built when its sources exist.
// It is safe to run repeatedly.
func setupCommand(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	repo := fs.String("repo", "", "restic repository path or URL (defaults to ~/ditch-windows-repo)")
	noDownload := fs.Bool("no-download", false, "do not download restic automatically")
	skipUI := fs.Bool("skip-ui", false, "do not build the terminal UI from ui/")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runSetup(setupOptions{Repo: *repo, NoDownload: *noDownload, SkipUI: *skipUI})
}

func runSetup(opts setupOptions) error {
	cfg, err := core.LoadConfig()
	if err != nil {
		return err
	}

	fmt.Println("ditch-windows setup")
	fmt.Println()

	// 1. restic: the backup engine everything else drives.
	binary, err := restic.FindBinary(cfg.ResticPath)
	if err != nil {
		if opts.NoDownload {
			return errors.New("restic was not found; install it (winget install restic.restic) or re-run setup without --no-download")
		}
		fmt.Println("restic is not installed; fetching it for this user...")
		binary, err = restic.EnsureBinary(context.Background(), cfg.ResticPath, restic.EnsureOptions{
			AllowDownload: true,
			Status:        func(line string) { fmt.Println("  " + line) },
		})
		if err != nil {
			return err
		}
	}
	fmt.Printf("restic:     %s\n", binary)

	// 2. Repository configuration. Only write the config when it is missing or
	// the caller asked for a specific repository, so re-running setup never
	// clobbers a working install.
	spec := opts.Repo
	if spec == "" {
		spec = cfg.RepoURL
	}
	if spec == "" {
		spec = core.DefaultRepoPath()
	}
	_, url, err := storage.BackendFor(spec)
	if err != nil {
		return err
	}
	if opts.Repo != "" || cfg.RepoURL == "" {
		cfg.RepoURL = url
		if err := core.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("repository: %s\n", url)
		fmt.Printf("config:     %s\n", core.ConfigPath())
	} else {
		fmt.Printf("repository: %s (already configured)\n", url)
	}

	// 3. Terminal UI: use an existing build or compile the TypeScript sources.
	if opts.SkipUI {
		fmt.Println("TUI:        skipped (--skip-ui)")
	} else {
		reportUI()
	}

	fmt.Println()
	fmt.Println("Setup complete. Next steps:")
	fmt.Println("  ditch-windows          open the terminal UI")
	fmt.Println("  ditch-windows demo     run the safe synthetic demo (no real files)")
	fmt.Println("  ditch-windows backup   back up this PC")
	return nil
}

// reportUI resolves the frontend and builds it when only sources are present.
func reportUI() {
	if spec, err := resolveTUICommand(); err == nil {
		fmt.Printf("TUI:        ready (%s)\n", spec.source)
		return
	}
	root := findUIRoot()
	if root == "" {
		fmt.Println("TUI:        unavailable (no bundled frontend); the numbered menu still works")
		return
	}
	if info, err := os.Stat(filepath.Join(root, "dist", "index.js")); err == nil && !info.IsDir() && uiDepsInstalled(root) {
		fmt.Printf("TUI:        ready (%s)\n", filepath.Join(root, "dist", "index.js"))
		return
	}
	installer, runner, err := uiToolchain()
	if err != nil {
		fmt.Println("TUI:        skipped (Node.js or Bun is required to build it); the numbered menu still works")
		return
	}
	fmt.Println("TUI:        building the OpenTUI frontend")
	if err := runner(root, installer...); err != nil {
		fmt.Printf("TUI:        build failed: %v\n", err)
		return
	}
	if err := runner(root, "run", "build"); err != nil {
		fmt.Printf("TUI:        build failed: %v\n", err)
		return
	}
	fmt.Printf("TUI:        ready (%s)\n", filepath.Join(root, "dist", "index.js"))
}

// uiToolchain returns the install command and a runner for the available
// JavaScript package manager.
func uiToolchain() ([]string, func(dir string, args ...string) error, error) {
	if npm, err := exec.LookPath(npmExecutable()); err == nil {
		_ = npm
		return []string{"install"}, runCommand(npmExecutable()), nil
	}
	if bun, err := exec.LookPath("bun"); err == nil {
		_ = bun
		return []string{"install"}, runCommand("bun"), nil
	}
	return nil, nil, errors.New("no JavaScript package manager found")
}

func npmExecutable() string {
	if runtime.GOOS == "windows" {
		return "npm.cmd"
	}
	return "npm"
}

// runCommand runs a package manager in dir, passing the terminal through so the
// user sees progress.
func runCommand(name string) func(dir string, args ...string) error {
	return func(dir string, args ...string) error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" && strings.HasSuffix(name, ".cmd") {
			cmd = exec.Command("cmd", append([]string{"/c", name}, args...)...)
		} else {
			cmd = exec.Command(name, args...)
		}
		cmd.Dir = dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return nil
	}
}

// firstRunSetup is used when ditch-windows starts without a frontend and
// without a repository. It turns a fresh clone into a working install.
func firstRunSetup() error {
	fmt.Println("First run: preparing ditch-windows.")
	fmt.Println()
	if err := runSetup(setupOptions{}); err != nil {
		return err
	}
	return nil
}
