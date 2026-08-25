package systemd

import (
	"context"
	"fmt"
	"os/exec"
)

// SystemctlRunner executes systemctl commands
type SystemctlRunner interface {
	DaemonReload() error
	Enable(unit string) error
	Disable(unit string) error
	Start(unit string) error
	Stop(unit string) error
	IsActive(unit string) (bool, error)
	IsEnabled(unit string) (bool, error)
}

// ContextSystemctlRunner is implemented by runners that support canceling an
// in-flight systemctl command via context.Context. Manager prefers it over
// the legacy SystemctlRunner methods when the injected runner implements it,
// only checking ctx.Err() before falling back to the legacy methods
// otherwise — so existing external SystemctlRunner implementations remain
// source-compatible without gaining in-flight cancellation.
type ContextSystemctlRunner interface {
	DaemonReloadContext(ctx context.Context) error
	EnableContext(ctx context.Context, unit string) error
	DisableContext(ctx context.Context, unit string) error
	StartContext(ctx context.Context, unit string) error
	StopContext(ctx context.Context, unit string) error
	IsActiveContext(ctx context.Context, unit string) (bool, error)
	IsEnabledContext(ctx context.Context, unit string) (bool, error)
}

// DefaultSystemctlRunner executes real systemctl commands. It implements
// ContextSystemctlRunner so an in-flight command is killed promptly when its
// context is canceled or expires; the legacy methods delegate to the
// context-aware ones with context.Background().
type DefaultSystemctlRunner struct{}

var (
	_ SystemctlRunner        = (*DefaultSystemctlRunner)(nil)
	_ ContextSystemctlRunner = (*DefaultSystemctlRunner)(nil)
)

func (r *DefaultSystemctlRunner) DaemonReload() error {
	return r.DaemonReloadContext(context.Background())
}

func (r *DefaultSystemctlRunner) DaemonReloadContext(ctx context.Context) error {
	return runSystemctl(ctx, "daemon-reload")
}

func (r *DefaultSystemctlRunner) Enable(unit string) error {
	return r.EnableContext(context.Background(), unit)
}

func (r *DefaultSystemctlRunner) EnableContext(ctx context.Context, unit string) error {
	return runSystemctl(ctx, "enable", unit)
}

func (r *DefaultSystemctlRunner) Disable(unit string) error {
	return r.DisableContext(context.Background(), unit)
}

func (r *DefaultSystemctlRunner) DisableContext(ctx context.Context, unit string) error {
	return runSystemctl(ctx, "disable", unit)
}

func (r *DefaultSystemctlRunner) Start(unit string) error {
	return r.StartContext(context.Background(), unit)
}

func (r *DefaultSystemctlRunner) StartContext(ctx context.Context, unit string) error {
	return runSystemctl(ctx, "start", unit)
}

func (r *DefaultSystemctlRunner) Stop(unit string) error {
	return r.StopContext(context.Background(), unit)
}

func (r *DefaultSystemctlRunner) StopContext(ctx context.Context, unit string) error {
	return runSystemctl(ctx, "stop", unit)
}

func (r *DefaultSystemctlRunner) IsActive(unit string) (bool, error) {
	return r.IsActiveContext(context.Background(), unit)
}

func (r *DefaultSystemctlRunner) IsActiveContext(ctx context.Context, unit string) (bool, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", unit)
	err := cmd.Run()
	if err != nil {
		// A context cancellation/expiry that killed the process takes
		// priority over "signal: killed" and over the exit-code-3 =
		// inactive convention below: neither should be mistaken for a
		// clean inactive result.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		// Exit code 3 means inactive, not an error
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 3 {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

func (r *DefaultSystemctlRunner) IsEnabled(unit string) (bool, error) {
	return r.IsEnabledContext(context.Background(), unit)
}

func (r *DefaultSystemctlRunner) IsEnabledContext(ctx context.Context, unit string) (bool, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "is-enabled", unit)
	err := cmd.Run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		// Exit code 1 means disabled/not-found, not an error
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

// runSystemctl executes a systemctl command with the given arguments,
// interrupting it if ctx is canceled or expires. When that happens, the
// process-kill error (typically "signal: killed") is discarded in favor of
// ctx.Err(), so callers can match the result with errors.Is against
// context.Canceled or context.DeadlineExceeded.
func runSystemctl(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if len(args) > 0 {
			return fmt.Errorf("systemctl %s failed: %w", args[0], err)
		}
		return fmt.Errorf("systemctl failed: %w", err)
	}
	return nil
}
