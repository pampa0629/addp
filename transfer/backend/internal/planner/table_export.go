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
	modeBatch             = "batch"
	dataTypeTable         = "table"
	representationNative  = "native"
	representationEncoded = "encoded"
	defaultWriteMode      = "overwrite"
)

const (
	EndpointResourceKindNativeTable = "native_table"
	EndpointResourceKindFile        = "file"
	EndpointResourceKindObject      = "object"
)

type EngineRef struct {
	Scope string `json:"scope"`
	ID    uint   `json:"id"`
	Type  string `json:"type,omitempty"`
}

type EndpointResourceSpec struct {
	Kind string      `json:"kind"`
	Path interface{} `json:"path"`
}

// EndpointSpec 是 Transfer 任务 JSON 中 source / target 的业务端点描述。
// EndpointResource 字段表示端点所在的引擎资源形态，不是 common/contentio 抽象。
type EndpointSpec struct {
	Engine           EngineRef              `json:"engine"`
	EndpointResource EndpointResourceSpec   `json:"resource"`
	DataType         string                 `json:"data_type"`
	Representation   string                 `json:"representation"`
	Format           format.FormatType      `json:"format,omitempty"`
	Options          map[string]interface{} `json:"options,omitempty"`
	Policy           map[string]interface{} `json:"policy,omitempty"`
}

type TableExportTaskSpec struct {
	Mode       string          `json:"mode"`
	Source     EndpointSpec    `json:"source"`
	Target     EndpointSpec    `json:"target"`
	Transforms []TransformSpec `json:"transforms,omitempty"`
	BatchSize  int             `json:"batch_size,omitempty"`
}

