package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *UploadService) WithInitialUploads(db *gorm.DB, meta *client.MetaClient, system *client.SystemServiceClient) *UploadService {
	s.db = db
	s.uploadIdentification = meta
	s.issueUploadGrant = func(ctx context.Context, tenant uint, id, fp string) (*authorization.UploadInitialResult, error) {
		return system.WithTenantID(tenant).IssueInitialUploadGrant(ctx, id, fp)
	}
	return s
}

// The request has already passed the upload function guard. The immutable
// actor comes only from middleware's current User origin, never an HTTP field.
func (s *UploadService) uploadNFS(ctx context.Context, req *UploadRequest, loc *resourcetree.ResourceLocator, engine *commonmodels.Engine, provider plugin.ContentWritableProvider, parent plugin.EngineCatalogPath) (*UploadResult, error) {
	if s.db == nil || s.uploadIdentification == nil || s.issueUploadGrant == nil {
		return nil, errors.New("upload initial authorization dependencies unavailable")
	}
	if len(req.Files) > client.MaxUploadFiles || engine.Version <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	now := time.Now().UTC()
	run := &execution.TaskExecution{TenantID: int(req.TenantID), ExecutionID: uuid.NewString(), Module: execution.ModuleManager, TaskType: execution.TaskTypeUpload, Source: execution.ModuleManager, Status: execution.ExecutionStatusRunning, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, StartedAt: &now, ExecutionConfig: commonmodels.JSONMap{"engine_id": engine.ID, "engine_version": strconv.FormatInt(engine.Version, 10)}, Metadata: commonmodels.JSONMap{}}
	if err := execution.ApplyUserOrigin(ctx, run); err != nil {
		return nil, err
	}
	// Validate the complete request before the first write or persisted operation.
	planned := make([]plugin.EngineCatalogPath, len(req.Files))
	names := make([]string, len(req.Files))
	seen := map[string]bool{}
	for i, file := range req.Files {
		name, err := cleanUploadFileName(file.FileName)
		if err != nil || file.Reader == nil || seen[name] {
			return nil, commonapi.ErrBadRequest
		}
		seen[name] = true
		names[i] = name
		planned[i] = uploadLeafPath(parent, name, loc.Type)
	}
	run.ExecutionConfig["upload_targets"] = planned
	// Bound both persisted operation and worst-case receipt before any source write.
	budget := client.UploadReceipt{SchemaVersion: client.UploadReceiptSchema, UploadID: run.ExecutionID, TenantID: int64(req.TenantID), EngineID: engine.ID, EngineVersion: strconv.FormatInt(engine.Version, 10), Uploader: client.UploadUserOrigin{PrincipalID: *run.ActorPrincipalID, MembershipID: *run.ActorTenantMembershipID, AuthorizationVersion: *run.IssuedAuthorizationVersion}, CommittedAt: now}
	for _, target := range planned {
		budget.Writes = append(budget.Writes, plugin.ContentWriteEvidence{Target: target, Disposition: "created", SizeBytes: 9223372036854775807, Committed: true})
	}
	encoded, encodeErr := json.Marshal(budget)
	configBytes, configErr := json.Marshal(run.ExecutionConfig)
	if encodeErr != nil || configErr != nil || len(encoded) > client.MaxUploadReceiptBytes || len(configBytes) > execution.MaxExecutionConfigBytes {
		return nil, commonapi.ErrBadRequest
	}
	if err := s.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, err
	}
	result := &UploadResult{UploadID: run.ExecutionID, TargetNodeLocator: req.TargetNodeLocator, InitialStatus: "pending"}
	receipt := client.UploadReceipt{SchemaVersion: client.UploadReceiptSchema, UploadID: run.ExecutionID, TenantID: int64(req.TenantID), EngineID: engine.ID, EngineVersion: strconv.FormatInt(engine.Version, 10), Uploader: client.UploadUserOrigin{PrincipalID: *run.ActorPrincipalID, MembershipID: *run.ActorTenantMembershipID, AuthorizationVersion: *run.IssuedAuthorizationVersion}}
	for i, file := range req.Files {
		raw, err := provider.CreateContent(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), planned[i], plugin.WriteOptions{Overwrite: true, ContentType: file.ContentType})
		if err != nil {
			result.Failures = append(result.Failures, names[i])
			continue
		}
		writer, ok := raw.(plugin.ContentWriteCommitter)
		if !ok {
			_ = raw.Close()
			result.Failures = append(result.Failures, names[i])
			continue
		}
		n, writeErr := io.Copy(writer, file.Reader)
		closeErr := writer.Close()
		proof, proofErr := writer.ContentWriteEvidence()
		if writeErr != nil || closeErr != nil || proofErr != nil || proof.Validate(planned[i]) != nil || proof.SizeBytes != n {
			result.Failures = append(result.Failures, names[i])
			continue
		}
		receipt.Writes = append(receipt.Writes, proof)
		result.Files = append(result.Files, UploadedFileResult{FileName: names[i], Locator: uploadFileLocator(loc, names[i])})
	}
	receipt.CommittedAt = time.Now().UTC()
	status := execution.ExecutionStatusSuccess
	if len(result.Failures) > 0 {
		status = execution.ExecutionStatusFailed
	}
	initial := "pending"
	if len(receipt.Writes) == 0 {
		initial = "denied"
		result.InitialStatus = initial
	}
	metadata := commonmodels.JSONMap{"upload_initial": map[string]any{"status": initial}, "upload_failed_files": result.Failures}
	if len(receipt.Writes) > 0 {
		if err := receipt.Validate(); err != nil {
			result.InitialStatus = "pending_receipt"
			return result, nil
		}
		metadata["upload_receipt"] = receipt
	}
	// A disconnected browser must not turn confirmed writes into an untracked
	// success, nor prompt a retry of the physical upload. No User token survives.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.db.WithContext(persistCtx).Model(&execution.TaskExecution{}).Where("tenant_id=? AND execution_id=? AND module=? AND task_type=? AND status=?", req.TenantID, run.ExecutionID, execution.ModuleManager, execution.TaskTypeUpload, execution.ExecutionStatusRunning).Updates(map[string]any{"status": status, "metadata": metadata, "completed_at": receipt.CommittedAt, "progress": 100}).Error; err != nil {
		result.InitialStatus = "pending_receipt" // bytes remain; never replay the write
	}
	return result, nil
}

