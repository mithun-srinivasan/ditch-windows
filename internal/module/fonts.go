package module

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/platform"
)

// Fonts captures user-installed fonts plus system fonts that did not ship with
// Windows. Restores copy files back to the user or system font directory and
// register them through the platform layer.
type Fonts struct {
	provider platform.FontProvider
}

// NewFonts returns the production font module.
func NewFonts() *Fonts { return &Fonts{provider: platform.SystemFonts{}} }

// FontsManifest is written to fonts.json.
type FontsManifest struct {
	CapturedAt time.Time      `json:"captured_at"`
	Fonts      []FontManifest `json:"fonts"`
}

// FontManifest describes one captured font file.
type FontManifest struct {
	DisplayName string `json:"display_name"`
	FileName    string `json:"file_name"`
	SourcePath  string `json:"source_path,omitempty"`
	BackupFile  string `json:"backup_file"`
	Scope       string `json:"scope"`
}

// Name implements core.Module.
func (f *Fonts) Name() string { return "fonts" }

// Description implements core.Module.
func (f *Fonts) Description() string {
	return "User and non-default system fonts, restored with registry registration"
}

// NeedsAdmin implements core.Module.
func (f *Fonts) NeedsAdmin() bool { return false }

// Scan implements core.Module.
func (f *Fonts) Scan(ctx context.Context) (core.Summary, error) {
	p, err := env(ctx)
	if err != nil {
		return core.Summary{}, err
	}
	if f.provider == nil {
		f.provider = platform.SystemFonts{}
	}
	user, err := f.provider.UserFonts()
	if err != nil {
		if errors.Is(err, platform.ErrUnsupported) {
			return core.Summary{Details: "font capture requires Windows"}, nil
		}
		return core.Summary{}, fmt.Errorf("enumerate user fonts: %w", err)
	}
	system, err := f.provider.SystemFonts()
	if err != nil {
		return core.Summary{}, fmt.Errorf("enumerate system fonts: %w", err)
	}
	installTime, _ := f.provider.InstallTime()
	selected := selectFontRecords(system, user, installTime)
	systemCount := 0
	for _, rec := range selected {
		if rec.Scope == "system" {
			systemCount++
		}
	}
	if len(selected) == 0 {
		return core.Summary{Details: "no user or custom system fonts found"}, nil
	}
	p.Reportf("found %d user and %d custom system font(s)", len(selected)-systemCount, systemCount)
	return core.Summary{
		ItemCount: len(selected),
		Details:   fmt.Sprintf("%d user font(s), %d non-default system font(s)", len(selected)-systemCount, systemCount),
	}, nil
}

// Backup implements core.Module.
func (f *Fonts) Backup(ctx context.Context, dir string) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	if f.provider == nil {
		f.provider = platform.SystemFonts{}
	}
	user, err := f.provider.UserFonts()
	if err != nil {
		return fmt.Errorf("enumerate user fonts: %w", err)
	}
	system, err := f.provider.SystemFonts()
	if err != nil {
		return fmt.Errorf("enumerate system fonts: %w", err)
	}
	installTime, _ := f.provider.InstallTime()
	selected := selectFontRecords(system, user, installTime)
	manifest := FontsManifest{CapturedAt: time.Now().UTC()}
	filesDir := filepath.Join(dir, "files")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return err
	}
	usedNames := make(map[string]bool)
	for i, rec := range selected {
		source := fontSourcePath(rec, f.provider)
		if source == "" || !fileExists(source) {
			p.Logf("font %q is registered but its file is missing; skipping", rec.DisplayName)
			continue
		}
		base := filepath.Base(source)
		if base == "." || base == string(filepath.Separator) {
			base = filepath.Base(rec.FileName)
		}
		backupName := uniqueFileName(base, i, usedNames)
		rel := filepath.ToSlash(filepath.Join("files", rec.Scope, backupName))
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		if err := core.CopyFile(source, dst); err != nil {
			p.Logf("could not copy font %q: %v", rec.DisplayName, err)
			continue
		}
		manifest.Fonts = append(manifest.Fonts, FontManifest{
			DisplayName: rec.DisplayName,
			FileName:    filepath.Base(rec.FileName),
			SourcePath:  source,
			BackupFile:  rel,
			Scope:       rec.Scope,
		})
	}
	if err := writeJSON(filepath.Join(dir, "fonts.json"), manifest); err != nil {
		return err
	}
	p.Reportf("captured %d font(s)", len(manifest.Fonts))
	return nil
}

