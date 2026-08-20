package format

import "errors"

// DefinitiveParseError marks a format parse failure that must not be reduced to
// descriptor-only metadata. Plugins use it after deterministic in-content
// format semantics have been found but are invalid or unsupported.
type DefinitiveParseError struct {
	err error
}

func (e *DefinitiveParseError) Error() string {
	if e == nil || e.err == nil {
		return "definitive format parse error"
	}
	return e.err.Error()
}

func (e *DefinitiveParseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func NewDefinitiveParseError(err error) error {
	if err == nil || IsDefinitiveParseError(err) {
		return err
	}
	return &DefinitiveParseError{err: err}
}

func IsDefinitiveParseError(err error) bool {
	var target *DefinitiveParseError
	return errors.As(err, &target)
}
