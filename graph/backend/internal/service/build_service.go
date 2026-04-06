package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	commonModels "github.com/addp/common/models"
	commonRepo "github.com/addp/common/repository"
	commonStorage "github.com/addp/common/storage"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
)

const (
	minioBucket        = "graph"
	buildMaterialsDir  = "build/"
	copilotExtractPath = "/api/v1/copilot/kg-build/extract"
)

// BuildService 图谱构建业务逻辑
type BuildService struct {
	buildRepo         *repository.BuildRepository
	ontologyRepo      *repository.OntologyRepository
	graphRepo         *repository.KnowledgeGraphRepository
	taskExecutionRepo *commonRepo.TaskExecutionRepository
	neo4jSvc          *Neo4jService
	minioClient       *commonStorage.MinIOClient
	copilotURL        string
	httpClient        *http.Client

	// 取消令牌（graphID+taskID → cancel func）
	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

func NewBuildService(
	buildRepo *repository.BuildRepository,
	ontologyRepo *repository.OntologyRepository,
	graphRepo *repository.KnowledgeGraphRepository,
	taskExecutionRepo *commonRepo.TaskExecutionRepository,
	neo4jSvc *Neo4jService,
	minioClient *commonStorage.MinIOClient,
	copilotURL string,
) *BuildService {
	return &BuildService{
		buildRepo:         buildRepo,
		ontologyRepo:      ontologyRepo,
		graphRepo:         graphRepo,
		taskExecutionRepo: taskExecutionRepo,
		neo4jSvc:          neo4jSvc,
		minioClient:       minioClient,
		copilotURL:        copilotURL,
		httpClient:        &http.Client{Timeout: 120 * time.Second},
		cancels:           make(map[string]context.CancelFunc),
	}
}

// ============ 任务管理 ============

func (s *BuildService) ListTasks(graphID, tenantID uint) ([]models.BuildTask, error) {
	return s.buildRepo.ListTasks(graphID, tenantID)
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

func (s *BuildService) DeleteTask(id, tenantID uint) error {
	return s.buildRepo.DeleteTask(id, tenantID)
}

// ============ 材料管理 ============

func (s *BuildService) ListMaterials(taskID, tenantID uint) ([]models.BuildMaterial, error) {
	return s.buildRepo.ListMaterials(taskID, tenantID)
}

// UploadMaterial 上传材料到 MinIO，创建数据库记录
func (s *BuildService) UploadMaterial(taskID, tenantID, graphID uint, fileName string, reader io.Reader, fileSize int64) (*models.BuildMaterial, error) {
	key := fmt.Sprintf("%s%d/%d/%d/%s", buildMaterialsDir, tenantID, graphID, taskID, fileName)
	if err := s.minioClient.UploadFile(context.Background(), minioBucket, key, reader, "text/plain"); err != nil {
		return nil, fmt.Errorf("上传文件失败: %w", err)
	}
	mat := &models.BuildMaterial{
		TaskID:   taskID,
		TenantID: tenantID,
		GraphID:  graphID,
		Type:     "document",
		FileName: fileName,
		FilePath: key,
		FileSize: fileSize,
		Status:   models.BuildStatusPending,
	}
	if err := s.buildRepo.CreateMaterial(mat); err != nil {
		return nil, err
	}
	return mat, nil
}

func (s *BuildService) DeleteMaterial(id, tenantID uint) error {
	return s.buildRepo.DeleteMaterial(id, tenantID)
}

// ============ 任务执行 ============

// RunTask 异步启动构建任务
func (s *BuildService) RunTask(ctx context.Context, taskID, graphID, tenantID, userID uint) error {
	task, err := s.buildRepo.GetTask(taskID, tenantID)
	if err != nil {
		return fmt.Errorf("任务不存在: %w", err)
	}
	if task.Status == models.BuildStatusRunning {
		return fmt.Errorf("任务已在运行中")
	}

	materials, err := s.buildRepo.ListMaterials(taskID, tenantID)
	if err != nil || len(materials) == 0 {
		return fmt.Errorf("任务没有可处理的材料")
	}

	// 创建 Monitor 执行记录
	now := time.Now()
	executionID := uuid.New().String()
	uid := int(userID)
	srcID := int(taskID)
	execution := &commonModels.TaskExecution{
		TenantID:       int(tenantID),
		ExecutionID:    executionID,
		Module:         commonModels.ModuleGraph,
		TaskType:       commonModels.TaskTypeKGBuild,
		SourceTaskID:   &srcID,
		SourceTaskName: &task.Name,
		Status:         commonModels.ExecutionStatusPending,
		Progress:       0,
		TriggerType:    commonModels.TriggerTypeManual,
		TriggeredBy:    &uid,
		ExecutionConfig: commonModels.JSONMap{
			"graph_id":             graphID,
			"confidence_threshold": task.ConfidenceThreshold,
			"material_count":       len(materials),
		},
		StartedAt: &now,
	}
	_ = s.taskExecutionRepo.Create(ctx, execution)

	// 保存 execution_id 到 build_task
	task.ExecutionID = executionID
	task.Status = models.BuildStatusRunning
	task.StartedAt = &now
	_ = s.buildRepo.UpdateTask(task)

	// 异步执行
	runCtx, cancel := context.WithCancel(context.Background())
	key := fmt.Sprintf("%d-%d", graphID, taskID)
	s.cancelMu.Lock()
	s.cancels[key] = cancel
	s.cancelMu.Unlock()

	go func() {
		defer func() {
			s.cancelMu.Lock()
			delete(s.cancels, key)
			s.cancelMu.Unlock()
			cancel()
		}()
		s.executeTask(runCtx, task, materials, int(tenantID), executionID)
	}()

	return nil
}

// CancelTask 取消运行中的任务
func (s *BuildService) CancelTask(taskID, graphID, tenantID uint) error {
	task, err := s.buildRepo.GetTask(taskID, tenantID)
	if err != nil {
		return err
	}
	if task.Status != models.BuildStatusRunning {
		return fmt.Errorf("任务未在运行中")
	}

	key := fmt.Sprintf("%d-%d", graphID, taskID)
	s.cancelMu.Lock()
	cancel, ok := s.cancels[key]
	s.cancelMu.Unlock()
	if ok {
		cancel()
	}

	task.Status = models.BuildStatusCancelled
	now := time.Now()
	task.CompletedAt = &now
	_ = s.buildRepo.UpdateTask(task)

	if task.ExecutionID != "" {
		_ = s.taskExecutionRepo.UpdateFields(context.Background(), task.ExecutionID, int(tenantID), map[string]interface{}{
			"status":       commonModels.ExecutionStatusCancelled,
			"completed_at": now,
		})
	}
	return nil
}

// ============ 核心执行逻辑 ============

func (s *BuildService) executeTask(ctx context.Context, task *models.BuildTask, materials []models.BuildMaterial, tenantID int, executionID string) {
	autoWritten := 0
	pendingReview := 0
	processed := 0

	// 加载本体快照
	kg, err := s.graphRepo.GetByID(task.GraphID, uint(tenantID))
	if err != nil {
		s.failTask(ctx, task, tenantID, executionID, fmt.Sprintf("获取图谱失败: %v", err))
		return
	}
	ontology, err := s.ontologyRepo.GetDetail(kg.OntologyID, uint(tenantID))
	if err != nil {
		s.failTask(ctx, task, tenantID, executionID, fmt.Sprintf("获取本体失败: %v", err))
		return
	}
	ontologySchema := buildOntologySchema(ontology)

	total := len(materials)
	for i, mat := range materials {
		select {
		case <-ctx.Done():
			return
		default:
		}

		mat.Status = "processing"
		_ = s.buildRepo.UpdateMaterial(&mat)

		written, queued, err := s.processMaterial(ctx, task, &mat, ontologySchema, uint(tenantID))
		if err != nil {
			mat.Status = models.BuildStatusFailed
			mat.ErrorMessage = err.Error()
			_ = s.buildRepo.UpdateMaterial(&mat)
			// 继续处理其他材料，不中断任务
		} else {
			now := time.Now()
			mat.Status = models.BuildStatusCompleted
			mat.ProcessedAt = &now
			_ = s.buildRepo.UpdateMaterial(&mat)
			autoWritten += written
			pendingReview += queued
		}
		processed++

		// 更新 Monitor 进度
		progress := int(float64(i+1) / float64(total) * 100)
		_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, tenantID, map[string]interface{}{
			"status":   commonModels.ExecutionStatusRunning,
			"progress": progress,
			"metadata": commonModels.JSONMap{
				"processed_materials": processed,
				"total_materials":     total,
				"auto_written":        autoWritten,
				"pending_review":      pendingReview,
			},
		})
	}

	// 完成
	now := time.Now()
	task.Status = models.BuildStatusCompleted
	task.CompletedAt = &now

	statsBytes, _ := json.Marshal(models.BuildTaskStats{
		TotalMaterials: total,
		Processed:      processed,
		AutoWritten:    autoWritten,
		PendingReview:  pendingReview,
	})
	task.Stats = statsBytes
	_ = s.buildRepo.UpdateTask(task)

	rw := int64(autoWritten)
	_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, tenantID, map[string]interface{}{
		"status":           commonModels.ExecutionStatusSuccess,
		"progress":         100,
		"completed_at":     now,
		"records_written":  rw,
		"metadata": commonModels.JSONMap{
			"auto_written":    autoWritten,
			"pending_review":  pendingReview,
			"total_materials": total,
		},
	})
}

