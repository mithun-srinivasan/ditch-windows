// Package tui implements the bubbletea demo interface. The same model can later
// host the production screens; for now it drives the synthetic Docker demo.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ModuleRow is one module's status or preview row.
type ModuleRow struct {
	Name      string
	Status    string
	Details   string
	Items     int
	Conflicts int
}

// BackupResult is returned by a demo backup.
type BackupResult struct {
	BackupID  string
	Snapshots int
	Modules   []ModuleRow
}

// PreviewResult is a dry-run restore summary.
type PreviewResult struct {
	BackupID  string
	Modules   []ModuleRow
	Files     int64
	Bytes     int64
	Conflicts int64
}

// RestoreResult is an applied restore summary.
type RestoreResult struct {
	Modules []ModuleRow
	Baks    []string
}

// VerifyResult is a repository/manifest verification summary.
type VerifyResult struct {
	BackupID string
	RepoOK   bool
	HashOK   bool
	Modules  int
	Message  string
}

// BackupRow is one entry in the backup list.
type BackupRow struct {
	ID   string
	Time string
	Host string
}

// DemoActions are the operations the TUI can run. Each closure is executed on a
// bubbletea command goroutine and must not touch the model directly.
type DemoActions struct {
	Backup  func(context.Context) (BackupResult, error)
	List    func(context.Context) ([]BackupRow, error)
	Preview func(context.Context) (PreviewResult, error)
	Apply   func(context.Context) (RestoreResult, error)
	Verify  func(context.Context) (VerifyResult, error)
	Files   func() ([]string, error)
}

// RunDemo starts the full-screen demo TUI.
func RunDemo(actions DemoActions) error {
	model := newDemoModel(actions)
	_, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}

type action int

const (
	actBackup action = iota
	actPreview
	actApply
	actVerify
	actList
	actFiles
	actQuit
)

type menuEntry struct {
	label  string
	key    string
	action action
}

var demoMenu = []menuEntry{
	{"Backup", "b", actBackup},
	{"Preview restore", "p", actPreview},
	{"Apply restore", "a", actApply},
	{"Verify", "v", actVerify},
	{"Backups", "l", actList},
	{"Demo files", "f", actFiles},
	{"Quit", "q", actQuit},
}

type screen int

const (
	screenHome screen = iota
	screenBusy
	screenPreview
	screenConfirm
	screenFiles
)

type actionDoneMsg struct {
	action  action
	backup  BackupResult
	preview PreviewResult
	restore RestoreResult
	verify  VerifyResult
	backups []BackupRow
	files   []string
	err     error
}

type demoModel struct {
	actions DemoActions
	width   int
	height  int
	cursor  int
	screen  screen
	pending action
	spinner spinner.Model
	busy    string
	err     string
	notice  string

	modules   []ModuleRow
	backupID  string
	snapshots int
	preview   PreviewResult
	restore   RestoreResult
	verify    VerifyResult
	backups   []BackupRow
	files     []string
}

func newDemoModel(actions DemoActions) demoModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorAccent)
	return demoModel{actions: actions, spinner: sp}
}

var (
	colorBg      = lipgloss.Color("#0B0F14")
	colorPanel   = lipgloss.Color("#111827")
	colorPanel2  = lipgloss.Color("#0F172A")
	colorBorder  = lipgloss.Color("#243044")
	colorText    = lipgloss.Color("#E5E7EB")
	colorMuted   = lipgloss.Color("#94A3B8")
	colorAccent  = lipgloss.Color("#8B5CF6")
	colorAccent2 = lipgloss.Color("#22D3EE")
	colorGreen   = lipgloss.Color("#34D399")
	colorYellow  = lipgloss.Color("#FBBF24")
	colorRed     = lipgloss.Color("#F87171")

	baseStyle = lipgloss.NewStyle().
			Background(colorBg).
			Foreground(colorText)
	panelStyle = lipgloss.NewStyle().
			Background(colorPanel).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)
	mutedStyle  = lipgloss.NewStyle().Foreground(colorMuted)
	accentStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	cyanStyle   = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	greenStyle  = lipgloss.NewStyle().Foreground(colorGreen)
	yellowStyle = lipgloss.NewStyle().Foreground(colorYellow)
	redStyle    = lipgloss.NewStyle().Foreground(colorRed)
)

