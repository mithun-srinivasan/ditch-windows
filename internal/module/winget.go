package module

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
	"github.com/mithun-srinivasan/ditch-windows/internal/platform"
)

// Winget captures installed applications with "winget export" and classifies
// everything else so a fresh machine has an actionable reinstall list.
type Winget struct {
	lister func() ([]platform.Program, error)
}

// NewWinget returns the production winget module.
func NewWinget() *Winget {
	return &Winget{lister: platform.InstalledPrograms}
}

// WingetPackage is one package in a winget export file.
type WingetPackage struct {
	PackageIdentifier string `json:"packageIdentifier"`
	Version           string `json:"version,omitempty"`
}

// WingetSource groups packages by source in a winget export file.
type WingetSource struct {
	Packages      []WingetPackage `json:"packages"`
	SourceDetails struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"sourceDetails"`
}

// WingetExport is the JSON document produced by "winget export".
type WingetExport struct {
	Schema       string         `json:"$schema,omitempty"`
	CreationDate string         `json:"creationDate,omitempty"`
	Sources      []WingetSource `json:"sources"`
}

// Packages flattens all exported packages.
func (e WingetExport) Packages() []WingetPackage {
	var out []WingetPackage
	for _, source := range e.Sources {
		out = append(out, source.Packages...)
	}
	return out
}

// WingetListEntry is one row parsed from "winget list".
type WingetListEntry struct {
	Name    string `json:"name"`
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

// InventoryApp is one installed application and how it can be reinstalled.
type InventoryApp struct {
	Name          string `json:"name"`
	Version       string `json:"version,omitempty"`
	Publisher     string `json:"publisher,omitempty"`
	PackageID     string `json:"package_id,omitempty"`
	InstallSource string `json:"install_source"`
	Reason        string `json:"reason,omitempty"`
}

// Inventory is the classified app inventory written next to apps.json.
type Inventory struct {
	CapturedAt   time.Time      `json:"captured_at"`
	Winget       []InventoryApp `json:"winget"`
	Store        []InventoryApp `json:"store"`
	Scoop        []InventoryApp `json:"scoop"`
	Choco        []InventoryApp `json:"choco"`
	Manual       []InventoryApp `json:"manual"`
	BucketCounts map[string]int `json:"bucket_counts"`
}

// Counts returns the three TUI buckets: winget, alternative package managers,
// and manual reinstall.
func (i Inventory) Counts() (winget, alternatives, manual int) {
	return len(i.Winget), len(i.Store) + len(i.Scoop) + len(i.Choco), len(i.Manual)
}

func (i Inventory) all() []InventoryApp {
	out := make([]InventoryApp, 0, len(i.Winget)+len(i.Store)+len(i.Scoop)+len(i.Choco)+len(i.Manual))
	out = append(out, i.Winget...)
	out = append(out, i.Store...)
	out = append(out, i.Scoop...)
	out = append(out, i.Choco...)
	out = append(out, i.Manual...)
	return out
}

// Name implements core.Module.
func (w *Winget) Name() string { return "winget" }

// Description implements core.Module.
func (w *Winget) Description() string {
	return "Installed applications via winget export, with Store/Scoop/Choco and manual buckets"
}

// NeedsAdmin implements core.Module.
func (w *Winget) NeedsAdmin() bool { return false }

// Scan implements core.Module.
func (w *Winget) Scan(ctx context.Context) (core.Summary, error) {
	p, err := env(ctx)
	if err != nil {
		return core.Summary{}, err
	}
	if _, err := p.Exec.LookPath("winget"); err != nil {
		return core.Summary{Details: "winget not found; install App Installer from the Microsoft Store"}, nil
	}
	programs, listErr := w.lister()
	if listErr != nil {
		p.Logf("winget inventory: registry scan failed: %v", listErr)
		return core.Summary{Details: "winget export available; installed-app inventory unavailable"}, nil
	}
	count := 0
	for _, program := range programs {
		if inventoryProgram(program) {
			count++
		}
	}
	return core.Summary{
		ItemCount: count,
		Details:   fmt.Sprintf("%d installed programs; exports winget packages and a manual reinstall list", count),
	}, nil
}

// Backup implements core.Module.
func (w *Winget) Backup(ctx context.Context, dir string) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	if _, err := p.Exec.LookPath("winget"); err != nil {
		return fmt.Errorf("winget is not installed; install App Installer from the Microsoft Store")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create winget module directory: %w", err)
	}
	exportPath, err := filepath.Abs(filepath.Join(dir, "apps.json"))
	if err != nil {
		return err
	}
	p.Reportf("exporting installed winget packages")
	res, err := p.Exec.Run(ctx, "winget", "export",
		"--output", exportPath,
		"--include-versions",
		"--accept-source-agreements",
		"--disable-interactivity",
	)
	if err != nil {
		return fmt.Errorf("winget export failed: %s", commandError(err, res))
	}
	exportData, err := os.ReadFile(exportPath)
	if err != nil {
		return fmt.Errorf("winget did not produce an export file: %w", err)
	}
	var export WingetExport
	if err := json.Unmarshal(exportData, &export); err != nil {
		return fmt.Errorf("parse winget export: %w", err)
	}

	programs, registryErr := w.lister()
	if registryErr != nil {
		p.Logf("winget inventory: registry scan failed: %v", registryErr)
	}

	var wingetList []WingetListEntry
	listRes, listErr := p.Exec.Run(ctx, "winget", "list",
		"--accept-source-agreements",
		"--disable-interactivity",
	)
	if listErr != nil {
		p.Logf("winget list failed (classification falls back to export IDs): %s", commandError(listErr, listRes))
	} else {
		wingetList = ParseWingetList(listRes.Stdout)
	}

	inventory := ClassifyApps(programs, wingetList, export.Packages(), time.Now().UTC())
	if err := writeJSON(filepath.Join(dir, "inventory.json"), inventory); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manual-reinstall.md"), []byte(ManualReinstallMarkdown(inventory)), 0o600); err != nil {
		return fmt.Errorf("write manual reinstall list: %w", err)
	}
	reinstallable, alternatives, manual := inventory.Counts()
	p.Reportf("winget: %d reinstallable, %d Store/Scoop/Choco, %d manual", reinstallable, alternatives, manual)
	return nil
}

// Restore implements core.Module.
func (w *Winget) Restore(ctx context.Context, dir string, opts core.RestoreOpts) error {
	p, err := env(ctx)
	if err != nil {
		return err
	}
	exportPath := filepath.Join(dir, "apps.json")
	if !fileExists(exportPath) {
		p.Logf("winget: no apps.json in backup; nothing to import")
		return nil
	}
	data, err := os.ReadFile(exportPath)
	if err != nil {
		return fmt.Errorf("read winget export: %w", err)
	}
	var export WingetExport
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("parse winget export: %w", err)
	}
	packages := export.Packages()
	if opts.DryRun {
		p.Reportf("would import %d winget packages", len(packages))
		return nil
	}
	if len(packages) == 0 {
		return nil
	}
	if _, err := p.Exec.LookPath("winget"); err != nil {
		return fmt.Errorf("winget is not installed; install App Installer from the Microsoft Store")
	}
	p.Reportf("importing %d winget packages (this can take a long time)", len(packages))
	res, err := p.Exec.Run(ctx, "winget", "import",
		"--import-file", exportPath,
		"--accept-package-agreements",
		"--accept-source-agreements",
		"--ignore-unavailable",
		"--disable-interactivity",
	)
	if err != nil {
		return fmt.Errorf("winget import failed: %s", commandError(err, res))
	}
	if fileExists(filepath.Join(dir, "manual-reinstall.md")) {
		p.Reportf("see manual-reinstall.md for apps that winget cannot reinstall")
	}
	return nil
}

// Verify implements core.Module.
func (w *Winget) Verify(ctx context.Context, dir string) error {
	var export WingetExport
	if err := readJSON(filepath.Join(dir, "apps.json"), &export); err != nil {
		return fmt.Errorf("winget export: %w", err)
	}
	var inventory Inventory
	if err := readJSON(filepath.Join(dir, "inventory.json"), &inventory); err != nil {
		return fmt.Errorf("winget inventory: %w", err)
	}
	if !fileExists(filepath.Join(dir, "manual-reinstall.md")) {
		return fmt.Errorf("winget: manual-reinstall.md is missing")
	}
	return nil
}

var packageIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*\.[A-Za-z0-9._+-]+$`)
var columnSplitPattern = regexp.MustCompile(`[ \t]{2,}`)

