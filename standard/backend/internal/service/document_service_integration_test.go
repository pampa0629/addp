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

	"github.com/addp/common/events"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/objectstore"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

// Physical bucket selected by the gate's remapped deployment.
const documentTestBucket = "project-a-standard-docs"

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

	bindCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	area, err := bootstrap.OpenObjectArea(bindCtx, objectstore.Standard)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("committed document file and conflicting replacement", func(t *testing.T) {
		verifyCommittedDocumentFile(t, db, client, area)
	})

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
				for object := range client.ListObjects(ctx, documentTestBucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
					if object.Err != nil {
						t.Errorf("list fixture objects: %v", object.Err)
						continue
					}
					if err := client.RemoveObject(ctx, documentTestBucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
						t.Errorf("remove fixture object: %v", err)
					}
				}
				for object := range client.ListObjects(ctx, documentTestBucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
					t.Errorf("fixture object residue: key=%q err=%v", object.Key, object.Err)
				}
			})
			svc := NewDocumentService(repository.NewDocumentRepository(f.tx), repository.NewTenantReferenceRepository(f.tx), area, DocumentStorageOptions{MaxFileSize: 1024, Timeout: 10 * time.Second})
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
	_, err := client.StatObject(ctx, documentTestBucket, key, minio.StatObjectOptions{})
	if minio.ToErrorResponse(err).Code != "NoSuchKey" {
		t.Fatalf("expected missing object %q, got %v", key, err)
	}
}

// Observe a successful real PUT before the service attempts its PG transaction.
type observedDocumentPut struct {
	objectstore.Area
	afterPut func(context.Context, string) error
}

func (s observedDocumentPut) Put(ctx context.Context, key string, body io.Reader, size int64, mediaType string) error {
	if err := s.Area.Put(ctx, key, body, size, mediaType); err != nil {
		return err
	}
	return s.afterPut(ctx, key)
}

