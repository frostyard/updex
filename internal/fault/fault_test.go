package fault

import (
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestMarkKeepsTextAndChain(t *testing.T) {
	sentinel := errors.New("sentinel")
	tests := []struct {
		name  string
		class Class
		err   error
	}{
		{name: "plain", class: HTTPStatus, err: errors.New("download failed with status: 404 Not Found")},
		{name: "wrapped sentinel", class: Signature, err: fmt.Errorf("%w: signature fetch failed", sentinel)},
		{name: "io error", class: Network, err: io.ErrUnexpectedEOF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Mark(tt.class, tt.err)
			if got.Error() != tt.err.Error() {
				t.Errorf("Error() = %q, want %q", got.Error(), tt.err.Error())
			}
			if !errors.Is(got, tt.err) {
				t.Error("errors.Is does not find the wrapped error")
			}
			if errors.Unwrap(got) != tt.err {
				t.Error("Unwrap does not return the wrapped error")
			}
			class, ok := ClassOf(got)
			if !ok || class != tt.class {
				t.Errorf("ClassOf() = %q, %v, want %q, true", class, ok, tt.class)
			}
		})
	}
	if !errors.Is(Mark(Signature, fmt.Errorf("x: %w", sentinel)), sentinel) {
		t.Error("errors.Is does not see through the mark to a sentinel")
	}
}

func TestMarkNil(t *testing.T) {
	if Mark(Config, nil) != nil {
		t.Error("Mark(nil) != nil")
	}
}

func TestClassOf(t *testing.T) {
	inner := Mark(Signature, errors.New("invalid signature"))
	tests := []struct {
		name   string
		err    error
		want   Class
		wantOK bool
	}{
		{name: "nil", err: nil},
		{name: "unmarked", err: errors.New("plain")},
		{name: "marked", err: inner, want: Signature, wantOK: true},
		{name: "wrapped mark", err: fmt.Errorf("signature verification failed: %w", inner), want: Signature, wantOK: true},
		{name: "outermost mark wins", err: Mark(Config, fmt.Errorf("policy: %w", inner)), want: Config, wantOK: true},
		{name: "joined: first mark", err: errors.Join(errors.New("a"), Mark(Link, errors.New("b")), inner), want: Link, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ClassOf(tt.err)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("ClassOf() = %q, %v, want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
