package module

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

const sampleDefaultAppsXML = `<?xml version="1.0" encoding="UTF-8"?>
<DefaultAssociations Version="1.0">
  <Association Identifier=".pdf" ApplicationName="PDF Viewer" ProgId="Acme.PDF" ApplicationType="Application" />
  <Association Identifier=".html" ApplicationName="Browser" ProgId="Acme.Browser" ApplicationType="Application" />
</DefaultAssociations>`

func TestParseDefaultAssociations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "DefaultAppAssociations.xml")
	if err := os.WriteFile(path, []byte(sampleDefaultAppsXML), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := ParseDefaultAssociations(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Associations) != 2 {
		t.Fatalf("expected 2 associations, got %d", len(doc.Associations))
	}
	if doc.Associations[0].Identifier != ".pdf" || doc.Associations[0].ProgID != "Acme.PDF" {
		t.Fatalf("unexpected association: %+v", doc.Associations[0])
	}
}

func TestParseDefaultAssociationsRejectsWrongRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.xml")
	if err := os.WriteFile(path, []byte(`<NotDefaultApps/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseDefaultAssociations(path); err == nil {
		t.Fatal("expected an error for an unexpected root element")
	}
}

func TestDefaultAppsBackupRestoreWithFakeDISM(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "module")
	exec := newFakeExec()
	exec.look["dism"] = true
	exec.run = func(name string, args []string) (core.Result, error) {
		if name != "dism" {
			return core.Result{}, nil
		}
		for _, arg := range args {
			if strings.HasPrefix(arg, "/Export-DefaultAppAssociations:") {
				path := strings.TrimPrefix(arg, "/Export-DefaultAppAssociations:")
				if err := os.WriteFile(path, []byte(sampleDefaultAppsXML), 0o600); err != nil {
					return core.Result{}, err
				}
			}
		}
		return core.Result{}, nil
	}
	p := testParams(exec, &fakeRepo{}, home)
	d := NewDefaultApps()
	ctx := ctxWith(p)
	if _, err := d.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Backup(ctx, dir); err != nil {
		t.Fatalf("backup: %v", err)
	}
	requireFile(t, filepath.Join(dir, "DefaultAppAssociations.xml"))
	requireContains(t, filepath.Join(dir, "defaultapps.json"), `"association_count": 2`)
	if err := d.Verify(ctx, dir); err != nil {
		t.Fatalf("verify: %v", err)
	}

	if err := d.Restore(ctx, dir, core.RestoreOpts{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	for _, call := range exec.callsFor("dism") {
		if containsArg(call.Args, "/Import-DefaultAppAssociations:"+filepath.Join(dir, "DefaultAppAssociations.xml")) {
			t.Fatal("dry run must not import associations")
		}
	}
	if err := d.Restore(ctx, dir, core.RestoreOpts{}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	imported := false
	for _, call := range exec.callsFor("dism") {
		for _, arg := range call.Args {
			if strings.HasPrefix(arg, "/Import-DefaultAppAssociations:") {
				imported = true
			}
		}
	}
	if !imported {
		t.Fatal("expected DISM import to run")
	}
}
