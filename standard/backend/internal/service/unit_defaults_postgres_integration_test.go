package service

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresUnitDefaultsConcurrentFirstRead(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	t.Cleanup(func() {
		for _, model := range []interface{}{&models.Unit{}, &models.MeasurementCategory{}} {
			if err := db.Where("tenant_id = ?", tenantID).Delete(model).Error; err != nil {
				t.Error(err)
			}
		}
	})
	svc := NewUnitService(repository.NewMeasurementCategoryRepository(db), repository.NewUnitRepository(db))
	start := make(chan struct{})
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				rows, err := svc.ListUnits(tenantID, nil)
				if err != nil || len(rows) != 31 {
					failures <- fmt.Errorf("units count=%d: %v", len(rows), err)
				}
			} else {
				rows, err := svc.ListCategories(tenantID)
				if err != nil || len(rows) != 10 {
					failures <- fmt.Errorf("categories count=%d: %v", len(rows), err)
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	for _, check := range []struct {
		model    interface{}
		expected int64
	}{{&models.Unit{}, 31}, {&models.MeasurementCategory{}, 10}} {
		var count int64
		if err := db.Model(check.model).Where("tenant_id = ?", tenantID).Count(&count).Error; err != nil || count != check.expected {
			t.Fatalf("persisted count=%d want=%d err=%v", count, check.expected, err)
		}
	}
}
