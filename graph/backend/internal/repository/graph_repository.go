package repository

import (
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
)

type KnowledgeGraphRepository struct {
	db *gorm.DB
}

func NewKnowledgeGraphRepository(db *gorm.DB) *KnowledgeGraphRepository {
	return &KnowledgeGraphRepository{db: db}
}

func (r *KnowledgeGraphRepository) List(tenantID uint) ([]models.KnowledgeGraph, error) {
	var graphs []models.KnowledgeGraph
	err := r.db.Where("tenant_id = ?", tenantID).
		Preload("Ontology").
		Order("created_at DESC").Find(&graphs).Error
	return graphs, err
}

func (r *KnowledgeGraphRepository) GetByID(id, tenantID uint) (*models.KnowledgeGraph, error) {
	var kg models.KnowledgeGraph
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		Preload("Ontology").First(&kg).Error
	return &kg, err
}

// GetByIDGlobal 不限租户查询（用于 is_public 图谱的跨租户访问）
func (r *KnowledgeGraphRepository) GetByIDGlobal(id uint) (*models.KnowledgeGraph, error) {
	var kg models.KnowledgeGraph
	err := r.db.Where("id = ?", id).Preload("Ontology").First(&kg).Error
	return &kg, err
}

func (r *KnowledgeGraphRepository) Create(kg *models.KnowledgeGraph) error {
	return r.db.Create(kg).Error
}

func (r *KnowledgeGraphRepository) Update(kg *models.KnowledgeGraph) error {
	return r.db.Save(kg).Error
}

func (r *KnowledgeGraphRepository) Delete(id, tenantID uint) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.KnowledgeGraph{}).Error
}

type OntologyVersionRepository struct {
	db *gorm.DB
}

func NewOntologyVersionRepository(db *gorm.DB) *OntologyVersionRepository {
	return &OntologyVersionRepository{db: db}
}

func (r *OntologyVersionRepository) ListByOntology(ontologyID, tenantID uint) ([]models.OntologyVersion, error) {
	var versions []models.OntologyVersion
	err := r.db.Where("ontology_id = ? AND tenant_id = ?", ontologyID, tenantID).
		Order("created_at DESC").Find(&versions).Error
	return versions, err
}

func (r *OntologyVersionRepository) Create(v *models.OntologyVersion) error {
	return r.db.Create(v).Error
}
