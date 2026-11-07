package restic

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testArchive(t *testing.T, name, contents string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(contents)); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func sumsFor(asset string, data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x  %s\n", sum, asset)
}

func newReleaseServer(t *testing.T, version, asset string, archive []byte, sums string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v"+version+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/v"+version+"/SHA256SUMS", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sums))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestInstallDownloadsVerifiesAndUnpacks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DITCH_WINDOWS_HOME", home)
	t.Setenv("DITCH_RESTIC_BINARY", "")

	const version = "1.2.3"
	asset := "restic_" + version + "_windows_amd64.zip"
	archive := testArchive(t, "restic_"+version+"_windows_amd64.exe", "fake-restic-binary")
	srv := newReleaseServer(t, version, asset, archive, sumsFor(asset, archive))

	var statuses []string
	path, err := Install(context.Background(), InstallOptions{
		Version: version,
		BaseURL: srv.URL,
		GOOS:    "windows",
		GOARCH:  "amd64",
		Client:  srv.Client(),
		Status:  func(line string) { statuses = append(statuses, line) },
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if want := filepath.Join(home, "bin", "restic.exe"); path != want {
		t.Fatalf("Install path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installed restic: %v", err)
	}
	if string(data) != "fake-restic-binary" {
		t.Fatalf("installed contents = %q", data)
	}
	if len(statuses) == 0 {
		t.Fatal("expected progress messages")
	}
}

func TestInstallRejectsBadChecksum(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DITCH_WINDOWS_HOME", home)
	t.Setenv("DITCH_RESTIC_BINARY", "")

	const version = "1.2.3"
	asset := "restic_" + version + "_windows_amd64.zip"
	archive := testArchive(t, "restic.exe", "fake")
	srv := newReleaseServer(t, version, asset, archive,
		strings.Repeat("0", 64)+"  "+asset+"\n")

	_, err := Install(context.Background(), InstallOptions{
		Version: version,
		BaseURL: srv.URL,
		GOOS:    "windows",
		GOARCH:  "amd64",
		Client:  srv.Client(),
	})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Install error = %v, want checksum failure", err)
	}
	if _, statErr := os.Stat(ManagedBinary()); !os.IsNotExist(statErr) {
		t.Fatalf("binary should not exist after failed verification: %v", statErr)
	}
}

func TestInstallReportsMissingChecksumEntry(t *testing.T) {
	const version = "1.2.3"
	asset := "restic_" + version + "_windows_amd64.zip"
	archive := testArchive(t, "restic.exe", "fake")
	srv := newReleaseServer(t, version, asset, archive, sumsFor("other.zip", archive))

	_, err := Install(context.Background(), InstallOptions{
		Version: version,
		BaseURL: srv.URL,
		GOOS:    "windows",
		GOARCH:  "amd64",
		Client:  srv.Client(),
	})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Install error = %v, want missing checksum failure", err)
	}
}

func TestAssetName(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         string
		wantErr      bool
	}{
		{"windows", "amd64", "restic_0.19.1_windows_amd64.zip", false},
		{"windows", "arm64", "restic_0.19.1_windows_amd64.zip", false},
		{"linux", "arm64", "restic_0.19.1_linux_arm64.zip", false},
		{"plan9", "amd64", "", true},
		{"linux", "mips", "", true},
	}
	for _, tt := range tests {
		got, err := assetName("0.19.1", tt.goos, tt.goarch)
		if tt.wantErr {
			if err == nil {
				t.Errorf("assetName(%s/%s) = %q, want error", tt.goos, tt.goarch, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("assetName(%s/%s): %v", tt.goos, tt.goarch, err)
			continue
		}
		if got != tt.want {
			t.Errorf("assetName(%s/%s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	sums := []byte(strings.Repeat("a", 64) + "  restic_1_windows_amd64.zip\n" +
		strings.Repeat("b", 64) + " *restic_1_linux_amd64.zip\n")
	got, err := checksumFor(sums, "restic_1_linux_amd64.zip")
	if err != nil {
		t.Fatalf("checksumFor: %v", err)
	}
	if got != strings.Repeat("b", 64) {
		t.Fatalf("checksumFor = %q", got)
	}
	if _, err := checksumFor(sums, "missing.zip"); err == nil {
		t.Fatal("expected error for missing entry")
	}
}

func TestFindBinaryFindsManagedCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DITCH_WINDOWS_HOME", home)
	t.Setenv("DITCH_RESTIC_BINARY", "")

	path := ManagedBinary()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindBinary("")
	if err != nil {
		t.Fatalf("FindBinary: %v", err)
	}
	if got != path {
		t.Fatalf("FindBinary = %q, want managed copy %q", got, path)
	}
}

func TestEnsureBinaryWithoutDownloadFails(t *testing.T) {
	t.Setenv("DITCH_WINDOWS_HOME", t.TempDir())
	t.Setenv("DITCH_RESTIC_BINARY", "")
	t.Setenv("PATH", t.TempDir())

	_, err := EnsureBinary(context.Background(), "", EnsureOptions{})
	if err == nil || !strings.Contains(err.Error(), "restic was not found") {
		t.Fatalf("EnsureBinary error = %v", err)
	}
}