func (m demoModel) Init() tea.Cmd { return nil }

func (m demoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case spinner.TickMsg:
		if m.screen != screenBusy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case actionDoneMsg:
		return m.finishAction(msg)
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.screen {
		case screenBusy:
			return m, nil
		case screenConfirm:
			switch key {
			case "y", "Y", "enter":
				return m.startAction(actApply)
			case "n", "N", "esc":
				m.screen = screenPreview
				return m, nil
			case "q":
				return m, tea.Quit
			}
			return m, nil
		case screenPreview:
			switch key {
			case "a", "A":
				return m.startAction(actApply)
			case "esc", "enter":
				m.screen = screenHome
			case "q":
				return m, tea.Quit
			}
			return m, nil
		case screenFiles:
			switch key {
			case "esc", "enter", "f":
				m.screen = screenHome
			case "q":
				return m, tea.Quit
			}
			return m, nil
		default:
			return m.updateHome(key)
		}
	}
	return m, nil
}

func (m demoModel) updateHome(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(demoMenu)-1 {
			m.cursor++
		}
	case "enter":
		return m.startAction(demoMenu[m.cursor].action)
	case "b":
		return m.startAction(actBackup)
	case "p":
		return m.startAction(actPreview)
	case "a":
		return m.startAction(actApply)
	case "v":
		return m.startAction(actVerify)
	case "l":
		return m.startAction(actList)
	case "f":
		return m.startAction(actFiles)
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m demoModel) startAction(a action) (tea.Model, tea.Cmd) {
	if a == actQuit {
		return m, tea.Quit
	}
	if a == actApply {
		if m.screen != screenConfirm && m.preview.BackupID == "" {
			m.err = "Preview a restore before applying it."
			m.notice = ""
			m.screen = screenHome
			return m, nil
		}
		if m.screen == screenConfirm {
			m.pending = actApply
			m.err = ""
			m.notice = ""
			m.screen = screenBusy
			m.busy = busyLabel(a)
			return m, tea.Batch(m.spinner.Tick, m.runAction(a))
		}
		m.pending = actApply
		m.screen = screenConfirm
		return m, nil
	}
	m.pending = a
	m.err = ""
	m.notice = ""
	m.screen = screenBusy
	m.busy = busyLabel(a)
	return m, tea.Batch(m.spinner.Tick, m.runAction(a))
}

func (m demoModel) runAction(a action) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		msg := actionDoneMsg{action: a}
		switch a {
		case actBackup:
			msg.backup, msg.err = m.actions.Backup(ctx)
		case actPreview:
			msg.preview, msg.err = m.actions.Preview(ctx)
		case actApply:
			msg.restore, msg.err = m.actions.Apply(ctx)
		case actVerify:
			msg.verify, msg.err = m.actions.Verify(ctx)
		case actList:
			msg.backups, msg.err = m.actions.List(ctx)
		case actFiles:
			msg.files, msg.err = m.actions.Files()
		}
		return msg
	}
}

func (m demoModel) finishAction(msg actionDoneMsg) (tea.Model, tea.Cmd) {
	m.pending = 0
	if msg.err != nil {
		m.err = msg.err.Error()
	}
	switch msg.action {
	case actBackup:
		m.modules = msg.backup.Modules
		m.backupID = msg.backup.BackupID
		m.snapshots = msg.backup.Snapshots
		// A new backup changes what "newest" means. Never let an old preview
		// authorize an apply that would restore a different snapshot.
		m.preview = PreviewResult{}
		m.verify = VerifyResult{}
		m.backups = nil
		if msg.err == nil {
			m.notice = "Backup complete. Preview the restore next."
		}
		m.screen = screenHome
	case actPreview:
		m.preview = msg.preview
		m.screen = screenPreview
	case actApply:
		m.restore = msg.restore
		if msg.err == nil {
			m.notice = "Restore applied inside the demo home."
			if len(msg.restore.Baks) > 0 {
				m.notice = fmt.Sprintf("Restored; %d file(s) kept as .bak.", len(msg.restore.Baks))
			}
		}
		m.screen = screenHome
	case actVerify:
		m.verify = msg.verify
		if msg.err == nil {
			m.notice = "Verification finished."
		}
		m.screen = screenHome
	case actList:
		m.backups = msg.backups
		if msg.err == nil {
			m.notice = "Loaded backups from demo repository."
		}
		m.screen = screenHome
	case actFiles:
		m.files = msg.files
		m.screen = screenFiles
	}
	return m, nil
}

