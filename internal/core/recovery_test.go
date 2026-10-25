package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRandomHex(t *testing.T) {
	old := readRandom
	readRandom = func(buf []byte) (int, error) {
		for i := range buf {
			buf[i] = byte(i)
		}
		return len(buf), nil
	}
	t.Cleanup(func() { readRandom = old })

	got, err := RandomHex(3)
	if err != nil {
		t.Fatal(err)
	}
	if got != "000102" {
		t.Fatalf("got %q", got)
	}
}

func TestRecoveryKeyFormatAndAlphabet(t *testing.T) {
	old := readRandom
	readRandom = func(buf []byte) (int, error) {
		for i := range buf {
			buf[i] = byte(i * 7)
		}
		return len(buf), nil
	}
	t.Cleanup(func() { readRandom = old })

	key, err := RecoveryKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 39 {
		t.Fatalf("unexpected key length %d: %q", len(key), key)
	}
	parts := strings.Split(key, "-")
	if len(parts) != 8 {
		t.Fatalf("expected 8 groups, got %d: %q", len(parts), key)
	}
	for _, part := range parts {
		if len(part) != 4 {
			t.Fatalf("unexpected group %q", part)
		}
		for _, r := range part {
			if strings.ContainsRune("IO01", r) {
				t.Fatalf("ambiguous character %q in key %q", r, key)
			}
			if !strings.ContainsRune("ABCDEFGHJKLMNPQRSTUVWXYZ23456789", r) {
				t.Fatalf("invalid key character %q", r)
			}
		}
	}
}

func TestRecoveryKeyPropagatesRandomError(t *testing.T) {
	old := readRandom
	boom := errors.New("entropy unavailable")
	readRandom = func([]byte) (int, error) { return 0, boom }
	t.Cleanup(func() { readRandom = old })
	if _, err := RecoveryKey(); !errors.Is(err, boom) {
		t.Fatalf("expected entropy error, got %v", err)
	}
}

func TestJSONLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWriterLogger(&buf).WithContext("winget")
	logger.Infof("hello %s", "world")
	logger.Warnf("careful")
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d: %q", len(lines), buf.String())
	}
	for _, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid JSON log line %q: %v", line, err)
		}
		if entry["context"] != "winget" || entry["time"] == "" {
			t.Fatalf("missing structured fields: %v", entry)
		}
	}
	if !strings.Contains(buf.String(), `"level":"INFO"`) || !strings.Contains(buf.String(), `"message":"careful"`) {
		t.Fatalf("unexpected log output: %s", buf.String())
	}
}
