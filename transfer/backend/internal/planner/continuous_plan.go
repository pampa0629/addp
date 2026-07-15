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

const (
	ContinuousEnvelopeRecord             = "record"
	ContinuousEnvelopePostgreSQLDebezium = "postgresql_debezium"
)

type ContinuousSourcePlan struct {
	ConnInfo        engineplugin.ConnectionInfo
	Path            engineplugin.CatalogPath
	SourceIdentity  string
	ConsumerGroup   string
	InitialPosition string
	PollBatchSize   int
}

type PostgreSQLCDCSourcePlan struct {
	Database    string
	Schema      string
	Table       string
	SpatialInfo *datatype.SpatialInfo
}

type PostgreSQLCDCStreamBinding struct {
	ConnInfo       engineplugin.ConnectionInfo
	Path           engineplugin.CatalogPath
	ConsumerGroup  string
	SourceIdentity string
	Database       string
	Schema         string
	Table          string
	SpatialInfo    *datatype.SpatialInfo
}

type ContinuousTargetPlan struct {
	ConnInfo    engineplugin.ConnectionInfo
	Path        engineplugin.CatalogPath
	Fields      []datatype.FieldInfo
	SpatialInfo *datatype.SpatialInfo
	Keys        []string
}

type ContinuousPlan struct {
	Source     ContinuousSourcePlan
	Target     ContinuousTargetPlan
	Mappings   []ContinuousFieldPlan
	SourceKeys []string
	SourceType string
	TargetType string
	Envelope   string
	CDC        *PostgreSQLCDCSourcePlan
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
	if !declaresPartitionedMonotonicApply(target, engineplugin.TableChangeOperationUpsert) {
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
		Envelope: ContinuousEnvelopeRecord,
	}, nil
}

