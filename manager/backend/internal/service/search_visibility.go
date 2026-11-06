package service

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
	"github.com/meilisearch/meilisearch-go"
)

var ErrSearchVisibilityUnavailable = errors.New("search visibility unavailable")

type SearchVisibilityChecker interface {
	CheckManagerSearchRead(context.Context, string, []plugin.EngineCatalogPath) (*authorization.SourceSearchCheck, error)
}

type indexedSearchIdentity struct {
	ID         string `json:"document_id"`
	Locator    string `json:"locator"`
	EngineType string `json:"engine_type"`
	TenantID   uint   `json:"tenant_id"`
	EngineID   uint   `json:"engine_id"`
}

type searchVisibility struct {
	metadata map[string]indexedSearchIdentity
	content  map[string]indexedSearchIdentity
}

func (s *HybridSearchService) observeSearchVisibility(ctx context.Context, tenant uint, engineID *uint, credential string, checker SearchVisibilityChecker) (searchVisibility, error) {
	result := searchVisibility{metadata: map[string]indexedSearchIdentity{}, content: map[string]indexedSearchIdentity{}}
	const batchSize int64 = 200
	const maxDocuments int64 = 100000
	filter := buildSearchFilter(&tenant, engineID)
	index := s.client.Index(s.contentIndex)
	seen := map[string]bool{}
	total := int64(-1)
	for offset := int64(0); ; {
		var page meilisearch.DocumentsResult
		err := index.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{Filter: filter, Offset: offset, Limit: batchSize,
			Fields: []string{"document_id", "locator", "engine_type", "tenant_id", "engine_id"}}, &page)
		if err != nil || page.Offset != offset || page.Limit != batchSize || page.Total < 0 || page.Total > maxDocuments || total >= 0 && total != page.Total {
			return searchVisibility{}, ErrSearchVisibilityUnavailable
		}
		total = page.Total
		var docs []indexedSearchIdentity
		if err := page.Results.DecodeInto(&docs); err != nil || len(docs) > int(batchSize) {
			return searchVisibility{}, ErrSearchVisibilityUnavailable
		}
		paths := make([]plugin.EngineCatalogPath, 0, len(docs))
		candidates := make([]indexedSearchIdentity, 0, len(docs))
		for _, doc := range docs {
			if doc.TenantID != tenant || doc.EngineID == 0 || engineID != nil && doc.EngineID != *engineID || doc.ID == "" || seen[doc.ID] {
				return searchVisibility{}, ErrSearchVisibilityUnavailable
			}
			seen[doc.ID] = true
			path, ok := doc.catalogPath()
			if ok {
				paths = append(paths, path)
				candidates = append(candidates, doc)
			}
		}
		if len(paths) > 0 {
			observed, err := checker.CheckManagerSearchRead(ctx, credential, paths)
			if err != nil {
				return searchVisibility{}, err
			}
			if !observed.Matches(authorization.SourceMetadataCheckRequest{Targets: paths}) {
				return searchVisibility{}, ErrSearchVisibilityUnavailable
			}
			for i, target := range observed.Targets {
				doc := candidates[i]
				if target.ContentReadable {
					result.content[doc.ID] = doc
				} else if target.Visible {
					result.metadata[doc.ID] = doc
				}
			}
		}
		offset += int64(len(docs))
		if offset > total || offset < total && len(docs) == 0 {
			return searchVisibility{}, ErrSearchVisibilityUnavailable
		}
		if offset == total {
			return result, nil
		}
	}
}

func (doc indexedSearchIdentity) catalogPath() (plugin.EngineCatalogPath, bool) {
	loc, err := resourcetree.ParseURI(doc.Locator)
	if err != nil || loc.ItemID == nil || *loc.ItemID == 0 || loc.EngineID != doc.EngineID {
		return plugin.EngineCatalogPath{}, false
	}
	model, err := dbbridge.EngineCatalogModel(doc.EngineType)
	if err != nil {
		return plugin.EngineCatalogPath{}, false
	}
	path, err := resourcetree.EngineCatalogPathFromLocator(model, loc)
	if err != nil {
		return plugin.EngineCatalogPath{}, false
	}
	fingerprint, err := authorizationfacts.PrimaryCandidateFingerprint(path)
	return path, err == nil && fingerprint == doc.ID
}

func visibleSearchIDs(identities map[string]indexedSearchIdentity) []string {
	ids := make([]string, 0, len(identities))
	for id := range identities {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func searchIdentityFilter(filter string, identities map[string]indexedSearchIdentity) string {
	ids := visibleSearchIDs(identities)
	for i, id := range ids {
		ids[i] = strconv.Quote(id)
	}
	return filter + " AND document_id IN [" + strings.Join(ids, ",") + "]"
}
