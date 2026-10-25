//go:build !windows

package core

import "os/exec"

func configureCommand(cmd *exec.Cmd) {}
