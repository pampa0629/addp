package logger

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type privateLogRow struct {
	ID      int `gorm:"primaryKey"`
	Payload string
}

func privateLogDB(t *testing.T, output *bytes.Buffer, level gormlogger.LogLevel) *gorm.DB {
	t.Helper()
	delegate := gormlogger.New(log.New(output, "", 0), gormlogger.Config{
		LogLevel: level, SlowThreshold: time.Nanosecond,
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: WithoutSQLValues(delegate).LogMode(level),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&privateLogRow{}); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	return db
}

func TestORMTraceDoesNotExposePrivateHistoryValues(t *testing.T) {
	for _, level := range []gormlogger.LogLevel{gormlogger.Info, gormlogger.Warn} {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			var output bytes.Buffer
			db := privateLogDB(t, &output, level)
			const payload = `{"protected_query_history":{"rows":[{"phone":"private-history-sentinel"}]}}`
			row := privateLogRow{ID: 1, Payload: payload}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			var saved privateLogRow
			if err := db.First(&saved, row.ID).Error; err != nil || saved.Payload != payload {
				t.Fatal("log filtering must not alter persisted history")
			}
			trace := output.String()
			if strings.Contains(trace, "private-history-sentinel") || strings.Contains(trace, "protected_query_history") {
				t.Fatal("ORM trace exposed a bound private history value")
			}
			if !strings.Contains(trace, "INSERT INTO") || !strings.Contains(trace, "payload") || !strings.Contains(trace, "[rows:") || !strings.Contains(trace, "ms]") {
				t.Fatal("ORM trace lost SQL structure, row count or timing")
			}
		})
	}
}

type traceSQLStateError struct{}

func (*traceSQLStateError) Error() string    { return "private-error-sentinel" }
func (*traceSQLStateError) SQLState() string { return "22001" }

type invalidTraceSQLStateError struct{}

func (*invalidTraceSQLStateError) Error() string    { return "private-error-sentinel" }
func (*invalidTraceSQLStateError) SQLState() string { return "private-error-sentinel" }

func TestORMTraceDoesNotEchoPrivateDatabaseErrors(t *testing.T) {
	for _, source := range []error{&traceSQLStateError{}, &invalidTraceSQLStateError{}, errors.New("private-error-sentinel")} {
		t.Run(fmt.Sprintf("%T", source), func(t *testing.T) {
			var output bytes.Buffer
			db := privateLogDB(t, &output, gormlogger.Error)
			if err := db.Create(&privateLogRow{ID: 1}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().After("gorm:update").Register("test:private-error", func(tx *gorm.DB) {
				tx.AddError(fmt.Errorf("wrapped database failure: %w", source))
			}); err != nil {
				t.Fatal(err)
			}
			result := db.Model(&privateLogRow{}).Where("id = ?", 1).Update("payload", "private-bound-sentinel")
			if !errors.Is(result.Error, source) {
				t.Fatal("log filtering changed the caller's error chain")
			}
			trace := output.String()
			if strings.Contains(trace, "private-error-sentinel") || strings.Contains(trace, "private-bound-sentinel") {
				t.Fatal("ORM error trace exposed a private value")
			}
			if !strings.Contains(trace, "UPDATE") || !strings.Contains(trace, "database error") {
				t.Fatal("ORM error trace lost the operation or safe error diagnostic")
			}
			if _, ok := source.(*traceSQLStateError); ok && !strings.Contains(trace, "22001") {
				t.Fatal("ORM error trace lost SQLSTATE")
			}
		})
	}
}
