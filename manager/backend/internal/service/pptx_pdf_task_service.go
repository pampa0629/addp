package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type PPTXPDFTaskService struct {
	repo     *repository.PPTXPDFRepository
	executor PPTXPDFExecutor
	cleaner  PPTXPDFCleaner
	meta     pptxPDFMetaClient
	bucket   string
}

type PPTXPDFCleaner interface {
	DeleteByStorageRef(ctx context.Context, storageRef string) error
}

type pptxPDFMetaClient interface {
	GetItemByIDForTenant(tenantID, itemID uint) (*commonModels.MetaItem, error)
}

var (
	ErrInvalidPPTXPDFSource  = errors.New("invalid PPTX PDF source")
	ErrPPTXPDFTaskNotFound   = errors.New("PPTX PDF task not found")
	ErrPPTXPDFResultNotFound = errors.New("PPTX PDF result not found")
)

func NewPPTXPDFTaskService(repo *repository.PPTXPDFRepository) *PPTXPDFTaskService {
	return &PPTXPDFTaskService{repo: repo}
}
func (s *PPTXPDFTaskService) SetExecutor(executor PPTXPDFExecutor) { s.executor = executor }
func (s *PPTXPDFTaskService) SetCleaner(cleaner PPTXPDFCleaner)    { s.cleaner = cleaner }
func (s *PPTXPDFTaskService) SetMetaClient(meta pptxPDFMetaClient) { s.meta = meta }
func (s *PPTXPDFTaskService) SetBucket(bucket string)              { s.bucket = strings.TrimSpace(bucket) }
func (s *PPTXPDFTaskService) GetByID(ctx context.Context, id, tenantID uint) (*models.PPTXPDFTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}
func (s *PPTXPDFTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.PPTXPDFTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}
func (s *PPTXPDFTaskService) GetResult(ctx context.Context, id, tenantID uint) (*models.PPTXPDF, error) {
	return s.repo.GetResult(ctx, id, tenantID)
}

func (s *PPTXPDFTaskService) DeleteTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.repo.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if task == nil {
		return ErrPPTXPDFTaskNotFound
	}
	return s.repo.DeleteTask(ctx, id, tenantID)
}

func (s *PPTXPDFTaskService) DeleteResult(ctx context.Context, id, tenantID uint) error {
	result, err := s.repo.GetResult(ctx, id, tenantID)
	if err != nil {
		return err
	}
	if result == nil {
		return ErrPPTXPDFResultNotFound
	}
	if strings.TrimSpace(result.StorageRef) != "" {
		if s.cleaner == nil {
			return errors.New("PPTX PDF cleaner is not configured")
		}
		if err := s.cleaner.DeleteByStorageRef(ctx, result.StorageRef); err != nil {
			return err
		}
	}
	return s.repo.DeleteResult(ctx, id, tenantID)
}

func (s *PPTXPDFTaskService) EnsureTask(ctx context.Context, task *models.PPTXPDFTask) error {
	if err := resolvePPTXPDFTaskSource(s.meta, task, s.bucket); err != nil {
		return err
	}
	existing, err := s.repo.GetTaskByFingerprint(ctx, task.TenantID, task.ItemFingerprint)
	if err != nil {
		return err
	}
	if existing == nil {
		if createErr := s.repo.CreateTask(ctx, task); createErr == nil {
			return nil
		} else {
			existing, err = s.repo.GetTaskByFingerprint(ctx, task.TenantID, task.ItemFingerprint)
			if err != nil {
				return err
			}
			if existing == nil {
				return createErr
			}
		}
	}
	return s.reuseTask(ctx, task, existing)
}

func (s *PPTXPDFTaskService) reuseTask(ctx context.Context, task, existing *models.PPTXPDFTask) error {
	existing.Name, existing.Description, existing.Enabled = task.Name, task.Description, task.Enabled
	existing.SourceEngineID, existing.ItemID, existing.Locator = task.SourceEngineID, task.ItemID, task.Locator
	existing.SourceVersion, existing.SourceSizeBytes, existing.Config = task.SourceVersion, task.SourceSizeBytes, task.Config.Clone()
	if existing.CreatedBy == nil {
		existing.CreatedBy = task.CreatedBy
	}
	if err := s.repo.SaveTask(ctx, existing); err != nil {
		return err
	}
	*task = *existing
	return nil
}

