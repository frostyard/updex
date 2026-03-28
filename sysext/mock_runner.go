package sysext

import "github.com/frostyard/updex/config"

// MockRunner is a test double for SysextRunner
type MockRunner struct {
	RefreshCalled      bool
	RefreshErr         error
	MergeCalled        bool
	MergeErr           error
	UnmergeCalled      bool
	UnmergeErr         error
	LinkToSysextCalled bool
	LinkToSysextErr    error
}

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
