package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/demo"
	"github.com/mithun-srinivasan/ditch-windows/internal/module"
	"github.com/mithun-srinivasan/ditch-windows/internal/restic"
	"github.com/mithun-srinivasan/ditch-windows/internal/storage"
)

// The bridge is the stable, machine-readable seam between the Go engine and the
// OpenTUI frontend. One JSON object per line travels in each direction:
//
//	{"id":1,"cmd":"backup","args":{"modules":["winget"]}}
//	{"id":1,"ok":true,"data":{...}}
//
// Unsolicited progress is emitted with an "event" key while a command runs.

type bridgeRequest struct {
	ID   json.RawMessage `json:"id"`
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args"`
}

type bridgeReply struct {
	ID    json.RawMessage `json:"id,omitempty"`
	OK    bool            `json:"ok"`
	Data  any             `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

type bridgeEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

type bridgeWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (b *bridgeWriter) write(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	_, _ = b.w.Write(append(data, '\n'))
}

func (b *bridgeWriter) reply(req bridgeRequest, data any, err error) {
	r := bridgeReply{ID: req.ID, OK: err == nil, Data: data}
	if err != nil {
		r.Error = err.Error()
		r.Data = nil
	}
	b.write(r)
}

func (b *bridgeWriter) event(name string, data any) {
	b.write(bridgeEvent{Event: name, Data: data})
}

type bridgeSession struct {
	cfg        *core.Config
	params     *core.Params
	runner     *core.Runner
	logger     core.Logger
	demo       bool
	configured bool
	base       string
	home       string
	out        *bridgeWriter
	modules    []core.Module
}

type bridgeProfileInfo struct {
	Modules  []string `json:"modules"`
	Includes []string `json:"includes"`
	Excludes []string `json:"excludes"`
}

type bridgeModuleInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	NeedsAdmin  bool   `json:"needsAdmin"`
}

type bridgeStatus struct {
	Configured     bool                         `json:"configured"`
	Demo           bool                         `json:"demo"`
	Repo           string                       `json:"repo"`
	RepoExists     bool                         `json:"repoExists"`
	RepoError      string                       `json:"repoError,omitempty"`
	Encrypted      bool                         `json:"encrypted"`
	Elevated       bool                         `json:"elevated"`
	Platform       string                       `json:"platform"`
	Hostname       string                       `json:"hostname"`
	Home           string                       `json:"home"`
	Base           string                       `json:"base,omitempty"`
	ConfigPath     string                       `json:"configPath"`
	LogsDir        string                       `json:"logsDir"`
	BackupsDir     string                       `json:"backupsDir"`
	DefaultProfile string                       `json:"defaultProfile"`
	ConflictMode   string                       `json:"conflictMode"`
	Profiles       map[string]bridgeProfileInfo `json:"profiles"`
	Modules        []bridgeModuleInfo           `json:"modules"`
	Version        string                       `json:"version"`
}

type bridgeBackupArgs struct {
	Modules []string `json:"modules"`
	Profile string   `json:"profile"`
}

type bridgeModuleResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Items      int    `json:"items"`
	Details    string `json:"details,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

type bridgeBackupResult struct {
	BackupID   string               `json:"backupId"`
	Snapshots  int                  `json:"snapshots"`
	Partial    bool                 `json:"partial"`
	DurationMS int64                `json:"durationMs"`
	Modules    []bridgeModuleResult `json:"modules"`
	Error      string               `json:"error,omitempty"`
}

type bridgePreviewArgs struct {
	BackupID string   `json:"backupId"`
	Modules  []string `json:"modules"`
}

type bridgePreviewModule struct {
	Name       string `json:"name"`
	Available  bool   `json:"available"`
	NeedsAdmin bool   `json:"needsAdmin"`
	Elevated   bool   `json:"elevated"`
	Items      int    `json:"items"`
	Details    string `json:"details,omitempty"`
	Files      int64  `json:"files"`
	Bytes      int64  `json:"bytes"`
	Conflicts  int64  `json:"conflicts"`
	Error      string `json:"error,omitempty"`
}

type bridgePreviewResult struct {
	BackupID  string                `json:"backupId"`
	Time      time.Time             `json:"time"`
	Hostname  string                `json:"hostname"`
	Modules   []bridgePreviewModule `json:"modules"`
	Files     int64                 `json:"files"`
	Bytes     int64                 `json:"bytes"`
	Conflicts int64                 `json:"conflicts"`
}

type bridgeApplyArgs struct {
	BackupID     string   `json:"backupId"`
	Modules      []string `json:"modules"`
	ConflictMode string   `json:"conflictMode"`
}

type bridgeRestoreModule struct {
	Name       string `json:"name"`
	Success    bool   `json:"success"`
	Skipped    bool   `json:"skipped"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"durationMs"`
}

type bridgeApplyResult struct {
	BackupID   string                `json:"backupId"`
	DryRun     bool                  `json:"dryRun"`
	DurationMS int64                 `json:"durationMs"`
	Modules    []bridgeRestoreModule `json:"modules"`
	Baks       []string              `json:"baks"`
}

type bridgeVerifyArgs struct {
	BackupID string `json:"backupId"`
	ReadData bool   `json:"readData"`
}

type bridgeVerifyModule struct {
	Name     string `json:"name"`
	Verified bool   `json:"verified"`
	Error    string `json:"error,omitempty"`
}

type bridgeVerifyResult struct {
	BackupID        string               `json:"backupId"`
	RepositoryOK    bool                 `json:"repositoryOk"`
	RepositoryError string               `json:"repositoryError,omitempty"`
	HashOK          bool                 `json:"hashOk"`
	HashError       string               `json:"hashError,omitempty"`
	Modules         []bridgeVerifyModule `json:"modules"`
}

type bridgeFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Bak  bool   `json:"bak"`
}

