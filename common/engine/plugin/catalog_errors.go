package plugin

import (
	"errors"
	"fmt"
)

type CatalogErrorKind string

const (
	CatalogErrorInvalidPath CatalogErrorKind = "invalid_path"
	CatalogErrorNotFound    CatalogErrorKind = "not_found"
	CatalogErrorUnsupported CatalogErrorKind = "unsupported"
	CatalogErrorUnavailable CatalogErrorKind = "unavailable"
)

type CatalogError struct {
	Kind CatalogErrorKind
	Err  error
}

func (e *CatalogError) Error() string {
	if e == nil || e.Err == nil {
		return "catalog operation failed"
	}
	return e.Err.Error()
}

func (e *CatalogError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func WrapCatalogError(kind CatalogErrorKind, err error) error {
	if err == nil {
		return nil
	}
	switch kind {
	case CatalogErrorInvalidPath, CatalogErrorNotFound, CatalogErrorUnsupported, CatalogErrorUnavailable:
	default:
		return fmt.Errorf("invalid catalog error kind %q: %w", kind, err)
	}
	var existing *CatalogError
	if errors.As(err, &existing) {
		return err
	}
	return &CatalogError{Kind: kind, Err: err}
}

func IsCatalogErrorKind(err error, kind CatalogErrorKind) bool {
	var catalogError *CatalogError
	return errors.As(err, &catalogError) && catalogError.Kind == kind
}
