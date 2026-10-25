package module

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

const sampleWifiProfiles = `Profiles on interface Wi-Fi:

Group policy profiles (read only)
---------------------------------
    <None>

User profiles
-------------
    All User Profile     : HomeNet
    All User Profile     : Cafe WiFi
    All User Profile     : HomeNet
`

const sampleWifiXML = `<?xml version="1.0"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1">
  <name>HomeNet</name>
  <SSIDConfig><SSID><name>HomeNet</name></SSID></SSIDConfig>
  <MSM><security><authEncryption><authentication>WPA2PSK</authentication></authEncryption>
  <sharedKey><keyType>passPhrase</keyType><protected>false</protected><keyMaterial>super-secret-passphrase</keyMaterial></sharedKey>
  </security></MSM>
</WLANProfile>`

func TestParseWifiProfiles(t *testing.T) {
	got := ParseWifiProfiles([]byte(sampleWifiProfiles))
	want := []string{"Cafe WiFi", "HomeNet"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestProfileNameFromXMLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Wi-Fi-HomeNet.xml")
	if err := os.WriteFile(path, []byte(sampleWifiXML), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := ProfileNameFromXMLFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if name != "HomeNet" {
		t.Fatalf("got %q", name)
	}
}

func TestSecureWipeDirRemovesFilesAndContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "secret")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "nested", "profile.xml")
	if err := os.WriteFile(file, []byte(sampleWifiXML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SecureWipeDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed", dir)
	}
}

func TestWifiBackupNeverStagesPlaintext(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "module")
	exec := newFakeExec()
	exec.look["netsh"] = true
	var exportedDir string
	exec.run = func(name string, args []string) (core.Result, error) {
		if name != "netsh" {
			return core.Result{}, nil
		}
		folder := argValue(args, "folder=")
		if folder == "" {
			for _, a := range args {
				if strings.HasPrefix(a, "folder=") {
					folder = strings.TrimPrefix(a, "folder=")
				}
			}
		}
		exportedDir = folder
		if err := os.MkdirAll(folder, 0o700); err != nil {
			return core.Result{}, err
		}
		return core.Result{}, os.WriteFile(filepath.Join(folder, "Wi-Fi-HomeNet.xml"), []byte(sampleWifiXML), 0o600)
	}
	repo := &fakeRepo{}
	p := testParams(exec, repo, home)
	w := NewWifi()
	if err := w.Backup(ctxWith(p), dir); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if exportedDir == "" {
		t.Fatal("netsh export did not run")
	}
	if _, err := os.Stat(exportedDir); !os.IsNotExist(err) {
		t.Fatalf("secure export dir was not wiped: %s", exportedDir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.EqualFold(filepath.Ext(entry.Name()), ".xml") {
			t.Fatalf("plaintext XML leaked into staging: %s", entry.Name())
		}
	}
	requireFile(t, filepath.Join(dir, "wifi.json"))
	requireContains(t, filepath.Join(dir, "wifi.json"), "HomeNet")
	data, _ := os.ReadFile(filepath.Join(dir, "wifi.json"))
	if strings.Contains(string(data), "super-secret") {
		t.Fatal("wifi.json leaked the Wi-Fi passphrase")
	}
	if len(repo.BackupCalls) != 1 {
		t.Fatalf("expected one repository backup, got %d", len(repo.BackupCalls))
	}
	if !containsArg(repo.BackupCalls[0].Opts.Tags, "module=wifi") {
		t.Fatalf("missing wifi tag: %v", repo.BackupCalls[0].Opts.Tags)
	}
	if got := w.Snapshots(); len(got) != 1 || got[0] != "snap-1" {
		t.Fatalf("unexpected snapshots: %v", got)
	}
}

func TestWifiRestoreUsesSecureTempAndWipesIt(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "module")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "wifi.json"), `{"profiles":["HomeNet"],"count":1}`, time.Time{})
	exec := newFakeExec()
	exec.look["netsh"] = true
	var restoredTo string
	exec.run = func(name string, args []string) (core.Result, error) {
		return core.Result{}, nil
	}
	repo := &fakeRepo{RestoreFn: func(snapshotID, target string) error {
		restoredTo = target
		return os.WriteFile(filepath.Join(target, "Wi-Fi-HomeNet.xml"), []byte(sampleWifiXML), 0o600)
	}}
	p := testParams(exec, repo, home)
	p.Manifest = &core.Manifest{Modules: map[string]core.ModuleStatus{
		"wifi": {Name: "wifi", Success: true, Snapshots: []string{"snap-1"}},
	}}
	w := NewWifi()
	if err := w.Restore(ctxWith(p), dir, core.RestoreOpts{}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restoredTo == "" {
		t.Fatal("repository restore did not run")
	}
	if _, err := os.Stat(restoredTo); !os.IsNotExist(err) {
		t.Fatalf("restore temp dir was not wiped: %s", restoredTo)
	}
	calls := exec.callsFor("netsh")
	if len(calls) != 1 {
		t.Fatalf("expected one netsh add call, got %d", len(calls))
	}
	if !containsArg(calls[0].Args, "add") || !containsArg(calls[0].Args, "profile") {
		t.Fatalf("unexpected netsh args: %v", calls[0].Args)
	}
}