func busyLabel(a action) string {
	switch a {
	case actBackup:
		return "Backing up apps, Wi-Fi, files, and theme"
	case actPreview:
		return "Decrypting metadata and building the restore preview"
	case actApply:
		return "Restoring into the synthetic demo home"
	case actVerify:
		return "Checking repository integrity and manifest hashes"
	case actList:
		return "Reading snapshot list"
	case actFiles:
		return "Reading demo home"
	}
	return "Working"
}

func (m demoModel) View() string {
	if m.width < 80 || m.height < 24 {
		return baseStyle.Render("ditch-windows demo\n\n" +
			"This interface needs at least an 80x24 terminal.\n" +
			"Resize the window, or run the automated tour without -it.\n")
	}
	header := m.headerView()
	footer := m.footerView()
	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer) - 2
	if bodyHeight < 6 {
		bodyHeight = 6
	}
	left := m.menuView(bodyHeight)
	right := m.contentView(bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	return baseStyle.Render(lipgloss.JoinVertical(lipgloss.Left, header, body, footer))
}

func (m demoModel) headerView() string {
	title := accentStyle.Render("ditch-windows")
	subtitle := mutedStyle.Render("  Windows 11 rebuild kit")
	badge := lipgloss.NewStyle().
		Background(lipgloss.Color("#172554")).
		Foreground(colorAccent2).
		Bold(true).
		Padding(0, 1).
		Render("SAFE DOCKER DEMO")
	inner := m.width - 2
	if inner < 30 {
		inner = 30
	}
	left := title + subtitle
	gap := inner - lipgloss.Width(left) - lipgloss.Width(badge) - 2
	if gap < 1 {
		gap = 1
	}
	line1 := left + strings.Repeat(" ", gap) + badge
	line2 := mutedStyle.Render("apps · wifi · files · theme   •   restic encrypted   •   no host data")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Width(inner).
		Render(line1 + "\n" + line2)
}

