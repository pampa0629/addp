package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/addp/standard/internal/models"
)

const (
	defaultDocumentCandidateFamilyPageSize = 20
	maxDocumentCandidateFamilyPageSize     = 100
)

var ErrDocumentCandidateFamilyQueryInvalid = errors.New("document candidate family query invalid")

type DocumentCandidateFamilyListOptions struct {
	State            string
	CandidateType    string
	Keyword          string
	ComparisonResult string
	Page             int
	PageSize         int
}

type documentCandidateGroupBuilder struct {
	group              models.DocumentExtractionCandidateGroup
	hasFormalization   bool
	formalizationTime  time.Time
	hasReview          bool
	reviewTime         time.Time
	representativeSeen time.Time
}

type canonicalDocumentCandidate struct {
	CandidateType string                            `json:"candidate_type"`
	Code          string                            `json:"code"`
	Name          string                            `json:"name"`
	Definition    string                            `json:"definition"`
	Payload       canonicalDocumentCandidatePayload `json:"payload"`
}

type canonicalDocumentCandidatePayload struct {
	DataType           string                                          `json:"data_type"`
	ValueDomainKind    string                                          `json:"value_domain_kind"`
	CodeSetCode        string                                          `json:"code_set_code"`
	Unit               string                                          `json:"unit"`
	CalculationFormula string                                          `json:"calculation_formula"`
	StatisticalScope   string                                          `json:"statistical_scope"`
	Aggregation        string                                          `json:"aggregation"`
	Dimensions         []string                                        `json:"dimensions"`
	Items              []models.DocumentExtractionCandidatePayloadItem `json:"items"`
}

func (s *DocumentService) ListCandidateFamilies(documentID, tenantID int64, opts DocumentCandidateFamilyListOptions) (*models.PaginatedDocumentExtractionCandidateFamilyResponse, error) {
	page, pageSize, err := normalizeDocumentCandidateFamilyOptions(&opts)
	if err != nil {
		return nil, err
	}
	document, err := s.repo.GetByID(documentID, tenantID)
	if err != nil {
		return nil, err
	}
	extractions, err := s.repo.ListExtractions(documentID, tenantID)
	if err != nil {
		return nil, err
	}
	groups := buildDocumentCandidateGroups(extractions)

	response := &models.PaginatedDocumentExtractionCandidateFamilyResponse{
		Data:       []models.DocumentExtractionCandidateFamily{},
		Page:       page,
		PageSize:   pageSize,
		TotalPages: 1,
	}
	filtered := make([]models.DocumentExtractionCandidateGroup, 0, len(groups))
	for _, group := range groups {
		incrementCandidateVariantStatusCount(&response.VariantStatusCounts, group.State)
		if opts.State != "" && group.State != opts.State {
			continue
		}
		if opts.CandidateType != "" && group.Candidate.CandidateType != opts.CandidateType {
			continue
		}
		if opts.Keyword != "" && !documentCandidateMatchesKeyword(group.Candidate, opts.Keyword) {
			continue
		}
		filtered = append(filtered, group)
	}
	if err := attachCandidateGroupComparisons(s, document, filtered); err != nil {
		return nil, err
	}
	for _, family := range buildDocumentCandidateFamilies(filtered) {
		incrementCandidateFamilyComparisonCounts(&response.FamilyComparisonCounts, family)
	}
	if opts.ComparisonResult != "" {
		matched := make([]models.DocumentExtractionCandidateGroup, 0, len(filtered))
		for _, group := range filtered {
			if group.Candidate.Comparison != nil && group.Candidate.Comparison.Result == opts.ComparisonResult {
				matched = append(matched, group)
			}
		}
		filtered = matched
	}
	families := buildDocumentCandidateFamilies(filtered)
	response.Total = int64(len(families))
	response.VariantTotal = int64(len(filtered))
	response.TotalPages = max(1, (len(families)+pageSize-1)/pageSize)
	if page > response.TotalPages {
		return response, nil
	}
	start := min((page-1)*pageSize, len(families))
	end := min(start+pageSize, len(families))
	response.Data = families[start:end]
	return response, nil
}

