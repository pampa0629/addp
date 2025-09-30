package service

import (
	"fmt"
	"time"

	"github.com/addp/manager/internal/connector"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type DataSourceService struct {
	repo         *repository.DataSourceRepository
	systemClient *connector.SystemClient
}

func NewDataSourceService(repo *repository.DataSourceRepository, systemClient *connector.SystemClient) *DataSourceService {
	return &DataSourceService{
		repo:         repo,
		systemClient: systemClient,
	}
}

// SyncFromSystem 从 System 模块同步存储引擎资源
func (s *DataSourceService) SyncFromSystem(token string) error {
	resources, err := s.systemClient.GetResources(token)
	if err != nil {
		return fmt.Errorf("failed to get resources from system: %w", err)
	}

	for _, res := range resources {
		// 检查是否已存在
		existing, err := s.repo.GetBySystemResourceID(res.ID)
		if err == nil {
			// 更新现有记录
			updates := map[string]interface{}{
				"name":            res.Name,
				"resource_type":   res.ResourceType,
				"connection_info": res.ConnectionInfo,
				"description":     res.Description,
				"updated_at":      time.Now(),
			}
			if err := s.repo.Update(existing.ID, updates); err != nil {
				return fmt.Errorf("failed to update datasource %d: %w", existing.ID, err)
			}
		} else {
			// 创建新记录
			ds := &models.DataSource{
				SystemResourceID: res.ID,
				Name:             res.Name,
				ResourceType:     res.ResourceType,
				ConnectionInfo:   res.ConnectionInfo,
				Description:      res.Description,
				Status:           "active",
			}
			if err := s.repo.Create(ds); err != nil {
				return fmt.Errorf("failed to create datasource: %w", err)
			}
		}
	}

	return nil
}

// List 获取数据源列表
func (s *DataSourceService) List(page, pageSize int) ([]models.DataSource, int64, error) {
	dataSources, err := s.repo.List(page, pageSize)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.repo.Count()
	if err != nil {
		return nil, 0, err
	}

	return dataSources, total, nil
}

// GetByID 获取单个数据源
func (s *DataSourceService) GetByID(id uint) (*models.DataSource, error) {
	return s.repo.GetByID(id)
}

// UpdateStatus 更新数据源状态
func (s *DataSourceService) UpdateStatus(id uint, status string, lastChecked time.Time) error {
	updates := map[string]interface{}{
		"status":       status,
		"last_checked": lastChecked,
		"updated_at":   time.Now(),
	}
	return s.repo.Update(id, updates)
}

// Delete 删除数据源
func (s *DataSourceService) Delete(id uint) error {
	return s.repo.Delete(id)
}