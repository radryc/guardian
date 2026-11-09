package opentofu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner executes a command in a workspace directory. It is injectable so the
// backend can be tested without a tofu binary or a GCP project.
type Runner interface {
	Run(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr []byte, err error)
}

// ExitError carries the process exit code. Plan uses it to distinguish "no
// changes" (0) from "drift detected" (2).
type ExitError struct {
	Code   int
	Stderr string
	Err    error
}

func (e *ExitError) Error() string {
	if e == nil {
		return ""
	}
	if e.Stderr != "" {
		return fmt.Sprintf("%s exited with code %d: %s", e.Err, e.Code, e.Stderr)
	}
	return fmt.Sprintf("%s exited with code %d", e.Err, e.Code)
}

func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode returns the process exit code behind err, or -1 when unknown.
func ExitCode(err error) int {
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	if err == nil {
		return 0
	}
	return -1
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.Bytes(), stderr.Bytes(), &ExitError{
				Code:   exitErr.ExitCode(),
				Stderr: strings.TrimSpace(stderr.String()),
				Err:    err,
			}
		}
		return stdout.Bytes(), stderr.Bytes(), err
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}
