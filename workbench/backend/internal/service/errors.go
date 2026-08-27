package service

import "errors"

var (
	ErrInvalidView         = errors.New("invalid workbench view")
	ErrViewNotFound        = errors.New("workbench view not found")
	ErrViewVersionConflict = errors.New("workbench view version conflict")
	ErrServiceAccessDenied = errors.New("service access denied")
	ErrServiceUnavailable  = errors.New("service unavailable")
)
