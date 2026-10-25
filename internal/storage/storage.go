// Package storage turns user-facing storage choices into restic repository
// URLs. restic itself is the only component that touches files or cloud APIs.
package storage

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// Kind identifies a storage backend.
type Kind string

const (
	KindLocal  Kind = "local"
	KindUSB    Kind = "usb"
	KindS3     Kind = "s3"
	KindRclone Kind = "rclone"
)

// Backend describes where a restic repository lives.
type Backend interface {
	Kind() Kind
	RepoURL() string
	Description() string
	Validate() error
}

// Parse converts a user-supplied repository location into a backend.
//
// Accepted forms:
//
//	C:\Backups\ditch          local path
//	local:D:\Backups\ditch    local path
//	usb:E:                    E:\ditch-windows-repo
//	usb:E:\backups            E:\backups
//	s3:s3.amazonaws.com/bucket/prefix
//	s3://bucket/prefix
//	rclone:gdrive:ditch
func Parse(spec string) (Backend, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("repository URL is empty")
	}
	lower := strings.ToLower(spec)
	switch {
	case strings.HasPrefix(lower, "local:"):
		p := spec[len("local:"):]
		if p == "" {
			return nil, errors.New("local repository path is empty")
		}
		return &Local{Path: filepath.Clean(p)}, nil
	case strings.HasPrefix(lower, "usb:"):
		return parseUSB(spec[len("usb:"):])
	case strings.HasPrefix(lower, "s3:"):
		return parseS3(spec)
	case strings.HasPrefix(lower, "rclone:"):
		remote := spec[len("rclone:"):]
		if !strings.Contains(remote, ":") {
			return nil, fmt.Errorf("rclone repository %q must look like rclone:remote:path", spec)
		}
		return &Rclone{Remote: remote}, nil
	}
	if isDriveRoot(spec) {
		return &USB{Drive: spec, Subdir: "ditch-windows-repo"}, nil
	}
	if isWindowsDrivePath(spec) {
		// Explicit full path on a drive is treated as local storage; it does not
		// imply removable media.
		return &Local{Path: filepath.Clean(spec)}, nil
	}
	return &Local{Path: filepath.Clean(spec)}, nil
}

func parseUSB(rest string) (Backend, error) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil, errors.New("usb repository needs a drive letter, e.g. usb:E:")
	}
	if isDriveRoot(rest) {
		return &USB{Drive: strings.ToUpper(rest[:1]) + ":", Subdir: "ditch-windows-repo"}, nil
	}
	if isWindowsDrivePath(rest) {
		return &USB{Target: filepath.Clean(rest)}, nil
	}
	return nil, fmt.Errorf("usb repository %q must start with a drive letter", rest)
}

func parseS3(spec string) (Backend, error) {
	rest := spec[len("s3:"):]
	if rest == "" {
		return nil, errors.New("s3 repository URL is empty")
	}
	if strings.HasPrefix(rest, "//") {
		u, err := url.Parse("s3:" + rest)
		if err != nil {
			return nil, fmt.Errorf("invalid s3 URL: %w", err)
		}
		if strings.TrimSpace(u.Host) == "" {
			return nil, errors.New("s3 URL must include a bucket")
		}
		return &S3{
			Bucket:   u.Host,
			Prefix:   strings.Trim(u.Path, "/"),
			Endpoint: "s3.amazonaws.com",
		}, nil
	}
	// restic-native form: s3:endpoint/bucket/prefix. The endpoint may carry an
	// http(s) scheme, e.g. s3:https://minio.example.com:9000/bucket/prefix.
	scheme := ""
	switch {
	case strings.HasPrefix(rest, "https://"):
		scheme, rest = "https://", rest[len("https://"):]
	case strings.HasPrefix(rest, "http://"):
		scheme, rest = "http://", rest[len("http://"):]
	}
	parts := strings.SplitN(rest, "/", 2)
	if strings.TrimSpace(parts[0]) == "" || len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		return nil, errors.New("s3 repository must include an endpoint and bucket")
	}
	b := &S3{Endpoint: scheme + parts[0]}
	segs := strings.SplitN(parts[1], "/", 2)
	b.Bucket = segs[0]
	if len(segs) > 1 {
		b.Prefix = segs[1]
	}
	return b, nil
}

