package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/restic"
	"github.com/mithun-srinivasan/ditch-windows/internal/storage"
)

// errNoTUI means no OpenTUI frontend could be located. Callers fall back to the
// plain numbered menu.
var errNoTUI = errors.New("no TUI frontend found")

type tuiCommand struct {
	path   string
	args   []string
	source string
}

// runTUI unlocks the repository (if needed) and hands the terminal to the
// OpenTUI frontend. The frontend spawns `ditch-windows bridge` using the
// environment below, so the bridge inherits the unlocked RESTIC_PASSWORD.
func runTUI(mode string, bridgeArgs []string, preparePassphrase bool) error {
	if preparePassphrase {
		if err := ensureTUIPassphrase(); err != nil {
			return err
		}
	}
	return launchTUI(mode, bridgeArgs)
}

func launchTUI(mode string, bridgeArgs []string) error {
	cmdSpec, err := resolveTUICommand()
	if err != nil {
		return err
	}
	bridge, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	argsJSON, err := json.Marshal(bridgeArgs)
	if err != nil {
		return err
	}
	cmd := exec.Command(cmdSpec.path, cmdSpec.args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"DITCH_BRIDGE="+bridge,
		"DITCH_BRIDGE_ARGS_JSON="+string(argsJSON),
		"DITCH_UI_MODE="+mode,
	)
	return cmd.Run()
}

func resolveTUICommand() (tuiCommand, error) {
	if bin := os.Getenv("DITCH_TUI_BIN"); bin != "" {
		if isJavaScript(bin) {
			return tuiCommand{path: "node", args: []string{"--experimental-ffi", bin}, source: "DITCH_TUI_BIN"}, nil
		}
		return tuiCommand{path: bin, source: "DITCH_TUI_BIN"}, nil
	}

	if exe, err := os.Executable(); err == nil {
		sibling := "ditch-windows-tui"
		if runtime.GOOS == "windows" {
			sibling += ".exe"
		}
		candidate := filepath.Join(filepath.Dir(exe), sibling)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return tuiCommand{path: candidate, source: "sibling binary"}, nil
		}
	}

	root := findUIRoot()
	if root != "" {
		standalone := "ditch-windows-tui"
		if runtime.GOOS == "windows" {
			standalone += ".exe"
		}
		exe := filepath.Join(root, "dist", standalone)
		if info, err := os.Stat(exe); err == nil && !info.IsDir() {
			return tuiCommand{path: exe, source: "ui/dist standalone"}, nil
		}
		dist := filepath.Join(root, "dist", "index.js")
		if info, err := os.Stat(dist); err == nil && !info.IsDir() && uiDepsInstalled(root) {
			return tuiCommand{path: "node", args: []string{"--experimental-ffi", dist}, source: "ui/dist"}, nil
		}
		src := filepath.Join(root, "src", "index.tsx")
		if _, err := os.Stat(src); err == nil && uiDepsInstalled(root) {
			if bun, lookErr := exec.LookPath("bun"); lookErr == nil {
				return tuiCommand{path: bun, args: []string{src}, source: "ui/src (bun)"}, nil
			}
		}
	}
	return tuiCommand{}, errNoTUI
}

// uiDepsInstalled reports whether the frontend's node_modules exists. The
// bundle is built with external packages, so it cannot run without them.
func uiDepsInstalled(root string) bool {
	info, err := os.Stat(filepath.Join(root, "node_modules"))
	return err == nil && info.IsDir()
}

func isInteractiveTerminal() bool {
	for _, f := range []*os.File{os.Stdin, os.Stdout} {
		info, err := f.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice == 0 {
			return false
		}
	}
	return true
}

func isJavaScript(path string) bool {
	switch filepath.Ext(path) {
	case ".js", ".mjs", ".cjs":
		return true
	}
	return false
}

func findUIRoot() string {
	var starts []string
	if dir := os.Getenv("DITCH_UI_DIR"); dir != "" {
		return dir
	}
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir, err := filepath.Abs(start)
		if err != nil {
			continue
		}
		for {
			candidate := filepath.Join(dir, "ui")
			if info, statErr := os.Stat(filepath.Join(candidate, "package.json")); statErr == nil && !info.IsDir() {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

// ensureTUIPassphrase acquires the restic passphrase before the full-screen UI
// takes over the terminal. The TUI cannot prompt for it safely.
func ensureTUIPassphrase() error {
	if os.Getenv("RESTIC_PASSWORD") != "" {
		return nil
	}
	cfg, err := core.LoadConfig()
	if err != nil {
		return err
	}
	if cfg.RepoURL == "" {
		// First run: the UI shows onboarding. No repository to unlock yet.
		return nil
	}
	_, repoURL, err := storage.BackendFor(cfg.RepoURL)
	if err != nil {
		return err
	}
	binary, err := restic.FindBinary(cfg.ResticPath)
	if err != nil {
		return err
	}
	password, err := getPassphrase()
	if err != nil {
		return err
	}
	repo := restic.New(repoURL, password, binary)
	exists, existsErr := repo.Exists(context.Background())
	if existsErr != nil {
		return existsErr
	}
	if !exists {
		confirmPassword, readErr := readSecret("Confirm new repository passphrase: ")
		if readErr != nil {
			return readErr
		}
		if confirmPassword != password {
			return errors.New("passphrases do not match")
		}
	}
	return os.Setenv("RESTIC_PASSWORD", password)
}
