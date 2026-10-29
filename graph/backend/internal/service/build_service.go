package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/contentio"
	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	graphi18n "github.com/addp/graph/i18n"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/google/uuid"
)

const (
	minioBucket        = "graph"
	buildMaterialsDir  = "build/"
	copilotExtractPath = "/api/v1/copilot/kg-build/extract"
)

var ErrReviewBatchFailed = errors.New("one or more review items failed")

// BuildService 图谱构建业务逻辑
type BuildService struct {
	buildRepo          *repository.BuildRepository
	ontologyRepo       *repository.OntologyRepository
	ontologySvc        *OntologyService
	graphRepo          *repository.KnowledgeGraphRepository
	taskExecutionRepo  *commonExecution.TaskExecutionRepository
	graphDataSvc       *GraphDataService
	materialReader     contentio.Reader
	materialWriter     contentio.Writer
	copilotURL         string
	serviceTokenSource commonClient.ServiceTokenProvider
	httpClient         *http.Client

	supervisorMu     sync.Mutex
	supervisorCancel context.CancelFunc
	supervisorDone   chan struct{}
}

func NewBuildService(
	buildRepo *repository.BuildRepository,
	ontologyRepo *repository.OntologyRepository,
	ontologySvc *OntologyService,
	graphRepo *repository.KnowledgeGraphRepository,
	taskExecutionRepo *commonExecution.TaskExecutionRepository,
	graphDataSvc *GraphDataService,
	materialReader contentio.Reader,
	materialWriter contentio.Writer,
	copilotURL string,
	serviceTokenSource commonClient.ServiceTokenProvider,
) *BuildService {
	return &BuildService{
		buildRepo:          buildRepo,
		ontologyRepo:       ontologyRepo,
		ontologySvc:        ontologySvc,
		graphRepo:          graphRepo,
		taskExecutionRepo:  taskExecutionRepo,
		graphDataSvc:       graphDataSvc,
		materialReader:     materialReader,
		materialWriter:     materialWriter,
		copilotURL:         copilotURL,
		serviceTokenSource: serviceTokenSource,
		httpClient:         &http.Client{Timeout: 120 * time.Second},
	}
}

// ============ 任务管理 ============

func (s *BuildService) ListTasks(graphID, tenantID uint) ([]models.BuildTask, error) {
	return s.buildRepo.ListTasks(graphID, tenantID)
}

func (s *BuildService) ListAllTasks(tenantID uint) ([]models.BuildTask, error) {
	return s.buildRepo.ListAllTasks(tenantID)
}

func (s *BuildService) GetTask(id, tenantID uint) (*models.BuildTask, error) {
	task, err := s.buildRepo.GetTask(id, tenantID)
	if err != nil {
		return nil, err
	}
	mats, _ := s.buildRepo.ListMaterials(id, tenantID)
	task.Materials = mats
	// 填充 pending review 数量
	pending, _ := s.buildRepo.CountPendingReview(task.GraphID, tenantID)
	_ = pending // 前端通过 /review 接口单独获取
	return task, nil
}

func (s *BuildService) CreateTask(task *models.BuildTask) error {
	return s.buildRepo.CreateTask(task)
}

func (s *BuildService) DeleteTask(ctx context.Context, id, graphID, tenantID uint) error {
	return s.buildRepo.DeleteTask(ctx, id, graphID, tenantID)
}

// ============ 材料管理 ============

func (s *BuildService) ListMaterials(taskID, tenantID uint) ([]models.BuildMaterial, error) {
	return s.buildRepo.ListMaterials(taskID, tenantID)
}

