package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Result is the captured result of a child process.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Combined returns stdout followed by stderr. It is convenient for error
// messages, but callers that parse output should use Stdout directly.
func (r Result) Combined() string {
	return string(r.Stdout) + string(r.Stderr)
}

// Exec runs external commands. It is injected into modules so tests never
// shell out to winget, netsh, dism, or restic.
type Exec interface {
	// Run executes name with args and waits for it to finish. A non-zero exit
	// status is returned as a non-nil error together with the captured result.
	Run(ctx context.Context, name string, args ...string) (Result, error)
	// LookPath resolves a command in PATH.
	LookPath(name string) (string, error)
}

// OSExec is the production Exec implementation.
type OSExec struct {
	// ExtraEnv is appended to the process environment. It is primarily useful
	// for tests and for passing repository credentials to restic.
	ExtraEnv []string
}

// NewDefaultExec returns an Exec backed by os/exec.
func NewDefaultExec() Exec { return &OSExec{} }

// Run implements Exec.
func (e *OSExec) Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), e.ExtraEnv...)
	configureCommand(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	} else if err != nil {
		res.ExitCode = -1
	}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return res, fmt.Errorf("%s: command not found", name)
		}
		return res, fmt.Errorf("%s: %w", name, err)
	}
	return res, nil
}

// LookPath implements Exec.
func (e *OSExec) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}
