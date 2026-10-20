package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	_ "github.com/addp/common/engine/plugins/postgresql"
	_ "github.com/addp/common/engine/plugins/s3"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
)

func exerciseScanPlanFacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	item := seedAuthorizationFacts(t, db, "schema", "table", 9)
	engine := &commonmodels.Engine{ID: 9, TenantID: func() *uint { v := uint(7); return &v }(), Version: 3, EngineType: "postgresql"}
	run := scantask.NewManualExecution(7, 33, 9, item.ID, "postgresql", nil, nil, nil, "deep", false, "manager", time.Now())
	freezeScanEngineSnapshot(run, engine)
	run.ExecutionConfig["has_header"] = false
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	s := NewScanExecutionService(db, nil, nil, nil)
	facts, err := s.GetScanPlanFacts(context.Background(), 7, run.ExecutionID)
	if err != nil || facts.BasisState != authorizationfacts.AuthorizationBasisComplete || facts.SourceScope.Mode != "exact" || facts.EngineVersion != "3" || facts.ReadOptions.HasHeader == nil || *facts.ReadOptions.HasHeader {
		t.Fatalf("facts=%+v err=%v", facts, err)
	}
	if facts.SourceScope.Anchors[0].Segments[2].Name != item.Name {
		t.Fatal("name bytes changed")
	}
	raw, _ := json.Marshal(facts)
	for _, secret := range []string{"must-not-leak", "execution_config", "actor", "connection", "triggered_by"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private fact leaked", secret)
		}
	}
	if _, err := s.GetScanPlanFacts(context.Background(), 8, run.ExecutionID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("cross-tenant read", err)
	}
	if err := db.Model(run).Update("module", "transfer").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetScanPlanFacts(context.Background(), 7, run.ExecutionID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("cross-module read", err)
	}
	if err := db.Model(run).Update("module", "meta").Error; err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		mutate      func(commonmodels.JSONMap)
		unavailable bool
		state       authorizationfacts.AuthorizationBasisState
	}{
		{"legacy missing version", func(c commonmodels.JSONMap) { delete(c, "engine_version") }, true, ""},
		{"invalid version", func(c commonmodels.JSONMap) { c["engine_version"] = "0" }, true, ""},
		{"null header", func(c commonmodels.JSONMap) { c["has_header"] = nil }, true, ""},
		{"raw path no proof", func(c commonmodels.JSONMap) { c["catalog_paths"] = []string{"Out.door"} }, false, authorizationfacts.AuthorizationBasisIncomplete},
		{"unknown selector", func(c commonmodels.JSONMap) { c["scope"] = "everything" }, false, authorizationfacts.AuthorizationBasisIncomplete},
		{"missing item", func(c commonmodels.JSONMap) { c["item_id"] = uint(900) }, false, authorizationfacts.AuthorizationBasisIncomplete},
		{"wrong catalog model", func(c commonmodels.JSONMap) { c["engine_type"] = "s3" }, false, authorizationfacts.AuthorizationBasisNeedsRefresh},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := commonmodels.JSONMap{"engine_id": uint(9), "engine_version": "3", "engine_type": "postgresql", "scan_depth": "deep", "item_id": item.ID}
			test.mutate(config)
			if err := db.Model(run).Update("execution_config", config).Error; err != nil {
				t.Fatal(err)
			}
			got, err := s.GetScanPlanFacts(context.Background(), 7, run.ExecutionID)
			if test.unavailable {
				if !errors.Is(err, ErrScanPlanFactsUnavailable) {
					t.Fatalf("expected unavailable, got=%+v err=%v", got, err)
				}
				return
			}
			if err != nil || got.BasisState != test.state || got.SourceScope != nil {
				t.Fatalf("incomplete selector widened: got=%+v err=%v", got, err)
			}
		})
	}
	// Whole requires its own explicit frozen model; it is not a fallback for
	// an unresolved item/path. No live engine client or source exists here.
	if err := db.Model(run).Update("execution_config", commonmodels.JSONMap{"engine_id": uint(9), "engine_version": "3", "engine_type": "postgresql", "scan_depth": "basic"}).Error; err != nil {
		t.Fatal(err)
	}
	whole, err := s.GetScanPlanFacts(context.Background(), 7, run.ExecutionID)
	if err != nil || whole.BasisState != authorizationfacts.AuthorizationBasisComplete || whole.SourceScope.Mode != "full" || len(whole.SourceScope.Anchors) != 1 || len(whole.SourceScope.Anchors[0].Segments) != 1 {
		t.Fatalf("whole=%+v err=%v", whole, err)
	}
	// SQLite can retain duplicate object members. PostgreSQL jsonb already
	// canonicalizes them at storage; do not claim to recover original wire JSON.
	if db.Dialector.Name() == "sqlite" {
		if err := db.Model(run).Update("execution_config", `{"engine_id":9,"engine_id":9,"engine_version":"3","engine_type":"postgresql","scan_depth":"basic"}`).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetScanPlanFacts(context.Background(), 7, run.ExecutionID); !errors.Is(err, ErrScanPlanFactsUnavailable) {
			t.Fatal("ambiguous raw plan accepted", err)
		}
	}
}

func TestScanPlanFactsReadOnlyProjection(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	createTaskExecutionTable(t, db)
	exerciseScanPlanFacts(t, db)
}

func TestScanPlanTaskFunctionalDigestAndFreeze(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	createTaskExecutionTable(t, db)
	createScanTaskTable(t, db)
	item := seedAuthorizationFacts(t, db, "schema", "table", 9)
	task := models.ScanTask{TenantID: 7, EngineID: 9, Name: "display", Scope: models.JSONMap{"type": "engine"}, Parameters: models.JSONMap{"scan_depth": "basic", "force": false}}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	run := scantask.NewTaskExecution(&task, 33, "postgresql", "manual", "meta", nil, time.Now())
	run.ExecutionConfig["item_id"] = item.ID
	run.ExecutionConfig["engine_version"] = "3"
	run.ExecutionConfig["engine_type"] = "postgresql"
	if err := freezeScanTaskDefinition(db, run); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	s := NewScanExecutionService(db, nil, nil, nil)
	a, err := s.GetScanPlanFacts(context.Background(), 7, run.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&task).Updates(map[string]any{"name": "renamed", "last_execution_status": "success"}).Error; err != nil {
		t.Fatal(err)
	}
	b, err := s.GetScanPlanFacts(context.Background(), 7, run.ExecutionID)
	if err != nil || *a.TaskDefinitionDigest != *b.TaskDefinitionDigest {
		t.Fatal("runtime/display changed functional identity", err)
	}
	if err := db.Model(&task).Update("parameters", models.JSONMap{"scan_depth": "deep", "force": false}).Error; err != nil {
		t.Fatal(err)
	}
	c, err := s.GetScanPlanFacts(context.Background(), 7, run.ExecutionID)
	if err != nil || *a.TaskDefinitionDigest != *c.TaskDefinitionDigest || a.ExecutionPlanDigest != c.ExecutionPlanDigest || c.BasisState != authorizationfacts.AuthorizationBasisNeedsRefresh || c.SourceScope != nil {
		t.Fatal("changed task did not invalidate frozen plan", err)
	}
	tenant := uint(7)
	other := &commonmodels.Engine{ID: 10, TenantID: &tenant, Version: 8, EngineType: "postgresql"}
	freezeScanEngineSnapshot(run, other)
	if run.ExecutionConfig["engine_version"] != "3" {
		t.Fatal("wrong engine snapshot bound")
	}
	var count int64
	if err := db.Model(&execution.TaskExecution{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("facts read created executions", err)
	}
}
