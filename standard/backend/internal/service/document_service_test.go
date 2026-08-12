package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	minio "github.com/minio/minio-go/v7"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeDocumentObjectStore struct {
	objects     map[string][]byte
	putKeys     []string
	removedKeys []string
	putErr      error
	removeErr   error
}

func (f *fakeDocumentObjectStore) BucketExists(context.Context, string) (bool, error) {
	return true, nil
}
func (f *fakeDocumentObjectStore) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	return nil
}
func (f *fakeDocumentObjectStore) PutObject(_ context.Context, _ string, key string, reader io.Reader, _ int64, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	if f.putErr != nil {
		return minio.UploadInfo{}, f.putErr
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	f.objects[key] = content
	f.putKeys = append(f.putKeys, key)
	return minio.UploadInfo{Key: key, Size: int64(len(content))}, nil
}
func (f *fakeDocumentObjectStore) RemoveObject(_ context.Context, _ string, key string, _ minio.RemoveObjectOptions) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	delete(f.objects, key)
	f.removedKeys = append(f.removedKeys, key)
	return nil
}
func (f *fakeDocumentObjectStore) StatObject(_ context.Context, _ string, key string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	content, ok := f.objects[key]
	if !ok {
		return minio.ObjectInfo{}, errors.New("object not found")
	}
	return minio.ObjectInfo{Key: key, Size: int64(len(content))}, nil
}
func (f *fakeDocumentObjectStore) GetObject(_ context.Context, _ string, key string, _ minio.GetObjectOptions) (io.ReadCloser, error) {
	content, ok := f.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func TestSanitizeDocumentFileName(t *testing.T) {
	name, err := sanitizeDocumentFileName(`../nested\report.pdf`)
	if err != nil || name != "report.pdf" {
		t.Fatalf("sanitizeDocumentFileName() = %q, %v", name, err)
	}
	if _, err := sanitizeDocumentFileName("line\nbreak.pdf"); !errors.Is(err, ErrDocumentFileNameInvalid) {
		t.Fatalf("unsafe file name error = %v, want ErrDocumentFileNameInvalid", err)
	}
}

func TestUploadFileReplacesObjectAfterDatabaseUpdate(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	doc := &models.Document{TenantID: 7, Name: "doc", FileKey: "tenant_7/documents/1/old.pdf", FileName: "old.pdf", FileSize: 3, CreatedBy: 1}
	if err := repo.Create(doc); err != nil {
		t.Fatalf("create document: %v", err)
	}
	store := &fakeDocumentObjectStore{objects: map[string][]byte{doc.FileKey: []byte("old")}}
	svc := &DocumentService{repo: repo, objectStore: store, maxFileSize: 10, timeout: time.Second}

	if err := svc.UploadFile(doc.ID, doc.TenantID, "../new.pdf", strings.NewReader("new content"), 11, "application/pdf"); !errors.Is(err, ErrDocumentFileTooLarge) {
		t.Fatalf("oversized upload error = %v, want ErrDocumentFileTooLarge", err)
	}
	if err := svc.UploadFile(doc.ID, doc.TenantID, "../new.pdf", strings.NewReader("new"), 3, "application/pdf"); err != nil {
		t.Fatalf("replacement upload: %v", err)
	}
	updated, err := repo.GetByID(doc.ID, doc.TenantID)
	if err != nil {
		t.Fatalf("reload document: %v", err)
	}
	if updated.FileName != "new.pdf" || updated.FileKey == doc.FileKey || string(store.objects[updated.FileKey]) != "new" {
		t.Fatalf("updated document/object = %+v, objects=%v", updated, store.objects)
	}
	if len(store.removedKeys) != 1 || store.removedKeys[0] != doc.FileKey {
		t.Fatalf("removed keys = %v, want old key only", store.removedKeys)
	}
}

func TestUploadFileRollsBackNewObjectWhenDatabaseUpdateFails(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	if err := db.Callback().Update().Before("gorm:update").Register("test_fail_document_update", func(tx *gorm.DB) {
		tx.AddError(errors.New("document update failed"))
	}); err != nil {
		t.Fatalf("register update failure callback: %v", err)
	}
	repo := repository.NewDocumentRepository(db)
	doc := &models.Document{TenantID: 8, Name: "doc", FileKey: "old.pdf", FileName: "old.pdf", FileSize: 3, CreatedBy: 1}
	if err := repo.Create(doc); err != nil {
		t.Fatalf("create document: %v", err)
	}
	store := &fakeDocumentObjectStore{objects: map[string][]byte{doc.FileKey: []byte("old")}}
	svc := &DocumentService{repo: repo, objectStore: store, maxFileSize: 10, timeout: time.Second}
	if err := svc.UploadFile(doc.ID, doc.TenantID, "new.pdf", strings.NewReader("new"), 3, "application/pdf"); err == nil {
		t.Fatal("UploadFile() should return database update error")
	}
	if len(store.putKeys) != 1 || len(store.removedKeys) != 1 || store.removedKeys[0] != store.putKeys[0] {
		t.Fatalf("put/rollback keys = %v/%v", store.putKeys, store.removedKeys)
	}
	if string(store.objects[doc.FileKey]) != "old" {
		t.Fatalf("old object was not preserved: %v", store.objects)
	}
}

func TestFileCleanupFailureStaysQueuedForRetry(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	cleanup, err := repo.EnqueueFileCleanup("stale.pdf")
	if err != nil {
		t.Fatalf("enqueue cleanup: %v", err)
	}
	store := &fakeDocumentObjectStore{objects: map[string][]byte{"stale.pdf": []byte("stale")}, removeErr: errors.New("minio unavailable")}
	svc := &DocumentService{repo: repo, objectStore: store, timeout: time.Second}
	svc.tryFileCleanup(*cleanup)

	var queued models.DocumentFileCleanup
	if err := db.First(&queued, cleanup.ID).Error; err != nil {
		t.Fatalf("reload cleanup: %v", err)
	}
	if queued.Attempts != 1 || queued.LastError == "" || !queued.NextAttemptAt.After(cleanup.NextAttemptAt) {
		t.Fatalf("queued cleanup = %+v", queued)
	}
	store.removeErr = nil
	svc.tryFileCleanup(queued)
	if err := db.First(&queued, cleanup.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("completed cleanup error = %v, want record not found", err)
	}
}

func openDocumentServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach standard schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE standard.documents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		doc_type TEXT,
		source_org TEXT,
		version TEXT,
		publish_date DATETIME,
		description TEXT,
		file_key TEXT,
		file_name TEXT,
		file_size INTEGER,
		created_by INTEGER NOT NULL,
		updated_by INTEGER,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create documents: %v", err)
	}
	if err := db.Exec(`CREATE TABLE standard.document_file_cleanups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		object_key TEXT NOT NULL UNIQUE,
		attempts INTEGER NOT NULL DEFAULT 0,
		next_attempt_at DATETIME NOT NULL,
		last_error TEXT,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("create document file cleanups: %v", err)
	}
	return db
}
