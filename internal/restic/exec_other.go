//go:build !windows

package restic

import "os/exec"

func configureCommand(cmd *exec.Cmd) {}
