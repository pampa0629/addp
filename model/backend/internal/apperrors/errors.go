package apperrors

import "errors"

// Kind identifies the HTTP-level category of a model domain error.
type Kind string

const (
	KindValidation  Kind = "validation"
	KindConflict    Kind = "conflict"
	KindNotFound    Kind = "not_found"
	KindUnavailable Kind = "unavailable"
)

// DomainError is the stable error contract between Model services and API handlers.
// MessageID is an i18n key; Code is the public, machine-readable error code.
type DomainError struct {
	Kind      Kind
	Code      string
	MessageID string
	Cause     error
}

func (e *DomainError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return e.Code
	}
	return string(e.Kind)
}

func (e *DomainError) Unwrap() error { return e.Cause }

func New(kind Kind, code, messageID string) error {
	return &DomainError{Kind: kind, Code: code, MessageID: messageID}
}

func Wrap(kind Kind, code, messageID string, cause error) error {
	return &DomainError{Kind: kind, Code: code, MessageID: messageID, Cause: cause}
}

func Validation(code, messageID string) error  { return New(KindValidation, code, messageID) }
func Conflict(code, messageID string) error    { return New(KindConflict, code, messageID) }
func NotFound(code, messageID string) error    { return New(KindNotFound, code, messageID) }
func Unavailable(code, messageID string) error { return New(KindUnavailable, code, messageID) }

func As(err error) (*DomainError, bool) {
	var target *DomainError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
