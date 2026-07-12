package planner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/transfer/internal/executor"
)

type RawCopyTaskSpec struct {
	Runtime RuntimeSpec  `json:"runtime"`
	Load    LoadSpec     `json:"load"`
	Source  EndpointSpec `json:"source"`
	Target  EndpointSpec `json:"target"`
}

type RawCopyBuildResult struct {
	SourceEngineType string
	TargetEngineType string
	Plan             executor.RawCopyPlan
}

func ParseRawCopyTaskSpec(config map[string]interface{}) (RawCopyTaskSpec, error) {
	if config == nil {
		return RawCopyTaskSpec{}, fmt.Errorf("transfer task config is required")
	}
	if hasLegacyTaskConfigFields(config) {
		return RawCopyTaskSpec{}, fmt.Errorf("legacy transfer task config is not supported; use source/target endpoint config")
	}
	if hasUnsupportedEndpointAttributes(config) {
		return RawCopyTaskSpec{}, fmt.Errorf("endpoint attributes are not supported in transfer task config; use source locator item_id to reference Meta item attributes")
	}
	var spec RawCopyTaskSpec
	configBytes, err := json.Marshal(config)
	if err != nil {
		return RawCopyTaskSpec{}, fmt.Errorf("marshal raw copy task config: %w", err)
	}
	if err := decodeStrictTaskConfig(configBytes, &spec, "raw copy"); err != nil {
		return RawCopyTaskSpec{}, err
	}
	if err := validateRawCopySpec(&spec); err != nil {
		return RawCopyTaskSpec{}, err
	}
	return spec, nil
}

func BuildRawCopyPlan(spec RawCopyTaskSpec, resolver EngineResolver) (*RawCopyBuildResult, error) {
	if resolver == nil {
		return nil, fmt.Errorf("engine resolver is required")
	}
	if err := validateRawCopySpec(&spec); err != nil {
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
	sourceDescriptor, _ := sourceItemDescriptorFromMetaAttributes(spec.Source.Attributes)
	sourcePath, err := sourceEndpointContentCatalogPath(spec.Source, sourceDescriptor)
	if err != nil {
		return nil, fmt.Errorf("build raw copy source path: %w", err)
	}
	targetPath, err := targetEndpointContentCatalogPath(spec.Target, "", nil)
	if err != nil {
		return nil, fmt.Errorf("build raw copy target path: %w", err)
	}
	return &RawCopyBuildResult{
		SourceEngineType: sourceType,
		TargetEngineType: targetType,
		Plan: executor.RawCopyPlan{
			Source: executor.RawCopyEndpointPlan{
				ConnInfo: sourceEngine.ConnInfo,
				Path:     sourcePath,
			},
			Target: executor.RawCopyEndpointPlan{
				ConnInfo:          targetEngine.ConnInfo,
				Path:              targetPath,
				ContentWrite:      engineWriteOptionsForRawCopy(spec.Target.Policy),
				DeleteBeforeWrite: applyMode(spec.Target.Policy) == applyModeReplace,
			},
			DataType: spec.Source.DataType,
			Format:   spec.Source.Format,
		},
	}, nil
}

func validateRawCopySpec(spec *RawCopyTaskSpec) error {
	if spec == nil {
		return fmt.Errorf("raw copy spec is required")
	}
	if spec.Runtime.Boundary != runtimeBoundaryBounded || spec.Load.Mode != loadModeSnapshot || spec.Load.ChangeDetection != nil {
		return fmt.Errorf("raw copy requires runtime.boundary=bounded and load.mode=snapshot")
	}
	if err := validateRawCopyEndpoint(spec.Source, "source"); err != nil {
		return err
	}
	normalizeRawCopyTarget(&spec.Target, spec.Source)
	if err := validateRawCopyEndpoint(spec.Target, "target"); err != nil {
		return err
	}
	if spec.Source.DataType != spec.Target.DataType {
		return fmt.Errorf("raw copy target data type %q must match source data type %q", spec.Target.DataType, spec.Source.DataType)
	}
	if spec.Source.Format != spec.Target.Format {
		return fmt.Errorf("raw copy target format %q must match source format %q", spec.Target.Format, spec.Source.Format)
	}
	if applyMode(spec.Target.Policy) != applyModeReplace {
		return fmt.Errorf("raw copy only supports target policy.apply_mode=replace")
	}
	if len(spec.Source.Attributes) > 0 {
		descriptor, ok := sourceItemDescriptorFromMetaAttributes(spec.Source.Attributes)
		if !ok {
			return fmt.Errorf("source meta item attributes do not contain item descriptor")
		}
		if descriptor.DataType != "" && string(descriptor.DataType) != spec.Source.DataType {
			return fmt.Errorf("source data type %q conflicts with Meta item data type %q", spec.Source.DataType, descriptor.DataType)
		}
		if descriptor.Format != "" && format.FormatType(descriptor.Format) != spec.Source.Format {
			return fmt.Errorf("source format %q conflicts with Meta item format %q", spec.Source.Format, descriptor.Format)
		}
		if descriptor.Layout != "" && descriptor.Layout != format.LayoutSingle {
			return fmt.Errorf("raw copy requires source layout=%q, got %q", format.LayoutSingle, descriptor.Layout)
		}
	}
	return nil
}

func normalizeRawCopyTarget(target *EndpointSpec, source EndpointSpec) {
	if target == nil {
		return
	}
	if strings.TrimSpace(target.DataType) == "" {
		target.DataType = source.DataType
	}
	if target.Format == "" {
		target.Format = source.Format
	}
}

func validateRawCopyEndpoint(endpoint EndpointSpec, role string) error {
	if err := validateEndpointCommon(endpoint, role, strings.TrimSpace(endpoint.DataType)); err != nil {
		return err
	}
	if endpoint.Representation != representationEncoded {
		return fmt.Errorf("%s raw copy endpoint representation must be %q, got %q", role, representationEncoded, endpoint.Representation)
	}
	if !isRawCopyDataType(endpoint.DataType) {
		return fmt.Errorf("%s raw copy data type must be document, media, or unknown, got %q", role, endpoint.DataType)
	}
	if endpoint.Format == "" {
		return fmt.Errorf("%s raw copy format is required", role)
	}
	if endpoint.Format != format.FormatUnknown {
		descriptor, ok := format.GetFormatDescriptor(endpoint.Format)
		if !ok {
			return fmt.Errorf("%s raw copy format %q is not registered", role, endpoint.Format)
		}
		if descriptor.DataType != "" && string(descriptor.DataType) != endpoint.DataType && descriptor.DataType != datatype.Unknown {
			return fmt.Errorf("%s raw copy format %q default data type %q conflicts with endpoint data type %q", role, endpoint.Format, descriptor.DataType, endpoint.DataType)
		}
	}
	return nil
}

func isRawCopyDataType(dataType string) bool {
	switch strings.TrimSpace(dataType) {
	case string(datatype.Document), string(datatype.Media), string(datatype.Unknown):
		return true
	default:
		return false
	}
}

func engineWriteOptionsForRawCopy(policy map[string]interface{}) engineplugin.WriteOptions {
	return engineplugin.WriteOptions{Overwrite: false}
}
