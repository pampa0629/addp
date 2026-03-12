package service

import (
	"fmt"
	"log"
	"time"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
)

// DevTaskService 开发项业务逻辑层
type DevTaskService struct {
	devTaskRepo *repository.DevTaskRepository
}

// NewDevTaskService 创建开发项服务
func NewDevTaskService(devTaskRepo *repository.DevTaskRepository) *DevTaskService {
	return &DevTaskService{
		devTaskRepo: devTaskRepo,
	}
}

// CreateDevItem 创建开发项
func (s *DevTaskService) CreateDevItem(req *models.CreateDevTaskRequest, tenantID uint, userID uint) (*models.DevTask, error) {
	// 业务验证：检查名称是否已存在
	exists, err := s.devTaskRepo.ExistsByName(req.Name, tenantID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to check name existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("开发项名称 '%s' 已存在", req.Name)
	}

	// 业务验证：验证 dev_type
	validTypes := []string{"query", "workflow", "script", "notebook"}
	isValidType := false
	for _, t := range validTypes {
		if req.DevType == t {
			isValidType = true
			break
		}
	}
	if !isValidType {
		return nil, fmt.Errorf("无效的 dev_type: %s", req.DevType)
	}

	// 业务验证：如果是 query 类型，必须在 content 中提供 query_type
	if req.DevType == "query" {
		if req.Content["query_type"] == nil || req.Content["query_type"] == "" {
			return nil, fmt.Errorf("query 类型必须在 content 中提供 query_type")
		}
	}

	// 业务验证：content 不能为空
	if req.Content == nil || len(req.Content) == 0 {
		return nil, fmt.Errorf("content 不能为空")
	}

	// 设置默认值
	if req.Timeout <= 0 {
		req.Timeout = 300 // 默认5分钟
	}

	// 创建开发项
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
		return nil, fmt.Errorf("failed to create dev item: %w", err)
	}

	log.Printf("✅ [DevTaskService] 创建开发项成功 id=%d name=%s type=%s", item.ID, item.Name, item.DevType)
	return item, nil
}

// UpdateDevItem 更新开发项
func (s *DevTaskService) UpdateDevItem(id uint, req *models.UpdateDevTaskRequest, tenantID uint, userID uint) (*models.DevTask, error) {
	// 获取现有开发项
	item, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("开发项不存在")
	}

	// 业务验证：检查名称是否重复
	if req.Name != "" && req.Name != item.Name {
		exists, err := s.devTaskRepo.ExistsByName(req.Name, tenantID, &id)
		if err != nil {
			return nil, fmt.Errorf("failed to check name existence: %w", err)
		}
		if exists {
			return nil, fmt.Errorf("开发项名称 '%s' 已存在", req.Name)
		}
		item.Name = req.Name
	}

	// 更新字段
	if req.DisplayName != "" {
		item.DisplayName = req.DisplayName
	}
	if req.Content != nil && len(req.Content) > 0 {
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
		return nil, fmt.Errorf("failed to update dev item: %w", err)
	}

	log.Printf("✅ [DevTaskService] 更新开发项成功 id=%d name=%s", item.ID, item.Name)
	return item, nil
}

// GetDevItem 获取开发项详情
func (s *DevTaskService) GetDevItem(id uint, tenantID uint) (*models.DevTask, error) {
	item, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("开发项不存在")
	}
	return item, nil
}

// ListDevItems 查询开发项列表
func (s *DevTaskService) ListDevItems(req *models.ListDevTasksRequest, tenantID uint) ([]models.DevTask, int64, error) {
	// 设置默认分页
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	items, total, err := s.devTaskRepo.List(req, tenantID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list dev items: %w", err)
	}

	return items, total, nil
}

// DeleteDevItem 删除开发项（软删除）
func (s *DevTaskService) DeleteDevItem(id uint, tenantID uint) error {
	// 验证开发项是否存在
	_, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		return fmt.Errorf("开发项不存在")
	}

	if err := s.devTaskRepo.Delete(id, tenantID); err != nil {
		return fmt.Errorf("failed to delete dev item: %w", err)
	}

	log.Printf("✅ [DevTaskService] 删除开发项成功 id=%d", id)
	return nil
}

// UpdateLastExecution 更新最后执行信息
func (s *DevTaskService) UpdateLastExecution(id uint, tenantID uint, executionID string, status string, executedAt time.Time) error {
	if err := s.devTaskRepo.UpdateLastExecution(id, tenantID, executionID, status, executedAt); err != nil {
		return fmt.Errorf("failed to update last execution: %w", err)
	}
	return nil
}

// FindScheduledItems 查找所有启用了调度的开发项
func (s *DevTaskService) FindScheduledItems(tenantID uint) ([]models.DevTask, error) {
	items, err := s.devTaskRepo.FindScheduledItems(tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to find scheduled items: %w", err)
	}
	return items, nil
}

// UpdateStatus 更新开发项状态
func (s *DevTaskService) UpdateStatus(id uint, tenantID uint, status string) error {
	// 验证状态值
	if status != "active" && status != "inactive" && status != "archived" {
		return fmt.Errorf("无效的状态: %s", status)
	}

	// 验证开发项是否存在
	_, err := s.devTaskRepo.FindByID(id, tenantID)
	if err != nil {
		return fmt.Errorf("开发项不存在")
	}

	if err := s.devTaskRepo.UpdateStatus(id, tenantID, status); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	log.Printf("✅ [DevTaskService] 更新状态成功 id=%d status=%s", id, status)
	return nil
}

// CountByType 统计各类型的开发项数量
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
