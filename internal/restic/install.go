package restic

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mithun-srinivasan/ditch-windows/internal/core"
)

// DefaultVersion is the restic release that ditch-windows downloads when the
// user has no restic on PATH. Override with DITCH_RESTIC_VERSION.
const DefaultVersion = "0.19.1"

const defaultReleaseBase = "https://github.com/restic/restic/releases/download"

// maxDownload bounds how much we are willing to read from the network.
const maxDownload = 200 << 20 // 200 MiB

// ManagedDir is the per-user directory that holds a restic copy downloaded by
// ditch-windows. It keeps the tool self-contained without touching PATH.
func ManagedDir() string { return filepath.Join(core.AppDataDir(), "bin") }

// ManagedBinary is the path of the restic executable inside ManagedDir.
func ManagedBinary() string {
	name := "restic"
	if runtime.GOOS == "windows" {
		name = "restic.exe"
	}
	return filepath.Join(ManagedDir(), name)
}

// InstallOptions configures a restic download.
type InstallOptions struct {
	// Version is the restic release to fetch. "v" prefixes are accepted.
	Version string
	// BaseURL is the release download root. Defaults to GitHub Releases and
	// can be overridden with DITCH_RESTIC_DOWNLOAD_BASE.
	BaseURL string
	// GOOS and GOARCH default to the current platform. They exist for tests.
	GOOS   string
	GOARCH string
	// Client defaults to an HTTP client with a generous timeout.
	Client *http.Client
	// Status receives short human-readable progress lines. May be nil.
	Status func(string)
}

func (o InstallOptions) normalize() InstallOptions {
	if o.Version == "" {
		o.Version = os.Getenv("DITCH_RESTIC_VERSION")
	}
	if o.Version == "" {
		o.Version = DefaultVersion
	}
	o.Version = strings.TrimPrefix(strings.TrimSpace(o.Version), "v")
	if o.BaseURL == "" {
		o.BaseURL = os.Getenv("DITCH_RESTIC_DOWNLOAD_BASE")
	}
	if o.BaseURL == "" {
		o.BaseURL = defaultReleaseBase
	}
	o.BaseURL = strings.TrimRight(o.BaseURL, "/")
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 5 * time.Minute}
	}
	return o
}

func (o InstallOptions) status(format string, args ...any) {
	if o.Status != nil {
		o.Status(fmt.Sprintf(format, args...))
	}
}

// Install downloads, verifies, and unpacks restic into ManagedDir. The
// checksum is validated against the SHA256SUMS file published in the same
// release, and nothing is written until the archive has been verified.
func Install(ctx context.Context, opts InstallOptions) (string, error) {
	opts = opts.normalize()
	asset, err := assetName(opts.Version, opts.GOOS, opts.GOARCH)
	if err != nil {
		return "", err
	}
	releaseURL := fmt.Sprintf("%s/v%s", opts.BaseURL, opts.Version)
	archiveURL := releaseURL + "/" + asset
	sumsURL := releaseURL + "/SHA256SUMS"

	opts.status("downloading restic v%s (%s)", opts.Version, asset)
	data, err := fetch(ctx, opts.Client, archiveURL)
	if err != nil {
		return "", err
	}
	sums, err := fetch(ctx, opts.Client, sumsURL)
	if err != nil {
		return "", err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return "", fmt.Errorf("restic download failed checksum verification (expected %s, got %x)", want, got)
	}
	opts.status("checksum verified; installing to %s", ManagedDir())

	binary, err := extractBinary(data, opts.GOOS)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(ManagedDir(), 0o700); err != nil {
		return "", fmt.Errorf("create restic directory: %w", err)
	}
	tmp, err := os.CreateTemp(ManagedDir(), ".restic-*")
	if err != nil {
		return "", fmt.Errorf("stage restic binary: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("write restic binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("write restic binary: %w", err)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		cleanup()
		return "", fmt.Errorf("mark restic executable: %w", err)
	}
	target := ManagedBinary()
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return "", fmt.Errorf("install restic binary: %w", err)
	}
	return target, nil
}

// EnsureOptions controls EnsureBinary.
type EnsureOptions struct {
	// AllowDownload lets EnsureBinary fetch restic when it is missing.
	AllowDownload bool
	// Status receives progress lines. May be nil.
	Status func(string)
	// Install overrides download settings.
	Install InstallOptions
}

// EnsureBinary returns a usable restic path, downloading restic into ManagedDir
// when it is missing and AllowDownload is set.
func EnsureBinary(ctx context.Context, explicit string, opts EnsureOptions) (string, error) {
	if binary, err := FindBinary(explicit); err == nil {
		return binary, nil
	}
	if !opts.AllowDownload {
		return "", fmt.Errorf("restic was not found; %s", InstallHint)
	}
	installOpts := opts.Install
	installOpts.Status = opts.Status
	return Install(ctx, installOpts)
}

// assetName maps a platform to the release archive name used by restic.
func assetName(version, goos, goarch string) (string, error) {
	switch goos {
	case "windows", "linux", "darwin", "freebsd", "openbsd", "netbsd":
	default:
		return "", fmt.Errorf("automatic restic install is not supported on %s; %s", goos, InstallHint)
	}
	arch := goarch
	if goos == "windows" && arch == "arm64" {
		// restic publishes no Windows/arm64 archive. The amd64 build runs
		// under Windows 11's x64 emulation.
		arch = "amd64"
	}
	switch arch {
	case "amd64", "arm64", "386", "arm":
	default:
		return "", fmt.Errorf("automatic restic install is not supported on %s/%s; %s", goos, goarch, InstallHint)
	}
	return fmt.Sprintf("restic_%s_%s_%s.zip", version, goos, arch), nil
}

func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "ditch-windows")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if int64(len(data)) > maxDownload {
		return nil, fmt.Errorf("download %s: response exceeds %d bytes", url, maxDownload)
	}
	return data, nil
}

// checksumFor finds the SHA-256 entry for asset in a restic SHA256SUMS file.
func checksumFor(sums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("restic release did not publish a checksum for %s", asset)
}

func extractBinary(data []byte, goos string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("read restic archive: %w", err)
	}
	want := "restic"
	ext := ""
	if goos == "windows" {
		want = "restic.exe"
		ext = ".exe"
	}
	// restic archives ship either `restic(.exe)` or a versioned name such as
	// `restic_0.19.1_windows_amd64.exe`, so accept both.
	var fallback *zip.File
	for _, file := range zr.File {
		if file.FileInfo().IsDir() {
			continue
		}
		base := path.Base(filepath.ToSlash(file.Name))
		if base == want {
			return readZipFile(file, want)
		}
		if fallback == nil && strings.HasPrefix(base, "restic") && strings.HasSuffix(base, ext) {
			fallback = file
		}
	}
	if fallback != nil {
		return readZipFile(fallback, fallback.Name)
	}
	return nil, fmt.Errorf("%s was not found in the restic archive", want)
}

func readZipFile(file *zip.File, label string) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s in restic archive: %w", label, err)
	}
	defer rc.Close()
	binary, err := io.ReadAll(io.LimitReader(rc, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("read %s from restic archive: %w", label, err)
	}
	if int64(len(binary)) > maxDownload {
		return nil, fmt.Errorf("%s in restic archive is unexpectedly large", label)
	}
	return binary, nil
}
