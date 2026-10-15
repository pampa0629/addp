package pq

import (
	"database/sql/driver"
	"fmt"
	"io"
	"net"
	"runtime"

	"github.com/lib/pq"
)

// Error severities
const (
	Efatal   = "FATAL"
	Epanic   = "PANIC"
	Ewarning = "WARNING"
	Enotice  = "NOTICE"
	Edebug   = "DEBUG"
	Einfo    = "INFO"
	Elog     = "LOG"
)

// Reuse lib/pq's public error types so platform SQLSTATE and query error
// handling retain the same typed contract across both registered drivers.
type Error = pq.Error
type ErrorCode = pq.ErrorCode
type ErrorClass = pq.ErrorClass

func parseError(r *readBuf) *Error {
	err := new(Error)
	for t := r.byte(); t != 0; t = r.byte() {
		msg := r.string()
		switch t {
		case 'S':
			err.Severity = msg
		case 'C':
			err.Code = ErrorCode(msg)
		case 'M':
			err.Message = msg
		case 'D':
			err.Detail = msg
		case 'H':
			err.Hint = msg
		case 'P':
			err.Position = msg
		case 'p':
			err.InternalPosition = msg
		case 'q':
			err.InternalQuery = msg
		case 'W':
			err.Where = msg
		case 's':
			err.Schema = msg
		case 't':
			err.Table = msg
		case 'c':
			err.Column = msg
		case 'd':
			err.DataTypeName = msg
		case 'n':
			err.Constraint = msg
		case 'F':
			err.File = msg
		case 'L':
			err.Line = msg
		case 'R':
			err.Routine = msg
		}
	}
	return err
}

func errorf(s string, args ...interface{}) {
	panic(fmt.Errorf("pq: %s", fmt.Sprintf(s, args...)))
}

// TODO(ainar-g) Rename to errorf after removing panics.
func fmterrorf(s string, args ...interface{}) error {
	return fmt.Errorf("pq: %s", fmt.Sprintf(s, args...))
}

func errRecoverNoErrBadConn(err *error) {
	e := recover()
	if e == nil {
		// Do nothing
		return
	}
	var ok bool
	*err, ok = e.(error)
	if !ok {
		*err = fmt.Errorf("pq: unexpected error: %#v", e)
	}
}

func (cn *conn) errRecover(err *error) {
	e := recover()
	switch v := e.(type) {
	case nil:
		// Do nothing
	case runtime.Error:
		cn.err.set(driver.ErrBadConn)
		panic(v)
	case *Error:
		if v.Fatal() {
			*err = driver.ErrBadConn
		} else {
			*err = v
		}
	case *net.OpError:
		cn.err.set(driver.ErrBadConn)
		*err = v
	case *safeRetryError:
		cn.err.set(driver.ErrBadConn)
		*err = driver.ErrBadConn
	case error:
		if v == io.EOF || v.Error() == "remote error: handshake failure" {
			*err = driver.ErrBadConn
		} else {
			*err = v
		}

	default:
		cn.err.set(driver.ErrBadConn)
		panic(fmt.Sprintf("unknown error: %#v", e))
	}

	// Any time we return ErrBadConn, we need to remember it since *Tx doesn't
	// mark the connection bad in database/sql.
	if *err == driver.ErrBadConn {
		cn.err.set(driver.ErrBadConn)
	}
}
