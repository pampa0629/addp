package service

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"

	"github.com/addp/common/dataitem/authorizationfacts"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type model3DGLBFrozenHeader struct {
	Version   string `json:"config_version"`
	OwnerTask struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	} `json:"owner_task"`
	EngineVersion string                                      `json:"source_engine_version"`
	Composition   *authorizationfacts.FileCompositionBoundary `json:"source_composition"`
}

func freezeFileModel3DGLBConfig(snapshot *repository.Model3DGLBTaskSnapshot, engine *commonModels.Engine, candidate *model3DGLBSourceCandidate, executionID, bucket string) (commonModels.JSONMap, error) {
	reject := ErrModel3DGLBSourceAuthorizationRequired
	if snapshot == nil || snapshot.TaskID == 0 || snapshot.Version == 0 || snapshot.TenantID == 0 || engine == nil || !commonExecution.ManagerModel3DSourceEngineType(engine.EngineType) || engine.Version <= 0 || engine.TenantID == nil || *engine.TenantID != snapshot.TenantID || candidate == nil || candidate.composition == nil || !commonExecution.ManagerModel3DSourceComposition(engine.EngineType, candidate.composition) {
		return nil, reject
	}
	conversion, err := decodeFileModel3DGLBConversion(snapshot.Config)
	if err != nil || conversion.Source.SourceEngineID != engine.ID {
		return nil, reject
	}
	primary, err := candidate.composition.PrimaryTarget()
	locator, locatorErr := resourcetree.ParseURI(conversion.Source.ItemLocator)
	if err != nil || locatorErr != nil || primary.StringPath() != locator.FullName() || candidate.composition.Format != conversion.Source.Format {
		return nil, reject
	}
	target, err := model3DGLBExecutionOutput(snapshot.TenantID, executionID, bucket, conversion.Result.FileName)
	if err != nil {
		return nil, err
	}
	object, err := jsonmap.DecodeObjectStrict(snapshot.Config, commonExecution.MaxExecutionConfigBytes, 64)
	if err != nil {
		return nil, reject
	}
	config := commonModels.JSONMap(object)
	for _, reserved := range []string{"config_version", "owner_task", "source_engine_version", "source_composition"} {
		if _, exists := config[reserved]; exists {
			return nil, reject
		}
	}
	result, ok := config["result"].(map[string]interface{})
	if !ok {
		return nil, reject
	}
	result["storage_ref"], result["file_name"] = target.StorageRef, target.FileName
	config["config_version"] = models.Model3DGLBFileConfigVersion
	config["owner_task"] = map[string]string{"id": strconv.FormatUint(uint64(snapshot.TaskID), 10), "version": strconv.FormatUint(uint64(snapshot.Version), 10)}
	config["source_engine_version"] = strconv.FormatInt(engine.Version, 10)
	config["source_composition"] = candidate.composition.Clone()
	return config, nil
}

func decodeModel3DGLBFrozenHeader(raw json.RawMessage) (*model3DGLBFrozenHeader, error) {
	var header model3DGLBFrozenHeader
	if json.Unmarshal(raw, &header) != nil || header.Version != models.Model3DGLBFileConfigVersion || header.Composition == nil {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	for _, value := range []string{header.OwnerTask.ID, header.OwnerTask.Version, header.EngineVersion} {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != value {
			return nil, ErrModel3DGLBSourceAuthorizationRequired
		}
	}
	primary, err := header.Composition.PrimaryTarget()
	if err != nil || header.Composition.Validate(primary.EngineID) != nil {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	return &header, nil
}

// Read the signed conversion values directly from SQL JSON. JSONMap.Clone and
// the editable task normalizer are deliberately absent: either could round
// large option integers or rewrite the already frozen source/result values.
func decodeFileModel3DGLBConversion(raw json.RawMessage) (Model3DGLBExecutionConfig, error) {
	var wire struct {
		ConfigVersion string `json:"config_version"`
		Source        struct {
			ItemLocator     string `json:"item_locator"`
			SourceEngineID  uint   `json:"source_engine_id"`
			ItemFingerprint string `json:"item_fingerprint"`
			ItemID          uint   `json:"item_id"`
			Format          string `json:"format"`
			SourceSizeBytes int64  `json:"source_size_bytes"`
		} `json:"source"`
		Result struct {
			StorageRef string `json:"storage_ref"`
			FileName   string `json:"file_name"`
		} `json:"result"`
		Options json.RawMessage `json:"options"`
	}
	if len(raw) == 0 || len(raw) > commonExecution.MaxExecutionConfigBytes || json.Unmarshal(raw, &wire) != nil {
		return Model3DGLBExecutionConfig{}, ErrModel3DGLBSourceAuthorizationRequired
	}
	if wire.Source.ItemLocator == "" || wire.Source.SourceEngineID == 0 || wire.Source.ItemID == 0 || wire.Source.ItemFingerprint == "" ||
		!commonExecution.ManagerModel3DConversionFormat(wire.Source.Format) || wire.Result.StorageRef == "" || wire.Result.FileName == "" {
		return Model3DGLBExecutionConfig{}, ErrModel3DGLBSourceAuthorizationRequired
	}
	var result Model3DGLBExecutionConfig
	result.ConfigVersion = wire.ConfigVersion
	decoder := json.NewDecoder(bytes.NewReader(wire.Options))
	decoder.UseNumber()
	if decoder.Decode(&result.Options) != nil || result.Options == nil || decoder.Decode(new(any)) != io.EOF {
		return result, ErrModel3DGLBSourceAuthorizationRequired
	}
	result.Source = Model3DGLBSourceConfig{ItemLocator: wire.Source.ItemLocator, SourceEngineID: wire.Source.SourceEngineID, ItemFingerprint: wire.Source.ItemFingerprint, ItemID: wire.Source.ItemID, Format: wire.Source.Format, SourceSizeBytes: wire.Source.SourceSizeBytes}
	result.Result = Model3DGLBResultConfig{StorageRef: wire.Result.StorageRef, FileName: wire.Result.FileName}
	return result, nil
}
