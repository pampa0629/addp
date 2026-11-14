package service

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strconv"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/format"
	"github.com/addp/common/infra/objectstore"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/engineaccess"
)

var (
	ErrExportSourceUnsupported = errors.New("export source is not a supported database item")
	ErrExportFormatUnsupported = errors.New("export format is not supported")
	ErrExportSessionNotFound   = exportartifact.ErrSessionNotFound
	ErrExportNotReady          = exportartifact.ErrNotReady
)

type ExportService struct {
	systemClient    SystemClient
	artifacts       *exportartifact.Service
	fileGDBIssuer   *commonClient.SystemExecutionAuthorizationClient
	fileGDBTransfer fileGDBExportBinder
}

type fileGDBExportBinder interface {
	BindFileGDBExportAuthorization(context.Context, uint, *commonClient.IssuedFileGDBExportAuthorization) error
}

type ExportRequest struct {
	SourceItemLocator string `json:"source_item_locator"`
	Format            string `json:"format"`
	FileName          string `json:"file_name,omitempty"`
	TenantID          uint   `json:"-"`
	UserID            uint   `json:"-"`
	UserCredential    string `json:"-"`
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
type ExportExecutionSourceRequest = commonClient.ExportExecutionSourceRequest
type ExportExecutionSource = commonClient.ExportExecutionSource

func NewExportService(systemClient SystemClient, transferClient exportartifact.TransferClient, sessionStore exportartifact.Store, objectArea objectstore.ContentArea, areaName string) *ExportService {
	return &ExportService{
		systemClient: systemClient,
		artifacts:    exportartifact.NewService(transferClient, sessionStore, objectArea, areaName, "manager", "/api/v1/manager/exports"),
	}
}

func (s *ExportService) SetFileGDBAuthorization(issuer *commonClient.SystemExecutionAuthorizationClient, transfer fileGDBExportBinder) {
	s.fileGDBIssuer, s.fileGDBTransfer = issuer, transfer
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
	if loc.Type == resourcetree.TypeTable && (!databaseCanRead(engine) || !databaseTableExportFormat(engine, formatType)) {
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
	create := exportartifact.CreateRequest{
		TenantID: req.TenantID, UserID: req.UserID, SourceRef: sourceLocator,
		Format: formatType, FileName: fileName, ExecutionName: "manager_export_" + strings.TrimSuffix(path.Base(fileName), path.Ext(fileName)),
		ExecutionConfig: config,
	}
	if formatType == format.FormatFileGDB {
		if err := s.prepareFileGDBExport(ctx, req, loc, engine, &create); err != nil {
			return nil, err
		}
	}
	created, err := s.artifacts.Create(ctx, create)
	if err != nil {
		return nil, err
	}
	return managerExportResponse(created), nil
}

func (s *ExportService) prepareFileGDBExport(ctx context.Context, req *ExportRequest, loc *resourcetree.ResourceLocator, engine *commonModels.Engine, create *exportartifact.CreateRequest) error {
	if s.fileGDBIssuer == nil || s.fileGDBTransfer == nil {
		return engineaccess.ErrUnavailable
	}
	actor := int(req.UserID)
	origin := execution.TaskExecution{TenantID: int(req.TenantID), TriggeredBy: &actor}
	if req.UserCredential == "" || execution.ApplyUserOrigin(ctx, &origin) != nil {
		return commonAPI.ErrForbidden
	}
	namespace := engineplugin.EngineCatalogTermSchema
	if strings.EqualFold(engine.EngineType, "mysql") {
		namespace = engineplugin.EngineCatalogTermDatabase
	}
	target, err := resourcetree.EngineCatalogPathFromLocator(engineplugin.TabularCatalogModel(namespace), loc)
	if err != nil {
		return ErrExportSourceUnsupported
	}
	candidate, err := authorizationfacts.NativeCandidateBinding(target)
	if err != nil {
		return ErrExportSourceUnsupported
	}
	source := &execution.FileGDBExportSource{EngineVersion: strconv.FormatInt(engine.Version, 10), Binding: candidate}
	if source.Validate() != nil {
		return ErrExportSourceUnsupported
	}
	create.ExecutionConfig.Source.Locator = loc.ToURI()
	create.ExecutionConfig.FileGDBSource = source
	create.FileGDBActor = &commonClient.FileGDBExportActor{
		PrincipalID: *origin.ActorPrincipalID, TenantMembershipID: *origin.ActorTenantMembershipID, AuthorizationVersion: *origin.IssuedAuthorizationVersion,
	}
	create.AdmitFileGDB = func(ctx context.Context, id string, config commonClient.TransferExecutionConfig) error {
		raw, err := json.Marshal(config)
		if err != nil {
			return ErrExportSourceUnsupported
		}
		scope, err := execution.NewFileGDBExportReadScope(raw)
		if err != nil {
			return ErrExportSourceUnsupported
		}
		admitCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		issued, err := s.fileGDBIssuer.IssueFileGDBExport(admitCtx, req.UserCredential, commonClient.IssueManagerFileAuthorizationRequest{ExecutionID: id, ExpiresIn: 3600})
		if err != nil {
			var upstream *commonClient.SystemAPIError
			if errors.As(err, &upstream) {
				switch upstream.StatusCode {
				case 401:
					return commonClient.ErrManagerPreviewCredentialRejected
				case 403, 404, 409:
					return commonAPI.ErrForbidden
				}
			}
			return engineaccess.ErrUnavailable
		}
		if !issued.Matches(req.TenantID, id, scope) {
			return commonAPI.ErrForbidden
		}
		if err := s.fileGDBTransfer.BindFileGDBExportAuthorization(admitCtx, req.TenantID, issued); err != nil {
			return engineaccess.ErrUnavailable
		}
		return nil
	}
	return nil
}

func (s *ExportService) GetExport(ctx context.Context, id, tenantID, userID uint) (*ExportSessionResponse, error) {
	response, err := s.artifacts.Get(ctx, id, tenantID, userID, nil)
	if err != nil {
		return nil, err
	}
	return managerExportResponse(response), nil
}

func (s *ExportService) ResolveExecutionSource(ctx context.Context, id, tenantID uint, request commonClient.ExportExecutionSourceRequest) (*commonClient.ExportExecutionSource, error) {
	return s.artifacts.ResolveExecutionSource(ctx, id, tenantID, request)
}

func (s *ExportService) OpenExportFile(ctx context.Context, id, tenantID, userID uint) (*ExportFile, error) {
	return s.artifacts.Open(ctx, id, tenantID, userID, nil)
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