// ParseWingetList parses the human-readable table emitted by "winget list".
// Column boundaries are two or more spaces; names with single spaces remain
// intact. Localized headers are tolerated because rows are identified by a
// package-shaped ID.
func ParseWingetList(data []byte) []WingetListEntry {
	var out []WingetListEntry
	for _, line := range splitLines(data) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "\u2500") {
			continue
		}
		fields := columnSplitPattern.Split(trimmed, -1)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSpace(fields[0])
		if name == "" || strings.EqualFold(name, "Name") {
			continue
		}
		idIndex := -1
		for i := 1; i < len(fields); i++ {
			field := strings.TrimSpace(fields[i])
			if packageIDPattern.MatchString(field) {
				idIndex = i
				break
			}
		}
		if idIndex < 0 {
			continue
		}
		entry := WingetListEntry{Name: name, ID: strings.TrimSpace(fields[idIndex])}
		if idIndex+1 < len(fields) {
			entry.Version = strings.TrimSpace(fields[idIndex+1])
			if strings.EqualFold(entry.Version, "Unknown") || strings.EqualFold(entry.Version, "Tag") {
				entry.Version = ""
			}
		}
		if len(fields) > idIndex+2 {
			source := strings.ToLower(strings.TrimSpace(fields[len(fields)-1]))
			switch source {
			case "winget", "msstore", "scoop", "choco", "chocolatey":
				entry.Source = source
			}
		}
		if entry.Source == "" {
			entry.Source = "winget"
		}
		out = append(out, entry)
	}
	return out
}

