package service

import "errors"

var (
	ErrInvalidComponentConfiguration   = errors.New("invalid data application component configuration")
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
