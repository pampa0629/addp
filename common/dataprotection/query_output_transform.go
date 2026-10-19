package dataprotection

import (
	"fmt"
	"time"

	"github.com/addp/common/engine/plugin"
)

// QueryOutputTransformsAt lowers validated structured rules through the same
// lineage mapping and exact-subject decision used by result protection. It
// contains no SQL, provider names or business data. An empty result means no
// output needs a native transformation; it must not be bound as an empty SQL rewrite.
func QueryOutputTransformsAt(source plugin.QueryOutputSource, action string, rules []Rule, subject SubjectReference, now time.Time) ([]plugin.QueryOutputTransform, error) {
	mapped, err := queryOutputRules(source, action, rules)
	if err != nil {
		return nil, err
	}
	operations := []plugin.QueryOutputTransform{}
	seen := map[string]bool{}
	unsupported := fmt.Errorf("%w: %w", ErrDenied, plugin.ErrQueryOutputTransformUnsupported)
	for _, rule := range mapped {
		if err := ValidateStructuredDecision(rule.Decision, rule.Component.ValueType); err != nil {
			return nil, unsupported
		}
		decision := rule.EffectiveDecision(subject, now)
		if decision.Effect == EffectAllow {
			continue
		}
		if len(rule.Component.Path) != 1 || rule.Component.Path[0].Container != "scalar" {
			return nil, unsupported
		}
		column := rule.Component.Path[0].Name
		if seen[column] {
			return nil, unsupported
		}
		seen[column] = true
		operation := plugin.QueryOutputTransform{Column: column}
		switch decision.Effect {
		case EffectSuppress:
			operation.Kind = "omit"
		case EffectDeny:
			return nil, ErrDenied
		case EffectMask:
			// These validated operations stay in the same shared Go protection
			// pass. They never create a second query or a plaintext output path.
			if decision.Algorithm == AlgorithmSM3V1 || decision.Algorithm == AlgorithmConstantV1 && rule.Component.ValueType != "string" {
				continue
			}
			if rule.Component.ValueType != "string" {
				return nil, unsupported
			}
			switch decision.Algorithm {
			case AlgorithmKeepPrefixSuffixV2:
				operation.Kind = "keep_prefix_suffix"
				operation.Prefix, err = integerParameter(decision.Parameters, "prefix_runes")
				if err != nil {
					return nil, unsupported
				}
				operation.Suffix, err = integerParameter(decision.Parameters, "suffix_runes")
				if err != nil {
					return nil, unsupported
				}
				operation.MaskRune = decision.Parameters["mask_rune"].(string)
				operation.InvalidValue = "reject"
				if invalidEffect(decision) == EffectSuppress {
					operation.InvalidValue = "omit"
				}
			case AlgorithmConstantV1:
				operation.Kind = "constant_text"
				operation.Replacement = decision.Parameters["value"].(string)
			default:
				return nil, unsupported
			}
		default:
			return nil, unsupported
		}
		operations = append(operations, operation)
	}
	return operations, nil
}