func (s *PPTXPDFTaskService) Current(ctx context.Context, tenantID uint, fingerprint string) (*models.PPTXPDF, error) {
	return s.repo.Current(ctx, tenantID, fingerprint)
}

func (s *PPTXPDFTaskService) Execute(ctx context.Context, taskID, tenantID uint, triggerType, source string, parentExecutionID *string, overwrite bool) (string, error) {
	normalizedTrigger, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(source) == "" {
		source = commonExecution.ModuleManager
	}
	executionID := uuid.NewString()
	now := time.Now()
	step := "生成 PPTX 静态 PDF 快显"
	execution := &commonExecution.TaskExecution{ExecutionID: executionID, TenantID: int(tenantID), Module: commonExecution.ModuleManager, TaskType: commonExecution.TaskTypePPTXPDFGeneration, Source: source, ParentExecutionID: parentExecutionID, Status: commonExecution.ExecutionStatusPending, Progress: 0, CurrentStep: &step, TriggerType: normalizedTrigger, CreatedAt: now, UpdatedAt: now}
	_, err = s.repo.ClaimExecution(ctx, taskID, tenantID, execution, overwrite)
	if err != nil {
		if errors.Is(err, repository.ErrExistingResultActionRequired) {
			return "", ErrExistingResultActionRequired
		}
		if errors.Is(err, commonAPI.ErrNotFound) {
			return "", ErrTaskNotFound
		}
		if errors.Is(err, commonAPI.ErrConflict) {
			return "", ErrTaskExecutionBusy
		}
		return "", err
	}
	return executionID, nil
}

func (s *PPTXPDFTaskService) ClaimPendingExecution(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, *models.PPTXPDFTask, error) {
	return s.repo.ClaimPendingExecution(ctx, workerID, now, leaseDuration)
}

func (s *PPTXPDFTaskService) RenewExecutionLease(ctx context.Context, lease commonExecution.Lease, expiresAt time.Time) error {
	return s.repo.RenewExecutionLease(ctx, lease, expiresAt)
}

func (s *PPTXPDFTaskService) ExecutionAttemptIsTerminal(ctx context.Context, lease commonExecution.Lease) (bool, error) {
	return s.repo.ExecutionAttemptIsTerminal(ctx, lease)
}

func (s *PPTXPDFTaskService) RecoverExpiredExecutions(ctx context.Context, now time.Time, limit int) (int, error) {
	return s.repo.RecoverExpiredExecutions(ctx, now, limit)
}

func (s *PPTXPDFTaskService) RunClaimedExecution(ctx context.Context, execution *commonExecution.TaskExecution, lease commonExecution.Lease, task *models.PPTXPDFTask) error {
	if execution == nil || task == nil || execution.ExecutionID != lease.ExecutionID {
		return errors.New("claimed PPTX PDF execution, lease and task are required")
	}
	executionID := execution.ExecutionID
	startedAt := time.Now()
	if execution.StartedAt != nil {
		startedAt = *execution.StartedAt
	}
	result, err := s.prepareResult(ctx, task, executionID)
	var built *PPTXPDFExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("PPTX PDF executor is not configured")
		} else {
			built, err = s.executor.BuildPPTXPDF(ctx, PPTXPDFExecutionRequest{TenantID: task.TenantID, SourceEngineID: task.SourceEngineID, ItemID: task.ItemID, ItemFingerprint: task.ItemFingerprint, Locator: task.Locator, SourceVersion: task.SourceVersion, SourceSizeBytes: task.SourceSizeBytes, StorageRef: result.StorageRef, FileName: result.FileName})
		}
	}
	metadata := commonModels.JSONMap{}
	if err == nil {
		metadata, err = pptxPDFExecutionMetadata(task, built, s.bucket)
	}
	status := commonExecution.ExecutionStatusSuccess
	progress := 100
	var errorDetails commonModels.JSONMap
	fields := map[string]interface{}{}
	if err != nil {
		status, progress = commonExecution.ExecutionStatusFailed, 0
		errorDetails = commonModels.JSONMap{"message": err.Error()}
		fields = map[string]interface{}{"status": models.PPTXPDFStatusFailed, "error_message": err.Error(), "last_execution_id": executionID}
	} else {
		metadata["result_id"] = result.ID
		fields = map[string]interface{}{"status": models.PPTXPDFStatusReady, "error_message": "", "storage_ref": built.StorageRef, "file_name": built.FileName, "size_bytes": built.SizeBytes, "page_count": built.PageCount, "content_url": pptxPDFContentURL(result.ID), "metadata": metadata, "last_execution_id": executionID}
	}
	completedAt := time.Now()
	duration := completedAt.Sub(startedAt).Milliseconds()
	resultID := uint(0)
	if result != nil {
		resultID = result.ID
	} else {
		fields = nil
	}
	if completeErr := s.repo.CompleteExecutionWithLease(ctx, task.ID, task.TenantID, lease, resultID, fields, map[string]interface{}{"status": status, "progress": progress, "metadata": metadata, "error_details": errorDetails, "execution_time_ms": duration}, completedAt); completeErr != nil {
		return completeErr
	}
	return nil
}