func (s *BuildService) failTask(ctx context.Context, task *models.BuildTask, tenantID int, executionID, msg string) {
	task.Status = models.BuildStatusFailed
	task.ErrorMessage = msg
	now := time.Now()
	task.CompletedAt = &now
	_ = s.buildRepo.UpdateTask(task)

	if executionID != "" {
		_ = s.taskExecutionRepo.UpdateFields(ctx, executionID, tenantID, map[string]interface{}{
			"status":       commonModels.ExecutionStatusFailed,
			"completed_at": now,
			"error_details": commonModels.JSONMap{"message": msg},
		})
	}
}

// processMaterial 处理单份材料（分块 + 调 Copilot + 写入/审核）
// 返回：autoWritten, pendingReview, error
func (s *BuildService) processMaterial(ctx context.Context, task *models.BuildTask, mat *models.BuildMaterial, ontology *ontologySchemaDTO, tenantID uint) (int, int, error) {
	// 从 MinIO 读取文件内容
	text, err := s.minioClient.GetTextContent(ctx, minioBucket, mat.FilePath)
	if err != nil {
		return 0, 0, fmt.Errorf("读取文件失败: %w", err)
	}

	// 分块
	chunks := splitText(text, task.ChunkSize, task.ChunkOverlap)
	docContext := extractDocContext(text, task.DocContextSize)

	if mat.TotalChunks == 0 {
		mat.TotalChunks = len(chunks)
		_ = s.buildRepo.UpdateMaterial(mat)
	}

	autoWritten := 0
	pendingReview := 0
	// 材料内去重：key = "type::uniqueVal"，value = neo4jID 或 "queued"
	seenEntities := make(map[string]string)

	startFrom := mat.ProcessedChunks
	for i := startFrom; i < len(chunks); i++ {
		select {
		case <-ctx.Done():
			return autoWritten, pendingReview, fmt.Errorf("任务已取消")
		default:
		}

		result, err := s.callCopilotExtract(ctx, chunks[i], docContext, ontology, task.ConfidenceThreshold)
		if err != nil {
			mat.ProcessedChunks = i
			mat.ErrorMessage = err.Error()
			_ = s.buildRepo.UpdateMaterial(mat)
			return autoWritten, pendingReview, err
		}

		aw, pv := s.processChunkResult(ctx, task, mat, result, ontology, seenEntities, tenantID)
		autoWritten += aw
		pendingReview += pv

		mat.ProcessedChunks = i + 1
		_ = s.buildRepo.UpdateMaterial(mat)
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
	seenEntities map[string]string,
	tenantID uint,
) (autoWritten, pendingReview int) {

	// ——— 实体处理 ———
	for _, entity := range result.Entities {
		uniqueField := getUniqueField(entity.Type, ontology)
		uniqueVal := getPropertyValue(entity.Properties, uniqueField)
		key := entity.Type + "::" + fmt.Sprint(uniqueVal)

		if entity.Confidence >= task.ConfidenceThreshold {
			if _, seen := seenEntities[key]; seen {
				continue // 材料内已处理，跳过
			}
			neo4jID, err := s.neo4jSvc.MergeEntity(ctx, task.GraphID, tenantID,
				entity.Type, uniqueField, uniqueVal, entity.Properties)
			if err == nil {
				seenEntities[key] = neo4jID
				autoWritten++
			}
		} else {
			if _, seen := seenEntities[key]; seen {
				continue
			}
			content := buildEntityReviewContent(entity, uniqueField, uniqueVal)
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
			if err := s.buildRepo.CreateReviewItem(item); err == nil {
				seenEntities[key] = "queued"
				pendingReview++
			}
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
		srcUniqueField := getUniqueField(srcEntity.Type, ontology)
		tgtUniqueField := getUniqueField(tgtEntity.Type, ontology)
		srcVal := getPropertyValue(srcEntity.Properties, srcUniqueField)
		tgtVal := getPropertyValue(tgtEntity.Properties, tgtUniqueField)

		relKey := fmt.Sprintf("%v::%s::%v", srcVal, rel.Type, tgtVal)
		if seenRelations[relKey] {
			continue
		}
		seenRelations[relKey] = true

		if rel.Confidence >= task.ConfidenceThreshold {
			// 只有 source/target 节点已写入 Neo4j 才写关系
			srcKey := srcEntity.Type + "::" + fmt.Sprint(srcVal)
			tgtKey := tgtEntity.Type + "::" + fmt.Sprint(tgtVal)
			if seenEntities[srcKey] == "" || seenEntities[srcKey] == "queued" {
				continue
			}
			if seenEntities[tgtKey] == "" || seenEntities[tgtKey] == "queued" {
				continue
			}
			_, err := s.neo4jSvc.MergeRelation(ctx, task.GraphID, tenantID,
				rel.Type,
				srcEntity.Type, srcUniqueField, srcVal,
				tgtEntity.Type, tgtUniqueField, tgtVal,
				rel.Properties)
			if err == nil {
				autoWritten++
			}
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
				"source_unique_field": srcUniqueField,
				"source_unique_value": srcVal,
				"target_type":         tgtTypeName,
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
			if err := s.buildRepo.CreateReviewItem(item); err == nil {
				pendingReview++
			}
		}
	}

	return
}

// ============ 审核操作 ============

func (s *BuildService) ListReviewItems(filter repository.ReviewFilter) ([]models.ReviewItem, int64, error) {
	return s.buildRepo.ListReviewItems(filter)
}

func (s *BuildService) ApproveReviewItem(ctx context.Context, itemID, tenantID, userID uint) error {
	item, err := s.buildRepo.GetReviewItem(itemID, tenantID)
	if err != nil {
		return err
	}
	if item.Status != models.ReviewStatusPending {
		return fmt.Errorf("该条目已被处理")
	}

	neo4jID, err := s.writeReviewItemToNeo4j(ctx, item)
	if err != nil {
		return fmt.Errorf("写入 Neo4j 失败: %w", err)
	}

	now := time.Now()
	item.Status = models.ReviewStatusApproved
	item.Neo4jID = neo4jID
	item.ReviewedBy = &userID
	item.ReviewedAt = &now
	return s.buildRepo.UpdateReviewItem(item)
}

func (s *BuildService) RejectReviewItem(ctx context.Context, itemID, tenantID, userID uint) error {
	item, err := s.buildRepo.GetReviewItem(itemID, tenantID)
	if err != nil {
		return err
	}
	if item.Status != models.ReviewStatusPending {
		return fmt.Errorf("该条目已被处理")
	}

	now := time.Now()
	item.Status = models.ReviewStatusRejected
	item.ReviewedBy = &userID
	item.ReviewedAt = &now
	return s.buildRepo.UpdateReviewItem(item)
}

func (s *BuildService) ModifyAndApproveReviewItem(ctx context.Context, itemID, tenantID, userID uint, finalContent map[string]interface{}) error {
	item, err := s.buildRepo.GetReviewItem(itemID, tenantID)
	if err != nil {
		return err
	}
	if item.Status != models.ReviewStatusPending {
		return fmt.Errorf("该条目已被处理")
	}

	finalBytes := mustMarshalJSON(finalContent)
	item.FinalContent = finalBytes

	// 用修改后的内容覆盖写入 Neo4j
	neo4jID, err := s.writeReviewItemToNeo4jWithContent(ctx, item, finalContent)
	if err != nil {
		return fmt.Errorf("写入 Neo4j 失败: %w", err)
	}

	now := time.Now()
	item.Status = models.ReviewStatusModified
	item.Neo4jID = neo4jID
	item.ReviewedBy = &userID
	item.ReviewedAt = &now
	return s.buildRepo.UpdateReviewItem(item)
}

func (s *BuildService) BatchReview(ctx context.Context, graphID, tenantID, userID uint, ids []uint, action string) error {
	for _, id := range ids {
		switch action {
		case "approve":
			_ = s.ApproveReviewItem(ctx, id, tenantID, userID)
		case "reject":
			_ = s.RejectReviewItem(ctx, id, tenantID, userID)
		}
	}
	return nil
}

func (s *BuildService) CountPendingReview(graphID, tenantID uint) (int64, error) {
	return s.buildRepo.CountPendingReview(graphID, tenantID)
}

// ============ 辅助：将审核项写入 Neo4j ============

func (s *BuildService) writeReviewItemToNeo4j(ctx context.Context, item *models.ReviewItem) (string, error) {
	var content map[string]interface{}
	if err := json.Unmarshal(item.Content, &content); err != nil {
		return "", err
	}
	return s.writeReviewItemToNeo4jWithContent(ctx, item, content)
}

func (s *BuildService) writeReviewItemToNeo4jWithContent(ctx context.Context, item *models.ReviewItem, content map[string]interface{}) (string, error) {
	if item.ItemType == models.ReviewItemEntity {
		entityType, _ := content["type"].(string)
		uniqueField, _ := content["unique_key_field"].(string)
		uniqueValue := content["unique_key_value"]
		properties, _ := content["properties"].(map[string]interface{})
		if properties == nil {
			properties = make(map[string]interface{})
		}
		return s.neo4jSvc.MergeEntity(ctx, item.GraphID, item.TenantID,
			entityType, uniqueField, uniqueValue, properties)
	}

	// relation
	relType, _ := content["type"].(string)
	srcType, _ := content["source_type"].(string)
	srcField, _ := content["source_unique_field"].(string)
	srcVal := content["source_unique_value"]
	tgtType, _ := content["target_type"].(string)
	tgtField, _ := content["target_unique_field"].(string)
	tgtVal := content["target_unique_value"]
	props, _ := content["properties"].(map[string]interface{})
	if props == nil {
		props = make(map[string]interface{})
	}
	return s.neo4jSvc.MergeRelation(ctx, item.GraphID, item.TenantID,
		relType,
		srcType, srcField, srcVal,
		tgtType, tgtField, tgtVal,
		props)
}

// ============ Copilot 调用 ============

type copilotExtractRequest struct {
	Text                string          `json:"text"`
	DocContext          string          `json:"doc_context"`
	Ontology            *ontologySchemaDTO `json:"ontology"`
	GraphID             uint            `json:"graph_id"`
	ConfidenceThreshold float64         `json:"confidence_threshold"`
}

type copilotExtractResponse struct {
	Entities []extractedEntity   `json:"entities"`
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

func (s *BuildService) callCopilotExtract(ctx context.Context, text, docContext string, ontology *ontologySchemaDTO, threshold float64) (*copilotExtractResponse, error) {
	req := copilotExtractRequest{
		Text:                text,
		DocContext:          docContext,
		Ontology:            ontology,
		ConfidenceThreshold: threshold,
	}
	body, _ := json.Marshal(req)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.copilotURL+copilotExtractPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("Copilot 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Copilot 返回错误 %d: %s", resp.StatusCode, string(respBody))
	}

	var result copilotExtractResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 Copilot 响应失败: %w", err)
	}
	return &result, nil
}

// ============ 本体 Schema DTO ============

type ontologySchemaDTO struct {
	EntityTypes   []entityTypeDTO   `json:"entity_types"`
	RelationTypes []relationTypeDTO `json:"relation_types"`
}

type entityTypeDTO struct {
	Name        string          `json:"name"`
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Properties  []propertyDTO   `json:"properties"`
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
	for _, et := range ontology.EntityTypes {
		idToName[et.ID] = et.Name
	}

	dto := &ontologySchemaDTO{}
	for _, et := range ontology.EntityTypes {
		etDTO := entityTypeDTO{
			Name:        et.Name,
			Label:       et.Label,
			Description: et.Description,
		}
		props, _ := et.ParsedProperties()
		for _, p := range props {
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

// ============ 文本分块（语义感知） ============

// splitText 语义感知分块（优先在段落/句子边界截断）
func splitText(text string, chunkSize, overlap int) []string {
	if len(text) == 0 {
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
	for start < len(text) {
		end := start + chunkSize
		if end >= len(text) {
			chunks = append(chunks, text[start:])
			break
		}

		// 在 [end-overlap, end] 范围内找最近的语义边界
		cutAt := findSeparator(text, end-overlap/2, end, separators)
		if cutAt <= start {
			cutAt = end // 无合适边界，强制截断
		}
		chunks = append(chunks, text[start:cutAt])

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
func findSeparator(text string, from, to int, separators []string) int {
	for _, sep := range separators {
		idx := strings.LastIndex(text[from:to], sep)
		if idx >= 0 {
			return from + idx + len(sep)
		}
	}
	return -1
}

// extractDocContext 提取文档头部上下文
func extractDocContext(text string, size int) string {
	if size <= 0 || len(text) == 0 {
		return ""
	}
	if len(text) <= size {
		return text
	}
	return text[:size]
}

// ============ 辅助函数 ============

func getUniqueField(entityType string, ontology *ontologySchemaDTO) string {
	for _, et := range ontology.EntityTypes {
		if et.Name == entityType {
			for _, p := range et.Properties {
				if p.Unique {
					return p.Name
				}
			}
		}
	}
	return "name" // fallback
}

func getRelationType(relTypeName string, ontology *ontologySchemaDTO) *relationTypeDTO {
	for i := range ontology.RelationTypes {
		if ontology.RelationTypes[i].Name == relTypeName {
			return &ontology.RelationTypes[i]
		}
	}
	return nil
}

func getPropertyValue(props map[string]interface{}, field string) interface{} {
	if v, ok := props[field]; ok {
		return v
	}
	return ""
}

func buildEntityReviewContent(entity extractedEntity, uniqueField string, uniqueValue interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":             entity.Type,
		"unique_key_field": uniqueField,
		"unique_key_value": uniqueValue,
		"properties":       entity.Properties,
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
