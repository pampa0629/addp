package service

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	minio "github.com/minio/minio-go/v7"
)

const minioBucket = "standard"

// DocumentService 标准文档服务
type DocumentService struct {
	repo        *repository.DocumentRepository
	refs        *repository.TenantReferenceRepository
	minioClient *minio.Client
}

func NewDocumentService(repo *repository.DocumentRepository, refs *repository.TenantReferenceRepository, minioClient *minio.Client) *DocumentService {
	svc := &DocumentService{repo: repo, refs: refs, minioClient: minioClient}
	if minioClient != nil {
		ctx := context.Background()
		exists, _ := minioClient.BucketExists(ctx, minioBucket)
		if !exists {
			_ = minioClient.MakeBucket(ctx, minioBucket, minio.MakeBucketOptions{})
		}
	}
	return svc
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
		TenantID:    tenantID,
		Name:        req.Name,
		DocType:     docType,
		SourceOrg:   req.SourceOrg,
		Version:     req.Version,
		Description: req.Description,
		CreatedBy:   userID,
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
	doc.Version = req.Version
	doc.Description = req.Description
	doc.UpdatedBy = &userID
	if err := s.repo.Update(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *DocumentService) DeleteDocument(id, tenantID int64) error {
	// 删除时同步清理 MinIO 文件
	if s.minioClient != nil {
		doc, err := s.repo.GetByID(id, tenantID)
		if err == nil && doc.FileKey != "" {
			_ = s.minioClient.RemoveObject(context.Background(), minioBucket, doc.FileKey, minio.RemoveObjectOptions{})
		}
	}
	return s.repo.Delete(id, tenantID)
}

// UploadFile 上传文件到 MinIO 并更新文档记录
func (s *DocumentService) UploadFile(docID, tenantID int64, fileName string, content []byte, contentType string) error {
	if s.minioClient == nil {
		return fmt.Errorf("文件存储服务不可用")
	}
	doc, err := s.repo.GetByID(docID, tenantID)
	if err != nil {
		return fmt.Errorf("文档不存在")
	}

	// 清理旧文件
	if doc.FileKey != "" {
		_ = s.minioClient.RemoveObject(context.Background(), minioBucket, doc.FileKey, minio.RemoveObjectOptions{})
	}

	fileKey := fmt.Sprintf("tenant_%d/documents/%d/%s", tenantID, docID, fileName)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	ctx := context.Background()
	_, err = s.minioClient.PutObject(ctx, minioBucket, fileKey, bytes.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("文件上传失败: %w", err)
	}

	doc.FileKey = fileKey
	doc.FileName = fileName
	doc.FileSize = int64(len(content))
	return s.repo.Update(doc)
}

// DownloadFile 从 MinIO 获取文件内容，返回 ReadCloser 和内容类型
func (s *DocumentService) DownloadFile(docID, tenantID int64) (io.ReadCloser, string, int64, error) {
	if s.minioClient == nil {
		return nil, "", 0, fmt.Errorf("文件存储服务不可用")
	}
	doc, err := s.repo.GetByID(docID, tenantID)
	if err != nil {
		return nil, "", 0, fmt.Errorf("文档不存在")
	}
	if doc.FileKey == "" {
		return nil, "", 0, fmt.Errorf("该文档尚未上传文件")
	}

	ctx := context.Background()
	obj, err := s.minioClient.GetObject(ctx, minioBucket, doc.FileKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", 0, fmt.Errorf("获取文件失败: %w", err)
	}
	return obj, doc.FileName, doc.FileSize, nil
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
	return s.repo.SetMappings(docID, req.ElementIDs, req.GlossaryIDs, req.MetricIDs, locations)
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

func (s *DocumentService) CreateAndLinkElement(req *models.CreateDocumentRequest, tenantID, userID, elementID int64) (*models.Document, error) {
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return nil, err
	}
	doc := newDocument(req, tenantID, userID)
	mapping := &models.DocumentElementMapping{DocumentID: doc.ID, ElementID: elementID}
	if err := s.repo.CreateWithMapping(doc, mapping); err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *DocumentService) CreateAndLinkGlossary(req *models.CreateDocumentRequest, tenantID, userID, glossaryID int64) (*models.Document, error) {
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return nil, err
	}
	doc := newDocument(req, tenantID, userID)
	mapping := &models.DocumentGlossaryMapping{DocumentID: doc.ID, GlossaryID: glossaryID}
	if err := s.repo.CreateWithMapping(doc, mapping); err != nil {
		return nil, err
	}
	return doc, nil
}

func (s *DocumentService) CreateAndLinkMetric(req *models.CreateDocumentRequest, tenantID, userID, metricID int64) (*models.Document, error) {
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return nil, err
	}
	doc := newDocument(req, tenantID, userID)
	mapping := &models.DocumentMetricMapping{DocumentID: doc.ID, MetricID: metricID}
	if err := s.repo.CreateWithMapping(doc, mapping); err != nil {
		return nil, err
	}
	return doc, nil
}

// ===== 关联已有文档到标准项 =====

func (s *DocumentService) LinkDocToElement(docID, tenantID, elementID int64) error {
	// 验证文档存在且属于该租户
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return fmt.Errorf("文档不存在")
	}
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return err
	}
	return s.repo.AddElementMapping(docID, elementID)
}

func (s *DocumentService) LinkDocToGlossary(docID, tenantID, glossaryID int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return fmt.Errorf("文档不存在")
	}
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return err
	}
	return s.repo.AddGlossaryMapping(docID, glossaryID)
}

func (s *DocumentService) LinkDocToMetric(docID, tenantID, metricID int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return fmt.Errorf("文档不存在")
	}
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return err
	}
	return s.repo.AddMetricMapping(docID, metricID)
}

// ===== 解除文档与标准项的关联 =====

func (s *DocumentService) UnlinkDocFromElement(docID, tenantID, elementID int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return fmt.Errorf("文档不存在")
	}
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return err
	}
	return s.repo.RemoveElementMapping(docID, elementID)
}

func (s *DocumentService) UnlinkDocFromGlossary(docID, tenantID, glossaryID int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return fmt.Errorf("文档不存在")
	}
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return err
	}
	return s.repo.RemoveGlossaryMapping(docID, glossaryID)
}

func (s *DocumentService) UnlinkDocFromMetric(docID, tenantID, metricID int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return fmt.Errorf("文档不存在")
	}
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return err
	}
	return s.repo.RemoveMetricMapping(docID, metricID)
}