func pptxPDFExecutionMetadata(task *models.PPTXPDFTask, built *PPTXPDFExecutionResult, bucket string) (commonModels.JSONMap, error) {
	if task == nil || built == nil {
		return nil, errors.New("PPTX PDF lineage requires task and result")
	}
	output, err := managerInfraObjectLineageRef(built.StorageRef, bucket)
	if err != nil {
		return nil, fmt.Errorf("build PPTX PDF output lineage: %w", err)
	}
	return managerExecutionLineage(
		built.Metadata.Clone(),
		commonExecution.TaskTypePPTXPDFGeneration,
		[]commonExecution.LineageResourceRef{managerItemLineageRef(task.Locator, task.ItemFingerprint, task.ItemID)},
		[]commonExecution.LineageResourceRef{output},
		"",
		"",
	), nil
}

func (s *PPTXPDFTaskService) prepareResult(ctx context.Context, task *models.PPTXPDFTask, executionID string) (*models.PPTXPDF, error) {
	current, err := s.repo.Current(ctx, task.TenantID, task.ItemFingerprint)
	if err != nil {
		return nil, err
	}
	storageRef, fileName := pptxPDFTarget(task, s.bucket)
	if current != nil {
		fields := map[string]interface{}{"source_version": task.SourceVersion, "source_engine_id": task.SourceEngineID, "item_id": task.ItemID, "locator": task.Locator, "task_id": task.ID, "last_execution_id": executionID, "storage_ref": storageRef, "file_name": fileName, "status": models.PPTXPDFStatusBuilding, "metadata": commonModels.JSONMap{}, "error_message": "", "content_url": pptxPDFContentURL(current.ID), "updated_at": time.Now()}
		if err := s.repo.UpdateResult(ctx, current.ID, task.TenantID, fields); err != nil {
			return nil, err
		}
		current.StorageRef, current.FileName = storageRef, fileName
		return current, nil
	}
	result := &models.PPTXPDF{TenantID: task.TenantID, ItemFingerprint: task.ItemFingerprint, ArtifactVariant: models.PPTXPDFArtifactVariant, SourceVersion: task.SourceVersion, SourceEngineID: task.SourceEngineID, ItemID: task.ItemID, Locator: task.Locator, TaskID: &task.ID, LastExecutionID: &executionID, StorageRef: storageRef, FileName: fileName, Status: models.PPTXPDFStatusBuilding, Metadata: commonModels.JSONMap{}, CreatedBy: task.CreatedBy}
	if err := s.repo.CreateResult(ctx, result); err != nil {
		return nil, err
	}
	result.ContentURL = pptxPDFContentURL(result.ID)
	_ = s.repo.UpdateResult(ctx, result.ID, task.TenantID, map[string]interface{}{"content_url": result.ContentURL})
	return result, nil
}

