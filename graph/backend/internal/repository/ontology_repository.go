package repository

import (
	"errors"

	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OntologyRepository struct {
	db *gorm.DB
}

func NewOntologyRepository(db *gorm.DB) *OntologyRepository {
	return &OntologyRepository{db: db}
}

func (r *OntologyRepository) List(tenantID uint) ([]models.Ontology, error) {
	var ontologies []models.Ontology
	err := r.db.Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&ontologies).Error
	return ontologies, err
}

func (r *OntologyRepository) GetByID(id, tenantID uint) (*models.Ontology, error) {
	var ontology models.Ontology
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&ontology).Error
	return &ontology, err
}

func (r *OntologyRepository) GetDetail(id, tenantID uint) (*models.Ontology, error) {
	var ontology models.Ontology
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		Preload("EntityTypes", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC, created_at ASC")
		}).
		Preload("RelationTypes", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC, created_at ASC")
		}).
		First(&ontology).Error
	return &ontology, err
}

func (r *OntologyRepository) Create(ontology *models.Ontology) error {
	return r.db.Create(ontology).Error
}

func (r *OntologyRepository) Update(ontology *models.Ontology) error {
	return r.db.Save(ontology).Error
}

func (r *OntologyRepository) Delete(id, tenantID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var ontology models.Ontology
		err := tx.Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", id, tenantID).First(&ontology).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, model := range []any{&models.RelationType{}, &models.EntityType{}, &models.OntologyVersion{}} {
			if err := tx.Where("ontology_id = ? AND tenant_id = ?", id, tenantID).Delete(model).Error; err != nil {
				return err
			}
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Ontology{}).Error
	})
}