func verifyCommittedDocumentFile(t *testing.T, db *gorm.DB, client *minio.Client, area objectstore.Area) {
	t.Helper()
	tenant := time.Now().UnixNano()
	repo := repository.NewDocumentRepository(db)
	svc := NewDocumentService(repo, repository.NewTenantReferenceRepository(db), area, DocumentStorageOptions{MaxFileSize: 1024, Timeout: 10 * time.Second})
	t.Cleanup(svc.Stop)
	from := time.Now().UTC().Add(-time.Hour)
	doc, err := svc.CreateDocument(&models.CreateDocumentRequest{ScopeType: models.StandardScopeTenantCommon, Code: "file_binding", DocType: "reference", Name: "File binding", EffectiveFrom: &from}, tenant, 1, "create")
	if err != nil {
		t.Fatal(err)
	}
	id, revision := doc.ID, doc.DraftRevision.ID
	// Remove this committed fixture even if an assertion aborts the test.
	t.Cleanup(func() {
		var count int64
		if err := db.Model(&models.Document{}).Where("id=? AND tenant_id=?", id, tenant).Count(&count).Error; err != nil {
			t.Error(err)
			return
		}
		if count != 0 {
			if err := svc.DeleteDocument(id, tenant); err != nil {
				t.Error(err)
			}
		}
	})
	download := func(want []byte) {
		t.Helper()
		r, name, media, size, err := svc.DownloadFile(id, revision, tenant)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := io.ReadAll(r)
		closeErr := r.Close()
		if err != nil || closeErr != nil || name != "document.bin" || media != "application/octet-stream" || size != int64(len(want)) || !bytes.Equal(actual, want) {
			t.Fatalf("download %q (%s, %d): got %x read=%v close=%v", name, media, size, actual, err, closeErr)
		}
	}
	checkCommitted := func(key string, want []byte, version int64) {
		t.Helper()
		saved, err := repo.GetAggregate(id, tenant)
		if err != nil {
			t.Fatal(err)
		}
		r := saved.DraftRevision
		digest := sha256.Sum256(want)
		if r == nil || r.ID != revision || r.FileKey != key || r.ContentSHA256 != hex.EncodeToString(digest[:]) || r.FileSize != int64(len(want)) || r.MediaType != "application/octet-stream" || saved.Version != version {
			t.Fatalf("committed version=%d revision=%+v", saved.Version, r)
		}
	}
	upload := func(version int64, data []byte) (*models.DocumentAggregate, error) {
		return svc.UploadFile(id, revision, tenant, 1, version, "document.bin", bytes.NewReader(data), int64(len(data)), "application/octet-stream")
	}
	first := []byte{0, 1, 2, 255}
	initial, err := upload(doc.Version, first)
	if err != nil {
		t.Fatal(err)
	}
	firstKey := initial.DraftRevision.FileKey
	checkCommitted(firstKey, first, doc.Version+1)
	download(first)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := client.StatObject(ctx, documentTestBucket, firstKey, minio.StatObjectOptions{}); err != nil {
		t.Fatal(err)
	}

	rejected := []byte{9, 0, 8}
	var rejectedKey string
	observed := observedDocumentPut{area, func(ctx context.Context, key string) error {
		obj, err := client.GetObject(ctx, documentTestBucket, key, minio.GetObjectOptions{})
		if err != nil {
			return err
		}
		data, err := io.ReadAll(obj)
		closeErr := obj.Close()
		if err != nil || closeErr != nil || !bytes.Equal(data, rejected) {
			return fmt.Errorf("read new object before PG switch: bytes=%x err=%v close=%v", data, err, closeErr)
		}
		rejectedKey = key
		return nil
	}}
	staleWriter := NewDocumentService(repo, repository.NewTenantReferenceRepository(db), observed, DocumentStorageOptions{MaxFileSize: 1024, Timeout: 10 * time.Second})
	_, err = staleWriter.UploadFile(id, revision, tenant, 1, doc.Version, "document.bin", bytes.NewReader(rejected), int64(len(rejected)), "application/octet-stream")
	staleWriter.Stop()
	if !errors.Is(err, repository.ErrVersionConflict) || rejectedKey == "" || rejectedKey == firstKey {
		t.Fatalf("stale replacement: key=%q err=%v", rejectedKey, err)
	}
	checkCommitted(firstKey, first, initial.Version)
	download(first)
	assertMissingMinioDocumentObject(t, client, rejectedKey)
	assertNoCleanup := func() {
		t.Helper()
		var pending int64
		prefix := fmt.Sprintf("tenant_%d/documents/%d/", tenant, id)
		if err := db.Model(&models.DocumentFileCleanup{}).Where("starts_with(object_key, ?)", prefix).Count(&pending).Error; err != nil || pending != 0 {
			t.Fatalf("pending cleanup=%d err=%v", pending, err)
		}
	}
	assertNoCleanup()

	second := []byte{3, 0, 254, 4}
	replacement, err := upload(initial.Version, second)
	if err != nil {
		t.Fatal(err)
	}
	secondKey := replacement.DraftRevision.FileKey
	if secondKey == firstKey {
		t.Fatal("replacement reused current key")
	}
	checkCommitted(secondKey, second, initial.Version+1)
	download(second)
	assertMissingMinioDocumentObject(t, client, firstKey)
	assertNoCleanup()

	cleanup := NewCleanupService(db, nil, area)
	stats, err := cleanup.ExecuteCleanup(ctx, uint(tenant), events.CleanupModePhysical, map[string]interface{}{"tenant_id": uint(tenant)})
	if err != nil || stats == nil || len(stats.Errors) > 0 || stats.DeletedPhysicalArtifacts != 1 {
		t.Fatalf("tenant cleanup=%+v err=%v", stats, err)
	}
	assertMissingMinioDocumentObject(t, client, secondKey)
}
