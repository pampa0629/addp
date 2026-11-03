package engineaccess

import (
	"context"
	"encoding/json"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
)

type NodeIdentityReader interface {
	ResolveNodeIdentity(context.Context, uint, authorizationfacts.NodeIdentityQuery) (*authorizationfacts.NodeIdentity, error)
}

type NodeIdentityPageReader interface {
	QueryNodeIdentities(context.Context, uint, authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error)
}

func (s *Service) WithNodeIdentityPageReader(reader NodeIdentityPageReader) *Service {
	s.nodeIdentityPages = reader
	return s
}

// This is management discovery only. Meta facts never substitute for the
// current caller qualification, either before or after cross-owner IO.
func (s *Service) ListAuthorizationNodes(ctx context.Context, actor Actor, engineID int64, query authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error) {
	if query.Validate() != nil || int64(query.Target.EngineID) != engineID {
		return nil, commonapi.ErrBadRequest
	}
	qualify := func() error {
		return s.withEngineManagementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessGrantRead, false, func(tx *Repository, check func() error) error {
			engine, err := tx.engine(ctx, actor.TenantID, engineID, false)
			if err != nil {
				return err
			}
			if engine.EngineType != "postgresql" {
				return commonapi.ErrBadRequest
			}
			return check()
		})
	}
	if err := qualify(); err != nil {
		return nil, err
	}
	if s.nodeIdentityPages == nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	result, err := s.nodeIdentityPages.QueryNodeIdentities(ctx, uint(actor.TenantID), query)
	if qualifyErr := qualify(); qualifyErr != nil {
		return nil, qualifyErr
	}
	if err != nil || result == nil || result.Validate(uint(actor.TenantID), query) != nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	return result, nil
}

func (s *Service) WithNodeIdentityReader(reader NodeIdentityReader) *Service {
	s.nodeIdentities = reader
	return s
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func sameOptionalBool(a, b *bool) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func validateIndependentGrantTarget(input CreateIndependentGrantInput) error {
	if input.NodeRecursive == nil {
		if input.Action == shared.SourceAccessMetadataRead {
			candidate, err := authorizationfacts.PrimaryCandidateFingerprint(input.CatalogPath)
			if err != nil || candidate != input.ItemFingerprint || !authorizationfacts.ValidItemFingerprint(input.ExpectedBoundaryHash) {
				return commonapi.ErrBadRequest
			}
			return nil
		}
		return validateExpectedPrimary(input.CatalogPath, input.ItemFingerprint, input.ExpectedBoundaryHash)
	}
	if input.ItemFingerprint != "" || input.ExpectedBoundaryHash != "" {
		return commonapi.ErrBadRequest
	}
	if (shared.SourceNodeScope{Target: input.CatalogPath, Recursive: *input.NodeRecursive}).Validate() != nil {
		return commonapi.ErrBadRequest
	}
	return nil
}

func encodeIndependentGrantTarget(path plugin.EngineCatalogPath, recursive *bool) (json.RawMessage, error) {
	if recursive == nil {
		return shared.EncodeSharingTarget(path)
	}
	if err := (shared.SourceNodeScope{Target: path, Recursive: *recursive}).Validate(); err != nil {
		return nil, commonapi.ErrBadRequest
	}
	return json.Marshal(path)
}

// Qualification completes before cross-owner IO. A node is proved by Meta,
// never by accepting browser ancestry or enumerating its current leaf items.
func (s *Service) verifyNewIndependentGrantTarget(ctx context.Context, input CreateIndependentGrantInput, row *sourceGrant) (int64, error) {
	if row.NodeRecursive != nil {
		engine, err := s.repository.engine(ctx, row.TenantID, row.EngineID, false)
		if err != nil {
			return 0, err
		}
		if engine.EngineType != "postgresql" {
			return 0, commonapi.ErrBadRequest
		}
		if s.nodeIdentities == nil {
			return 0, ErrAuthorizationFactsUnavailable
		}
		query := authorizationfacts.NodeIdentityQuery{Target: input.CatalogPath}
		facts, err := s.nodeIdentities.ResolveNodeIdentity(ctx, uint(row.TenantID), query)
		if err != nil || facts == nil || facts.Validate(uint(row.TenantID), query) != nil {
			return 0, ErrAuthorizationFactsUnavailable
		}
		if !facts.Exists {
			return 0, commonapi.ErrNotFound
		}
		return engine.Version, nil
	}
	var binding *authorizationfacts.Binding
	if input.Action == shared.SourceAccessMetadataRead {
		bindings, err := s.resolveMetadataBindings(ctx, row.TenantID, []plugin.EngineCatalogPath{input.CatalogPath})
		if err != nil {
			return 0, err
		}
		if len(bindings) != 1 || bindings[0].ItemFingerprint != row.ItemFingerprint || bindings[0].BoundaryHash != row.BoundaryHash {
			return 0, ErrAuthorizationBoundaryChanged
		}
		binding = &bindings[0]
	} else {
		var err error
		binding, err = s.resolveApprovedBoundary(ctx, row.TenantID, input.CatalogPath, row.ItemFingerprint, row.BoundaryHash)
		if err != nil {
			return 0, err
		}
	}
	if binding.Scope != nil {
		encoded, err := json.Marshal(binding.Scope)
		if err != nil {
			return 0, err
		}
		row.ScopeBoundary = encoded
	}
	if binding.FileComposition != nil {
		encoded, err := json.Marshal(binding.FileComposition)
		if err != nil {
			return 0, err
		}
		row.FileComposition = encoded
	}
	if binding.MetadataTarget != nil {
		encoded, err := json.Marshal(binding.MetadataTarget)
		if err != nil {
			return 0, err
		}
		row.MetadataTarget = encoded
	}
	if input.Action == shared.SourceAccessMetadataRead {
		engine, err := s.repository.engine(ctx, row.TenantID, row.EngineID, false)
		if err != nil {
			return 0, err
		}
		return engine.Version, nil
	}
	if s.independentTarget == nil {
		return 0, ErrIndependentGrantSourceUnavailable
	}
	version, err := s.independentTarget.VerifyIndependentReadTarget(ctx, input.Actor.TenantID, input.CatalogPath)
	if err != nil {
		return 0, err
	}
	if version <= 0 {
		return 0, ErrIndependentGrantSourceUnavailable
	}
	return version, nil
}
