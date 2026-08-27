package service

import "errors"

var (
	ErrInvalidView                     = errors.New("invalid workbench view")
	ErrViewNotFound                    = errors.New("workbench view not found")
	ErrViewVersionConflict             = errors.New("workbench view version conflict")
	ErrServiceAccessDenied             = errors.New("service access denied")
	ErrServiceUnavailable              = errors.New("service unavailable")
	ErrInvalidDataApplication          = errors.New("invalid data application")
	ErrDataApplicationNotFound         = errors.New("data application not found")
	ErrDataApplicationVersionConflict  = errors.New("data application version conflict")
	ErrDataApplicationAlreadyPublished = errors.New("published data application cannot be deleted")
	ErrDataApplicationNotPublished     = errors.New("data application is not published")
	ErrDataApplicationAccessDenied     = errors.New("data application access denied")
	ErrInvalidResourceGrant            = errors.New("invalid resource grant")
	ErrResourceGrantConflict           = errors.New("resource grant conflict")
)