// UploadMaterial 上传材料到 MinIO，创建数据库记录
func (s *BuildService) UploadMaterial(ctx context.Context, taskID, tenantID, graphID uint, fileName string, reader io.Reader, fileSize int64) (*models.BuildMaterial, error) {
	if err := s.buildRepo.CheckMaterialAdmission(ctx, taskID, graphID, tenantID); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%s%d/%d/%d/%s", buildMaterialsDir, tenantID, graphID, taskID, uuid.NewString())
	if s.materialWriter == nil {
		return nil, fmt.Errorf("材料写入器未初始化")
	}
	wc, err := s.materialWriter.Create(ctx, contentio.NewRef(minioBucket+"/"+key, contentio.RoleMain))
	if err != nil {
		return nil, fmt.Errorf("创建材料文件失败: %w", err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(wc, hash), reader)
	if copyErr != nil {
		_ = wc.Close()
		return nil, fmt.Errorf("upload material: %w", copyErr)
	}
	if err := wc.Close(); err != nil {
		return nil, fmt.Errorf("上传文件失败: %w", err)
	}
	if size != fileSize {
		return nil, fmt.Errorf("material size changed during upload")
	}
	mat := &models.BuildMaterial{
		TaskID:        taskID,
		TenantID:      tenantID,
		GraphID:       graphID,
		Type:          "document",
		FileName:      fileName,
		FilePath:      key,
		FileSize:      size,
		ContentSHA256: hex.EncodeToString(hash.Sum(nil)),
		Status:        models.BuildMaterialStatusPending,
	}
	if err := s.buildRepo.CreateMaterial(ctx, mat); err != nil {
		return nil, err
	}
	return mat, nil
}

func (s *BuildService) DeleteMaterial(ctx context.Context, id, taskID, graphID, tenantID uint) error {
	return s.buildRepo.DeleteMaterial(ctx, id, taskID, graphID, tenantID)
}

// ============ 任务执行 ============

// RunTask 异步启动构建任务
func (s *BuildService) RunTaskWithContext(
	ctx context.Context,
	taskID, graphID, tenantID, userID uint,
	triggerType string,
	source string,
	parentExecutionID *string,
) (string, error) {
	return s.enqueueTask(ctx, taskID, graphID, tenantID, userID, triggerType, source, parentExecutionID, repository.BuildExecutionClaimRun)
}

func (s *BuildService) enqueueTask(
	ctx context.Context,
	taskID, graphID, tenantID, userID uint,
	triggerType string,
	source string,
	parentExecutionID *string,
	claimMode repository.BuildExecutionClaimMode,
) (string, error) {
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return "", err
	}
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		normalizedSource = commonExecution.ModuleGraph
	}

	materials, err := s.buildRepo.ListMaterials(taskID, tenantID)
	if err != nil {
		return "", err
	}
	verified := make([]models.FrozenBuildMaterial, 0, len(materials))
	for _, mat := range materials {
		if s.materialReader == nil {
			return "", fmt.Errorf("material reader is required")
		}
		rc, err := s.materialReader.Open(ctx, contentio.NewRef(minioBucket+"/"+mat.FilePath, contentio.RoleMain))
		if err != nil {
			return "", err
		}
		hash := sha256.New()
		size, readErr := io.Copy(hash, rc)
		closeErr := rc.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return "", err
		}
		digest := hex.EncodeToString(hash.Sum(nil))
		if mat.ContentSHA256 != "" && mat.ContentSHA256 != digest {
			return "", fmt.Errorf("material content identity changed")
		}
		verified = append(verified, models.FrozenBuildMaterial{Material: mat, SHA256: digest, Size: size})
	}
	kg, target, err := s.graphDataSvc.getGraphAndEngine(ctx, graphID, tenantID)
	if err != nil {
		return "", err
	}
	target.close()
	executionID := uuid.NewString()
	uid := int(userID)
	execution := &commonExecution.TaskExecution{TenantID: int(tenantID), ExecutionID: executionID, Module: commonExecution.ModuleGraph, TaskType: commonExecution.TaskTypeKGBuild, Source: normalizedSource, ParentExecutionID: parentExecutionID, Status: commonExecution.ExecutionStatusPending, Progress: 0, TriggerType: normalizedTriggerType, TriggeredBy: &uid}
	if _, _, err := s.buildRepo.EnqueueExecution(ctx, taskID, graphID, tenantID, execution, claimMode, verified, target.Engine.Version, *kg); err != nil {
		return "", err
	}

	return executionID, nil
}

func (s *BuildService) RunTaskByIDWithContext(
	ctx context.Context,
	taskID, tenantID, userID uint,
	triggerType string,
	source string,
	parentExecutionID *string,
) (string, error) {
	task, err := s.buildRepo.GetTask(taskID, tenantID)
	if err != nil {
		return "", fmt.Errorf("任务不存在: %w", err)
	}
	return s.RunTaskWithContext(ctx, taskID, task.GraphID, tenantID, userID, triggerType, source, parentExecutionID)
}

// CancelTask durably addresses the exact observed execution and attempt.
func (s *BuildService) CancelTask(ctx context.Context, taskID, graphID, tenantID, userID uint, executionID string, attempt int) (*commonExecution.TaskExecution, error) {
	return s.buildRepo.CancelExecution(ctx, taskID, graphID, tenantID, userID, executionID, attempt)
}

// RerunTask 重置任务状态并重新执行（对 success/failed/cancelled 任务适用）
func (s *BuildService) RerunTask(ctx context.Context, taskID, graphID, tenantID, userID uint) (string, error) {
	return s.enqueueTask(
		ctx, taskID, graphID, tenantID, userID,
		commonExecution.TriggerTypeManual, commonExecution.ModuleGraph, nil,
		repository.BuildExecutionClaimRerun,
	)
}

// ============ 核心执行逻辑 ============

