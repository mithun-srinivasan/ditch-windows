package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func sampleManifest() *Manifest {
	return &Manifest{
		SchemaVersion: SchemaVersion,
		BackupID:      "20260101T000000Z-abcdef",
		ToolVersion:   "0.1.0",
		GoVersion:     "go1.22",
		OS:            "windows",
		Arch:          "amd64",
		Hostname:      "desktop",
		Username:      "user",
		WindowsBuild:  26100,
		StartedUTC:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CompletedUTC:  time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC),
		DurationMS:    60000,
		RepoInfo:      "local:C:/backup",
		Modules: map[string]ModuleStatus{
			"winget": {Name: "winget", Success: true, ItemCount: 42},
			"wifi":   {Name: "wifi", Success: true, ItemCount: 2},
		},
		Hashes: map[string]string{"modules/winget/apps.json": strings.Repeat("a", 64)},
	}
}

func TestManifestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup", "manifest.json")
	m := sampleManifest()
	if err := WriteManifest(path, m); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("manifest permissions are too broad: %v", info.Mode())
	}
	got, err := ReadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.BackupID != m.BackupID || got.WindowsBuild != 26100 || got.Modules["winget"].ItemCount != 42 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if !got.AllSucceeded() {
		t.Fatal("manifest with successful modules reported failure")
	}
}

func TestManifestNewerSchemaRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":99,"modules":{},"hashes":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected newer-schema error, got %v", err)
	}
}

func TestFindManifest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "backup", "manifest.json")
	if err := WriteManifest(path, sampleManifest()); err != nil {
		t.Fatal(err)
	}
	found, m, err := FindManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if found != path || m.BackupID == "" {
		t.Fatalf("found=%s manifest=%+v", found, m)
	}
	if _, _, err := FindManifest(t.TempDir()); err == nil {
		t.Fatal("expected missing manifest error")
	}
}

func TestManifestStatusHelpers(t *testing.T) {
	m := sampleManifest()
	m.Modules["failing"] = ModuleStatus{Name: "failing", Error: "boom"}
	m.Modules["skipped"] = ModuleStatus{Name: "skipped", Skipped: true}
	if m.AllSucceeded() {
		t.Fatal("AllSucceeded ignored a failing module")
	}
	statuses := m.SortModuleStatuses()
	var names []string
	for _, status := range statuses {
		names = append(names, status.Name)
	}
	if strings.Join(names, ",") != "failing,skipped,wifi,winget" {
		t.Fatalf("unexpected sort order: %v", names)
	}
	if !strings.Contains(m.Modules["winget"].SummaryLine(), "ok") {
		t.Fatal("success summary line is wrong")
	}
	if !strings.Contains(m.Modules["failing"].SummaryLine(), "FAILED") {
		t.Fatal("failure summary line is wrong")
	}
	if !strings.Contains(m.Modules["skipped"].SummaryLine(), "skipped") {
		t.Fatal("skipped summary line is wrong")
	}
}

func TestBackupTagsAndSanitize(t *testing.T) {
	tags := BackupTags("id", "metadata")
	if len(tags) != 3 || tags[0] != "backup=id" || tags[1] != "kind=metadata" || tags[2] != "tool=ditch-windows" {
		t.Fatalf("unexpected tags: %v", tags)
	}
	long := strings.Repeat("x", 2100)
	got := SanitizeLogText(" a\r\nb " + long)
	if strings.Contains(got, "\r") || len(got) > 2000 {
		t.Fatalf("sanitize failed: len=%d", len(got))
	}
}
