//go:build windows

package core

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// IsElevated reports whether the current process has administrator rights.
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// RequestElevation starts the current executable again with the given
// arguments under a UAC "runas" prompt. It returns nil once the child has been
// launched; the caller should exit so the child can take over.
func RequestElevation(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	exePtr, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	verbPtr, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, syscall.EscapeArg(a))
	}
	argPtr, err := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	var cwdPtr *uint16
	if cwd != "" {
		cwdPtr, _ = windows.UTF16PtrFromString(cwd)
	}
	if err := windows.ShellExecute(0, verbPtr, exePtr, argPtr, cwdPtr, 1); err != nil {
		return fmt.Errorf("elevation was cancelled or failed: %w", err)
	}
	return nil
}

// EnsureElevation relaunches with UAC when needed. It returns ErrRelaunched
// after successfully starting an elevated child.
func EnsureElevation(needed bool) error {
	if !needed || IsElevated() {
		return nil
	}
	fmt.Fprintln(os.Stderr, "Administrator privileges are required. Requesting elevation via UAC...")
	if err := RequestElevation(os.Args[1:]); err != nil {
		return err
	}
	return ErrRelaunched
}
