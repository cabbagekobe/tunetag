package tunetag

import "errors"

// ErrUnknownFormat is returned by Detect and Open when the input
// does not match any supported container.
var ErrUnknownFormat = errors.New("tunetag: unknown format")
