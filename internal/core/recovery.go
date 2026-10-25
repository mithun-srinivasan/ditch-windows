package core

import (
	"crypto/rand"
	"fmt"
)

// readRandom is replaceable in tests.
var readRandom = rand.Read

// RandomHex returns n random bytes encoded as hex.
func RandomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := readRandom(buf); err != nil {
		return "", err
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, b := range buf {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0xf]
	}
	return string(out), nil
}

// RecoveryKey generates a human-friendly recovery key. The key is a restic
// passphrase; it is never stored by ditch-windows.
func RecoveryKey() (string, error) {
	buf := make([]byte, 20)
	if _, err := readRandom(buf); err != nil {
		return "", fmt.Errorf("generate recovery key: %w", err)
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, 32)
	for i := range out {
		out[i] = alphabet[int(buf[i%len(buf)])%len(alphabet)]
	}
	groups := make([]byte, 0, 32+7)
	for i, c := range out {
		if i > 0 && i%4 == 0 {
			groups = append(groups, '-')
		}
		groups = append(groups, c)
	}
	return string(groups), nil
}
