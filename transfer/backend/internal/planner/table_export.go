package planner

import (
	"bytes"
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
	"github.com/addp/common/resourcetree"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/addp/transfer/internal/executor"
)

const (
	modeBatch             = "batch"
	dataTypeTable         = "table"
	representationNative  = "native"
	representationEncoded = "encoded"
	defaultWriteMode      = "overwrite"
)

type EngineRef struct {
	ID   uint   `json:"id"`
	Type string `json:"type,omitempty"`
}

// EndpointSpec 是 Transfer 任务 JSON 中 source / target 的业务端点描述。
// Source 使用 locator 指向已存在资源；target 使用 parent_locator + name 表达待写入资源。
type EndpointSpec struct {
	Locator        string                 `json:"locator"`
	ParentLocator  string                 `json:"parent_locator,omitempty"`
	Name           string                 `json:"name,omitempty"`
	DataType       string                 `json:"data_type"`
	Representation string                 `json:"representation"`
	Format         format.FormatType      `json:"format,omitempty"`
	Options        map[string]interface{} `json:"options,omitempty"`
	Policy         map[string]interface{} `json:"policy,omitempty"`
	Attributes     map[string]interface{} `json:"-"`
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
	Type         string
	ConnInfo     engineplugin.ConnectionInfo
	EngineID     uint
	PluginType   string
	Capabilities *engineplugin.EngineCapabilities
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

func (e EndpointSpec) ResourceLocator() (*resourcetree.ResourceLocator, error) {
	loc, err := resourcetree.ParseURI(strings.TrimSpace(e.Locator))
	if err != nil {
		return nil, err
	}
	return loc, nil
}

func (e EndpointSpec) InfraLocator() (*InfraLocator, error) {
	return ParseInfraLocatorURI(strings.TrimSpace(e.Locator))
}

func (e EndpointSpec) ParentResourceLocator() (*resourcetree.ResourceLocator, error) {
	loc, err := resourcetree.ParseURI(strings.TrimSpace(e.ParentLocator))
	if err != nil {
		return nil, err
	}
	return loc, nil
}

func (e EndpointSpec) ParentInfraLocator() (*InfraLocator, error) {
	return ParseInfraLocatorURI(strings.TrimSpace(e.ParentLocator))
}

func (e EndpointSpec) EngineRef() (EngineRef, error) {
	if IsInfraLocatorURI(e.Locator) {
		loc, err := e.InfraLocator()
		if err != nil {
			return EngineRef{}, err
		}
		return loc.EngineRef(), nil
	}
	if IsInfraLocatorURI(e.ParentLocator) {
		loc, err := e.ParentInfraLocator()
		if err != nil {
			return EngineRef{}, err
		}
		return loc.EngineRef(), nil
	}
	loc, err := e.endpointEngineLocator()
	if err != nil {
		return EngineRef{}, err
	}
	return EngineRef{ID: loc.EngineID}, nil
}

func (e EndpointSpec) LocatorEngineID() uint {
	if IsInfraLocatorURI(e.Locator) {
		return 0
	}
	loc, err := e.endpointEngineLocator()
	if err != nil || loc == nil {
		return 0
	}
	return loc.EngineID
}

func (e EndpointSpec) LocatorItemID() uint {
	if IsInfraLocatorURI(e.Locator) {
		return 0
	}
	loc, err := e.ResourceLocator()
	if err != nil || loc == nil || loc.ItemID == nil {
		return 0
	}
	return *loc.ItemID
}

func (e EndpointSpec) LocatorType() resourcetree.ResourceType {
	if IsInfraLocatorURI(e.Locator) {
		loc, err := e.InfraLocator()
		if err != nil || loc == nil {
			return ""
		}
		return loc.Type
	}
	loc, err := e.ResourceLocator()
	if err != nil || loc == nil {
		return ""
	}
	return loc.Type
}

func (e EndpointSpec) TargetResourceType() resourcetree.ResourceType {
	switch e.Representation {
	case representationNative:
		return resourcetree.TypeTable
	case representationEncoded:
		if IsInfraLocatorURI(e.ParentLocator) {
			parent, err := e.ParentInfraLocator()
			if err != nil {
				return ""
			}
			return targetContentResourceTypeFromParent(parent.Type)
		}
		parent, err := e.ParentResourceLocator()
		if err != nil {
			return ""
		}
		return targetContentResourceTypeFromParent(parent.Type)
	default:
		return ""
	}
}

func (e EndpointSpec) endpointEngineLocator() (*resourcetree.ResourceLocator, error) {
	if strings.TrimSpace(e.Locator) != "" {
		return e.ResourceLocator()
	}
	return e.ParentResourceLocator()
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
	if hasUnsupportedEndpointAttributes(config) {
		return TableExportTaskSpec{}, fmt.Errorf("endpoint attributes are not supported in transfer task config; use source locator item_id to reference Meta item attributes")
	}

	var spec TableExportTaskSpec
	configBytes, err := json.Marshal(config)
	if err != nil {
		return TableExportTaskSpec{}, fmt.Errorf("marshal transfer task config: %w", err)
	}
	if err := decodeStrictTaskConfig(configBytes, &spec, "transfer"); err != nil {
		return TableExportTaskSpec{}, err
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

	sourceRef, err := spec.Source.EngineRef()
	if err != nil {
		return nil, fmt.Errorf("parse source locator: %w", err)
	}
	targetRef, err := spec.Target.EngineRef()
	if err != nil {
		return nil, fmt.Errorf("parse target parent locator: %w", err)
	}

	sourceEngine, err := resolver.ResolveEngine(sourceRef)
	if err != nil {
		return nil, fmt.Errorf("resolve source engine: %w", err)
	}
	targetEngine, err := resolver.ResolveEngine(targetRef)
	if err != nil {
		return nil, fmt.Errorf("resolve target engine: %w", err)
	}
	sourceType := effectiveEngineType(sourceEngine, sourceRef)
	targetType := effectiveEngineType(targetEngine, targetRef)
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
	transforms := buildTableTransforms(spec.Transforms)
	if sourcePlan.Kind == executor.TableEndpointNative && targetPlan.Kind == executor.TableEndpointEncoded && engineSupportsSpatialTransform(sourceEngine) {
		sourcePlan.ReadOptions = mergeReadOptions(sourcePlan.ReadOptions, readOptionsForGeoJSONTarget(targetPlan.Format, targetPlan.FormatOptions, sourcePlan.SpatialInfo))
	}
	spatialTransform, err := buildSpatialGeoJSONTransform(&sourcePlan, targetPlan, sourceEngine)
	if err != nil {
		return nil, err
	}
	if spatialTransform != nil {
		transforms = append(transforms, *spatialTransform)
	}
	if err := applySourceGeometryEncodingForTarget(&sourcePlan, targetPlan, sourceEngine, targetEngine); err != nil {
		return nil, err
	}
	return &TableTransferBuildResult{
		SourceEngineType: sourceType,
		TargetEngineType: targetType,
		Plan: executor.TableTransferPlan{
			Source:     sourcePlan,
			Target:     targetPlan,
			Transforms: transforms,
			BatchSize:  spec.BatchSize,
		},
	}, nil
}

func applySourceGeometryEncodingForTarget(sourcePlan *executor.TableSourcePlan, targetPlan executor.TableTargetPlan, sourceEngine EngineBinding, targetEngine EngineBinding) error {
	if sourcePlan == nil {
		return nil
	}
	if !sourceHasSpatialRows(sourcePlan) {
		return nil
	}
	if !targetConsumesSpatialRows(targetPlan, targetEngine) {
		return nil
	}
	if existing := sourceGeometryEncoding(sourcePlan); existing != "" && existing != format.GeometryEncodingWKT && targetSupportsGeometryWriteEncoding(targetPlan, targetEngine, existing) {
		return nil
	}
	if nativeEncoding, ok := encodedNativeGeometryPassthroughEncoding(sourcePlan, targetPlan); ok {
		if sourcePlan.ParseOptions == nil {
			sourcePlan.ParseOptions = format.DefaultParseOptions()
		}
		sourcePlan.ParseOptions.GeometryEncoding = nativeEncoding
		return nil
	}
	if !targetSupportsGeometryWriteEncoding(targetPlan, targetEngine, format.GeometryEncodingEWKB) {
		return fmt.Errorf("target cannot consume spatial geometry encoding %q", format.GeometryEncodingEWKB)
	}
	switch sourcePlan.Kind {
	case executor.TableEndpointEncoded:
		if !formatSupportsGeometryReadEncoding(sourcePlan.Format, format.GeometryEncodingEWKB) {
			return fmt.Errorf("source format %q cannot provide spatial geometry encoding %q", sourcePlan.Format, format.GeometryEncodingEWKB)
		}
		if sourcePlan.ParseOptions == nil {
			sourcePlan.ParseOptions = format.DefaultParseOptions()
		}
		if sourcePlan.ParseOptions.GeometryEncoding == "" ||
			sourcePlan.ParseOptions.GeometryEncoding == format.GeometryEncodingWKT ||
			sourcePlan.ParseOptions.GeometryEncoding == format.GeometryEncodingGeoJSON {
			sourcePlan.ParseOptions.GeometryEncoding = format.GeometryEncodingEWKB
		}
	case executor.TableEndpointNative:
		if !nativeTableSupportsGeometryReadEncoding(sourceEngine, format.GeometryEncodingEWKB) {
			return fmt.Errorf("source native table engine %q cannot provide spatial geometry encoding %q", effectiveEngineType(sourceEngine, EngineRef{}), format.GeometryEncodingEWKB)
		}
		sourcePlan.ReadOptions = mergeReadOptions(sourcePlan.ReadOptions, map[string]interface{}{
			engineplugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
		})
	}
	return nil
}

func sourceHasSpatialRows(sourcePlan *executor.TableSourcePlan) bool {
	if sourcePlan == nil {
		return false
	}
	if sourcePlan.SpatialInfo != nil {
		return true
	}
	if formatImpliesSpatialRows(sourcePlan.Format) {
		return true
	}
	capability, err := format.GetSpatialEncodingCapability(sourcePlan.Format)
	return err == nil && len(capability.GeometryReadEncodings) > 0
}

func targetConsumesSpatialRows(targetPlan executor.TableTargetPlan, targetEngine EngineBinding) bool {
	switch targetPlan.Kind {
	case executor.TableEndpointNative:
		return nativeTableSpatialEncodingCapability(targetEngine) != nil
	case executor.TableEndpointEncoded:
		if formatImpliesSpatialRows(targetPlan.Format) {
			return true
		}
		capability, err := format.GetSpatialEncodingCapability(targetPlan.Format)
		return err == nil && len(capability.GeometryWriteEncodings) > 0
	default:
		return false
	}
}

func sourceGeometryEncoding(sourcePlan *executor.TableSourcePlan) format.GeometryEncoding {
	if sourcePlan == nil {
		return ""
	}
	switch sourcePlan.Kind {
	case executor.TableEndpointEncoded:
		if sourcePlan.ParseOptions == nil {
			return ""
		}
		return sourcePlan.ParseOptions.GeometryEncoding
	case executor.TableEndpointNative:
		if sourcePlan.ReadOptions == nil {
			return ""
		}
		return format.GeometryEncoding(strings.TrimSpace(commonJSON.InterfaceString(sourcePlan.ReadOptions[engineplugin.TableReadHintGeometryEncoding])))
	default:
		return ""
	}
}

func targetSupportsGeometryWriteEncoding(targetPlan executor.TableTargetPlan, targetEngine EngineBinding, encoding format.GeometryEncoding) bool {
	switch targetPlan.Kind {
	case executor.TableEndpointNative:
		return nativeTableSupportsGeometryWriteEncoding(targetEngine, encoding)
	case executor.TableEndpointEncoded:
		return formatSupportsGeometryWriteEncoding(targetPlan.Format, encoding)
	default:
		return false
	}
}

func encodedNativeGeometryPassthroughEncoding(sourcePlan *executor.TableSourcePlan, targetPlan executor.TableTargetPlan) (format.GeometryEncoding, bool) {
	if sourcePlan == nil || sourcePlan.Kind != executor.TableEndpointEncoded || targetPlan.Kind != executor.TableEndpointEncoded {
		return "", false
	}
	sourceCapability, err := format.GetSpatialEncodingCapability(sourcePlan.Format)
	if err != nil {
		return "", false
	}
	targetCapability, err := format.GetSpatialEncodingCapability(targetPlan.Format)
	if err != nil {
		return "", false
	}
	encoding := sourceCapability.NativeReadEncoding
	if encoding == "" || encoding != targetCapability.NativeWriteEncoding {
		return "", false
	}
	if !geometryEncodingInList(sourceCapability.GeometryReadEncodings, encoding) || !geometryEncodingInList(targetCapability.GeometryWriteEncodings, encoding) {
		return "", false
	}
	return encoding, true
}

func geometryEncodingInList(values []format.GeometryEncoding, target format.GeometryEncoding) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func formatImpliesSpatialRows(formatType format.FormatType) bool {
	return format.IsGeospatialFormat(formatType)
}

func formatSupportsGeometryReadEncoding(formatType format.FormatType, encoding format.GeometryEncoding) bool {
	capability, err := format.GetSpatialEncodingCapability(formatType)
	if err != nil {
		return false
	}
	for _, supported := range capability.GeometryReadEncodings {
		if supported == encoding {
			return true
		}
	}
	return false
}

func formatSupportsGeometryWriteEncoding(formatType format.FormatType, encoding format.GeometryEncoding) bool {
	capability, err := format.GetSpatialEncodingCapability(formatType)
	if err != nil {
		return false
	}
	for _, supported := range capability.GeometryWriteEncodings {
		if supported == encoding {
			return true
		}
	}
	return false
}

func nativeTableSupportsGeometryReadEncoding(engine EngineBinding, encoding format.GeometryEncoding) bool {
	capability := nativeTableSpatialEncodingCapability(engine)
	if capability == nil {
		return false
	}
	return stringSliceContainsFold(capability.GeometryReadEncodings, string(encoding))
}

func nativeTableSupportsGeometryWriteEncoding(engine EngineBinding, encoding format.GeometryEncoding) bool {
	capability := nativeTableSpatialEncodingCapability(engine)
	if capability == nil {
		return false
	}
	return stringSliceContainsFold(capability.GeometryWriteEncodings, string(encoding))
}

func nativeTableSpatialEncodingCapability(engine EngineBinding) *engineplugin.NativeTableSpatialEncodingCapability {
	capabilities := effectiveEngineCapabilities(engine)
	if capabilities == nil || capabilities.Storage == nil || capabilities.Storage.Store == nil {
		return nil
	}
	return capabilities.Storage.Store.TableSpatialEncoding
}

func stringSliceContainsFold(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func sourceItemDescriptorFromMetaAttributes(attrs map[string]interface{}) (dataitem.ItemDescriptor, bool) {
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

func sourceEndpointContentCatalogPath(endpoint EndpointSpec, descriptor dataitem.ItemDescriptor) (engineplugin.CatalogPath, error) {
	if IsInfraLocatorURI(endpoint.Locator) {
		loc, err := endpoint.InfraLocator()
		if err != nil {
			return engineplugin.CatalogPath{}, err
		}
		return loc.CatalogPath()
	}
	loc, err := endpoint.ResourceLocator()
	if err != nil {
		return engineplugin.CatalogPath{}, err
	}
	switch loc.Type {
	case resourcetree.TypeFile:
		path := descriptor.PhysicalPath
		if path == "" {
			path = engineplugin.NormalizeFileCatalogPath(loc.PathString())
		}
		if path == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("source file locator requires path")
		}
		if descriptor.Layout == format.LayoutWhole {
			return engineplugin.FileDirectoryPath(loc.EngineID, path), nil
		}
		return engineplugin.FileItemPath(loc.EngineID, path), nil
	case resourcetree.TypeObject:
		bucket := descriptor.StorageBucket
		objectPath := objectPathFromDescriptor(descriptor, &bucket)
		if bucket == "" || objectPath == "" {
			locatorBucket, locatorObjectPath := objectLocatorParts(loc)
			if bucket == "" {
				bucket = locatorBucket
			}
			if objectPath == "" {
				objectPath = locatorObjectPath
			}
		}
		if bucket == "" || objectPath == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("source object locator requires bucket and path")
		}
		if descriptor.Layout == format.LayoutWhole {
			return engineplugin.ObjectDirectoryPath(loc.EngineID, bucket, objectPath), nil
		}
		return engineplugin.ObjectItemPath(loc.EngineID, bucket, objectPath), nil
	default:
		return endpointContentCatalogPath(endpoint, "source")
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

func sourceTableInfoFromMetaAttributes(attrs map[string]interface{}, spatialInfo *datatype.SpatialInfo) *datatype.TableInfo {
	info := datatype.TableInfoFromPayload(commonJSON.Section(attrs, "type_info.table"), "table")
	if info == nil {
		return nil
	}
	if spatialInfo != nil {
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

func sourceSpatialInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.SpatialInfo {
	return datatype.SpatialInfoFromPayload(commonJSON.Section(attrs, "capabilities.spatial"))
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
	itemDescriptor, hasItemAttributes := sourceItemDescriptorFromMetaAttributes(endpoint.Attributes)
	sourceSpatialInfo := sourceSpatialInfoFromMetaAttributes(endpoint.Attributes)
	sourceTableInfo := sourceTableInfoFromMetaAttributes(endpoint.Attributes, sourceSpatialInfo)
	switch endpoint.Representation {
	case representationNative:
		sourcePath, err := nativeTablePathFromLocator(endpoint, engine)
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
			TableInfo:   sourceTableInfo,
			SpatialInfo: sourceSpatialInfo,
		}, nil
	case representationEncoded:
		sourcePath, err := sourceEndpointContentCatalogPath(endpoint, itemDescriptor)
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
		if itemDescriptor.Layout == format.LayoutMulti {
			if len(relatedRefs) == 0 {
				return executor.TableSourcePlan{}, fmt.Errorf("source meta item layout=multi requires attributes.item.refs; rescan the Meta node to restore item refs")
			}
			if err := validateSourceRelatedRefs(sourceFormat, relatedRefs); err != nil {
				return executor.TableSourcePlan{}, err
			}
			if primary, err := format.PrimaryRelatedRef(relatedRefs); err == nil {
				if primaryPath, pathErr := sourceEndpointContentCatalogPath(endpoint, itemDescriptorWithPhysicalPath(itemDescriptor, primary.Ref.Path)); pathErr == nil {
					sourcePath = primaryPath
				}
			}
		} else if hasItemAttributes && itemDescriptor.Layout == format.LayoutWhole {
			relatedRefs = itemDescriptor.RelatedRefs()
		}
		return executor.TableSourcePlan{
			Kind:         executor.TableEndpointEncoded,
			ConnInfo:     engine.ConnInfo,
			Path:         sourcePath,
			Format:       sourceFormat,
			Layout:       itemDescriptor.Layout,
			ParseOptions: parseOptions,
			TableInfo:    sourceTableInfo,
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
		targetPath, err := nativeTableTargetPath(endpoint, engine)
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
		targetPath, err := targetEndpointContentCatalogPath(endpoint, targetFormat, writeOptions)
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
		spec.Source.Representation, spec.Source.LocatorType(),
		spec.Target.Representation, spec.Target.TargetResourceType())
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
		isEndpointNativeTableType(spec.Source.LocatorType()) &&
		spec.Target.Representation == representationNative &&
		isEndpointNativeTableType(spec.Target.TargetResourceType())
}

func isTableExportSpec(spec TableExportTaskSpec) bool {
	return spec.Source.Representation == representationNative &&
		isEndpointNativeTableType(spec.Source.LocatorType()) &&
		spec.Target.Representation == representationEncoded &&
		isEndpointContentType(spec.Target.TargetResourceType())
}

func isTableImportSpec(spec TableExportTaskSpec) bool {
	return spec.Source.Representation == representationEncoded &&
		isEndpointContentType(spec.Source.LocatorType()) &&
		spec.Target.Representation == representationNative &&
		isEndpointNativeTableType(spec.Target.TargetResourceType())
}

func isEncodedTableTransferSpec(spec TableExportTaskSpec) bool {
	return spec.Source.Representation == representationEncoded &&
		isEndpointContentType(spec.Source.LocatorType()) &&
		spec.Target.Representation == representationEncoded &&
		isEndpointContentType(spec.Target.TargetResourceType())
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

func isEndpointNativeTableType(resourceType resourcetree.ResourceType) bool {
	return resourceType == resourcetree.TypeTable
}

func isEndpointContentType(resourceType resourcetree.ResourceType) bool {
	return resourceType == resourcetree.TypeFile || resourceType == resourcetree.TypeObject
}

func validateEndpointCommon(endpoint EndpointSpec, role, dataType string) error {
	if role == "target" {
		if err := validateTargetEndpointIdentity(endpoint, dataType); err != nil {
			return err
		}
	} else if err := validateSourceEndpointIdentity(endpoint, role, dataType); err != nil {
		return err
	}
	switch endpoint.Representation {
	case representationNative:
		resourceType := endpointResourceType(endpoint, role)
		if !isEndpointNativeTableType(resourceType) {
			return fmt.Errorf("%s native endpoint type must be %q, got %q", role, resourcetree.TypeTable, resourceType)
		}
	case representationEncoded:
		resourceType := endpointResourceType(endpoint, role)
		if !isEndpointContentType(resourceType) {
			return fmt.Errorf("%s encoded endpoint type must be %q or %q, got %q", role, resourcetree.TypeFile, resourcetree.TypeObject, resourceType)
		}
	default:
		return fmt.Errorf("%s representation must be %q or %q, got %q", role, representationNative, representationEncoded, endpoint.Representation)
	}
	return nil
}

func endpointResourceType(endpoint EndpointSpec, role string) resourcetree.ResourceType {
	if role == "target" {
		return endpoint.TargetResourceType()
	}
	return endpoint.LocatorType()
}

func validateSourceEndpointIdentity(endpoint EndpointSpec, role, dataType string) error {
	if strings.TrimSpace(endpoint.Locator) == "" {
		return fmt.Errorf("%s locator is required", role)
	}
	if strings.TrimSpace(endpoint.ParentLocator) != "" || strings.TrimSpace(endpoint.Name) != "" {
		return fmt.Errorf("%s endpoint must use locator only; parent_locator/name are only valid for target", role)
	}
	loc, err := endpoint.ResourceLocator()
	if IsInfraLocatorURI(endpoint.Locator) {
		infraLoc, infraErr := endpoint.InfraLocator()
		if infraErr != nil {
			return fmt.Errorf("%s locator is invalid: %w", role, infraErr)
		}
		if infraLoc.EngineRef().Type == "" {
			return fmt.Errorf("%s infra locator kind is required", role)
		}
	} else if err != nil {
		return fmt.Errorf("%s locator is invalid: %w", role, err)
	} else if loc.EngineID == 0 {
		return fmt.Errorf("%s locator engine_id is required", role)
	}
	if endpoint.DataType != dataType {
		return fmt.Errorf("%s data type must be %q, got %q", role, dataType, endpoint.DataType)
	}
	return nil
}

func validateTargetEndpointIdentity(endpoint EndpointSpec, dataType string) error {
	if strings.TrimSpace(endpoint.Locator) != "" {
		return fmt.Errorf("target locator is not supported for creation targets; use parent_locator and name")
	}
	if strings.TrimSpace(endpoint.ParentLocator) == "" {
		return fmt.Errorf("target parent_locator is required")
	}
	if strings.TrimSpace(endpoint.Name) == "" {
		return fmt.Errorf("target name is required")
	}
	loc, err := endpoint.ParentResourceLocator()
	if IsInfraLocatorURI(endpoint.ParentLocator) {
		infraLoc, infraErr := endpoint.ParentInfraLocator()
		if infraErr != nil {
			return fmt.Errorf("target parent_locator is invalid: %w", infraErr)
		}
		if infraLoc.EngineRef().Type == "" {
			return fmt.Errorf("target infra parent_locator kind is required")
		}
	} else if err != nil {
		return fmt.Errorf("target parent_locator is invalid: %w", err)
	} else if loc.EngineID == 0 {
		return fmt.Errorf("target parent_locator engine_id is required")
	}
	if strings.Contains(strings.TrimSpace(endpoint.Name), "/") {
		return fmt.Errorf("target name must be a single path segment")
	}
	if endpoint.DataType != dataType {
		return fmt.Errorf("target data type must be %q, got %q", dataType, endpoint.DataType)
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

func hasUnsupportedEndpointAttributes(config map[string]interface{}) bool {
	return endpointHasAttributes(config["source"]) || endpointHasAttributes(config["target"])
}

func endpointHasAttributes(raw interface{}) bool {
	endpoint, ok := raw.(map[string]interface{})
	if !ok {
		return false
	}
	_, ok = endpoint["attributes"]
	return ok
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

func decodeStrictTaskConfig(configBytes []byte, target interface{}, label string) error {
	decoder := json.NewDecoder(bytes.NewReader(configBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("parse %s task config: %w", label, err)
	}
	return nil
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

func effectiveEngineCapabilities(binding EngineBinding) *engineplugin.EngineCapabilities {
	if binding.Capabilities != nil {
		return binding.Capabilities
	}
	engineType := effectiveEngineType(binding, EngineRef{})
	if strings.TrimSpace(engineType) == "" {
		return nil
	}
	registered, err := engineplugin.Get(engineType)
	if err != nil || registered == nil {
		return nil
	}
	capabilities := registered.Capabilities()
	return &capabilities
}

func nativeTablePathFromLocator(endpoint EndpointSpec, engine EngineBinding) (engineplugin.CatalogPath, error) {
	loc, err := endpoint.ResourceLocator()
	if err != nil {
		return engineplugin.CatalogPath{}, err
	}
	if loc.Type != resourcetree.TypeTable {
		return engineplugin.CatalogPath{}, fmt.Errorf("native endpoint locator type must be %q, got %q", resourcetree.TypeTable, loc.Type)
	}
	if len(loc.Path) < 2 {
		return engineplugin.CatalogPath{}, fmt.Errorf("native table locator requires namespace and table path")
	}
	namespace := strings.TrimSpace(loc.Path[len(loc.Path)-2])
	table := strings.TrimSpace(loc.Path[len(loc.Path)-1])
	if namespace == "" || table == "" {
		return engineplugin.CatalogPath{}, fmt.Errorf("native table locator requires namespace and table path")
	}
	return engineplugin.TabularItemPath(loc.EngineID, tabularNamespaceTerm(engine), namespace, table), nil
}

func nativeTableTargetPath(endpoint EndpointSpec, engine EngineBinding) (engineplugin.CatalogPath, error) {
	parent, err := endpoint.ParentResourceLocator()
	if err != nil {
		return engineplugin.CatalogPath{}, err
	}
	if !isNativeTableParentType(parent.Type) {
		return engineplugin.CatalogPath{}, fmt.Errorf("native target parent_locator type must be %q or %q, got %q", resourcetree.TypeSchema, resourcetree.TypeDatabase, parent.Type)
	}
	namespace := strings.TrimSpace(parent.LastSegment())
	table := strings.TrimSpace(endpoint.Name)
	if namespace == "" || table == "" {
		return engineplugin.CatalogPath{}, fmt.Errorf("native target requires parent namespace and target name")
	}
	return engineplugin.TabularItemPath(parent.EngineID, tabularNamespaceTerm(engine), namespace, table), nil
}

func isNativeTableParentType(resourceType resourcetree.ResourceType) bool {
	return resourceType == resourcetree.TypeSchema || resourceType == resourcetree.TypeDatabase
}

func tabularNamespaceTerm(engine EngineBinding) string {
	engineType := strings.ToLower(strings.TrimSpace(effectiveEngineType(engine, EngineRef{})))
	if strings.Contains(engineType, "postgres") {
		return engineplugin.CatalogTermSchema
	}
	return engineplugin.CatalogTermDatabase
}

func targetEndpointContentCatalogPath(endpoint EndpointSpec, formatType format.FormatType, writeOptions *format.WriteOptions) (engineplugin.CatalogPath, error) {
	if IsInfraLocatorURI(endpoint.ParentLocator) {
		parent, err := endpoint.ParentInfraLocator()
		if err != nil {
			return engineplugin.CatalogPath{}, err
		}
		name := strings.TrimSpace(endpoint.Name)
		if name == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("target name is required")
		}
		if parent.Type != resourcetree.TypePrefix && parent.Type != resourcetree.TypeDirectory {
			return engineplugin.CatalogPath{}, fmt.Errorf("target infra parent_locator type must be %q or %q, got %q", resourcetree.TypePrefix, resourcetree.TypeDirectory, parent.Type)
		}
		objectPath := strings.Trim(pathpkg.Join(strings.Join(parent.Path, "/"), name), "/")
		normalizedPath, err := normalizeTargetContentPathExtension(objectPath, formatType, writeOptions)
		if err != nil {
			return engineplugin.CatalogPath{}, err
		}
		return engineplugin.ObjectItemPath(0, parent.Namespace, normalizedPath), nil
	}
	parent, err := endpoint.ParentResourceLocator()
	if err != nil {
		return engineplugin.CatalogPath{}, err
	}
	name := strings.TrimSpace(endpoint.Name)
	if name == "" {
		return engineplugin.CatalogPath{}, fmt.Errorf("target name is required")
	}
	resourceType := targetContentResourceTypeFromParent(parent.Type)
	switch resourceType {
	case resourcetree.TypeFile:
		path := strings.Trim(pathpkg.Join(parent.PathString(), name), "/")
		if path == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("target file path requires parent path or name")
		}
		normalizedPath, err := normalizeTargetContentPathExtension(path, formatType, writeOptions)
		if err != nil {
			return engineplugin.CatalogPath{}, err
		}
		return engineplugin.FileItemPath(parent.EngineID, normalizedPath), nil
	case resourcetree.TypeObject:
		bucket, prefix := objectParentLocatorParts(parent)
		if bucket == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("target object parent_locator requires bucket")
		}
		objectPath := strings.Trim(pathpkg.Join(prefix, name), "/")
		if objectPath == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("target object name is required")
		}
		normalizedPath, err := normalizeTargetContentPathExtension(objectPath, formatType, writeOptions)
		if err != nil {
			return engineplugin.CatalogPath{}, err
		}
		return engineplugin.ObjectItemPath(parent.EngineID, bucket, normalizedPath), nil
	default:
		return engineplugin.CatalogPath{}, fmt.Errorf("target encoded parent_locator type must be content container, got %q", parent.Type)
	}
}

func endpointContentCatalogPath(endpoint EndpointSpec, role string) (engineplugin.CatalogPath, error) {
	return endpointContentCatalogPathWithTargetFormat(endpoint, role, "", nil)
}

func endpointContentCatalogPathWithTargetFormat(endpoint EndpointSpec, role string, formatType format.FormatType, writeOptions *format.WriteOptions) (engineplugin.CatalogPath, error) {
	loc, err := endpoint.ResourceLocator()
	if err != nil {
		return engineplugin.CatalogPath{}, err
	}
	switch loc.Type {
	case resourcetree.TypeFile:
		path := engineplugin.NormalizeFileCatalogPath(loc.PathString())
		if path == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("%s file locator requires path", role)
		}
		if role == "target" && formatType != "" {
			normalizedPath, err := normalizeTargetContentPathExtension(path, formatType, writeOptions)
			if err != nil {
				return engineplugin.CatalogPath{}, err
			}
			path = normalizedPath
		}
		return engineplugin.FileItemPath(loc.EngineID, path), nil
	case resourcetree.TypeObject:
		bucket, objectPath := objectLocatorParts(loc)
		if bucket == "" || objectPath == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("%s object locator requires bucket and path", role)
		}
		if role == "target" && formatType != "" {
			normalizedPath, err := normalizeTargetContentPathExtension(objectPath, formatType, writeOptions)
			if err != nil {
				return engineplugin.CatalogPath{}, err
			}
			objectPath = normalizedPath
		}
		return engineplugin.ObjectItemPath(loc.EngineID, bucket, objectPath), nil
	default:
		return engineplugin.CatalogPath{}, fmt.Errorf("unsupported %s locator type %q", role, loc.Type)
	}
}

func objectLocatorParts(loc *resourcetree.ResourceLocator) (string, string) {
	if loc == nil || len(loc.Path) == 0 {
		return "", ""
	}
	bucket := strings.Trim(loc.Path[0], "/")
	objectPath := ""
	if len(loc.Path) > 1 {
		objectPath = strings.Trim(strings.Join(loc.Path[1:], "/"), "/")
	}
	return bucket, objectPath
}

func objectParentLocatorParts(loc *resourcetree.ResourceLocator) (string, string) {
	if loc == nil || len(loc.Path) == 0 {
		return "", ""
	}
	bucket := strings.Trim(loc.Path[0], "/")
	prefix := ""
	if len(loc.Path) > 1 {
		prefix = strings.Trim(strings.Join(loc.Path[1:], "/"), "/")
	}
	return bucket, prefix
}

func targetContentResourceTypeFromParent(parentType resourcetree.ResourceType) resourcetree.ResourceType {
	switch parentType {
	case resourcetree.TypeBucket, resourcetree.TypePrefix, resourcetree.TypeService:
		return resourcetree.TypeObject
	case resourcetree.TypeRoot, resourcetree.TypeDirectory, resourcetree.TypeDir:
		return resourcetree.TypeFile
	default:
		return ""
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
	if descriptor.DataType != datatype.Table {
		if !hasTableTransferReader(formatType) {
			_, err := format.GetTableReaderProvider(formatType)
			return fmt.Errorf("format %q has no table reader provider: %w", formatType, err)
		}
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
	if _, err := format.GetScopeTableReaderProvider(formatType); err == nil {
		return true
	}
	return false
}

func validateTransferWritableTableFormat(formatType format.FormatType) error {
	descriptor, ok := format.GetFormatDescriptor(formatType)
	if !ok {
		return fmt.Errorf("format %q is not registered", formatType)
	}
	if descriptor.DataType != datatype.Table {
		if !hasTableExportWriter(formatType) {
			_, err := format.GetTableWriterProvider(formatType)
			return fmt.Errorf("format %q has no table writer provider: %w", formatType, err)
		}
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

func readOptionsForGeoJSONTarget(formatType format.FormatType, writeOptions *format.WriteOptions, spatialInfo *datatype.SpatialInfo) map[string]interface{} {
	if formatType != format.FormatGeoJSON {
		return nil
	}
	options := map[string]interface{}{
		engineplugin.TableReadHintGeometryEncoding:        string(format.GeometryEncodingGeoJSON),
		engineplugin.TableReadHintGeometryTargetSRID:      4326,
		engineplugin.TableReadHintGeometryTransformPolicy: "required",
	}
	if writeOptions != nil && writeOptions.ExtraParams != nil {
		if geometryField := stringValue(writeOptions.ExtraParams, "geometry_field"); geometryField != "" {
			options[engineplugin.TableReadHintGeometryField] = geometryField
		}
	}
	if spatialInfo != nil {
		if geometryField := strings.TrimSpace(spatialInfo.PrimaryGeometryName()); geometryField != "" {
			options[engineplugin.TableReadHintGeometryField] = geometryField
		}
	}
	return options
}

func buildSpatialGeoJSONTransform(sourcePlan *executor.TableSourcePlan, targetPlan executor.TableTargetPlan, sourceEngine EngineBinding) (*executor.TableTransformPlan, error) {
	if sourcePlan == nil || targetPlan.Kind != executor.TableEndpointEncoded || targetPlan.Format != format.FormatGeoJSON {
		return nil, nil
	}
	spatialInfo := sourcePlan.SpatialInfo
	if spatialInfo == nil || spatialInfo.PrimaryGeometryName() == "" {
		return nil, nil
	}

	geometryColumn := strings.TrimSpace(spatialInfo.PrimaryGeometryName())
	sourceCRS := strings.TrimSpace(spatialInfo.PrimaryCRSRef())
	if sourceCRS == "" {
		if srid := spatialInfo.PrimarySRIDValue(); srid > 0 {
			sourceCRS = fmt.Sprintf("EPSG:%d", srid)
		}
	}
	if sourceCRS == "" {
		return nil, fmt.Errorf("source CRS is required for GeoJSON export")
	}
	targetCRS := "EPSG:4326"

	if sourcePlan.Kind == executor.TableEndpointNative && engineSupportsSpatialTransform(sourceEngine) {
		sourcePlan.ReadOptions = mergeReadOptions(sourcePlan.ReadOptions, readOptionsForGeoJSONTarget(targetPlan.Format, targetPlan.FormatOptions, spatialInfo))
		sourcePlan.SpatialInfo = spatialInfoForTargetCRS(spatialInfo, geometryColumn, targetCRS)
		return nil, nil
	}

	if spatialCRSEquivalent(sourceCRS, targetCRS) {
		if nativeEncoding, ok := encodedNativeGeometryPassthroughEncoding(sourcePlan, targetPlan); ok {
			if sourcePlan.ParseOptions == nil {
				sourcePlan.ParseOptions = format.DefaultParseOptions()
			}
			sourcePlan.ParseOptions.GeometryEncoding = nativeEncoding
			if sourcePlan.ParseOptions.ExtraParams == nil {
				sourcePlan.ParseOptions.ExtraParams = map[string]interface{}{}
			}
			sourcePlan.ParseOptions.ExtraParams["geometry_field"] = geometryColumn
		}
		return nil, nil
	}

	if sourcePlan.Kind == executor.TableEndpointNative {
		return nil, fmt.Errorf("source engine does not support spatial transform for GeoJSON export")
	}

	if sourcePlan.ParseOptions == nil {
		sourcePlan.ParseOptions = format.DefaultParseOptions()
	}
	sourcePlan.ParseOptions.GeometryEncoding = format.GeometryEncodingEWKB
	if sourcePlan.ParseOptions.ExtraParams == nil {
		sourcePlan.ParseOptions.ExtraParams = map[string]interface{}{}
	}
	sourcePlan.ParseOptions.ExtraParams["geometry_field"] = geometryColumn

	return &executor.TableTransformPlan{
		Type: "spatial_reproject",
		SpatialReproject: &executor.SpatialReprojectTransformPlan{
			GeometryColumn: geometryColumn,
			SourceCRS:      sourceCRS,
			TargetCRS:      targetCRS,
			Reproject:      true,
		},
	}, nil
}

func spatialInfoForTargetCRS(source *datatype.SpatialInfo, geometryColumn, targetCRS string) *datatype.SpatialInfo {
	next := source.Clone()
	if next == nil {
		next = datatype.NewSingleGeometrySpatialInfo(geometryColumn, "", 0, 0)
	}
	geometryColumn = strings.TrimSpace(geometryColumn)
	if geometryColumn == "" {
		geometryColumn = strings.TrimSpace(next.PrimaryGeometryName())
	}
	if geometryColumn != "" {
		next.PrimaryGeometryColumn = geometryColumn
	}
	srid := commonSpatial.ParseSRID(targetCRS)
	next.CRSRef = targetCRS
	if srid > 0 {
		next.SRID = &srid
	}
	next.CRSDefinitions = nil
	next.Extent = nil
	next.HasSpatialIndex = nil
	next.IndexName = ""

	if len(next.GeometryColumns) == 0 {
		next.GeometryColumns = []datatype.GeometryColumnInfo{{Name: geometryColumn}}
	}
	primaryIndex := 0
	for i := range next.GeometryColumns {
		if strings.EqualFold(strings.TrimSpace(next.GeometryColumns[i].Name), geometryColumn) {
			primaryIndex = i
			break
		}
	}
	next.GeometryColumns[primaryIndex].Name = geometryColumn
	next.GeometryColumns[primaryIndex].CRSRef = targetCRS
	if srid > 0 {
		next.GeometryColumns[primaryIndex].SRID = &srid
	}
	return next
}

func spatialCRSEquivalent(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	if strings.EqualFold(left, right) {
		return true
	}
	leftSRID := commonSpatial.ParseSRID(left)
	rightSRID := commonSpatial.ParseSRID(right)
	return leftSRID > 0 && leftSRID == rightSRID
}

func engineSupportsSpatialTransform(engine EngineBinding) bool {
	capabilities := effectiveEngineCapabilities(engine)
	if capabilities == nil || capabilities.Storage == nil || capabilities.Storage.Store == nil {
		return false
	}
	store := capabilities.Storage.Store
	return (store.TableSpatialEncoding != nil && store.TableSpatialEncoding.ReadTransform) || store.TableReadSpatialTransform
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
