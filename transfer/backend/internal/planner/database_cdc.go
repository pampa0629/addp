package planner

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
)

const changeTypeCDC = "cdc"

// DatabaseCDCTaskSpec 是数据库单表 CDC v1 的公开任务语义。
// provider 与 connector 私有资源不属于任务 JSON，必须通过 source Engine 解析。
type DatabaseCDCTaskSpec struct {
	Runtime    ContinuousRuntimeSpec `json:"runtime"`
	Load       DatabaseCDCLoadSpec   `json:"load"`
	Source     EndpointSpec          `json:"source"`
	Target     EndpointSpec          `json:"target"`
	Transforms []TransformSpec       `json:"transforms"`
}

type DatabaseCDCLoadSpec struct {
	Mode            string                     `json:"mode"`
	ChangeDetection DatabaseCDCChangeDetection `json:"change_detection"`
}

type DatabaseCDCChangeDetection struct {
	Type      string `json:"type"`
	Bootstrap string `json:"bootstrap"`
}

// ParseDatabaseCDCTaskSpec 只解析并严格校验数据库 CDC 的通用任务语义。
// source provider 必须由调用方解析 System Engine，不能由该配置形态推断。
func ParseDatabaseCDCTaskSpec(config map[string]interface{}) (DatabaseCDCTaskSpec, error) {
	if config == nil {
		return DatabaseCDCTaskSpec{}, fmt.Errorf("transfer task config is required")
	}
	if hasLegacyTaskConfigFields(config) {
		return DatabaseCDCTaskSpec{}, fmt.Errorf("legacy transfer task config is not supported")
	}
	if hasUnsupportedEndpointAttributes(config) {
		return DatabaseCDCTaskSpec{}, fmt.Errorf("endpoint attributes are not supported in transfer task config")
	}
	b, err := json.Marshal(config)
	if err != nil {
		return DatabaseCDCTaskSpec{}, fmt.Errorf("marshal database CDC task config: %w", err)
	}
	var spec DatabaseCDCTaskSpec
	if err := decodeStrictTaskConfig(b, &spec, "database CDC transfer"); err != nil {
		return DatabaseCDCTaskSpec{}, err
	}
	if err := validateDatabaseCDCTaskSpec(spec); err != nil {
		return DatabaseCDCTaskSpec{}, err
	}
	return spec, nil
}

func validateDatabaseCDCTaskSpec(spec DatabaseCDCTaskSpec) error {
	if spec.Runtime.Boundary != RuntimeBoundaryContinuous {
		return fmt.Errorf("runtime.boundary must be %q", RuntimeBoundaryContinuous)
	}
	if spec.Runtime.RecordFailure.Mode != RecordFailureModeBlock {
		return fmt.Errorf("database CDC runtime.record_failure.mode must be %q", RecordFailureModeBlock)
	}
	if spec.Load.Mode != loadModeIncremental || spec.Load.ChangeDetection.Type != changeTypeCDC {
		return fmt.Errorf("database CDC load must use incremental cdc change detection")
	}
	if spec.Load.ChangeDetection.Bootstrap != "initial_snapshot" {
		return fmt.Errorf("database CDC bootstrap must be initial_snapshot")
	}
	source, err := spec.Source.ResourceLocator()
	if err != nil || source.Type != resourcetree.TypeTable || len(source.Path) != 2 {
		return fmt.Errorf("database CDC source.locator must identify one namespace table")
	}
	if spec.Source.DataType != dataTypeTable || spec.Source.Representation != representationNative {
		return fmt.Errorf("database CDC source must be a native table")
	}
	parent, err := spec.Target.ParentResourceLocator()
	if err != nil || !isNativeTableParentType(parent.Type) || len(parent.Path) != 1 {
		return fmt.Errorf("database CDC target.parent_locator must identify one database or schema")
	}
	if strings.TrimSpace(spec.Target.Name) == "" || spec.Target.DataType != dataTypeTable || spec.Target.Representation != representationNative {
		return fmt.Errorf("database CDC target must be a named native table")
	}
	if policyString(spec.Target.Policy, "apply_mode") != applyModeUpsertDelete {
		return fmt.Errorf("database CDC target.policy.apply_mode must be upsert_delete")
	}
	keys, err := policyStringSlice(spec.Target.Policy, "keys")
	if err != nil || len(keys) == 0 {
		return fmt.Errorf("database CDC target.policy.keys must be a non-empty string array")
	}
	if len(spec.Transforms) != 1 || spec.Transforms[0].Type != "field_mapping" || spec.Transforms[0].Version != "v1" || spec.Transforms[0].Mode != "project" {
		return fmt.Errorf("database CDC tasks require one field_mapping v1 project transform")
	}
	if len(spec.Transforms[0].Fields) == 0 {
		return fmt.Errorf("database CDC field mapping must not be empty")
	}
	_, fields, err := buildDatabaseCDCFields(spec.Transforms[0].Fields)
	if err != nil {
		return err
	}
	mappedTargets := make(map[string]bool, len(spec.Transforms[0].Fields))
	for _, field := range spec.Transforms[0].Fields {
		sourceName := strings.TrimSpace(field.Source)
		targetName := strings.TrimSpace(field.Target)
		if sourceName == "" || targetName == "" {
			return fmt.Errorf("database CDC field mappings require source and target")
		}
		if mappedTargets[targetName] {
			return fmt.Errorf("database CDC target field %q is mapped more than once", targetName)
		}
		mappedTargets[targetName] = true
	}
	for _, key := range keys {
		if !mappedTargets[key] {
			return fmt.Errorf("database CDC target key %q is not mapped", key)
		}
		for _, field := range fields {
			if field.Name == key && field.Type == datatype.FieldTypeGeometry {
				return fmt.Errorf("database CDC target key %q cannot use geometry", key)
			}
		}
	}
	return nil
}