func attachCandidateGroupComparisons(s *DocumentService, document *models.Document, groups []models.DocumentExtractionCandidateGroup) error {
	if len(groups) == 0 {
		return nil
	}
	projection := []models.DocumentExtraction{{Candidates: make([]models.DocumentExtractionCandidate, len(groups))}}
	for index := range groups {
		projection[0].Candidates[index] = groups[index].Candidate
	}
	if err := s.attachCandidateComparisons(document, projection); err != nil {
		return err
	}
	for index := range groups {
		groups[index].Candidate.Comparison = projection[0].Candidates[index].Comparison
	}
	return nil
}

func normalizeDocumentCandidateFamilyOptions(opts *DocumentCandidateFamilyListOptions) (int, int, error) {
	opts.Keyword = strings.ToLower(normalizeCandidateSemanticText(opts.Keyword))
	validState := opts.State == "" || opts.State == models.CandidateGroupStatePending || opts.State == models.CandidateGroupStateRetained || opts.State == models.CandidateGroupStateRejected || opts.State == models.CandidateGroupStateFormalized
	validType := opts.CandidateType == "" || opts.CandidateType == "glossary" || opts.CandidateType == "element" || opts.CandidateType == "code_set" || opts.CandidateType == "metric"
	validComparison := opts.ComparisonResult == "" || opts.ComparisonResult == models.CandidateComparisonNew || opts.ComparisonResult == models.CandidateComparisonExact || opts.ComparisonResult == models.CandidateComparisonContentConflict || opts.ComparisonResult == models.CandidateComparisonScopeConflict
	if !validState || !validType || !validComparison || opts.Page < 0 || opts.PageSize < 0 || opts.PageSize > maxDocumentCandidateFamilyPageSize {
		return 0, 0, ErrDocumentCandidateFamilyQueryInvalid
	}
	page := opts.Page
	if page == 0 {
		page = 1
	}
	pageSize := opts.PageSize
	if pageSize == 0 {
		pageSize = defaultDocumentCandidateFamilyPageSize
	}
	return page, pageSize, nil
}

func documentCandidateMatchesKeyword(candidate models.DocumentExtractionCandidate, keyword string) bool {
	code := strings.ToLower(normalizeCandidateSemanticText(candidate.Code))
	name := strings.ToLower(normalizeCandidateSemanticText(candidate.Name))
	return strings.Contains(code, keyword) || strings.Contains(name, keyword)
}

func buildDocumentCandidateFamilies(groups []models.DocumentExtractionCandidateGroup) []models.DocumentExtractionCandidateFamily {
	families := make([]models.DocumentExtractionCandidateFamily, 0, len(groups))
	indexes := make(map[string]int, len(groups))
	for _, group := range groups {
		key := documentCandidateFamilyKey(group.Candidate)
		index, exists := indexes[key]
		if !exists {
			indexes[key] = len(families)
			families = append(families, models.DocumentExtractionCandidateFamily{
				FamilyKey:          key,
				CandidateType:      group.Candidate.CandidateType,
				Code:               group.Candidate.Code,
				RepresentativeName: group.Candidate.Name,
				FirstSeenAt:        group.FirstSeenAt,
				LastSeenAt:         group.LastSeenAt,
				Variants:           []models.DocumentExtractionCandidateGroup{},
			})
			index = len(families) - 1
		}
		family := &families[index]
		family.Variants = append(family.Variants, group)
		family.VariantCount++
		family.OccurrenceCount += group.OccurrenceCount
		if group.FirstSeenAt.Before(family.FirstSeenAt) {
			family.FirstSeenAt = group.FirstSeenAt
		}
		if group.LastSeenAt.After(family.LastSeenAt) {
			family.LastSeenAt = group.LastSeenAt
		}
	}
	return families
}

func documentCandidateFamilyKey(candidate models.DocumentExtractionCandidate) string {
	return candidate.CandidateType + ":" + candidate.Code
}

