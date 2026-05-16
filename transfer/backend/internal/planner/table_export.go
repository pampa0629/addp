package planner

import (
	"encoding/json"
	"fmt"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/transfer/internal/executor"
)

const (
	modeBatch               = "batch"
	dataTypeTable           = "table"
	representationNative    = "native"
	representationEncoded   = "encoded"
	resourceKindNativeTable = "native_table"
	resourceKindFile        = "file"
	resourceKindObject      = "object"
	defaultWriteMode        = "overwrite"
)

type EngineRef struct {
	Scope string `json:"scope"`
	ID    uint   `json:"id"`
	Type  string `json:"type,omitempty"`
}

type ResourceSpec struct {
	Kind string      `json:"kind"`
	Path interface{} `json:"path"`
}

type EndpointSpec struct {
	Engine         EngineRef              `json:"engine"`
	Resource       ResourceSpec           `json:"resource"`
	DataType       string                 `json:"data_type"`
	Representation string                 `json:"representation"`
	Format         format.FormatType      `json:"format,omitempty"`
	Options        map[string]interface{} `json:"options,omitempty"`
	Policy         map[string]interface{} `json:"policy,omitempty"`
}

type TableExportTaskSpec struct {
	Mode      string       `json:"mode"`
	Source    EndpointSpec `json:"source"`
	Target    EndpointSpec `json:"target"`
	BatchSize int          `json:"batch_size,omitempty"`
}

type EngineBinding struct {
	Type       string
	ConnInfo   engineplugin.ConnectionInfo
	EngineID   uint
	PluginType string
}

type EngineResolver interface {
	ResolveEngine(ref EngineRef) (EngineBinding, error)
}

type StaticEngineResolver map[uint]EngineBinding

func (r StaticEngineResolver) ResolveEngine(ref EngineRef) (EngineBinding, error) {
	binding, ok := r[ref.ID]
	if !ok {
		return EngineBinding{}, fmt.Errorf("engine %d not found", ref.ID)
	}
	if binding.Type == "" {
		binding.Type = ref.Type
	}
	if binding.EngineID == 0 {
		binding.EngineID = ref.ID
	}
	return binding, nil
}

type TableExportBuildResult struct {
	SourceEngineType string
	TargetEngineType string
	Format           format.FormatType
	Plan             executor.TableExportPlan
}

func ParseTableExportTaskSpec(config map[string]interface{}, fallbackBatchSize int) (TableExportTaskSpec, error) {
	if config == nil {
		return TableExportTaskSpec{}, fmt.Errorf("transfer task config is required")
	}
	if hasLegacyTaskConfigFields(config) {
		return TableExportTaskSpec{}, fmt.Errorf("legacy transfer task config is not supported; use source/target endpoint config")
	}

	var spec TableExportTaskSpec
	configBytes, err := json.Marshal(config)
	if err != nil {
		return TableExportTaskSpec{}, fmt.Errorf("marshal transfer task config: %w", err)
	}
	if err := json.Unmarshal(configBytes, &spec); err != nil {
		return TableExportTaskSpec{}, fmt.Errorf("parse transfer task config: %w", err)
	}
	if spec.BatchSize <= 0 {
		spec.BatchSize = fallbackBatchSize
	}
	if err := validateTableExportSpec(spec); err != nil {
		return TableExportTaskSpec{}, err
	}
	return spec, nil
}

