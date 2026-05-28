package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/catalogutil"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/repository"
)

type MetadataService struct {
	metadataRepo *repository.MetadataRepository
	systemClient *commonClient.SystemClient
	metaClient   *commonClient.MetaClient
	previews     *preview.PreviewRegistry
	content      *objectcontent.ObjectContentRegistry
}

var ErrEngineAccessDenied = errors.New("engine not accessible for current tenant")

func NewMetadataService(metadataRepo *repository.MetadataRepository, systemClient *commonClient.SystemClient, metaClient *commonClient.MetaClient, previewRegistry *preview.PreviewRegistry, contentRegistry *objectcontent.ObjectContentRegistry) *MetadataService {
	pr := previewRegistry
	if pr == nil {
		pr = preview.NewPreviewRegistry()
	}
	cr := contentRegistry
	if cr == nil {
		cr = objectcontent.NewObjectContentRegistry()
	}
	return &MetadataService{
		metadataRepo: metadataRepo,
		systemClient: systemClient,
		metaClient:   metaClient,
		previews:     pr,
		content:      cr,
	}
}

// PreviewRegistry 返回预览插件注册表
func (s *MetadataService) PreviewRegistry() *preview.PreviewRegistry {
	return s.previews
}

// ContentRegistry 返回内容处理器注册表
func (s *MetadataService) ContentRegistry() *objectcontent.ObjectContentRegistry {
	return s.content
}

func (s *MetadataService) RefreshItem(ctx context.Context, tenantID *uint, engineID uint, req *models.MetaManualScanRequest) (*models.MetaScanResponse, error) {
	if s.metaClient == nil {
		return nil, fmt.Errorf("meta client not initialized")
	}

	opts := commonClient.MetaScanOptions{EngineID: engineID, Force: true}
	if req != nil {
		if req.NodeID > 0 {
			return nil, fmt.Errorf("item refresh requires item_id; use node refresh for node_id")
		}
		if req.ItemID > 0 {
			opts.ItemID = req.ItemID
		}
	}
	if opts.ItemID == 0 {
		return nil, fmt.Errorf("item_id is required")
	}
	if tenantID != nil {
		s.metaClient.SetTenantID(tenantID)
	}

	result, err := s.metaClient.RefreshItem(opts.ItemID, opts)
	if err != nil {
		return nil, err
	}
	resp := &models.MetaScanResponse{
		Status:              result.Status,
		Message:             result.Message,
		CatalogNodesScanned: result.CatalogNodesScanned,
		ItemsScanned:        result.ItemsScanned,
		FieldsScanned:       result.FieldsScanned,
		DurationMs:          result.DurationMs,
		StartedAt:           result.StartedAt,
	}
	if result.Extraction != nil {
		resp.Extraction = &models.MetaExtractionScanStats{
			Documents:   result.Extraction.Documents,
			Extracted:   result.Extraction.Extracted,
			Unsupported: result.Extraction.Unsupported,
			Failed:      result.Extraction.Failed,
			Indexed:     result.Extraction.Indexed,
			IndexFailed: result.Extraction.IndexFailed,
		}
	}
	return resp, nil
}

func resourceAccessible(resource *models.Engine, tenantID *uint) bool {
	if resource == nil || !resource.IsActive {
		return false
	}
	if tenantID == nil {
		return true
	}
	if resource.TenantID == nil {
		return false
	}
	return *resource.TenantID == *tenantID
}

// getResource 通过 System 服务获取解密后的资源信息
func (s *MetadataService) getResource(engineID uint) (*models.Engine, error) {
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	sysResource, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from system: %w", err)
	}

	return convertResource(sysResource), nil
}

func (s *MetadataService) getResourceForTenant(engineID uint, tenantID *uint) (*models.Engine, error) {
	resource, err := s.getResource(engineID)
	if err != nil {
		return nil, err
	}
	if !resourceAccessible(resource, tenantID) {
		return nil, ErrEngineAccessDenied
	}
	return resource, nil
}

func convertResource(src *commonModels.Engine) *models.Engine {
	if src == nil {
		return nil
	}

	var tenantIDPtr *uint
	if src.TenantID != nil && *src.TenantID != 0 {
		tenantID := *src.TenantID
		tenantIDPtr = &tenantID
	}

	connInfo := make(models.ConnectionInfo, len(src.ConnectionInfo))
	for k, v := range src.ConnectionInfo {
		connInfo[k] = v
	}

	return &models.Engine{
		ID:             src.ID,
		Name:           src.Name,
		EngineType:     src.EngineType,
		ConnectionInfo: connInfo,
		Description:    src.Description,
		CreatedBy:      src.CreatedBy,
		TenantID:       tenantIDPtr,
		IsActive:       src.IsActive,
	}
}

