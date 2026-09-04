package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/google/uuid"
	minio "github.com/minio/minio-go/v7"
)

const minioBucket = "standard"

const (
	defaultDocumentMaxFileSize    = 100 * 1024 * 1024
	defaultDocumentStorageTimeout = 30 * time.Second
)

var (
	ErrDocumentStorageUnavailable = errors.New("document storage unavailable")
	ErrDocumentFileTooLarge       = errors.New("document file too large")
	ErrDocumentFileUpload         = errors.New("document file upload failed")
	ErrDocumentFileDownload       = errors.New("document file download failed")
	ErrDocumentFileCleanup        = errors.New("document file cleanup failed")
	ErrDocumentFileNameInvalid    = errors.New("document file name invalid")
)

type DocumentStorageOptions struct {
	MaxFileSize int64
	Timeout     time.Duration
}

type documentContextReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *documentContextReadCloser) Close() error {
	r.cancel()
	return r.ReadCloser.Close()
}

// DocumentService 标准文档服务
type DocumentService struct {
	repo        *repository.DocumentRepository
	refs        *repository.TenantReferenceRepository
	objectStore documentObjectStore
	maxFileSize int64
	timeout     time.Duration
	stopCh      chan struct{}
}

func NewDocumentService(repo *repository.DocumentRepository, refs *repository.TenantReferenceRepository, minioClient *minio.Client, options DocumentStorageOptions) *DocumentService {
	if options.MaxFileSize <= 0 {
		options.MaxFileSize = defaultDocumentMaxFileSize
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultDocumentStorageTimeout
	}
	svc := &DocumentService{
		repo:        repo,
		refs:        refs,
		objectStore: newMinioDocumentObjectStore(minioClient),
		maxFileSize: options.MaxFileSize,
		timeout:     options.Timeout,
		stopCh:      make(chan struct{}),
	}
	if svc.objectStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), svc.timeout)
		defer cancel()
		exists, err := svc.objectStore.BucketExists(ctx, minioBucket)
		if err != nil {
			log.Printf("standard document bucket check failed: %v", err)
		} else if !exists {
			if err := svc.objectStore.MakeBucket(ctx, minioBucket, minio.MakeBucketOptions{}); err != nil {
				log.Printf("standard document bucket creation failed: %v", err)
			}
		}
	}
	go svc.runFileCleanupWorker()
	return svc
}

func (s *DocumentService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	close(s.stopCh)
}

func (s *DocumentService) MaxFileSize() int64 {
	return s.maxFileSize
}

func (s *DocumentService) ListDocuments(tenantID int64, opts repository.ListDocumentOptions) ([]models.Document, int64, error) {
	return s.repo.List(tenantID, opts)
}

func (s *DocumentService) GetDocument(id, tenantID int64) (*models.Document, error) {
	return s.repo.GetByID(id, tenantID)
}