func BuildTableExportPlan(spec TableExportTaskSpec, resolver EngineResolver) (*TableExportBuildResult, error) {
	if resolver == nil {
		return nil, fmt.Errorf("engine resolver is required")
	}
	if err := validateTableExportSpec(spec); err != nil {
		return nil, err
	}

	sourceEngine, err := resolver.ResolveEngine(spec.Source.Engine)
	if err != nil {
		return nil, fmt.Errorf("resolve source engine: %w", err)
	}
	targetEngine, err := resolver.ResolveEngine(spec.Target.Engine)
	if err != nil {
		return nil, fmt.Errorf("resolve target engine: %w", err)
	}
	sourceType := effectiveEngineType(sourceEngine, spec.Source.Engine)
	targetType := effectiveEngineType(targetEngine, spec.Target.Engine)
	if sourceType == "" {
		return nil, fmt.Errorf("source engine type is required")
	}
	if targetType == "" {
		return nil, fmt.Errorf("target engine type is required")
	}

	sourcePath, err := nativeTablePath(sourceEngine.EngineID, spec.Source.Resource.Path)
	if err != nil {
		return nil, fmt.Errorf("build source path: %w", err)
	}
	targetPath, err := targetContentPath(targetEngine.EngineID, spec.Target.Resource)
	if err != nil {
		return nil, fmt.Errorf("build target path: %w", err)
	}

	formatType := spec.Target.Format
	if formatType == "" {
		return nil, fmt.Errorf("target format is required")
	}
	descriptor, ok := format.GetFormatDescriptor(formatType)
	if !ok {
		return nil, fmt.Errorf("format %q is not registered", formatType)
	}
	if descriptor.DataType != format.FormatDataTypeTable {
		return nil, fmt.Errorf("format %q data type is %q, want table", formatType, descriptor.DataType)
	}
	if !descriptor.TransferWrite {
		return nil, fmt.Errorf("format %q is not declared as transfer writable", formatType)
	}
	if _, err := format.GetTableWriterProvider(formatType); err != nil {
		return nil, fmt.Errorf("format %q has no table writer provider: %w", formatType, err)
	}

	writeOptions := tableWriteOptions(spec.Target.Options, formatType)

	return &TableExportBuildResult{
		SourceEngineType: sourceType,
		TargetEngineType: targetType,
		Format:           formatType,
		Plan: executor.TableExportPlan{
			SourceConnInfo: sourceEngine.ConnInfo,
			SourcePath:     sourcePath,
			TargetConnInfo: targetEngine.ConnInfo,
			TargetPath:     targetPath,
			TargetWrite: engineplugin.WriteOptions{
				Overwrite: writeMode(spec.Target.Policy) == defaultWriteMode,
			},
			Format:       formatType,
			BatchSize:    spec.BatchSize,
			WriteOptions: writeOptions,
		},
	}, nil
}

func validateTableExportSpec(spec TableExportTaskSpec) error {
	if spec.Mode == "" {
		return fmt.Errorf("transfer task mode is required")
	}
	if spec.Mode != modeBatch {
		return fmt.Errorf("only batch mode is supported by table export planner, got %q", spec.Mode)
	}
	if err := validateEndpoint(spec.Source, "source", representationNative, dataTypeTable); err != nil {
		return err
	}
	if spec.Source.Resource.Kind != resourceKindNativeTable {
		return fmt.Errorf("source resource kind must be %q, got %q", resourceKindNativeTable, spec.Source.Resource.Kind)
	}
	if err := validateEndpoint(spec.Target, "target", representationEncoded, dataTypeTable); err != nil {
		return err
	}
	if spec.Target.Resource.Kind != resourceKindFile && spec.Target.Resource.Kind != resourceKindObject {
		return fmt.Errorf("target resource kind must be %q or %q, got %q", resourceKindFile, resourceKindObject, spec.Target.Resource.Kind)
	}
	return nil
}

func validateEndpoint(endpoint EndpointSpec, role, representation, dataType string) error {
	if endpoint.Engine.ID == 0 {
		return fmt.Errorf("%s engine id is required", role)
	}
	if endpoint.Engine.Scope != "system" {
		return fmt.Errorf("%s engine scope must be %q, got %q", role, "system", endpoint.Engine.Scope)
	}
	if endpoint.DataType != dataType {
		return fmt.Errorf("%s data type must be %q, got %q", role, dataType, endpoint.DataType)
	}
	if endpoint.Representation != representation {
		return fmt.Errorf("%s representation must be %q, got %q", role, representation, endpoint.Representation)
	}
	return nil
}