func resolvePPTXPDFTaskSource(meta pptxPDFMetaClient, task *models.PPTXPDFTask, bucket string) error {
	if err := normalizePPTXPDFTask(task); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPPTXPDFSource, err)
	}
	if meta == nil {
		return errors.New("Meta client is required to resolve PPTX source identity")
	}
	item, err := meta.GetItemByIDForTenant(task.TenantID, task.ItemID)
	if err != nil {
		return fmt.Errorf("resolve PPTX source item from Meta: %w", err)
	}
	loc, _ := resourcetree.ParseURI(task.Locator)
	if item == nil || item.ID != task.ItemID || item.TenantID != task.TenantID || item.EngineID != task.SourceEngineID ||
		!strings.EqualFold(strings.TrimSpace(item.ItemType), strings.TrimSpace(string(loc.Type))) || item.FullName != loc.FullName() {
		return fmt.Errorf("%w: PPTX locator does not match the current Meta DataItem", ErrInvalidPPTXPDFSource)
	}
	task.Name = strings.TrimSpace(item.Name)
	if task.Name == "" {
		task.Name = filepath.Base(item.FullName)
	}
	task.SourceVersion = sourceVersionForItem(task.ItemFingerprint, *item)
	task.SourceSizeBytes = 0
	if item.ObjectSizeBytes != nil {
		task.SourceSizeBytes = *item.ObjectSizeBytes
	} else if item.SizeBytes != nil {
		task.SourceSizeBytes = *item.SizeBytes
	}
	storageRef, fileName := pptxPDFTarget(task, bucket)
	task.Config = commonModels.JSONMap{"source": commonModels.JSONMap{"item_locator": task.Locator, "source_engine_id": task.SourceEngineID, "item_id": task.ItemID, "item_fingerprint": task.ItemFingerprint, "source_version": task.SourceVersion, "source_size_bytes": task.SourceSizeBytes, "format": "pptx"}, "result": commonModels.JSONMap{"storage_ref": storageRef, "file_name": fileName}, "options": commonModels.JSONMap{"strip_embedded_media": true}}
	return nil
}

func normalizePPTXPDFTask(task *models.PPTXPDFTask) error {
	if task == nil {
		return errors.New("PPTX PDF task is nil")
	}
	task.Name, task.Description, task.Locator = strings.TrimSpace(task.Name), strings.TrimSpace(task.Description), strings.TrimSpace(task.Locator)
	task.ItemFingerprint, task.SourceVersion = strings.TrimSpace(task.ItemFingerprint), strings.TrimSpace(task.SourceVersion)
	if task.Locator == "" {
		return errors.New("PPTX PDF task requires locator")
	}
	loc, err := resourcetree.ParseURI(task.Locator)
	if err != nil {
		return fmt.Errorf("PPTX PDF locator is invalid: %w", err)
	}
	if task.SourceEngineID != 0 && loc.EngineID != task.SourceEngineID {
		return errors.New("PPTX PDF locator engine_id does not match source_engine_id")
	}
	task.SourceEngineID = loc.EngineID
	if loc.Type != resourcetree.TypeFile && loc.Type != resourcetree.TypeObject {
		return errors.New("PPTX PDF locator must identify a file or object")
	}
	if !strings.EqualFold(filepath.Ext(loc.FullName()), ".pptx") {
		return errors.New("PPTX PDF source must use the .pptx extension")
	}
	if loc.ItemID == nil || *loc.ItemID == 0 {
		return errors.New("PPTX PDF locator must reference a scanned item with item_id")
	}
	if task.ItemID != 0 && task.ItemID != *loc.ItemID {
		return errors.New("PPTX PDF locator item_id does not match item_id")
	}
	task.ItemID = *loc.ItemID
	expectedFingerprint := commonModels.GenerateItemFingerprint(task.SourceEngineID, loc.FullName())
	if task.ItemFingerprint != "" && task.ItemFingerprint != expectedFingerprint {
		return errors.New("PPTX PDF item_fingerprint does not match source locator")
	}
	task.ItemFingerprint = expectedFingerprint
	task.ArtifactVariant = models.PPTXPDFArtifactVariant
	task.Schedule, task.NextRunAt = "", nil
	return nil
}

func pptxPDFTarget(task *models.PPTXPDFTask, bucket string) (string, string) {
	name := "presentation.pdf"
	if loc, err := resourcetree.ParseURI(task.Locator); err == nil {
		base := filepath.Base(loc.FullName())
		if strings.EqualFold(filepath.Ext(base), ".pptx") {
			base = strings.TrimSuffix(base, filepath.Ext(base))
		}
		if strings.TrimSpace(base) != "" {
			name = base + ".pdf"
		}
	}
	objectName := joinFilePath(fmt.Sprintf("tenant_%d/document-preview/%s", task.TenantID, task.ItemFingerprint), name)
	return rastercogref.ObjectStorageRef(firstNonEmptyConfig(bucket, "manager"), objectName), name
}

func pptxPDFContentURL(id uint) string {
	if id == 0 {
		return ""
	}
	return fmt.Sprintf("/api/v1/manager/pptx_pdf/%d/content", id)
}
