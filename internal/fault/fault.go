// Package fault marks an error with a class, so a caller can tell why an
// operation failed without parsing the error text. The last-run record of
// updex (docs/specs/last-run-record.md) reports the class of every failed
// transfer.
//
// A mark wraps the error: Error() returns the wrapped text byte for byte, and
// errors.Is and errors.As see through the mark (Unwrap). The mark is set where
// the error is made (an HTTP status, a hash mismatch, a bad signature), not by
// matching the text later.
package fault

import "errors"

// Class is the cause of a failure. The values are the error_class values of
// the last-run record.
type Class string

// The classes. A consumer must read an unknown class as Other.
const (
	// Network: no source answered, or the connection failed before or while
	// it got a response (DNS, refused, reset, timeout, TLS verification).
	Network Class = "network"
	// HTTPStatus: a source answered with an HTTP error status.
	HTTPStatus Class = "http_status"
	// Signature: the signature of SHA256SUMS is missing, invalid, or cannot
	// be checked (no usable keyring).
	Signature Class = "signature"
	// Checksum: a download or a file does not match its SHA256 hash.
	Checksum Class = "checksum"
	// DiskSpace: the disk or the quota is full.
	DiskSpace Class = "disk_space"
	// Link: the systemd-sysext link could not be moved or restored.
	Link Class = "link"
	// Config: the configuration on this host is wrong or incomplete.
	Config Class = "config"
	// Other: any other failure.
	Other Class = "other"
)

// marked is an error with a class.
type marked struct {
	class Class
	err   error
}

// Error returns the text of the wrapped error, unchanged.
func (e *marked) Error() string { return e.err.Error() }

// Unwrap returns the wrapped error.
func (e *marked) Unwrap() error { return e.err }

// Mark returns err with the class. Error() of the result is err.Error(). Mark
// returns nil for a nil err.
func Mark(class Class, err error) error {
	if err == nil {
		return nil
	}
	return &marked{class: class, err: err}
}

// ClassOf returns the class of the outermost mark in the chain of err (the
// first mark errors.As finds), and false when err has no mark.
func ClassOf(err error) (Class, bool) {
	var m *marked
	if errors.As(err, &m) {
		return m.class, true
	}
	return "", false
}
