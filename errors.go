package tunetag

import "errors"

var (
	// ErrUnknownFormat is returned by Detect and Open when the input
	// does not match any supported container.
	ErrUnknownFormat = errors.New("tunetag: unknown format")

	// ErrEmptyFile is returned by Detect and Open when the input has
	// zero bytes. errors.Is(err, ErrUnknownFormat) reports true so
	// existing callers that only branch on ErrUnknownFormat keep
	// working.
	ErrEmptyFile = &detectError{msg: "tunetag: empty file"}

	// ErrFileTooSmall is returned by Detect and Open when the input
	// is shorter than any supported tag header can be. As with
	// ErrEmptyFile, errors.Is(err, ErrUnknownFormat) reports true.
	ErrFileTooSmall = &detectError{msg: "tunetag: file too small to contain any tag"}
)

// detectError is the concrete type behind ErrEmptyFile and
// ErrFileTooSmall. Its Is method also matches ErrUnknownFormat so
// the new sentinels remain a strict refinement of the old one.
type detectError struct{ msg string }

func (e *detectError) Error() string { return e.msg }

func (e *detectError) Is(target error) bool {
	return target == e || target == ErrUnknownFormat
}