func (s *BuildService) executeTask(ctx context.Context, task *models.BuildTask, materials []models.BuildMaterial, tenantID int, executionID string) {
	autoWritten := 0
	pendingReview := 0
	processed := 0
	var materialErrors []error
	if s.finishCancelledIfRequested(ctx, task, tenantID, executionID) {
		return
	}

	input, ok := buildInputFromContext(ctx)
	if !ok {
		s.failTask(ctx, task, tenantID, executionID, "immutable build input is required")
		return
	}
	ontologySchema := buildOntologySchema(&input.Ontology)
	ancestorChains := buildAncestorChains(&input.Ontology)
	spatialLayerLookup := buildSpatialLayerMappingsByEntityType(input.Ontology.EntityTypes)
	if err := s.syncSpatialLayersBeforeBuild(ctx, task.GraphID, uint(tenantID), spatialLayerLookup); err != nil {
		s.failTask(ctx, task, tenantID, executionID, err.Error())
		return
	}

	total := len(materials)
	for i, mat := range materials {
		if s.finishCancelledIfRequested(ctx, task, tenantID, executionID) {
			return
		}

		mat.Status = models.BuildMaterialStatusProcessing
		err := s.buildRepo.UpdateMaterial(ctx, &mat)
		written, queued := 0, 0
		if err == nil {
			written, queued, err = s.processMaterial(ctx, task, &mat, ontologySchema, spatialLayerLookup, ancestorChains, uint(tenantID))
		}
		if ctx.Err() != nil {
			s.finishCancelledIfRequested(ctx, task, tenantID, executionID)
			return
		}

		if err != nil {
			mat.Status = models.BuildMaterialStatusFailed
			mat.ErrorMessage = err.Error()
			// 继续处理其他材料，不中断任务
		} else {
			now := time.Now()
			mat.Status = models.BuildMaterialStatusCompleted
			mat.ProcessedAt = &now
		}
		err = errors.Join(err, s.buildRepo.UpdateMaterial(ctx, &mat))
		if err != nil {
			materialErrors = append(materialErrors, err)
		}
		// Successful writes from a failed material remain committed and counted.
		autoWritten += written
		pendingReview += queued
		processed++

		// 更新 Monitor 进度
		progress := int(float64(i+1) / float64(total) * 100)
		if err := s.buildRepo.ProgressBuild(ctx, progress, commonModels.JSONMap{
			"processed_materials": processed,
			"total_materials":     total,
			"auto_written":        autoWritten,
			"pending_review":      pendingReview,
		}); err != nil {
			return
		}
	}
	if s.finishCancelledIfRequested(ctx, task, tenantID, executionID) {
		return
	}

	// 完成
	statsBytes, _ := json.Marshal(models.BuildTaskStats{
		TotalMaterials: total,
		Processed:      processed,
		AutoWritten:    autoWritten,
		PendingReview:  pendingReview,
	})
	task.Stats = statsBytes

	rw := int64(autoWritten)
	status, errorMessage := commonExecution.ExecutionStatusSuccess, ""
	if err := errors.Join(materialErrors...); err != nil {
		status, errorMessage = commonExecution.ExecutionStatusFailed, err.Error()
	}
	if err := s.finishTask(ctx, task, tenantID, executionID, status, errorMessage, commonModels.JSONMap{
		"auto_written":    autoWritten,
		"pending_review":  pendingReview,
		"total_materials": total,
	}, &rw); err != nil {
		log.Printf("failed to finish graph execution %s: %v", executionID, err)
	}
}

func (s *BuildService) failTask(ctx context.Context, task *models.BuildTask, tenantID int, executionID, msg string) {
	if executionID == "" {
		return
	}
	if err := s.finishTask(ctx, task, tenantID, executionID, commonExecution.ExecutionStatusFailed, msg, nil, nil); err != nil {
		log.Printf("failed to persist graph execution %s failure: %v", executionID, err)
	}
}

func (s *BuildService) finishCancelledIfRequested(ctx context.Context, task *models.BuildTask, tenantID int, executionID string) bool {
	requested, err := s.buildRepo.CancellationRequested(context.WithoutCancel(ctx), executionID, tenantID)
	if err != nil || (!requested && ctx.Err() == nil) {
		return false
	}
	status := commonExecution.ExecutionStatusCancelled
	if !requested {
		status = commonExecution.ExecutionStatusFailed
	}
	if err := s.finishTask(context.WithoutCancel(ctx), task, tenantID, executionID, status, "", nil, nil); err != nil {
		log.Printf("graph cancellation remains unresolved: %v", err)
	}
	return true
}

func (s *BuildService) finishTask(ctx context.Context, task *models.BuildTask, tenantID int, executionID, status, errorMessage string, metadata commonModels.JSONMap, recordsWritten *int64) error {
	return s.buildRepo.CompleteBuild(ctx, task, status, errorMessage, metadata, recordsWritten)
}

// syncSpatialLayersBeforeBuild 在构建前自动同步所有空间图层（幂等，含继承类型）
func (s *BuildService) syncSpatialLayersBeforeBuild(ctx context.Context, graphID, tenantID uint, spatialLayerLookup map[string]SpatialLayerMapping) error {
	for _, mapping := range spatialLayerLookup {
		err := s.graphDataSvc.SyncSpatialLayerByConfig(ctx, graphID, tenantID, mapping.Config)
		if err != nil {
			return err
		}
	}
	return nil
}

