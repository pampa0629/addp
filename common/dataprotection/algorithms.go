package dataprotection

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/emmansun/gmsm/sm3"
)

const (
	AlgorithmConstantV1 = "addp.mask.constant/v1"
	AlgorithmSM3V1      = "addp.mask.sm3/v1"
)

// Algorithm describes trusted structured-value code, never tenant executable code.
type Algorithm struct {
	Key                 string   `json:"key"`
	NameI18nKey         string   `json:"name_i18n_key"`
	DescriptionI18nKey  string   `json:"description_i18n_key"`
	SupportedFieldTypes []string `json:"supported_field_types"`
	Parameters          []string `json:"parameters"`
	OutputType          string   `json:"output_type"`
}

func ListAlgorithms() []Algorithm {
	return []Algorithm{
		{AlgorithmKeepPrefixSuffixV2, "security.algorithms.prefix.name", "security.algorithms.prefix.description", []string{"string"}, []string{"prefix_runes", "suffix_runes", "mask_rune"}, "string"},
		{AlgorithmConstantV1, "security.algorithms.constant.name", "security.algorithms.constant.description", []string{"string", "int", "bigint", "float", "double", "bool"}, []string{"value"}, "same_as_input"},
		{AlgorithmSM3V1, "security.algorithms.sm3.name", "security.algorithms.sm3.description", []string{"string"}, []string{}, "string"},
	}
}

func StructuredAlgorithm(key string) (Algorithm, bool) {
	for _, algorithm := range ListAlgorithms() {
		if algorithm.Key == key {
			return algorithm, true
		}
	}
	return Algorithm{}, false
}

// ValidateStructuredDecision is shared by the compiler and data owners.
func ValidateStructuredDecision(decision Decision, fieldType string) error {
	if err := decision.Validate(); err != nil {
		return err
	}
	if decision.Effect != EffectMask {
		return nil
	}
	algorithm, ok := StructuredAlgorithm(decision.Algorithm)
	if !ok || !slices.Contains(algorithm.SupportedFieldTypes, fieldType) {
		return errors.New("algorithm does not support field type")
	}
	if decision.Algorithm == AlgorithmConstantV1 && !scalarMatchesType(decision.Parameters["value"], fieldType) {
		return errors.New("replacement type does not match field type")
	}
	return nil
}

func (d Decision) Validate() error { return d.validate() }

func validateConstantParameters(parameters map[string]any) error {
	if len(parameters) != 1 {
		return errors.New("invalid constant parameters")
	}
	value, ok := parameters["value"]
	if !ok {
		return errors.New("missing constant value")
	}
	if text, ok := value.(string); ok {
		if !utf8.ValidString(text) || len(text) > 4096 {
			return errors.New("invalid constant text")
		}
		return nil
	}
	if _, ok := value.(bool); ok {
		return nil
	}
	if _, ok := numericValue(value); ok {
		return nil
	}
	return errors.New("constant must be a finite scalar")
}

func numericValue(value any) (float64, bool) {
	var number float64
	switch v := value.(type) {
	case int:
		number = float64(v)
	case int8:
		number = float64(v)
	case int16:
		number = float64(v)
	case int32:
		number = float64(v)
	case int64:
		number = float64(v)
	case uint:
		number = float64(v)
	case uint32:
		number = float64(v)
	case uint64:
		number = float64(v)
	case float32:
		number = float64(v)
	case float64:
		number = v
	case json.Number:
		var err error
		number, err = v.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func scalarMatchesType(value any, fieldType string) bool {
	switch datatype.FieldType(fieldType) {
	case datatype.FieldTypeString:
		text, ok := value.(string)
		return ok && utf8.ValidString(text)
	case datatype.FieldTypeBool:
		_, ok := value.(bool)
		return ok
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		switch v := value.(type) {
		case int, int8, int16, int32, int64, uint, uint32, uint64:
			return true
		case json.Number:
			_, err := v.Int64()
			return err == nil
		default:
			number, ok := numericValue(value)
			return ok && math.Trunc(number) == number && number >= -9007199254740991 && number <= 9007199254740991
		}
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble:
		_, ok := numericValue(value)
		return ok
	default:
		return false
	}
}

func executeStructuredAlgorithm(value any, decision Decision) (any, error) {
	switch decision.Algorithm {
	case AlgorithmConstantV1:
		return decision.Parameters["value"], nil
	case AlgorithmKeepPrefixSuffixV2, AlgorithmSM3V1:
		text, ok := value.(string)
		if !ok || !utf8.ValidString(text) {
			return nil, errors.New("invalid text value")
		}
		if decision.Algorithm == AlgorithmKeepPrefixSuffixV2 {
			return maskKeepPrefixSuffixV2(text, decision.Parameters)
		}
		sum := sm3.Sum([]byte(text))
		return hex.EncodeToString(sum[:]), nil
	default:
		return nil, errors.New("unsupported structured algorithm")
	}
}
