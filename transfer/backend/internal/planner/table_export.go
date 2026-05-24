package planner

import (
	"encoding/json"
	"fmt"
	"github.com/addp/common/datatype"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/addp/common/dataitem"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
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
	Attributes       map[string]interface{} `json:"attributes,omitempty"`
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

	sourcePlan, err := buildTableSourcePlan(spec.Source, sourceEngine, spec.Transforms)
	if err != nil {
		return nil, err
	}
	targetPlan, err := buildTableTargetPlan(spec.Target, targetEngine)
	if err != nil {
		return nil, err
	}
	if sourcePlan.Kind == executor.TableEndpointNative && targetPlan.Kind == executor.TableEndpointEncoded {
		sourcePlan.ReadOptions = mergeReadOptions(sourcePlan.ReadOptions, readOptionsForTarget(targetPlan.Format, targetPlan.FormatOptions))
	}
	applySourceGeometryEncodingForTarget(&sourcePlan, targetPlan)
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

func applySourceGeometryEncodingForTarget(sourcePlan *executor.TableSourcePlan, targetPlan executor.TableTargetPlan) {
	if sourcePlan == nil || sourcePlan.Kind != executor.TableEndpointEncoded || targetPlan.Kind != executor.TableEndpointNative {
		return
	}
	if !formatHasSpatialRows(sourcePlan.Format) && sourcePlan.SpatialInfo == nil {
		return
	}
	if sourcePlan.ParseOptions == nil {
		sourcePlan.ParseOptions = format.DefaultParseOptions()
	}
	if sourcePlan.ParseOptions.GeometryEncoding == "" || sourcePlan.ParseOptions.GeometryEncoding == format.GeometryEncodingWKT {
		sourcePlan.ParseOptions.GeometryEncoding = format.GeometryEncodingEWKB
	}
}

func formatHasSpatialRows(formatType format.FormatType) bool {
	descriptor, ok := format.GetFormatDescriptor(formatType)
	return ok && descriptor.Spatial
}

func sourceItemDescriptor(attrs map[string]interface{}) (dataitem.ItemDescriptor, bool) {
	descriptor := dataitem.DescriptorFromAttributes(attrs)
	if descriptor.Layout == "" && descriptor.DataType == "" && descriptor.Format == "" && descriptor.PhysicalPath == "" && descriptor.StoragePath == "" && len(descriptor.Refs) == 0 {
		return dataitem.ItemDescriptor{}, false
	}
	return descriptor, true
}

func sourceFormatFromEndpoint(endpoint EndpointSpec, descriptor dataitem.ItemDescriptor) (format.FormatType, error) {
	metaFormat := format.FormatType(strings.TrimSpace(descriptor.Format))
	endpointFormat := format.FormatType(strings.TrimSpace(string(endpoint.Format)))
	if metaFormat == "" {
		return endpointFormat, nil
	}
	if endpointFormat != "" && endpointFormat != metaFormat {
		return "", fmt.Errorf("source format %q conflicts with Meta item format %q", endpointFormat, metaFormat)
	}
	return metaFormat, nil
}

func sourceEndpointContentCatalogPath(engineID uint, resource EndpointResourceSpec, descriptor dataitem.ItemDescriptor) (engineplugin.CatalogPath, error) {
	switch resource.Kind {
	case EndpointResourceKindFile:
		path := descriptor.PhysicalPath
		if path == "" {
			path = engineplugin.NormalizeFileCatalogPath(contentPathString(resource.Path, "file"))
		}
		if path == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("source file resource path requires path")
		}
		if descriptor.Layout == dataitem.LayoutWhole {
			return engineplugin.FileDirectoryPath(engineID, path), nil
		}
		return engineplugin.FileItemPath(engineID, path), nil
	case EndpointResourceKindObject:
		bucket := descriptor.StorageBucket
		objectPath := objectPathFromDescriptor(descriptor, &bucket)
		if bucket == "" || objectPath == "" {
			values, _ := resource.Path.(map[string]interface{})
			if bucket == "" {
				bucket = stringValue(values, "bucket")
			}
			if objectPath == "" {
				objectPath = contentPathString(resource.Path, "object")
			}
		}
		if bucket == "" || objectPath == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("source object resource path requires bucket and path")
		}
		if descriptor.Layout == dataitem.LayoutWhole {
			return engineplugin.ObjectDirectoryPath(engineID, bucket, objectPath), nil
		}
		return engineplugin.ObjectItemPath(engineID, bucket, objectPath), nil
	default:
		return endpointContentCatalogPath(engineID, resource, "source")
	}
}