func hasLegacyTaskConfigFields(config map[string]interface{}) bool {
	legacyKeys := []string{
		"source_config",
		"target_config",
		"connector_type",
		"output_format",
		"file_type",
	}
	for _, key := range legacyKeys {
		if _, ok := config[key]; ok {
			return true
		}
	}
	return endpointHasLegacyFields(config["source"]) || endpointHasLegacyFields(config["target"])
}

func endpointHasLegacyFields(raw interface{}) bool {
	endpoint, ok := raw.(map[string]interface{})
	if !ok {
		return false
	}
	legacyKeys := []string{
		"connector_type",
		"engine_id",
		"output_format",
		"file_type",
		"source_config",
		"target_config",
	}
	for _, key := range legacyKeys {
		if _, ok := endpoint[key]; ok {
			return true
		}
	}
	return false
}

func effectiveEngineType(binding EngineBinding, ref EngineRef) string {
	if binding.Type != "" {
		return binding.Type
	}
	if binding.PluginType != "" {
		return binding.PluginType
	}
	return ref.Type
}

func nativeTablePath(engineID uint, raw interface{}) (engineplugin.CatalogPath, error) {
	values, ok := raw.(map[string]interface{})
	if !ok {
		return engineplugin.CatalogPath{}, fmt.Errorf("native table path requires object value")
	}
	if qualified := stringValue(values, "name"); qualified != "" {
		parts := strings.Split(qualified, ".")
		if len(parts) == 2 {
			values["schema"] = parts[0]
			values["table"] = parts[1]
		}
	}
	schema := stringValue(values, "schema")
	table := stringValue(values, "table")
	if schema == "" || table == "" {
		return engineplugin.CatalogPath{}, fmt.Errorf("native table path requires schema and table")
	}
	return engineplugin.CatalogPath{
		Version:  engineplugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []engineplugin.CatalogSegment{
			{Term: engineplugin.CatalogTermSchema, Kind: engineplugin.CatalogKindNamespace, Name: schema},
			{Term: engineplugin.CatalogTermTable, Kind: engineplugin.CatalogKindTable, Name: table},
		},
	}, nil
}

func targetContentPath(engineID uint, resource ResourceSpec) (engineplugin.CatalogPath, error) {
	switch resource.Kind {
	case resourceKindFile:
		path := contentPathString(resource.Path, "file")
		if path == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("file resource path requires path")
		}
		return engineplugin.FileItemPath(engineID, path), nil
	case resourceKindObject:
		values, _ := resource.Path.(map[string]interface{})
		bucket := stringValue(values, "bucket")
		objectPath := contentPathString(resource.Path, "object")
		if bucket == "" || objectPath == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("object resource path requires bucket and path")
		}
		return engineplugin.ObjectItemPath(engineID, bucket, objectPath), nil
	default:
		return engineplugin.CatalogPath{}, fmt.Errorf("unsupported target resource kind %q", resource.Kind)
	}
}

func writeMode(policy map[string]interface{}) string {
	value := stringValue(policy, "write_mode")
	if value == "" {
		return defaultWriteMode
	}
	return strings.ToLower(value)
}

func tableWriteOptions(raw map[string]interface{}, formatType format.FormatType) *format.WriteOptions {
	opts := format.DefaultWriteOptions()
	if formatType == format.FormatTSV {
		opts.Delimiter = '\t'
	}
	if raw == nil {
		return opts
	}
	if header, ok := raw["header"].(bool); ok {
		opts.OmitHeader = !header
	}
	if delimiter := stringValue(raw, "delimiter"); delimiter != "" {
		runes := []rune(delimiter)
		if len(runes) > 0 {
			opts.Delimiter = runes[0]
		}
	}
	return opts
}

func contentPathString(raw interface{}, alternateKey string) string {
	if raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]interface{}:
		path := stringValue(typed, "path")
		if path == "" {
			path = stringValue(typed, alternateKey)
		}
		return path
	default:
		return strings.TrimSpace(fmt.Sprint(raw))
	}
}

func stringValue(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