// ClassifyApps builds the three-bucket inventory. It is exported for tests.
func ClassifyApps(programs []platform.Program, wingetList []WingetListEntry, exported []WingetPackage, now time.Time) Inventory {
	inventory := Inventory{
		CapturedAt:   now,
		BucketCounts: make(map[string]int),
	}
	matchedIDs := make(map[string]bool)
	matchedNames := make(map[string]bool)
	candidates := append([]WingetListEntry(nil), wingetList...)
	for _, pkg := range exported {
		id := strings.TrimSpace(pkg.PackageIdentifier)
		if id == "" {
			continue
		}
		found := false
		for _, candidate := range candidates {
			if strings.EqualFold(candidate.ID, id) {
				found = true
				break
			}
		}
		if !found {
			candidates = append(candidates, WingetListEntry{
				Name:    displayNameFromID(id),
				ID:      id,
				Version: strings.TrimSpace(pkg.Version),
				Source:  "winget",
			})
		}
	}

	for _, program := range programs {
		if !inventoryProgram(program) {
			continue
		}
		entry := InventoryApp{
			Name:      strings.TrimSpace(program.DisplayName),
			Version:   strings.TrimSpace(program.DisplayVersion),
			Publisher: strings.TrimSpace(program.Publisher),
		}
		match := matchWinget(entry.Name, candidates)
		source := detectInstallSource(program)
		switch {
		case match != nil && source == "":
			entry.InstallSource = "winget"
		case match != nil && source != "store" && source != "scoop" && source != "choco":
			entry.InstallSource = "winget"
		case source != "":
			entry.InstallSource = source
		default:
			entry.InstallSource = "manual"
			entry.Reason = "no winget package or alternative package-manager source found"
		}
		if match != nil {
			entry.PackageID = match.ID
			if entry.Version == "" {
				entry.Version = match.Version
			}
			matchedIDs[strings.ToLower(match.ID)] = true
			matchedNames[normalizeAppName(match.Name)] = true
		}
		appendInventory(&inventory, entry)
	}

	// Add export packages that the registry did not surface (portable apps and
	// per-user installs frequently fall into this gap).
	exportedSeen := make(map[string]bool)
	for _, pkg := range exported {
		id := strings.TrimSpace(pkg.PackageIdentifier)
		if id == "" || exportedSeen[strings.ToLower(id)] {
			continue
		}
		exportedSeen[strings.ToLower(id)] = true
		if matchedIDs[strings.ToLower(id)] {
			continue
		}
		entry := InventoryApp{
			Name:          displayNameFromID(id),
			Version:       strings.TrimSpace(pkg.Version),
			PackageID:     id,
			InstallSource: "winget",
		}
		if match := matchWinget(entry.Name, wingetList); match != nil {
			if match.Version != "" && entry.Version == "" {
				entry.Version = match.Version
			}
		}
		appendInventory(&inventory, entry)
		matchedIDs[strings.ToLower(id)] = true
	}

	// Remaining winget rows are Store apps or packages installed outside the
	// uninstall registry. They still appear in one of the three buckets.
	for _, entry := range wingetList {
		if entry.ID == "" || matchedIDs[strings.ToLower(entry.ID)] {
			continue
		}
		app := InventoryApp{
			Name:      entry.Name,
			Version:   entry.Version,
			PackageID: entry.ID,
		}
		switch strings.ToLower(entry.Source) {
		case "msstore", "store":
			app.InstallSource = "store"
			app.Reason = "Microsoft Store app"
		case "scoop":
			app.InstallSource = "scoop"
		case "choco", "chocolatey":
			app.InstallSource = "choco"
		default:
			app.InstallSource = "winget"
		}
		appendInventory(&inventory, app)
		matchedIDs[strings.ToLower(entry.ID)] = true
	}

	sort.Slice(inventory.Winget, func(i, j int) bool {
		return strings.ToLower(inventory.Winget[i].Name) < strings.ToLower(inventory.Winget[j].Name)
	})
	sort.Slice(inventory.Store, func(i, j int) bool {
		return strings.ToLower(inventory.Store[i].Name) < strings.ToLower(inventory.Store[j].Name)
	})
	sort.Slice(inventory.Scoop, func(i, j int) bool {
		return strings.ToLower(inventory.Scoop[i].Name) < strings.ToLower(inventory.Scoop[j].Name)
	})
	sort.Slice(inventory.Choco, func(i, j int) bool {
		return strings.ToLower(inventory.Choco[i].Name) < strings.ToLower(inventory.Choco[j].Name)
	})
	sort.Slice(inventory.Manual, func(i, j int) bool {
		return strings.ToLower(inventory.Manual[i].Name) < strings.ToLower(inventory.Manual[j].Name)
	})

	inventory.BucketCounts["winget"] = len(inventory.Winget)
	inventory.BucketCounts["store"] = len(inventory.Store)
	inventory.BucketCounts["scoop"] = len(inventory.Scoop)
	inventory.BucketCounts["choco"] = len(inventory.Choco)
	inventory.BucketCounts["manual"] = len(inventory.Manual)
	return inventory
}