func objectPathFromDescriptor(descriptor dataitem.ItemDescriptor, bucket *string) string {
	if physicalPath := strings.Trim(descriptor.PhysicalPath, "/"); physicalPath != "" {
		if bucket != nil && *bucket != "" {
			prefix := strings.Trim(*bucket, "/") + "/"
			return strings.Trim(strings.TrimPrefix(physicalPath, prefix), "/")
		}
		if splitBucket, splitPath, ok := strings.Cut(physicalPath, "/"); ok {
			if bucket != nil && *bucket == "" {
				*bucket = splitBucket
			}
			return strings.Trim(splitPath, "/")
		}
		return physicalPath
	}
	storagePath := strings.Trim(descriptor.StoragePath, "/")
	storageName := strings.Trim(descriptor.StorageName, "/")
	if storageName != "" {
		return strings.Trim(pathpkg.Join(storagePath, storageName), "/")
	}
	return storagePath
}

func itemDescriptorWithPhysicalPath(descriptor dataitem.ItemDescriptor, physicalPath string) dataitem.ItemDescriptor {
	if strings.TrimSpace(physicalPath) == "" {
		return descriptor
	}
	descriptor.PhysicalPath = strings.TrimSpace(physicalPath)
	return descriptor
}

func validateSourceRelatedRefs(formatType format.FormatType, refs []format.RelatedRef) error {
	if len(refs) == 0 {
		return nil
	}
	provider, err := format.GetMultiTableReaderProvider(formatType)
	if err != nil {
		return fmt.Errorf("source meta item layout=multi requires attributes.item.refs, but format %q has no multi table reader provider: %w", formatType, err)
	}
	if err := format.ValidateRelatedRefs(refs); err != nil {
		return fmt.Errorf("source meta item related refs are invalid: %w", err)
	}
	specs := provider.RelatedRefSpecs()
	if len(specs) == 0 {
		return nil
	}
	required := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if !spec.Required {
			continue
		}
		required[relatedRefSpecKey(spec)] = false
	}
	for _, ref := range refs {
		for _, spec := range specs {
			if !spec.Required {
				continue
			}
			if relatedRefMatchesSpec(ref, spec) {
				required[relatedRefSpecKey(spec)] = true
			}
		}
	}
	missing := make([]string, 0, len(required))
	for key, present := range required {
		if !present {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("source meta item related refs are incomplete, missing required refs: %s", strings.Join(missing, ", "))
	}
	return nil
}

func relatedRefMatchesSpec(ref format.RelatedRef, spec format.RelatedRefSpec) bool {
	ext := format.NormalizeExtension(filepath.Ext(ref.Ref.Path))
	if specExt := format.NormalizeExtension(spec.Extension); specExt != "" && ext == specExt {
		return true
	}
	role := strings.ToLower(strings.TrimSpace(ref.Ref.Role))
	if specRole := strings.ToLower(strings.TrimSpace(spec.Role)); specRole != "" && role == specRole {
		return true
	}
	return false
}

func relatedRefSpecKey(spec format.RelatedRefSpec) string {
	ext := format.NormalizeExtension(spec.Extension)
	role := strings.ToLower(strings.TrimSpace(spec.Role))
	if role == "" {
		role = strings.TrimPrefix(ext, ".")
	}
	return role + ":" + ext
}

func tableInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.TableInfo {
	info := datatype.TableInfoFromAttributes(attrs, "table")
	if info == nil {
		return nil
	}
	if spatialInfo := spatialInfoFromMetaAttributes(attrs); spatialInfo != nil {
		geometryColumn := spatialInfo.PrimaryGeometryName()
		for i := range info.Fields {
			if strings.EqualFold(info.Fields[i].Name, geometryColumn) {
				info.Fields[i].Type = datatype.FieldTypeGeometry
				break
			}
		}
	}
	return info
}

func spatialInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.SpatialInfo {
	spatialAttrs := commonJSON.Section(attrs, "capabilities.spatial")
	if len(spatialAttrs) == 0 {
		return nil
	}
	geometryColumn := commonJSON.InterfaceString(spatialAttrs["primary_geometry_column"])
	var geometryType string
	var srid int
	var dimension int
	for _, item := range interfaceSlice(spatialAttrs["geometry_columns"]) {
		column := rawMapAttribute(item)
		if len(column) == 0 {
			continue
		}
		name := strings.TrimSpace(commonJSON.InterfaceString(column["name"]))
		if geometryColumn != "" && !strings.EqualFold(name, geometryColumn) {
			continue
		}
		if geometryColumn == "" {
			geometryColumn = name
		}
		geometryType = strings.TrimSpace(commonJSON.InterfaceString(column["geometry_type"]))
		srid = int(commonJSON.InterfaceInt64(column["srid"]))
		dimension = int(commonJSON.InterfaceInt64(column["dimension"]))
		break
	}
	if geometryColumn == "" {
		return nil
	}
	if dimension == 0 {
		dimension = int(commonJSON.InterfaceInt64(spatialAttrs["dimension"]))
	}
	if dimension == 0 {
		dimension = 2
	}
	spatialInfo := datatype.NewSingleGeometrySpatialInfo(geometryColumn, geometryType, srid, dimension)
	hasSpatialIndex := commonJSON.InterfaceBool(spatialAttrs["has_spatial_index"])
	spatialInfo.HasSpatialIndex = &hasSpatialIndex
	spatialInfo.IndexName = commonJSON.InterfaceString(spatialAttrs["index_name"])
	if extent := commonJSON.InterfaceFloat64Slice(spatialAttrs["extent"]); len(extent) == 4 {
		boundingBox := datatype.BoundingBox{extent[0], extent[1], extent[2], extent[3]}
		spatialInfo.Extent = &boundingBox
	}
	return spatialInfo
}

func applyMetaSpatialParseOptions(opts *format.ParseOptions, spatialInfo *datatype.SpatialInfo) {
	if opts == nil || spatialInfo == nil {
		return
	}
	if opts.ExtraParams == nil {
		opts.ExtraParams = map[string]interface{}{}
	}
	if geometryColumn := spatialInfo.PrimaryGeometryName(); strings.TrimSpace(commonJSON.InterfaceString(opts.ExtraParams["geometry_field"])) == "" && geometryColumn != "" {
		opts.ExtraParams["geometry_field"] = geometryColumn
	}
}

func interfaceSlice(value interface{}) []interface{} {
	return commonJSON.InterfaceSlice(value)
}

func rawMapAttribute(value interface{}) map[string]interface{} {
	return commonJSON.InterfaceMap(value)
}

func cloneInterfaceMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func buildTableSourcePlan(endpoint EndpointSpec, engine EngineBinding, transforms []TransformSpec) (executor.TableSourcePlan, error) {
	itemDescriptor, hasItemAttributes := sourceItemDescriptor(endpoint.Attributes)
	sourceSchema := tableInfoFromMetaAttributes(endpoint.Attributes)
	sourceSpatialInfo := spatialInfoFromMetaAttributes(endpoint.Attributes)
	switch endpoint.Representation {
	case representationNative:
		sourcePath, err := nativeTablePath(engine.EngineID, endpoint.EndpointResource.Path)
		if err != nil {
			return executor.TableSourcePlan{}, fmt.Errorf("build source path: %w", err)
		}
		readOptions := nativeReadOptionsFromTransforms(transforms)
		return executor.TableSourcePlan{
			Kind:        executor.TableEndpointNative,
			ConnInfo:    engine.ConnInfo,
			Path:        sourcePath,
			ReadOptions: readOptions,
			Layout:      itemDescriptor.Layout,
			Schema:      sourceSchema,
			SpatialInfo: sourceSpatialInfo,
		}, nil
	case representationEncoded:
		sourcePath, err := sourceEndpointContentCatalogPath(engine.EngineID, endpoint.EndpointResource, itemDescriptor)
		if err != nil {
			return executor.TableSourcePlan{}, fmt.Errorf("build source path: %w", err)
		}
		sourceFormat, err := sourceFormatFromEndpoint(endpoint, itemDescriptor)
		if err != nil {
			return executor.TableSourcePlan{}, err
		}
		if sourceFormat == "" {
			return executor.TableSourcePlan{}, fmt.Errorf("source format is required")
		}
		if err := validateTransferReadableTableFormat(sourceFormat); err != nil {
			return executor.TableSourcePlan{}, err
		}
		parseOptions := tableParseOptions(endpoint.Options, sourceFormat)
		applyMetaSpatialParseOptions(parseOptions, sourceSpatialInfo)
		if selection := sourceFieldSelectionFromTransforms(transforms); selection != nil {
			parseOptions.FieldSelection = selection
		}
		relatedRefs := itemDescriptor.RelatedRefs()
		if itemDescriptor.Layout == dataitem.LayoutMulti {
			if len(relatedRefs) == 0 {
				return executor.TableSourcePlan{}, fmt.Errorf("source meta item layout=multi requires attributes.item.refs; rescan the Meta node to restore item refs")
			}
			if err := validateSourceRelatedRefs(sourceFormat, relatedRefs); err != nil {
				return executor.TableSourcePlan{}, err
			}
			if primary, err := format.PrimaryRelatedRef(relatedRefs); err == nil {
				if primaryPath, pathErr := sourceEndpointContentCatalogPath(engine.EngineID, endpoint.EndpointResource, itemDescriptorWithPhysicalPath(itemDescriptor, primary.Ref.Path)); pathErr == nil {
					sourcePath = primaryPath
				}
			}
		} else if hasItemAttributes && itemDescriptor.Layout == dataitem.LayoutWhole {
			relatedRefs = itemDescriptor.RelatedRefs()
		}
		return executor.TableSourcePlan{
			Kind:         executor.TableEndpointEncoded,
			ConnInfo:     engine.ConnInfo,
			Path:         sourcePath,
			Format:       sourceFormat,
			Layout:       itemDescriptor.Layout,
			ParseOptions: parseOptions,
			Schema:       sourceSchema,
			SpatialInfo:  sourceSpatialInfo,
			RelatedRefs:  relatedRefs,
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
		return executor.TableTargetPlan{
			Kind:              executor.TableEndpointNative,
			ConnInfo:          engine.ConnInfo,
			Path:              targetPath,
			DeleteBeforeWrite: writeMode(endpoint.Policy) == defaultWriteMode,
			TablePrepare:      engineplugin.TableWriteOptions{},
			TableWrite: engineplugin.BatchWriteOptions{
				Method: importWriteMethod(endpoint.Policy),
			},
		}, nil
	case representationEncoded:
		targetFormat := endpoint.Format
		if targetFormat == "" {
			return executor.TableTargetPlan{}, fmt.Errorf("target format is required")
		}
		if err := validateTransferWritableTableFormat(targetFormat); err != nil {
			return executor.TableTargetPlan{}, err
		}
		writeOptions := tableWriteOptions(endpoint.Options, targetFormat)
		targetPath, err := targetEndpointContentCatalogPath(engine.EngineID, endpoint.EndpointResource, targetFormat, writeOptions)
		if err != nil {
			return executor.TableTargetPlan{}, fmt.Errorf("build target path: %w", err)
		}
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

func sourceFieldSelectionFromTransforms(transforms []TransformSpec) *format.FieldSelectionOptions {
	if len(transforms) == 0 {
		return nil
	}
	fields := make([]string, 0)
	seen := map[string]bool{}
	for _, transform := range transforms {
		if strings.ToLower(strings.TrimSpace(transform.Type)) != "field_mapping" {
			return nil
		}
		mode := normalizeFieldMappingMode(transform.Mode)
		if mode == string(executor.FieldMappingModePassthrough) {
			return nil
		}
		if mode != string(executor.FieldMappingModeProject) {
			return nil
		}
		for _, field := range transform.Fields {
			source := strings.TrimSpace(field.Source)
			if source == "" || seen[source] {
				continue
			}
			seen[source] = true
			fields = append(fields, source)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return &format.FieldSelectionOptions{
		Include:            fields,
		MissingFieldPolicy: format.MissingFieldError,
	}
}

func nativeReadOptionsFromTransforms(transforms []TransformSpec) map[string]interface{} {
	selection := sourceFieldSelectionFromTransforms(transforms)
	if selection == nil {
		return nil
	}
	return map[string]interface{}{
		format.FieldSelectionOptionKey: selection,
	}
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

func targetEndpointContentCatalogPath(engineID uint, resource EndpointResourceSpec, formatType format.FormatType, writeOptions *format.WriteOptions) (engineplugin.CatalogPath, error) {
	return endpointContentCatalogPathWithTargetFormat(engineID, resource, "target", formatType, writeOptions)
}

func endpointContentCatalogPath(engineID uint, resource EndpointResourceSpec, role string) (engineplugin.CatalogPath, error) {
	return endpointContentCatalogPathWithTargetFormat(engineID, resource, role, "", nil)
}

func endpointContentCatalogPathWithTargetFormat(engineID uint, resource EndpointResourceSpec, role string, formatType format.FormatType, writeOptions *format.WriteOptions) (engineplugin.CatalogPath, error) {
	switch resource.Kind {
	case EndpointResourceKindFile:
		path := engineplugin.NormalizeFileCatalogPath(contentPathString(resource.Path, "file"))
		if path == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("%s file resource path requires path", role)
		}
		if role == "target" && formatType != "" {
			normalizedPath, err := normalizeTargetContentPathExtension(path, formatType, writeOptions)
			if err != nil {
				return engineplugin.CatalogPath{}, err
			}
			path = normalizedPath
		}
		return engineplugin.FileItemPath(engineID, path), nil
	case EndpointResourceKindObject:
		values, _ := resource.Path.(map[string]interface{})
		bucket := stringValue(values, "bucket")
		objectPath := contentPathString(resource.Path, "object")
		if bucket == "" || objectPath == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("%s object resource path requires bucket and path", role)
		}
		if role == "target" && formatType != "" {
			normalizedPath, err := normalizeTargetContentPathExtension(objectPath, formatType, writeOptions)
			if err != nil {
				return engineplugin.CatalogPath{}, err
			}
			objectPath = normalizedPath
		}
		return engineplugin.ObjectItemPath(engineID, bucket, objectPath), nil
	default:
		return engineplugin.CatalogPath{}, fmt.Errorf("unsupported %s resource kind %q", role, resource.Kind)
	}
}

func normalizeTargetContentPathExtension(rawPath string, formatType format.FormatType, writeOptions *format.WriteOptions) (string, error) {
	path := strings.TrimSpace(rawPath)
	expectedExt := format.DefaultWriteExtension(formatType, writeOptions)
	if path == "" || expectedExt == "" {
		return path, nil
	}
	currentExt := format.NormalizeExtension(pathpkg.Ext(path))
	if currentExt == "" {
		return path + expectedExt, nil
	}
	if currentExt != expectedExt {
		return "", fmt.Errorf("target path extension %q conflicts with target format %q; expected %q", currentExt, formatType, expectedExt)
	}
	return path, nil
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
	if descriptor.DataType != datatype.DataTypeTable {
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
	if _, err := format.GetMultiTableReaderProvider(formatType); err == nil {
		return true
	}
	return false
}

func validateTransferWritableTableFormat(formatType format.FormatType) error {
	descriptor, ok := format.GetFormatDescriptor(formatType)
	if !ok {
		return fmt.Errorf("format %q is not registered", formatType)
	}
	if descriptor.DataType != datatype.DataTypeTable {
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

func importWriteMethod(policy map[string]interface{}) string {
	return strings.ToLower(stringValue(policy, "write_method"))
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

func mergeReadOptions(base map[string]interface{}, overlays ...map[string]interface{}) map[string]interface{} {
	var result map[string]interface{}
	if len(base) > 0 {
		result = make(map[string]interface{}, len(base))
		for key, value := range base {
			result[key] = value
		}
	}
	for _, overlay := range overlays {
		if len(overlay) == 0 {
			continue
		}
		if result == nil {
			result = make(map[string]interface{}, len(overlay))
		}
		for key, value := range overlay {
			result[key] = value
		}
	}
	return result
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
	if encoding := stringValue(raw, "encoding"); encoding != "" {
		opts.Encoding = encoding
	}
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
