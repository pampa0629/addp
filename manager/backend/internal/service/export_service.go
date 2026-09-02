package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

var (
	ErrExportSourceUnsupported = errors.New("export source is not a supported database item")
	ErrExportFormatUnsupported = errors.New("export format is not supported")
	ErrExportSessionNotFound   = errors.New("export session not found")
	ErrExportNotReady          = errors.New("export file is not ready")
)

type ExportSessionStore interface {
	Create(ctx context.Context, session *models.ExportSession) error
	Get(ctx context.Context, id uint, tenantID uint) (*models.ExportSession, error)
	UpdateStatus(ctx context.Context, session *models.ExportSession) error
}

type ExportService struct {
	systemClient   SystemClient
	transferClient *commonClient.TransferClient
	sessionStore   ExportSessionStore
	minioClient    *minio.Client
	minioBucket    string
}

type ExportRequest struct {
	SourceItemLocator string `json:"source_item_locator"`
	Format            string `json:"format"`
	FileName          string `json:"file_name,omitempty"`
	TenantID          uint   `json:"-"`
	UserID            uint   `json:"-"`
}

type ExportSessionResponse struct {
	ID                  uint   `json:"id"`
	SourceItemLocator   string `json:"source_item_locator"`
	Format              string `json:"format"`
	FileName            string `json:"file_name"`
	TargetLocator       string `json:"target_locator"`
	TransferTaskID      uint   `json:"transfer_task_id"`
	TransferExecutionID string `json:"transfer_execution_id"`
	Status              string `json:"status"`
	ErrorMessage        string `json:"error_message,omitempty"`
	DownloadURL         string `json:"download_url,omitempty"`
	CreatedAt           string `json:"created_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

type ExportFile struct {
	Reader      io.ReadCloser
	FileName    string
	ContentType string
}

const exportArtifactManifestVersion = "addp.export_artifact.v1"

type exportArtifactRef struct {
	Path      string `json:"path"`
	Role      string `json:"role,omitempty"`
	Required  bool   `json:"required,omitempty"`
	Primary   bool   `json:"primary,omitempty"`
	Extension string `json:"extension,omitempty"`
	Entry     string `json:"entry,omitempty"`
}

type exportArtifactDownload struct {
	Kind     string `json:"kind"`
	FileName string `json:"file_name"`
}

type exportArtifactManifest struct {
	SchemaVersion string                 `json:"schema_version"`
	DataType      string                 `json:"data_type"`
	Format        string                 `json:"format"`
	Layout        string                 `json:"layout"`
	BaseName      string                 `json:"base_name"`
	PrimaryRef    string                 `json:"primary_ref,omitempty"`
	Refs          []exportArtifactRef    `json:"refs,omitempty"`
	Download      exportArtifactDownload `json:"download"`
}

func NewExportService(
	systemClient SystemClient,
	transferClient *commonClient.TransferClient,
	sessionStore ExportSessionStore,
	minioClient *minio.Client,
	minioBucket string,
) *ExportService {
	return &ExportService{
		systemClient:   systemClient,
		transferClient: transferClient,
		sessionStore:   sessionStore,
		minioClient:    minioClient,
		minioBucket:    minioBucket,
	}
}

func (s *ExportService) CreateExport(ctx context.Context, req *ExportRequest) (*ExportSessionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("export request is required")
	}
	sourceLocator := strings.TrimSpace(req.SourceItemLocator)
	if sourceLocator == "" {
		return nil, fmt.Errorf("source_item_locator is required")
	}
	loc, err := resourcetree.ParseURI(sourceLocator)
	if err != nil {
		return nil, err
	}
	if !isDatabaseItemType(loc.Type) {
		return nil, ErrExportSourceUnsupported
	}
	if s == nil || s.systemClient == nil {
		return nil, fmt.Errorf("system client is required")
	}
	if s.transferClient == nil {
		return nil, fmt.Errorf("transfer client is required")
	}
	if s.sessionStore == nil {
		return nil, fmt.Errorf("export session store is required")
	}
	if s.minioClient == nil {
		return nil, fmt.Errorf("infra minio client is required")
	}
	engine, err := s.systemClient.GetEngineForTenant(ctx, req.TenantID, loc.EngineID)
	if err != nil {
		return nil, err
	}
	if !resourceBelongsToTenant(engine, tenantPtr(req.TenantID)) {
		return nil, ErrEngineAccessDenied
	}
	if err := engineaccess.EnsureAvailable(engine); err != nil {
		return nil, err
	}
	formatType := format.FormatType(strings.ToLower(strings.TrimSpace(req.Format)))
	if formatType == "" && loc.Type == resourcetree.TypeCollection {
		formatType = format.FormatMongoDBExtendedJSONL
	} else if formatType == "" {
		formatType = format.FormatCSV
	}
	if loc.Type == resourcetree.TypeTable && !databaseCanRead(engine) {
		return nil, ErrExportSourceUnsupported
	}
	if loc.Type == resourcetree.TypeTable && !supportedExportFormat(formatType) {
		return nil, ErrExportFormatUnsupported
	}
	if loc.Type == resourcetree.TypeCollection {
		recordFormats := encodedRecordExportFormats(engine)
		if len(recordFormats) == 0 {
			return nil, ErrExportSourceUnsupported
		}
		if !containsExactString(recordFormats, string(formatType)) {
			return nil, ErrExportFormatUnsupported
		}
	}

	now := time.Now()
	exportID := uuid.New().String()
	parentPrefix := managerExportStagingPrefix(req.TenantID, exportID, now)
	baseName := exportBaseName(req.FileName, loc)
	targetFileName := withExportExtension(baseName, formatType)
	downloadFileName := exportDownloadFileName(baseName, formatType)
	targetObjectPath := strings.Trim(parentPrefix, "/") + "/" + targetFileName
	parentLocator := managerInfraMinioPrefixLocator(s.minioBucket, parentPrefix)
	targetLocator := managerInfraMinioObjectLocator(s.minioBucket, targetObjectPath)

	autoScanMetadata := false
	taskConfig := buildTableExportTaskConfig(sourceLocator, parentLocator, targetFileName, formatType)
	if loc.Type == resourcetree.TypeCollection {
		taskConfig = buildEncodedRecordExportTaskConfig(sourceLocator, parentLocator, targetFileName, formatType)
	}
	taskReq := &commonClient.CreateTransferTaskRequest{
		Name:             fmt.Sprintf("manager_export_%s_%s", strings.TrimSuffix(targetFileName, path.Ext(targetFileName)), now.Format("20060102_150405")),
		TaskType:         commonExecution.TaskTypeSync,
		Config:           taskConfig,
		AutoScanMetadata: &autoScanMetadata,
		BatchSize:        1000,
		TenantID:         req.TenantID,
	}
	taskResp, err := s.transferClient.CreateTask(taskReq)
	if err != nil {
		return nil, fmt.Errorf("create transfer sync task: %w", err)
	}
	triggerResp, err := s.transferClient.TriggerTask(taskResp.ID, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("trigger transfer sync task %d: %w", taskResp.ID, err)
	}
	if strings.TrimSpace(triggerResp.ExecutionID) == "" {
		return nil, fmt.Errorf("transfer sync execution uuid is empty")
	}

	session := &models.ExportSession{
		TenantID:            req.TenantID,
		UserID:              req.UserID,
		SourceItemLocator:   sourceLocator,
		Format:              string(formatType),
		FileName:            downloadFileName,
		TargetParentLocator: parentLocator,
		TargetLocator:       targetLocator,
		TransferTaskID:      taskResp.ID,
		TransferExecutionID: strings.TrimSpace(triggerResp.ExecutionID),
		Status:              models.ExportSessionStatusPending,
	}
	if err := s.sessionStore.Create(ctx, session); err != nil {
		return nil, err
	}
	return exportSessionResponse(session), nil
}

func (s *ExportService) GetExport(ctx context.Context, id uint, tenantID uint) (*ExportSessionResponse, error) {
	session, err := s.getSession(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.refreshSessionStatus(ctx, session); err != nil {
		return nil, err
	}
	return exportSessionResponse(session), nil
}

func (s *ExportService) OpenExportFile(ctx context.Context, id uint, tenantID uint) (*ExportFile, error) {
	session, err := s.getSession(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.refreshSessionStatus(ctx, session); err != nil {
		return nil, err
	}
	if session.Status != models.ExportSessionStatusSuccess {
		return nil, ErrExportNotReady
	}
	manifest := exportArtifactManifestFromJSON(session.ArtifactManifest)
	if isMultiExportFormat(format.FormatType(session.Format)) || manifest.Layout == format.LayoutMulti || manifest.Download.Kind == "zip" {
		return s.openExportZip(ctx, session, manifest)
	}
	return s.openExportSingleFile(ctx, session)
}

func (s *ExportService) openExportSingleFile(ctx context.Context, session *models.ExportSession) (*ExportFile, error) {
	objectPath, err := managerInfraObjectPath(session.TargetLocator, s.minioBucket)
	if err != nil {
		return nil, err
	}
	obj, err := s.minioClient.GetObject(ctx, s.minioBucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	contentType := mime.TypeByExtension(path.Ext(session.FileName))
	if contentType == "" {
		contentType = format.FormatToMIME(format.FormatType(session.Format))
	}
	return &ExportFile{
		Reader:      obj,
		FileName:    session.FileName,
		ContentType: contentType,
	}, nil
}

func (s *ExportService) openExportZip(ctx context.Context, session *models.ExportSession, manifest exportArtifactManifest) (*ExportFile, error) {
	if len(manifest.Refs) == 0 {
		return nil, fmt.Errorf("%w: export artifact manifest has no refs", ErrExportNotReady)
	}
	prefix, err := exportSessionObjectPrefix(session, s.minioBucket)
	if err != nil {
		return nil, err
	}
	refs := make([]exportArtifactRef, 0, len(manifest.Refs))
	for _, ref := range manifest.Refs {
		ref.Path = strings.Trim(ref.Path, "/")
		ref.Entry = cleanZipEntryName(ref.Entry)
		if ref.Path == "" || ref.Entry == "" {
			return nil, fmt.Errorf("%w: export artifact manifest contains empty ref", ErrExportNotReady)
		}
		if !objectPathInsidePrefix(ref.Path, prefix) {
			return nil, fmt.Errorf("export artifact ref is outside session prefix")
		}
		refs = append(refs, ref)
	}
	reader, writer := io.Pipe()
	go func() {
		err := s.writeExportZip(ctx, writer, refs)
		_ = writer.CloseWithError(err)
	}()
	fileName := strings.TrimSpace(manifest.Download.FileName)
	if fileName == "" {
		fileName = session.FileName
	}
	return &ExportFile{
		Reader:      reader,
		FileName:    fileName,
		ContentType: "application/zip",
	}, nil
}

func (s *ExportService) writeExportZip(ctx context.Context, writer io.Writer, refs []exportArtifactRef) error {
	zipWriter := zip.NewWriter(writer)
	defer zipWriter.Close()

	for _, ref := range refs {
		obj, err := s.minioClient.GetObject(ctx, s.minioBucket, ref.Path, minio.GetObjectOptions{})
		if err != nil {
			if ref.Required {
				return err
			}
			continue
		}
		if _, err := obj.Stat(); err != nil {
			_ = obj.Close()
			if ref.Required {
				return err
			}
			continue
		}
		entry, err := zipWriter.Create(ref.Entry)
		if err != nil {
			_ = obj.Close()
			return err
		}
		_, copyErr := io.Copy(entry, obj)
		closeErr := obj.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func (s *ExportService) getSession(ctx context.Context, id uint, tenantID uint) (*models.ExportSession, error) {
	if s == nil || s.sessionStore == nil {
		return nil, fmt.Errorf("export session store is required")
	}
	session, err := s.sessionStore.Get(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrExportSessionNotFound
	}
	return session, nil
}

func (s *ExportService) refreshSessionStatus(ctx context.Context, session *models.ExportSession) error {
	if session == nil || isFinalExportStatus(session.Status) {
		return nil
	}
	execution, err := s.transferClient.GetExecution(session.TransferExecutionID, session.TenantID)
	if err != nil {
		return err
	}
	nextStatus := exportStatusFromTransferStatus(execution.Status)
	nextManifest := session.ArtifactManifest
	if nextStatus == models.ExportSessionStatusSuccess {
		nextManifest = exportArtifactManifestJSON(session, execution.Metadata)
	}
	if nextStatus == session.Status &&
		strings.TrimSpace(execution.ErrorMessage) == strings.TrimSpace(session.ErrorMessage) &&
		jsonMapsEqual(nextManifest, session.ArtifactManifest) {
		return nil
	}
	session.Status = nextStatus
	session.ErrorMessage = strings.TrimSpace(execution.ErrorMessage)
	session.ArtifactManifest = nextManifest
	return s.sessionStore.UpdateStatus(ctx, session)
}

func buildTableExportTaskConfig(sourceLocator, parentLocator, fileName string, formatType format.FormatType) map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{
			"boundary": "bounded",
		},
		"load": map[string]interface{}{
			"mode": "snapshot",
		},
		"source": map[string]interface{}{
			"locator":        sourceLocator,
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": parentLocator,
			"name":           fileName,
			"data_type":      "table",
			"representation": "encoded",
			"format":         string(formatType),
			"policy": map[string]interface{}{
				"apply_mode": "replace",
			},
		},
		"transforms": []interface{}{},
	}
}

func buildEncodedRecordExportTaskConfig(sourceLocator, parentLocator, fileName string, formatType format.FormatType) map[string]interface{} {
	return map[string]interface{}{
		"runtime": map[string]interface{}{"boundary": "bounded"},
		"load":    map[string]interface{}{"mode": "snapshot"},
		"source": map[string]interface{}{
			"locator": sourceLocator, "data_type": "unknown", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": parentLocator, "name": fileName,
			"data_type": "unknown", "representation": "encoded", "format": string(formatType),
			"policy": map[string]interface{}{"apply_mode": "replace"},
		},
	}
}

func containsExactString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func supportedExportFormat(formatType format.FormatType) bool {
	if _, err := format.GetTableWriterProvider(formatType); err == nil {
		return true
	}
	if isMultiExportFormat(formatType) {
		return true
	}
	return false
}

func isMultiExportFormat(formatType format.FormatType) bool {
	_, err := format.GetMultiTableWriterProvider(formatType)
	return err == nil
}

func exportArtifactManifestFromJSON(raw models.JSONMap) exportArtifactManifest {
	if len(raw) == 0 {
		return exportArtifactManifest{}
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return exportArtifactManifest{}
	}
	var manifest exportArtifactManifest
	if err := json.Unmarshal(bytes, &manifest); err != nil {
		return exportArtifactManifest{}
	}
	return manifest
}

func jsonMapsEqual(left, right models.JSONMap) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftBytes) == string(rightBytes)
}

func exportArtifactManifestJSON(session *models.ExportSession, metadata models.JSONMap) models.JSONMap {
	if session == nil {
		return models.JSONMap{}
	}
	formatType := format.FormatType(strings.ToLower(strings.TrimSpace(session.Format)))
	refs := exportTargetRefsFromMetadata(metadata)
	if len(refs) == 0 {
		if isMultiExportFormat(formatType) {
			return models.JSONMap{}
		}
		objectPath, err := managerInfraObjectPath(session.TargetLocator, "")
		if err != nil {
			return models.JSONMap{}
		}
		refs = []exportArtifactRef{{
			Path:      objectPath,
			Role:      "main",
			Required:  true,
			Primary:   true,
			Extension: format.NormalizeExtension(path.Ext(objectPath)),
			Entry:     path.Base(session.FileName),
		}}
	}

	isMulti := isMultiExportFormat(formatType)
	layout := format.LayoutSingle
	downloadKind := "stream"
	if isMulti {
		layout = format.LayoutMulti
		downloadKind = "zip"
	}
	baseName := strings.TrimSuffix(path.Base(session.FileName), path.Ext(session.FileName))
	usedEntries := map[string]int{}
	primaryRef := ""
	for i := range refs {
		if refs[i].Extension == "" {
			refs[i].Extension = format.NormalizeExtension(path.Ext(refs[i].Path))
		}
		if refs[i].Entry == "" {
			if isMulti && refs[i].Extension != "" {
				refs[i].Entry = baseName + refs[i].Extension
			} else {
				refs[i].Entry = path.Base(refs[i].Path)
			}
		}
		refs[i].Entry = uniqueZipEntryName(cleanZipEntryName(refs[i].Entry), usedEntries)
		if refs[i].Primary {
			primaryRef = refs[i].Path
		}
	}
	dataType := "table"
	if formatType == format.FormatMongoDBExtendedJSONL {
		dataType = "unknown"
	}
	manifest := exportArtifactManifest{
		SchemaVersion: exportArtifactManifestVersion,
		DataType:      dataType,
		Format:        string(formatType),
		Layout:        layout,
		BaseName:      baseName,
		PrimaryRef:    primaryRef,
		Refs:          refs,
		Download: exportArtifactDownload{
			Kind:     downloadKind,
			FileName: session.FileName,
		},
	}
	bytes, err := json.Marshal(manifest)
	if err != nil {
		return models.JSONMap{}
	}
	var result map[string]interface{}
	if err := json.Unmarshal(bytes, &result); err != nil {
		return models.JSONMap{}
	}
	return models.JSONMap(result)
}

func exportTargetRefsFromMetadata(metadata models.JSONMap) []exportArtifactRef {
	raw, ok := metadata["target_refs"]
	if !ok || raw == nil {
		return nil
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var refs []exportArtifactRef
	if err := json.Unmarshal(bytes, &refs); err != nil {
		return nil
	}
	cleaned := make([]exportArtifactRef, 0, len(refs))
	for _, ref := range refs {
		ref.Path = strings.Trim(ref.Path, "/")
		ref.Extension = format.NormalizeExtension(ref.Extension)
		if ref.Extension == "" {
			ref.Extension = format.NormalizeExtension(path.Ext(ref.Path))
		}
		if ref.Path == "" {
			continue
		}
		cleaned = append(cleaned, ref)
	}
	return cleaned
}

func managerExportStagingPrefix(tenantID uint, exportID string, now time.Time) string {
	return fmt.Sprintf("tenant_%d/export/%s/%s/", tenantID, now.Format("20060102"), exportID)
}

func managerInfraMinioPrefixLocator(bucket, prefix string) string {
	return fmt.Sprintf("addp-infra://minio/%s/%s?type=prefix", strings.Trim(bucket, "/"), strings.Trim(prefix, "/"))
}

func managerInfraObjectPath(locatorURI, expectedBucket string) (string, error) {
	loc, err := parseManagerInfraLocator(locatorURI)
	if err != nil {
		return "", err
	}
	if expected := strings.Trim(expectedBucket, "/"); expected != "" && loc.bucket != expected {
		return "", fmt.Errorf("export target bucket mismatch")
	}
	if loc.objectPath == "" {
		return "", fmt.Errorf("export target path is empty")
	}
	return loc.objectPath, nil
}

func exportSessionObjectPrefix(session *models.ExportSession, expectedBucket string) (string, error) {
	objectPath, err := managerInfraObjectPath(session.TargetLocator, expectedBucket)
	if err != nil {
		return "", err
	}
	prefix := strings.Trim(path.Dir(objectPath), "/")
	if prefix == "" || prefix == "." {
		return "", fmt.Errorf("export target prefix is empty")
	}
	return prefix, nil
}

func objectPathInsidePrefix(objectPath, prefix string) bool {
	objectPath = strings.Trim(objectPath, "/")
	prefix = strings.Trim(prefix, "/")
	return objectPath == prefix || strings.HasPrefix(objectPath, prefix+"/")
}

func cleanZipEntryName(name string) string {
	cleaned := path.Clean(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return ""
	}
	return cleaned
}

type managerInfraLocator struct {
	bucket     string
	objectPath string
}

func parseManagerInfraLocator(locatorURI string) (managerInfraLocator, error) {
	const prefix = "addp-infra://minio/"
	raw := strings.TrimSpace(locatorURI)
	if !strings.HasPrefix(raw, prefix) {
		return managerInfraLocator{}, fmt.Errorf("unsupported infra locator")
	}
	withoutScheme := strings.TrimPrefix(raw, prefix)
	if i := strings.Index(withoutScheme, "?"); i >= 0 {
		withoutScheme = withoutScheme[:i]
	}
	parts := strings.SplitN(strings.Trim(withoutScheme, "/"), "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return managerInfraLocator{}, fmt.Errorf("invalid infra locator")
	}
	return managerInfraLocator{bucket: strings.Trim(parts[0], "/"), objectPath: strings.Trim(parts[1], "/")}, nil
}

func exportBaseName(requested string, loc *resourcetree.ResourceLocator) string {
	if cleaned, err := cleanUploadFileName(requested); err == nil && cleaned != "" {
		return strings.TrimSuffix(cleaned, path.Ext(cleaned))
	}
	if loc != nil && len(loc.Path) > 0 {
		name := strings.TrimSpace(loc.Path[len(loc.Path)-1])
		if name != "" {
			return name
		}
	}
	return "export"
}

func withExportExtension(baseName string, formatType format.FormatType) string {
	base := strings.TrimSuffix(path.Base(strings.ReplaceAll(strings.TrimSpace(baseName), "\\", "/")), path.Ext(baseName))
	if base == "" || base == "." || base == "/" {
		base = "export"
	}
	ext := format.DefaultWriteExtension(formatType, nil)
	if ext == "" {
		ext = "." + string(formatType)
	}
	return base + ext
}

func exportDownloadFileName(baseName string, formatType format.FormatType) string {
	if isMultiExportFormat(formatType) {
		base := strings.TrimSuffix(path.Base(strings.ReplaceAll(strings.TrimSpace(baseName), "\\", "/")), path.Ext(baseName))
		if base == "" || base == "." || base == "/" {
			base = "export"
		}
		return base + ".zip"
	}
	return withExportExtension(baseName, formatType)
}

func tenantPtr(tenantID uint) *uint {
	if tenantID == 0 {
		return nil
	}
	return &tenantID
}

func exportStatusFromTransferStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success":
		return models.ExportSessionStatusSuccess
	case "failed", "timeout", "cancelled":
		return models.ExportSessionStatusFailed
	case "running":
		return models.ExportSessionStatusRunning
	default:
		return models.ExportSessionStatusPending
	}
}

func isFinalExportStatus(status string) bool {
	switch status {
	case models.ExportSessionStatusSuccess, models.ExportSessionStatusFailed:
		return true
	default:
		return false
	}
}

func exportSessionResponse(session *models.ExportSession) *ExportSessionResponse {
	if session == nil {
		return nil
	}
	resp := &ExportSessionResponse{
		ID:                  session.ID,
		SourceItemLocator:   session.SourceItemLocator,
		Format:              session.Format,
		FileName:            session.FileName,
		TargetLocator:       session.TargetLocator,
		TransferTaskID:      session.TransferTaskID,
		TransferExecutionID: session.TransferExecutionID,
		Status:              session.Status,
		ErrorMessage:        session.ErrorMessage,
		CreatedAt:           session.CreatedAt.Format(time.RFC3339),
		UpdatedAt:           session.UpdatedAt.Format(time.RFC3339),
	}
	if session.Status == models.ExportSessionStatusSuccess {
		resp.DownloadURL = fmt.Sprintf("/api/v1/manager/exports/%d/file", session.ID)
	}
	return resp
}