func inventoryProgram(p platform.Program) bool {
	name := strings.TrimSpace(p.DisplayName)
	if name == "" || p.SystemComponent || p.Hidden {
		return false
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "kb") && len(name) <= 10 {
		return false
	}
	return true
}

func appendInventory(inv *Inventory, app InventoryApp) {
	switch app.InstallSource {
	case "winget":
		inv.Winget = append(inv.Winget, app)
	case "store", "msstore":
		app.InstallSource = "store"
		inv.Store = append(inv.Store, app)
	case "scoop":
		inv.Scoop = append(inv.Scoop, app)
	case "choco", "chocolatey":
		app.InstallSource = "choco"
		inv.Choco = append(inv.Choco, app)
	default:
		app.InstallSource = "manual"
		inv.Manual = append(inv.Manual, app)
	}
}

func matchWinget(name string, entries []WingetListEntry) *WingetListEntry {
	target := normalizeAppName(name)
	if target == "" {
		return nil
	}
	var best *WingetListEntry
	bestScore := 0
	for i := range entries {
		candidate := normalizeAppName(entries[i].Name)
		if candidate == "" {
			continue
		}
		score := 0
		switch {
		case candidate == target:
			score = 100
		case strings.HasPrefix(target, candidate+" ") || strings.HasPrefix(candidate, target+" "):
			score = 60 + len(candidate)
		case normalizeAppName(displayNameFromID(entries[i].ID)) == target:
			score = 90
		}
		if score > bestScore {
			bestScore = score
			best = &entries[i]
		}
	}
	return best
}

