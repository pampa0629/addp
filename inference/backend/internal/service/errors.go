package service

import "errors"

var (
	ErrInvalidRequest      = errors.New("inference request invalid")
	ErrForbidden           = errors.New("inference scope forbidden")
	ErrNotFound            = errors.New("model resource not found")
	ErrResourceInUse       = errors.New("resource in use")
	ErrProfileUnavailable  = errors.New("model profile unavailable")
	ErrUnsupported         = errors.New("inference operation unsupported")
	ErrUpstreamFailed      = errors.New("inference upstream failed")
	ErrUpstreamUnavailable = errors.New("inference upstream unavailable")
	ErrTimeout             = errors.New("inference timeout")
)