func buildDocumentCandidateGroups(extractions []models.DocumentExtraction) []models.DocumentExtractionCandidateGroup {
	builders := map[string]*documentCandidateGroupBuilder{}
	for extractionIndex := range extractions {
		extraction := &extractions[extractionIndex]
		for candidateIndex := range extraction.Candidates {
			candidate := extraction.Candidates[candidateIndex]
			if candidate.Evidences == nil {
				candidate.Evidences = []models.DocumentExtractionEvidence{}
			}
			fingerprint := documentCandidateSemanticFingerprint(candidate)
			builder := builders[fingerprint]
			if builder == nil {
				builder = &documentCandidateGroupBuilder{group: models.DocumentExtractionCandidateGroup{
					SemanticFingerprint: fingerprint,
					State:               models.CandidateGroupStatePending,
					FirstSeenAt:         extraction.CreatedAt,
					LastSeenAt:          extraction.CreatedAt,
					Candidate:           candidate,
					Occurrences:         []models.DocumentExtractionCandidateOccurrence{},
				}, representativeSeen: extraction.CreatedAt}
				builders[fingerprint] = builder
			}
			if extraction.CreatedAt.Before(builder.group.FirstSeenAt) {
				builder.group.FirstSeenAt = extraction.CreatedAt
			}
			if extraction.CreatedAt.After(builder.group.LastSeenAt) {
				builder.group.LastSeenAt = extraction.CreatedAt
			}
			builder.group.Occurrences = append(builder.group.Occurrences, models.DocumentExtractionCandidateOccurrence{
				CandidateID: candidate.ID, ExtractionID: extraction.ID, DocumentRevisionID: extraction.DocumentRevisionID,
				RequestedBy: extraction.RequestedBy, ExtractedAt: extraction.CreatedAt, Status: candidate.Status, Version: candidate.Version,
				ReviewedBy: candidate.ReviewedBy, ReviewedAt: candidate.ReviewedAt, Evidences: candidate.Evidences, Formalization: candidate.Formalization,
			})
			selectDocumentCandidateGroupRepresentative(builder, candidate, extraction.CreatedAt)
		}
	}

	groups := make([]models.DocumentExtractionCandidateGroup, 0, len(builders))
	for _, builder := range builders {
		builder.group.OccurrenceCount = len(builder.group.Occurrences)
		sort.SliceStable(builder.group.Occurrences, func(i, j int) bool {
			left, right := builder.group.Occurrences[i], builder.group.Occurrences[j]
			if !left.ExtractedAt.Equal(right.ExtractedAt) {
				return left.ExtractedAt.After(right.ExtractedAt)
			}
			if left.ExtractionID != right.ExtractionID {
				return left.ExtractionID > right.ExtractionID
			}
			return left.CandidateID > right.CandidateID
		})
		groups = append(groups, builder.group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		left, right := groups[i], groups[j]
		if candidateGroupStateRank(left.State) != candidateGroupStateRank(right.State) {
			return candidateGroupStateRank(left.State) < candidateGroupStateRank(right.State)
		}
		if !left.LastSeenAt.Equal(right.LastSeenAt) {
			return left.LastSeenAt.After(right.LastSeenAt)
		}
		if left.Candidate.CandidateType != right.Candidate.CandidateType {
			return left.Candidate.CandidateType < right.Candidate.CandidateType
		}
		return left.Candidate.Code < right.Candidate.Code
	})
	return groups
}

func selectDocumentCandidateGroupRepresentative(builder *documentCandidateGroupBuilder, candidate models.DocumentExtractionCandidate, seenAt time.Time) {
	if candidate.Formalization != nil {
		formalizedAt := candidate.Formalization.CreatedAt
		if !builder.hasFormalization || formalizedAt.After(builder.formalizationTime) || (formalizedAt.Equal(builder.formalizationTime) && candidate.ID > builder.group.Candidate.ID) {
			builder.hasFormalization, builder.formalizationTime = true, formalizedAt
			builder.group.State, builder.group.Candidate = models.CandidateGroupStateFormalized, candidate
		}
		return
	}
	if builder.hasFormalization {
		return
	}
	if candidate.ReviewedAt != nil && (candidate.Status == models.CandidateGroupStateRetained || candidate.Status == models.CandidateGroupStateRejected) {
		if !builder.hasReview || candidate.ReviewedAt.After(builder.reviewTime) || (candidate.ReviewedAt.Equal(builder.reviewTime) && candidate.ID > builder.group.Candidate.ID) {
			builder.hasReview, builder.reviewTime = true, *candidate.ReviewedAt
			builder.group.State, builder.group.Candidate = candidate.Status, candidate
		}
		return
	}
	if !builder.hasReview && (seenAt.After(builder.representativeSeen) || (seenAt.Equal(builder.representativeSeen) && candidate.ID > builder.group.Candidate.ID)) {
		builder.representativeSeen, builder.group.Candidate = seenAt, candidate
	}
}

