package module

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/platform"
)

type execCall struct {
	Name string
	Args []string
}

type fakeExec struct {
	mu    sync.Mutex
	look  map[string]bool
	run   func(name string, args []string) (core.Result, error)
	calls []execCall
}

func newFakeExec() *fakeExec {
	return &fakeExec{look: make(map[string]bool)}
}

func (f *fakeExec) Run(ctx context.Context, name string, args ...string) (core.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, execCall{Name: name, Args: append([]string(nil), args...)})
	f.mu.Unlock()
	if f.run == nil {
		return core.Result{}, fmt.Errorf("%s: no fake configured", name)
	}
	return f.run(name, args)
}

func (f *fakeExec) LookPath(name string) (string, error) {
	if f.look[name] {
		return name, nil
	}
	return "", fmt.Errorf("%s not found", name)
}

func (f *fakeExec) callsFor(name string) []execCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []execCall
	for _, c := range f.calls {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

type backupCall struct {
	Paths []string
	Opts  core.BackupOptions
}

type fakeRepo struct {
	Info        string
	Snapshots   []core.Snapshot
	BackupCalls []backupCall
	RestoreFn   func(snapshotID, target string) error
	LsNodes     []core.FileNode
}

func (f *fakeRepo) RepoInfo() string                         { return f.Info }
func (f *fakeRepo) Init(ctx context.Context) error           { return nil }
func (f *fakeRepo) Exists(ctx context.Context) (bool, error) { return true, nil }
func (f *fakeRepo) Backup(ctx context.Context, paths []string, opts core.BackupOptions) (core.Snapshot, error) {
	f.BackupCalls = append(f.BackupCalls, backupCall{Paths: append([]string(nil), paths...), Opts: opts})
	return core.Snapshot{ID: fmt.Sprintf("snap-%d", len(f.BackupCalls)), Time: time.Now().UTC(), Tags: opts.Tags}, nil
}
func (f *fakeRepo) Restore(ctx context.Context, snapshotID, target string, opts core.RestoreOptions) error {
	if f.RestoreFn != nil {
		return f.RestoreFn(snapshotID, target)
	}
	return nil
}
func (f *fakeRepo) List(ctx context.Context, tags []string) ([]core.Snapshot, error) {
	return f.Snapshots, nil
}
func (f *fakeRepo) Ls(ctx context.Context, snapshotID string) ([]core.FileNode, error) {
	return f.LsNodes, nil
}
func (f *fakeRepo) Check(ctx context.Context, readData bool) error { return nil }

func testParams(exec core.Exec, repo core.Repository, home string) *core.Params {
	return &core.Params{
		Exec:        exec,
		Repo:        repo,
		RepoInfo:    "local:/tmp/repo",
		HomeDir:     home,
		TempDir:     filepath.Join(home, ".tmp"),
		BackupID:    "20260101T000000Z-abcdef",
		ToolVersion: "test",
		Profile: core.Profile{
			EnabledModules: []string{"files"},
			IncludeGlobs:   []string{"Documents"},
		},
		Logger: core.NopLogger(),
		Report: func(string) {},
	}
}

func ctxWith(p *core.Params) context.Context {
	return core.WithParams(context.Background(), p)
}

func writeTestFile(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if !modTime.IsZero() {
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
}

func requireFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func requireContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s does not contain %q:\n%s", path, want, data)
	}
}

type fakeThemeProvider struct {
	state   platform.ThemeState
	readErr error
	applied *platform.ThemeState
}

func (f *fakeThemeProvider) ReadTheme() (platform.ThemeState, error) {
	return f.state, f.readErr
}
func (f *fakeThemeProvider) ApplyTheme(state platform.ThemeState) error {
	f.applied = &state
	return nil
}

type fakeFontProvider struct {
	system, user []platform.FontRecord
	installTime  int64
	userDir      string
	systemDir    string
	registered   []platform.FontRecord
}

func (f *fakeFontProvider) SystemFonts() ([]platform.FontRecord, error) { return f.system, nil }
func (f *fakeFontProvider) UserFonts() ([]platform.FontRecord, error)   { return f.user, nil }
func (f *fakeFontProvider) InstallTime() (int64, error)                 { return f.installTime, nil }
func (f *fakeFontProvider) UserFontsDir() string                        { return f.userDir }
func (f *fakeFontProvider) SystemFontsDir() string                      { return f.systemDir }
func (f *fakeFontProvider) RegisterFont(rec platform.FontRecord, destPath, scope string) error {
	f.registered = append(f.registered, rec)
	return nil
}
