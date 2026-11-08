package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
)

func (s *searchSession) render(ctx context.Context, step *searchStep, rows []searchRankedIdentity, next uuid.UUID, exhausted bool) (*SearchContinuationResult, error) {
	ids := make([]searchIdentity, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	identities, err := s.identities(ctx, ids)
	if err != nil {
		return nil, err
	}
	identityByID := make(map[searchIdentity]indexedSearchIdentity, len(identities))
	for _, identity := range identities {
		id, _ := decodeSearchIdentity(identity.ID)
		identityByID[id] = identity
	}
	paths := make([]plugin.EngineCatalogPath, 0, len(rows))
	for _, row := range rows {
		identity, ok := identityByID[row.ID]
		if !ok {
			return nil, ErrSearchContextChanged
		}
		path, ok := identity.catalogPath()
		if !ok {
			return nil, ErrSearchContextChanged
		}
		paths = append(paths, path)
	}
	observed, err := step.observe(ctx, paths)
	if err != nil {
		return nil, err
	}
	for i, target := range observed.Targets {
		row := rows[i]
		readable := row.KeywordKind == searchKeywordContent || row.Ranks[1] > 0
		if !target.Visible || target.ItemFingerprint != searchIdentityString(row.ID) || target.ContentReadable != readable {
			return nil, ErrSearchContextChanged
		}
	}
	keyword := make(map[searchIdentity]SearchDocument, len(rows))
	for _, kind := range []uint8{searchKeywordMetadata, searchKeywordContent} {
		group := []searchIdentity{}
		for _, row := range rows {
			if row.Ranks[0] > 0 && row.KeywordKind == kind {
				group = append(group, row.ID)
			}
		}
		for len(group) > 0 {
			size := len(group)
			if int64(size) > s.segmentSize+1 {
				size = int(s.segmentSize + 1)
			}
			if size > 50 {
				size = 50
			}
			chunk := group[:size]
			group = group[size:]
			projected, err := s.keywordProjection(ctx, chunk, kind)
			if err != nil {
				return nil, err
			}
			for id, document := range projected {
				keyword[id] = document
			}
		}
	}
	vectorIDs := []string{}
	for _, row := range rows {
		if row.Ranks[1] > 0 {
			vectorIDs = append(vectorIDs, searchIdentityString(row.ID))
		}
	}
	vectors := make(map[searchIdentity]repository.EmbeddingSimilarityResult, len(vectorIDs))
	if len(vectorIDs) > 0 {
		if s.vectorQuery == nil {
			return nil, ErrSearchContextChanged
		}
		found, err := s.service.vectorRepo.SearchVectorIdentities(ctx, *s.vectorQuery, vectorIDs)
		if err != nil {
			return nil, err
		}
		for _, hit := range found {
			if hit.Embedding == nil {
				return nil, ErrSearchContextChanged
			}
			id, ok := decodeSearchIdentity(hit.Embedding.ItemFingerprint)
			if !ok {
				return nil, ErrSearchContextChanged
			}
			identity, ok := identityByID[id]
			if !ok || identity.Locator != hit.Embedding.Locator || identity.EngineID != hit.Embedding.EngineID {
				return nil, ErrSearchContextChanged
			}
			vectors[id] = hit
		}
	}
	result := &SearchContinuationResult{Hits: make([]SearchDocument, 0, len(rows)), PageSize: s.pageSize, Exhausted: exhausted}
	if next != uuid.Nil {
		result.NextCursor = next.String()
	}
	for _, row := range rows {
		var document SearchDocument
		if row.Ranks[0] > 0 {
			var ok bool
			document, ok = keyword[row.ID]
			if !ok {
				return nil, ErrSearchContextChanged
			}
			identity := identityByID[row.ID]
			if document.Locator != identity.Locator || document.EngineID != identity.EngineID {
				return nil, ErrSearchContextChanged
			}
			document.MatchMethods = []string{"keyword"}
		}
		if row.Ranks[1] > 0 {
			hit, ok := vectors[row.ID]
			if !ok {
				return nil, ErrSearchContextChanged
			}
			emb := hit.Embedding
			vector := vectorDocumentToSearchDocument(VectorDocument{DocumentID: emb.ItemFingerprint, Locator: emb.Locator, EngineID: emb.EngineID, Distance: hit.Distance, ModelProfileID: emb.ModelProfileID, Name: locatorLastSegment(emb.Locator), FileName: locatorLastSegment(emb.Locator)})
			if row.Ranks[0] > 0 {
				mergeVectorSearchFacts(&document, vector)
				document.MatchMethods = []string{"keyword", "vector"}
			} else {
				document = vector
				document.MatchMethods = []string{"vector"}
			}
		}
		document.Score = searchRankScore(row.Ranks)
		result.Hits = append(result.Hits, document)
	}
	if err := s.prepareModel(ctx, false); err != nil {
		return nil, err
	}
	before, _, err := s.service.searchMaterial(ctx, s.tenant)
	if err != nil {
		return nil, err
	}
	if before != s.material {
		return nil, ErrSearchContextChanged
	}
	if _, err := step.observe(ctx, nil); err != nil {
		return nil, err
	}
	after, _, err := s.service.searchMaterial(ctx, s.tenant)
	if err != nil {
		return nil, err
	}
	if after != s.material {
		return nil, ErrSearchContextChanged
	}
	if !time.Now().Before(s.expires) {
		return nil, ErrSearchContinuationExpired
	}
	return result, nil
}

func (s *searchSession) keywordProjection(ctx context.Context, ids []searchIdentity, kind uint8) (map[searchIdentity]SearchDocument, error) {
	quoted := make([]string, len(ids))
	wanted := make(map[string]bool, len(ids))
	for i, id := range ids {
		value := searchIdentityString(id)
		quoted[i] = fmt.Sprintf("%q", value)
		wanted[value] = true
	}
	request := &meilisearch.SearchRequest{Filter: buildSearchFilter(&s.tenant, s.engineFilter()) + " AND document_id IN [" + strings.Join(quoted, ",") + "]", Limit: int64(len(ids)), ShowRankingScoreDetails: true, HighlightPreTag: "<mark>", HighlightPostTag: "</mark>", AttributesToHighlight: []string{"name", "title", "full_name", "content_preview", "description", "tags", "keywords", "author", "fields.name", "fields.data_type", "fields.comment"}, AttributesToRetrieve: []string{"document_id", "locator", "engine_id", "engine_name", "engine_type", "data_item_type", "name", "full_name", "schema", "bucket", "path", "fields", "title", "author", "keywords", "content_preview", "content_type", "document_type", "size_bytes", "word_count", "page_count", "created_date", "modified_date", "data_updated_at", "metadata"}}
	if kind == searchKeywordMetadata {
		request.AttributesToSearchOn = []string{"name", "full_name", "fields.name"}
		request.AttributesToHighlight = request.AttributesToSearchOn
		request.AttributesToRetrieve = []string{"document_id", "locator", "engine_id", "engine_name", "engine_type", "data_item_type", "name", "full_name", "schema", "bucket", "path", "fields.name"}
	}
	response, err := s.service.client.Index(s.service.contentIndex).SearchWithContext(ctx, s.query, request)
	if err != nil {
		return nil, err
	}
	var hits []map[string]any
	if response == nil || response.Hits.DecodeInto(&hits) != nil || len(hits) != len(ids) {
		return nil, ErrSearchContextChanged
	}
	result := make(map[searchIdentity]SearchDocument, len(hits))
	for _, hit := range hits {
		encoded, _ := json.Marshal(hit["_rankingScoreDetails"])
		if bytes.Contains(encoded, []byte(`"skipped"`)) {
			return nil, ErrSearchVisibilityUnavailable
		}
		document := mapMeilisearchHit(hit)
		if !wanted[document.DocumentID] {
			return nil, ErrSearchContextChanged
		}
		id, ok := decodeSearchIdentity(document.DocumentID)
		if !ok {
			return nil, ErrSearchContextChanged
		}
		if _, exists := result[id]; exists {
			return nil, ErrSearchContextChanged
		}
		if kind == searchKeywordMetadata {
			var fields []SearchFieldMatch
			for _, field := range document.FieldMatches {
				if highlight, ok := field.Highlights["name"]; ok {
					fields = append(fields, SearchFieldMatch{Name: field.Name, Highlights: map[string]string{"name": highlight}})
				}
			}
			document = SearchDocument{DocumentID: document.DocumentID, Locator: document.Locator, EngineID: document.EngineID, EngineName: document.EngineName, EngineType: document.EngineType, DataItemType: document.DataItemType, Name: document.Name, FileName: document.FileName, FullName: document.FullName, Schema: document.Schema, Bucket: document.Bucket, Path: document.Path, FieldMatches: fields}
		}
		result[id] = document
	}
	return result, nil
}
