package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"sort"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

var (
	ErrUploadNoFiles            = errors.New("upload requires at least one file")
	ErrUploadTargetUnsupported  = errors.New("upload target is not a supported storage node")
	ErrUploadEngineUnsupported  = errors.New("upload engine does not support content write")
	ErrUploadFileNameInvalid    = errors.New("upload file name is invalid")
	ErrUploadFileContentMissing = errors.New("upload file content is required")
)

type UploadSystemClient interface {
	GetEngine(engineID uint) (*commonModels.Engine, error)
}

type UploadMetaClient interface {
	CreateManualScanRunForTenant(tenantID uint, opts commonClient.MetaScanOptions) (*commonExecution.TaskExecution, error)
}

type UploadService struct {
	systemClient UploadSystemClient
	metaClient   UploadMetaClient
}

type UploadFile struct {
	FileName    string
	ContentType string
	Reader      io.Reader
}

type UploadRequest struct {
	TargetNodeLocator string
	Files             []UploadFile
	TenantID          uint
}

type UploadedFileResult struct {
	FileName string `json:"file_name"`
	Locator  string `json:"locator"`
}

type UploadResult struct {
	TargetNodeLocator string                         `json:"target_node_locator"`
	Files             []UploadedFileResult           `json:"files"`
	ScanExecutionID   string                         `json:"scan_execution_id,omitempty"`
	ScanRun           *commonExecution.TaskExecution `json:"scan_run,omitempty"`
}

func NewUploadService(systemClient UploadSystemClient, metaClient UploadMetaClient) *UploadService {
	return &UploadService{systemClient: systemClient, metaClient: metaClient}
}

func (s *UploadService) UploadFiles(ctx context.Context, req *UploadRequest) (*UploadResult, error) {
	if req == nil || len(req.Files) == 0 {
		return nil, ErrUploadNoFiles
	}
	loc, err := resourcetree.ParseURI(strings.TrimSpace(req.TargetNodeLocator))
	if err != nil {
		return nil, err
	}
	if !isStorageNodeType(loc.Type) {
		return nil, ErrUploadTargetUnsupported
	}
	if s == nil || s.systemClient == nil {
		return nil, fmt.Errorf("system client is required")
	}
	engine, err := s.systemClient.GetEngine(loc.EngineID)
	if err != nil {
		return nil, err
	}
	tenantID := req.TenantID
	var tenantPtr *uint
	if tenantID > 0 {
		tenantPtr = &tenantID
	}
	if !resourceAccessible(engine, tenantPtr) {
		return nil, ErrEngineAccessDenied
	}
	if !storageCanWrite(engine) {
		return nil, ErrUploadEngineUnsupported
	}
	enginePlugin, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", engine.EngineType)
	}
	writer, ok := enginePlugin.(plugin.ContentWritableProvider)
	if !ok {
		return nil, ErrUploadEngineUnsupported
	}
	modelProvider, ok := enginePlugin.(plugin.CatalogModelProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not expose catalog model", engine.EngineType)
	}
	parentPath, err := resourcetree.ProviderCatalogPathFromLocator(modelProvider.CatalogModel(), loc)
	if err != nil {
		return nil, err
	}

	results := make([]UploadedFileResult, 0, len(req.Files))
	uploadedPaths := make([]string, 0, len(req.Files))
	for _, file := range req.Files {
		cleanName, err := cleanUploadFileName(file.FileName)
		if err != nil {
			return nil, err
		}
		if file.Reader == nil {
			return nil, fmt.Errorf("%w: %s", ErrUploadFileContentMissing, cleanName)
		}
		targetPath := uploadLeafPath(parentPath, cleanName, loc.Type)
		contentType := strings.TrimSpace(file.ContentType)
		if contentType == "" {
			contentType = mime.TypeByExtension(path.Ext(cleanName))
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		wc, err := writer.CreateContent(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), targetPath, plugin.WriteOptions{
			ContentType: contentType,
			Overwrite:   true,
		})
		if err != nil {
			return nil, fmt.Errorf("create upload target %s: %w", cleanName, err)
		}
		if _, err := io.Copy(wc, file.Reader); err != nil {
			_ = wc.Close()
			return nil, fmt.Errorf("write upload target %s: %w", cleanName, err)
		}
		if err := wc.Close(); err != nil {
			return nil, fmt.Errorf("close upload target %s: %w", cleanName, err)
		}
		results = append(results, UploadedFileResult{
			FileName: cleanName,
			Locator:  uploadFileLocator(loc, cleanName),
		})
		if catalogPath := strings.Trim(targetPath.StringPath(), "/"); catalogPath != "" {
			uploadedPaths = append(uploadedPaths, catalogPath)
		}
	}

	run, err := s.submitUploadScan(req.TenantID, loc.EngineID, uploadedPaths)
	if err != nil {
		return nil, err
	}
	result := &UploadResult{
		TargetNodeLocator: strings.TrimSpace(req.TargetNodeLocator),
		Files:             results,
		ScanRun:           run,
	}
	if run != nil {
		result.ScanExecutionID = run.ExecutionID
	}
	return result, nil
}

func cleanUploadFileName(raw string) (string, error) {
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"))
	if name == "" || name == "." || name == "/" {
		return "", ErrUploadFileNameInvalid
	}
	return name, nil
}

