package systemd

import "context"

// MockSystemctlRunner is a test double for SystemctlRunner
type MockSystemctlRunner struct {
	DaemonReloadCalled bool
	DaemonReloadErr    error

	EnableCalled bool
	EnableUnit   string
	EnableErr    error

	DisableCalled bool
	DisableUnit   string
	DisableErr    error

	StartCalled bool
	StartUnit   string
	StartErr    error

	StopCalled bool
	StopUnit   string
	StopErr    error

	IsActiveCalled bool
	IsActiveUnit   string
	IsActiveResult bool
	IsActiveErr    error

	IsEnabledCalled bool
	IsEnabledUnit   string
	IsEnabledResult bool
	IsEnabledErr    error
}

func (m *MockSystemctlRunner) DaemonReload() error {
	m.DaemonReloadCalled = true
	return m.DaemonReloadErr
}

func (m *MockSystemctlRunner) Enable(unit string) error {
	m.EnableCalled = true
	m.EnableUnit = unit
	return m.EnableErr
}

func (m *MockSystemctlRunner) Disable(unit string) error {
	m.DisableCalled = true
	m.DisableUnit = unit
	return m.DisableErr
}

func (m *MockSystemctlRunner) Start(unit string) error {
	m.StartCalled = true
	m.StartUnit = unit
	return m.StartErr
}

func (m *MockSystemctlRunner) Stop(unit string) error {
	m.StopCalled = true
	m.StopUnit = unit
	return m.StopErr
}

func (m *MockSystemctlRunner) IsActive(unit string) (bool, error) {
	m.IsActiveCalled = true
	m.IsActiveUnit = unit
	return m.IsActiveResult, m.IsActiveErr
}

func (m *MockSystemctlRunner) IsEnabled(unit string) (bool, error) {
	m.IsEnabledCalled = true
	m.IsEnabledUnit = unit
	return m.IsEnabledResult, m.IsEnabledErr
}

// MockContextSystemctlRunner is a test double for ContextSystemctlRunner. It
// embeds MockSystemctlRunner so it also satisfies the legacy SystemctlRunner
// interface, but Manager prefers the Context methods below when a runner
// implements both — exercising that preference is this type's purpose.
// MockSystemctlRunner on its own (which does not implement
// ContextSystemctlRunner) is what pins the legacy fallback path.
type MockContextSystemctlRunner struct {
	MockSystemctlRunner

	DaemonReloadContextCalled bool
	DaemonReloadContextCtx    context.Context
	DaemonReloadContextErr    error

	EnableContextCalled bool
	EnableContextUnit   string
	EnableContextErr    error

	DisableContextCalled bool
	DisableContextUnit   string
	DisableContextErr    error

	StartContextCalled bool
	StartContextUnit   string
	StartContextErr    error

	StopContextCalled bool
	StopContextUnit   string
	StopContextErr    error

	IsActiveContextCalled bool
	IsActiveContextUnit   string
	IsActiveContextResult bool
	IsActiveContextErr    error

	IsEnabledContextCalled bool
	IsEnabledContextUnit   string
	IsEnabledContextResult bool
	IsEnabledContextErr    error
}

func (m *MockContextSystemctlRunner) DaemonReloadContext(ctx context.Context) error {
	m.DaemonReloadContextCalled = true
	m.DaemonReloadContextCtx = ctx
	return m.DaemonReloadContextErr
}

func (m *MockContextSystemctlRunner) EnableContext(ctx context.Context, unit string) error {
	m.EnableContextCalled = true
	m.EnableContextUnit = unit
	return m.EnableContextErr
}

func (m *MockContextSystemctlRunner) DisableContext(ctx context.Context, unit string) error {
	m.DisableContextCalled = true
	m.DisableContextUnit = unit
	return m.DisableContextErr
}

func (m *MockContextSystemctlRunner) StartContext(ctx context.Context, unit string) error {
	m.StartContextCalled = true
	m.StartContextUnit = unit
	return m.StartContextErr
}

func (m *MockContextSystemctlRunner) StopContext(ctx context.Context, unit string) error {
	m.StopContextCalled = true
	m.StopContextUnit = unit
	return m.StopContextErr
}

func (m *MockContextSystemctlRunner) IsActiveContext(ctx context.Context, unit string) (bool, error) {
	m.IsActiveContextCalled = true
	m.IsActiveContextUnit = unit
	return m.IsActiveContextResult, m.IsActiveContextErr
}

func (m *MockContextSystemctlRunner) IsEnabledContext(ctx context.Context, unit string) (bool, error) {
	m.IsEnabledContextCalled = true
	m.IsEnabledContextUnit = unit
	return m.IsEnabledContextResult, m.IsEnabledContextErr
}
