package service

import (
	"context"
	"errors"
	"path"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/format"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/minio/minio-go/v7"
)

var (
	ErrExportSourceUnsupported = errors.New("export source is not a supported database item")
	ErrExportFormatUnsupported = errors.New("export format is not supported")
	ErrExportSessionNotFound   = exportartifact.ErrSessionNotFound
	ErrExportNotReady          = exportartifact.ErrNotReady
)

type ExportService struct {
	systemClient SystemClient
	artifacts    *exportartifact.Service
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
	TransferExecutionID string `json:"transfer_execution_id"`
	Status              string `json:"status"`
	ErrorMessage        string `json:"error_message,omitempty"`
	DownloadURL         string `json:"download_url,omitempty"`
	CreatedAt           string `json:"created_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

type ExportFile = exportartifact.File

func NewExportService(systemClient SystemClient, transferClient exportartifact.TransferClient, sessionStore exportartifact.Store, minioClient *minio.Client, minioBucket string) *ExportService {
	return &ExportService{
		systemClient: systemClient,
		artifacts:    exportartifact.NewService(transferClient, sessionStore, minioClient, minioBucket, "manager", "/api/v1/manager/exports"),
	}
}

func (s *ExportService) CreateExport(ctx context.Context, req *ExportRequest) (*ExportSessionResponse, error) {
	if req == nil {
		return nil, errors.New("export request is required")
	}
	sourceLocator := strings.TrimSpace(req.SourceItemLocator)
	if sourceLocator == "" {
		return nil, errors.New("source_item_locator is required")
	}
	loc, err := resourcetree.ParseURI(sourceLocator)
	if err != nil {
		return nil, err
	}
	if !isDatabaseItemType(loc.Type) {
		return nil, ErrExportSourceUnsupported
	}
	if s == nil || s.systemClient == nil || s.artifacts == nil {
		return nil, errors.New("export service is not configured")
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
	if loc.Type == resourcetree.TypeTable && (!databaseCanRead(engine) || !supportedExportFormat(formatType)) {
		return nil, ErrExportFormatUnsupported
	}
	if loc.Type == resourcetree.TypeCollection {
		formats := encodedRecordExportFormats(engine)
		if len(formats) == 0 || !containsExactString(formats, string(formatType)) {
			return nil, ErrExportFormatUnsupported
		}
	}
	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" && len(loc.Path) > 0 {
		fileName = loc.Path[len(loc.Path)-1]
	}
	config := buildTableExportExecutionConfig(sourceLocator, "", "", formatType)
	if loc.Type == resourcetree.TypeCollection {
		config = buildEncodedRecordExportExecutionConfig(sourceLocator, "", "", formatType)
	}
	created, err := s.artifacts.Create(ctx, exportartifact.CreateRequest{
		TenantID: req.TenantID, UserID: req.UserID, SourceRef: sourceLocator,
		Format: formatType, FileName: fileName, ExecutionName: "manager_export_" + strings.TrimSuffix(path.Base(fileName), path.Ext(fileName)),
		ExecutionConfig: config,
	})
	if err != nil {
		return nil, err
	}
	return managerExportResponse(created), nil
}

func (s *ExportService) GetExport(ctx context.Context, id, tenantID, userID uint) (*ExportSessionResponse, error) {
	response, err := s.artifacts.Get(ctx, id, tenantID, userID)
	if err != nil {
		return nil, err
	}
	return managerExportResponse(response), nil
}

func (s *ExportService) OpenExportFile(ctx context.Context, id, tenantID, userID uint) (*ExportFile, error) {
	return s.artifacts.Open(ctx, id, tenantID, userID)
}

func managerExportResponse(response *exportartifact.SessionResponse) *ExportSessionResponse {
	if response == nil {
		return nil
	}
	return &ExportSessionResponse{
		ID: response.ID, SourceItemLocator: response.SourceRef, Format: response.Format, FileName: response.FileName,
		TransferExecutionID: response.TransferExecutionID, Status: response.Status, ErrorMessage: response.ErrorMessage,
		DownloadURL: response.DownloadURL, CreatedAt: response.CreatedAt, UpdatedAt: response.UpdatedAt,
	}
}

func buildTableExportExecutionConfig(sourceLocator, parentLocator, fileName string, formatType format.FormatType) commonClient.TransferExecutionConfig {
	return commonClient.TransferExecutionConfig{
		Runtime: commonClient.TransferExecutionRuntime{Boundary: "bounded"}, Load: commonClient.TransferExecutionLoad{Mode: "snapshot"},
		Source:     commonClient.TransferExecutionEndpoint{Locator: sourceLocator, DataType: "table", Representation: "native"},
		Target:     commonClient.TransferExecutionEndpoint{ParentLocator: parentLocator, Name: fileName, DataType: "table", Representation: "encoded", Format: string(formatType), Policy: map[string]interface{}{"apply_mode": "replace"}},
		Transforms: []commonClient.TransferExecutionTransform{},
	}
}

func buildEncodedRecordExportExecutionConfig(sourceLocator, parentLocator, fileName string, formatType format.FormatType) commonClient.TransferExecutionConfig {
	return commonClient.TransferExecutionConfig{
		Runtime: commonClient.TransferExecutionRuntime{Boundary: "bounded"}, Load: commonClient.TransferExecutionLoad{Mode: "snapshot"},
		Source: commonClient.TransferExecutionEndpoint{Locator: sourceLocator, DataType: "unknown", Representation: "native"},
		Target: commonClient.TransferExecutionEndpoint{ParentLocator: parentLocator, Name: fileName, DataType: "unknown", Representation: "encoded", Format: string(formatType), Policy: map[string]interface{}{"apply_mode": "replace"}},
	}
}

func supportedExportFormat(formatType format.FormatType) bool {
	if _, err := format.GetTableWriterProvider(formatType); err == nil {
		return true
	}
	_, err := format.GetMultiTableWriterProvider(formatType)
	return err == nil
}

func containsExactString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func tenantPtr(tenantID uint) *uint {
	if tenantID == 0 {
		return nil
	}
	return &tenantID
}
