package service

import (
	"context"
	"fmt"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
)

type CheckTaskService struct {
	repo         *repository.CheckTaskRepository
	systemClient *commonClient.SystemServiceClient
}

func NewCheckTaskService(repo *repository.CheckTaskRepository, systemClient *commonClient.SystemServiceClient) *CheckTaskService {
	return &CheckTaskService{repo: repo, systemClient: systemClient}
}

func (s *CheckTaskService) List(tenantID int64, page, pageSize int) ([]models.CheckTask, int64, error) {
	return s.repo.List(tenantID, page, pageSize)
}

func (s *CheckTaskService) Get(id, tenantID int64) (*models.CheckTask, error) {
	return s.repo.Get(id, tenantID)
}

func (s *CheckTaskService) Create(ctx context.Context, tenantID, userID int64, req *CreateCheckTaskRequest) (*models.CheckTask, error) {
	if err := requirePostgreSQLEngine(ctx, s.systemClient, tenantID, req.EngineID); err != nil {
		return nil, err
	}
	req.Name = strings.TrimSpace(req.Name)
	req.SchemaName = strings.TrimSpace(req.SchemaName)
	req.TableName = strings.TrimSpace(req.TableName)
	if req.Name == "" || req.SchemaName == "" || req.TableName == "" {
		return nil, fmt.Errorf("%w: name, schema_name and table_name are required", commonAPI.ErrBadRequest)
	}
	if _, err := requirePostgreSQLCatalogTable(ctx, s.systemClient, tenantID, req.EngineID, req.SchemaName, req.TableName); err != nil {
		return nil, err
	}
	task := &models.CheckTask{
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		EngineID:    req.EngineID,
		SchemaName:  req.SchemaName,
		Table:       req.TableName,
		CreatedBy:   userID,
	}
	if err := s.repo.Create(task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *CheckTaskService) Update(ctx context.Context, id, tenantID, userID int64, req *UpdateCheckTaskRequest) (*models.CheckTask, error) {
	task, err := s.repo.Get(id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := requirePostgreSQLEngine(ctx, s.systemClient, tenantID, req.EngineID); err != nil {
		return nil, err
	}
	task.Name = strings.TrimSpace(req.Name)
	task.Description = req.Description
	task.EngineID = req.EngineID
	task.SchemaName = strings.TrimSpace(req.SchemaName)
	task.Table = strings.TrimSpace(req.TableName)
	task.UpdatedBy = &userID
	if task.Name == "" || task.SchemaName == "" || task.Table == "" {
		return nil, fmt.Errorf("%w: name, schema_name and table_name are required", commonAPI.ErrBadRequest)
	}
	if _, err := requirePostgreSQLCatalogTable(ctx, s.systemClient, tenantID, req.EngineID, task.SchemaName, task.Table); err != nil {
		return nil, err
	}
	if err := s.repo.Replace(ctx, task); err != nil {
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
	SchemaName  string `json:"schema_name" binding:"required"`
	TableName   string `json:"table_name" binding:"required"`
}

type UpdateCheckTaskRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	EngineID    int64  `json:"engine_id" binding:"required"`
	SchemaName  string `json:"schema_name" binding:"required"`
	TableName   string `json:"table_name" binding:"required"`
}