func (s *DocumentService) CreateDocument(req *models.CreateDocumentRequest, tenantID, userID int64) (*models.Document, error) {
	doc := newDocument(req, tenantID, userID)
	if err := s.repo.Create(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func newDocument(req *models.CreateDocumentRequest, tenantID, userID int64) *models.Document {
	docType := req.DocType
	if docType == "" {
		docType = "reference"
	}
	return &models.Document{
		TenantID:        tenantID,
		Name:            req.Name,
		DocType:         docType,
		SourceOrg:       req.SourceOrg,
		DocumentVersion: req.DocumentVersion,
		Description:     req.Description,
		CreatedBy:       userID,
	}
}

func (s *DocumentService) UpdateDocument(id, tenantID, userID int64, req *models.UpdateDocumentRequest) (*models.Document, error) {
	doc, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		doc.Name = req.Name
	}
	if req.DocType != "" {
		doc.DocType = req.DocType
	}
	doc.SourceOrg = req.SourceOrg
	doc.DocumentVersion = req.DocumentVersion
	doc.Description = req.Description
	doc.UpdatedBy = &userID
	if err := s.repo.Update(doc, req.Version); err != nil {
		return nil, err
	}
	return s.repo.GetByID(id, tenantID)
}

func (s *DocumentService) DeleteDocument(id, tenantID int64) error {
	// 先提交数据库删除，数据库失败时保留物理文件；物理清理失败由日志记录并可由回收任务重试。
	cleanup, err := s.repo.Delete(id, tenantID)
	if err != nil {
		return err
	}
	if cleanup != nil {
		s.tryFileCleanup(*cleanup)
	}
	return nil
}

// UploadFile 上传文件到 MinIO 并更新文档记录
func (s *DocumentService) UploadFile(docID, tenantID, version int64, fileName string, content io.Reader, size int64, contentType string) (*models.Document, error) {
	if s.objectStore == nil {
		return nil, ErrDocumentStorageUnavailable
	}
	if size < 0 || size > s.maxFileSize {
		return nil, ErrDocumentFileTooLarge
	}
	doc, err := s.repo.GetByID(docID, tenantID)
	if err != nil {
		return nil, commonapi.ErrNotFound
	}
	fileName, err = sanitizeDocumentFileName(fileName)
	if err != nil {
		return nil, err
	}
	extension := strings.ToLower(filepath.Ext(fileName))
	fileKey := fmt.Sprintf("tenant_%d/documents/%d/%s%s", tenantID, docID, uuid.NewString(), extension)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	_, err = s.objectStore.PutObject(ctx, minioBucket, fileKey, content, size,
		minio.PutObjectOptions{ContentType: contentType})
	cancel()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDocumentFileUpload, err)
	}

	cleanup, err := s.repo.ReplaceFile(doc.ID, tenantID, version, fileKey, fileName, size)
	if err != nil {
		if cleanupErr := s.removeObject(fileKey); cleanupErr != nil {
			if _, queueErr := s.repo.EnqueueFileCleanup(fileKey); queueErr != nil {
				log.Printf("standard document new file cleanup failed and enqueue failed, key=%q: cleanup=%v enqueue=%v", fileKey, cleanupErr, queueErr)
			}
		}
		return nil, err
	}
	if cleanup != nil {
		s.tryFileCleanup(*cleanup)
	}
	return s.repo.GetByID(docID, tenantID)
}

// DownloadFile 从 MinIO 获取文件内容，返回 ReadCloser 和内容类型
func (s *DocumentService) DownloadFile(docID, tenantID int64) (io.ReadCloser, string, int64, error) {
	if s.objectStore == nil {
		return nil, "", 0, ErrDocumentStorageUnavailable
	}
	doc, err := s.repo.GetByID(docID, tenantID)
	if err != nil {
		return nil, "", 0, commonapi.ErrNotFound
	}
	if doc.FileKey == "" {
		return nil, "", 0, commonapi.ErrNotFound
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	if _, err := s.objectStore.StatObject(ctx, minioBucket, doc.FileKey, minio.StatObjectOptions{}); err != nil {
		cancel()
		return nil, "", 0, fmt.Errorf("%w: %v", ErrDocumentFileDownload, err)
	}
	obj, err := s.objectStore.GetObject(ctx, minioBucket, doc.FileKey, minio.GetObjectOptions{})
	if err != nil {
		cancel()
		return nil, "", 0, fmt.Errorf("%w: %v", ErrDocumentFileDownload, err)
	}
	return &documentContextReadCloser{ReadCloser: obj, cancel: cancel}, doc.FileName, doc.FileSize, nil
}

func (s *DocumentService) removeObject(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	return s.objectStore.RemoveObject(ctx, minioBucket, key, minio.RemoveObjectOptions{})
}

func (s *DocumentService) runFileCleanupWorker() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.processDueFileCleanups()
		case <-s.stopCh:
			return
		}
	}
}

