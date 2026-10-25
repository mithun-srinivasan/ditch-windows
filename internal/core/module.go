package core

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Module is the contract implemented by every backup/restore module.
//
// Scan must be cheap and side-effect free: it reports what Backup would
// capture. Backup writes module metadata into dir; secrets that may not sit in
// plaintext staging (Wi-Fi XML) are backed up directly through Env.Repo and
// only non-secret metadata is written to dir. Restore applies the contents of
// dir, which is a verified restore of the module's metadata snapshot.
type Module interface {
	Name() string
	Description() string
	NeedsAdmin() bool
	Scan(ctx context.Context) (Summary, error)
	Backup(ctx context.Context, dir string) error
	Restore(ctx context.Context, dir string, opts RestoreOpts) error
	Verify(ctx context.Context, dir string) error
}

// Summary describes what a module would back up.
type Summary struct {
	ItemCount int
	Details   string
}

// RestoreOpts controls restore behavior.
//
// The zero value is safe: DryRun=false, OverwriteKeepBak, no per-item prompts.
type RestoreOpts struct {
	DryRun       bool
	ConflictMode ConflictMode
	AskPerItem   bool
	// SelectedModules limits restore to these modules. Empty restores every
	// module captured by the backup.
	SelectedModules []string
	// Ask is called for every conflict when AskPerItem is true. It must return
	// OverwriteKeepBak, Skip, or AskPerItem (treated as overwrite). A nil Ask
	// falls back to OverwriteKeepBak.
	Ask func(path string) (ConflictMode, error)
}

// EffectiveMode returns the conflict strategy to apply.
func (o RestoreOpts) EffectiveMode() ConflictMode {
	if o.AskPerItem && o.Ask != nil {
		return AskPerItem
	}
	if o.ConflictMode == "" {
		return OverwriteKeepBak
	}
	return o.ConflictMode
}

// ConflictMode is a restore conflict strategy.
type ConflictMode string

const (
	// OverwriteKeepBak restores the backed-up file and renames an existing
	// destination file to <name>.bak (or .bak.1, .bak.2, ...).
	OverwriteKeepBak ConflictMode = "overwrite_keep_bak"
	// Skip leaves existing destination files untouched.
	Skip ConflictMode = "skip"
	// AskPerItem prompts for each conflict (or overwrites if no prompt exists).
	AskPerItem ConflictMode = "ask_per_item"
)

// ValidConflictMode reports whether m is a known mode.
func ValidConflictMode(m ConflictMode) bool {
	switch m {
	case OverwriteKeepBak, Skip, AskPerItem:
		return true
	}
	return false
}

// Params carries all services available to modules through context.
type Params struct {
	Exec        Exec
	Repo        Repository
	RepoInfo    string
	Config      *Config
	Profile     Profile
	ProfileName string
	HomeDir     string
	TempDir     string
	BackupID    string
	ToolVersion string
	Commit      string
	Logger      Logger
	// Manifest is set during restore and verify for the backup being applied.
	Manifest *Manifest
	// Report surfaces human-readable progress detail to the runner/UI.
	Report func(message string)
}

// Reportf sends a formatted progress detail when a reporter is configured.
func (p *Params) Reportf(format string, args ...any) {
	if p != nil && p.Report != nil {
		p.Report(fmt.Sprintf(format, args...))
	}
}

// Logf logs at info level when a logger exists.
func (p *Params) Logf(format string, args ...any) {
	if p != nil && p.Logger != nil {
		p.Logger.Infof(format, args...)
	}
}

type paramsKey struct{}

// WithParams attaches Params to a context.
func WithParams(ctx context.Context, p *Params) context.Context {
	return context.WithValue(ctx, paramsKey{}, p)
}

// ParamsFrom returns the Params attached to ctx, or nil.
func ParamsFrom(ctx context.Context) *Params {
	p, _ := ctx.Value(paramsKey{}).(*Params)
	return p
}

// ErrElevationNeeded is returned when an operation requires administrator
// privileges.
var ErrElevationNeeded = errors.New("administrator elevation is required")

// ErrRelaunched is returned by EnsureElevation after a UAC child process has
// been started. The parent process should exit successfully.
var ErrRelaunched = errors.New("relaunched with administrator privileges")

// ModuleError wraps an error with the module name.
type ModuleError struct {
	Module string
	Err    error
}

func (e *ModuleError) Error() string {
	if e.Module == "" {
		return e.Err.Error()
	}
	return e.Module + ": " + e.Err.Error()
}
func (e *ModuleError) Unwrap() error { return e.Err }

// BackupOptions configures a repository snapshot.
type BackupOptions struct {
	Tags     []string
	Excludes []string
	VSS      bool
	Host     string
}

// RestoreOptions configures snapshot extraction.
type RestoreOptions struct {
	Overwrite string
}

// Snapshot is a repository snapshot.
type Snapshot struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Hostname string    `json:"hostname"`
	Paths    []string  `json:"paths"`
	Tags     []string  `json:"tags"`
	Tree     string    `json:"tree"`
}

// TagValue returns the value of a KEY=VALUE snapshot tag.
func (s Snapshot) TagValue(key string) string {
	for _, t := range s.Tags {
		if len(t) > len(key) && t[:len(key)+1] == key+"=" {
			return t[len(key)+1:]
		}
	}
	return ""
}

// FileNode describes one entry in a snapshot.
type FileNode struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	Mode string `json:"mode"`
}

// Repository is the storage engine used by the runner and modules. The restic
// wrapper implements it; tests use an in-memory fake.
type Repository interface {
	RepoInfo() string
	Init(ctx context.Context) error
	Exists(ctx context.Context) (bool, error)
	Backup(ctx context.Context, paths []string, opts BackupOptions) (Snapshot, error)
	Restore(ctx context.Context, snapshotID, target string, opts RestoreOptions) error
	List(ctx context.Context, tags []string) ([]Snapshot, error)
	Ls(ctx context.Context, snapshotID string) ([]FileNode, error)
	Check(ctx context.Context, readData bool) error
}