func (s *UploadService) ReadUploadReceipt(ctx context.Context, tenant uint, id string) (*client.UploadReceipt, error) {
	if s == nil || s.db == nil || tenant == 0 {
		return nil, commonapi.ErrForbidden
	}
	var run execution.TaskExecution
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND execution_id=? AND module=? AND task_type=?", tenant, id, execution.ModuleManager, execution.TaskTypeUpload).Take(&run).Error; err != nil {
		return nil, err
	}
	if run.Source != "manager" || run.ParentExecutionID != nil || run.SourceTaskID != nil || run.TriggerType != "manual" || (run.Status != "success" && run.Status != "failed") {
		return nil, commonapi.ErrConflict
	}
	data, err := json.Marshal(run.Metadata["upload_receipt"])
	var receipt client.UploadReceipt
	if err != nil || json.Unmarshal(data, &receipt) != nil || receipt.Validate() != nil || receipt.UploadID != id || receipt.TenantID != int64(tenant) || run.ActorPrincipalID == nil || *run.ActorPrincipalID != receipt.Uploader.PrincipalID || run.ActorTenantMembershipID == nil || *run.ActorTenantMembershipID != receipt.Uploader.MembershipID || run.IssuedAuthorizationVersion == nil || *run.IssuedAuthorizationVersion != receipt.Uploader.AuthorizationVersion {
		return nil, commonapi.ErrConflict
	}
	var config struct {
		EngineID      uint                       `json:"engine_id"`
		EngineVersion string                     `json:"engine_version"`
		Targets       []plugin.EngineCatalogPath `json:"upload_targets"`
	}
	encoded, _ := json.Marshal(run.ExecutionConfig)
	if json.Unmarshal(encoded, &config) != nil || config.EngineID != receipt.EngineID || config.EngineVersion != receipt.EngineVersion {
		return nil, commonapi.ErrConflict
	}
	planned := map[string]bool{}
	for _, target := range config.Targets {
		path, _ := plugin.RequireFileLeafPath(target)
		planned[path] = target.EngineID == receipt.EngineID
	}
	for _, write := range receipt.Writes {
		path, _ := plugin.RequireFileLeafPath(write.Target)
		if !planned[path] {
			return nil, commonapi.ErrConflict
		}
	}
	return &receipt, nil
}