func BuildPostgreSQLCDCContinuousPlan(spec PostgreSQLCDCTaskSpec, resolver EngineResolver, stream PostgreSQLCDCStreamBinding, pollBatchSize int) (*ContinuousPlan, error) {
	if resolver == nil {
		return nil, fmt.Errorf("PostgreSQL CDC continuous plan requires engine resolver")
	}
	if err := validatePostgreSQLCDCTaskSpec(spec); err != nil {
		return nil, err
	}
	if strings.TrimSpace(stream.SourceIdentity) == "" || strings.TrimSpace(stream.ConsumerGroup) == "" ||
		strings.TrimSpace(stream.Database) == "" || strings.TrimSpace(stream.Schema) == "" || strings.TrimSpace(stream.Table) == "" {
		return nil, fmt.Errorf("PostgreSQL CDC stream binding is incomplete")
	}
	if pollBatchSize <= 0 {
		pollBatchSize = 1000
	}
	sourceRef, err := spec.Source.EngineRef()
	if err != nil {
		return nil, err
	}
	targetRef, err := spec.Target.EngineRef()
	if err != nil {
		return nil, err
	}
	source, err := resolver.ResolveEngine(sourceRef)
	if err != nil {
		return nil, fmt.Errorf("resolve PostgreSQL CDC source engine: %w", err)
	}
	target, err := resolver.ResolveEngine(targetRef)
	if err != nil {
		return nil, fmt.Errorf("resolve PostgreSQL CDC target engine: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(effectiveEngineType(source, sourceRef))) != "postgresql" ||
		strings.ToLower(strings.TrimSpace(effectiveEngineType(target, targetRef))) != "postgresql" {
		return nil, fmt.Errorf("PostgreSQL CDC v1 requires PostgreSQL source and target")
	}
	if !declaresPartitionedMonotonicApply(target, engineplugin.TableChangeOperationUpsert, engineplugin.TableChangeOperationDelete) {
		return nil, fmt.Errorf("target PostgreSQL engine does not declare atomic monotonic upsert/delete partitioned_table_change_apply")
	}
	sourceLocator, _ := spec.Source.ResourceLocator()
	sourceDatabase := strings.TrimSpace(engineplugin.GetString(source.ConnInfo, "database"))
	if stream.SourceIdentity != strings.TrimSpace(spec.Source.Locator) || stream.Database != sourceDatabase ||
		stream.Schema != sourceLocator.Path[0] || stream.Table != sourceLocator.Path[1] {
		return nil, fmt.Errorf("PostgreSQL CDC stream binding does not match registered source identity")
	}
	targetPath, err := nativeTableTargetPath(spec.Target, target)
	if err != nil {
		return nil, fmt.Errorf("build PostgreSQL CDC target path: %w", err)
	}
	mappings, fields, err := buildPostgreSQLCDCFields(spec.Transforms[0].Fields)
	if err != nil {
		return nil, err
	}
	sourceKeys, targetKeys, err := PostgreSQLCDCSourceToTargetKeys(spec)
	if err != nil {
		return nil, err
	}
	targetSpatialInfo, err := mapPostgreSQLCDCSpatialInfo(stream.SpatialInfo, mappings)
	if err != nil {
		return nil, err
	}
	return &ContinuousPlan{
		Source: ContinuousSourcePlan{
			ConnInfo: stream.ConnInfo, Path: stream.Path, SourceIdentity: stream.SourceIdentity,
			ConsumerGroup: stream.ConsumerGroup, InitialPosition: engineplugin.ChangeStreamInitialEarliest,
			PollBatchSize: pollBatchSize,
		},
		Target:   ContinuousTargetPlan{ConnInfo: target.ConnInfo, Path: targetPath, Fields: fields, SpatialInfo: targetSpatialInfo, Keys: targetKeys},
		Mappings: mappings, SourceKeys: sourceKeys, SourceType: "kafka", TargetType: "postgresql",
		Envelope: ContinuousEnvelopePostgreSQLDebezium,
		CDC:      &PostgreSQLCDCSourcePlan{Database: stream.Database, Schema: stream.Schema, Table: stream.Table, SpatialInfo: stream.SpatialInfo.Clone()},
	}, nil
}

func mapPostgreSQLCDCSpatialInfo(source *datatype.SpatialInfo, mappings []ContinuousFieldPlan) (*datatype.SpatialInfo, error) {
	geometryMappings := make(map[string]ContinuousFieldPlan)
	for _, mapping := range mappings {
		if mapping.Type == datatype.FieldTypeGeometry {
			geometryMappings[mapping.Source] = mapping
		}
	}
	if len(geometryMappings) == 0 {
		if source != nil && len(source.GeometryColumns) > 0 {
			return nil, fmt.Errorf("PostgreSQL CDC capture contains spatial facts but task has no geometry mapping")
		}
		return nil, nil
	}
	if source == nil || len(source.GeometryColumns) == 0 {
		return nil, fmt.Errorf("PostgreSQL CDC geometry mappings require frozen capture spatial facts")
	}
	columns := make([]datatype.GeometryColumnInfo, 0, len(source.GeometryColumns))
	seen := make(map[string]bool)
	for _, sourceColumn := range source.GeometryColumns {
		mapping, ok := geometryMappings[sourceColumn.Name]
		if !ok {
			return nil, fmt.Errorf("PostgreSQL CDC spatial source field %q is not mapped as geometry", sourceColumn.Name)
		}
		column := sourceColumn
		column.Name = mapping.Target
		column.Nullable = boolPtr(mapping.Nullable)
		columns = append(columns, column)
		seen[mapping.Source] = true
	}
	for sourceName := range geometryMappings {
		if !seen[sourceName] {
			return nil, fmt.Errorf("PostgreSQL CDC geometry field %q has no frozen source spatial fact", sourceName)
		}
	}
	result := &datatype.SpatialInfo{GeometryColumns: columns, PrimaryGeometryColumn: columns[0].Name}
	if len(columns) == 1 {
		result.SRID = columns[0].SRID
		result.CRSRef = columns[0].CRSRef
	}
	return result, nil
}

func boolPtr(value bool) *bool { return &value }

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

func declaresPartitionedMonotonicApply(binding EngineBinding, operations ...string) bool {
	caps := effectiveEngineCapabilities(binding)
	if caps == nil || caps.Storage == nil || caps.Storage.Store == nil {
		return false
	}
	capability := caps.Storage.Store.PartitionedTableChangeApply
	if capability == nil || !capability.Supported || !capability.AtomicPositionCommit || !capability.Monotonic {
		return false
	}
	if !containsString(capability.PositionTypes, "kafka_offset/v1") {
		return false
	}
	for _, operation := range operations {
		if !containsString(capability.Operations, operation) {
			return false
		}
	}
	return true
}

func buildContinuousFields(specs []FieldMappingSpec) ([]ContinuousFieldPlan, []datatype.FieldInfo, error) {
	return buildContinuousFieldsWithTypeSupport(specs, ContinuousFieldTypeSupported)
}

func buildPostgreSQLCDCFields(specs []FieldMappingSpec) ([]ContinuousFieldPlan, []datatype.FieldInfo, error) {
	return buildContinuousFieldsWithTypeSupport(specs, PostgreSQLCDCFieldTypeSupported)
}

func buildContinuousFieldsWithTypeSupport(specs []FieldMappingSpec, supported func(datatype.FieldType) bool) ([]ContinuousFieldPlan, []datatype.FieldInfo, error) {
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
		if !supported(fieldType) {
			return nil, nil, fmt.Errorf("continuous v1 does not support target_type %q", fieldType)
		}
		if spec.Nullable == nil {
			return nil, nil, fmt.Errorf("continuous field %q requires explicit nullable", target)
		}
		if strings.TrimSpace(spec.Format) != "" {
			return nil, nil, fmt.Errorf("continuous field %q does not support format conversion", target)
		}
		mappings = append(mappings, ContinuousFieldPlan{
			Source: source, Target: target, Type: fieldType, Nullable: *spec.Nullable, Default: spec.Default,
		})
		fields = append(fields, datatype.FieldInfo{Name: target, Type: fieldType, Nullable: *spec.Nullable})
	}
	return mappings, fields, nil
}

// PostgreSQLCDCFieldTypeSupported extends the scalar continuous contract with
// ADDP's canonical EWKB + SpatialInfo geometry representation.
func PostgreSQLCDCFieldTypeSupported(fieldType datatype.FieldType) bool {
	return ContinuousFieldTypeSupported(fieldType) || fieldType == datatype.FieldTypeGeometry
}

// ContinuousFieldTypeSupported 返回 continuous v1 数据面可无歧义应用的字段类型。
func ContinuousFieldTypeSupported(fieldType datatype.FieldType) bool {
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