func IsDatabaseCDCTaskConfig(config map[string]interface{}) bool {
	_, err := ParseDatabaseCDCTaskSpec(config)
	return err == nil
}

type DatabaseCDCBindings struct {
	Source     EngineBinding
	Target     EngineBinding
	SourceType string
	TargetType string
}

// ResolveDatabaseCDCBindings 是数据库 CDC provider 判定的唯一入口。
func ResolveDatabaseCDCBindings(spec DatabaseCDCTaskSpec, resolver EngineResolver) (DatabaseCDCBindings, error) {
	if resolver == nil {
		return DatabaseCDCBindings{}, fmt.Errorf("database CDC requires engine resolver")
	}
	sourceRef, err := spec.Source.EngineRef()
	if err != nil {
		return DatabaseCDCBindings{}, err
	}
	targetRef, err := spec.Target.EngineRef()
	if err != nil {
		return DatabaseCDCBindings{}, err
	}
	source, err := resolver.ResolveEngine(sourceRef)
	if err != nil {
		return DatabaseCDCBindings{}, fmt.Errorf("resolve database CDC source engine: %w", err)
	}
	target, err := resolver.ResolveEngine(targetRef)
	if err != nil {
		return DatabaseCDCBindings{}, fmt.Errorf("resolve database CDC target engine: %w", err)
	}
	sourceType := strings.ToLower(strings.TrimSpace(effectiveEngineType(source, sourceRef)))
	targetType := strings.ToLower(strings.TrimSpace(effectiveEngineType(target, targetRef)))
	if sourceType != "postgresql" && sourceType != "mysql" && sourceType != "oracle" {
		return DatabaseCDCBindings{}, fmt.Errorf("database CDC v1 does not support source engine type %q", sourceType)
	}
	if !declaresPartitionedMonotonicApply(target, engineplugin.TableChangeOperationUpsert, engineplugin.TableChangeOperationDelete) {
		return DatabaseCDCBindings{}, fmt.Errorf("database CDC target engine %q does not declare atomic monotonic upsert/delete partitioned_table_change_apply", targetType)
	}
	return DatabaseCDCBindings{Source: source, Target: target, SourceType: sourceType, TargetType: targetType}, nil
}

// DatabaseCDCSourceToTargetKeys 返回 source 字段到 target keys 的一一映射，
// capture 初始化用它校验真实源表主键。
func DatabaseCDCSourceToTargetKeys(spec DatabaseCDCTaskSpec) ([]string, []string, error) {
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
			return nil, nil, fmt.Errorf("database CDC target key %q is not mapped", targetKey)
		}
		sourceKeys = append(sourceKeys, sourceKey)
	}
	if len(sourceKeys) != len(targetKeys) || reflect.DeepEqual(sourceKeys, []string{}) {
		return nil, nil, fmt.Errorf("database CDC keys are invalid")
	}
	return sourceKeys, targetKeys, nil
}