// Recovery only continues identification and Grant issuance. Idempotency is
// owned by Meta/System; it never opens a writer or replays request bodies.
func (s *UploadService) StartUploadRecovery(ctx context.Context) {
	if s == nil || s.db == nil || s.uploadIdentification == nil || s.issueUploadGrant == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			s.continueUploads(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *UploadService) continueUploads(ctx context.Context) {
	var runs []execution.TaskExecution
	if s.db.WithContext(ctx).Where("module=? AND task_type=? AND status IN ? AND metadata->'upload_initial'->>'status'='pending'", execution.ModuleManager, execution.TaskTypeUpload, []string{"success", "failed"}).Order("updated_at,id").Limit(8).Find(&runs).Error != nil {
		return
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return
		}
		// Rotate bounded batches even when one dependency remains unavailable.
		_ = s.db.WithContext(ctx).Model(&execution.TaskExecution{}).Where("tenant_id=? AND execution_id=? AND module=? AND task_type=? AND metadata->'upload_initial'->>'status'='pending'", run.TenantID, run.ExecutionID, execution.ModuleManager, execution.TaskTypeUpload).UpdateColumn("updated_at", time.Now().UTC()).Error
		s.continueUpload(ctx, &run)
	}
}
func (s *UploadService) continueUpload(ctx context.Context, run *execution.TaskExecution) {
	receipt, err := s.ReadUploadReceipt(ctx, uint(run.TenantID), run.ExecutionID)
	if err != nil {
		return
	}
	observed, err := s.uploadIdentification.EnsureUploadBasicScan(ctx, uint(run.TenantID), run.ExecutionID)
	if err != nil {
		if status, ok := client.TenantAPIStatusCode(err); ok && (status == 400 || status == 403 || status == 409) {
			s.saveUploadInitial(ctx, run, "denied", "", nil)
		}
		return
	}
	if observed == nil || observed.Validate() != nil || observed.UploadID != run.ExecutionID {
		return
	}
	if observed.Status == "pending" || observed.Status == "running" {
		s.saveUploadInitial(ctx, run, "pending", observed.ScanExecutionID, nil)
		return
	}
	outcomes := []authorization.UploadInitialResult{}
	state := "denied"
	if observed.Status == "success" {
		for _, write := range receipt.Writes {
			fp := commonmodels.GenerateItemFingerprint(receipt.EngineID, write.Target.StringPath())
			result, err := s.issueUploadGrant(ctx, uint(run.TenantID), run.ExecutionID, fp)
			if err != nil || result == nil || result.Validate() != nil || result.UploadID != run.ExecutionID || result.ItemFingerprint != fp || result.TenantID != int64(run.TenantID) {
				return
			}
			outcomes = append(outcomes, *result)
		}
		state = "settled"
	}
	s.saveUploadInitial(ctx, run, state, observed.ScanExecutionID, outcomes)
}
func (s *UploadService) saveUploadInitial(ctx context.Context, run *execution.TaskExecution, state, scan string, outcomes []authorization.UploadInitialResult) {
	value, _ := json.Marshal(map[string]any{"status": state, "scan_execution_id": scan, "outcomes": outcomes})
	// A late duplicate never changes a settled result; receipt and physical state remain immutable.
	_ = s.db.WithContext(ctx).Model(&execution.TaskExecution{}).Where("tenant_id=? AND execution_id=? AND module=? AND task_type=? AND metadata->'upload_initial'->>'status'='pending'", run.TenantID, run.ExecutionID, execution.ModuleManager, execution.TaskTypeUpload).Update("metadata", gorm.Expr("jsonb_set(metadata,'{upload_initial}',?::jsonb)", string(value))).Error
}

func (s *UploadService) GetOwnUpload(ctx context.Context, tenant uint, principal int64, id string) (*execution.TaskExecution, error) {
	if s == nil || s.db == nil || tenant == 0 || principal <= 0 {
		return nil, commonapi.ErrForbidden
	}
	var run execution.TaskExecution
	err := s.db.WithContext(ctx).Where("tenant_id=? AND execution_id=? AND module=? AND task_type=? AND actor_principal_id=?", tenant, id, execution.ModuleManager, execution.TaskTypeUpload, principal).Take(&run).Error
	return &run, err
}
