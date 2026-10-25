//go:build windows

package core

import (
	"os/exec"
	"syscall"
)

// configureCommand prevents console windows from flashing while the TUI is
// running. It is a no-op on non-Windows platforms.
func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
