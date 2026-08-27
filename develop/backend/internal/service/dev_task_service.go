package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	engineselection "github.com/addp/common/engine/selection"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"gorm.io/gorm"
)

var (
	ErrNotebookNotFound          = errors.New("notebook task not found")
	ErrTaskNotNotebook           = errors.New("task is not a notebook")
	ErrDevTaskNotFound           = errors.New("development task not found")
	ErrTaskNotWorkflow           = errors.New("task is not a workflow")
	ErrStorageBindingNotFound    = errors.New("storage engine binding not found")
	ErrStorageBindingConflict    = errors.New("storage engine binding changed concurrently")
	ErrStorageEngineUnavailable  = errors.New("storage engine is unavailable")
	ErrStorageEngineIncompatible = errors.New("storage engine is incompatible with resource locators")
	ErrStorageEngineDiscovery    = errors.New("storage engine discovery is unavailable")
)

// DevTaskService 开发任务业务逻辑层
type DevTaskService struct {
	devTaskRepo  *repository.DevTaskRepository
	systemClient *commonClient.SystemServiceClient
}

// NewDevTaskService 创建开发任务服务
func NewDevTaskService(devTaskRepo *repository.DevTaskRepository, systemClient *commonClient.SystemServiceClient) *DevTaskService {
	return &DevTaskService{
		devTaskRepo:  devTaskRepo,
		systemClient: systemClient,
	}
}

// CreateDevTask 创建开发任务
func (s *DevTaskService) CreateDevTask(req *models.CreateDevTaskRequest, tenantID uint, userID uint) (*models.DevTask, error) {
	// 业务验证：检查名称是否已存在
	exists, err := s.devTaskRepo.ExistsByName(req.Name, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to check name existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("开发任务名称 '%s' 已存在", req.Name)
	}

	if !isDevelopDevType(req.DevType) {
		return nil, fmt.Errorf("无效的 dev_type: %s", req.DevType)
	}

	if err := validateDevTaskContent(req.DevType, req.Content); err != nil {
		return nil, err
	}
	if err := validateDevTaskExecutionConfig(req.DevType, req.Content, req.ExecutionConfig); err != nil {
		return nil, err
	}

	// 设置默认值
	if req.Timeout <= 0 {
		req.Timeout = 300 // 默认5分钟
	}

	// 创建开发任务
	editorLayout := req.EditorLayout
	if editorLayout == nil {
		editorLayout = commonModels.JSONMap{}
	}
	item := &models.DevTask{
		TenantID:        tenantID,
		Name:            req.Name,
		DisplayName:     req.DisplayName,
		DevType:         req.DevType,
		Content:         req.Content,
		ExecutionConfig: req.ExecutionConfig,
		EditorLayout:    editorLayout,
		Timeout:         req.Timeout,
		Description:     req.Description,
		Tags:            req.Tags,
		CreatedBy:       &userID,
		Status:          "active",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := s.devTaskRepo.Create(item); err != nil {
		return nil, fmt.Errorf("failed to create dev task: %w", err)
	}

	log.Printf("✅ [DevTaskService] 创建开发任务成功 id=%d name=%s type=%s", item.ID, item.Name, item.DevType)
	return item, nil
}

// UpdateDevTask 更新开发任务
func (s *DevTaskService) UpdateDevTask(id uint, req *models.UpdateDevTaskRequest, tenantID uint, userID uint) (*models.DevTask, error) {
	// 获取现有开发任务
	item, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("开发任务不存在")
	}

	// 业务验证：检查名称是否重复
	if req.Name != "" && req.Name != item.Name {
		exists, err := s.devTaskRepo.ExistsByName(req.Name, tenantID, &id)
		if err != nil {
			return nil, fmt.Errorf("failed to check name existence: %w", err)
		}
		if exists {
			return nil, fmt.Errorf("开发任务名称 '%s' 已存在", req.Name)
		}
		item.Name = req.Name
	}

	// 更新字段
	if req.DisplayName != "" {
		item.DisplayName = req.DisplayName
	}

	nextContent := map[string]interface{}(item.Content)
	if req.Content != nil && len(req.Content) > 0 {
		nextContent = req.Content
	}
	nextExecutionConfig := map[string]interface{}(item.ExecutionConfig)
	if req.ExecutionConfig != nil {
		nextExecutionConfig = req.ExecutionConfig
	}

	if req.Content != nil && len(req.Content) > 0 {
		if err := validateDevTaskContent(item.DevType, req.Content); err != nil {
			return nil, err
		}
	}
	if err := validateDevTaskExecutionConfig(item.DevType, nextContent, nextExecutionConfig); err != nil {
		return nil, err
	}
	if req.Content != nil && len(req.Content) > 0 {
		item.Content = req.Content
	}
	if req.ExecutionConfig != nil {
		item.ExecutionConfig = req.ExecutionConfig
	}
	if req.EditorLayout != nil {
		item.EditorLayout = req.EditorLayout
	}
	if req.Timeout > 0 {
		item.Timeout = req.Timeout
	}
	if req.Description != "" {
		item.Description = req.Description
	}
	if req.Tags != nil {
		item.Tags = req.Tags
	}
	if req.Status != "" {
		item.Status = req.Status
	}

	item.UpdatedBy = &userID
	item.UpdatedAt = time.Now()

	if err := s.devTaskRepo.Update(item); err != nil {
		return nil, fmt.Errorf("failed to update dev task: %w", err)
	}

	log.Printf("✅ [DevTaskService] 更新开发任务成功 id=%d name=%s", item.ID, item.Name)
	return item, nil
}