// Restore implements core.Module.
func (f *Fonts) Restore(ctx context.Context, dir string, opts core.RestoreOpts) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	if f.provider == nil {
		f.provider = platform.SystemFonts{}
	}
	var manifest FontsManifest
	if err := readJSON(filepath.Join(dir, "fonts.json"), &manifest); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read font manifest: %w", err)
	}
	conflicts := 0
	restored := 0
	for _, rec := range manifest.Fonts {
		src := filepath.Join(dir, filepath.FromSlash(rec.BackupFile))
		if !fileExists(src) {
			return fmt.Errorf("font file %s is missing from the backup", rec.BackupFile)
		}
		scope := rec.Scope
		if scope != "system" || !core.IsElevated() {
			scope = "user"
		}
		targetDir := f.provider.UserFontsDir()
		if scope == "system" {
			targetDir = f.provider.SystemFontsDir()
		}
		if targetDir == "" {
			return fmt.Errorf("font directory is unavailable for %q", rec.DisplayName)
		}
		dst := filepath.Join(targetDir, filepath.Base(rec.FileName))
		copied, conflict, err := copyFileWithConflict(src, dst, opts)
		if err != nil {
			return fmt.Errorf("restore font %q: %w", rec.DisplayName, err)
		}
		if conflict {
			conflicts++
		}
		if copied && !opts.DryRun {
			if err := f.provider.RegisterFont(platform.FontRecord{
				DisplayName: rec.DisplayName,
				FileName:    rec.FileName,
				Path:        dst,
				Scope:       scope,
			}, dst, scope); err != nil {
				return fmt.Errorf("register font %q: %w", rec.DisplayName, err)
			}
			restored++
			p.Reportf("restored font %q", rec.DisplayName)
		}
	}
	if opts.DryRun {
		p.Reportf("would restore %d font(s), %d conflict(s)", len(manifest.Fonts), conflicts)
	} else {
		p.Reportf("restored %d font(s), %d conflict(s)", restored, conflicts)
	}
	return nil
}

// PreviewRestore implements core.Previewer.
func (f *Fonts) PreviewRestore(ctx context.Context, dir string, opts core.RestoreOpts) (core.PreviewResult, error) {
	if f.provider == nil {
		f.provider = platform.SystemFonts{}
	}
	var manifest FontsManifest
	if err := readJSON(filepath.Join(dir, "fonts.json"), &manifest); err != nil {
		return core.PreviewResult{}, err
	}
	result := core.PreviewResult{ItemCount: len(manifest.Fonts), Details: fmt.Sprintf("%d font(s)", len(manifest.Fonts))}
	for _, rec := range manifest.Fonts {
		src := filepath.Join(dir, filepath.FromSlash(rec.BackupFile))
		if info, err := os.Stat(src); err == nil {
			result.Files++
			result.Bytes += info.Size()
		}
		targetDir := f.provider.UserFontsDir()
		if rec.Scope == "system" && core.IsElevated() {
			targetDir = f.provider.SystemFontsDir()
		}
		if targetDir != "" && fileExists(filepath.Join(targetDir, filepath.Base(rec.FileName))) {
			result.Conflicts++
		}
	}
	return result, nil
}

// Verify implements core.Module.
func (f *Fonts) Verify(ctx context.Context, dir string) error {
	var manifest FontsManifest
	if err := readJSON(filepath.Join(dir, "fonts.json"), &manifest); err != nil {
		return fmt.Errorf("fonts manifest: %w", err)
	}
	for _, rec := range manifest.Fonts {
		if rec.BackupFile == "" {
			return fmt.Errorf("font %q has no backup file", rec.DisplayName)
		}
		if !fileExists(filepath.Join(dir, filepath.FromSlash(rec.BackupFile))) {
			return fmt.Errorf("font file %s is missing", rec.BackupFile)
		}
	}
	return nil
}

// selectFontRecords filters out fonts that ship with Windows. Newer files in
// the system font directory that are not on the allowlist are treated as
// customized fonts, which errs on the side of preserving user data.
func selectFontRecords(system, user []platform.FontRecord, installTime int64) []platform.FontRecord {
	seen := make(map[string]bool)
	out := make([]platform.FontRecord, 0, len(system)+len(user))
	for _, rec := range user {
		key := "user:" + strings.ToLower(filepath.Base(rec.FileName))
		if rec.FileName == "" || seen[key] {
			continue
		}
		seen[key] = true
		rec.Scope = "user"
		out = append(out, rec)
	}
	for _, rec := range system {
		if rec.FileName == "" || IsDefaultWindowsFont(rec.FileName) {
			continue
		}
		if installTime > 0 && !fontModifiedAfterInstall(rec, installTime) {
			continue
		}
		key := "system:" + strings.ToLower(filepath.Base(rec.FileName))
		if seen[key] {
			continue
		}
		seen[key] = true
		rec.Scope = "system"
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return strings.ToLower(out[i].DisplayName) < strings.ToLower(out[j].DisplayName)
	})
	return out
}

func fontModifiedAfterInstall(rec platform.FontRecord, installTime int64) bool {
	path := rec.Path
	if path == "" {
		return true
	}
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return info.ModTime().Unix() > installTime+24*60*60
}

