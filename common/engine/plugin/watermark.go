package plugin

import (
	"fmt"
	"strings"
)

// NormalizeWatermarkFields validates and orders one watermark field followed by stable tie-breakers.
func NormalizeWatermarkFields(watermark string, tieBreakers []string) ([]string, error) {
	watermark = strings.TrimSpace(watermark)
	if watermark == "" {
		return nil, fmt.Errorf("watermark field is required")
	}
	result := []string{watermark}
	seen := map[string]bool{watermark: true}
	for _, tieBreaker := range tieBreakers {
		tieBreaker = strings.TrimSpace(tieBreaker)
		if tieBreaker == "" || seen[tieBreaker] {
			return nil, fmt.Errorf("watermark tie_breaker fields must be non-empty and unique")
		}
		seen[tieBreaker] = true
		result = append(result, tieBreaker)
	}
	if len(result) == 1 {
		return nil, fmt.Errorf("watermark requires at least one tie_breaker field")
	}
	return result, nil
}
