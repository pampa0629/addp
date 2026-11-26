package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/repository"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Run in the disposable installation gates, after the production schema and grants exist.
func TestLineageProjectionConcurrency(t *testing.T) {
	if os.Getenv("ADDP_TEST_LINEAGE_CONCURRENCY") != "1" {
		t.Skip("requires a disposable Meta installation")
	}
	db, err := repository.OpenRuntimeDatabase(config.LoadConfig())
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	engine := uint(101)
	timeoutSQL := "SET lock_timeout = '1s'"
	idSQL := "SELECT pg_backend_pid()"
	if db.Dialector.Name() == "mysql" {
		engine = 99
		timeoutSQL = "SET SESSION innodb_lock_wait_timeout=1"
		idSQL = "SELECT CONNECTION_ID()"
	}
	const tenant = 7
	prefix := uuid.NewString()
	source := createLineageItemOnEngine(t, db, tenant, engine, "src", prefix+"s")
	target := createLineageItemOnEngine(t, db, tenant, engine, "dst", prefix+"t")
	other := createLineageItemOnEngine(t, db, tenant, engine, "other", prefix+"o")
	oldTime := time.Now().UTC().Truncate(time.Second)
	newTime := oldTime.Add(time.Minute)
	err = db.Connection(func(first *gorm.DB) error {
		return db.Connection(func(second *gorm.DB) error {
			return verifyLineageWriters(t, first.Session(&gorm.Session{NewDB: true}), second.Session(&gorm.Session{NewDB: true}), idSQL, timeoutSQL, source, target, other, oldTime, newTime)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func verifyLineageWriters(t *testing.T, first, second *gorm.DB, idSQL, timeoutSQL string, source, target, other models.MetaItem, oldTime, newTime time.Time) error {
	t.Helper()
	const tenant = 7
	ctx := t.Context()
	var a, b int64
	if err := first.Raw(idSQL).Scan(&a).Error; err != nil {
		return err
	}
	if err := second.Raw(idSQL).Scan(&b).Error; err != nil {
		return err
	}
	if a == b {
		return fmt.Errorf("connections share ID %d", a)
	}
	exercise := func(name string, apply func(*gorm.DB, uint, time.Time) error) {
		t.Helper()
		if err := second.Exec(timeoutSQL).Error; err != nil {
			t.Fatal(err)
		}
		held := first.Begin()
		if held.Error != nil {
			t.Fatal(held.Error)
		}
		defer held.Rollback()
		if err := apply(held, target.ID, newTime); err != nil {
			t.Fatal(err)
		}
		err := apply(second, target.ID, oldTime)
		var my *mysqlDriver.MySQLError
		var pg interface{ SQLState() string }
		if !(errors.As(err, &my) && my.Number == 1205 || errors.As(err, &pg) && pg.SQLState() == "55P03") {
			t.Fatalf("%s: wanted database lock timeout, got %v", name, err)
		}
		if err := apply(second, other.ID, oldTime); err != nil {
			t.Fatalf("%s: different target: %v", name, err)
		}
		// Observe the old transaction executing its lock statement before releasing the newer one.
		// The earlier timeout proves that this statement contends on the same key.
		reset := "RESET lock_timeout"
		if second.Dialector.Name() == "mysql" {
			reset = "SET SESSION innodb_lock_wait_timeout=DEFAULT"
		}
		if err := second.Exec(reset).Error; err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- apply(second, target.ID, oldTime) }()
		joined := false
		defer func() {
			if !joined {
				held.Rollback()
				<-done
			}
		}()
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		waitSQL := "SELECT ? = ANY(pg_blocking_pids(?))"
		waitArgs := []any{a, b}
		if first.Dialector.Name() == "mysql" {
			waitSQL = "SELECT EXISTS(SELECT 1 FROM information_schema.processlist WHERE ID=? AND LOWER(INFO) LIKE ?)"
			statement := "%lineage_service_publication_times%"
			if name == "field" {
				statement = "%meta_item%for update%"
			}
			waitArgs = []any{b, statement}
		}
		for {
			var waiting bool
			if err := held.WithContext(waitCtx).Raw(waitSQL, waitArgs...).Scan(&waiting).Error; err != nil {
				t.Fatalf("%s: waiter was not observed: %v", name, err)
			}
			if waiting {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if err := held.Commit().Error; err != nil {
			t.Fatal(err)
		}
		err = <-done
		joined = true
		if err != nil {
			t.Fatalf("%s: waiting notification after commit: %v", name, err)
		}
		t.Logf("%s connections %d/%d: same key timed out, different target committed, queued old notification completed after holder committed", name, a, b)
	}
	publish := func(conn *gorm.DB, id uint, at time.Time) error {
		req := models.RecordServicePublicationRequest{ServiceID: id, ServiceName: "publication", PublishedRevision: "new-empty", ServiceUpdatedAt: at}
		if at.Equal(oldTime) {
			req.PublishedRevision = "old"
			req.Dependencies = []models.LineageServiceDependencyInput{{SourceItemID: source.ID, DependencyKind: "table"}}
		}
		return NewLineageService(conn, nil).RecordServicePublication(ctx, tenant, req)
	}
	exercise("service", publish)
	var count int64
	if err := second.Model(&models.LineageServiceDependency{}).Where("tenant_id=? AND service_id=? AND status='active'", tenant, target.ID).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("late publication recreated %d dependencies after empty publication", count)
	}
	var stamp models.LineageServicePublicationTime
	if err := second.Where("tenant_id=? AND service_id=?", tenant, target.ID).Take(&stamp).Error; err != nil {
		return err
	}
	if !stamp.ServiceUpdatedAt.Equal(newTime) {
		return fmt.Errorf("owner time changed to %s", stamp.ServiceUpdatedAt)
	}
	sourceSchema := fieldTestSchema(t, "before", "after")
	targetSchema := fieldTestSchema(t, "out")
	collect := func(conn *gorm.DB, id uint, at time.Time) error {
		field := "after"
		if at.Equal(oldTime) {
			field = "before"
		}
		facts := models.LineageFacts{SchemaVersion: commonExecution.LineageFactsSchemaVersion,
			Inputs:     []commonExecution.LineageResourceRef{{Port: "source", ItemID: &source.ID, SchemaSnapshot: sourceSchema}},
			Outputs:    []commonExecution.LineageResourceRef{{Port: "target", ItemID: &id, SchemaSnapshot: targetSchema, WriteMode: "replace"}},
			Operations: []commonExecution.LineageOperation{{Kind: "derive", InputPorts: []string{"source"}, OutputPorts: []string{"target"}, FieldLineageStatus: "complete", FieldMappings: []commonExecution.LineageFieldMapping{{InputPort: "source", OutputPort: "target", SourceField: field, TargetField: "out", Transformation: "direct"}}}}}
		return conn.Transaction(func(tx *gorm.DB) error {
			_, err := NewLineageService(tx, nil).collectFacts(ctx, tenant, fmt.Sprintf("%d-%s", id, field), "transfer", &facts, at)
			return err
		})
	}
	exercise("field", collect)
	var fields []string
	if err := second.Model(&models.LineageItemRelation{}).Where("tenant_id=? AND target_item_id=? AND granularity='field' AND status <> 'closed'", tenant, target.ID).Pluck("source_field_name", &fields).Error; err != nil {
		return err
	}
	if len(fields) != 1 || fields[0] != "after" {
		return fmt.Errorf("current fields after late execution: %v", fields)
	}
	if err := second.Model(&models.LineageObservation{}).Where("tenant_id=? AND target_item_id=? AND granularity='field'", tenant, target.ID).Count(&count).Error; err != nil {
		return err
	}
	if count != 2 {
		return fmt.Errorf("expected both historical observations, got %d", count)
	}
	t.Log("empty publication preserved; latest field projection preserved with both historical observations")
	return nil
}
