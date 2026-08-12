package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDocumentFileLifecycleAgainstPostgresAndMinIO(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	endpoint := os.Getenv("STANDARD_MINIO_TEST_ENDPOINT")
	if dsn == "" || endpoint == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN and STANDARD_MINIO_TEST_ENDPOINT are required")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatalf("migrate standard schema: %v", err)
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(envOrDefault("STANDARD_MINIO_TEST_ACCESS_KEY", "minioadmin"), envOrDefault("STANDARD_MINIO_TEST_SECRET_KEY", "minioadmin"), ""),
	})
	if err != nil {
		t.Fatalf("create minio client: %v", err)
	}

	tenantID := int64(9_000_000_002)
	repo := repository.NewDocumentRepository(db)
	svc := NewDocumentService(repo, nil, client, DocumentStorageOptions{MaxFileSize: 1024, Timeout: 10 * time.Second})
	defer svc.Stop()
	doc := &models.Document{TenantID: tenantID, Name: "integration-" + uuid.NewString(), CreatedBy: 1}
	if err := repo.Create(doc); err != nil {
		t.Fatalf("create document: %v", err)
	}
	defer db.Exec("DELETE FROM standard.documents WHERE id = ?", doc.ID)

	if err := svc.UploadFile(doc.ID, tenantID, "first.pdf", bytes.NewReader([]byte("first")), 5, "application/pdf"); err != nil {
		t.Fatalf("upload first file: %v", err)
	}
	first, err := repo.GetByID(doc.ID, tenantID)
	if err != nil {
		t.Fatalf("load first file metadata: %v", err)
	}
	if err := svc.UploadFile(doc.ID, tenantID, "second.pdf", bytes.NewReader([]byte("second")), 6, "application/pdf"); err != nil {
		t.Fatalf("replace file: %v", err)
	}
	if _, err := client.StatObject(context.Background(), minioBucket, first.FileKey, minio.StatObjectOptions{}); err == nil {
		t.Fatalf("old object %q still exists", first.FileKey)
	}
	reader, name, size, err := svc.DownloadFile(doc.ID, tenantID)
	if err != nil {
		t.Fatalf("download file: %v", err)
	}
	content, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || name != "second.pdf" || size != 6 || string(content) != "second" {
		t.Fatalf("download = name=%q size=%d content=%q read=%v close=%v", name, size, content, readErr, closeErr)
	}
	second, err := repo.GetByID(doc.ID, tenantID)
	if err != nil {
		t.Fatalf("load second file metadata: %v", err)
	}
	if err := svc.DeleteDocument(doc.ID, tenantID); err != nil {
		t.Fatalf("delete document: %v", err)
	}
	if _, err := client.StatObject(context.Background(), minioBucket, second.FileKey, minio.StatObjectOptions{}); err == nil {
		t.Fatalf("deleted document object %q still exists", second.FileKey)
	}
	var pending int64
	if err := db.Model(&models.DocumentFileCleanup{}).Where("object_key IN ?", []string{first.FileKey, second.FileKey}).Count(&pending).Error; err != nil {
		t.Fatalf("count pending cleanups: %v", err)
	}
	if pending != 0 {
		t.Fatalf("pending file cleanups = %d, want 0", pending)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
