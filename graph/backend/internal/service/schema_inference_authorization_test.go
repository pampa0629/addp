package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"gorm.io/gorm"
)

func TestApplyInferredSchemaRejectsOtherTenantOntologyBeforeReadingGraph(t *testing.T) {
	db := setupGraphCleanupTestDB(t)
	otherTenantOntology := models.Ontology{TenantID: 8, Name: "other tenant", Status: "active"}
	if err := db.Create(&otherTenantOntology).Error; err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	svc := NewSchemaInferenceService(nil, repository.NewOntologyRepository(db), nil, nil, nil)
	_, err := svc.ApplyInferredSchema(
		context.Background(), 999, 7,
		&models.ApplyInferredSchemaRequest{OntologyID: otherTenantOntology.ID},
	)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant ontology error = %v, want record not found", err)
	}
}

func TestApplyInferredSchemaFromEngineRejectsOtherTenantOntologyBeforeReadingEngine(t *testing.T) {
	db := setupGraphCleanupTestDB(t)
	otherTenantOntology := models.Ontology{TenantID: 8, Name: "other tenant", Status: "active"}
	if err := db.Create(&otherTenantOntology).Error; err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	svc := NewSchemaInferenceService(nil, repository.NewOntologyRepository(db), nil, nil, nil)
	_, err := svc.ApplyInferredSchemaFromEngine(
		context.Background(), 999, otherTenantOntology.ID, 7,
		&models.ApplyInferredSchemaFromEngineRequest{},
	)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant ontology error = %v, want record not found", err)
	}
}
