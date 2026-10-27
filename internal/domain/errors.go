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
	ErrNotFound     = errors.New("not found")
	ErrExists       = errors.New("already exists")
	ErrNoManifest   = errors.New("no manifest in this multiverse")
	ErrUnsupported  = errors.New("unsupported")
	ErrPrerequisite = errors.New("prerequisite not met")
)
