package common

import (
	"fmt"

	"github.com/frostyard/pm/progress"
)

// TextReporter is a progress reporter that prints to stdout with text formatting.
// It follows the pm pattern, printing only when events start (not end).
type TextReporter struct{}

// NewTextReporter creates a new text-based progress reporter.
func NewTextReporter() *TextReporter {
	return &TextReporter{}
}

// OnAction is called when an action starts or ends.
func (r *TextReporter) OnAction(action progress.ProgressAction) {
	// Only print on start (EndedAt is zero)
	if !action.StartedAt.IsZero() && action.EndedAt.IsZero() {
		fmt.Printf("→ %s\n", action.Name)
	}
}

// OnTask is called when a task starts or ends.
func (r *TextReporter) OnTask(task progress.ProgressTask) {
	// Only print on start
	if !task.StartedAt.IsZero() && task.EndedAt.IsZero() {
		fmt.Printf("  • %s\n", task.Name)
	}
}

// OnStep is called when a step starts or ends.
func (r *TextReporter) OnStep(step progress.ProgressStep) {
	// Only print on start
	if !step.StartedAt.IsZero() && step.EndedAt.IsZero() {
		fmt.Printf("    - %s\n", step.Name)
	}
}

// OnMessage is called when a message is emitted.
func (r *TextReporter) OnMessage(msg progress.ProgressMessage) {
	prefix := ""
	switch msg.Severity {
	case progress.SeverityInfo:
		prefix = "ℹ"
	case progress.SeverityWarning:
		prefix = "⚠"
	case progress.SeverityError:
		prefix = "✗"
	}
	fmt.Printf("    %s %s\n", prefix, msg.Text)
}

// Ensure TextReporter implements ProgressReporter
var _ progress.ProgressReporter = (*TextReporter)(nil)