// RebindNotebookRuntime 更新原 Notebook 任务的引擎和 Kernel，仅影响后续执行。
func (s *DevTaskService) RebindNotebookRuntime(
	id uint,
	tenantID uint,
	userID uint,
	engineID uint,
	kernel string,
) (*models.DevTask, error) {
	item, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		return nil, ErrNotebookNotFound
	}
	if !item.IsNotebookScript() {
		return nil, ErrTaskNotNotebook
	}
	if engineID == 0 {
		return nil, fmt.Errorf("engine_id must be a positive integer")
	}
	kernel = strings.TrimSpace(kernel)
	if kernel == "" {
		return nil, fmt.Errorf("kernel must not be empty")
	}

	content := cloneDevTaskContent(item.Content)
	content["kernel"] = kernel
	executionConfig := cloneDevTaskContent(item.ExecutionConfig)
	executionConfig["engine_id"] = engineID
	item.Content = content
	item.ExecutionConfig = executionConfig

	if err := s.devTaskRepo.UpdateNotebookRuntimeBinding(item, userID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotebookNotFound
		}
		return nil, fmt.Errorf("update notebook runtime binding: %w", err)
	}
	return item, nil
}

// ListWorkflowStorageEngineBindings 返回工作流当前 Locator 绑定和可选存储 Engine。
func (s *DevTaskService) ListWorkflowStorageEngineBindings(
	ctx context.Context,
	id uint,
	tenantID uint,
) (*models.WorkflowStorageEngineBindingsResponse, error) {
	item, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDevTaskNotFound
		}
		return nil, fmt.Errorf("load workflow task: %w", err)
	}
	if item.DevType != commonExecution.TaskTypeWorkflow {
		return nil, ErrTaskNotWorkflow
	}

	bindings := collectWorkflowStorageEngineBindings(item.Content)
	candidates, err := s.listStorageEngineDescriptors(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	descriptorsByID := make(map[uint]models.WorkflowStorageEngineCandidate, len(candidates))
	responseCandidates := make([]models.WorkflowStorageEngineCandidate, 0, len(candidates))
	for index := range candidates {
		candidate := workflowStorageEngineCandidate(candidates[index])
		descriptorsByID[candidate.ID] = candidate
		responseCandidates = append(responseCandidates, candidate)
	}

	items := make([]models.WorkflowStorageEngineBinding, 0, len(bindings))
	for _, binding := range bindings {
		entry := models.WorkflowStorageEngineBinding{
			EngineID:       binding.engineID,
			ReferenceCount: binding.referenceCount,
			ResourceTypes:  binding.resourceTypes,
		}
		if descriptor, ok := descriptorsByID[binding.engineID]; ok {
			descriptorCopy := descriptor
			entry.Available = descriptor.LifecycleState == commonModels.EngineLifecycleActive &&
				descriptor.ConnectionStatus == commonModels.EngineConnectionOnline
			entry.Engine = &descriptorCopy
		}
		for index := range candidates {
			if storageEngineSupportsResourceTypes(&candidates[index], binding.resourceTypes) {
				entry.CompatibleEngineIDs = append(entry.CompatibleEngineIDs, candidates[index].ID)
			}
		}
		items = append(items, entry)
	}

	return &models.WorkflowStorageEngineBindingsResponse{
		Items:            items,
		CandidateEngines: responseCandidates,
	}, nil
}