type bridgeInitArgs struct {
	Repo string `json:"repo"`
}

type bridgeInitResult struct {
	Saved         bool   `json:"saved"`
	Repo          string `json:"repo"`
	NeedsRelaunch bool   `json:"needsRelaunch"`
	Message       string `json:"message"`
}

func bridgeCommand(args []string) error {
	session, err := newBridgeSession(args)
	out := &bridgeWriter{w: os.Stdout}
	if err != nil {
		out.event("fatal", map[string]string{"message": err.Error()})
		return readBridgeLoop(nil, out)
	}
	session.out = out
	if session.runner != nil {
		session.runner.SetEventHandler(session.emitProgress)
	}
	out.event("ready", session.status())
	return readBridgeLoop(session, out)
}

func newBridgeSession(args []string) (*bridgeSession, error) {
	demoMode := false
	base := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--demo":
			demoMode = true
		case "--base":
			if i+1 >= len(args) {
				return nil, errors.New("--base requires a directory")
			}
			i++
			base = args[i]
		}
	}
	if demoMode {
		s, err := setupDemo(base)
		if err != nil {
			return nil, err
		}
		return &bridgeSession{
			cfg: s.cfg, params: s.params, runner: s.runner, logger: s.logger,
			demo: true, configured: true, base: s.base, home: s.home, modules: demo.Modules(),
		}, nil
	}

	cfg, err := core.LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.RepoURL == "" {
		home, _ := os.UserHomeDir()
		return &bridgeSession{cfg: cfg, home: home, modules: module.All()}, nil
	}

	_, params, runner, logger, err := runtimeForMode("", false)
	if err != nil {
		return nil, err
	}
	return &bridgeSession{cfg: cfg, params: params, runner: runner, logger: logger, configured: true, home: params.HomeDir, modules: runner.Modules()}, nil
}

