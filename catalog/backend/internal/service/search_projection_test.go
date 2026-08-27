package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type projectionRecordNotFoundCaptureLogger struct {
	recordNotFoundCount int
}

func (capture *projectionRecordNotFoundCaptureLogger) LogMode(logger.LogLevel) logger.Interface { return capture }
func (capture *projectionRecordNotFoundCaptureLogger) Info(context.Context, string, ...interface{}) {}
func (capture *projectionRecordNotFoundCaptureLogger) Warn(context.Context, string, ...interface{}) {}
func (capture *projectionRecordNotFoundCaptureLogger) Error(context.Context, string, ...interface{}) {}
func (capture *projectionRecordNotFoundCaptureLogger) Trace(_ context.Context, _ time.Time, _ func() (string, int64), err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		capture.recordNotFoundCount++
	}
}

type fakeCatalogSearchProjection struct {
	document *CatalogSearchDocument
	deleted  string
	err      error
}

func (f *fakeCatalogSearchProjection) Upsert(_ context.Context, document CatalogSearchDocument) error {
	if f.err != nil {
		return f.err
	}
	f.document = &document
	return nil
}

func (f *fakeCatalogSearchProjection) Delete(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = id
	return nil
}

func TestProjectionWorkerBuildsOwnerScopedSearchDocumentAndCompletesTask(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, component := createEditableCatalogEntry(t, db, 7)
	name, description := "Customer orders", "Trusted order facts"
	if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Updates(map[string]interface{}{
		"business_name": name, "business_description": description, "updated_at": time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SemanticAssociation{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, SemanticType: models.SemanticTypeDomain,
		SemanticID: 9007199254740993, RelationRole: models.SemanticRolePrimary, ObservedVersion: 3,
		ObservedSnapshot: map[string]interface{}{"name": "Sales"}, VerifiedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Responsibility{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, Role: models.ResponsibilityRoleAccountableDepartment,
		SubjectType: models.ResponsibilitySubjectDepartment, SubjectID: 9007199254740994,
		Status: models.ResponsibilityStatusActive, ObservedSnapshot: map[string]interface{}{"name": "Data Office"}, VerifiedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ProjectionTask{
		TenantID: 7, CatalogEntryID: entry.ID, Projection: "catalog_entries", Status: "pending", AvailableAt: time.Now().UTC().Add(-time.Second),
	}).Error; err != nil {
		t.Fatal(err)
	}

	projection := &fakeCatalogSearchProjection{}
	if err := NewProjectionWorker(db, projection, time.Second).ProcessNext(context.Background()); err != nil {
		t.Fatalf("process projection: %v", err)
	}
	if projection.document == nil || projection.document.ID != entry.ID.String() || projection.document.BusinessName != name ||
		projection.document.PrimaryDomainID != "9007199254740993" || projection.document.AccountableDepartmentID != "9007199254740994" ||
		len(projection.document.ComponentNames) != 1 || projection.document.ComponentNames[0] != component.DisplayName {
		t.Fatalf("document = %#v", projection.document)
	}
	var taskCount int64
	if err := db.Model(&models.ProjectionTask{}).Where("catalog_entry_id = ?", entry.ID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 {
		t.Fatalf("completed projection tasks = %d", taskCount)
	}
}

func TestProjectionWorkerEmptyQueueDoesNotTraceRecordNotFound(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	capture := &projectionRecordNotFoundCaptureLogger{}
	db = db.Session(&gorm.Session{Logger: capture})

	if err := NewProjectionWorker(db, &fakeCatalogSearchProjection{}, time.Second).ProcessNext(context.Background()); err != nil {
		t.Fatalf("process empty projection queue: %v", err)
	}
	if capture.recordNotFoundCount != 0 {
		t.Fatalf("empty projection queue traced record not found %d times", capture.recordNotFoundCount)
	}
}

func TestProjectionWorkerDeletesMergedEntryAndRetriesFailure(t *testing.T) {
	t.Run("merged entry", func(t *testing.T) {
		db := openCatalogServiceTestDB(t)
		entry, _ := createEditableCatalogEntry(t, db, 8)
		mergedInto, _ := createEditableCatalogEntry(t, db, 8)
		if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Updates(map[string]interface{}{
			"entry_status": models.EntryStatusMerged, "merged_into_entry_id": mergedInto.ID,
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ProjectionTask{TenantID: 8, CatalogEntryID: entry.ID, Projection: "search", Status: "pending", AvailableAt: time.Now().UTC().Add(-time.Second)}).Error; err != nil {
			t.Fatal(err)
		}
		projection := &fakeCatalogSearchProjection{}
		if err := NewProjectionWorker(db, projection, time.Second).ProcessNext(context.Background()); err != nil {
			t.Fatal(err)
		}
		if projection.deleted != entry.ID.String() {
			t.Fatalf("deleted = %q", projection.deleted)
		}
	})

	t.Run("projection failure", func(t *testing.T) {
		db := openCatalogServiceTestDB(t)
		entry, _ := createEditableCatalogEntry(t, db, 9)
		if err := db.Create(&models.ProjectionTask{TenantID: 9, CatalogEntryID: entry.ID, Projection: "search", Status: "pending", AvailableAt: time.Now().UTC().Add(-time.Second)}).Error; err != nil {
			t.Fatal(err)
		}
		err := NewProjectionWorker(db, &fakeCatalogSearchProjection{err: errors.New("index unavailable")}, time.Second).ProcessNext(context.Background())
		if err == nil {
			t.Fatal("projection failure was ignored")
		}
		var task models.ProjectionTask
		if err := db.Where("catalog_entry_id = ?", entry.ID).First(&task).Error; err != nil {
			t.Fatal(err)
		}
		if task.Status != "pending" || task.AttemptCount != 1 || !task.AvailableAt.After(time.Now()) {
			t.Fatalf("retry task = %#v", task)
		}
	})
}