func uploadLeafPath(parent plugin.CatalogPath, fileName string, targetType resourcetree.ResourceType) plugin.CatalogPath {
	switch targetType {
	case resourcetree.TypeBucket, resourcetree.TypePrefix:
		return plugin.ObjectItemPath(parent.EngineID, objectBucket(parent), objectChildPath(parent, fileName))
	case resourcetree.TypeRoot, resourcetree.TypeDirectory, resourcetree.TypeDir:
		return plugin.FileItemPath(parent.EngineID, path.Join(parent.StringPath(), fileName))
	default:
		return parent
	}
}

func objectBucket(parent plugin.CatalogPath) string {
	for _, segment := range parent.Segments {
		if segment.Term == plugin.CatalogTermBucket || segment.Kind == plugin.CatalogKindBucket {
			return strings.Trim(segment.Name, "/")
		}
	}
	return ""
}

func objectChildPath(parent plugin.CatalogPath, fileName string) string {
	parts := make([]string, 0, len(parent.Segments))
	skipUntilBucket := true
	for _, segment := range parent.Segments {
		if segment.Term == plugin.CatalogTermBucket || segment.Kind == plugin.CatalogKindBucket {
			skipUntilBucket = false
			continue
		}
		if skipUntilBucket || plugin.IsCatalogRootSegment(segment) {
			continue
		}
		if name := strings.Trim(segment.Name, "/"); name != "" {
			parts = append(parts, name)
		}
	}
	parts = append(parts, fileName)
	return strings.Join(parts, "/")
}

func uploadFileLocator(parent *resourcetree.ResourceLocator, fileName string) string {
	if parent == nil {
		return ""
	}
	next := &resourcetree.ResourceLocator{
		EngineID: parent.EngineID,
		Path:     append(append([]string{}, parent.Path...), fileName),
		Type:     uploadLeafLocatorType(parent.Type),
	}
	return next.ToURI()
}

func uploadLeafLocatorType(parentType resourcetree.ResourceType) resourcetree.ResourceType {
	switch parentType {
	case resourcetree.TypeBucket, resourcetree.TypePrefix:
		return resourcetree.TypeObject
	default:
		return resourcetree.TypeFile
	}
}

func (s *UploadService) submitUploadScan(tenantID uint, engineID uint, uploadedPaths []string) (*commonExecution.TaskExecution, error) {
	if s == nil || s.metaClient == nil || engineID == 0 {
		return nil, nil
	}
	refGroups := uploadScanRefGroups(uploadedPaths)
	if len(refGroups) == 0 {
		return nil, nil
	}
	opts := commonClient.MetaScanOptions{
		EngineID:    engineID,
		RefGroups:   refGroups,
		ScanDepth:   commonClient.MetaScanDepthDeep,
		Force:       true,
		TriggerType: commonExecution.TriggerTypeManual,
		Source:      commonExecution.ModuleManager,
	}
	return s.metaClient.CreateManualScanRunForTenant(tenantID, opts)
}

func uploadScanRefGroups(uploadedPaths []string) []commonClient.MetaScanRefGroup {
	type groupKey struct {
		dir  string
		base string
	}
	grouped := map[groupKey][]string{}
	for _, rawPath := range uploadedPaths {
		catalogPath := strings.Trim(strings.TrimSpace(rawPath), "/")
		if catalogPath == "" {
			continue
		}
		dir := path.Dir(catalogPath)
		if dir == "." {
			dir = ""
		}
		base := strings.TrimSuffix(path.Base(catalogPath), path.Ext(catalogPath))
		key := groupKey{dir: dir, base: base}
		grouped[key] = append(grouped[key], catalogPath)
	}

	keys := make([]groupKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].dir != keys[j].dir {
			return keys[i].dir < keys[j].dir
		}
		return keys[i].base < keys[j].base
	})

	groups := make([]commonClient.MetaScanRefGroup, 0, len(keys))
	for _, key := range keys {
		paths := dedupeAndSortCatalogPaths(grouped[key])
		if len(paths) == 0 {
			continue
		}
		primary := uploadScanPrimaryPath(paths)
		refs := make([]commonClient.MetaScanRef, 0, len(paths))
		for _, catalogPath := range paths {
			refs = append(refs, commonClient.MetaScanRef{
				Path:     catalogPath,
				Role:     uploadScanRefRole(catalogPath, primary),
				Required: catalogPath == primary,
				Primary:  catalogPath == primary,
			})
		}
		groups = append(groups, commonClient.MetaScanRefGroup{
			Primary: primary,
			Refs:    refs,
		})
	}
	return groups
}

func dedupeAndSortCatalogPaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, rawPath := range paths {
		catalogPath := strings.Trim(strings.TrimSpace(rawPath), "/")
		if catalogPath == "" || seen[catalogPath] {
			continue
		}
		seen[catalogPath] = true
		result = append(result, catalogPath)
	}
	sort.Strings(result)
	return result
}

func uploadScanPrimaryPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	for _, ext := range []string{".shp", ".geojson", ".json", ".csv", ".parquet"} {
		for _, catalogPath := range paths {
			if strings.EqualFold(path.Ext(catalogPath), ext) {
				return catalogPath
			}
		}
	}
	return paths[0]
}

func uploadScanRefRole(catalogPath, primary string) string {
	if catalogPath == primary {
		return "main"
	}
	return "sidecar"
}