func workflowStorageEngineCandidate(descriptor commonModels.EngineRuntimeDescriptor) models.WorkflowStorageEngineCandidate {
	return models.WorkflowStorageEngineCandidate{
		ID:               descriptor.ID,
		Name:             descriptor.Name,
		EngineType:       descriptor.EngineType,
		LifecycleState:   descriptor.LifecycleState,
		ConnectionStatus: descriptor.ConnectionStatus,
	}
}

// RebindWorkflowStorageEngine 原子替换工作流内容中指向 sourceEngineID 的全部标准 Locator。
func (s *DevTaskService) RebindWorkflowStorageEngine(
	ctx context.Context,
	id uint,
	tenantID uint,
	userID uint,
	sourceEngineID uint,
	targetEngineID uint,
) (*models.RebindWorkflowStorageEngineResponse, error) {
	item, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDevTaskNotFound
		}
		return nil, fmt.Errorf("load workflow task: %w", err)
	}
	if item.DevType != commonExecution.TaskTypeWorkflow {
		return nil, ErrTaskNotWorkflow
	}
	if sourceEngineID == 0 || targetEngineID == 0 || sourceEngineID == targetEngineID {
		return nil, ErrStorageEngineUnavailable
	}

	bindings := collectWorkflowStorageEngineBindings(item.Content)
	var sourceBinding *workflowStorageEngineBindingFacts
	for index := range bindings {
		if bindings[index].engineID == sourceEngineID {
			sourceBinding = &bindings[index]
			break
		}
	}
	if sourceBinding == nil {
		return nil, ErrStorageBindingNotFound
	}

	candidates, err := s.listStorageEngineDescriptors(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	var target *commonModels.EngineRuntimeDescriptor
	for index := range candidates {
		if candidates[index].ID == targetEngineID {
			target = &candidates[index]
			break
		}
	}
	if target == nil {
		return nil, ErrStorageEngineUnavailable
	}
	if !engineselection.IsAvailableStorageEngine(target.AsEngine()) {
		return nil, ErrStorageEngineUnavailable
	}
	if !storageEngineSupportsResourceTypes(target, sourceBinding.resourceTypes) {
		return nil, ErrStorageEngineIncompatible
	}

	content, err := cloneWorkflowContent(item.Content)
	if err != nil {
		return nil, fmt.Errorf("clone workflow content: %w", err)
	}
	rewritten, replaced := rewriteWorkflowStorageEngineLocators(content, sourceEngineID, targetEngineID)
	if replaced == 0 {
		return nil, ErrStorageBindingNotFound
	}
	item.Content = models.DevTaskContent(rewritten.(map[string]interface{}))
	expectedUpdatedAt := item.UpdatedAt
	if err := s.devTaskRepo.UpdateWorkflowStorageEngineBindings(item, userID, expectedUpdatedAt); err != nil {
		if errors.Is(err, repository.ErrDevTaskConcurrentUpdate) {
			return nil, ErrStorageBindingConflict
		}
		return nil, fmt.Errorf("update workflow storage engine binding: %w", err)
	}

	return &models.RebindWorkflowStorageEngineResponse{
		Task:                 *item,
		SourceEngineID:       sourceEngineID,
		TargetEngineID:       targetEngineID,
		ReplacedLocatorCount: replaced,
	}, nil
}

type workflowStorageEngineBindingFacts struct {
	engineID       uint
	referenceCount int
	resourceTypes  []string
}