// processMaterial 处理单份材料（分块 + 调 Copilot + 写入/审核）
// 返回：autoWritten, pendingReview, error
func (s *BuildService) processMaterial(ctx context.Context, task *models.BuildTask, mat *models.BuildMaterial, ontology *ontologySchemaDTO, spatialLayerLookup map[string]SpatialLayerMapping, ancestorChains map[string][]string, tenantID uint) (int, int, error) {
	// 从 MinIO 读取文件内容
	rc, err := s.materialReader.Open(ctx, contentio.NewRef(minioBucket+"/"+mat.FilePath, contentio.RoleMain))
	if err != nil {
		return 0, 0, fmt.Errorf("读取文件失败: %w", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return 0, 0, fmt.Errorf("读取文件失败: %w", err)
	}
	input, ok := buildInputFromContext(ctx)
	if !ok {
		return 0, 0, fmt.Errorf("immutable material input is required")
	}
	var frozen *models.FrozenBuildMaterial
	for i := range input.Materials {
		if input.Materials[i].Material.ID == mat.ID {
			frozen = &input.Materials[i]
			break
		}
	}
	digest := sha256.Sum256(data)
	if frozen == nil || frozen.Material.FilePath != mat.FilePath || frozen.Size != int64(len(data)) || frozen.SHA256 != hex.EncodeToString(digest[:]) {
		return 0, 0, fmt.Errorf("material content identity changed")
	}
	text := string(data)

	// 分块
	chunks := splitText(text, task.ChunkSize, task.ChunkOverlap)
	docContext := extractDocContext(text, task.DocContextSize)

	if mat.TotalChunks == 0 {
		mat.TotalChunks = len(chunks)
		if err := s.buildRepo.UpdateMaterial(ctx, mat); err != nil {
			return 0, 0, err
		}
	}

	autoWritten := 0
	pendingReview := 0
	// 材料内去重：key = "type::uniqueVal"，value = elementID 或 "queued"
	seenEntities := make(map[string]string)

	startFrom := mat.ProcessedChunks
	for i := startFrom; i < len(chunks); i++ {
		select {
		case <-ctx.Done():
			return autoWritten, pendingReview, fmt.Errorf("任务已取消")
		default:
		}

		result, err := s.callCopilotExtract(ctx, tenantID, chunks[i], docContext, ontology, task.ConfidenceThreshold)
		if err != nil {
			mat.ProcessedChunks = i
			mat.ErrorMessage = err.Error()
			return autoWritten, pendingReview, errors.Join(err, s.buildRepo.UpdateMaterial(ctx, mat))
		}

		aw, pv, err := s.processChunkResult(ctx, task, mat, result, ontology, spatialLayerLookup, ancestorChains, seenEntities, tenantID)
		autoWritten += aw
		pendingReview += pv
		if err != nil {
			mat.ErrorMessage = err.Error()
			return autoWritten, pendingReview, errors.Join(err, s.buildRepo.UpdateMaterial(ctx, mat))
		}

		mat.ProcessedChunks = i + 1
		if err := s.buildRepo.UpdateMaterial(ctx, mat); err != nil {
			return autoWritten, pendingReview, err
		}
	}

	return autoWritten, pendingReview, nil
}

// processChunkResult 处理单个 chunk 的抽取结果
func (s *BuildService) processChunkResult(
	ctx context.Context,
	task *models.BuildTask,
	mat *models.BuildMaterial,
	result *copilotExtractResponse,
	ontology *ontologySchemaDTO,
	spatialLayerLookup map[string]SpatialLayerMapping,
	ancestorChains map[string][]string,
	seenEntities map[string]string,
	tenantID uint,
) (autoWritten, pendingReview int, err error) {
	uniqueFields, err := validateBuildExtraction(result, ontology)
	if err != nil {
		return 0, 0, err
	}

	// ——— 实体处理 ———
	for _, entity := range result.Entities {
		uniqueField := uniqueFields[entity.Type]
		uniqueVal := entity.Properties[uniqueField]
		key := entity.Type + "::" + fmt.Sprint(uniqueVal)

		if entity.Confidence >= task.ConfidenceThreshold {
			// 不跳过已见实体：后续 chunk 可能携带更完整的属性（如空间坐标），
			// 始终调用 MergeEntity 实现"渐进叠加"语义，配合 nonEmptyProperties 的
			// nil/空值过滤，确保属性只增不减。
			labels := ancestorChains[entity.Type]
			if len(labels) == 0 {
				labels = []string{entity.Type}
			}
			elementID, err := s.graphDataSvc.MergeEntity(ctx, task.GraphID, tenantID,
				labels, uniqueField, uniqueVal, entity.Properties)
			if err != nil {
				return autoWritten, pendingReview, err
			}
			seenEntities[key] = elementID
			autoWritten++
			// 注册到空间图层（若该 label 或其祖先有空间图层配置）
			if elementID != "" && spatialLayerLookup != nil {
				if mapping, ok := spatialLayerLookup[entity.Type]; ok && mapping.Config != nil {
					if err := s.graphDataSvc.AddNodeToSpatialLayer(ctx, task.GraphID, tenantID, elementID, mapping.Config.LayerName); err != nil {
						return autoWritten, pendingReview, err
					}
				}
			}
		} else {
			if _, seen := seenEntities[key]; seen {
				continue
			}
			content := buildEntityReviewContent(entity, uniqueField, uniqueVal, ancestorChains[entity.Type])
			item := &models.ReviewItem{
				TaskID:     task.ID,
				MaterialID: mat.ID,
				TenantID:   tenantID,
				GraphID:    task.GraphID,
				ItemType:   models.ReviewItemEntity,
				Content:    mustMarshalJSON(content),
				Confidence: entity.Confidence,
				SourceText: entity.SourceText,
				Status:     models.ReviewStatusPending,
			}
			if err := s.buildRepo.CreateReviewItem(ctx, item); err != nil {
				return autoWritten, pendingReview, err
			}
			seenEntities[key] = "queued"
			pendingReview++
		}
	}

	// ——— 关系处理 ———
	entityTempIDMap := buildTempIDMap(result.Entities)
	seenRelations := make(map[string]bool)
	for _, rel := range result.Relations {
		srcEntity := entityTempIDMap[rel.SourceTempID]
		tgtEntity := entityTempIDMap[rel.TargetTempID]
		if srcEntity == nil || tgtEntity == nil {
			continue // 引用了不存在的实体
		}

		rt := getRelationType(rel.Type, ontology)
		srcUniqueField := uniqueFields[srcEntity.Type]
		tgtUniqueField := uniqueFields[tgtEntity.Type]
		srcVal := srcEntity.Properties[srcUniqueField]
		tgtVal := tgtEntity.Properties[tgtUniqueField]

		relKey := fmt.Sprintf("%v::%s::%v", srcVal, rel.Type, tgtVal)
		if seenRelations[relKey] {
			continue
		}
		seenRelations[relKey] = true

		if rel.Confidence >= task.ConfidenceThreshold {
			// 只有 source/target 节点已写入图数据库 才写关系
			srcKey := srcEntity.Type + "::" + fmt.Sprint(srcVal)
			tgtKey := tgtEntity.Type + "::" + fmt.Sprint(tgtVal)
			if seenEntities[srcKey] == "" || seenEntities[srcKey] == "queued" {
				continue
			}
			if seenEntities[tgtKey] == "" || seenEntities[tgtKey] == "queued" {
				continue
			}
			srcLabels := nodeLabelsForEntityType(srcEntity.Type, ancestorChains)
			tgtLabels := nodeLabelsForEntityType(tgtEntity.Type, ancestorChains)
			_, err := s.graphDataSvc.MergeRelation(ctx, task.GraphID, tenantID,
				rel.Type,
				srcLabels, srcUniqueField, srcVal,
				tgtLabels, tgtUniqueField, tgtVal,
				rel.Properties)
			if err != nil {
				return autoWritten, pendingReview, err
			}
			autoWritten++
		} else {
			srcTypeName := srcEntity.Type
			tgtTypeName := tgtEntity.Type
			if rt != nil {
				srcTypeName = rt.SourceType
				tgtTypeName = rt.TargetType
			}
			content := map[string]interface{}{
				"type":                rel.Type,
				"source_type":         srcTypeName,
				"source_node_labels":  nodeLabelsForEntityType(srcEntity.Type, ancestorChains),
				"source_unique_field": srcUniqueField,
				"source_unique_value": srcVal,
				"target_type":         tgtTypeName,
				"target_node_labels":  nodeLabelsForEntityType(tgtEntity.Type, ancestorChains),
				"target_unique_field": tgtUniqueField,
				"target_unique_value": tgtVal,
				"properties":          rel.Properties,
			}
			item := &models.ReviewItem{
				TaskID:     task.ID,
				MaterialID: mat.ID,
				TenantID:   tenantID,
				GraphID:    task.GraphID,
				ItemType:   models.ReviewItemRelation,
				Content:    mustMarshalJSON(content),
				Confidence: rel.Confidence,
				SourceText: rel.SourceText,
				Status:     models.ReviewStatusPending,
			}
			if err := s.buildRepo.CreateReviewItem(ctx, item); err != nil {
				return autoWritten, pendingReview, err
			}
			pendingReview++
		}
	}

	return
}

// ============ 审核操作 ============

func (s *BuildService) ListReviewItems(filter repository.ReviewFilter) ([]models.ReviewItem, int64, error) {
	return s.buildRepo.ListReviewItems(filter)
}

func (s *BuildService) executeReview(ctx context.Context, graphID, itemID, tenantID, userID uint, status string, content map[string]interface{}) error {
	item, err := s.buildRepo.BeginReview(ctx, graphID, itemID, tenantID, userID)
	if err != nil {
		return err
	}
	var elementID string
	var finalBytes []byte
	if status != models.ReviewStatusRejected {
		scope := &effectScope{repo: s.buildRepo, graphID: graphID, tenantID: tenantID, reviewItemID: item.ID, commandID: item.ActiveCommandID}
		callCtx := context.WithValue(ctx, effectScopeKey{}, scope)
		if content == nil {
			elementID, err = s.writeReviewItemToGraph(callCtx, item)
		} else {
			finalBytes = mustMarshalJSON(content)
			elementID, err = s.writeReviewItemToGraphWithContent(callCtx, item, content)
		}
	}
	finishErr := s.buildRepo.EndReview(context.WithoutCancel(ctx), item, status, elementID, finalBytes, err)
	return errors.Join(err, finishErr)
}

func (s *BuildService) ApproveReviewItem(ctx context.Context, graphID, itemID, tenantID, userID uint) error {
	return s.executeReview(ctx, graphID, itemID, tenantID, userID, models.ReviewStatusApproved, nil)
}
func (s *BuildService) RejectReviewItem(ctx context.Context, graphID, itemID, tenantID, userID uint) error {
	return s.executeReview(ctx, graphID, itemID, tenantID, userID, models.ReviewStatusRejected, nil)
}
func (s *BuildService) ModifyAndApproveReviewItem(ctx context.Context, graphID, itemID, tenantID, userID uint, content map[string]interface{}) error {
	return s.executeReview(ctx, graphID, itemID, tenantID, userID, models.ReviewStatusModified, content)
}

func (s *BuildService) BatchReview(ctx context.Context, graphID, tenantID, userID uint, ids []uint, action string) error {
	if action != "approve" && action != "reject" {
		return fmt.Errorf("%w: unsupported review action", commonAPI.ErrBadRequest)
	}
	if len(ids) == 0 || len(ids) > 100 {
		return fmt.Errorf("%w: review batch must contain 1-100 IDs", commonAPI.ErrBadRequest)
	}
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; id == 0 || duplicate {
			return fmt.Errorf("%w: review IDs must be positive and distinct", commonAPI.ErrBadRequest)
		}
		seen[id] = struct{}{}
	}
	// Validate the entire ownership boundary before any graph or review mutation.
	for _, id := range ids {
		if _, err := s.buildRepo.GetReviewItem(id, graphID, tenantID); err != nil {
			return err
		}
	}
	var failures []error
	for _, id := range ids {
		var err error
		if action == "approve" {
			err = s.ApproveReviewItem(ctx, graphID, id, tenantID, userID)
		} else {
			err = s.RejectReviewItem(ctx, graphID, id, tenantID, userID)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("review item %d: %w", id, err))
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("%w: %w", ErrReviewBatchFailed, errors.Join(failures...))
	}
	return nil
}

