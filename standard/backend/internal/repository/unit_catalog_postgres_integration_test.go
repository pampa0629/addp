package repository

import (
	"github.com/addp/standard/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"sync"
	"testing"
	"time"
)

func TestPostgresUnitCatalogMigrationKeepsReferences(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	tx := db.Begin()
	defer tx.Rollback()
	for _, sql := range []string{
		`DROP SCHEMA IF EXISTS standard CASCADE`, `CREATE SCHEMA standard`,
		`CREATE TABLE standard.measurement_categories(id BIGSERIAL PRIMARY KEY,tenant_id BIGINT NOT NULL,name VARCHAR(100) NOT NULL,code VARCHAR(50) NOT NULL,description TEXT,sort_order INTEGER DEFAULT 0,is_system BOOLEAN DEFAULT false,created_at TIMESTAMPTZ,updated_at TIMESTAMPTZ,version BIGINT NOT NULL DEFAULT 1,UNIQUE(tenant_id,code))`,
		`CREATE TABLE standard.units(id BIGSERIAL PRIMARY KEY,tenant_id BIGINT NOT NULL,category_id BIGINT NOT NULL REFERENCES standard.measurement_categories(id),name VARCHAR(100) NOT NULL,symbol VARCHAR(30),description TEXT,sort_order INTEGER DEFAULT 0,is_system BOOLEAN DEFAULT false,created_at TIMESTAMPTZ,updated_at TIMESTAMPTZ,version BIGINT NOT NULL DEFAULT 1)`,
		`INSERT INTO standard.measurement_categories(id,tenant_id,name,code,description,sort_order,is_system) VALUES(101,7,'长度','length','表示距离、长度',4,true),(102,8,'长度','length','表示距离、长度',4,true),(103,8,'自定义','my_units','',0,false),(104,9,'我的长度','length','租户描述',8,true)`,
		`INSERT INTO standard.units(id,tenant_id,category_id,name,symbol,description,sort_order,is_system) VALUES(201,7,101,'米','m','米',3,true),(202,8,102,'米','m','米',3,true),(203,8,103,'箱','箱','',0,false),(204,9,104,'米','m','米',3,true)`,
		`SELECT setval(pg_get_serial_sequence('standard.measurement_categories','id'),104)`,
		`SELECT setval(pg_get_serial_sequence('standard.units','id'),204)`,
	} {
		if err := tx.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.AutoMigrate(&models.Element{}, &models.ElementRevision{}, &models.MetricDefinition{}, &models.MetricDefinitionRevision{}); err != nil {
		t.Fatal(err)
	}
	oldID := int64(202)
	oldFrom := time.Now().UTC().Add(-time.Hour)
	element := models.Element{TenantID: 8, ScopeType: "tenant_common", Code: "old_unit", CreatedBy: 1, Version: 1, LifecycleState: "active"}
	if err := tx.Create(&element).Error; err != nil {
		t.Fatal(err)
	}
	revision := models.ElementRevision{ElementID: element.ID, RevisionNo: 1, Status: "published", EffectiveFrom: &oldFrom, Name: "Old unit", Definition: "Old unit", DataType: "int", ValueDomainKind: "unrestricted", UnitID: &oldID, CompiledQualityRules: models.JSONB{}, ChangeSummary: "Initial", CreatedBy: 1}
	if err := tx.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	metric := models.MetricDefinition{TenantID: 8, ScopeType: "tenant_common", Code: "old_metric", CreatedBy: 1, Version: 1, LifecycleState: "active"}
	if err := tx.Create(&metric).Error; err != nil {
		t.Fatal(err)
	}
	mr := models.MetricDefinitionRevision{MetricDefinitionID: metric.ID, RevisionNo: 1, Status: "draft", MetricType: "atomic", Name: "Old metric", Definition: "Old metric", StatisticalCaliber: "All", UnitID: &oldID, ChangeSummary: "Initial", CreatedBy: 1}
	if err := tx.Create(&mr).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(tx); err != nil {
		t.Fatal(err)
	}
	var got models.Unit
	if err := tx.First(&got, 201).Error; err != nil {
		t.Fatal(err)
	}
	got.Localize("en")
	if got.TenantID != nil || got.Code != "metre" || got.Name != "Metre" {
		t.Fatalf("not promoted: %+v", got)
	}
	var elementUnit, metricUnit int64
	if err := tx.Model(&models.ElementRevision{}).Where("id = ?", revision.ID).Pluck("unit_id", &elementUnit).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&models.MetricDefinitionRevision{}).Where("id = ?", mr.ID).Pluck("unit_id", &metricUnit).Error; err != nil {
		t.Fatal(err)
	}
	if elementUnit != 201 || metricUnit != 201 {
		t.Fatalf("lost reference: element=%d metric=%d", elementUnit, metricUnit)
	}
	var custom models.Unit
	if err := tx.First(&custom, 203).Error; err != nil {
		t.Fatal(err)
	}
	if custom.TenantID == nil || *custom.TenantID != 8 || custom.Name != "箱" {
		t.Fatalf("custom changed: %+v", custom)
	}
	var customized models.MeasurementCategory
	if err := tx.First(&customized, 104).Error; err != nil {
		t.Fatal(err)
	}
	if customized.TenantID == nil || *customized.TenantID != 9 || customized.Name != "我的长度" || customized.Description != "租户描述" {
		t.Fatalf("edited built-in category overwritten: %+v", customized)
	}
	var customizedUnit models.Unit
	if err := tx.First(&customizedUnit, 204).Error; err != nil {
		t.Fatal(err)
	}
	if customizedUnit.TenantID == nil || *customizedUnit.TenantID != 9 || customizedUnit.CategoryID != 104 {
		t.Fatalf("edited category membership changed: %+v", customizedUnit)
	}
	if tx.Migrator().HasColumn("standard.units", "is_system") {
		t.Fatal("old ownership path retained")
	}
	if err := Migrate(tx); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresUnitCatalogConcurrentStartup(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.Exec("DROP SCHEMA IF EXISTS standard CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() { defer group.Done(); <-start; results <- Migrate(db) }()
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Error(err)
		}
	}
	for _, check := range []struct {
		model interface{}
		count int64
	}{{&models.MeasurementCategory{}, 10}, {&models.Unit{}, 36}} {
		var count int64
		if err := db.Model(check.model).Where("tenant_id IS NULL").Count(&count).Error; err != nil || count != check.count {
			t.Fatalf("concurrent startup: count=%d want=%d err=%v", count, check.count, err)
		}
	}
}
