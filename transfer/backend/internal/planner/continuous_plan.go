package planner

import (
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
)

type ContinuousFieldPlan struct {
	Source   string
	Target   string
	Type     datatype.FieldType
	Nullable bool
	Default  interface{}
}

type ContinuousSourcePlan struct {
	ConnInfo        engineplugin.ConnectionInfo
	Path            engineplugin.CatalogPath
	SourceIdentity  string
	InitialPosition string
	PollBatchSize   int
}

type ContinuousTargetPlan struct {
	ConnInfo engineplugin.ConnectionInfo
	Path     engineplugin.CatalogPath
	Fields   []datatype.FieldInfo
	Keys     []string
}

type ContinuousPlan struct {
	Source     ContinuousSourcePlan
	Target     ContinuousTargetPlan
	Mappings   []ContinuousFieldPlan
	SourceKeys []string
	SourceType string
	TargetType string
}

func BuildContinuousPlan(spec ContinuousTaskSpec, resolver EngineResolver) (*ContinuousPlan, error) {
	if resolver == nil {
		return nil, fmt.Errorf("continuous plan requires engine resolver")
	}
	if err := validateContinuousTaskSpec(spec); err != nil {
		return nil, err
	}
	sourceRef, err := continuousSourceEngineRef(spec.Source)
	if err != nil {
		return nil, err
	}
	targetRef, err := spec.Target.EngineRef()
	if err != nil {
		return nil, err
	}
	source, err := resolver.ResolveEngine(sourceRef)
	if err != nil {
		return nil, fmt.Errorf("resolve continuous source engine: %w", err)
	}
	target, err := resolver.ResolveEngine(targetRef)
	if err != nil {
		return nil, fmt.Errorf("resolve continuous target engine: %w", err)
	}
	sourceType := strings.ToLower(strings.TrimSpace(effectiveEngineType(source, sourceRef)))
	targetType := strings.ToLower(strings.TrimSpace(effectiveEngineType(target, targetRef)))
	if sourceType != "kafka" || targetType != "postgresql" {
		return nil, fmt.Errorf("continuous v1 only supports Kafka -> PostgreSQL, got %s -> %s", sourceType, targetType)
	}
	if !declaresChangeStreamRead(source) {
		return nil, fmt.Errorf("source Kafka engine does not declare partitioned seekable change_stream_read")
	}
	if !declaresPartitionedMonotonicApply(target) {
		return nil, fmt.Errorf("target PostgreSQL engine does not declare atomic monotonic partitioned_table_change_apply")
	}
	sourcePlugin, err := engineplugin.Get(sourceType)
	if err != nil {
		return nil, err
	}
	catalogProvider, ok := sourcePlugin.(engineplugin.CatalogModelProvider)
	if !ok {
		return nil, fmt.Errorf("source Kafka engine does not implement CatalogModelProvider")
	}
	sourceLocator, err := resourcetree.ParseURI(strings.TrimSpace(spec.Source.Locator))
	if err != nil {
		return nil, err
	}
	sourcePath, err := resourcetree.ProviderCatalogPathFromLocator(catalogProvider.CatalogModel(), sourceLocator)
	if err != nil {
		return nil, fmt.Errorf("build continuous source path: %w", err)
	}
	targetPath, err := nativeTableTargetPath(spec.Target, target)
	if err != nil {
		return nil, fmt.Errorf("build continuous target path: %w", err)
	}
	mappings, fields, err := buildContinuousFields(spec.Transforms[0].Fields)
	if err != nil {
		return nil, err
	}
	return &ContinuousPlan{
		Source: ContinuousSourcePlan{
			ConnInfo: source.ConnInfo, Path: sourcePath, SourceIdentity: strings.TrimSpace(spec.Source.Locator),
			InitialPosition: spec.Source.ChangeStream.Start.Initial, PollBatchSize: spec.Source.ChangeStream.PollBatchSize,
		},
		Target: ContinuousTargetPlan{
			ConnInfo: target.ConnInfo, Path: targetPath, Fields: fields,
			Keys: append([]string(nil), mustPolicyStrings(spec.Target.Policy, "keys")...),
		},
		Mappings: mappings, SourceKeys: append([]string(nil), spec.Source.ChangeStream.Key.Fields...), SourceType: sourceType, TargetType: targetType,
	}, nil
}