func (s *BuildService) CountPendingReview(graphID, tenantID uint) (int64, error) {
	return s.buildRepo.CountPendingReview(graphID, tenantID)
}

// ============ 辅助：将审核项写入图数据库 ============

func (s *BuildService) writeReviewItemToGraph(ctx context.Context, item *models.ReviewItem) (string, error) {
	var content map[string]interface{}
	if err := json.Unmarshal(item.Content, &content); err != nil {
		return "", err
	}
	return s.writeReviewItemToGraphWithContent(ctx, item, content)
}

func (s *BuildService) writeReviewItemToGraphWithContent(ctx context.Context, item *models.ReviewItem, content map[string]interface{}) (string, error) {
	if item.ItemType == models.ReviewItemEntity {
		entityType, _ := content["type"].(string)
		labels := jsonStringSlice(content["node_labels"])
		if len(labels) == 0 {
			labels = []string{entityType}
		}
		uniqueField, _ := content["unique_key_field"].(string)
		uniqueValue := content["unique_key_value"]
		properties, _ := content["properties"].(map[string]interface{})
		if properties == nil {
			properties = make(map[string]interface{})
		}
		return s.graphDataSvc.MergeEntity(ctx, item.GraphID, item.TenantID,
			labels, uniqueField, uniqueValue, properties)
	}

	// relation
	relType, _ := content["type"].(string)
	srcType, _ := content["source_type"].(string)
	srcLabels := jsonStringSlice(content["source_node_labels"])
	if len(srcLabels) == 0 {
		srcLabels = []string{srcType}
	}
	srcField, _ := content["source_unique_field"].(string)
	srcVal := content["source_unique_value"]
	tgtType, _ := content["target_type"].(string)
	tgtLabels := jsonStringSlice(content["target_node_labels"])
	if len(tgtLabels) == 0 {
		tgtLabels = []string{tgtType}
	}
	tgtField, _ := content["target_unique_field"].(string)
	tgtVal := content["target_unique_value"]
	props, _ := content["properties"].(map[string]interface{})
	if props == nil {
		props = make(map[string]interface{})
	}
	return s.graphDataSvc.MergeRelation(ctx, item.GraphID, item.TenantID,
		relType,
		srcLabels, srcField, srcVal,
		tgtLabels, tgtField, tgtVal,
		props)
}

