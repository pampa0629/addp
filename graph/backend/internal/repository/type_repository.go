package repository

import (
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
)

type EntityTypeRepository struct {
	db *gorm.DB
}

func NewEntityTypeRepository(db *gorm.DB) *EntityTypeRepository {
	return &EntityTypeRepository{db: db}
}

func (r *EntityTypeRepository) ListByOntology(ontologyID, tenantID uint) ([]models.EntityType, error) {
	var types []models.EntityType
	err := r.db.Where("ontology_id = ? AND tenant_id = ?", ontologyID, tenantID).
		Order("sort_order ASC, created_at ASC").Find(&types).Error
	return types, err
}

func (r *EntityTypeRepository) GetByID(id, ontologyID, tenantID uint) (*models.EntityType, error) {
	var et models.EntityType
	err := r.db.Where("id = ? AND ontology_id = ? AND tenant_id = ?", id, ontologyID, tenantID).First(&et).Error
	return &et, err
}

func (r *EntityTypeRepository) Create(et *models.EntityType) error {
	return r.db.Create(et).Error
}

func (r *EntityTypeRepository) Update(et *models.EntityType) error {
	return r.db.Save(et).Error
}

func (r *EntityTypeRepository) Delete(id, ontologyID, tenantID uint) error {
	return r.db.Where("id = ? AND ontology_id = ? AND tenant_id = ?", id, ontologyID, tenantID).
		Delete(&models.EntityType{}).Error
}

type RelationTypeRepository struct {
	db *gorm.DB
}

func NewRelationTypeRepository(db *gorm.DB) *RelationTypeRepository {
	return &RelationTypeRepository{db: db}
}

func (r *RelationTypeRepository) ListByOntology(ontologyID, tenantID uint) ([]models.RelationType, error) {
	var types []models.RelationType
	err := r.db.Where("ontology_id = ? AND tenant_id = ?", ontologyID, tenantID).
		Preload("SourceType").Preload("TargetType").
		Order("sort_order ASC, created_at ASC").Find(&types).Error
	return types, err
}

func (r *RelationTypeRepository) GetByID(id, ontologyID, tenantID uint) (*models.RelationType, error) {
	var rt models.RelationType
	err := r.db.Where("id = ? AND ontology_id = ? AND tenant_id = ?", id, ontologyID, tenantID).
		Preload("SourceType").Preload("TargetType").First(&rt).Error
	return &rt, err
}

func (r *RelationTypeRepository) Create(rt *models.RelationType) error {
	return r.db.Create(rt).Error
}

func (r *RelationTypeRepository) Update(rt *models.RelationType) error {
	return r.db.Save(rt).Error
}

func (r *RelationTypeRepository) Delete(id, ontologyID, tenantID uint) error {
	return r.db.Where("id = ? AND ontology_id = ? AND tenant_id = ?", id, ontologyID, tenantID).
		Delete(&models.RelationType{}).Error
}