func continuousSourceEngineRef(source ContinuousSourceSpec) (EngineRef, error) {
	loc, err := resourcetree.ParseURI(strings.TrimSpace(source.Locator))
	if err != nil {
		return EngineRef{}, err
	}
	return EngineRef{ID: loc.EngineID}, nil
}

func declaresChangeStreamRead(binding EngineBinding) bool {
	caps := effectiveEngineCapabilities(binding)
	if caps == nil || caps.Storage == nil || caps.Storage.Store == nil {
		return false
	}
	capability := caps.Storage.Store.ChangeStreamRead
	if capability == nil || !capability.Supported || !capability.Partitioned || !capability.Seek || !capability.PauseResume {
		return false
	}
	return containsString(capability.PositionTypes, "kafka_offset/v1")
}

func declaresPartitionedMonotonicApply(binding EngineBinding) bool {
	caps := effectiveEngineCapabilities(binding)
	if caps == nil || caps.Storage == nil || caps.Storage.Store == nil {
		return false
	}
	capability := caps.Storage.Store.PartitionedTableChangeApply
	if capability == nil || !capability.Supported || !capability.AtomicPositionCommit || !capability.Monotonic {
		return false
	}
	return containsString(capability.PositionTypes, "kafka_offset/v1") && containsString(capability.Operations, engineplugin.TableChangeOperationUpsert)
}

func buildContinuousFields(specs []FieldMappingSpec) ([]ContinuousFieldPlan, []datatype.FieldInfo, error) {
	mappings := make([]ContinuousFieldPlan, 0, len(specs))
	fields := make([]datatype.FieldInfo, 0, len(specs))
	seenSource := map[string]bool{}
	seenTarget := map[string]bool{}
	for _, spec := range specs {
		source := strings.TrimSpace(spec.Source)
		target := strings.TrimSpace(spec.Target)
		if seenSource[source] || seenTarget[target] {
			return nil, nil, fmt.Errorf("continuous field mapping source and target names must be unique")
		}
		seenSource[source], seenTarget[target] = true, true
		fieldType := datatype.ParseFieldType(spec.TargetType)
		if fieldType == datatype.FieldTypeUnknown {
			return nil, nil, fmt.Errorf("continuous field %q requires a known target_type", target)
		}
		if !continuousFieldTypeSupported(fieldType) {
			return nil, nil, fmt.Errorf("continuous v1 does not support target_type %q", fieldType)
		}
		if spec.Nullable == nil {
			return nil, nil, fmt.Errorf("continuous field %q requires explicit nullable", target)
		}
		mappings = append(mappings, ContinuousFieldPlan{
			Source: source, Target: target, Type: fieldType, Nullable: *spec.Nullable, Default: spec.Default,
		})
		fields = append(fields, datatype.FieldInfo{Name: target, Type: fieldType, Nullable: *spec.Nullable})
	}
	return mappings, fields, nil
}

func continuousFieldTypeSupported(fieldType datatype.FieldType) bool {
	switch fieldType {
	case datatype.FieldTypeString, datatype.FieldTypeBool,
		datatype.FieldTypeInt, datatype.FieldTypeBigInt,
		datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeDecimal,
		datatype.FieldTypeDate, datatype.FieldTypeTime, datatype.FieldTypeTimestamp,
		datatype.FieldTypeJSON, datatype.FieldTypeUUID:
		return true
	default:
		return false
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func mustPolicyStrings(policy map[string]interface{}, key string) []string {
	values, _ := policyStringSlice(policy, key)
	return values
}
