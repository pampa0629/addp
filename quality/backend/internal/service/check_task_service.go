package service

import (
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
)

type CheckTaskService struct {
	repo *repository.CheckTaskRepository
}

func NewCheckTaskService(repo *repository.CheckTaskRepository) *CheckTaskService {
	return &CheckTaskService{repo: repo}
}

func (s *CheckTaskService) List(tenantID int64) ([]models.CheckTask, error) {
	return s.repo.List(tenantID)
}

func (s *CheckTaskService) Get(id, tenantID int64) (*models.CheckTask, error) {
	return s.repo.Get(id, tenantID)
}

func (s *CheckTaskService) Create(tenantID, userID int64, req *CreateCheckTaskRequest) (*models.CheckTask, error) {
	task := &models.CheckTask{
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		EngineID:    req.EngineID,
		SchemaName:  req.SchemaName,
		Table:       req.TableName,
		Enabled:     true,
		CreatedBy:   userID,
	}
	if err := s.repo.Create(task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *CheckTaskService) Update(id, tenantID int64, req *UpdateCheckTaskRequest) (*models.CheckTask, error) {
	task, err := s.repo.Get(id, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		task.Name = req.Name
	}
	if req.Description != "" {
		task.Description = req.Description
	}
	if req.Enabled != nil {
		task.Enabled = *req.Enabled
	}
	if err := s.repo.Update(task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *CheckTaskService) Delete(id, tenantID int64) error {
	return s.repo.Delete(id, tenantID)
}

type CreateCheckTaskRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	EngineID    int64  `json:"engine_id" binding:"required"`
	SchemaName  string `json:"schema_name"`
	TableName   string `json:"table_name"`
}

type UpdateCheckTaskRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
}
