package service

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
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
var ErrInvalidRange = errors.New("invalid range")
var ErrDownloadNotSupported = errors.New("download not supported")

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

	opts := commonClient.MetaScanOptions{EngineID: engineID, ScanDepth: "deep", TriggerType: "manual", Source: commonExecution.ModuleManager, Force: true}
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

// StreamStorageContent 对存储叶子内容做流式传输，支持 HTTP Range 请求。
func (s *MetadataService) StreamStorageContent(
	ctx context.Context,
	resourceID uint,
	storageRef string,
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
	factsProvider, _ := pl.(plugin.CatalogFactsProvider)
	contentReader, _ := pl.(plugin.ContentReadableProvider)
	rangeReader, _ := pl.(plugin.RangeReadableProvider)
	if contentReader == nil {
		return nil, 0, "", "", fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	itemPath, displayPath, err := streamStorageRefPath(resource.EngineType, resource.ID, storageRef)
	if err != nil {
		return nil, 0, "", "", err
	}

	meta, err := streamCatalogFacts(ctx, factsProvider, connInfo, itemPath, displayPath)
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to stat storage content: %w", err)
	}

	contentType := objectcontent.InferContentType(displayPath, meta.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	readOptions, contentLength, contentRange, err := parseStorageRange(rangeHeader, meta.Size)
	if err != nil {
		return nil, 0, "", "", err
	}

	// 获取对象流
	var reader io.ReadCloser
	if readOptions.Length > 0 && rangeReader != nil {
		reader, err = rangeReader.OpenRange(ctx, connInfo, itemPath, readOptions)
	} else {
		reader, err = contentReader.OpenContent(ctx, connInfo, itemPath, readOptions)
	}
	if err != nil {
		return nil, 0, "", "", fmt.Errorf("failed to get storage content: %w", err)
	}

	return reader, contentLength, contentRange, contentType, nil
}

func (s *MetadataService) ResolveStorageDownloadPlan(ctx context.Context, resourceID uint, storageRef string, tenantID *uint) (*models.DownloadPlan, error) {
	resource, err := s.getResourceForTenant(resourceID, tenantID)
	if err != nil {
		return nil, ErrEngineAccessDenied
	}
	_, displayPath, err := streamStorageRefPath(resource.EngineType, resource.ID, storageRef)
	if err != nil {
		return nil, err
	}

	item := s.downloadMetaItem(resource.ID, displayPath, tenantID)
	if item == nil && storageRefRequiresMetaRefs(displayPath) {
		return nil, fmt.Errorf("%w: multi-ref storage item requires scanned meta item refs", ErrDownloadNotSupported)
	}
	descriptor := downloadItemDescriptor(item)
	refs := downloadRefsFromDescriptor(descriptor)
	if descriptor.Layout == format.LayoutMulti && len(refs) == 0 {
		return nil, fmt.Errorf("%w: multi item is missing item.refs; rescan the node to rebuild related refs", ErrDownloadNotSupported)
	}
	refs = normalizeDownloadRefs(resource.EngineType, displayPath, refs)
	if len(refs) == 0 {
		refs = []models.DownloadRef{{
			StorageRef: displayPath,
			Role:       "main",
			Required:   true,
			Primary:    true,
			FileName:   path.Base(displayPath),
		}}
	}
	if err := validateDownloadPlanRefs(refs); err != nil {
		return nil, err
	}
	if err := validateDownloadRefs(resource.EngineType, resource.ID, refs); err != nil {
		return nil, err
	}

	kind := models.DownloadKindStream
	fileName := path.Base(displayPath)
	contentType := objectcontent.InferContentType(displayPath, "")
	if len(refs) > 1 {
		kind = models.DownloadKindBundle
		fileName = bundleFileName(displayPath, descriptor)
		contentType = "application/zip"
	} else if refs[0].FileName != "" {
		fileName = refs[0].FileName
	}
	if fileName == "." || fileName == "/" || fileName == "" {
		fileName = "download"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return &models.DownloadPlan{
		Kind:        kind,
		URL:         storageDownloadURL(resource.ID, displayPath),
		FileName:    fileName,
		ContentType: contentType,
		Refs:        refs,
	}, nil
}

func (s *MetadataService) StreamStorageDownloadPlan(ctx context.Context, resourceID uint, plan *models.DownloadPlan, tenantID *uint, writer io.Writer) error {
	if plan == nil {
		return ErrDownloadNotSupported
	}
	if plan.Kind == models.DownloadKindBundle {
		entries, err := s.openDownloadBundleEntries(ctx, resourceID, plan, tenantID)
		if err != nil {
			return err
		}
		return writeDownloadBundle(entries, writer)
	}
	if plan.Kind != models.DownloadKindStream || len(plan.Refs) == 0 {
		return ErrDownloadNotSupported
	}
	reader, err := s.openDownloadRef(ctx, resourceID, plan.Refs[0], tenantID)
	if err != nil {
		return err
	}
	defer reader.Close()
	_, err = io.Copy(writer, reader)
	return err
}

func (s *MetadataService) OpenStorageDownloadPlan(ctx context.Context, resourceID uint, plan *models.DownloadPlan, tenantID *uint) (io.ReadCloser, error) {
	if plan == nil {
		return nil, ErrDownloadNotSupported
	}
	if plan.Kind == models.DownloadKindBundle {
		entries, err := s.openDownloadBundleEntries(ctx, resourceID, plan, tenantID)
		if err != nil {
			return nil, err
		}
		reader, writer := io.Pipe()
		go func() {
			err := writeDownloadBundle(entries, writer)
			_ = writer.CloseWithError(err)
		}()
		return reader, nil
	}
	if plan.Kind != models.DownloadKindStream || len(plan.Refs) == 0 {
		return nil, ErrDownloadNotSupported
	}
	return s.openDownloadRef(ctx, resourceID, plan.Refs[0], tenantID)
}

type downloadBundleEntry struct {
	name   string
	reader io.ReadCloser
}

func (s *MetadataService) openDownloadBundleEntries(ctx context.Context, resourceID uint, plan *models.DownloadPlan, tenantID *uint) ([]downloadBundleEntry, error) {
	usedNames := map[string]int{}
	entries := make([]downloadBundleEntry, 0, len(plan.Refs))
	for _, ref := range plan.Refs {
		reader, err := s.openDownloadRef(ctx, resourceID, ref, tenantID)
		if err != nil {
			if ref.Required {
				closeDownloadBundleEntries(entries)
				return nil, err
			}
			continue
		}
		entries = append(entries, downloadBundleEntry{
			name:   uniqueZipEntryName(ref.FileName, usedNames),
			reader: reader,
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: no readable refs for bundle", ErrDownloadNotSupported)
	}
	return entries, nil
}

func (s *MetadataService) openDownloadRef(ctx context.Context, resourceID uint, ref models.DownloadRef, tenantID *uint) (io.ReadCloser, error) {
	reader, _, _, _, err := s.StreamStorageContent(ctx, resourceID, ref.StorageRef, "", tenantID)
	return reader, err
}

func writeDownloadBundle(entries []downloadBundleEntry, writer io.Writer) error {
	zipWriter := zip.NewWriter(writer)
	defer zipWriter.Close()

	for _, ref := range entries {
		entry, err := zipWriter.Create(ref.name)
		if err != nil {
			closeDownloadBundleEntries(entries)
			return err
		}
		_, copyErr := io.Copy(entry, ref.reader)
		closeErr := ref.reader.Close()
		if copyErr != nil {
			closeDownloadBundleEntries(entries)
			return copyErr
		}
		if closeErr != nil {
			closeDownloadBundleEntries(entries)
			return closeErr
		}
	}
	return nil
}

func closeDownloadBundleEntries(entries []downloadBundleEntry) {
	for _, entry := range entries {
		if entry.reader != nil {
			_ = entry.reader.Close()
		}
	}
}

func (s *MetadataService) downloadMetaItem(engineID uint, storageRef string, tenantID *uint) *commonModels.MetaItem {
	if s.metaClient == nil {
		return nil
	}
	if tenantID != nil {
		s.metaClient.SetTenantID(tenantID)
	}
	item, err := s.metaClient.GetItemByCatalogPath(engineID, storageRef)
	if err == nil && item != nil {
		return item
	}
	return nil
}

func downloadRefsFromDescriptor(descriptor dataitem.ItemDescriptor) []models.DownloadRef {
	if len(descriptor.Refs) == 0 {
		return nil
	}
	refs := make([]models.DownloadRef, 0, len(descriptor.Refs))
	for _, itemRef := range descriptor.Refs {
		storageRef := strings.Trim(strings.TrimSpace(itemRef.Path), "/")
		if storageRef == "" {
			continue
		}
		refs = append(refs, models.DownloadRef{
			StorageRef: storageRef,
			Role:       strings.TrimSpace(itemRef.Role),
			Required:   itemRef.Required,
			Primary:    itemRef.Primary,
			FileName:   path.Base(storageRef),
		})
	}
	return refs
}

func storageRefRequiresMetaRefs(storageRef string) bool {
	formatType := format.DetectFormat(storageRef, nil)
	if formatType == "" || formatType == format.FormatUnknown {
		return false
	}
	descriptor, ok := format.GetFormatDescriptor(formatType)
	return ok && format.HasLayout(descriptor.Layouts, format.LayoutMulti)
}

func downloadItemDescriptor(item *commonModels.MetaItem) dataitem.ItemDescriptor {
	if item == nil {
		return dataitem.ItemDescriptor{}
	}
	return dataitem.DescriptorFromAttributes(item.Attributes)
}

func normalizeDownloadRefs(engineType, primaryStorageRef string, refs []models.DownloadRef) []models.DownloadRef {
	primaryStorageRef = strings.Trim(primaryStorageRef, "/")
	result := make([]models.DownloadRef, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		storageRef := strings.Trim(ref.StorageRef, "/")
		if storageRef == "" {
			continue
		}
		if catalogutil.ItemTermMatches(engineType, plugin.CatalogTermObject) {
			storageRef = normalizeObjectDownloadRef(primaryStorageRef, storageRef)
		}
		if seen[storageRef] {
			continue
		}
		seen[storageRef] = true
		ref.StorageRef = storageRef
		if strings.TrimSpace(ref.FileName) == "" {
			ref.FileName = path.Base(storageRef)
		}
		result = append(result, ref)
	}
	return result
}

func validateDownloadPlanRefs(refs []models.DownloadRef) error {
	if len(refs) == 0 {
		return fmt.Errorf("%w: download refs are empty", ErrDownloadNotSupported)
	}
	if len(refs) == 1 {
		return nil
	}
	primaryCount := 0
	for _, ref := range refs {
		if ref.Primary {
			primaryCount++
		}
	}
	if primaryCount != 1 {
		return fmt.Errorf("%w: multi item requires exactly one primary ref", ErrDownloadNotSupported)
	}
	return nil
}

func normalizeObjectDownloadRef(primaryStorageRef, ref string) string {
	ref = strings.Trim(ref, "/")
	parts := strings.SplitN(primaryStorageRef, "/", 2)
	if len(parts) != 2 || strings.HasPrefix(ref, parts[0]+"/") {
		return ref
	}
	if strings.Contains(ref, "/") {
		return strings.Trim(parts[0], "/") + "/" + ref
	}
	dir := path.Dir(parts[1])
	if dir == "." || dir == "/" {
		return strings.Trim(parts[0], "/") + "/" + ref
	}
	return strings.Trim(parts[0], "/") + "/" + strings.Trim(dir, "/") + "/" + ref
}

func validateDownloadRefs(engineType string, engineID uint, refs []models.DownloadRef) error {
	for _, ref := range refs {
		if _, _, err := streamStorageRefPath(engineType, engineID, ref.StorageRef); err != nil {
			if ref.Required {
				return err
			}
		}
	}
	return nil
}

func bundleFileName(storageRef string, descriptor dataitem.ItemDescriptor) string {
	base := strings.TrimSuffix(path.Base(storageRef), path.Ext(storageRef))
	formatName := descriptor.Format
	if formatName != "" && !strings.Contains(strings.ToLower(base), strings.ToLower(formatName)) {
		base = base + "." + formatName
	}
	if strings.TrimSpace(base) == "" {
		base = "download"
	}
	return base + ".zip"
}

func storageDownloadURL(engineID uint, storageRef string) string {
	return fmt.Sprintf("/api/v1/manager/storage-download?engine_id=%d&storage_ref=%s", engineID, url.QueryEscape(storageRef))
}

func uniqueZipEntryName(name string, used map[string]int) string {
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" || name == "." {
		name = "file"
	}
	count := used[name]
	used[name] = count + 1
	if count == 0 {
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%d%s", base, count+1, ext)
}

func parseStorageRange(rangeHeader string, size int64) (plugin.ReadOptions, int64, string, error) {
	if strings.TrimSpace(rangeHeader) == "" {
		return plugin.ReadOptions{}, size, "", nil
	}
	if size <= 0 {
		return plugin.ReadOptions{}, 0, "", fmt.Errorf("%w: empty content", ErrInvalidRange)
	}

	rangeHeader = strings.TrimSpace(rangeHeader)
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return plugin.ReadOptions{}, 0, "", fmt.Errorf("%w: unsupported range unit", ErrInvalidRange)
	}
	spec := strings.TrimSpace(strings.TrimPrefix(rangeHeader, "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return plugin.ReadOptions{}, 0, "", fmt.Errorf("%w: unsupported range spec", ErrInvalidRange)
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return plugin.ReadOptions{}, 0, "", fmt.Errorf("%w: malformed range", ErrInvalidRange)
	}

	startText := strings.TrimSpace(parts[0])
	endText := strings.TrimSpace(parts[1])
	if startText == "" && endText == "" {
		return plugin.ReadOptions{}, 0, "", fmt.Errorf("%w: malformed range", ErrInvalidRange)
	}

	var start, end int64
	if startText == "" {
		suffixLength, err := strconv.ParseInt(endText, 10, 64)
		if err != nil || suffixLength <= 0 {
			return plugin.ReadOptions{}, 0, "", fmt.Errorf("%w: invalid suffix length", ErrInvalidRange)
		}
		if suffixLength >= size {
			start = 0
		} else {
			start = size - suffixLength
		}
		end = size - 1
	} else {
		parsedStart, err := strconv.ParseInt(startText, 10, 64)
		if err != nil || parsedStart < 0 {
			return plugin.ReadOptions{}, 0, "", fmt.Errorf("%w: invalid start", ErrInvalidRange)
		}
		start = parsedStart
		if endText == "" {
			end = size - 1
		} else {
			parsedEnd, err := strconv.ParseInt(endText, 10, 64)
			if err != nil || parsedEnd < 0 {
				return plugin.ReadOptions{}, 0, "", fmt.Errorf("%w: invalid end", ErrInvalidRange)
			}
			end = parsedEnd
		}
	}

	if start >= size || start > end {
		return plugin.ReadOptions{}, 0, "", fmt.Errorf("%w: unsatisfiable range", ErrInvalidRange)
	}
	if end >= size {
		end = size - 1
	}

	length := end - start + 1
	contentRange := fmt.Sprintf("bytes %d-%d/%d", start, end, size)
	return plugin.ReadOptions{Offset: start, Length: length}, length, contentRange, nil
}

func streamStorageRefPath(engineType string, engineID uint, storageRef string) (plugin.CatalogPath, string, error) {
	storageRef = strings.Trim(storageRef, "/")
	if storageRef == "" {
		return plugin.CatalogPath{}, "", fmt.Errorf("storage ref is empty")
	}
	if catalogutil.ItemTermMatches(engineType, plugin.CatalogTermObject) {
		parts := strings.SplitN(storageRef, "/", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return plugin.CatalogPath{}, "", fmt.Errorf("invalid storage ref for object catalog: %s", storageRef)
		}
		return plugin.ObjectItemPath(engineID, parts[0], parts[1]), storageRef, nil
	}
	if catalogutil.ItemTermMatches(engineType, plugin.CatalogTermFile) {
		return plugin.FileItemPath(engineID, storageRef), storageRef, nil
	}
	return plugin.CatalogPath{}, "", fmt.Errorf("resource type %s does not support storage streaming", engineType)
}

func streamCatalogFacts(ctx context.Context, factsProvider plugin.CatalogFactsProvider, connInfo plugin.ConnectionInfo, itemPath plugin.CatalogPath, fallbackPath string) (*plugin.StorageObjectFacts, error) {
	if factsProvider == nil {
		return &plugin.StorageObjectFacts{Name: path.Base(fallbackPath), Path: fallbackPath}, nil
	}
	item, err := factsProvider.DescribeCatalogFacts(ctx, connInfo, itemPath, plugin.CatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	return catalogutil.CatalogFactsToStorageObjectFacts(item, fallbackPath), nil
}
