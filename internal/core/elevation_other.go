//go:build !windows

package core

import "fmt"

// IsElevated is always true on non-Windows platforms (elevation is a Windows
// concept; this keeps shared runner logic from aborting in tests).
func IsElevated() bool { return true }

// RequestElevation is unsupported outside Windows.
func RequestElevation(args []string) error {
	return fmt.Errorf("UAC elevation is only available on Windows")
}

// EnsureElevation is a no-op on non-Windows platforms.
func EnsureElevation(needed bool) error { return nil }
