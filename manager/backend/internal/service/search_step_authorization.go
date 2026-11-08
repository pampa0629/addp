package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	shared "github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/meilisearch/meilisearch-go"
)

func decodeSearchIdentity(value string) (searchIdentity, bool) {
	var id searchIdentity
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != len(id) || value != strings.ToLower(value) {
		return id, false
	}
	copy(id[:], decoded)
	return id, true
}
func searchIdentityString(id searchIdentity) string { return hex.EncodeToString(id[:]) }
func (s *searchSession) engineFilter() *uint {
	if s.engine == 0 {
		return nil
	}
	return &s.engine
}
func (step *searchStep) observe(ctx context.Context, targets []plugin.EngineCatalogPath) (*shared.SourceSearchCheck, error) {
	s := step.session
	request := shared.SourceSearchCheckRequest{Targets: targets, QueryNonce: hex.EncodeToString(s.nonce[:])}
	if s.observationSet {
		tag := base64.RawURLEncoding.EncodeToString(s.observationTag[:])
		request.ExpectedObservationTag = &tag
	}
	response, err := step.checker.CheckManagerSearchRead(ctx, step.credential, request)
	if errors.Is(err, commonClient.ErrSearchContextChanged) {
		return nil, ErrSearchContextChanged
	}
	if err != nil {
		return nil, err
	}
	if !response.Matches(request) {
		return nil, ErrSearchVisibilityUnavailable
	}
	if !s.observationSet {
		tag, _ := base64.RawURLEncoding.DecodeString(response.ObservationTag)
		copy(s.observationTag[:], tag)
		s.observationSet = true
	}
	return response, nil
}

// Hydrate only this candidate batch; never enumerate tenant documents or ACLs.
func (s *searchSession) identities(ctx context.Context, ids []searchIdentity) ([]indexedSearchIdentity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	quoted := make([]string, len(ids))
	wanted := make(map[string]bool, len(ids))
	for i, id := range ids {
		encoded := searchIdentityString(id)
		quoted[i] = fmt.Sprintf("%q", encoded)
		wanted[encoded] = true
	}
	filter := buildSearchFilter(&s.tenant, s.engineFilter()) + " AND document_id IN [" + strings.Join(quoted, ",") + "]"
	var page meilisearch.DocumentsResult
	if err := s.service.client.Index(s.service.contentIndex).GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{Filter: filter, Limit: int64(len(ids)), Fields: []string{"id", "document_id", "locator", "engine_type", "tenant_id", "engine_id"}}, &page); err != nil {
		return nil, err
	}
	var indexed []struct {
		indexedSearchIdentity
		PrimaryID string `json:"id"`
	}
	if err := page.Results.DecodeInto(&indexed); err != nil {
		return nil, err
	}
	if len(indexed) > len(ids) {
		return nil, ErrSearchVisibilityUnavailable
	}
	result := make([]indexedSearchIdentity, 0, len(indexed))
	seen := map[string]bool{}
	for _, doc := range indexed {
		if !wanted[doc.ID] || seen[doc.ID] || doc.PrimaryID != doc.ID || doc.TenantID != s.tenant || doc.EngineID == 0 || s.engine != 0 && doc.EngineID != s.engine {
			return nil, ErrSearchVisibilityUnavailable
		}
		seen[doc.ID] = true
		result = append(result, doc.indexedSearchIdentity)
	}
	return result, nil
}

func searchAdmitted(kind uint8, flags uint8) bool {
	if kind == searchKeywordMetadata {
		return flags&1 != 0 && flags&2 == 0
	}
	return flags&2 != 0
}
func (step *searchStep) authorize(ctx context.Context, hits []searchSourceHit) ([]bool, error) {
	if err := step.input(len(hits)); err != nil {
		return nil, err
	}
	missing := make([]searchIdentity, 0, len(hits))
	for _, hit := range hits {
		entry := step.decision(hit.ID)
		if entry == nil || !entry.Used {
			if _, err := step.ensureDecision(hit.ID); err != nil {
				return nil, err
			}
			missing = append(missing, hit.ID)
		}
	}
	identities, err := step.session.identities(ctx, missing)
	if err != nil {
		return nil, err
	}
	paths := make([]plugin.EngineCatalogPath, 0, len(identities))
	valid := make([]indexedSearchIdentity, 0, len(identities))
	for _, identity := range identities {
		if path, ok := identity.catalogPath(); ok {
			paths = append(paths, path)
			valid = append(valid, identity)
		}
	}
	if len(paths) > 0 {
		observed, err := step.observe(ctx, paths)
		if err != nil {
			return nil, err
		}
		for i, target := range observed.Targets {
			id, _ := decodeSearchIdentity(valid[i].ID)
			entry := step.decision(id)
			entry.Engine = valid[i].EngineID
			entry.Locator = sha256.Sum256([]byte(valid[i].Locator))
			if target.Visible {
				if target.ItemFingerprint != valid[i].ID {
					continue
				}
				entry.Flags |= 1
			}
			if target.ContentReadable {
				entry.Flags |= 2
			}
		}
	}
	result := make([]bool, len(hits))
	for i, hit := range hits {
		entry := step.decision(hit.ID)
		result[i] = searchAdmitted(hit.Kind, entry.Flags)
		if hit.Kind == searchVectorContent && (entry.Engine != hit.ExpectedEngine || entry.Locator != hit.ExpectedLocator) {
			result[i] = false
		}
	}
	return result, nil
}
