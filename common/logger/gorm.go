package logger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// WithoutSQLValues keeps ORM query diagnostics without exposing bound values.
func WithoutSQLValues(delegate gormlogger.Interface) gormlogger.Interface {
	return sqlValueFilteredLogger{Interface: delegate}
}

type sqlValueFilteredLogger struct {
	gormlogger.Interface
}

func (l sqlValueFilteredLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	return WithoutSQLValues(l.Interface.LogMode(level))
}

// GORM invokes ParamsFilter before the dialect interpolates values for Trace.
func (l sqlValueFilteredLogger) ParamsFilter(_ context.Context, sql string, _ ...interface{}) (string, []interface{}) {
	return sql, nil
}

func (l sqlValueFilteredLogger) Trace(ctx context.Context, begin time.Time, sql func() (string, int64), err error) {
	l.Interface.Trace(ctx, begin, sql, safeSQLTraceError(err))
}

func safeSQLTraceError(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{gorm.ErrRecordNotFound, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return known
		}
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		code := state.SQLState()
		valid := len(code) == 5
		for _, character := range code {
			valid = valid && (character >= '0' && character <= '9' || character >= 'A' && character <= 'Z')
		}
		if valid {
			return fmt.Errorf("database error (SQLSTATE %s)", code)
		}
	}
	return fmt.Errorf("database error (%T)", err)
}
