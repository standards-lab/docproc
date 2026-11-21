//go:build !linux

package tool

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

var errLinuxOnly = errors.New("the exec runner runs on Linux only")

// NewExec is the exec runner's stand-in on a platform other than Linux, where the runner's
// process groups, parent-death signal, and prlimit don't exist: it validates spec and returns an
// error naming the platform.
func NewExec(_ context.Context, spec Spec) (*Tool, error) {
	name := filepath.Base(spec.Program)
	if _, err := spec.validate(name); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("tool: %s: %w", name, errLinuxOnly)
}

type execConfig struct {
	name      string
	path      string
	killAfter time.Duration
}

func execRun(cfg execConfig) runFunc {
	return func(context.Context, Invocation) error {
		return fmt.Errorf("tool: %s: %w", cfg.name, errLinuxOnly)
	}
}
