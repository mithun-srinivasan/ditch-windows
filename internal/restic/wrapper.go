package restic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

// ErrWrongPassword indicates that the repository exists but the supplied
// passphrase cannot unlock it.
var ErrWrongPassword = errors.New("restic: wrong password or no key found")

// ErrRepoNotFound indicates that no repository exists at the configured URL.
var ErrRepoNotFound = errors.New("restic: repository does not exist")

// InstallHint is shown when restic cannot be found.
const InstallHint = "install restic (winget install restic.restic) or set restic_path in config"

// Wrapper drives a restic binary through its subprocess interface.
type Wrapper struct {
	Repo       string
	Password   string
	Binary     string
	CacheDir   string
	OnProgress func(Progress)
}

// New creates a wrapper. An empty binary resolves to "restic"; use FindBinary
// for friendlier discovery and error messages.
func New(repo, password, binary string) *Wrapper {
	if binary == "" {
		binary = "restic"
	}
	return &Wrapper{Repo: repo, Password: password, Binary: binary}
}

// RepoInfo returns a display string for manifests.
func (w *Wrapper) RepoInfo() string { return w.Repo }

// FindBinary locates restic, preferring an explicit path or DITCH_RESTIC_BINARY.
func FindBinary(explicit string) (string, error) {
	candidates := []string{}
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if env := os.Getenv("DITCH_RESTIC_BINARY"); env != "" {
		candidates = append(candidates, env)
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	if p, err := exec.LookPath("restic"); err == nil {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		for _, c := range windowsCandidates() {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("restic was not found in PATH; %s", InstallHint)
}

func windowsCandidates() []string {
	var out []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA", "USERPROFILE", "ProgramData"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		out = append(out,
			filepath.Join(base, "restic", "restic.exe"),
			filepath.Join(base, "Programs", "restic", "restic.exe"),
			filepath.Join(base, "Microsoft", "WinGet", "Links", "restic.exe"),
			filepath.Join(base, "scoop", "shims", "restic.exe"),
			filepath.Join(base, "chocolatey", "bin", "restic.exe"),
		)
	}
	return out
}

func (w *Wrapper) env() []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, e := range os.Environ() {
		key := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			key = e[:i]
		}
		switch strings.ToUpper(key) {
		case "RESTIC_PASSWORD", "RESTIC_REPOSITORY", "RESTIC_CACHE_DIR":
			continue
		}
		env = append(env, e)
	}
	env = append(env, "RESTIC_PASSWORD="+w.Password)
	if w.Repo != "" {
		env = append(env, "RESTIC_REPOSITORY="+w.Repo)
	}
	if w.CacheDir != "" {
		env = append(env, "RESTIC_CACHE_DIR="+w.CacheDir)
	}
	return env
}

func (w *Wrapper) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, w.Binary, args...)
	cmd.Env = w.env()
	configureCommand(cmd)
	return cmd
}

func (w *Wrapper) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := w.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), w.wrapError(args, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func (w *Wrapper) wrapError(args []string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = err.Error()
	}
	msg = core.SanitizeLogText(msg)
	if strings.Contains(strings.ToLower(msg), "wrong password") || strings.Contains(strings.ToLower(msg), "no key found") {
		return fmt.Errorf("%w: %s", ErrWrongPassword, msg)
	}
	if strings.Contains(strings.ToLower(msg), "unable to open config file") && strings.Contains(strings.ToLower(msg), "no such file") {
		return fmt.Errorf("%w: %s", ErrRepoNotFound, msg)
	}
	return fmt.Errorf("restic %s failed: %s", strings.Join(args, " "), msg)
}

// Init creates a new repository.
func (w *Wrapper) Init(ctx context.Context) error {
	_, err := w.run(ctx, "init")
	return err
}

// Exists reports whether the repository can be opened. A wrong passphrase
// returns true together with ErrWrongPassword.
func (w *Wrapper) Exists(ctx context.Context) (bool, error) {
	_, err := w.run(ctx, "cat", "config")
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrWrongPassword) {
		return true, err
	}
	if errors.Is(err, ErrRepoNotFound) {
		return false, nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "does not exist") || strings.Contains(lower, "no such file") || strings.Contains(lower, "failed to open repository") {
		return false, nil
	}
	return false, err
}

// Version returns `restic version` output.
func (w *Wrapper) Version(ctx context.Context) (string, error) {
	out, err := w.run(ctx, "version")
	return strings.TrimSpace(string(out)), err
}

// Progress is emitted by restic's --json backup stream.
type Progress struct {
	MessageType string   `json:"message_type"`
	Percent     float64  `json:"percent_done"`
	BytesDone   int64    `json:"bytes_done"`
	TotalBytes  int64    `json:"total_bytes"`
	FilesDone   int64    `json:"files_done"`
	TotalFiles  int64    `json:"total_files"`
	Current     []string `json:"current_files"`
}

