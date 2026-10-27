package format

import "errors"

// ErrTableSourceChanged requires fresh metadata before reading another source version.
var ErrTableSourceChanged = errors.New("table source changed; refresh metadata")