func detectInstallSource(p platform.Program) string {
	haystack := strings.ToLower(p.UninstallString + " " + p.InstallLocation + " " + p.DisplayName)
	switch {
	case strings.Contains(haystack, "windowsapps"), strings.Contains(haystack, "microsoft store"):
		return "store"
	case strings.Contains(haystack, "scoop"):
		return "scoop"
	case strings.Contains(haystack, "chocolatey"), strings.Contains(haystack, "choco"):
		return "choco"
	}
	return ""
}

func normalizeAppName(name string) string {
	name = strings.ToLower(name)
	name = strings.NewReplacer("\u00ae", "", "\u2122", "", "\u00a9", "", "(", " ", ")", " ").Replace(name)
	var b strings.Builder
	space := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r > 127:
			b.WriteRune(r)
			space = false
		default:
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func displayNameFromID(id string) string {
	base := id
	if idx := strings.LastIndex(base, "."); idx >= 0 && idx < len(base)-1 {
		base = base[idx+1:]
	}
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}

// ManualReinstallMarkdown renders the human-actionable fallback list.
func ManualReinstallMarkdown(inv Inventory) string {
	var b strings.Builder
	b.WriteString("# Manual reinstall list\n\n")
	b.WriteString("Generated by ditch-windows. These applications were installed on the backed-up machine but cannot be reproduced by `winget import`.\n\n")
	b.WriteString("## Manual reinstall\n\n")
	if len(inv.Manual) == 0 {
		b.WriteString("_No manual-reinstall apps were detected._\n")
	} else {
		b.WriteString("| Application | Version | Publisher | Why manual |\n")
		b.WriteString("| --- | --- | --- | --- |\n")
		for _, app := range inv.Manual {
			reason := app.Reason
			if reason == "" {
				reason = "not available from winget"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
				mdEscape(app.Name), mdEscape(app.Version), mdEscape(app.Publisher), mdEscape(reason))
		}
	}
	b.WriteString("\n## Reinstall from another package manager\n\n")
	alternatives := append(append(append([]InventoryApp{}, inv.Store...), inv.Scoop...), inv.Choco...)
	if len(alternatives) == 0 {
		b.WriteString("_No Store, Scoop, or Chocolatey apps were detected._\n")
	} else {
		b.WriteString("| Application | Version | Source | Package |\n")
		b.WriteString("| --- | --- | --- | --- |\n")
		for _, app := range alternatives {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
				mdEscape(app.Name), mdEscape(app.Version), mdEscape(app.InstallSource), mdEscape(app.PackageID))
		}
	}
	b.WriteString("\n## winget-reinstallable\n\n")
	b.WriteString(fmt.Sprintf("%d packages are captured in `apps.json` and replayed by `winget import`.\n", len(inv.Winget)))
	return b.String()
}

func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.TrimSpace(s)
	if s == "" {
		return "—"
	}
	return s
}

func commandError(err error, res core.Result) string {
	text := core.SanitizeLogText(string(res.Stderr))
	if text == "" {
		text = core.SanitizeLogText(string(res.Stdout))
	}
	if text == "" {
		text = err.Error()
	}
	return text
}