func isDriveRoot(s string) bool {
	return len(s) == 2 && s[1] == ':' && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z'))
}

func isWindowsDrivePath(s string) bool {
	return len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/')
}

// Local is a repository on a local filesystem.
type Local struct{ Path string }

func (l *Local) Kind() Kind          { return KindLocal }
func (l *Local) RepoURL() string     { return l.Path }
func (l *Local) Description() string { return "Local folder: " + l.Path }
func (l *Local) Validate() error {
	if strings.TrimSpace(l.Path) == "" || l.Path == "." {
		return errors.New("local repository path is empty")
	}
	return nil
}

// USB is a repository on removable media. Target may be set for an explicit
// path; otherwise it is Drive + Subdir.
type USB struct {
	Drive  string
	Subdir string
	Target string
}

func (u *USB) Kind() Kind { return KindUSB }
func (u *USB) RepoURL() string {
	if u.Target != "" {
		return u.Target
	}
	sub := u.Subdir
	if sub == "" {
		sub = "ditch-windows-repo"
	}
	return strings.TrimRight(u.Drive, `\/`) + "/" + strings.TrimLeft(sub, `\/`)
}
func (u *USB) Description() string { return "USB / removable drive: " + u.RepoURL() }
func (u *USB) Validate() error {
	if u.Target != "" {
		return nil
	}
	if !isDriveRoot(u.Drive) {
		return fmt.Errorf("invalid USB drive %q", u.Drive)
	}
	return nil
}

// S3 is an S3-compatible repository. Credentials come from the standard AWS
// environment variables or ~/.aws/credentials, never from ditch-windows.
type S3 struct {
	Bucket   string
	Prefix   string
	Endpoint string
}

func (s *S3) Kind() Kind { return KindS3 }
func (s *S3) RepoURL() string {
	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = "s3.amazonaws.com"
	}
	u := "s3:" + endpoint + "/" + s.Bucket
	if s.Prefix != "" {
		u += "/" + strings.TrimPrefix(s.Prefix, "/")
	}
	return u
}
func (s *S3) Description() string { return "S3 repository: " + s.RepoURL() }
func (s *S3) Validate() error {
	if strings.TrimSpace(s.Bucket) == "" {
		return errors.New("s3 bucket is empty")
	}
	return nil
}

// Rclone is a repository exposed by an rclone remote.
type Rclone struct{ Remote string }

func (r *Rclone) Kind() Kind          { return KindRclone }
func (r *Rclone) RepoURL() string     { return "rclone:" + r.Remote }
func (r *Rclone) Description() string { return "rclone remote: " + r.Remote }
func (r *Rclone) Validate() error {
	if !strings.Contains(r.Remote, ":") {
		return errors.New("rclone remote must look like remote:path")
	}
	return nil
}

var credentialRE = regexp.MustCompile(`(?i)(://[^/@:]+:)[^/@]+(@)`)

// SanitizeRepoURL redacts credentials that a user may have embedded in a
// repository URL. It is safe to show in logs and manifests.
func SanitizeRepoURL(repo string) string {
	return credentialRE.ReplaceAllString(repo, "${1}REDACTED${2}")
}

// BackendFor is a convenience wrapper around Parse that returns the restic URL.
func BackendFor(spec string) (Backend, string, error) {
	b, err := Parse(spec)
	if err != nil {
		return nil, "", err
	}
	if err := b.Validate(); err != nil {
		return nil, "", err
	}
	return b, b.RepoURL(), nil
}
