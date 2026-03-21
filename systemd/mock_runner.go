package systemd

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