type TransformSpec struct {
	Type    string                 `json:"type"`
	Version string                 `json:"version,omitempty"`
	Mode    string                 `json:"mode,omitempty"`
	Fields  []FieldMappingSpec     `json:"fields,omitempty"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

type FieldMappingSpec struct {
	Source     string      `json:"source,omitempty"`
	Target     string      `json:"target"`
	TargetType string      `json:"target_type,omitempty"`
	Nullable   *bool       `json:"nullable,omitempty"`
	Default    interface{} `json:"default,omitempty"`
	Format     string      `json:"format,omitempty"`
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

type TableTransferBuildResult struct {
	SourceEngineType string
	TargetEngineType string
	Plan             executor.TableTransferPlan
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

func hasTableExportWriter(formatType format.FormatType) bool {
	if _, err := format.GetTableWriterProvider(formatType); err == nil {
		return true
	}
	if _, err := format.GetMultiTableWriterProvider(formatType); err == nil {
		return true
	}
	return false
}

func BuildTableTransferPlan(spec TableExportTaskSpec, resolver EngineResolver) (*TableTransferBuildResult, error) {
	if resolver == nil {
		return nil, fmt.Errorf("engine resolver is required")
	}
	if err := validateTableTransferSpec(spec); err != nil {
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

	sourcePlan, err := buildTableSourcePlan(spec.Source, sourceEngine)
	if err != nil {
		return nil, err
	}
	targetPlan, err := buildTableTargetPlan(spec.Target, targetEngine)
	if err != nil {
		return nil, err
	}
	if sourcePlan.Kind == executor.TableEndpointNative && targetPlan.Kind == executor.TableEndpointEncoded {
		sourcePlan.ReadOptions = readOptionsForTarget(targetPlan.Format, targetPlan.FormatOptions)
	}
	return &TableTransferBuildResult{
		SourceEngineType: sourceType,
		TargetEngineType: targetType,
		Plan: executor.TableTransferPlan{
			Source:     sourcePlan,
			Target:     targetPlan,
			Transforms: buildTableTransforms(spec.Transforms),
			BatchSize:  spec.BatchSize,
		},
	}, nil
}

func buildTableSourcePlan(endpoint EndpointSpec, engine EngineBinding) (executor.TableSourcePlan, error) {
	switch endpoint.Representation {
	case representationNative:
		sourcePath, err := nativeTablePath(engine.EngineID, endpoint.EndpointResource.Path)
		if err != nil {
			return executor.TableSourcePlan{}, fmt.Errorf("build source path: %w", err)
		}
		return executor.TableSourcePlan{
			Kind:     executor.TableEndpointNative,
			ConnInfo: engine.ConnInfo,
			Path:     sourcePath,
		}, nil
	case representationEncoded:
		sourcePath, err := endpointContentCatalogPath(engine.EngineID, endpoint.EndpointResource, "source")
		if err != nil {
			return executor.TableSourcePlan{}, fmt.Errorf("build source path: %w", err)
		}
		sourceFormat := endpoint.Format
		if sourceFormat == "" {
			return executor.TableSourcePlan{}, fmt.Errorf("source format is required")
		}
		if err := validateTransferReadableTableFormat(sourceFormat); err != nil {
			return executor.TableSourcePlan{}, err
		}
		return executor.TableSourcePlan{
			Kind:         executor.TableEndpointEncoded,
			ConnInfo:     engine.ConnInfo,
			Path:         sourcePath,
			Format:       sourceFormat,
			ParseOptions: tableParseOptions(endpoint.Options, sourceFormat),
		}, nil
	default:
		return executor.TableSourcePlan{}, fmt.Errorf("unsupported source representation %q", endpoint.Representation)
	}
}

func buildTableTargetPlan(endpoint EndpointSpec, engine EngineBinding) (executor.TableTargetPlan, error) {
	switch endpoint.Representation {
	case representationNative:
		targetPath, err := nativeTablePath(engine.EngineID, endpoint.EndpointResource.Path)
		if err != nil {
			return executor.TableTargetPlan{}, fmt.Errorf("build target path: %w", err)
		}
		targetType := effectiveEngineType(engine, endpoint.Engine)
		return executor.TableTargetPlan{
			Kind:              executor.TableEndpointNative,
			ConnInfo:          engine.ConnInfo,
			Path:              targetPath,
			DeleteBeforeWrite: writeMode(endpoint.Policy) == defaultWriteMode,
			TablePrepare:      engineplugin.TableWriteOptions{},
			TableWrite: engineplugin.BatchWriteOptions{
				Method: importWriteMethod(endpoint.Policy, targetType),
			},
		}, nil
	case representationEncoded:
		targetPath, err := targetEndpointContentCatalogPath(engine.EngineID, endpoint.EndpointResource)
		if err != nil {
			return executor.TableTargetPlan{}, fmt.Errorf("build target path: %w", err)
		}
		targetFormat := endpoint.Format
		if targetFormat == "" {
			return executor.TableTargetPlan{}, fmt.Errorf("target format is required")
		}
		if err := validateTransferWritableTableFormat(targetFormat); err != nil {
			return executor.TableTargetPlan{}, err
		}
		writeOptions := tableWriteOptions(endpoint.Options, targetFormat)
		return executor.TableTargetPlan{
			Kind:              executor.TableEndpointEncoded,
			ConnInfo:          engine.ConnInfo,
			Path:              targetPath,
			DeleteBeforeWrite: writeMode(endpoint.Policy) == defaultWriteMode,
			ContentWrite:      engineplugin.WriteOptions{Overwrite: false},
			Format:            targetFormat,
			FormatOptions:     writeOptions,
		}, nil
	default:
		return executor.TableTargetPlan{}, fmt.Errorf("unsupported target representation %q", endpoint.Representation)
	}
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
	if err := validateTransformSpecs(spec.Transforms); err != nil {
		return err
	}
	if isTableExportSpec(spec) || isTableImportSpec(spec) || isEncodedTableTransferSpec(spec) || isNativeTableTransferSpec(spec) {
		return nil
	}
	return fmt.Errorf("unsupported table transfer shape: source %s/%s -> target %s/%s",
		spec.Source.Representation, spec.Source.EndpointResource.Kind,
		spec.Target.Representation, spec.Target.EndpointResource.Kind)
}

func validateTransformSpecs(transforms []TransformSpec) error {
	for i, transform := range transforms {
		transformType := strings.ToLower(strings.TrimSpace(transform.Type))
		if transformType == "" {
			return fmt.Errorf("transform[%d] type is required", i)
		}
		switch transformType {
		case "field_mapping":
			mode := strings.ToLower(strings.TrimSpace(transform.Mode))
			if mode != "" && mode != string(executor.FieldMappingModeProject) && mode != string(executor.FieldMappingModePassthrough) {
				return fmt.Errorf("transform[%d] field_mapping mode must be %q or %q, got %q", i, executor.FieldMappingModeProject, executor.FieldMappingModePassthrough, transform.Mode)
			}
			if len(transform.Fields) == 0 {
				return fmt.Errorf("transform[%d] field_mapping requires fields", i)
			}
			for j, field := range transform.Fields {
				if strings.TrimSpace(field.Target) == "" {
					return fmt.Errorf("transform[%d].fields[%d] target is required", i, j)
				}
			}
		default:
			return fmt.Errorf("unsupported transform type %q", transform.Type)
		}
	}
	return nil
}

func buildTableTransforms(transforms []TransformSpec) []executor.TableTransformPlan {
	if len(transforms) == 0 {
		return nil
	}
	plans := make([]executor.TableTransformPlan, 0, len(transforms))
	for _, transform := range transforms {
		switch strings.ToLower(strings.TrimSpace(transform.Type)) {
		case "field_mapping":
			plans = append(plans, executor.TableTransformPlan{
				Type: "field_mapping",
				FieldMapping: &executor.FieldMappingTransformPlan{
					Mode:   executor.FieldMappingMode(normalizeFieldMappingMode(transform.Mode)),
					Fields: buildFieldMappingFields(transform.Fields),
				},
			})
		}
	}
	return plans
}

func normalizeFieldMappingMode(mode string) string {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		return string(executor.FieldMappingModeProject)
	}
	return normalized
}

func buildFieldMappingFields(fields []FieldMappingSpec) []executor.FieldMappingFieldPlan {
	plans := make([]executor.FieldMappingFieldPlan, 0, len(fields))
	for _, field := range fields {
		nullable := true
		if field.Nullable != nil {
			nullable = *field.Nullable
		}
		plans = append(plans, executor.FieldMappingFieldPlan{
			Source:     strings.TrimSpace(field.Source),
			Target:     strings.TrimSpace(field.Target),
			TargetType: strings.TrimSpace(field.TargetType),
			Nullable:   nullable,
			Default:    field.Default,
			Format:     strings.TrimSpace(field.Format),
		})
	}
	return plans
}

func isNativeTableTransferSpec(spec TableExportTaskSpec) bool {
	return spec.Source.Representation == representationNative &&
		spec.Source.EndpointResource.Kind == EndpointResourceKindNativeTable &&
		spec.Target.Representation == representationNative &&
		spec.Target.EndpointResource.Kind == EndpointResourceKindNativeTable
}

func isTableExportSpec(spec TableExportTaskSpec) bool {
	return spec.Source.Representation == representationNative &&
		spec.Source.EndpointResource.Kind == EndpointResourceKindNativeTable &&
		spec.Target.Representation == representationEncoded &&
		isEndpointContentKind(spec.Target.EndpointResource.Kind)
}

func isTableImportSpec(spec TableExportTaskSpec) bool {
	return spec.Source.Representation == representationEncoded &&
		isEndpointContentKind(spec.Source.EndpointResource.Kind) &&
		spec.Target.Representation == representationNative &&
		spec.Target.EndpointResource.Kind == EndpointResourceKindNativeTable
}

func isEncodedTableTransferSpec(spec TableExportTaskSpec) bool {
	return spec.Source.Representation == representationEncoded &&
		isEndpointContentKind(spec.Source.EndpointResource.Kind) &&
		spec.Target.Representation == representationEncoded &&
		isEndpointContentKind(spec.Target.EndpointResource.Kind)
}

func IsTableImportSpec(spec TableExportTaskSpec) bool {
	return isTableImportSpec(spec)
}

func IsEncodedTableTransferSpec(spec TableExportTaskSpec) bool {
	return isEncodedTableTransferSpec(spec)
}

func IsNativeTableTransferSpec(spec TableExportTaskSpec) bool {
	return isNativeTableTransferSpec(spec)
}

func isEndpointContentKind(kind string) bool {
	return kind == EndpointResourceKindFile || kind == EndpointResourceKindObject
}

func validateEndpointCommon(endpoint EndpointSpec, role, dataType string) error {
	if err := validateEndpointIdentity(endpoint, role, dataType); err != nil {
		return err
	}
	switch endpoint.Representation {
	case representationNative:
		if endpoint.EndpointResource.Kind != EndpointResourceKindNativeTable {
			return fmt.Errorf("%s native endpoint resource kind must be %q, got %q", role, EndpointResourceKindNativeTable, endpoint.EndpointResource.Kind)
		}
	case representationEncoded:
		if !isEndpointContentKind(endpoint.EndpointResource.Kind) {
			return fmt.Errorf("%s encoded endpoint resource kind must be %q or %q, got %q", role, EndpointResourceKindFile, EndpointResourceKindObject, endpoint.EndpointResource.Kind)
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

func targetEndpointContentCatalogPath(engineID uint, resource EndpointResourceSpec) (engineplugin.CatalogPath, error) {
	return endpointContentCatalogPath(engineID, resource, "target")
}

func endpointContentCatalogPath(engineID uint, resource EndpointResourceSpec, role string) (engineplugin.CatalogPath, error) {
	switch resource.Kind {
	case EndpointResourceKindFile:
		path := engineplugin.NormalizeFileCatalogPath(contentPathString(resource.Path, "file"))
		if path == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("%s file resource path requires path", role)
		}
		return engineplugin.FileItemPath(engineID, path), nil
	case EndpointResourceKindObject:
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

func validateTransferReadableTableFormat(formatType format.FormatType) error {
	descriptor, ok := format.GetFormatDescriptor(formatType)
	if !ok {
		return fmt.Errorf("format %q is not registered", formatType)
	}
	if descriptor.DataType != format.FormatDataTypeTable {
		if !hasTableTransferReader(formatType) {
			_, err := format.GetTableReaderProvider(formatType)
			return fmt.Errorf("format %q has no table reader provider: %w", formatType, err)
		}
	}
	if !descriptor.TransferRead {
		return fmt.Errorf("format %q is not declared as transfer readable", formatType)
	}
	if !hasTableTransferReader(formatType) {
		_, err := format.GetTableReaderProvider(formatType)
		return fmt.Errorf("format %q has no table reader provider: %w", formatType, err)
	}
	return nil
}

func hasTableTransferReader(formatType format.FormatType) bool {
	if _, err := format.GetTableReaderProvider(formatType); err == nil {
		return true
	}
	if _, err := format.GetMultiTableProvider(formatType); err == nil {
		return true
	}
	return false
}

func validateTransferWritableTableFormat(formatType format.FormatType) error {
	descriptor, ok := format.GetFormatDescriptor(formatType)
	if !ok {
		return fmt.Errorf("format %q is not registered", formatType)
	}
	if descriptor.DataType != format.FormatDataTypeTable {
		if !hasTableExportWriter(formatType) {
			_, err := format.GetTableWriterProvider(formatType)
			return fmt.Errorf("format %q has no table writer provider: %w", formatType, err)
		}
	}
	if !descriptor.TransferWrite {
		return fmt.Errorf("format %q is not declared as transfer writable", formatType)
	}
	if !hasTableExportWriter(formatType) {
		_, err := format.GetTableWriterProvider(formatType)
		return fmt.Errorf("format %q has no table writer provider: %w", formatType, err)
	}
	return nil
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
