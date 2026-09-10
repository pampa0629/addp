package candidate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

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
