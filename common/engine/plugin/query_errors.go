package plugin

import "errors"

// QueryErrorCode identifies a stable, machine-readable query runtime error.
// Codes are intentionally not localized; callers should translate them at the UI boundary.
type QueryErrorCode string

const (
	QueryErrorCodeMongoDBDatabaseRequired QueryErrorCode = "mongodb_database_required"
)

// QueryError preserves a stable error code while retaining the provider error for diagnostics.
type QueryError struct {
	Code QueryErrorCode
	Err  error
}

func (e *QueryError) Error() string {
	if e == nil || e.Err == nil {
		return string(e.Code)
	}
	return e.Err.Error()
}

func (e *QueryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewQueryError wraps a provider failure with a stable query error code.
func NewQueryError(code QueryErrorCode, err error) error {
	if err == nil {
		err = errors.New(string(code))
	}
	return &QueryError{Code: code, Err: err}
}

// QueryErrorCodeOf returns the stable code carried by err, if any.
func QueryErrorCodeOf(err error) QueryErrorCode {
	var queryErr *QueryError
	if !errors.As(err, &queryErr) || queryErr == nil {
		return ""
	}
	return queryErr.Code
}
