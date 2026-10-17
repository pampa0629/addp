package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/datatypes"
)

type KnowledgeGraphService struct {
	graphRepo    *repository.KnowledgeGraphRepository
	graphDataSvc *GraphDataService
}

func NewKnowledgeGraphService(graphRepo *repository.KnowledgeGraphRepository, graphDataSvc *GraphDataService) *KnowledgeGraphService {
	return &KnowledgeGraphService{graphRepo: graphRepo, graphDataSvc: graphDataSvc}
}

func (s *KnowledgeGraphService) List(tenantID uint) ([]models.KnowledgeGraph, error) {
	return s.graphRepo.List(tenantID)
}

func (s *KnowledgeGraphService) GetByID(id, tenantID uint) (*models.KnowledgeGraph, error) {
	return s.graphRepo.GetByID(id, tenantID)
}

func (s *KnowledgeGraphService) Create(ctx context.Context, tenantID uint, req *models.CreateKnowledgeGraphRequest) (*models.KnowledgeGraph, error) {
	if req.IndexManagement != models.GraphIndexesManaged && req.IndexManagement != models.GraphIndexesExternal {
		return nil, ErrGraphTargetInvalid
	}
	if _, err := s.graphDataSvc.ontologyRepo.GetByID(req.OntologyID, tenantID); err != nil {
		return nil, err
	}
	target, err := s.graphDataSvc.resolveTarget(ctx, tenantID, req.EngineID, req.CatalogPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGraphTargetInvalid, err)
	}
	defer target.close()
	if _, err = target.query(ctx, "RETURN 1 AS available", nil); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGraphTargetInvalid, err)
	}
	kg := &models.KnowledgeGraph{
		TenantID:        tenantID,
		OntologyID:      req.OntologyID,
		EngineID:        req.EngineID,
		CatalogPath:     req.CatalogPath,
		IndexManagement: req.IndexManagement,
		Name:            req.Name,
		Description:     req.Description,
		Status:          "active",
		Stats:           datatypes.JSON([]byte("{}")),
	}
	if err := s.graphRepo.Create(kg); err != nil {
		return nil, fmt.Errorf("failed to create knowledge graph: %w", err)
	}
	return kg, nil
}

func (s *KnowledgeGraphService) Update(id, tenantID uint, req *models.UpdateKnowledgeGraphRequest) (*models.KnowledgeGraph, error) {
	kg, err := s.graphRepo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	if req.Name != "" {
		kg.Name = req.Name
	}
	if req.Description != "" {
		kg.Description = req.Description
	}
	if req.Status != "" {
		kg.Status = req.Status
	}
	return kg, s.graphRepo.Update(kg)
}

func (s *KnowledgeGraphService) Delete(id, tenantID uint) error {
	return s.graphRepo.Delete(id, tenantID)
}

// UpdateStats 更新图谱统计信息
func (s *KnowledgeGraphService) UpdateStats(id, tenantID uint, stats map[string]interface{}) error {
	kg, err := s.graphRepo.GetByID(id, tenantID)
	if err != nil {
		return err
	}
	statsJSON, _ := json.Marshal(stats)
	kg.Stats = datatypes.JSON(statsJSON)
	return s.graphRepo.Update(kg)
}

var ErrGraphTargetInvalid = fmt.Errorf("invalid graph target")
