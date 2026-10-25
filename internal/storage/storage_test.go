package storage

import (
	"strings"
	"testing"
)

func TestParseLocal(t *testing.T) {
	for _, spec := range []string{"/srv/ditch", "local:/srv/ditch", `C:\Backups\ditch`} {
		backend, err := Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		if backend.Kind() != KindLocal {
			t.Fatalf("Parse(%q) kind = %q, want local", spec, backend.Kind())
		}
		if backend.RepoURL() == "" {
			t.Fatalf("Parse(%q) returned an empty repo URL", spec)
		}
	}
}

func TestParseUSB(t *testing.T) {
	backend, err := Parse("usb:E:")
	if err != nil {
		t.Fatal(err)
	}
	usb, ok := backend.(*USB)
	if !ok {
		t.Fatalf("type = %T, want *USB", backend)
	}
	if got, want := usb.RepoURL(), "E:/ditch-windows-repo"; got != want {
		t.Fatalf("RepoURL = %q, want %q", got, want)
	}
	if err := usb.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	backend, err = Parse(`usb:e:\backups`)
	if err != nil {
		t.Fatal(err)
	}
	if backend.Kind() != KindUSB {
		t.Fatalf("kind = %q, want usb", backend.Kind())
	}

	backend, err = Parse("C:")
	if err != nil {
		t.Fatal(err)
	}
	if backend.Kind() != KindUSB {
		t.Fatalf("bare drive kind = %q, want usb", backend.Kind())
	}
}

func TestParseS3(t *testing.T) {
	backend, err := Parse("s3://my-bucket/ditch/prefix")
	if err != nil {
		t.Fatal(err)
	}
	s3, ok := backend.(*S3)
	if !ok {
		t.Fatalf("type = %T, want *S3", backend)
	}
	if s3.Bucket != "my-bucket" || s3.Prefix != "ditch/prefix" || s3.Endpoint != "s3.amazonaws.com" {
		t.Fatalf("unexpected s3 backend: %+v", s3)
	}
	if got, want := s3.RepoURL(), "s3:s3.amazonaws.com/my-bucket/ditch/prefix"; got != want {
		t.Fatalf("RepoURL = %q, want %q", got, want)
	}

	backend, err = Parse("s3:https://minio.example.com:9000/bucket/prefix")
	if err != nil {
		t.Fatal(err)
	}
	s3 = backend.(*S3)
	if s3.Endpoint != "https://minio.example.com:9000" || s3.Bucket != "bucket" || s3.Prefix != "prefix" {
		t.Fatalf("unexpected native s3 parse: %+v", s3)
	}
}

func TestParseRclone(t *testing.T) {
	backend, err := Parse("rclone:gdrive:ditch-windows")
	if err != nil {
		t.Fatal(err)
	}
	r, ok := backend.(*Rclone)
	if !ok {
		t.Fatalf("type = %T, want *Rclone", backend)
	}
	if r.Remote != "gdrive:ditch-windows" {
		t.Fatalf("Remote = %q", r.Remote)
	}
	if got, want := backend.RepoURL(), "rclone:gdrive:ditch-windows"; got != want {
		t.Fatalf("RepoURL = %q, want %q", got, want)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []string{
		"",
		"local:",
		"usb:",
		"usb:not-a-drive",
		"s3:",
		"s3:///no-bucket",
		"s3:onlyendpoint",
		"rclone:nocolon",
	}
	for _, spec := range cases {
		if _, err := Parse(spec); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", spec)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := (&Local{Path: ""}).Validate(); err == nil {
		t.Error("empty local path should fail validation")
	}
	if err := (&USB{Drive: "not-a-drive"}).Validate(); err == nil {
		t.Error("invalid USB drive should fail validation")
	}
	if err := (&S3{}).Validate(); err == nil {
		t.Error("empty S3 bucket should fail validation")
	}
	if err := (&Rclone{Remote: "plain"}).Validate(); err == nil {
		t.Error("rclone remote without colon should fail validation")
	}
}

func TestBackendFor(t *testing.T) {
	backend, repo, err := BackendFor("rclone:gdrive:ditch")
	if err != nil {
		t.Fatal(err)
	}
	if repo != "rclone:gdrive:ditch" || backend.Kind() != KindRclone {
		t.Fatalf("unexpected result: %q %q", backend.Kind(), repo)
	}
	if _, _, err := BackendFor(" "); err == nil {
		t.Fatal("empty spec should fail")
	}
}

func TestSanitizeRepoURL(t *testing.T) {
	in := "sftp://alice:hunter2@example.com/ditch"
	got := SanitizeRepoURL(in)
	if strings.Contains(got, "hunter2") {
		t.Fatalf("SanitizeRepoURL leaked a password: %q", got)
	}
	if !strings.Contains(got, "alice:REDACTED@example.com") {
		t.Fatalf("unexpected sanitized URL: %q", got)
	}
	plain := "C:/Backups/ditch"
	if SanitizeRepoURL(plain) != plain {
		t.Fatalf("plain path should be unchanged")
	}
}
