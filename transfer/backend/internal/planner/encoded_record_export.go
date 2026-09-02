package planner

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resourcetree"
	"github.com/addp/transfer/internal/executor"
)

const dataTypeUnknown = "unknown"

type EncodedRecordExportTaskSpec struct {
	Runtime   RuntimeSpec  `json:"runtime"`
	Load      LoadSpec     `json:"load"`
	Source    EndpointSpec `json:"source"`
	Target    EndpointSpec `json:"target"`
	BatchSize int          `json:"batch_size,omitempty"`
}

type EncodedRecordExportBuildResult struct {
	SourceEngineType string
	TargetEngineType string
	Plan             executor.EncodedRecordExportPlan
}

func ParseEncodedRecordExportTaskSpec(config map[string]interface{}, fallbackBatchSize int) (EncodedRecordExportTaskSpec, error) {
	if config == nil {
		return EncodedRecordExportTaskSpec{}, fmt.Errorf("transfer task config is required")
	}
	if hasLegacyTaskConfigFields(config) {
		return EncodedRecordExportTaskSpec{}, fmt.Errorf("legacy transfer task config is not supported; use source/target endpoint config")
	}
	if hasUnsupportedEndpointAttributes(config) {
		return EncodedRecordExportTaskSpec{}, fmt.Errorf("endpoint attributes are not supported in transfer task config; use source locator item_id to reference Meta item attributes")
	}
	configBytes, err := json.Marshal(config)
	if err != nil {
		return EncodedRecordExportTaskSpec{}, fmt.Errorf("marshal encoded record export task config: %w", err)
	}
	var spec EncodedRecordExportTaskSpec
	if err := decodeStrictTaskConfig(configBytes, &spec, "encoded record export"); err != nil {
		return EncodedRecordExportTaskSpec{}, err
	}
	if spec.BatchSize <= 0 {
		spec.BatchSize = fallbackBatchSize
	}
	if spec.BatchSize <= 0 {
		spec.BatchSize = 1000
	}
	if err := validateEncodedRecordExportSpec(spec); err != nil {
		return EncodedRecordExportTaskSpec{}, err
	}
	return spec, nil
}

func BuildEncodedRecordExportPlan(spec EncodedRecordExportTaskSpec, resolver EngineResolver) (*EncodedRecordExportBuildResult, error) {
	if resolver == nil {
		return nil, fmt.Errorf("engine resolver is required")
	}
	if err := validateEncodedRecordExportSpec(spec); err != nil {
		return nil, err
	}
	sourceRef, err := spec.Source.EngineRef()
	if err != nil {
		return nil, fmt.Errorf("parse encoded record source locator: %w", err)
	}
	targetRef, err := spec.Target.EngineRef()
	if err != nil {
		return nil, fmt.Errorf("parse encoded record target parent locator: %w", err)
	}
	sourceEngine, err := resolver.ResolveEngine(sourceRef)
	if err != nil {
		return nil, fmt.Errorf("resolve encoded record source engine: %w", err)
	}
	targetEngine, err := resolver.ResolveEngine(targetRef)
	if err != nil {
		return nil, fmt.Errorf("resolve encoded record target engine: %w", err)
	}
	if !engineSupportsEncodedRecordFormat(sourceEngine, string(spec.Target.Format)) {
		return nil, fmt.Errorf("source engine does not support encoded record format %q", spec.Target.Format)
	}
	sourcePath, err := encodedRecordSourcePath(spec.Source, sourceEngine)
	if err != nil {
		return nil, fmt.Errorf("build encoded record source path: %w", err)
	}
	targetPath, err := targetEndpointContentCatalogPath(spec.Target, spec.Target.Format, nil)
	if err != nil {
		return nil, fmt.Errorf("build encoded record target path: %w", err)
	}
	return &EncodedRecordExportBuildResult{
		SourceEngineType: effectiveEngineType(sourceEngine, sourceRef),
		TargetEngineType: effectiveEngineType(targetEngine, targetRef),
		Plan: executor.EncodedRecordExportPlan{
			Source: executor.EncodedRecordExportEndpointPlan{
				ConnInfo: sourceEngine.ConnInfo,
				Path:     sourcePath,
			},
			Target: executor.EncodedRecordExportEndpointPlan{
				ConnInfo:          targetEngine.ConnInfo,
				Path:              targetPath,
				ContentWrite:      engineplugin.WriteOptions{Overwrite: false},
				DeleteBeforeWrite: true,
			},
			Format:    string(spec.Target.Format),
			BatchSize: spec.BatchSize,
		},
	}, nil
}

