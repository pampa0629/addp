package service

import (
	"context"
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
	graphs, err := s.graphRepo.List(tenantID)
	if err != nil {
		return nil, err
	}
	for i := range graphs {
		if err = s.graphRepo.FillAdmissionView(context.Background(), &graphs[i]); err != nil {
			return nil, err
		}
	}
	return graphs, nil
}

func (s *KnowledgeGraphService) GetByID(id, tenantID uint) (*models.KnowledgeGraph, error) {
	kg, err := s.graphRepo.GetByID(id, tenantID)
	if err == nil {
		err = s.graphRepo.FillAdmissionView(context.Background(), kg)
	}
	return kg, err
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

func (s *KnowledgeGraphService) Update(ctx context.Context, id, tenantID uint, req *models.UpdateKnowledgeGraphRequest) (*models.KnowledgeGraph, error) {
	return s.graphRepo.UpdateDefinition(ctx, id, tenantID, req)
}
func (s *KnowledgeGraphService) Delete(ctx context.Context, id, tenantID uint, version int64) error {
	return s.graphRepo.DeleteDefinition(ctx, id, tenantID, version)
}
func (s *KnowledgeGraphService) Reconcile(ctx context.Context, id, tenantID, actor uint, version int64) (*models.KnowledgeGraph, error) {
	kg, err := s.graphRepo.Reconcile(ctx, id, tenantID, actor, version)
	if err == nil {
		err = s.graphRepo.FillAdmissionView(ctx, kg)
	}
	return kg, err
}
func (s *KnowledgeGraphService) Reopen(ctx context.Context, id, tenantID uint, version int64) (*models.KnowledgeGraph, error) {
	return s.graphRepo.Reopen(ctx, id, tenantID, version)
}

var ErrGraphTargetInvalid = fmt.Errorf("invalid graph target")