func fontSourcePath(rec platform.FontRecord, provider platform.FontProvider) string {
	if rec.Path != "" {
		return rec.Path
	}
	if rec.FileName == "" {
		return ""
	}
	if filepath.IsAbs(rec.FileName) {
		return rec.FileName
	}
	if rec.Scope == "system" {
		return filepath.Join(provider.SystemFontsDir(), rec.FileName)
	}
	return filepath.Join(provider.UserFontsDir(), rec.FileName)
}

func uniqueFileName(base string, index int, used map[string]bool) string {
	name := fmt.Sprintf("%03d-%s", index, base)
	lower := strings.ToLower(name)
	if used[lower] {
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		name = fmt.Sprintf("%03d-%s-%d%s", index, stem, index, ext)
	}
	used[strings.ToLower(name)] = true
	return name
}

var defaultWindowsFonts = map[string]bool{}

func init() {
	for _, name := range []string{
		"arial.ttf", "arialbd.ttf", "arialbi.ttf", "ariali.ttf", "ariblk.ttf",
		"bahnschrift.ttf", "calibri.ttf", "calibrib.ttf", "calibrii.ttf", "calibril.ttf", "calibrili.ttf", "calibriz.ttf",
		"cambria.ttc", "cambriab.ttf", "cambriai.ttf", "cambriaz.ttf",
		"candara.ttf", "candarab.ttf", "candarai.ttf", "candaral.ttf", "candarali.ttf", "candaraz.ttf",
		"comic.ttf", "comicbd.ttf", "comici.ttf", "comicz.ttf",
		"consola.ttf", "consolab.ttf", "consolai.ttf", "consolaz.ttf",
		"constan.ttf", "constanb.ttf", "constani.ttf", "constanz.ttf",
		"corbel.ttf", "corbelb.ttf", "corbeli.ttf", "corbell.ttf", "corbelli.ttf", "corbelz.ttf",
		"cour.ttf", "courbd.ttf", "courbi.ttf", "couri.ttf",
		"ebrima.ttf", "ebrimabd.ttf", "framd.ttf", "framdit.ttf",
		"gabriola.ttf", "georgia.ttf", "georgiab.ttf", "georgiai.ttf", "georgiaz.ttf",
		"impact.ttf", "inkfree.ttf", "lucon.ttf", "l_10646.ttf",
		"malgun.ttf", "malgunbd.ttf", "malgunsl.ttf", "marlett.ttf", "micross.ttf",
		"mingliu.ttc", "mmrtext.ttf", "mmrtextb.ttf", "mongolianbaiti.ttf",
		"msgothic.ttc", "msjh.ttc", "msjhbd.ttc", "msjhl.ttc", "msyh.ttc", "msyhbd.ttc", "msyhl.ttc", "msyi.ttf",
		"mvboli.ttf", "ntailu.ttf", "ntailub.ttf", "nirmala.ttf", "nirmalab.ttf", "nirmalas.ttf",
		"pala.ttf", "palab.ttf", "palabi.ttf", "palai.ttf",
		"segmdl2.ttf", "segoepr.ttf", "segoeprb.ttf", "segoesc.ttf", "segoescb.ttf",
		"segoeui.ttf", "segoeuib.ttf", "segoeuibi.ttf", "segoeuii.ttf", "segoeuil.ttf", "segoeuili.ttf",
		"segoeuisl.ttf", "segoeuislb.ttf", "segoeuiz.ttf", "seguibl.ttf", "seguibli.ttf",
		"seguiemj.ttf", "seguihis.ttf", "seguisb.ttf", "seguisbi.ttf", "seguisli.ttf", "seguisym.ttf",
		"simhei.ttf", "simsun.ttc", "simsunb.ttf", "sitka.ttc", "sitkab.ttc", "sitkai.ttc", "sitkaz.ttc",
		"sylfaen.ttf", "symbol.ttf", "tahoma.ttf", "tahomabd.ttf",
		"times.ttf", "timesbd.ttf", "timesbi.ttf", "timesi.ttf",
		"trebuc.ttf", "trebucbd.ttf", "trebucbi.ttf", "trebucit.ttf",
		"verdana.ttf", "verdanab.ttf", "verdanai.ttf", "verdanaz.ttf",
		"webdings.ttf", "wingding.ttf", "yugothb.ttf", "yugothl.ttf", "yugothm.ttf", "yugothr.ttf",
	} {
		defaultWindowsFonts[name] = true
	}
}

// IsDefaultWindowsFont reports whether filename is in the curated allowlist of
// fonts shipped with Windows. The list is intentionally conservative: missing
// an entry only causes an extra reinstallable font to be captured.
func IsDefaultWindowsFont(filename string) bool {
	base := strings.ToLower(filepath.Base(filename))
	if defaultWindowsFonts[base] {
		return true
	}
	if strings.HasSuffix(base, ".fon") {
		return true
	}
	return false
}
