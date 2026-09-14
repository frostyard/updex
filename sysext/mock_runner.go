package sysext

import "github.com/frostyard/updex/config"

// MockRunner is a test double for SysextRunner. It implements
// PathSysextRunner too, so an updex.Client links through
// LinkToSysextAt with the directory the client captured at construction —
// exactly like DefaultRunner and unlike a runner that predates
// PathSysextRunner, which the SDK now refuses rather than silently
// redirecting to the package-global SysextDir.
type MockRunner struct {
	RefreshCalled bool
	RefreshErr    error
	MergeCalled   bool
	MergeErr      error
	UnmergeCalled bool
	UnmergeErr    error
	// LinkToSysextCalled records a link through either entry point.
	LinkToSysextCalled bool
	LinkToSysextErr    error
	// LinkToSysextAtDir records the directory the last LinkToSysextAt call
	// was given. It stays empty when only the pathless LinkToSysext was
	// called, so a test can tell the two apart.
	LinkToSysextAtDir string
}

// MockRunner is a PathSysextRunner: a compile-time reminder that dropping
// LinkToSysextAt would turn every client using it into a refused legacy
// runner.
var _ PathSysextRunner = (*MockRunner)(nil)

func (m *MockRunner) Refresh() error {
	m.RefreshCalled = true
	return m.RefreshErr
}

func (m *MockRunner) Merge() error {
	m.MergeCalled = true
	return m.MergeErr
}

func (m *MockRunner) Unmerge() error {
	m.UnmergeCalled = true
	return m.UnmergeErr
}

func (m *MockRunner) LinkToSysext(_ *config.Transfer) error {
	m.LinkToSysextCalled = true
	return m.LinkToSysextErr
}

// LinkToSysextAt records sysextDir and reports LinkToSysextErr. It creates
// no link: callers that need a real one inject DefaultRunner instead.
func (m *MockRunner) LinkToSysextAt(_ *config.Transfer, sysextDir string) error {
	m.LinkToSysextCalled = true
	m.LinkToSysextAtDir = sysextDir
	return m.LinkToSysextErr
}