func (m demoModel) menuView(height int) string {
	var b strings.Builder
	b.WriteString(mutedStyle.Bold(true).Render("ACTIONS"))
	b.WriteString("\n\n")
	for i, item := range demoMenu {
		marker := "  "
		if i == m.cursor && m.screen != screenBusy {
			marker = "▸ "
		}
		line := fmt.Sprintf("%s%-16s %s", marker, item.label, mutedStyle.Render(item.key))
		if i == m.cursor && m.screen != screenBusy {
			line = lipgloss.NewStyle().
				Background(colorAccent).
				Foreground(lipgloss.Color("#0B0F14")).
				Bold(true).
				Width(24).
				Render(fmt.Sprintf("▸ %-16s %s", item.label, item.key))
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("j/k or ↑/↓ move"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("enter select"))
	style := panelStyle.Width(26).Height(height)
	return style.Render(fitLines(b.String(), 24, height))
}

func (m demoModel) contentView(height int) string {
	width := m.width - lipgloss.Width(m.menuView(height)) - 3
	if width < 30 {
		width = 30
	}
	style := panelStyle.Width(width).Height(height)
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	return style.Render(fitLines(m.contentBody(inner), inner, height))
}

func (m demoModel) contentBody(width int) string {
	if m.screen == screenBusy {
		return "\n  " + m.spinner.View() + " " + cyanStyle.Render(m.busy) + "\n\n" +
			mutedStyle.Render("  restic is working; this may take a few seconds.") + "\n\n" +
			mutedStyle.Render("  q is disabled while an operation is running.")
	}
	if m.screen == screenPreview {
		return m.previewView(width)
	}
	if m.screen == screenConfirm {
		return m.confirmView(width)
	}
	if m.screen == screenFiles {
		return m.filesView(width)
	}
	return m.homeView(width)
}

func (m demoModel) homeView(width int) string {
	var b strings.Builder
	b.WriteString(mutedStyle.Bold(true).Render("DASHBOARD"))
	b.WriteString("\n\n")
	if m.err != "" {
		b.WriteString(redStyle.Render("✗ " + m.err))
		b.WriteString("\n\n")
	}
	if m.notice != "" {
		b.WriteString(greenStyle.Render("✓ " + m.notice))
		b.WriteString("\n\n")
	}
	if len(m.modules) > 0 {
		b.WriteString(m.moduleTable(width))
		b.WriteString("\n")
		if m.backupID != "" {
			b.WriteString(mutedStyle.Render(fmt.Sprintf("backup %s  ·  %d snapshot(s)", m.backupID, m.snapshots)))
			b.WriteString("\n")
		}
	} else {
		b.WriteString(m.welcomeView(width))
	}
	if len(m.backups) > 0 {
		b.WriteString("\n")
		b.WriteString(m.backupListView())
	}
	if m.verify.Message != "" || m.verify.BackupID != "" {
		b.WriteString("\n")
		b.WriteString(m.verifyView())
	}
	if len(m.restore.Modules) > 0 {
		b.WriteString("\n")
		b.WriteString(cyanStyle.Render("LAST RESTORE"))
		b.WriteString("\n")
		b.WriteString(m.restoreView())
	}
	b.WriteString("\n\n")
	hint := "b backup   p preview   a apply   v verify   l backups   q quit"
	if width < 55 {
		hint = "b/p/a/v/l actions   q quit"
	}
	b.WriteString(mutedStyle.Render(hint))
	return b.String()
}

func (m demoModel) welcomeView(width int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render("Ready to rebuild, safely."))
	b.WriteString("\n\n")
	if width < 60 {
		b.WriteString("Sandbox only. The demo home and\n")
		b.WriteString("repository live under /demo.\n")
		b.WriteString("Your Windows profile is untouched.\n\n")
	} else {
		b.WriteString("This container is a sandbox. The demo home, repository, config, and logs\n")
		b.WriteString("all live under /demo. Your Windows profile is not mounted or read.\n\n")
	}
	b.WriteString(mutedStyle.Bold(true).Render("WHAT RUNS"))
	b.WriteString("\n")
	cards := []struct{ name, detail string }{
		{"apps", "synthetic winget inventory + manual list"},
		{"wifi", "fake secrets, encrypted-only restic payload"},
		{"files", "real production module + real restic"},
		{"theme", "synthetic wallpaper/accent fixture"},
	}
	if width < 60 {
		cards = []struct{ name, detail string }{
			{"apps", "synthetic winget inventory"},
			{"wifi", "encrypted-only fake secrets"},
			{"files", "real module + real restic"},
			{"theme", "synthetic theme fixture"},
		}
	}
	for _, c := range cards {
		b.WriteString(fmt.Sprintf("  %s  %s\n", cyanStyle.Width(7).Render(c.name), mutedStyle.Render(c.detail)))
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("Press b or enter to run the first backup."))
	return b.String()
}

func (m demoModel) previewView(width int) string {
	var b strings.Builder
	b.WriteString(cyanStyle.Bold(true).Render("RESTORE PREVIEW"))
	b.WriteString("  ")
	b.WriteString(mutedStyle.Render("dry run — nothing has been written"))
	b.WriteString("\n\n")
	if m.err != "" {
		b.WriteString(redStyle.Render("✗ " + m.err))
		b.WriteString("\n\n")
	}
	b.WriteString(m.moduleTable(width))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("files %d   bytes %d   conflicts %d\n",
		m.preview.Files, m.preview.Bytes, m.preview.Conflicts))
	if m.preview.BackupID != "" {
		b.WriteString(mutedStyle.Render("backup " + m.preview.BackupID))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("a apply this preview   esc back"))
	return b.String()
}