func collectWorkflowStorageEngineBindings(content models.DevTaskContent) []workflowStorageEngineBindingFacts {
	counts := make(map[uint]int)
	typesByEngine := make(map[uint]map[string]struct{})
	walkWorkflowStorageLocators(content, func(locator *resourcetree.ResourceLocator) {
		if locator.EngineID == 0 {
			return
		}
		counts[locator.EngineID]++
		if typesByEngine[locator.EngineID] == nil {
			typesByEngine[locator.EngineID] = make(map[string]struct{})
		}
		typesByEngine[locator.EngineID][string(locator.Type)] = struct{}{}
	})

	engineIDs := make([]uint, 0, len(counts))
	for engineID := range counts {
		engineIDs = append(engineIDs, engineID)
	}
	sort.Slice(engineIDs, func(i, j int) bool { return engineIDs[i] < engineIDs[j] })

	bindings := make([]workflowStorageEngineBindingFacts, 0, len(engineIDs))
	for _, engineID := range engineIDs {
		resourceTypes := make([]string, 0, len(typesByEngine[engineID]))
		for resourceType := range typesByEngine[engineID] {
			resourceTypes = append(resourceTypes, resourceType)
		}
		sort.Strings(resourceTypes)
		bindings = append(bindings, workflowStorageEngineBindingFacts{
			engineID:       engineID,
			referenceCount: counts[engineID],
			resourceTypes:  resourceTypes,
		})
	}
	return bindings
}

func walkWorkflowStorageLocators(value interface{}, visit func(*resourcetree.ResourceLocator)) {
	switch typed := value.(type) {
	case models.DevTaskContent:
		walkWorkflowStorageLocators(map[string]interface{}(typed), visit)
	case map[string]interface{}:
		for _, child := range typed {
			walkWorkflowStorageLocators(child, visit)
		}
	case []interface{}:
		for _, child := range typed {
			walkWorkflowStorageLocators(child, visit)
		}
	case string:
		locator, err := resourcetree.ParseURI(strings.TrimSpace(typed))
		if err == nil {
			visit(locator)
		}
	}
}

