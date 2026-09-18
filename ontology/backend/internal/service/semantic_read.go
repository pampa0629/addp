package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
)

var ErrActivationChanged = errors.New("ontology_activation_changed")
var ErrResultTooLarge = errors.New("result_too_large")

type SemanticBinding struct {
	OntologyID        string `json:"ontology_id"`
	Revision          uint64 `json:"revision"`
	Generation        string `json:"generation"`
	ActivationVersion uint64 `json:"activation_version"`
	Digest            string `json:"digest"`
	KnowledgeKind     string `json:"knowledge_kind"`
}
type ClassDirectory struct {
	SemanticBinding
	Classes []semantic.Class `json:"classes"`
}
type SemanticContext struct {
	SemanticBinding
	semantic.ClassContext
}

func (s *RevisionService) activeSnapshot(ctx context.Context, a models.Actor, id string) (*semantic.Snapshot, SemanticBinding, error) {
	h, r, err := s.repo.ActiveDefinition(ctx, a, id)
	if err != nil {
		return nil, SemanticBinding{}, err
	}
	snapshot, err := semantic.Restore([]byte(r.Payload), r.Digest)
	if err != nil || snapshot.Scope() != (semantic.Scope{TenantID: a.TenantID, OntologyID: id, Revision: r.Revision}) {
		return nil, SemanticBinding{}, repository.ErrIntegrity
	}
	return snapshot, SemanticBinding{id, r.Revision, *h.ActiveGeneration, h.ActivationVersion, r.Digest, "native_definition"}, nil
}
func boundSemanticResult(ctx context.Context, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return repository.ErrIntegrity
	}
	if len(data) > 128<<10 {
		return ErrResultTooLarge
	}
	return nil
}
func (s *RevisionService) ListClasses(ctx context.Context, a models.Actor, id string) (*ClassDirectory, error) {
	snapshot, binding, err := s.activeSnapshot(ctx, a, id)
	if err != nil {
		return nil, err
	}
	result := &ClassDirectory{binding, snapshot.Classes()}
	return result, boundSemanticResult(ctx, result)
}
func (s *RevisionService) ClassContext(ctx context.Context, a models.Actor, id, classID string, revision uint64, generation string, activation uint64) (*SemanticContext, error) {
	snapshot, binding, err := s.activeSnapshot(ctx, a, id)
	if err != nil {
		return nil, err
	}
	if binding.Revision != revision || binding.Generation != generation || binding.ActivationVersion != activation {
		return nil, ErrActivationChanged
	}
	definition, err := snapshot.ClassContext(classID)
	if err != nil {
		return nil, repository.ErrNotFound
	}
	result := &SemanticContext{binding, *definition}
	return result, boundSemanticResult(ctx, result)
}
