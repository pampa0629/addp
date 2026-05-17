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

type TableImportBuildResult struct {
	SourceEngineType string
	TargetEngineType string
	Format           format.FormatType
	Plan             executor.TableImportPlan
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
	if err := validateTableTransferSpec(spec); err != nil {
		return TableExportTaskSpec{}, err
	}
	return spec, nil
}

func BuildTableExportPlan(spec TableExportTaskSpec, resolver EngineResolver) (*TableExportBuildResult, error) {
	if resolver == nil {
		return nil, fmt.Errorf("engine resolver is required")
	}
	if err := validateTableTransferSpec(spec); err != nil {
		return nil, err
	}
	if !isTableExportSpec(spec) {
		return nil, fmt.Errorf("table export requires native table source and encoded file/object target")
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
		if _, err := format.GetTableWriterProvider(formatType); err != nil {
			return nil, fmt.Errorf("format %q has no table writer provider: %w", formatType, err)
		}
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
			ReadOptions:  readOptionsForTarget(formatType, writeOptions),
		},
	}, nil
}

func BuildTableImportPlan(spec TableExportTaskSpec, resolver EngineResolver) (*TableImportBuildResult, error) {
	if resolver == nil {
		return nil, fmt.Errorf("engine resolver is required")
	}
	if err := validateTableTransferSpec(spec); err != nil {
		return nil, err
	}
	if !isTableImportSpec(spec) {
		return nil, fmt.Errorf("table import requires encoded file/object source and native table target")
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

	sourcePath, err := contentResourcePath(sourceEngine.EngineID, spec.Source.Resource, "source")
	if err != nil {
		return nil, fmt.Errorf("build source path: %w", err)
	}
	targetPath, err := nativeTablePath(targetEngine.EngineID, spec.Target.Resource.Path)
	if err != nil {
		return nil, fmt.Errorf("build target path: %w", err)
	}

	formatType := spec.Source.Format
	if formatType == "" {
		return nil, fmt.Errorf("source format is required")
	}
	descriptor, ok := format.GetFormatDescriptor(formatType)
	if !ok {
		return nil, fmt.Errorf("format %q is not registered", formatType)
	}
	if descriptor.DataType != format.FormatDataTypeTable {
		if _, err := format.GetTableSampleProvider(formatType); err != nil {
			return nil, fmt.Errorf("format %q has no table sample provider: %w", formatType, err)
		}
	}
	if !descriptor.TransferRead {
		return nil, fmt.Errorf("format %q is not declared as transfer readable", formatType)
	}
	if _, err := format.GetTableSampleProvider(formatType); err != nil {
		return nil, fmt.Errorf("format %q has no table sample provider: %w", formatType, err)
	}

	return &TableImportBuildResult{
		SourceEngineType: sourceType,
		TargetEngineType: targetType,
		Format:           formatType,
		Plan: executor.TableImportPlan{
			SourceConnInfo: sourceEngine.ConnInfo,
			SourcePath:     sourcePath,
			TargetConnInfo: targetEngine.ConnInfo,
			TargetPath:     targetPath,
			TargetPrepare: engineplugin.TableWriteOptions{
				Mode: importPrepareMode(spec.Target.Policy),
			},
			TargetWrite: engineplugin.BatchWriteOptions{
				Mode:   importWriteMode(spec.Target.Policy),
				Method: importWriteMethod(spec.Target.Policy, targetType),
			},
			Format:       formatType,
			BatchSize:    spec.BatchSize,
			ParseOptions: tableParseOptions(spec.Source.Options, formatType),
		},
	}, nil
}

func validateTableTransferSpec(spec TableExportTaskSpec) error {
	if spec.Mode == "" {
		return fmt.Errorf("transfer task mode is required")
	}
	if spec.Mode != modeBatch {
		return fmt.Errorf("only batch mode is supported by table transfer planner, got %q", spec.Mode)
	}
	if err := validateEndpointCommon(spec.Source, "source", dataTypeTable); err != nil {
		return err
	}
	if err := validateEndpointCommon(spec.Target, "target", dataTypeTable); err != nil {
		return err
	}
	if isTableExportSpec(spec) || isTableImportSpec(spec) {
		return nil
	}
	return fmt.Errorf("unsupported table transfer shape: source %s/%s -> target %s/%s",
		spec.Source.Representation, spec.Source.Resource.Kind,
		spec.Target.Representation, spec.Target.Resource.Kind)
}

func isTableExportSpec(spec TableExportTaskSpec) bool {
	return spec.Source.Representation == representationNative &&
		spec.Source.Resource.Kind == resourceKindNativeTable &&
		spec.Target.Representation == representationEncoded &&
		isContentResourceKind(spec.Target.Resource.Kind)
}

func isTableImportSpec(spec TableExportTaskSpec) bool {
	return spec.Source.Representation == representationEncoded &&
		isContentResourceKind(spec.Source.Resource.Kind) &&
		spec.Target.Representation == representationNative &&
		spec.Target.Resource.Kind == resourceKindNativeTable
}

func isContentResourceKind(kind string) bool {
	return kind == resourceKindFile || kind == resourceKindObject
}

func validateEndpointCommon(endpoint EndpointSpec, role, dataType string) error {
	if err := validateEndpointIdentity(endpoint, role, dataType); err != nil {
		return err
	}
	switch endpoint.Representation {
	case representationNative:
		if endpoint.Resource.Kind != resourceKindNativeTable {
			return fmt.Errorf("%s native endpoint resource kind must be %q, got %q", role, resourceKindNativeTable, endpoint.Resource.Kind)
		}
	case representationEncoded:
		if !isContentResourceKind(endpoint.Resource.Kind) {
			return fmt.Errorf("%s encoded endpoint resource kind must be %q or %q, got %q", role, resourceKindFile, resourceKindObject, endpoint.Resource.Kind)
		}
	default:
		return fmt.Errorf("%s representation must be %q or %q, got %q", role, representationNative, representationEncoded, endpoint.Representation)
	}
	return nil
}

func validateEndpointIdentity(endpoint EndpointSpec, role, dataType string) error {
	if endpoint.Engine.ID == 0 {
		return fmt.Errorf("%s engine id is required", role)
	}
	if endpoint.Engine.Scope != "system" {
		return fmt.Errorf("%s engine scope must be %q, got %q", role, "system", endpoint.Engine.Scope)
	}
	if endpoint.DataType != dataType {
		return fmt.Errorf("%s data type must be %q, got %q", role, dataType, endpoint.DataType)
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
	return contentResourcePath(engineID, resource, "target")
}

func contentResourcePath(engineID uint, resource ResourceSpec, role string) (engineplugin.CatalogPath, error) {
	switch resource.Kind {
	case resourceKindFile:
		path := contentPathString(resource.Path, "file")
		if path == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("%s file resource path requires path", role)
		}
		return engineplugin.FileItemPath(engineID, path), nil
	case resourceKindObject:
		values, _ := resource.Path.(map[string]interface{})
		bucket := stringValue(values, "bucket")
		objectPath := contentPathString(resource.Path, "object")
		if bucket == "" || objectPath == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("%s object resource path requires bucket and path", role)
		}
		return engineplugin.ObjectItemPath(engineID, bucket, objectPath), nil
	default:
		return engineplugin.CatalogPath{}, fmt.Errorf("unsupported %s resource kind %q", role, resource.Kind)
	}
}