func validateEncodedRecordExportSpec(spec EncodedRecordExportTaskSpec) error {
	if spec.Runtime.Boundary != runtimeBoundaryBounded || spec.Load.Mode != loadModeSnapshot || spec.Load.ChangeDetection != nil {
		return fmt.Errorf("encoded record export requires runtime.boundary=bounded and load.mode=snapshot")
	}
	if err := validateSourceEndpointIdentity(spec.Source, "source", dataTypeUnknown); err != nil {
		return err
	}
	if spec.Source.Representation != representationNative {
		return fmt.Errorf("encoded record source representation must be %q", representationNative)
	}
	if spec.Source.LocatorType() != resourcetree.TypeCollection {
		return fmt.Errorf("encoded record source locator type must be %q", resourcetree.TypeCollection)
	}
	if spec.Source.Format != "" || spec.Source.Query != nil || len(spec.Source.Options) > 0 || len(spec.Source.Policy) > 0 {
		return fmt.Errorf("encoded record source does not accept format, query, options, or policy")
	}
	if err := validateEndpointCommon(spec.Target, "target", dataTypeUnknown); err != nil {
		return err
	}
	if spec.Target.Representation != representationEncoded {
		return fmt.Errorf("encoded record target representation must be %q", representationEncoded)
	}
	if spec.Target.Format == "" {
		return fmt.Errorf("encoded record target format is required")
	}
	descriptor, ok := format.GetFormatDescriptor(spec.Target.Format)
	if !ok {
		return fmt.Errorf("encoded record target format %q is not registered", spec.Target.Format)
	}
	if descriptor.DataType != datatype.Unknown {
		return fmt.Errorf("encoded record target format %q must have unknown data type", spec.Target.Format)
	}
	if len(spec.Target.Options) > 0 || spec.Target.Query != nil {
		return fmt.Errorf("encoded record target does not accept options or query")
	}
	if applyMode(spec.Target.Policy) != applyModeReplace {
		return fmt.Errorf("encoded record target policy.apply_mode must be replace")
	}
	if spec.BatchSize <= 0 {
		return fmt.Errorf("encoded record batch_size must be positive")
	}
	return nil
}

func encodedRecordSourcePath(endpoint EndpointSpec, engine EngineBinding) (engineplugin.EngineCatalogPath, error) {
	loc, err := endpoint.ResourceLocator()
	if err != nil {
		return engineplugin.EngineCatalogPath{}, err
	}
	capabilities := effectiveEngineCapabilities(engine)
	if capabilities == nil || capabilities.Storage == nil || capabilities.Storage.CatalogModel == nil {
		return engineplugin.EngineCatalogPath{}, fmt.Errorf("source engine catalog model is required")
	}
	return resourcetree.EngineCatalogPathFromLocator(*capabilities.Storage.CatalogModel, loc)
}

func engineSupportsEncodedRecordFormat(engine EngineBinding, formatName string) bool {
	capabilities := effectiveEngineCapabilities(engine)
	if capabilities == nil || capabilities.Storage == nil || capabilities.Storage.Store == nil || capabilities.Storage.Store.EncodedRecordReadSession == nil {
		return false
	}
	for _, supported := range capabilities.Storage.Store.EncodedRecordReadSession.Formats {
		if strings.TrimSpace(supported) == strings.TrimSpace(formatName) {
			return true
		}
	}
	return false
}

// EncodedRecordSourceFields restores the current Meta structure used to
// validate a collection protection projection before any source record is read.
func EncodedRecordSourceFields(spec EncodedRecordExportTaskSpec) []datatype.FieldInfo {
	info := sourceTableInfoFromMetaAttributes(spec.Source.Attributes, nil)
	if info == nil {
		return nil
	}
	return append([]datatype.FieldInfo(nil), info.Fields...)
}
