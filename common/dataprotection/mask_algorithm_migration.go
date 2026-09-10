package dataprotection

import (
	"errors"
	"unicode/utf8"
)

const legacyAlgorithmKeepPrefixSuffixV1 = "addp.mask.keep_prefix_suffix/v1"

// MigrateKeepPrefixSuffixAlgorithmV2 performs the one-way persisted-data
// migration from the fixed-length structured-field mask to the length-adaptive
// v2 contract. The legacy identifier is intentionally not accepted by the
// runtime validator or executor.
func MigrateKeepPrefixSuffixAlgorithmV2(projection *Projection) (bool, error) {
	if projection == nil {
		return false, errors.New("protection projection is nil")
	}
	changed := false
	for index := range projection.Rules {
		decision := &projection.Rules[index].Decision
		if decision.Algorithm != legacyAlgorithmKeepPrefixSuffixV1 {
			continue
		}
		if decision.Effect != EffectMask {
			return false, errors.New("legacy structured mask has invalid effect")
		}
		if err := validatePhoneOccurrencesV1Parameters(decision.Parameters); err != nil {
			return false, err
		}
		prefix, _ := integerParameter(decision.Parameters, "prefix_runes")
		suffix, _ := integerParameter(decision.Parameters, "suffix_runes")
		exact, _ := integerParameter(decision.Parameters, "exact_runes")
		replacement := decision.Parameters["replacement"].(string)
		replacementRunes := []rune(replacement)
		if len(replacementRunes) != exact-prefix-suffix || len(replacementRunes) == 0 {
			return false, errors.New("legacy structured mask replacement length is invalid")
		}
		for _, candidate := range replacementRunes[1:] {
			if candidate != replacementRunes[0] {
				return false, errors.New("legacy structured mask replacement is not repeatable")
			}
		}
		maskRune := string(replacementRunes[0])
		if !utf8.ValidString(maskRune) {
			return false, errors.New("legacy structured mask rune is invalid")
		}
		decision.Algorithm = AlgorithmKeepPrefixSuffixV2
		decision.Parameters = map[string]any{
			"prefix_runes": prefix,
			"suffix_runes": suffix,
			"mask_rune":    maskRune,
		}
		changed = true
	}
	if changed {
		if err := projection.Seal(); err != nil {
			return false, err
		}
	}
	return changed, nil
}
