package dataprotection

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

var ErrDenied = errors.New("protected data action denied")

type valueResult struct {
	value    any
	suppress bool
}

// ProtectDocument applies every rule for one action to a decoded JSON object.
// It mutates the supplied object in place and never includes protected values
// in returned errors.
func ProtectDocument(document map[string]any, action string, rules []Rule) error {
	for _, rule := range rules {
		if rule.Action != action {
			continue
		}
		if err := protectObjectPath(document, rule.Component.Path, rule.Decision.Effective(time.Now().UTC())); err != nil {
			return err
		}
	}
	return nil
}

func protectObjectPath(object map[string]any, path []PathSegment, decision Decision) error {
	if len(path) == 0 {
		return nil
	}
	segment := path[0]
	value, exists := object[segment.Name]
	if !exists {
		return nil
	}
	if len(path) == 1 {
		result, err := protectValue(value, decision)
		if err != nil {
			return err
		}
		if result.suppress {
			delete(object, segment.Name)
		} else {
			object[segment.Name] = result.value
		}
		return nil
	}

	switch segment.Container {
	case "object":
		nested, ok := value.(map[string]any)
		if !ok {
			return applyInvalidContainer(object, segment.Name, decision)
		}
		return protectObjectPath(nested, path[1:], decision)
	case "array":
		items, ok := value.([]any)
		if !ok {
			return applyInvalidContainer(object, segment.Name, decision)
		}
		for _, item := range items {
			nested, ok := item.(map[string]any)
			if !ok {
				if invalidEffect(decision) == EffectDeny {
					return ErrDenied
				}
				continue
			}
			if err := protectObjectPath(nested, path[1:], decision); err != nil {
				return err
			}
		}
		return nil
	default:
		return errors.New("invalid non-terminal protection path")
	}
}

func applyInvalidContainer(object map[string]any, key string, decision Decision) error {
	switch invalidEffect(decision) {
	case EffectDeny:
		return ErrDenied
	case EffectSuppress, EffectMask:
		delete(object, key)
	}
	return nil
}

func protectValue(value any, decision Decision) (valueResult, error) {
	switch decision.Effect {
	case EffectAllow:
		return valueResult{value: value}, nil
	case EffectSuppress:
		return valueResult{suppress: true}, nil
	case EffectDeny:
		return valueResult{}, ErrDenied
	case EffectMask:
		text, ok := value.(string)
		if !ok {
			return invalidValueResult(decision)
		}
		masked, err := maskKeepPrefixSuffix(text, decision.Parameters)
		if err != nil {
			return invalidValueResult(decision)
		}
		return valueResult{value: masked}, nil
	default:
		return valueResult{}, errors.New("unsupported protection effect")
	}
}

func invalidValueResult(decision Decision) (valueResult, error) {
	switch invalidEffect(decision) {
	case EffectDeny:
		return valueResult{}, ErrDenied
	case EffectSuppress, EffectMask:
		return valueResult{suppress: true}, nil
	default:
		return valueResult{}, errors.New("invalid protected value")
	}
}

func invalidEffect(decision Decision) string {
	if decision.InvalidValueEffect != "" {
		return decision.InvalidValueEffect
	}
	return EffectDeny
}

func maskKeepPrefixSuffix(value string, parameters map[string]any) (string, error) {
	if err := validateKeepPrefixSuffixParameters(parameters); err != nil {
		return "", err
	}
	prefix, err := integerParameter(parameters, "prefix_runes")
	if err != nil {
		return "", err
	}
	suffix, err := integerParameter(parameters, "suffix_runes")
	if err != nil {
		return "", err
	}
	exact, err := integerParameter(parameters, "exact_runes")
	if err != nil {
		return "", err
	}
	replacement := parameters["replacement"].(string)
	runes := []rune(value)
	if len(runes) != exact || prefix+suffix >= len(runes) {
		return "", errors.New("value length does not match masking parameters")
	}
	if parameters["character_class"] == "ascii_digit" {
		for _, current := range runes {
			if current < '0' || current > '9' {
				return "", errors.New("value character class does not match masking parameters")
			}
		}
	}
	return string(runes[:prefix]) + replacement + string(runes[len(runes)-suffix:]), nil
}

func validateKeepPrefixSuffixParameters(parameters map[string]any) error {
	if len(parameters) != 5 {
		return errors.New("invalid mask parameters")
	}
	prefix, err := integerParameter(parameters, "prefix_runes")
	if err != nil || prefix < 0 {
		return errors.New("invalid mask prefix")
	}
	suffix, err := integerParameter(parameters, "suffix_runes")
	if err != nil || suffix < 0 {
		return errors.New("invalid mask suffix")
	}
	replacement, ok := parameters["replacement"].(string)
	if !ok || replacement == "" || !utf8.ValidString(replacement) {
		return errors.New("invalid replacement")
	}
	exact, err := integerParameter(parameters, "exact_runes")
	if err != nil || exact <= prefix+suffix {
		return errors.New("invalid exact rune count")
	}
	characterClass, ok := parameters["character_class"].(string)
	if !ok || characterClass != "ascii_digit" {
		return errors.New("invalid character class")
	}
	return nil
}

func integerParameter(parameters map[string]any, key string) (int, error) {
	value, exists := parameters[key]
	if !exists {
		return 0, fmt.Errorf("missing %s", key)
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		integer := int(typed)
		if float64(integer) != typed {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		return integer, nil
	case json.Number:
		integer, err := typed.Int64()
		return int(integer), err
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}
