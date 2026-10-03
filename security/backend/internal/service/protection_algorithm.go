package service

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
)

func jsonText(value any) string { encoded, _ := json.Marshal(value); return string(encoded) }

func validateBaselineConfiguration(request models.ProtectionBaselineRequest) error {
	decision := policyDecision(request.Effect, request.Algorithm, request.Parameters, request.InvalidValueEffect)
	if err := decision.Validate(); err != nil {
		return err
	}
	if request.Effect != dataprotection.EffectMask {
		if len(request.AllowedAlgorithms) != 0 {
			return errors.New("non-transform baseline cannot permit algorithms")
		}
		return nil
	}
	if _, ok := dataprotection.StructuredAlgorithm(request.Algorithm); !ok {
		return errors.New("unsupported baseline algorithm")
	}
	if !slices.Contains(request.AllowedAlgorithms, request.Algorithm) {
		return errors.New("default algorithm must be explicitly permitted")
	}
	seen := make(map[string]bool)
	for _, key := range request.AllowedAlgorithms {
		if _, ok := dataprotection.StructuredAlgorithm(key); !ok || seen[key] {
			return errors.New("invalid permitted algorithm")
		}
		if key == dataprotection.AlgorithmKeepPrefixSuffixV2 && request.Algorithm != key {
			return errors.New("prefix limits require prefix baseline")
		}
		seen[key] = true
	}
	return nil
}

func validatePolicyDecision(baseline models.ProtectionBaseline, fieldType string, decision dataprotection.Decision) error {
	if err := dataprotection.ValidateStructuredDecision(decision, fieldType); err != nil {
		return err
	}
	if protectionEffectRank(decision.Effect) < protectionEffectRank(baseline.Effect) {
		return errors.New("policy cannot lower baseline")
	}
	if decision.Effect != dataprotection.EffectMask {
		return nil
	}
	if !slices.Contains(baseline.AllowedAlgorithms, decision.Algorithm) {
		return errors.New("algorithm is not permitted by baseline")
	}
	if decision.Algorithm == dataprotection.AlgorithmKeepPrefixSuffixV2 {
		if baseline.Algorithm != decision.Algorithm {
			return errors.New("missing baseline prefix limits")
		}
		for _, key := range []string{"prefix_runes", "suffix_runes"} {
			limit, err := parameterInteger(baseline.Parameters[key])
			if err != nil {
				return err
			}
			value, err := parameterInteger(decision.Parameters[key])
			if err != nil || value > limit {
				return errors.New("policy exceeds baseline retention limit")
			}
		}
	}
	if baseline.InvalidValueEffect == dataprotection.EffectDeny && decision.InvalidValueEffect != dataprotection.EffectDeny {
		return errors.New("policy cannot lower invalid-value protection")
	}
	return nil
}

func parameterInteger(value any) (int64, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	var result int64
	if err := json.Unmarshal(encoded, &result); err != nil {
		return 0, err
	}
	return result, nil
}

func policyDecision(effect, algorithm string, parameters map[string]any, invalid string) dataprotection.Decision {
	if invalid == "" {
		invalid = dataprotection.EffectSuppress
		if effect == dataprotection.EffectDeny {
			invalid = effect
		}
	}
	return dataprotection.Decision{Effect: effect, Algorithm: algorithm, Parameters: parameters, InvalidValueEffect: invalid}
}