func (m demoModel) confirmView(width int) string {
	var b strings.Builder
	b.WriteString(yellowStyle.Bold(true).Render("APPLY RESTORE?"))
	b.WriteString("\n\n")
	b.WriteString("This will write into ")
	b.WriteString(cyanStyle.Render("/demo/home"))
	b.WriteString(" only.\n")
	b.WriteString("Existing files are preserved as ")
	b.WriteString(cyanStyle.Render(".bak"))
	b.WriteString(" copies.\n\n")
	b.WriteString(fmt.Sprintf("files %d   conflicts %d\n", m.preview.Files, m.preview.Conflicts))
	if m.preview.BackupID != "" {
		b.WriteString(mutedStyle.Render("backup " + m.preview.BackupID))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(greenStyle.Render("y") + " apply    " + mutedStyle.Render("n/esc cancel"))
	return b.String()
}

func (m demoModel) filesView(width int) string {
	var b strings.Builder
	b.WriteString(mutedStyle.Bold(true).Render("SYNTHETIC DEMO HOME"))
	b.WriteString("\n\n")
	if len(m.files) == 0 {
		b.WriteString(mutedStyle.Render("No files found."))
	} else {
		for _, f := range m.files {
			b.WriteString("  " + f + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("esc back"))
	return b.String()
}

func (m demoModel) moduleTable(width int) string {
	if len(m.modules) == 0 {
		return mutedStyle.Render("No module data yet.")
	}
	var b strings.Builder
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%-10s %-8s %6s %6s  %s", "MODULE", "STATUS", "ITEMS", "CONFLICT", "DETAILS")))
	b.WriteString("\n")
	for _, row := range m.modules {
		status := row.Status
		if status == "" {
			status = "ok"
		}
		var styled string
		switch status {
		case "ok", "ready", "verified":
			styled = greenStyle.Render(status)
		case "skipped", "unavailable":
			styled = yellowStyle.Render(status)
		default:
			styled = redStyle.Render(status)
		}
		statusCell := styled + strings.Repeat(" ", max(0, 8-len(status)))
		details := truncate(row.Details, width-36)
		b.WriteString(fmt.Sprintf("%-10s %s %6d %6d  %s\n", row.Name, statusCell, row.Items, row.Conflicts, mutedStyle.Render(details)))
	}
	return b.String()
}

func (m demoModel) backupListView() string {
	var b strings.Builder
	b.WriteString(cyanStyle.Render("BACKUPS"))
	b.WriteString("\n")
	for _, row := range m.backups {
		fmt.Fprintf(&b, "  %-24s %s  %s\n", row.ID, mutedStyle.Render(row.Time), mutedStyle.Render(row.Host))
	}
	return b.String()
}

func (m demoModel) verifyView() string {
	ok := m.verify.RepoOK && m.verify.HashOK
	head := greenStyle.Render("✓ VERIFIED")
	if !ok {
		head = redStyle.Render("✗ VERIFICATION ISSUE")
	}
	line := fmt.Sprintf("%s  repo=%v  hashes=%v  modules=%d", head, m.verify.RepoOK, m.verify.HashOK, m.verify.Modules)
	if m.verify.Message != "" {
		line += "\n" + mutedStyle.Render(m.verify.Message)
	}
	return line
}

func (m demoModel) restoreView() string {
	var b strings.Builder
	for _, row := range m.restore.Modules {
		state := greenStyle.Render("ok")
		if row.Status != "" && row.Status != "ok" {
			state = redStyle.Render(row.Status)
		}
		fmt.Fprintf(&b, "  %-10s %s\n", row.Name, state)
	}
	if len(m.restore.Baks) > 0 {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  %d .bak file(s) preserved", len(m.restore.Baks))) + "\n")
	}
	return b.String()
}

func (m demoModel) footerView() string {
	hint := "↑↓/jk move   enter   b/p/a/v/l/f actions   q quit"
	if m.screen == screenPreview {
		hint = "a apply   esc back   q quit"
	}
	if m.screen == screenConfirm {
		hint = "y confirm   n/esc cancel"
	}
	if m.screen == screenFiles {
		hint = "esc back   q quit"
	}
	if m.screen == screenBusy {
		hint = "operation in progress…"
	}
	left := mutedStyle.Render(hint)
	right := mutedStyle.Render("v0.1 · MIT")
	inner := m.width
	if inner < 30 {
		inner = 30
	}
	gap := inner - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(colorBorder).
		Padding(0, 1).
		Width(inner).
		Render(left + strings.Repeat(" ", gap) + right)
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

// fitLines prevents lipgloss from growing a panel when content is wider or
// taller than the terminal. ansi.Truncate keeps styling escape sequences
// balanced, unlike slicing the raw string.
func fitLines(s string, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	return strings.Join(lines, "\n")
}
