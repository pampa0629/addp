package dataquality

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

const RuleBackfillNamespace = "f3889a4a-1675-4623-b6e3-773f9125a04d"

const ruleBackfillNamePrefix = "addp.quality.rule-backfill/v1"

// BackfillRuleKey implements the one-time legacy identity algorithm from the
// ADDP data quality spec. canonicalRule must be PostgreSQL jsonb text for the
// complete rule after removing rule_key.
func BackfillRuleKey(tenantID, elementID int64, canonicalRule []byte, duplicateOccurrence int) (string, error) {
	if tenantID <= 0 || elementID <= 0 {
		return "", fmt.Errorf("tenant_id and element_id must be positive")
	}
	if len(canonicalRule) == 0 {
		return "", fmt.Errorf("canonical rule must not be empty")
	}
	if duplicateOccurrence <= 0 {
		return "", fmt.Errorf("duplicate occurrence must be positive")
	}

	namespace := uuid.MustParse(RuleBackfillNamespace)
	fingerprint := sha256.Sum256(canonicalRule)
	name := fmt.Sprintf(
		"%s|tenant_id=%d|element_id=%d|rule_fingerprint=%s|duplicate_occurrence=%d",
		ruleBackfillNamePrefix,
		tenantID,
		elementID,
		hex.EncodeToString(fingerprint[:]),
		duplicateOccurrence,
	)
	payload := make([]byte, 0, len(namespace)+len(name))
	payload = append(payload, namespace[:]...)
	payload = append(payload, name...)
	digest := sha256.Sum256(payload)
	digest[6] = (digest[6] & 0x0f) | 0x80
	digest[8] = (digest[8] & 0x3f) | 0x80

	ruleKey, err := uuid.FromBytes(digest[:16])
	if err != nil {
		return "", fmt.Errorf("build backfill rule key: %w", err)
	}
	return ruleKey.String(), nil
}
