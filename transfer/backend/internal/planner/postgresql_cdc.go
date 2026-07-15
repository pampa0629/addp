package planner

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/resourcetree"
)

const changeTypeCDC = "cdc"

// PostgreSQLCDCTaskSpec 是 PostgreSQL CDC v1 的内部控制面配置。
// connector、slot、publication、Infra Kafka topic 等运行时资源不属于公开任务 JSON。
type PostgreSQLCDCTaskSpec struct {
	Runtime    RuntimeSpec           `json:"runtime"`
	Load       PostgreSQLCDCLoadSpec `json:"load"`
	Source     EndpointSpec          `json:"source"`
	Target     EndpointSpec          `json:"target"`
	Transforms []TransformSpec       `json:"transforms"`
}

type PostgreSQLCDCLoadSpec struct {
	Mode            string                       `json:"mode"`
	ChangeDetection PostgreSQLCDCChangeDetection `json:"change_detection"`
}

type PostgreSQLCDCChangeDetection struct {
	Type      string `json:"type"`
	Bootstrap string `json:"bootstrap"`
}

// ParsePostgreSQLCDCTaskSpec 解析并严格校验已冻结的 PostgreSQL CDC v1 公开配置。
// 当前公开任务创建仍由 service 层保持关闭；该解析器供 capture control plane 和后续 3D 数据面复用。
func ParsePostgreSQLCDCTaskSpec(config map[string]interface{}) (PostgreSQLCDCTaskSpec, error) {
	if config == nil {
		return PostgreSQLCDCTaskSpec{}, fmt.Errorf("transfer task config is required")
	}
	if hasLegacyTaskConfigFields(config) {
		return PostgreSQLCDCTaskSpec{}, fmt.Errorf("legacy transfer task config is not supported")
	}
	if hasUnsupportedEndpointAttributes(config) {
		return PostgreSQLCDCTaskSpec{}, fmt.Errorf("endpoint attributes are not supported in transfer task config")
	}
	b, err := json.Marshal(config)
	if err != nil {
		return PostgreSQLCDCTaskSpec{}, fmt.Errorf("marshal PostgreSQL CDC task config: %w", err)
	}
	var spec PostgreSQLCDCTaskSpec
	if err := decodeStrictTaskConfig(b, &spec, "PostgreSQL CDC transfer"); err != nil {
		return PostgreSQLCDCTaskSpec{}, err
	}
	if err := validatePostgreSQLCDCTaskSpec(spec); err != nil {
		return PostgreSQLCDCTaskSpec{}, err
	}
	return spec, nil
}

func validatePostgreSQLCDCTaskSpec(spec PostgreSQLCDCTaskSpec) error {
	if spec.Runtime.Boundary != RuntimeBoundaryContinuous {
		return fmt.Errorf("runtime.boundary must be %q", RuntimeBoundaryContinuous)
	}
	if spec.Load.Mode != loadModeIncremental || spec.Load.ChangeDetection.Type != changeTypeCDC {
		return fmt.Errorf("PostgreSQL CDC load must use incremental cdc change detection")
	}
	if spec.Load.ChangeDetection.Bootstrap != "initial_snapshot" {
		return fmt.Errorf("PostgreSQL CDC bootstrap must be initial_snapshot")
	}
	source, err := spec.Source.ResourceLocator()
	if err != nil || source.Type != resourcetree.TypeTable || len(source.Path) != 2 {
		return fmt.Errorf("PostgreSQL CDC source.locator must identify one schema table")
	}
	if spec.Source.DataType != dataTypeTable || spec.Source.Representation != representationNative {
		return fmt.Errorf("PostgreSQL CDC source must be a native table")
	}
	parent, err := spec.Target.ParentResourceLocator()
	if err != nil || parent.Type != resourcetree.TypeSchema || len(parent.Path) != 1 {
		return fmt.Errorf("PostgreSQL CDC target.parent_locator must identify one schema")
	}
	if strings.TrimSpace(spec.Target.Name) == "" || spec.Target.DataType != dataTypeTable || spec.Target.Representation != representationNative {
		return fmt.Errorf("PostgreSQL CDC target must be a named native table")
	}
	if policyString(spec.Target.Policy, "apply_mode") != applyModeUpsertDelete {
		return fmt.Errorf("PostgreSQL CDC target.policy.apply_mode must be upsert_delete")
	}
	keys, err := policyStringSlice(spec.Target.Policy, "keys")
	if err != nil || len(keys) == 0 {
		return fmt.Errorf("PostgreSQL CDC target.policy.keys must be a non-empty string array")
	}
	if len(spec.Transforms) != 1 || spec.Transforms[0].Type != "field_mapping" || spec.Transforms[0].Version != "v1" || spec.Transforms[0].Mode != "project" {
		return fmt.Errorf("PostgreSQL CDC tasks require one field_mapping v1 project transform")
	}
	if len(spec.Transforms[0].Fields) == 0 {
		return fmt.Errorf("PostgreSQL CDC field mapping must not be empty")
	}
	_, fields, err := buildPostgreSQLCDCFields(spec.Transforms[0].Fields)
	if err != nil {
		return err
	}
	mappedTargets := make(map[string]bool, len(spec.Transforms[0].Fields))
	for _, field := range spec.Transforms[0].Fields {
		sourceName := strings.TrimSpace(field.Source)
		targetName := strings.TrimSpace(field.Target)
		if sourceName == "" || targetName == "" {
			return fmt.Errorf("PostgreSQL CDC field mappings require source and target")
		}
		if mappedTargets[targetName] {
			return fmt.Errorf("PostgreSQL CDC target field %q is mapped more than once", targetName)
		}
		mappedTargets[targetName] = true
	}
	for _, key := range keys {
		if !mappedTargets[key] {
			return fmt.Errorf("PostgreSQL CDC target key %q is not mapped", key)
		}
		for _, field := range fields {
			if field.Name == key && field.Type == datatype.FieldTypeGeometry {
				return fmt.Errorf("PostgreSQL CDC target key %q cannot use geometry", key)
			}
		}
	}
	return nil
}

func IsPostgreSQLCDCTaskConfig(config map[string]interface{}) bool {
	_, err := ParsePostgreSQLCDCTaskSpec(config)
	return err == nil
}

// PostgreSQLCDCSourceToTargetKeys 返回 source 字段到 target keys 的一一映射，
// capture 初始化用它校验真实源表主键。
func PostgreSQLCDCSourceToTargetKeys(spec PostgreSQLCDCTaskSpec) ([]string, []string, error) {
	targetKeys, err := policyStringSlice(spec.Target.Policy, "keys")
	if err != nil {
		return nil, nil, err
	}
	targetToSource := make(map[string]string, len(spec.Transforms[0].Fields))
	for _, field := range spec.Transforms[0].Fields {
		targetToSource[strings.TrimSpace(field.Target)] = strings.TrimSpace(field.Source)
	}
	sourceKeys := make([]string, 0, len(targetKeys))
	for _, targetKey := range targetKeys {
		sourceKey, ok := targetToSource[targetKey]
		if !ok {
			return nil, nil, fmt.Errorf("PostgreSQL CDC target key %q is not mapped", targetKey)
		}
		sourceKeys = append(sourceKeys, sourceKey)
	}
	if len(sourceKeys) != len(targetKeys) || reflect.DeepEqual(sourceKeys, []string{}) {
		return nil, nil, fmt.Errorf("PostgreSQL CDC keys are invalid")
	}
	return sourceKeys, targetKeys, nil
}