// StreamObject 对象内容流式传输，支持 HTTP Range 请求。
func (s *MetadataService) StreamObject(
	ctx context.Context,
	resourceID uint,
	objectKey string,
	rangeHeader string,
	tenantID *uint,
) (io.ReadCloser, int64, string, string, error) {
	// 获取resource信息
	resource, err := s.getResourceForTenant(resourceID, tenantID)
	if err != nil {
		return nil, 0, "", "", ErrEngineAccessDenied
	}

	pl, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	metadataProvider, _ := pl.(plugin.ItemMetadataProvider)
	contentReader, _ := pl.(plugin.ContentReadableProvider)
	rangeReader, _ := pl.(plugin.RangeReadableProvider)
	if contentReader == nil {
		return nil, 0, "", "", fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	itemPath, displayPath, err := streamCatalogItemPath(resource.EngineType, resource.ID, objectKey)
	if err != nil {
		return nil, 0, "", "", err
	}

	meta, err := streamItemMetadata(ctx, metadataProvider, connInfo, itemPath, displayPath)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to stat object: %w", err)
	}

	contentType := objectcontent.InferContentType(displayPath, meta.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	var contentLength int64
	var contentRange string
	var readOptions plugin.ReadOptions

	if rangeHeader != "" {
		// 解析Range header (格式: "bytes=start-end")
		rangeHeader = strings.TrimPrefix(rangeHeader, "bytes=")
		rangeParts := strings.Split(rangeHeader, "-")

		if len(rangeParts) == 2 {
			start, err := strconv.ParseInt(rangeParts[0], 10, 64)
			if err != nil {
				start = 0
			}

			var end int64
			if rangeParts[1] != "" {
				end, err = strconv.ParseInt(rangeParts[1], 10, 64)
				if err != nil {
					end = meta.Size - 1
				}
			} else {
				end = meta.Size - 1
			}

			// 确保范围有效
			if start < 0 {
				start = 0
			}
			if end >= meta.Size {
				end = meta.Size - 1
			}
			if start > end {
				return nil, 0, "", "", fmt.Errorf("invalid range: start > end")
			}

			contentLength = end - start + 1
			contentRange = fmt.Sprintf("bytes %d-%d/%d", start, end, meta.Size)
			readOptions = plugin.ReadOptions{Offset: start, Length: contentLength}
		}
	} else {
		// 没有Range，返回完整内容
		contentLength = meta.Size
		contentRange = ""
	}

	// 获取对象流
	var reader io.ReadCloser
	if readOptions.Length > 0 && rangeReader != nil {
		reader, err = rangeReader.OpenRange(ctx, connInfo, itemPath, readOptions)
	} else {
		reader, err = contentReader.OpenContent(ctx, connInfo, itemPath, readOptions)
	}
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to get object: %w", err)
	}

	return reader, contentLength, contentRange, contentType, nil
}

func streamCatalogItemPath(engineType string, engineID uint, objectKey string) (plugin.CatalogPath, string, error) {
	objectKey = strings.Trim(objectKey, "/")
	if objectKey == "" {
		return plugin.CatalogPath{}, "", fmt.Errorf("object key is empty")
	}
	if catalogutil.ItemTermMatches(engineType, plugin.CatalogTermObject) {
		parts := strings.SplitN(objectKey, "/", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return plugin.CatalogPath{}, "", fmt.Errorf("invalid object key format: %s", objectKey)
		}
		return plugin.ObjectItemPath(engineID, parts[0], parts[1]), objectKey, nil
	}
	if catalogutil.ItemTermMatches(engineType, plugin.CatalogTermFile) {
		return plugin.FileItemPath(engineID, objectKey), objectKey, nil
	}
	return plugin.CatalogPath{}, "", fmt.Errorf("resource type %s does not support object streaming", engineType)
}

func streamItemMetadata(ctx context.Context, metadataProvider plugin.ItemMetadataProvider, connInfo plugin.ConnectionInfo, itemPath plugin.CatalogPath, fallbackPath string) (*plugin.FileMetadata, error) {
	if metadataProvider == nil {
		return &plugin.FileMetadata{Name: path.Base(fallbackPath), Path: fallbackPath}, nil
	}
	item, err := metadataProvider.DescribeItem(ctx, connInfo, itemPath, plugin.MetadataOptions{})
	if err != nil {
		return nil, err
	}
	return catalogutil.ItemMetadataToFileMetadata(item, fallbackPath), nil
}
