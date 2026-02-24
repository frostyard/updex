package sysupdate

// MockRunner records calls for testing.
type MockRunner struct {
	UpdateCalled    bool
	UpdateComponent string
	UpdateErr       error
	// UpdateCalls records all Update invocations in order.
	UpdateCalls []string
}

func (m *MockRunner) Update(component string) error {
	m.UpdateCalled = true
	m.UpdateComponent = component
	m.UpdateCalls = append(m.UpdateCalls, component)
	return m.UpdateErr
}
