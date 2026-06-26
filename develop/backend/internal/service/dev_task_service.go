package service

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
)

// DevTaskService 开发任务业务逻辑层
type DevTaskService struct {
	devTaskRepo *repository.DevTaskRepository
}

// NewDevTaskService 创建开发任务服务
func NewDevTaskService(devTaskRepo *repository.DevTaskRepository) *DevTaskService {
	return &DevTaskService{
		devTaskRepo: devTaskRepo,
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
	item := &models.DevTask{
		TenantID:        tenantID,
		Name:            req.Name,
		DisplayName:     req.DisplayName,
		DevType:         req.DevType,
		Content:         req.Content,
		ExecutionConfig: req.ExecutionConfig,
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
		query, ok := content["query"].(string)
		if !ok || strings.TrimSpace(query) == "" {
			return fmt.Errorf("query 类型必须在 content.query 中提供查询内容")
		}
		queryType, ok := content["query_type"].(string)
		if !ok || strings.TrimSpace(queryType) == "" {
			return fmt.Errorf("query 类型必须在 content.query_type 中提供查询类型")
		}
	case commonExecution.TaskTypeWorkflow:
		workflowDef, ok := content["workflow_definition"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("workflow 类型必须在 content.workflow_definition 中提供工作流定义")
		}
		if err := validateWorkflowDefinition(workflowDef); err != nil {
			return err
		}
	}

	return nil
}

func validateWorkflowDefinition(workflowDef map[string]interface{}) error {
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
	if devType != commonExecution.TaskTypeQuery {
		return nil
	}
	if content == nil {
		return fmt.Errorf("query 类型必须提供 content")
	}
	queryType, _ := content["query_type"].(string)
	if strings.TrimSpace(queryType) != "sql" {
		return nil
	}
	if executionConfig == nil {
		return fmt.Errorf("SQL 查询任务必须提供 execution_config")
	}

	queryMode, _ := executionConfig["query_mode"].(string)
	queryMode = strings.ToLower(strings.TrimSpace(queryMode))
	_, hasEngineID := executionConfig["engine_id"]

	if queryMode == "duckdb" {
		if hasEngineID {
			return fmt.Errorf("DuckDB 联邦查询任务不得提供 execution_config.engine_id")
		}
		return nil
	}
	if queryMode != "" {
		return fmt.Errorf("不支持的查询执行模式: %s", queryMode)
	}

	engineID := devTaskExecutionConfigEngineID(executionConfig)
	if engineID == nil {
		return fmt.Errorf("普通 SQL 查询任务必须提供 execution_config.engine_id")
	}
	return nil
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
