package module

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

// env returns the services attached to ctx by the runner.
func env(ctx context.Context) (*core.Params, error) {
	p := core.ParamsFrom(ctx)
	if p == nil {
		return nil, fmt.Errorf("module invoked without runner context")
	}
	return p, nil
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create directory for %s: %w", filepath.Base(path), err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// copyFileWithConflict copies one file while honoring the restore conflict
// policy. It reports whether a copy happened and whether a destination file
// already existed.
func copyFileWithConflict(src, dst string, opts core.RestoreOpts) (copied, conflict bool, err error) {
	conflict = fileExists(dst)
	if opts.DryRun {
		return false, conflict, nil
	}
	if conflict {
		mode := opts.EffectiveMode()
		if mode == core.AskPerItem {
			mode = core.OverwriteKeepBak
			if opts.Ask != nil {
				choice, askErr := opts.Ask(dst)
				if askErr != nil {
					return false, true, askErr
				}
				if choice == core.Skip || choice == core.OverwriteKeepBak {
					mode = choice
				}
			}
		}
		switch mode {
		case core.Skip:
			return false, true, nil
		case core.OverwriteKeepBak, "":
			if _, err := core.BackupExisting(dst); err != nil {
				return false, true, err
			}
		default:
			return false, true, fmt.Errorf("unsupported conflict mode %q", mode)
		}
	}
	if err := core.CopyFile(src, dst); err != nil {
		return false, conflict, err
	}
	return true, conflict, nil
}

// splitLines normalizes CRLF/LF output and drops empty trailing lines.
func splitLines(data []byte) []string {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func lastAfterColon(line string) string {
	idx := strings.LastIndex(line, ":")
	if idx < 0 || idx == len(line)-1 {
		return ""
	}
	return strings.TrimSpace(line[idx+1:])
}

func moduleLabel(name string) string { return "[" + name + "]" }