func writeMode(policy map[string]interface{}) string {
	value := stringValue(policy, "write_mode")
	if value == "" {
		return defaultWriteMode
	}
	return strings.ToLower(value)
}

func importWriteMode(policy map[string]interface{}) string {
	value := stringValue(policy, "write_mode")
	if value == "" {
		return "append"
	}
	value = strings.ToLower(value)
	if value == "insert" || value == "truncate_insert" || value == "create_if_not_exists" {
		return "append"
	}
	return value
}

func importWriteMethod(policy map[string]interface{}, targetEngineType string) string {
	value := strings.ToLower(stringValue(policy, "write_method"))
	if value != "" {
		return value
	}
	if targetEngineType == "postgresql" {
		return "copy"
	}
	return ""
}

func importPrepareMode(policy map[string]interface{}) string {
	value := strings.ToLower(stringValue(policy, "write_mode"))
	if value == "truncate_insert" || value == "create_if_not_exists" {
		return value
	}
	return ""
}

func tableWriteOptions(raw map[string]interface{}, formatType format.FormatType) *format.WriteOptions {
	opts := format.DefaultWriteOptions()
	if formatType == format.FormatTSV {
		opts.Delimiter = '\t'
	}
	if raw == nil {
		return opts
	}
	opts.ExtraParams = extraParams(raw, "header", "delimiter")
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

func readOptionsForTarget(formatType format.FormatType, writeOptions *format.WriteOptions) map[string]interface{} {
	if formatType != format.FormatJSON || writeOptions == nil || writeOptions.ExtraParams == nil {
		return nil
	}
	targetEncoding := strings.ToLower(strings.TrimSpace(stringValue(writeOptions.ExtraParams, "spatial.target_encoding")))
	if targetEncoding == "" {
		targetEncoding = nestedStringValue(writeOptions.ExtraParams, "spatial", "target_encoding")
	}
	if targetEncoding != "geojson" {
		return nil
	}
	options := map[string]interface{}{"spatial.target_encoding": "geojson"}
	if geometryField := stringValue(writeOptions.ExtraParams, "geometry_field"); geometryField != "" {
		options["geometry_field"] = geometryField
	}
	return options
}

func tableParseOptions(raw map[string]interface{}, formatType format.FormatType) *format.ParseOptions {
	opts := format.DefaultParseOptions()
	if formatType == format.FormatTSV {
		opts.Delimiter = '\t'
	}
	if raw == nil {
		return opts
	}
	opts.ExtraParams = extraParams(raw, "header", "delimiter")
	if header, ok := raw["header"].(bool); ok {
		opts.HasHeader = header
	}
	if delimiter := stringValue(raw, "delimiter"); delimiter != "" {
		runes := []rune(delimiter)
		if len(runes) > 0 {
			opts.Delimiter = runes[0]
		}
	}
	return opts
}

func extraParams(raw map[string]interface{}, excludedKeys ...string) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	excluded := make(map[string]struct{}, len(excludedKeys))
	for _, key := range excludedKeys {
		excluded[key] = struct{}{}
	}
	params := make(map[string]interface{}, len(raw))
	for key, value := range raw {
		if _, ok := excluded[key]; ok {
			continue
		}
		params[key] = value
	}
	if len(params) == 0 {
		return nil
	}
	return params
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

func nestedStringValue(values map[string]interface{}, parentKey, childKey string) string {
	if values == nil {
		return ""
	}
	switch nested := values[parentKey].(type) {
	case map[string]interface{}:
		return strings.ToLower(strings.TrimSpace(stringValue(nested, childKey)))
	case map[string]string:
		return strings.ToLower(strings.TrimSpace(nested[childKey]))
	default:
		return ""
	}
}