func (s *DevTaskService) listStorageEngineDescriptors(ctx context.Context, tenantID uint) ([]commonModels.EngineRuntimeDescriptor, error) {
	if s.systemClient == nil {
		return nil, fmt.Errorf("%w: system engine client is not configured", ErrStorageEngineDiscovery)
	}
	descriptors, err := s.systemClient.WithTenantID(tenantID).ListEngineRuntimeDescriptors(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageEngineDiscovery, err)
	}
	candidates := make([]commonModels.EngineRuntimeDescriptor, 0, len(descriptors))
	for index := range descriptors {
		descriptor := &descriptors[index]
		if !engineselection.IsStorageSelectionOption(descriptor.AsEngine()) {
			continue
		}
		candidates = append(candidates, *descriptor)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Name == candidates[j].Name {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates, nil
}

func storageEngineSupportsResourceTypes(engine *commonModels.EngineRuntimeDescriptor, resourceTypes []string) bool {
	if engine == nil || !engineselection.IsStorageSelectionOption(engine.AsEngine()) {
		return false
	}
	capabilities, err := engineselection.ParseCapabilities(engine.Capabilities)
	if err != nil || capabilities == nil || capabilities.Storage == nil || capabilities.Storage.CatalogModel == nil {
		return false
	}
	catalogModel := capabilities.Storage.CatalogModel
	for _, resourceType := range resourceTypes {
		supported := catalogModel.RootTerm == resourceType
		for _, level := range catalogModel.Levels {
			if level.Term == resourceType {
				supported = true
				break
			}
			for _, kind := range level.Kinds {
				if kind == resourceType {
					supported = true
					break
				}
			}
			if supported {
				break
			}
		}
		if !supported {
			return false
		}
	}
	return len(resourceTypes) > 0
}

func cloneWorkflowContent(content models.DevTaskContent) (map[string]interface{}, error) {
	raw, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	var cloned map[string]interface{}
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func rewriteWorkflowStorageEngineLocators(value interface{}, sourceEngineID, targetEngineID uint) (interface{}, int) {
	switch typed := value.(type) {
	case map[string]interface{}:
		total := 0
		for key, child := range typed {
			rewritten, count := rewriteWorkflowStorageEngineLocators(child, sourceEngineID, targetEngineID)
			typed[key] = rewritten
			total += count
		}
		return typed, total
	case []interface{}:
		total := 0
		for index, child := range typed {
			rewritten, count := rewriteWorkflowStorageEngineLocators(child, sourceEngineID, targetEngineID)
			typed[index] = rewritten
			total += count
		}
		return typed, total
	case string:
		locator, err := resourcetree.ParseURI(strings.TrimSpace(typed))
		if err != nil || locator.EngineID != sourceEngineID {
			return typed, 0
		}
		locator.EngineID = targetEngineID
		locator.NodeID = nil
		locator.ItemID = nil
		return locator.ToURI(), 1
	default:
		return value, 0
	}
}

func cloneDevTaskContent(source models.DevTaskContent) models.DevTaskContent {
	cloned := make(models.DevTaskContent, len(source)+1)
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func isDevelopDevType(devType string) bool {
	switch devType {
	case commonExecution.TaskTypeQuery, commonExecution.TaskTypeWorkflow, commonExecution.TaskTypeScript:
		return true
	default:
		return false
	}
}

func validateDevTaskContent(devType string, content map[string]interface{}) error {
	if content == nil || len(content) == 0 {
		return fmt.Errorf("content 不能为空")
	}

	switch devType {
	case commonExecution.TaskTypeQuery:
		if err := validateAllowedFields(content, "content", map[string]struct{}{
			"query": {}, "query_type": {}, "query_parameters": {}, "relation_inputs": {}, "target_locator": {},
		}); err != nil {
			return err
		}
		query, ok := content["query"].(string)
		if !ok || strings.TrimSpace(query) == "" {
			return fmt.Errorf("query 类型必须在 content.query 中提供查询内容")
		}
		queryType, ok := content["query_type"].(string)
		if !ok || strings.TrimSpace(queryType) == "" {
			return fmt.Errorf("query 类型必须在 content.query_type 中提供查询类型")
		}
		switch strings.ToLower(strings.TrimSpace(queryType)) {
		case "sql", "mql", "cypher":
		default:
			return fmt.Errorf("不支持的查询类型: %s", queryType)
		}
		if locator, ok := content["target_locator"].(string); ok && strings.TrimSpace(locator) != "" {
			if _, err := resourcetree.ParseURI(locator); err != nil {
				return fmt.Errorf("content.target_locator 无效: %v", err)
			}
		}
		if _, err := BuildQueryExecutionContract(content); err != nil {
			return err
		}
	case commonExecution.TaskTypeWorkflow:
		workflowDef, ok := content["workflow_definition"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("workflow 类型必须在 content.workflow_definition 中提供工作流定义")
		}
		if err := ValidateWorkflowDefinition(workflowDef); err != nil {
			return err
		}
	}

	return nil
}

// ValidateWorkflowDefinition 校验 addp.workflow/v1 的基础 DAG 结构。
// 具体工作流引擎的算子和公开参数由 OperatorDiscoveryService.ValidateWorkflow 校验。
func ValidateWorkflowDefinition(workflowDef map[string]interface{}) error {
	tasksValue, ok := workflowDef["tasks"]
	if !ok {
		return fmt.Errorf("workflow 类型必须在 content.workflow_definition.tasks 中提供任务数组")
	}

	tasks, ok := workflowTasksFromInterface(tasksValue)
	if !ok || len(tasks) == 0 {
		return fmt.Errorf("content.workflow_definition.tasks 必须是非空数组")
	}

	taskIDs := make(map[string]struct{}, len(tasks))
	dependencies := make(map[string][]string, len(tasks))
	for i, task := range tasks {
		taskID, ok := task["id"].(string)
		if !ok || strings.TrimSpace(taskID) == "" {
			return fmt.Errorf("content.workflow_definition.tasks[%d].id 必须是非空字符串", i)
		}
		if _, exists := taskIDs[taskID]; exists {
			return fmt.Errorf("content.workflow_definition.tasks[%d].id 重复: %s", i, taskID)
		}
		taskIDs[taskID] = struct{}{}

		operator, ok := task["operator"].(string)
		if !ok || strings.TrimSpace(operator) == "" {
			return fmt.Errorf("content.workflow_definition.tasks[%d].operator 必须是非空字符串", i)
		}

		if _, ok := task["params"]; !ok {
			return fmt.Errorf("content.workflow_definition.tasks[%d].params 必须显式提供", i)
		}
		if _, ok := task["params"].(map[string]interface{}); !ok {
			return fmt.Errorf("content.workflow_definition.tasks[%d].params 必须是对象", i)
		}

		dependsOn, ok := task["depends_on"]
		if !ok {
			return fmt.Errorf("content.workflow_definition.tasks[%d].depends_on 必须显式提供", i)
		}
		depList, ok := stringSliceFromInterface(dependsOn)
		if !ok {
			return fmt.Errorf("content.workflow_definition.tasks[%d].depends_on 必须是字符串数组", i)
		}
		dependencies[taskID] = depList
	}

	for taskID, depList := range dependencies {
		for _, depID := range depList {
			if depID == taskID {
				return fmt.Errorf("content.workflow_definition task %s 不得依赖自身", taskID)
			}
			if _, ok := taskIDs[depID]; !ok {
				return fmt.Errorf("content.workflow_definition task %s 依赖不存在的任务: %s", taskID, depID)
			}
		}
	}

	if hasWorkflowDependencyCycle(dependencies) {
		return fmt.Errorf("content.workflow_definition 存在循环依赖")
	}

	return nil
}

func hasWorkflowDependencyCycle(dependencies map[string][]string) bool {
	const (
		unvisited = iota
		visiting
		visited
	)

	states := make(map[string]int, len(dependencies))
	var visit func(string) bool
	visit = func(taskID string) bool {
		switch states[taskID] {
		case visiting:
			return true
		case visited:
			return false
		}

		states[taskID] = visiting
		for _, depID := range dependencies[taskID] {
			if visit(depID) {
				return true
			}
		}
		states[taskID] = visited
		return false
	}

	for taskID := range dependencies {
		if visit(taskID) {
			return true
		}
	}
	return false
}

func workflowTasksFromInterface(value interface{}) ([]map[string]interface{}, bool) {
	switch tasks := value.(type) {
	case []interface{}:
		result := make([]map[string]interface{}, 0, len(tasks))
		for _, item := range tasks {
			task, ok := item.(map[string]interface{})
			if !ok {
				return nil, false
			}
			result = append(result, task)
		}
		return result, true
	case []map[string]interface{}:
		return tasks, true
	default:
		return nil, false
	}
}

func stringSliceFromInterface(value interface{}) ([]string, bool) {
	switch items := value.(type) {
	case []interface{}:
		result := make([]string, 0, len(items))
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	case []string:
		return items, true
	default:
		return nil, false
	}
}

func validateDevTaskExecutionConfig(devType string, content map[string]interface{}, executionConfig map[string]interface{}) error {
	if devType == commonExecution.TaskTypeScript {
		if devTaskExecutionConfigEngineID(executionConfig) == nil {
			return fmt.Errorf("script 任务必须提供 execution_config.engine_id")
		}
		return nil
	}
	if devType != commonExecution.TaskTypeQuery {
		return nil
	}
	if content == nil {
		return fmt.Errorf("query 类型必须提供 content")
	}
	queryType, _ := content["query_type"].(string)
	queryType = strings.ToLower(strings.TrimSpace(queryType))
	if executionConfig == nil {
		return fmt.Errorf("查询任务必须提供 execution_config")
	}
	if err := validateAllowedFields(executionConfig, "execution_config", map[string]struct{}{"engine_id": {}}); err != nil {
		return err
	}

	engineID := devTaskExecutionConfigEngineID(executionConfig)
	if engineID == nil {
		return fmt.Errorf("查询任务必须提供 execution_config.engine_id")
	}
	if locatorValue, ok := content["target_locator"].(string); ok && strings.TrimSpace(locatorValue) != "" {
		locator, err := resourcetree.ParseURI(strings.TrimSpace(locatorValue))
		if err != nil {
			return fmt.Errorf("content.target_locator 无效: %v", err)
		}
		if locator.EngineID != *engineID {
			return fmt.Errorf("content.target_locator 的引擎 ID 必须与 execution_config.engine_id 一致")
		}
	}
	relationInputs, hasRelationInputs, err := relationInputBindings(content)
	if err != nil {
		return err
	}
	if hasRelationInputs {
		if queryType != "sql" {
			return fmt.Errorf("content.relation_inputs 仅支持 SQL 查询任务")
		}
		queryText, _ := content["query"].(string)
		analysis, analysisErr := AnalyzeQuery("sql", queryText)
		if analysisErr != nil || analysis.Statement != "SELECT" || analysis.Effect != string(SQLExecutionEffectRead) {
			return fmt.Errorf("关系输入写入的 content.query 必须是单条只读 SELECT")
		}
		if err := validateRelationResultSource(queryText, relationInputs); err != nil {
			return err
		}
	}
	return nil
}

func validateAllowedFields(value map[string]interface{}, path string, allowed map[string]struct{}) error {
	unknown := make([]string, 0)
	for field := range value {
		if _, exists := allowed[field]; !exists {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%s 包含未声明字段: %s", path, strings.Join(unknown, ", "))
}

func positiveInt64(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		converted := int64(typed)
		return converted, converted > 0 && float64(converted) == typed
	case int:
		return int64(typed), typed > 0
	case int64:
		return typed, typed > 0
	case uint:
		converted := int64(typed)
		return converted, converted > 0 && uint(converted) == typed
	case uint64:
		converted := int64(typed)
		return converted, converted > 0 && uint64(converted) == typed
	default:
		return 0, false
	}
}

func devTaskExecutionConfigEngineID(executionConfig map[string]interface{}) *uint {
	switch value := executionConfig["engine_id"].(type) {
	case float64:
		if value <= 0 {
			return nil
		}
		id := uint(value)
		return &id
	case int:
		if value <= 0 {
			return nil
		}
		id := uint(value)
		return &id
	case uint:
		if value == 0 {
			return nil
		}
		id := value
		return &id
	case json.Number:
		parsed, err := value.Int64()
		if err != nil || parsed <= 0 {
			return nil
		}
		id := uint(parsed)
		return &id
	default:
		return nil
	}
}

// GetDevTask 获取开发任务详情
func (s *DevTaskService) GetDevTask(id uint, tenantID uint) (*models.DevTask, error) {
	item, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("开发任务不存在")
	}
	return item, nil
}

// ListDevTasks 查询开发任务列表
func (s *DevTaskService) ListDevTasks(req *models.ListDevTasksRequest, tenantID uint) ([]models.DevTask, int64, error) {
	// 设置默认分页
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	items, total, err := s.devTaskRepo.List(req, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list dev tasks: %w", err)
	}

	return items, total, nil
}

// ListNotebookScripts 查询 Notebook 形态的脚本开发任务。
func (s *DevTaskService) ListNotebookScripts(req *models.ListDevTasksRequest, tenantID uint) ([]models.DevTask, int64, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	return s.devTaskRepo.ListNotebookScripts(req, tenantID)
}

// DeleteDevTask 删除开发任务（软删除）
func (s *DevTaskService) DeleteDevTask(id uint, tenantID uint) error {
	// 验证开发任务是否存在
	_, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		return fmt.Errorf("开发任务不存在")
	}

	if err := s.devTaskRepo.Delete(id, tenantID); err != nil {
		return fmt.Errorf("failed to delete dev task: %w", err)
	}

	log.Printf("✅ [DevTaskService] 删除开发任务成功 id=%d", id)
	return nil
}

// UpdateLastExecution 更新最后执行信息
func (s *DevTaskService) UpdateLastExecution(id uint, tenantID uint, executionID string, status string, executedAt time.Time) error {
	if err := s.devTaskRepo.UpdateLastExecution(id, tenantID, executionID, status, executedAt); err != nil {
		return fmt.Errorf("failed to update last execution: %w", err)
	}
	return nil
}

// UpdateStatus 更新开发任务状态
func (s *DevTaskService) UpdateStatus(id uint, tenantID uint, status string) error {
	// 验证状态值
	if status != "active" && status != "inactive" && status != "archived" {
		return fmt.Errorf("无效的状态: %s", status)
	}

	// 验证开发任务是否存在
	_, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		return fmt.Errorf("开发任务不存在")
	}

	if err := s.devTaskRepo.UpdateStatus(id, tenantID, status); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	log.Printf("✅ [DevTaskService] 更新状态成功 id=%d status=%s", id, status)
	return nil
}

// CountByType 统计各类型的开发任务数量
func (s *DevTaskService) CountByType(tenantID uint) (map[string]int64, error) {
	counts, err := s.devTaskRepo.CountByType(tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to count by type: %w", err)
	}
	return counts, nil
}

// BatchUpdateStatus 批量更新状态
func (s *DevTaskService) BatchUpdateStatus(ids []uint, tenantID uint, status string) error {
	// 验证状态值
	if status != "active" && status != "inactive" && status != "archived" {
		return fmt.Errorf("无效的状态: %s", status)
	}

	if len(ids) == 0 {
		return fmt.Errorf("ids 不能为空")
	}

	if err := s.devTaskRepo.BatchUpdateStatus(ids, tenantID, status); err != nil {
		return fmt.Errorf("failed to batch update status: %w", err)
	}

	log.Printf("✅ [DevTaskService] 批量更新状态成功 count=%d status=%s", len(ids), status)
	return nil
}
