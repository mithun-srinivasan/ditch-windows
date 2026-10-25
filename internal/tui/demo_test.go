package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func testModel() demoModel {
	m := newDemoModel(DemoActions{})
	m.width = 110
	m.height = 32
	return m
}

func TestDemoViewRendersDashboard(t *testing.T) {
	m := testModel()
	view := m.View()
	for _, want := range []string{"ditch-windows", "SAFE DOCKER DEMO", "ACTIONS", "Ready to rebuild", "pack up Windows. Put it back.", "Backup", "Demo files"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "…") {
		t.Errorf("dashboard has a stray truncation marker:\n%s", view)
	}
}

func TestFitLinesLeavesExactWidthAlone(t *testing.T) {
	line := strings.Repeat("x", 24)
	if got := fitLines(line, 24, 1); got != line {
		t.Fatalf("fitLines changed an exact-width line: %q", got)
	}
	if got := fitLines(line+"y", 24, 1); got == line+"y" {
		t.Fatal("fitLines failed to truncate an over-width line")
	}
}

func TestDemoViewFitsCommonTerminals(t *testing.T) {
	for _, size := range []struct{ width, height int }{{80, 24}, {100, 30}, {110, 32}, {140, 42}} {
		m := testModel()
		m.width, m.height = size.width, size.height
		view := m.View()
		if got := strings.Count(view, "\n") + 1; got > size.height {
			t.Errorf("%dx%d view has %d lines, terminal has %d", size.width, size.height, got, size.height)
		}
		if got := lipgloss.Width(view); got != size.width {
			t.Errorf("%dx%d view is %d columns wide", size.width, size.height, got)
		}
	}
}

func TestDemoViewShowsModulesAndPreview(t *testing.T) {
	m := testModel()
	m.screen = screenPreview
	m.preview = PreviewResult{
		BackupID:  "backup-1",
		Files:     5,
		Bytes:     330,
		Conflicts: 2,
		Modules: []ModuleRow{
			{Name: "apps", Status: "ready", Items: 6, Details: "3 winget"},
			{Name: "files", Status: "ready", Items: 5, Conflicts: 2, Details: "4 folders"},
		},
	}
	view := m.View()
	for _, want := range []string{"RESTORE PREVIEW", "apps", "files", "conflicts 2", "a apply"} {
		if !strings.Contains(view, want) {
			t.Errorf("preview view is missing %q:\n%s", want, view)
		}
	}
}

func TestDemoApplyRequiresPreview(t *testing.T) {
	m := testModel()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(demoModel)
	if m.screen != screenHome {
		t.Fatalf("screen = %v, want home", m.screen)
	}
	if cmd != nil {
		t.Fatal("apply without a preview must not start an action")
	}
	if !strings.Contains(m.err, "Preview a restore") {
		t.Fatalf("expected a preview guard message, got %q", m.err)
	}
}

func TestDemoConfirmAndQuit(t *testing.T) {
	m := testModel()
	m.screen = screenPreview
	m.preview = PreviewResult{BackupID: "backup-1", Files: 5, Conflicts: 2}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(demoModel)
	if m.screen != screenConfirm {
		t.Fatalf("screen = %v, want confirm", m.screen)
	}
	if !strings.Contains(m.View(), "APPLY RESTORE?") {
		t.Fatal("confirm screen should explain the restore")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(demoModel)
	if m.screen != screenPreview {
		t.Fatalf("cancel should return to preview, got %v", m.screen)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(demoModel)
	if m.screen != screenHome {
		t.Fatalf("esc should return home, got %v", m.screen)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q should quit the demo")
	}
	_ = next

	// Confirming the preview must actually start the apply, not bounce back to
	// the confirmation screen.
	m = testModel()
	m.screen = screenPreview
	m.preview = PreviewResult{BackupID: "backup-1"}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(demoModel)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = next.(demoModel)
	if m.screen != screenBusy {
		t.Fatalf("y should start the apply, screen = %v", m.screen)
	}
	if cmd == nil {
		t.Fatal("y should return an action command")
	}
}

func TestDemoBackupResult(t *testing.T) {
	m := testModel()
	m.preview = PreviewResult{BackupID: "old-backup"}
	next, _ := m.Update(actionDoneMsg{
		action: actBackup,
		backup: BackupResult{
			BackupID:  "20260101T000000Z-abcdef",
			Snapshots: 3,
			Modules: []ModuleRow{
				{Name: "apps", Status: "ok", Items: 6, Details: "3 winget"},
				{Name: "files", Status: "ok", Items: 5, Details: "5 files"},
			},
		},
	})
	m = next.(demoModel)
	view := m.View()
	for _, want := range []string{"apps", "files", "20260101T000000Z-abcdef", "3 snapshot(s)"} {
		if !strings.Contains(view, want) {
			t.Errorf("backup result is missing %q:\n%s", want, view)
		}
	}
	if m.preview.BackupID != "" {
		t.Fatal("a new backup must invalidate the old preview")
	}
}
