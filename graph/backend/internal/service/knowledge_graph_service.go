package service

import (
	"encoding/json"
	"fmt"

	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/datatypes"
)

type KnowledgeGraphService struct {
	graphRepo *repository.KnowledgeGraphRepository
}

func NewKnowledgeGraphService(graphRepo *repository.KnowledgeGraphRepository) *KnowledgeGraphService {
	return &KnowledgeGraphService{graphRepo: graphRepo}
}

func (s *KnowledgeGraphService) List(tenantID uint) ([]models.KnowledgeGraph, error) {
	return s.graphRepo.List(tenantID)
}

func (s *KnowledgeGraphService) GetByID(id, tenantID uint) (*models.KnowledgeGraph, error) {
	return s.graphRepo.GetByID(id, tenantID)
}

func (s *KnowledgeGraphService) Create(tenantID uint, req *models.CreateKnowledgeGraphRequest) (*models.KnowledgeGraph, error) {
	kg := &models.KnowledgeGraph{
		TenantID:    tenantID,
		OntologyID:  req.OntologyID,
		EngineID:    req.EngineID,
		Database:    req.Database,
		Name:        req.Name,
		Description: req.Description,
		Status:      "active",
		Stats:       datatypes.JSON([]byte("{}")),
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