func readBridgeLoop(s *bridgeSession, out *bridgeWriter) error {
	if s != nil {
		s.out = out
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req bridgeRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			out.write(bridgeReply{OK: false, Error: "invalid request: " + err.Error()})
			continue
		}
		if req.Cmd == "quit" {
			out.reply(req, map[string]bool{"bye": true}, nil)
			return nil
		}
		if s == nil {
			out.reply(req, nil, errors.New("bridge is not ready"))
			continue
		}
		data, err := s.handle(req)
		out.reply(req, data, err)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (s *bridgeSession) handle(req bridgeRequest) (any, error) {
	if s.runner == nil {
		switch req.Cmd {
		case "list", "backup", "preview", "apply", "verify":
			return nil, errors.New("repository is not configured; finish setup and relaunch")
		}
	}
	switch req.Cmd {
	case "status":
		return s.status(), nil
	case "list":
		return s.list()
	case "backup":
		var args bridgeBackupArgs
		if err := decodeArgs(req.Args, &args); err != nil {
			return nil, err
		}
		return s.backup(args)
	case "preview":
		var args bridgePreviewArgs
		if err := decodeArgs(req.Args, &args); err != nil {
			return nil, err
		}
		return s.preview(args)
	case "apply":
		var args bridgeApplyArgs
		if err := decodeArgs(req.Args, &args); err != nil {
			return nil, err
		}
		return s.apply(args)
	case "verify":
		var args bridgeVerifyArgs
		if err := decodeArgs(req.Args, &args); err != nil {
			return nil, err
		}
		return s.verify(args)
	case "files":
		return s.files()
	case "init":
		var args bridgeInitArgs
		if err := decodeArgs(req.Args, &args); err != nil {
			return nil, err
		}
		return s.init(args)
	default:
		return nil, fmt.Errorf("unknown command %q", req.Cmd)
	}
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func (s *bridgeSession) status() *bridgeStatus {
	mods := make([]bridgeModuleInfo, 0, len(s.modules))
	for _, m := range s.modules {
		mods = append(mods, bridgeModuleInfo{Name: m.Name(), Description: m.Description(), NeedsAdmin: m.NeedsAdmin()})
	}
	profiles := make(map[string]bridgeProfileInfo, len(s.cfg.Profiles))
	for name, p := range s.cfg.Profiles {
		profiles[name] = bridgeProfileInfo{Modules: p.EnabledModules, Includes: p.IncludeGlobs, Excludes: p.ExcludeGlobs}
	}
	host, _ := os.Hostname()
	repoInfo := ""
	if s.params != nil {
		repoInfo = s.params.RepoInfo
	}
	st := &bridgeStatus{
		Configured:     s.configured,
		Demo:           s.demo,
		Repo:           repoInfo,
		Encrypted:      true,
		Elevated:       core.IsElevated(),
		Platform:       runtime.GOOS,
		Hostname:       host,
		Home:           s.home,
		Base:           s.base,
		ConfigPath:     core.ConfigPath(),
		LogsDir:        core.LogsDir(),
		BackupsDir:     core.BackupsDir(),
		ConflictMode:   string(s.cfg.ActiveConflictMode()),
		Profiles:       profiles,
		Modules:        mods,
		Version:        version,
		DefaultProfile: s.cfg.DefaultProfile,
	}
	if s.demo {
		st.RepoExists = true
	} else if s.params != nil && s.params.Repo != nil {
		exists, err := s.params.Repo.Exists(context.Background())
		if err != nil {
			st.RepoError = err.Error()
		} else {
			st.RepoExists = exists
		}
	}
	return st
}

func (s *bridgeSession) list() (any, error) {
	backups, err := s.runner.ListBackups(context.Background())
	if err != nil {
		return nil, err
	}
	type row struct {
		ID       string    `json:"id"`
		Time     time.Time `json:"time"`
		Hostname string    `json:"hostname"`
	}
	rows := make([]row, 0, len(backups))
	for _, b := range backups {
		rows = append(rows, row{ID: b.BackupID, Time: b.Time, Hostname: b.Hostname})
	}
	return map[string]any{"backups": rows}, nil
}

func (s *bridgeSession) selectProfile(modules []string, profileName string) (core.Profile, []string, error) {
	base, err := s.cfg.Profile(profileName)
	if err != nil {
		return core.Profile{}, nil, err
	}
	if len(modules) == 0 {
		return base, base.EnabledModules, nil
	}
	seen := make(map[string]bool, len(modules))
	out := make([]string, 0, len(modules))
	for _, name := range modules {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	p := base
	p.EnabledModules = out
	return p, out, nil
}

func (s *bridgeSession) backup(args bridgeBackupArgs) (any, error) {
	profileName := args.Profile
	if profileName == "" {
		profileName = s.cfg.DefaultProfile
	}
	profile, modules, err := s.selectProfile(args.Modules, profileName)
	if err != nil {
		return nil, err
	}
	// Fresh backup ID per run; the runner otherwise keeps the first one.
	s.params.BackupID = ""
	if args.Profile == "" && len(args.Modules) > 0 {
		profileName = "custom"
	}
	s.runner.SetProfile(profileName, profile)
	manifest, runErr := s.runner.Backup(context.Background(), modules)
	if manifest == nil {
		return nil, runErr
	}
	result := bridgeBackupResult{
		BackupID:   manifest.BackupID,
		Snapshots:  len(manifest.SnapshotIDs),
		Partial:    manifest.Partial,
		DurationMS: manifest.DurationMS,
		Modules:    make([]bridgeModuleResult, 0, len(manifest.Modules)),
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	for _, st := range manifest.SortModuleStatuses() {
		status := "ok"
		if st.Skipped {
			status = "skipped"
		}
		if !st.Success && !st.Skipped {
			status = "failed"
		}
		result.Modules = append(result.Modules, bridgeModuleResult{
			Name: st.Name, Status: status, Items: st.ItemCount,
			Details: st.Details, Error: st.Error, DurationMS: st.DurationMS,
		})
	}
	return result, nil
}

func (s *bridgeSession) conflictMode(requested string) core.ConflictMode {
	mode := core.ConflictMode(requested)
	if mode == "" || mode == core.AskPerItem {
		mode = s.cfg.ActiveConflictMode()
	}
	if !core.ValidConflictMode(mode) || mode == core.AskPerItem {
		mode = core.OverwriteKeepBak
	}
	return mode
}

func (s *bridgeSession) preview(args bridgePreviewArgs) (any, error) {
	if s.demo {
		if err := demo.SimulateLocalEdit(s.home); err != nil {
			return nil, err
		}
	}
	report, err := s.runner.Restore(context.Background(), args.BackupID, core.RestoreOpts{
		DryRun:          true,
		ConflictMode:    core.OverwriteKeepBak,
		SelectedModules: args.Modules,
	})
	if report == nil || report.Preview == nil {
		return nil, err
	}
	p := report.Preview
	result := bridgePreviewResult{BackupID: p.BackupID, Time: p.Time, Hostname: p.Hostname, Files: p.FileCount, Bytes: p.FileBytes, Conflicts: p.Conflicts, Modules: make([]bridgePreviewModule, 0, len(p.Modules))}
	for _, m := range p.Modules {
		result.Modules = append(result.Modules, bridgePreviewModule{
			Name: m.Name, Available: m.Available, NeedsAdmin: m.NeedsAdmin, Elevated: m.Elevated,
			Items: m.ItemCount, Details: m.Details, Files: m.Files, Bytes: m.Bytes, Conflicts: m.Conflicts, Error: m.Error,
		})
	}
	return result, nil
}

func (s *bridgeSession) apply(args bridgeApplyArgs) (any, error) {
	mode := s.conflictMode(args.ConflictMode)
	if s.demo {
		if err := demo.SimulateLocalEdit(s.home); err != nil {
			return nil, err
		}
	}
	report, err := s.runner.Restore(context.Background(), args.BackupID, core.RestoreOpts{
		ConflictMode:    mode,
		SelectedModules: args.Modules,
	})
	if report == nil {
		return nil, err
	}
	result := bridgeApplyResult{BackupID: report.BackupID, DryRun: report.DryRun, DurationMS: report.DurationMS, Modules: make([]bridgeRestoreModule, 0, len(report.Modules)), Baks: []string{}}
	for _, m := range report.Modules {
		result.Modules = append(result.Modules, bridgeRestoreModule{Name: m.Name, Success: m.Success, Skipped: m.Skipped, Error: m.Error, DurationMS: m.DurationMS})
	}
	if s.demo {
		baks, _ := findBakFiles(s.home)
		for _, bak := range baks {
			if rel, relErr := filepath.Rel(s.home, bak); relErr == nil {
				result.Baks = append(result.Baks, filepath.ToSlash(rel))
			}
		}
		sort.Strings(result.Baks)
	}
	return result, nil
}

func (s *bridgeSession) verify(args bridgeVerifyArgs) (any, error) {
	if args.ReadData {
		if err := s.runner.CheckRepository(context.Background(), true); err != nil {
			return nil, err
		}
	}
	report, runErr := s.runner.Verify(context.Background(), args.BackupID)
	if report == nil {
		return nil, runErr
	}
	result := bridgeVerifyResult{
		BackupID: report.BackupID, RepositoryOK: report.RepositoryOK,
		RepositoryError: report.RepositoryError, HashOK: report.ManifestHashOK, HashError: report.ManifestHashErr,
		Modules: make([]bridgeVerifyModule, 0, len(report.Modules)),
	}
	for _, m := range report.Modules {
		result.Modules = append(result.Modules, bridgeVerifyModule{Name: m.Name, Verified: m.Verified, Error: m.Error})
	}
	return result, nil
}

func (s *bridgeSession) files() (any, error) {
	if !s.demo {
		return map[string]any{"files": []bridgeFile{}}, nil
	}
	var files = make([]bridgeFile, 0)
	err := filepath.WalkDir(s.home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
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
		files = append(files, bridgeFile{Path: filepath.ToSlash(rel), Size: info.Size(), Bak: strings.Contains(filepath.Base(path), ".bak")})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return map[string]any{"files": files}, err
}

func (s *bridgeSession) init(args bridgeInitArgs) (any, error) {
	repo := strings.TrimSpace(args.Repo)
	if repo == "" {
		return nil, errors.New("repository path is required")
	}
	_, url, err := storage.BackendFor(repo)
	if err != nil {
		return nil, err
	}
	cfg := core.DefaultConfig()
	cfg.RepoURL = url
	if err := core.SaveConfig(cfg); err != nil {
		return nil, err
	}
	return bridgeInitResult{
		Saved: true, Repo: url, NeedsRelaunch: os.Getenv("RESTIC_PASSWORD") == "",
		Message: "Configuration saved. Restart ditch-windows to unlock the repository.",
	}, nil
}

func (s *bridgeSession) emitProgress(e core.Event) {
	data := map[string]any{
		"type":    string(e.Type),
		"module":  e.Module,
		"message": e.Message,
	}
	if e.Err != nil {
		data["error"] = e.Err.Error()
	}
	if e.Summary != nil {
		data["items"] = e.Summary.ItemCount
		data["details"] = e.Summary.Details
	}
	if e.Status != nil {
		data["status"] = e.Status
	}
	s.out.event("progress", data)
}

// runtimeForMode is runtimeFor with an explicit prompt policy. The TUI bridge
// must never read from stdin because stdin carries the renderer protocol.
func runtimeForMode(override string, allowPrompt bool) (*core.Config, *core.Params, *core.Runner, core.Logger, error) {
	cfg, err := core.LoadConfig()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	repoSpec := cfg.RepoURL
	if override != "" {
		repoSpec = override
	}
	if repoSpec == "" {
		return nil, nil, nil, nil, fmt.Errorf("repository is not configured; run config init --repo <path>")
	}
	_, repoURL, err := storage.BackendFor(repoSpec)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	binary, err := restic.FindBinary(cfg.ResticPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	password := os.Getenv("RESTIC_PASSWORD")
	if password == "" {
		if !allowPrompt {
			return nil, nil, nil, nil, errors.New("RESTIC_PASSWORD is not set; the TUI must unlock the repository before starting")
		}
		password, err = getPassphrase()
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if os.Getenv("RESTIC_PASSWORD") == "" {
			repo := restic.New(repoURL, password, binary)
			exists, existsErr := repo.Exists(context.Background())
			if existsErr != nil {
				return nil, nil, nil, nil, existsErr
			}
			if !exists {
				confirmPassword, readErr := readSecret("Confirm new repository passphrase: ")
				if readErr != nil {
					return nil, nil, nil, nil, readErr
				}
				if confirmPassword != password {
					return nil, nil, nil, nil, errors.New("passphrases do not match")
				}
			}
		}
	}
	repo := restic.New(repoURL, password, binary)
	home, _ := os.UserHomeDir()
	logger, err := core.NewDailyFileLogger(core.LogsDir())
	if err != nil {
		return nil, nil, nil, nil, err
	}
	params := &core.Params{
		Repo: repo, RepoInfo: storage.SanitizeRepoURL(repoURL), Config: cfg,
		HomeDir: home, Exec: core.NewDefaultExec(), Logger: logger, ToolVersion: version, Commit: commit,
	}
	return cfg, params, core.NewRunner(module.All(), params), logger, nil
}