func (s *DocumentService) processDueFileCleanups() {
	if s.objectStore == nil {
		return
	}
	cleanups, err := s.repo.ListDueFileCleanups(time.Now(), 100)
	if err != nil {
		log.Printf("standard document file cleanup list failed: %v", err)
		return
	}
	for _, cleanup := range cleanups {
		s.tryFileCleanup(cleanup)
	}
}

func (s *DocumentService) tryFileCleanup(cleanup models.DocumentFileCleanup) {
	if s.objectStore == nil || cleanup.ObjectKey == "" {
		return
	}
	if err := s.removeObject(cleanup.ObjectKey); err != nil {
		attempts := cleanup.Attempts + 1
		backoff := time.Duration(1<<min(attempts, 10)) * time.Minute
		if updateErr := s.repo.FailFileCleanup(cleanup.ID, attempts, time.Now().Add(backoff), err.Error()); updateErr != nil {
			log.Printf("standard document file cleanup retry update failed, key=%q: %v", cleanup.ObjectKey, updateErr)
		}
		return
	}
	if err := s.repo.CompleteFileCleanup(cleanup.ID); err != nil {
		log.Printf("standard document file cleanup completion failed, key=%q: %v", cleanup.ObjectKey, err)
	}
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func sanitizeDocumentFileName(fileName string) (string, error) {
	fileName = strings.TrimSpace(strings.ReplaceAll(fileName, "\\", "/"))
	fileName = filepath.Base(fileName)
	if fileName == "." || fileName == ".." || fileName == "" || strings.ContainsAny(fileName, "\r\n\x00") {
		return "", ErrDocumentFileNameInvalid
	}
	return fileName, nil
}

func (s *DocumentService) GetMappings(docID, tenantID int64) (map[string]interface{}, error) {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return nil, err
	}
	elements, err := s.repo.GetElementMappings(docID, tenantID)
	if err != nil {
		return nil, err
	}
	glossaries, err := s.repo.GetGlossaryMappings(docID, tenantID)
	if err != nil {
		return nil, err
	}
	metrics, err := s.repo.GetMetricMappings(docID, tenantID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"elements":   elements,
		"glossaries": glossaries,
		"metrics":    metrics,
	}, nil
}

func (s *DocumentService) SetMappings(docID, tenantID int64, req *models.SetDocumentMappingsRequest) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return err
	}
	for _, validate := range []func() error{
		func() error { return s.refs.RequireElements(tenantID, req.ElementIDs) },
		func() error { return s.refs.RequireGlossaries(tenantID, req.GlossaryIDs) },
		func() error { return s.refs.RequireMetrics(tenantID, req.MetricIDs) },
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	locations := req.Locations
	if locations == nil {
		locations = map[string]string{}
	}
	return s.repo.SetMappings(docID, tenantID, req.Version, req.ElementIDs, req.GlossaryIDs, req.MetricIDs, locations)
}

// ===== 反向查询：按标准项列出关联文档 =====

func (s *DocumentService) ListByElement(tenantID, elementID int64) ([]models.Document, error) {
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return nil, err
	}
	return s.repo.ListByElementID(tenantID, elementID)
}

func (s *DocumentService) ListByGlossary(tenantID, glossaryID int64) ([]models.Document, error) {
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return nil, err
	}
	return s.repo.ListByGlossaryID(tenantID, glossaryID)
}

func (s *DocumentService) ListByMetric(tenantID, metricID int64) ([]models.Document, error) {
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return nil, err
	}
	return s.repo.ListByMetricID(tenantID, metricID)
}

// ===== 创建文档并关联到标准项（原子操作） =====

func (s *DocumentService) CreateAndLinkElement(req *models.CreateLinkedDocumentRequest, tenantID, userID, elementID int64) (*models.LinkedDocumentMutationResponse, error) {
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return nil, err
	}
	doc := newDocument(&req.CreateDocumentRequest, tenantID, userID)
	mapping := &models.DocumentElementMapping{DocumentID: doc.ID, ElementID: elementID}
	if err := s.repo.CreateWithMappingVersioned(doc, mapping, &models.Element{}, elementID, tenantID, req.Version); err != nil {
		return nil, err
	}
	return &models.LinkedDocumentMutationResponse{Document: doc, Version: req.Version + 1}, nil
}

