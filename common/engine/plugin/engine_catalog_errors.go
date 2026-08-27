package plugin

import (
	"errors"
	"fmt"
)

type EngineCatalogErrorKind string

const (
	EngineCatalogErrorInvalidPath EngineCatalogErrorKind = "invalid_path"
	EngineCatalogErrorNotFound    EngineCatalogErrorKind = "not_found"
	EngineCatalogErrorUnsupported EngineCatalogErrorKind = "unsupported"
	EngineCatalogErrorUnavailable EngineCatalogErrorKind = "unavailable"
)

type EngineCatalogError struct {
	Kind EngineCatalogErrorKind
	Err  error
}

func (e *EngineCatalogError) Error() string {
	if e == nil || e.Err == nil {
		return "catalog operation failed"
	}
	return e.Err.Error()
}

func (e *EngineCatalogError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func WrapEngineCatalogError(kind EngineCatalogErrorKind, err error) error {
	if err == nil {
		return nil
	}
	switch kind {
	case EngineCatalogErrorInvalidPath, EngineCatalogErrorNotFound, EngineCatalogErrorUnsupported, EngineCatalogErrorUnavailable:
	default:
		return fmt.Errorf("invalid catalog error kind %q: %w", kind, err)
	}
	var existing *EngineCatalogError
	if errors.As(err, &existing) {
		return err
	}
	return &EngineCatalogError{Kind: kind, Err: err}
}

func IsEngineCatalogErrorKind(err error, kind EngineCatalogErrorKind) bool {
	var catalogError *EngineCatalogError
	return errors.As(err, &catalogError) && catalogError.Kind == kind
}
