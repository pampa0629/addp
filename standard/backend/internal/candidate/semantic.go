package candidate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/addp/standard/internal/models"
)

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

func SemanticFingerprint(value models.DocumentExtractionCandidate) string {
	canonical := canonicalDocumentCandidate{
		CandidateType: value.CandidateType,
		Code:          value.Code,
		Name:          NormalizeText(value.Name),
		Definition:    NormalizeText(value.Definition),
		Payload: canonicalDocumentCandidatePayload{
			DataType: normalizedPointer(value.Payload.DataType), ValueDomainKind: normalizedPointer(value.Payload.ValueDomainKind),
			CodeSetCode: normalizedPointer(value.Payload.CodeSetCode), Unit: normalizedPointer(value.Payload.Unit),
			CalculationFormula: normalizedPointer(value.Payload.CalculationFormula), StatisticalScope: normalizedPointer(value.Payload.StatisticalScope),
			Aggregation: normalizedPointer(value.Payload.Aggregation), Dimensions: normalizeDimensions(value.Payload.Dimensions),
			Items: normalizeItems(value.Payload.Items),
		},
	}
	encoded, _ := json.Marshal(canonical)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func NormalizeText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// GovernanceState mirrors the read projection's precedence for one candidate occurrence.
func GovernanceState(value models.DocumentExtractionCandidate) string {
	if value.Formalization != nil {
		return models.CandidateGroupStateFormalized
	}
	if value.ReviewedAt != nil && (value.Status == models.CandidateGroupStateRetained || value.Status == models.CandidateGroupStateRejected) {
		return value.Status
	}
	return models.CandidateGroupStatePending
}

// IsPreferredRepresentative applies the single deterministic representative rule shared by reads and writes.
func IsPreferredRepresentative(candidate models.DocumentExtractionCandidate, seenAt time.Time, current models.DocumentExtractionCandidate, currentSeenAt time.Time) bool {
	candidateState, currentState := GovernanceState(candidate), GovernanceState(current)
	candidateRank, currentRank := governanceStateRank(candidateState), governanceStateRank(currentState)
	if candidateRank != currentRank {
		return candidateRank > currentRank
	}
	switch candidateState {
	case models.CandidateGroupStateFormalized:
		candidateTime, currentTime := candidate.Formalization.CreatedAt, current.Formalization.CreatedAt
		return candidateTime.After(currentTime) || (candidateTime.Equal(currentTime) && candidate.ID > current.ID)
	case models.CandidateGroupStateRetained, models.CandidateGroupStateRejected:
		candidateTime, currentTime := *candidate.ReviewedAt, *current.ReviewedAt
		return candidateTime.After(currentTime) || (candidateTime.Equal(currentTime) && candidate.ID > current.ID)
	default:
		return seenAt.After(currentSeenAt) || (seenAt.Equal(currentSeenAt) && candidate.ID > current.ID)
	}
}

func governanceStateRank(state string) int {
	switch state {
	case models.CandidateGroupStateFormalized:
		return 2
	case models.CandidateGroupStateRetained, models.CandidateGroupStateRejected:
		return 1
	default:
		return 0
	}
}

func normalizedPointer(value *string) string {
	if value == nil {
		return ""
	}
	return NormalizeText(*value)
}

func normalizeDimensions(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := NormalizeText(value)
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

func normalizeItems(values []models.DocumentExtractionCandidatePayloadItem) []models.DocumentExtractionCandidatePayloadItem {
	result := make([]models.DocumentExtractionCandidatePayloadItem, 0, len(values))
	for _, value := range values {
		result = append(result, models.DocumentExtractionCandidatePayloadItem{
			Code: NormalizeText(value.Code), Name: NormalizeText(value.Name), Definition: NormalizeText(value.Definition),
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