func (s *DocumentService) CreateAndLinkGlossary(req *models.CreateLinkedDocumentRequest, tenantID, userID, glossaryID int64) (*models.LinkedDocumentMutationResponse, error) {
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return nil, err
	}
	doc := newDocument(&req.CreateDocumentRequest, tenantID, userID)
	mapping := &models.DocumentGlossaryMapping{DocumentID: doc.ID, GlossaryID: glossaryID}
	if err := s.repo.CreateWithMappingVersioned(doc, mapping, &models.Glossary{}, glossaryID, tenantID, req.Version); err != nil {
		return nil, err
	}
	return &models.LinkedDocumentMutationResponse{Document: doc, Version: req.Version + 1}, nil
}

func (s *DocumentService) CreateAndLinkMetric(req *models.CreateLinkedDocumentRequest, tenantID, userID, metricID int64) (*models.LinkedDocumentMutationResponse, error) {
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return nil, err
	}
	doc := newDocument(&req.CreateDocumentRequest, tenantID, userID)
	mapping := &models.DocumentMetricMapping{DocumentID: doc.ID, MetricID: metricID}
	if err := s.repo.CreateWithMappingVersioned(doc, mapping, &models.MetricDefinition{}, metricID, tenantID, req.Version); err != nil {
		return nil, err
	}
	return &models.LinkedDocumentMutationResponse{Document: doc, Version: req.Version + 1}, nil
}

// ===== 关联已有文档到标准项 =====

func (s *DocumentService) LinkDocToElement(docID, tenantID, elementID, version int64) error {
	// 验证文档存在且属于该租户
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return err
	}
	mapping := &models.DocumentElementMapping{DocumentID: docID, ElementID: elementID}
	return s.repo.MutateMappingVersioned(&models.Element{}, elementID, tenantID, version, mapping, true, "document_id = ? AND element_id = ?", docID, elementID)
}

func (s *DocumentService) LinkDocToGlossary(docID, tenantID, glossaryID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return err
	}
	mapping := &models.DocumentGlossaryMapping{DocumentID: docID, GlossaryID: glossaryID}
	return s.repo.MutateMappingVersioned(&models.Glossary{}, glossaryID, tenantID, version, mapping, true, "document_id = ? AND glossary_id = ?", docID, glossaryID)
}

func (s *DocumentService) LinkDocToMetric(docID, tenantID, metricID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return err
	}
	mapping := &models.DocumentMetricMapping{DocumentID: docID, MetricID: metricID}
	return s.repo.MutateMappingVersioned(&models.MetricDefinition{}, metricID, tenantID, version, mapping, true, "document_id = ? AND metric_id = ?", docID, metricID)
}

// ===== 解除文档与标准项的关联 =====

func (s *DocumentService) UnlinkDocFromElement(docID, tenantID, elementID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return err
	}
	return s.repo.MutateMappingVersioned(&models.Element{}, elementID, tenantID, version, &models.DocumentElementMapping{}, false, "document_id = ? AND element_id = ?", docID, elementID)
}

func (s *DocumentService) UnlinkDocFromGlossary(docID, tenantID, glossaryID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return err
	}
	return s.repo.MutateMappingVersioned(&models.Glossary{}, glossaryID, tenantID, version, &models.DocumentGlossaryMapping{}, false, "document_id = ? AND glossary_id = ?", docID, glossaryID)
}

func (s *DocumentService) UnlinkDocFromMetric(docID, tenantID, metricID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return err
	}
	return s.repo.MutateMappingVersioned(&models.MetricDefinition{}, metricID, tenantID, version, &models.DocumentMetricMapping{}, false, "document_id = ? AND metric_id = ?", docID, metricID)
}