// ============ Copilot 调用 ============

type copilotExtractRequest struct {
	Text                string             `json:"text"`
	DocContext          string             `json:"doc_context"`
	Ontology            *ontologySchemaDTO `json:"ontology"`
	GraphID             uint               `json:"graph_id"`
	ConfidenceThreshold float64            `json:"confidence_threshold"`
}

type copilotExtractResponse struct {
	Entities  []extractedEntity   `json:"entities"`
	Relations []extractedRelation `json:"relations"`
}

type extractedEntity struct {
	TempID     string                 `json:"temp_id"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
	Confidence float64                `json:"confidence"`
	SourceText string                 `json:"source_text"`
}

type extractedRelation struct {
	Type         string                 `json:"type"`
	SourceTempID string                 `json:"source_temp_id"`
	TargetTempID string                 `json:"target_temp_id"`
	Properties   map[string]interface{} `json:"properties"`
	Confidence   float64                `json:"confidence"`
	SourceText   string                 `json:"source_text"`
}

func (s *BuildService) callCopilotExtract(ctx context.Context, tenantID uint, text, docContext string, ontology *ontologySchemaDTO, threshold float64) (*copilotExtractResponse, error) {
	req := copilotExtractRequest{
		Text:                text,
		DocContext:          docContext,
		Ontology:            ontology,
		ConfidenceThreshold: threshold,
	}
	if s.serviceTokenSource == nil {
		return nil, fmt.Errorf("Copilot Service Token Provider 未初始化")
	}
	body, _ := json.Marshal(req)
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.serviceTokenSource.Token(ctx, tenantID)
		if err != nil {
			return nil, fmt.Errorf("获取 Copilot Service Access Token 失败: %w", err)
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.copilotURL+copilotExtractPath, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+token)

		result, callErr := recordExternalCall(ctx, "copilot", body, func() (*copilotExtractResponse, error) {
			resp, err := s.httpClient.Do(httpReq)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				return nil, errCopilotUnauthorized
			}
			if resp.StatusCode != http.StatusOK {
				body, readErr := io.ReadAll(resp.Body)
				if readErr != nil {
					return nil, readErr
				}
				return nil, fmt.Errorf("Copilot request rejected with status %d: %s", resp.StatusCode, string(body))
			}
			var result copilotExtractResponse
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return nil, err
			}
			return &result, nil
		})
		if errors.Is(callErr, errCopilotUnauthorized) && attempt == 0 {
			if invalidator, ok := s.serviceTokenSource.(commonClient.ServiceTokenInvalidator); ok {
				invalidator.InvalidateToken(tenantID, token)
			}
			continue
		}
		return result, callErr
	}
	return nil, fmt.Errorf("Copilot Service Access Token 被拒绝")
}

// ============ 本体 Schema DTO ============

type ontologySchemaDTO struct {
	EntityTypes   []entityTypeDTO   `json:"entity_types"`
	RelationTypes []relationTypeDTO `json:"relation_types"`
}

type entityTypeDTO struct {
	Name        string        `json:"name"`
	Label       string        `json:"label"`
	NodeLabels  []string      `json:"node_labels,omitempty"`
	ParentName  string        `json:"parent_name,omitempty"`
	Description string        `json:"description"`
	Properties  []propertyDTO `json:"properties"`
}

type relationTypeDTO struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	SourceType  string `json:"source_type"`
	TargetType  string `json:"target_type"`
	Description string `json:"description"`
}

type propertyDTO struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	DataType string `json:"data_type"`
	Unique   bool   `json:"unique"`
	Required bool   `json:"required"`
}

func buildOntologySchema(ontology *models.Ontology) *ontologySchemaDTO {
	// 构建 entityType ID → Name 映射（用于解析关系类型的来源/目标）
	idToName := make(map[uint]string, len(ontology.EntityTypes))
	byID := make(map[uint]*models.EntityType, len(ontology.EntityTypes))
	for i := range ontology.EntityTypes {
		idToName[ontology.EntityTypes[i].ID] = ontology.EntityTypes[i].Name
		byID[ontology.EntityTypes[i].ID] = &ontology.EntityTypes[i]
	}

	// 找出有子类的类型（抽象父类），只向 LLM 发送叶子类型
	hasChildren := make(map[uint]bool)
	for _, et := range ontology.EntityTypes {
		if et.ParentID != nil {
			hasChildren[*et.ParentID] = true
		}
	}

	dto := &ontologySchemaDTO{}
	for _, et := range ontology.EntityTypes {
		// 跳过抽象父类（有子类的类型）
		if hasChildren[et.ID] {
			continue
		}
		etDTO := entityTypeDTO{
			Name:        et.Name,
			Label:       et.Label,
			NodeLabels:  effectiveNodeLabels(&et, byID),
			Description: et.Description,
		}
		if et.ParentID != nil {
			etDTO.ParentName = idToName[*et.ParentID]
		}
		// 合并祖先属性（祖先属性先加，子类同名属性覆盖）
		allProps := collectInheritedProperties(&et, byID)
		for _, p := range allProps {
			etDTO.Properties = append(etDTO.Properties, propertyDTO{
				Name:     p.Name,
				Label:    p.Label,
				DataType: p.DataType,
				Unique:   p.Unique,
				Required: p.Required,
			})
		}
		dto.EntityTypes = append(dto.EntityTypes, etDTO)
	}
	for _, rt := range ontology.RelationTypes {
		rtDTO := relationTypeDTO{
			Name:        rt.Name,
			Label:       rt.Label,
			Description: rt.Description,
		}
		if rt.SourceTypeID != nil {
			rtDTO.SourceType = idToName[*rt.SourceTypeID]
		}
		if rt.TargetTypeID != nil {
			rtDTO.TargetType = idToName[*rt.TargetTypeID]
		}
		dto.RelationTypes = append(dto.RelationTypes, rtDTO)
	}
	return dto
}

// collectInheritedProperties 收集实体类型的完整属性（含祖先属性，子类同名属性优先）
func collectInheritedProperties(et *models.EntityType, byID map[uint]*models.EntityType) []models.PropertyDefinition {
	// 收集祖先链（从根到当前）
	chain := []*models.EntityType{}
	visited := make(map[uint]struct{})
	cur := et
	for cur != nil {
		if cur.ID != 0 {
			if _, exists := visited[cur.ID]; exists {
				break
			}
			visited[cur.ID] = struct{}{}
		}
		chain = append([]*models.EntityType{cur}, chain...)
		if cur.ParentID == nil {
			break
		}
		parent, ok := byID[*cur.ParentID]
		if !ok {
			break
		}
		cur = parent
	}
	// 从根到叶合并属性，后者覆盖前者（同名属性子类优先）
	indexes := make(map[string]int)
	var result []models.PropertyDefinition
	for _, node := range chain {
		props, _ := node.ParsedProperties()
		for _, p := range props {
			if index, exists := indexes[p.Name]; exists {
				result[index] = p
			} else {
				indexes[p.Name] = len(result)
				result = append(result, p)
			}
		}
	}
	return result
}

// buildAncestorChains 为本体中每个实体类型构建 Neo4j 节点标签执行映射
// 返回 map[entityTypeName][]string，例如：
//
//	"City"    → ["City", "AOI"]
//	"Company" → ["Company", "POI"]
//	"AOI"     → ["AOI"]
func buildAncestorChains(ontology *models.Ontology) map[string][]string {
	byID := make(map[uint]*models.EntityType, len(ontology.EntityTypes))
	for i := range ontology.EntityTypes {
		byID[ontology.EntityTypes[i].ID] = &ontology.EntityTypes[i]
	}
	result := make(map[string][]string, len(ontology.EntityTypes))
	for _, et := range ontology.EntityTypes {
		result[et.Name] = effectiveNodeLabels(&et, byID)
	}
	return result
}

// ============ 文本分块（语义感知） ============

// splitText 语义感知分块（优先在段落/句子边界截断）
func splitText(text string, chunkSize, overlap int) []string {
	characters := []rune(text)
	if len(characters) == 0 {
		return nil
	}
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	if overlap < 0 {
		overlap = 0
	}

	separators := []string{"\n\n", "\n", "。", "！", "？", ". ", "! ", "? ", "，", ", ", " "}
	var chunks []string

	start := 0
	for start < len(characters) {
		end := start + chunkSize
		if end >= len(characters) {
			chunks = append(chunks, string(characters[start:]))
			break
		}

		// 在 [end-overlap, end] 范围内找最近的语义边界
		cutAt := findSeparator(characters, max(start, end-overlap/2), end, separators)
		if cutAt <= start {
			cutAt = end // 无合适边界，强制截断
		}
		chunks = append(chunks, string(characters[start:cutAt]))

		// 下一个 chunk 从 (cutAt - overlap) 开始
		nextStart := cutAt - overlap
		if nextStart <= start {
			nextStart = cutAt // 防止死循环
		}
		start = nextStart
	}
	return chunks
}

// findSeparator 在 text[from:to] 中从右向左找第一个语义分隔符的位置（返回分隔符之后的位置）
func findSeparator(text []rune, from, to int, separators []string) int {
	window := string(text[from:to])
	for _, sep := range separators {
		idx := strings.LastIndex(window, sep)
		if idx >= 0 {
			return from + utf8.RuneCountInString(window[:idx]) + utf8.RuneCountInString(sep)
		}
	}
	return -1
}

// extractDocContext 提取文档头部上下文
func extractDocContext(text string, size int) string {
	if size <= 0 || len(text) == 0 {
		return ""
	}
	characters := []rune(text)
	if len(characters) <= size {
		return text
	}
	return string(characters[:size])
}

// ============ 辅助函数 ============

// Validate the complete extraction chunk before persisting any of its results.
// A unique constraint is only usable as a merge key when it is unambiguous.
func validateBuildExtraction(result *copilotExtractResponse, ontology *ontologySchemaDTO) (map[string]string, error) {
	fields := map[string]string{}
	invalid := func(code, name string) error {
		// Background execution diagnostics retain a stable code plus the
		// localized default-language description and the offending identifier.
		return fmt.Errorf("%s: %s (%s)", code, commoni18n.ForLanguage(commoni18n.LangZhCN, code), name)
	}
	for _, entity := range result.Entities {
		field, found := fields[entity.Type]
		if !found {
			known, uniqueCount := false, 0
			for _, et := range ontology.EntityTypes {
				if et.Name != entity.Type {
					continue
				}
				known = true
				for _, property := range et.Properties {
					if property.Unique {
						uniqueCount++
						field = property.Name
					}
				}
			}
			if !known {
				return nil, invalid(graphi18n.MsgBuildEntityTypeUnknown, entity.Type)
			}
			if uniqueCount != 1 || strings.TrimSpace(field) == "" {
				return nil, invalid(graphi18n.MsgBuildMergeKeyInvalid, entity.Type)
			}
			fields[entity.Type] = field
		}
		value := entity.Properties[field]
		empty := value == nil
		switch value := value.(type) {
		case string:
			empty = strings.TrimSpace(value) == ""
		case []interface{}:
			empty = len(value) == 0
		case map[string]interface{}:
			empty = len(value) == 0
		}
		if empty {
			return nil, invalid(graphi18n.MsgBuildMergeKeyEmpty, entity.Type+"/"+field)
		}
	}
	for _, relation := range result.Relations {
		if getRelationType(relation.Type, ontology) == nil {
			return nil, invalid(graphi18n.MsgBuildRelationTypeUnknown, relation.Type)
		}
	}
	return fields, nil
}

func getRelationType(relTypeName string, ontology *ontologySchemaDTO) *relationTypeDTO {
	for i := range ontology.RelationTypes {
		if ontology.RelationTypes[i].Name == relTypeName {
			return &ontology.RelationTypes[i]
		}
	}
	return nil
}

func buildEntityReviewContent(entity extractedEntity, uniqueField string, uniqueValue interface{}, nodeLabels []string) map[string]interface{} {
	return map[string]interface{}{
		"type":             entity.Type,
		"unique_key_field": uniqueField,
		"unique_key_value": uniqueValue,
		"properties":       entity.Properties,
		"node_labels":      nodeLabels,
	}
}

func nodeLabelsForEntityType(entityType string, byEntityType map[string][]string) []string {
	if labels := byEntityType[entityType]; len(labels) > 0 {
		return labels
	}
	if strings.TrimSpace(entityType) == "" {
		return nil
	}
	return []string{entityType}
}

func jsonStringSlice(value interface{}) []string {
	switch values := value.(type) {
	case []string:
		return normalizedStringSet(values)
	case []interface{}:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if str := strings.TrimSpace(fmt.Sprint(value)); str != "" {
				result = append(result, str)
			}
		}
		return normalizedStringSet(result)
	default:
		return nil
	}
}

func buildTempIDMap(entities []extractedEntity) map[string]*extractedEntity {
	m := make(map[string]*extractedEntity, len(entities))
	for i := range entities {
		m[entities[i].TempID] = &entities[i]
	}
	return m
}

func mustMarshalJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
