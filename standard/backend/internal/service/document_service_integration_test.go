package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestDocumentFileLifecycleAgainstPostgresAndMinIO(t *testing.T) {
	if os.Getenv("ADDP_STANDARD_MINIO_INTEGRATION") != "1" {
		t.Skip("enable through make test-standard-postgres")
	}
	for _, name := range []string{"STANDARD_POSTGRES_TEST_DSN", "STANDARD_MINIO_TEST_ENDPOINT", "STANDARD_MINIO_TEST_ACCESS_KEY", "STANDARD_MINIO_TEST_SECRET_KEY"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is required by the owned MinIO gate", name)
		}
	}
	db := openStandardReferenceDeletionPostgres(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close PostgreSQL: %v", err)
		}
	})
	client, err := minio.New(os.Getenv("STANDARD_MINIO_TEST_ENDPOINT"), &minio.Options{
		Creds: credentials.NewStaticV4(os.Getenv("STANDARD_MINIO_TEST_ACCESS_KEY"), os.Getenv("STANDARD_MINIO_TEST_SECRET_KEY"), ""),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, state := range []string{"draft", "withdrawn"} {
		t.Run(state, func(t *testing.T) {
			// This callback runs after the transaction rollback and object cleanup.
			var prefix string
			t.Cleanup(func() {
				if prefix == "" {
					return
				}
				var pending int64
				if err := db.Model(&models.DocumentFileCleanup{}).Where("starts_with(object_key, ?)", prefix).Count(&pending).Error; err != nil || pending != 0 {
					t.Errorf("file cleanup residue: count=%d err=%v", pending, err)
				}
			})
			f := newRevisionLifecycleFixture(t, db)
			prefix = fmt.Sprintf("tenant_%d/documents/", f.tenantID)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				for object := range client.ListObjects(ctx, minioBucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
					if object.Err != nil {
						t.Errorf("list fixture objects: %v", object.Err)
						continue
					}
					if err := client.RemoveObject(ctx, minioBucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
						t.Errorf("remove fixture object: %v", err)
					}
				}
				for object := range client.ListObjects(ctx, minioBucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
					t.Errorf("fixture object residue: key=%q err=%v", object.Key, object.Err)
				}
			})
			svc := NewDocumentService(repository.NewDocumentRepository(f.tx), repository.NewTenantReferenceRepository(f.tx), client, DocumentStorageOptions{MaxFileSize: 1024, Timeout: 10 * time.Second})
			t.Cleanup(svc.Stop)
			from := time.Now().UTC().Add(-time.Hour)
			created, err := svc.CreateDocument(&models.CreateDocumentRequest{ScopeType: models.StandardScopeTenantCommon, Code: "minio_lifecycle", DocType: "reference", Name: "MinIO 文件联测", Description: "替换、删除和撤回后保留文件", EffectiveFrom: &from}, f.tenantID, 1, "初始创建")
			if err != nil {
				t.Fatal(err)
			}
			f.track(t, "documents", "document_revisions", created)
			id, revisionID := created.ID, created.DraftRevision.ID
			first, err := svc.UploadFile(id, revisionID, f.tenantID, 1, created.Version, "first.md", bytes.NewReader([]byte("first")), 5, "text/markdown")
			if err != nil {
				t.Fatal(err)
			}
			content := []byte("# 联测\n\n发布后撤回仍保留文件。\n")
			second, err := svc.UploadFile(id, revisionID, f.tenantID, 1, first.Version, "second.md", bytes.NewReader(content), int64(len(content)), "text/markdown")
			if err != nil {
				t.Fatal(err)
			}
			assertMissingMinioDocumentObject(t, client, first.LatestRevision.FileKey)
			digest := sha256.Sum256(content)
			if second.LatestRevision.ContentSHA256 != hex.EncodeToString(digest[:]) || second.LatestRevision.MediaType != "text/markdown" {
				t.Fatalf("uploaded file metadata mismatch: %#v", second.LatestRevision)
			}
			if state == "withdrawn" {
				withdrawn := runRevisionLifecycle(t, second, f.tenantID, svc.SubmitRevision, svc.PublishRevision, svc.WithdrawRevision)
				detail, err := svc.GetDocument(id, f.tenantID)
				assertWithdrawnLifecycleRead(t, detail, err, withdrawn)
				list, total, err := svc.ListDocuments(f.tenantID, repository.ListDocumentOptions{Status: models.RevisionStatusWithdrawn})
				assertWithdrawnLifecycleList(t, list, total, err, withdrawn)
				if detail.LatestRevision.FileKey != second.LatestRevision.FileKey || detail.LatestRevision.ContentSHA256 != second.LatestRevision.ContentSHA256 {
					t.Fatalf("withdrawal changed file reference: %#v", detail.LatestRevision)
				}
				if err := svc.DeleteDocument(id, f.tenantID); !errors.Is(err, ErrDocumentPublicationHistory) {
					t.Fatalf("withdrawn document deletion error=%v", err)
				}
			}
			reader, name, mediaType, size, err := svc.DownloadFile(id, revisionID, f.tenantID)
			if err != nil {
				t.Fatal(err)
			}
			actual, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil || name != "second.md" || mediaType != "text/markdown" || size != int64(len(content)) || !bytes.Equal(actual, content) {
				t.Fatalf("download mismatch: name=%q size=%d read=%v close=%v", name, size, readErr, closeErr)
			}
			if state == "draft" {
				if err := svc.DeleteDocument(id, f.tenantID); err != nil {
					t.Fatal(err)
				}
				assertMissingMinioDocumentObject(t, client, second.LatestRevision.FileKey)
			}
			var pending int64
			if err := f.tx.Model(&models.DocumentFileCleanup{}).Where("starts_with(object_key, ?)", prefix).Count(&pending).Error; err != nil || pending != 0 {
				t.Fatalf("pending file cleanup: count=%d err=%v", pending, err)
			}
		})
	}
}

func assertMissingMinioDocumentObject(t *testing.T, client *minio.Client, key string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.StatObject(ctx, minioBucket, key, minio.StatObjectOptions{})
	if minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatalf("expected missing object %q, got %v", key, err)
	}
}