// Backup stores paths in the repository. It implements core.Repository.
func (w *Wrapper) Backup(ctx context.Context, paths []string, opts core.BackupOptions) (core.Snapshot, error) {
	if len(paths) == 0 {
		return core.Snapshot{}, errors.New("restic backup: no paths")
	}
	args := []string{"backup", "--json"}
	if opts.Host != "" {
		args = append(args, "--host", opts.Host)
	}
	for _, tag := range opts.Tags {
		args = append(args, "--tag", tag)
	}
	for _, ex := range opts.Excludes {
		args = append(args, "--exclude", ex)
	}
	if opts.VSS && runtime.GOOS == "windows" {
		args = append(args, "--use-fs-snapshot")
	}
	args = append(args, "--")
	args = append(args, paths...)

	cmd := w.command(ctx, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return core.Snapshot{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return core.Snapshot{}, err
	}
	var snapshotID string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var p Progress
		if json.Unmarshal(line, &p) == nil {
			if p.MessageType == "status" && w.OnProgress != nil {
				w.OnProgress(p)
			}
			if p.MessageType == "summary" {
				var summary struct {
					SnapshotID string `json:"snapshot_id"`
				}
				if json.Unmarshal(line, &summary) == nil && summary.SnapshotID != "" {
					snapshotID = summary.SnapshotID
				}
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil && err == nil {
		err = scanErr
	}
	if waitErr := cmd.Wait(); waitErr != nil {
		return core.Snapshot{}, w.wrapError(args, waitErr, stderr.String())
	}
	if err != nil {
		return core.Snapshot{}, err
	}
	if snapshotID == "" {
		// Older restic versions omit snapshot_id from the summary. Recover the
		// newest matching snapshot by tag.
		snaps, listErr := w.List(ctx, opts.Tags)
		if listErr != nil || len(snaps) == 0 {
			return core.Snapshot{}, fmt.Errorf("restic backup succeeded but snapshot id could not be determined")
		}
		return snaps[0], nil
	}
	return core.Snapshot{ID: snapshotID, Tags: opts.Tags, Time: time.Now().UTC()}, nil
}

// Restore extracts a snapshot into target.
func (w *Wrapper) Restore(ctx context.Context, snapshotID, target string, opts core.RestoreOptions) error {
	if snapshotID == "" {
		return errors.New("restic restore: snapshot id is required")
	}
	args := []string{"restore", snapshotID, "--target", target}
	if opts.Overwrite != "" {
		args = append(args, "--overwrite", opts.Overwrite)
	}
	_, err := w.run(ctx, args...)
	return err
}

// List returns snapshots matching all given tags, newest first.
func (w *Wrapper) List(ctx context.Context, tags []string) ([]core.Snapshot, error) {
	args := []string{"snapshots", "--json"}
	for _, tag := range tags {
		args = append(args, "--tag", tag)
	}
	out, err := w.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	snaps, err := ParseSnapshotsJSON(out)
	if err != nil {
		return nil, err
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.After(snaps[j].Time) })
	return snaps, nil
}

// ParseSnapshotsJSON parses restic snapshots --json output.
func ParseSnapshotsJSON(out []byte) ([]core.Snapshot, error) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 || bytes.Equal(out, []byte("null")) {
		return nil, nil
	}
	var raw []struct {
		ID       string   `json:"id"`
		ShortID  string   `json:"short_id"`
		Time     string   `json:"time"`
		Hostname string   `json:"hostname"`
		Paths    []string `json:"paths"`
		Tags     []string `json:"tags"`
		Tree     string   `json:"tree"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse restic snapshots: %w", err)
	}
	snaps := make([]core.Snapshot, 0, len(raw))
	for _, s := range raw {
		t, _ := time.Parse(time.RFC3339Nano, s.Time)
		snaps = append(snaps, core.Snapshot{
			ID: s.ID, ShortID: s.ShortID, Time: t, Hostname: s.Hostname,
			Paths: s.Paths, Tags: s.Tags, Tree: s.Tree,
		})
	}
	return snaps, nil
}

// Ls lists nodes in a snapshot.
func (w *Wrapper) Ls(ctx context.Context, snapshotID string) ([]core.FileNode, error) {
	out, err := w.run(ctx, "ls", "--json", snapshotID)
	if err != nil {
		return nil, err
	}
	return ParseLsJSON(out)
}

// ParseLsJSON parses restic ls --json output, ignoring the snapshot header.
func ParseLsJSON(out []byte) ([]core.FileNode, error) {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var nodes []core.FileNode
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw struct {
			StructType string          `json:"struct_type"`
			Path       string          `json:"path"`
			Type       string          `json:"type"`
			Size       int64           `json:"size"`
			Mode       json.RawMessage `json:"mode"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if raw.StructType != "node" || raw.Path == "" {
			continue
		}
		nodes = append(nodes, core.FileNode{Path: raw.Path, Type: raw.Type, Size: raw.Size, Mode: parseMode(raw.Mode)})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nodes, nil
}

// parseMode normalizes restic's mode field. Depending on restic version it is
// either a string ("0644") or an integer (384). Integers are rendered as octal.
func parseMode(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		return fmt.Sprintf("0%o", number)
	}
	return string(raw)
}

// Check verifies repository integrity. readData also checks data blobs.
func (w *Wrapper) Check(ctx context.Context, readData bool) error {
	args := []string{"check"}
	if readData {
		args = append(args, "--read-data")
	}
	_, err := w.run(ctx, args...)
	return err
}

// Forget removes a snapshot by ID (used by maintenance commands).
func (w *Wrapper) Forget(ctx context.Context, snapshotID string) error {
	_, err := w.run(ctx, "forget", snapshotID)
	return err
}

// Stats returns raw repository stats JSON.
func (w *Wrapper) Stats(ctx context.Context) ([]byte, error) {
	return w.run(ctx, "stats", "--json")
}
