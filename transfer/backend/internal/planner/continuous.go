package planner

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/addp/common/resourcetree"
)

const (
	RuntimeBoundaryContinuous = "continuous"
	changeTypeKafka           = "kafka"
)

type ContinuousTaskSpec struct {
	Runtime    RuntimeSpec          `json:"runtime"`
	Load       ContinuousLoadSpec   `json:"load"`
	Source     ContinuousSourceSpec `json:"source"`
	Target     EndpointSpec         `json:"target"`
	Transforms []TransformSpec      `json:"transforms"`
}

type ContinuousLoadSpec struct {
	Mode            string                    `json:"mode"`
	ChangeDetection ContinuousChangeDetection `json:"change_detection"`
}

type ContinuousChangeDetection struct {
	Type string `json:"type"`
}

type ContinuousSourceSpec struct {
	Locator        string           `json:"locator"`
	Representation string           `json:"representation"`
	ChangeStream   ChangeStreamSpec `json:"change_stream"`
}

type ChangeStreamSpec struct {
	Envelope      string                `json:"envelope"`
	Encoding      string                `json:"encoding"`
	Key           ChangeStreamKeySpec   `json:"key"`
	Start         ChangeStreamStartSpec `json:"start"`
	PollBatchSize int                   `json:"poll_batch_size"`
}

type ChangeStreamKeySpec struct {
	Source string   `json:"source"`
	Fields []string `json:"fields"`
}

type ChangeStreamStartSpec struct {
	Mode    string `json:"mode"`
	Initial string `json:"initial"`
}

func TaskRuntimeBoundary(config map[string]interface{}) (string, error) {
	rawRuntime, ok := config["runtime"]
	if !ok {
		return "", fmt.Errorf("runtime is required")
	}
	runtime, ok := rawRuntime.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("runtime must be an object")
	}
	boundary, ok := runtime["boundary"].(string)
	if !ok || strings.TrimSpace(boundary) == "" {
		return "", fmt.Errorf("runtime.boundary is required")
	}
	return strings.TrimSpace(boundary), nil
}

func ParseContinuousTaskSpec(config map[string]interface{}) (ContinuousTaskSpec, error) {
	if config == nil {
		return ContinuousTaskSpec{}, fmt.Errorf("transfer task config is required")
	}
	if hasLegacyTaskConfigFields(config) {
		return ContinuousTaskSpec{}, fmt.Errorf("legacy transfer task config is not supported")
	}
	if hasUnsupportedEndpointAttributes(config) {
		return ContinuousTaskSpec{}, fmt.Errorf("endpoint attributes are not supported in transfer task config")
	}
	b, err := json.Marshal(config)
	if err != nil {
		return ContinuousTaskSpec{}, fmt.Errorf("marshal continuous task config: %w", err)
	}
	var spec ContinuousTaskSpec
	if err := decodeStrictTaskConfig(b, &spec, "continuous transfer"); err != nil {
		return ContinuousTaskSpec{}, err
	}
	if err := validateContinuousTaskSpec(spec); err != nil {
		return ContinuousTaskSpec{}, err
	}
	return spec, nil
}

func validateContinuousTaskSpec(spec ContinuousTaskSpec) error {
	if spec.Runtime.Boundary != RuntimeBoundaryContinuous {
		return fmt.Errorf("runtime.boundary must be %q", RuntimeBoundaryContinuous)
	}
	if spec.Load.Mode != loadModeIncremental || spec.Load.ChangeDetection.Type != changeTypeKafka {
		return fmt.Errorf("continuous load must use incremental kafka change detection")
	}
	sourceLocator, err := resourcetree.ParseURI(strings.TrimSpace(spec.Source.Locator))
	if err != nil || sourceLocator.Type != resourcetree.ResourceType("topic") {
		return fmt.Errorf("continuous source.locator must identify a topic")
	}
	if spec.Source.Representation != representationNative {
		return fmt.Errorf("continuous source.representation must be native")
	}
	stream := spec.Source.ChangeStream
	if stream.Envelope != "record" || stream.Encoding != "json" {
		return fmt.Errorf("continuous source.change_stream must use record/json")
	}
	if stream.Key.Source != "value" || len(stream.Key.Fields) == 0 {
		return fmt.Errorf("continuous source.change_stream.key must use value fields")
	}
	if stream.Start.Mode != "committed" || (stream.Start.Initial != "earliest" && stream.Start.Initial != "latest") {
		return fmt.Errorf("continuous source.change_stream.start must use committed with earliest or latest initial position")
	}
	if stream.PollBatchSize <= 0 {
		return fmt.Errorf("continuous source.change_stream.poll_batch_size must be greater than zero")
	}
	parent, err := spec.Target.ParentResourceLocator()
	if err != nil || parent.Type != resourcetree.TypeSchema {
		return fmt.Errorf("continuous target.parent_locator must identify a schema")
	}
	if strings.TrimSpace(spec.Target.Name) == "" || spec.Target.DataType != dataTypeTable || spec.Target.Representation != representationNative {
		return fmt.Errorf("continuous target must be a named native table")
	}
	if policyString(spec.Target.Policy, "apply_mode") != applyModeUpsert {
		return fmt.Errorf("continuous target.policy.apply_mode must be upsert")
	}
	keys, err := policyStringSlice(spec.Target.Policy, "keys")
	if err != nil || len(keys) == 0 {
		return fmt.Errorf("continuous target.policy.keys must be a non-empty string array")
	}
	mappedKeys, err := mappedContinuousKeys(stream.Key.Fields, spec.Transforms)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(mappedKeys, keys) {
		return fmt.Errorf("continuous source key fields must map one-to-one to target policy keys")
	}
	if _, _, err := buildContinuousFields(spec.Transforms[0].Fields); err != nil {
		return err
	}
	return nil
}

func mappedContinuousKeys(sourceKeys []string, transforms []TransformSpec) ([]string, error) {
	if len(transforms) != 1 || transforms[0].Type != "field_mapping" || transforms[0].Version != "v1" || transforms[0].Mode != "project" {
		return nil, fmt.Errorf("continuous tasks require one field_mapping v1 project transform")
	}
	mapping := make(map[string]string, len(transforms[0].Fields))
	for _, field := range transforms[0].Fields {
		if strings.TrimSpace(field.Source) == "" || strings.TrimSpace(field.Target) == "" {
			return nil, fmt.Errorf("continuous field mappings require source and target")
		}
		mapping[field.Source] = field.Target
	}
	result := make([]string, 0, len(sourceKeys))
	for _, sourceKey := range sourceKeys {
		targetKey, ok := mapping[sourceKey]
		if !ok {
			return nil, fmt.Errorf("continuous source key %q is not mapped", sourceKey)
		}
		result = append(result, targetKey)
	}
	return result, nil
}

func policyString(policy map[string]interface{}, key string) string {
	value, _ := policy[key].(string)
	return strings.TrimSpace(value)
}

func policyStringSlice(policy map[string]interface{}, key string) ([]string, error) {
	raw, ok := policy[key]
	if !ok {
		return nil, fmt.Errorf("missing %s", key)
	}
	items, ok := raw.([]interface{})
	if !ok {
		if stringsValue, ok := raw.([]string); ok {
			return stringsValue, nil
		}
		return nil, fmt.Errorf("%s must be an array", key)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s must contain strings", key)
		}
		result = append(result, value)
	}
	return result, nil
}
