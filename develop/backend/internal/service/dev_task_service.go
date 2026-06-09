package service

import (
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
		Schedule:        req.Schedule,
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
	if req.Content != nil && len(req.Content) > 0 {
		if err := validateDevTaskContent(item.DevType, req.Content); err != nil {
			return nil, err
		}
		item.Content = req.Content
	}
	if req.ExecutionConfig != nil {
		item.ExecutionConfig = req.ExecutionConfig
	}
	if req.Schedule != "" {
		item.Schedule = req.Schedule
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
		if _, ok := content["workflow_definition"].(map[string]interface{}); !ok {
			return fmt.Errorf("workflow 类型必须在 content.workflow_definition 中提供工作流定义")
		}
	}

	return nil
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

// FindScheduledItems 查找所有启用了调度的开发任务
func (s *DevTaskService) FindScheduledItems(tenantID uint) ([]models.DevTask, error) {
	items, err := s.devTaskRepo.FindScheduledItems(tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to find scheduled items: %w", err)
	}
	return items, nil
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
