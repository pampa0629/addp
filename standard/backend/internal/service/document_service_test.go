package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
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
	name, err := sanitizeDocumentFileName(`../nested\report.md`)
	if err != nil || name != "report.md" {
		t.Fatalf("sanitizeDocumentFileName() = %q, %v", name, err)
	}
	if _, err := sanitizeDocumentFileName("line\nbreak.md"); !errors.Is(err, ErrDocumentFileNameInvalid) {
		t.Fatalf("unsafe file name error = %v", err)
	}
}

func TestSplitMarkdownSectionsCarriesAbsoluteLineNumbers(t *testing.T) {
	sections := splitMarkdownSections("# Outdoor\n\n说明\n## 指标\n实际参加活动数")
	if len(sections) != 2 {
		t.Fatalf("sections = %#v, want 2", sections)
	}
	if sections[1].SectionPath != "Outdoor / 指标" || sections[1].StartLine != 4 || sections[1].EndLine != 5 {
		t.Fatalf("metric section metadata = %#v", sections[1])
	}
	if sections[1].Text != "L4: ## 指标\nL5: 实际参加活动数" {
		t.Fatalf("metric numbered text = %q", sections[1].Text)
	}
}

func TestUploadFileReplacesOnlyDraftRevisionObject(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	doc, revision := seedDocumentDraft(t, repo, 7, "tenant_7/documents/1/old.md")
	store := &fakeDocumentObjectStore{objects: map[string][]byte{revision.FileKey: []byte("old")}}
	svc := &DocumentService{repo: repo, objectStore: store, maxFileSize: 10, timeout: time.Second}
	if _, err := svc.UploadFile(doc.ID, revision.ID, doc.TenantID, 1, doc.Version, "new.md", strings.NewReader("new content"), 11, "text/markdown"); !errors.Is(err, ErrDocumentFileTooLarge) {
		t.Fatalf("oversized upload error = %v", err)
	}
	updated, err := svc.UploadFile(doc.ID, revision.ID, doc.TenantID, 1, doc.Version, "new.md", strings.NewReader("new"), 3, "text/markdown")
	if err != nil {
		t.Fatalf("replacement upload: %v", err)
	}
	if updated.DraftRevision == nil || updated.DraftRevision.FileName != "new.md" || updated.DraftRevision.FileKey == revision.FileKey || updated.DraftRevision.ContentSHA256 == "" {
		t.Fatalf("updated aggregate = %+v", updated)
	}
	if len(store.removedKeys) != 1 || store.removedKeys[0] != revision.FileKey {
		t.Fatalf("removed keys = %v", store.removedKeys)
	}
}