func documentCandidateSemanticFingerprint(candidate models.DocumentExtractionCandidate) string {
	canonical := canonicalDocumentCandidate{
		CandidateType: candidate.CandidateType,
		Code:          candidate.Code,
		Name:          normalizeCandidateSemanticText(candidate.Name),
		Definition:    normalizeCandidateSemanticText(candidate.Definition),
		Payload: canonicalDocumentCandidatePayload{
			DataType: normalizedCandidatePointer(candidate.Payload.DataType), ValueDomainKind: normalizedCandidatePointer(candidate.Payload.ValueDomainKind),
			CodeSetCode: normalizedCandidatePointer(candidate.Payload.CodeSetCode), Unit: normalizedCandidatePointer(candidate.Payload.Unit),
			CalculationFormula: normalizedCandidatePointer(candidate.Payload.CalculationFormula), StatisticalScope: normalizedCandidatePointer(candidate.Payload.StatisticalScope),
			Aggregation: normalizedCandidatePointer(candidate.Payload.Aggregation), Dimensions: normalizeCandidateDimensions(candidate.Payload.Dimensions),
			Items: normalizeCandidateItems(candidate.Payload.Items),
		},
	}
	encoded, _ := json.Marshal(canonical)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func normalizeCandidateSemanticText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizedCandidatePointer(value *string) string {
	if value == nil {
		return ""
	}
	return normalizeCandidateSemanticText(*value)
}

func normalizeCandidateDimensions(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeCandidateSemanticText(value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result
}

func normalizeCandidateItems(values []models.DocumentExtractionCandidatePayloadItem) []models.DocumentExtractionCandidatePayloadItem {
	result := make([]models.DocumentExtractionCandidatePayloadItem, 0, len(values))
	for _, value := range values {
		result = append(result, models.DocumentExtractionCandidatePayloadItem{
			Code: normalizeCandidateSemanticText(value.Code), Name: normalizeCandidateSemanticText(value.Name), Definition: normalizeCandidateSemanticText(value.Definition),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Code != result[j].Code {
			return result[i].Code < result[j].Code
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Definition < result[j].Definition
	})
	return result
}

func incrementCandidateVariantStatusCount(counts *models.DocumentExtractionCandidateVariantStatusCounts, state string) {
	switch state {
	case models.CandidateGroupStatePending:
		counts.Pending++
	case models.CandidateGroupStateRetained:
		counts.Retained++
	case models.CandidateGroupStateRejected:
		counts.Rejected++
	case models.CandidateGroupStateFormalized:
		counts.Formalized++
	}
}

func incrementCandidateFamilyComparisonCounts(counts *models.DocumentExtractionCandidateFamilyComparisonCounts, family models.DocumentExtractionCandidateFamily) {
	counts.All++
	results := make(map[string]struct{}, len(family.Variants))
	for _, variant := range family.Variants {
		if variant.Candidate.Comparison != nil {
			results[variant.Candidate.Comparison.Result] = struct{}{}
		}
	}
	if _, ok := results[models.CandidateComparisonNew]; ok {
		counts.New++
	}
	if _, ok := results[models.CandidateComparisonExact]; ok {
		counts.Exact++
	}
	if _, ok := results[models.CandidateComparisonContentConflict]; ok {
		counts.ContentConflict++
	}
	if _, ok := results[models.CandidateComparisonScopeConflict]; ok {
		counts.ScopeConflict++
	}
}

func candidateGroupStateRank(state string) int {
	switch state {
	case models.CandidateGroupStatePending:
		return 0
	case models.CandidateGroupStateRetained:
		return 1
	case models.CandidateGroupStateFormalized:
		return 2
	default:
		return 3
	}
}
