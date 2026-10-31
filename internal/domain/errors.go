package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ValidationError collects every manifest problem found in one pass.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid manifest:\n  - %s", strings.Join(e.Problems, "\n  - "))
}

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
	// ErrInvalid is what Invalid below carries: see it for what it means.
	ErrInvalid      = errors.New("invalid")
	ErrNoManifest   = errors.New("no manifest in this multiverse")
	ErrUnsupported  = errors.New("unsupported")
	ErrPrerequisite = errors.New("prerequisite not met")
)

// Invalid is an error about what was asked for rather than about the state of
// anything — an empty note, a note too long — so asking again will not help
// and nothing is worth retrying. errors.Is finds ErrInvalid in it, which is
// how an adapter answers one as a bad request, while what it says stays the
// words worth reading.
func Invalid(format string, a ...any) error { return invalid{fmt.Sprintf(format, a...)} }

type invalid struct{ what string }

func (e invalid) Error() string { return e.what }
func (e invalid) Unwrap() error { return ErrInvalid }