func TestExtractCandidatesPersistsCanonicalOutdoorEvidence(t *testing.T) {
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"candidate_type":"metric","code":"outdoor_participation_count","name":"实际参加活动数","definition":"人员实际参加的有效活动去重数","payload":{"aggregation":"count"},"evidences":[{"section_path":"Outdoor / 指标","start_line":3,"end_line":4}]},{"candidate_type":"glossary","code":"wrong_section","name":"错误章节","definition":"证据路径与行号不匹配的候选必须被丢弃","payload":{},"evidences":[{"section_path":"Outdoor","start_line":3,"end_line":4}]}]}`))
	}))
	defer server.Close()
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	doc, revision := seedDocumentDraft(t, repo, 8, "outdoor.md")
	content := "# Outdoor\n\n## 指标\n实际参加活动数只统计有效活动。\n"
	store := &fakeDocumentObjectStore{objects: map[string][]byte{revision.FileKey: []byte(content)}}
	svc := &DocumentService{repo: repo, objectStore: store, maxFileSize: 1024, timeout: time.Second, copilotURL: server.URL, serviceTokenSource: commonclient.ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		if tenantID != 8 {
			t.Fatalf("tenantID=%d", tenantID)
		}
		return "service-token", nil
	}), httpClient: server.Client()}
	extraction, err := svc.ExtractCandidates(context.Background(), doc.ID, revision.ID, doc.TenantID, 9, doc.Version)
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if gotAuthorization != "Bearer service-token" {
		t.Fatalf("Authorization=%q", gotAuthorization)
	}
	if len(extraction.Candidates) != 1 || len(extraction.Candidates[0].Evidences) != 1 {
		t.Fatalf("extraction=%+v", extraction)
	}
	if extraction.Candidates[0].Payload["aggregation"] != "count" {
		t.Fatalf("candidate payload=%+v", extraction.Candidates[0].Payload)
	}
	evidence := extraction.Candidates[0].Evidences[0]
	if evidence.Excerpt != "## 指标\n实际参加活动数只统计有效活动。" || evidence.ExcerptHash == "" {
		t.Fatalf("evidence=%+v", evidence)
	}
	loaded, err := repo.ListExtractions(doc.ID, doc.TenantID)
	if err != nil || len(loaded) != 1 || len(loaded[0].Candidates) != 1 {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	updated, err := repo.GetByID(doc.ID, doc.TenantID)
	if err != nil || updated.Version != 2 {
		t.Fatalf("document version=%+v err=%v", updated, err)
	}
}

func TestFileCleanupFailureStaysQueuedForRetry(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	cleanup, err := repo.EnqueueFileCleanup("stale.md")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeDocumentObjectStore{objects: map[string][]byte{"stale.md": []byte("stale")}, removeErr: errors.New("minio unavailable")}
	svc := &DocumentService{repo: repo, objectStore: store, timeout: time.Second}
	svc.tryFileCleanup(*cleanup)
	var queued models.DocumentFileCleanup
	if err := db.First(&queued, cleanup.ID).Error; err != nil {
		t.Fatal(err)
	}
	if queued.Attempts != 1 || queued.LastError == "" {
		t.Fatalf("queued=%+v", queued)
	}
	store.removeErr = nil
	svc.tryFileCleanup(queued)
	if err := db.First(&queued, cleanup.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("completed cleanup error=%v", err)
	}
}

func seedDocumentDraft(t *testing.T, repo *repository.DocumentRepository, tenantID int64, fileKey string) (*models.Document, *models.DocumentRevision) {
	t.Helper()
	doc := &models.Document{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: "outdoor_" + time.Now().Format("150405.000000000"), DocType: "internal", CreatedBy: 1, LifecycleState: "active"}
	revision := &models.DocumentRevision{Name: "Outdoor", ChangeSummary: "initial", CreatedBy: 1, FileKey: fileKey, FileName: "outdoor.md", FileSize: 7, MediaType: "text/markdown", ContentSHA256: "fixture"}
	if err := repo.Create(doc, revision); err != nil {
		t.Fatalf("create document: %v", err)
	}
	return doc, revision
}

func openDocumentServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE standard.documents (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, doc_type TEXT NOT NULL, source_org TEXT, steward_id INTEGER, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX standard.uq_test_documents_tenant_code ON documents (tenant_id, code)`,
		`CREATE TABLE standard.document_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, document_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, version_label TEXT, publish_date DATETIME, description TEXT, file_key TEXT, file_name TEXT, file_size INTEGER, media_type TEXT, content_sha256 TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.document_extractions (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, document_revision_id INTEGER NOT NULL, status TEXT NOT NULL, requested_by INTEGER NOT NULL, created_at DATETIME)`,
		`CREATE TABLE standard.document_extraction_candidates (id INTEGER PRIMARY KEY AUTOINCREMENT, extraction_id INTEGER NOT NULL, candidate_type TEXT NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, payload TEXT, status TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, reviewed_by INTEGER, reviewed_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.document_extraction_evidences (id INTEGER PRIMARY KEY AUTOINCREMENT, candidate_id INTEGER NOT NULL, document_revision_id INTEGER NOT NULL, section_path TEXT NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, excerpt TEXT NOT NULL, excerpt_hash TEXT NOT NULL, created_at DATETIME)`,
		`CREATE TABLE standard.document_file_cleanups (id INTEGER PRIMARY KEY AUTOINCREMENT, object_key TEXT NOT NULL UNIQUE, attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at DATETIME NOT NULL, last_error TEXT, created_at DATETIME, updated_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create document service test schema: %v", err)
		}
	}
	return db
}
