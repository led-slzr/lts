package cli

import (
	"errors"
	"fmt"
)

// Exit codes — part of the public contract (documented in `lts help agents`).
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitNotFound    = 3 // target not found or ambiguous
	ExitGuard       = 4 // a safety guard refused (confirmation / --force needed)
	ExitPartial     = 5 // some repos/worktrees succeeded, some failed
	ExitConflict    = 6 // rebase conflict (aborted, changes restored)
	ExitExists      = 7 // already exists (create without --reuse)
	ExitInterrupted = 130
)

// Error codes (machine-readable `error.code` in JSON output).
const (
	CodeError       = "error"
	CodeUsage       = "usage"
	CodeNotFound    = "not_found"
	CodeAmbiguous   = "ambiguous"
	CodeGuard       = "guard"
	CodePartial     = "partial"
	CodeConflict    = "conflict"
	CodeExists      = "exists"
	CodeBusy        = "busy"
	CodeInterrupted = "interrupted"
)

// Error is a CLI failure with a stable exit code, a machine-readable code,
// and optional hint / suggestions for the human or agent reading it.
type Error struct {
	Exit        int      `json:"-"`
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Hint        string   `json:"hint,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
	// Available lists valid choices when the input was missing entirely
	Available []string `json:"available,omitempty"`
	// Data optionally carries a partial result (e.g. per-repo outcomes)
	Data any `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func newErr(exit int, code, format string, a ...any) *Error {
	return &Error{Exit: exit, Code: code, Message: fmt.Sprintf(format, a...)}
}

func (e *Error) withHint(format string, a ...any) *Error {
	e.Hint = fmt.Sprintf(format, a...)
	return e
}

func (e *Error) withSuggestions(s []string) *Error {
	e.Suggestions = s
	return e
}

func usageErr(format string, a ...any) *Error {
	return newErr(ExitUsage, CodeUsage, format, a...)
}

func notFoundErr(format string, a ...any) *Error {
	return newErr(ExitNotFound, CodeNotFound, format, a...)
}

func guardErr(format string, a ...any) *Error {
	return newErr(ExitGuard, CodeGuard, format, a...)
}

// asCLIError normalizes any error into an *Error.
func asCLIError(err error) *Error {
	var ce *Error
	if errors.As(err, &ce) {
		return ce
	}
	return &Error{Exit: ExitError, Code: CodeError, Message: err.Error()}
}
