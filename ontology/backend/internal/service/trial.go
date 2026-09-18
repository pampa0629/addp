package service

import (
	"context"

	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
)

type TrialResult struct {
	SemanticBinding
	Decision semantic.Decision `json:"decision"`
}

// Trial evaluates user-supplied hypotheses, never authenticated business facts.
// No inputs or results are persisted and no graph/business queries are executed.
func (s *RevisionService) Trial(ctx context.Context, actor models.Actor, id, ruleID string, revision uint64, generation string, activation uint64, facts map[string]semantic.Fact) (*TrialResult, error) {
	snapshot, binding, err := s.activeSnapshot(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if binding.Revision != revision || binding.Generation != generation || binding.ActivationVersion != activation {
		return nil, ErrActivationChanged
	}
	decision := snapshot.Evaluate(ctx, snapshot.Scope(), ruleID, facts)
	switch decision.Code {
	case "unknown_rule":
		return nil, repository.ErrNotFound
	case "input_limit", "unexpected_input":
		return nil, repository.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Recheck publication/withdrawal and ABA before releasing any conclusion.
	_, current, err := s.activeSnapshot(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	if current != binding {
		return nil, ErrActivationChanged
	}
	result := &TrialResult{SemanticBinding: binding, Decision: decision}
	return result, boundSemanticResult(ctx, result)
}
