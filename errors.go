package gdco

import (
	"errors"
	"fmt"
)

// The errors are sentinels for errors.Is. The returned errors wrap them
// with details, and the wasm package turns them into codes that a UI can
// translate.
var (
	// ErrBinary reports an input with NUL bytes: text files never contain
	// one, and comparing a binary file line by line shows only noise.
	ErrBinary = errors.New("gdco: binary file")

	// ErrUnsupported reports a format this package does not read, such as
	// PDF, legacy .doc and .xls, or an encrypted Office file.
	ErrUnsupported = errors.New("gdco: unsupported format")

	// ErrMalformed reports a file that claims a format but breaks its
	// rules, such as a damaged zip archive.
	ErrMalformed = errors.New("gdco: malformed file")

	// ErrLimit reports an input beyond Options.Limits. Stopping early is
	// the only defence against a zip bomb in a browser tab, because a Go
	// program cannot recover from running out of memory.
	ErrLimit = errors.New("gdco: safety limit exceeded")

	// ErrMismatch reports two inputs that cannot be compared with each
	// other, such as a workbook and a text.
	ErrMismatch = errors.New("gdco: formats cannot be compared")

	// ErrOptions reports an Options or ReportOptions value with a field out
	// of range.
	ErrOptions = errors.New("gdco: invalid options")

	// ErrInternal reports a panic that was recovered. It is a bug, and it
	// is returned instead of crashing because in WebAssembly a panic would
	// end the program and every later call with it.
	ErrInternal = errors.New("gdco: internal error")
)

// guard turns a panic in an exported function into ErrInternal. It is the
// last line of defence behind the code being correct, not a substitute for
// it: the fuzz tests fail on ErrInternal.
func guard(err *error) {
	if p := recover(); p != nil {
		*err = fmt.Errorf("%w: %v", ErrInternal, p)
	}
}
