package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"reflect"
	"strconv"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	commonExecution "github.com/addp/common/execution"
	commonjson "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/repository"
)

type rasterCOGMetaClient interface {
	GetItemSnapshot(context.Context, uint, uint, uint, string) (*commonModels.MetaItem, error)
}

func (s *RasterCOGTaskService) SetSourceAuthorizationClients(meta rasterCOGMetaClient, engines model3DGLBSourceEngineClient, issuer *commonClient.SystemExecutionAuthorizationClient, consumer *commonClient.SystemServiceClient) {
	s.sourceMeta, s.sourceEngines, s.sourceIssuer, s.sourceConsumer = meta, engines, issuer, consumer
}

// Classification uses persisted Meta identity. A task cannot opt out of ENVI's
// two-file boundary by labelling its raw data TIFF, IMG or JPEG2000.
func (s *RasterCOGTaskService) sourceSnapshot(ctx context.Context, tenant uint, config RasterCOGExecutionConfig) (*commonModels.MetaItem, error) {
	if s == nil || s.sourceMeta == nil {
		return nil, engineaccess.ErrUnavailable
	}
	item, err := s.sourceMeta.GetItemSnapshot(ctx, tenant, config.Target.SourceEngineID, config.Target.ItemID, config.Target.FullName)
	if errors.Is(err, commonAPI.ErrNotFound) && config.Raster.SourceFormat != "envi" {
		return nil, nil
	}
	if err != nil {
		return nil, engineaccess.ErrUnavailable
	}
	if item == nil || item.TenantID != tenant || item.EngineID != config.Target.SourceEngineID || item.FullName != config.Target.FullName || item.Fingerprint != config.Target.ItemFingerprint || (config.Target.ItemID != 0 && item.ID != config.Target.ItemID) {
		return nil, ErrFileSourceAuthorizationRequired
	}
	format := commonjson.String(item.Attributes, "item", "format")
	if format != config.Raster.SourceFormat {
		return nil, ErrFileSourceAuthorizationRequired
	}
	return item, nil
}

func enviRasterVersion(item *commonModels.MetaItem) (string, string, error) {
	if item == nil {
		return "", "", ErrFileSourceAuthorizationRequired
	}
	header := commonjson.String(item.Attributes, "format_info.envi", "header_sha256")
	raster := commonjson.String(item.Attributes, "format_info.envi", "raster_sha256")
	if !authorizationfacts.ValidItemFingerprint(header) || !authorizationfacts.ValidItemFingerprint(raster) {
		return "", "", ErrFileSourceAuthorizationRequired
	}
	return header, raster, nil
}

func validateENVIRasterConfig(item *commonModels.MetaItem, actual RasterCOGRasterConfig) error {
	if item == nil {
		return ErrFileSourceAuthorizationRequired
	}
	facts := RasterQuickViewSourceFromAttributes(item.Attributes, "", item.EngineID)
	if facts == nil || facts.Format != "envi" || len(facts.Extent) != 4 || (facts.SourceSRID <= 0 && facts.SourceCRS == "") {
		return ErrFileSourceAuthorizationRequired
	}
	expected := RasterCOGRasterConfig{SourceFormat: "envi", SourceProfile: facts.Profile, SourceSizeBytes: facts.SizeBytes, HeaderSHA256: facts.HeaderSHA256, RasterSHA256: facts.RasterSHA256, Width: facts.Width, Height: facts.Height, BandCount: facts.BandCount, SourceSRID: facts.SourceSRID, SourceCRS: facts.SourceCRS, Extent: facts.Extent, ExtentSRID: facts.ExtentSRID}
	if !reflect.DeepEqual(expected, actual) {
		return ErrFileSourceAuthorizationRequired
	}
	return nil
}

func (s *RasterCOGTaskService) freezeENVISourceExecution(ctx context.Context, snapshot *repository.FileTaskSnapshot, execution *commonExecution.TaskExecution, credential string) (bool, error) {
	if snapshot == nil || execution == nil {
		return false, ErrFileSourceAuthorizationRequired
	}
	object, err := commonjson.DecodeObjectStrict(snapshot.Config, commonExecution.MaxExecutionConfigBytes, 64)
	if err != nil {
		return false, ErrFileSourceAuthorizationRequired
	}
	for _, key := range []string{"config_version", "owner_task", "source_engine_version", "source_composition"} {
		if _, found := object[key]; found {
			return false, ErrFileSourceAuthorizationRequired
		}
	}
	config := commonModels.JSONMap(object)
	cfg, err := readRasterCOGExecutionConfig(config, s.bucket, snapshot.TenantID)
	if err != nil {
		return false, err
	}
	item, err := s.sourceSnapshot(ctx, snapshot.TenantID, cfg)
	if err != nil || cfg.Raster.SourceFormat != "envi" {
		return false, err
	}
	if execution.ParentExecutionID != nil || execution.Source != commonExecution.ModuleManager || execution.TriggerType != commonExecution.TriggerTypeManual || credential == "" {
		return true, ErrFileSourceAuthorizationRequired
	}
	if err := commonExecution.ApplyUserOrigin(ctx, execution); err != nil {
		return true, err
	}
	if s.sourceEngines == nil || s.sourceIssuer == nil || s.sourceConsumer == nil {
		return true, engineaccess.ErrUnavailable
	}
	engine, err := s.sourceEngines.GetEngineForTenant(ctx, snapshot.TenantID, cfg.Target.SourceEngineID)
	if err != nil || engineaccess.EnsureAvailable(engine) != nil {
		return true, engineaccess.ErrUnavailable
	}
	composition, err := enviCompositionFromMeta(snapshot.TenantID, cfg.Target.SourceEngineID, cfg.Target.ItemID, cfg.Target.ItemFingerprint, cfg.Target.Locator, item)
	if err != nil || engine.ID != cfg.Target.SourceEngineID || engine.TenantID == nil || *engine.TenantID != snapshot.TenantID || engine.Version <= 0 || !commonExecution.ManagerFileSourceComposition(engine.EngineType, composition) {
		return true, ErrFileSourceAuthorizationRequired
	}
	if err := validateENVIRasterConfig(item, cfg.Raster); err != nil {
		return true, err
	}
	header, raster, err := enviRasterVersion(item)
	if err != nil || header != cfg.Raster.HeaderSHA256 || raster != cfg.Raster.RasterSHA256 {
		return true, ErrFileSourceAuthorizationRequired
	}
	name := safeCOGFileName(defaultRasterCOGFileName(cfg.Target.FullName))
	config["result"] = commonModels.JSONMap{"target_kind": models.RasterCOGTargetKindMinIO, "storage_ref": rastercogref.ObjectStorageRef(s.bucket, fmt.Sprintf("tenant_%d/cog/%s/%s", snapshot.TenantID, cfg.Target.ItemFingerprint, name)), "file_name": name}
	config["config_version"] = commonExecution.ManagerRasterCOGConfigVersion
	config["owner_task"] = map[string]string{"id": strconv.FormatUint(uint64(snapshot.TaskID), 10), "version": strconv.FormatUint(uint64(snapshot.Version), 10)}
	config["source_engine_version"] = strconv.FormatInt(engine.Version, 10)
	config["source_composition"] = composition
	execution.ExecutionConfig = config
	return true, nil
}

// Read the actual persisted execution JSON, not the mutable task definition.
func (s *RasterCOGTaskService) prepareENVISourceClaim(ctx context.Context, execution *commonExecution.TaskExecution) (RasterCOGExecutionConfig, *rasterCOGSourceAuthorization, error) {
	var cfg RasterCOGExecutionConfig
	if execution == nil {
		return cfg, nil, ErrFileSourceAuthorizationRequired
	}
	raw, err := s.repo.RawExecutionConfig(ctx, execution.TenantID, execution.ExecutionID)
	if err != nil {
		return cfg, nil, err
	}
	object, err := commonjson.DecodeObjectStrict(raw, commonExecution.MaxExecutionConfigBytes, 64)
	if err != nil {
		return cfg, nil, ErrFileSourceAuthorizationRequired
	}
	cfg, err = readRasterCOGExecutionConfig(commonModels.JSONMap(object), s.bucket, uint(execution.TenantID))
	if err != nil {
		return cfg, nil, err
	}
	item, err := s.sourceSnapshot(ctx, uint(execution.TenantID), cfg)
	if err != nil {
		return cfg, nil, err
	}
	if cfg.Raster.SourceFormat != "envi" {
		if object["config_version"] != nil || execution.ExecutionAuthorizationID != nil {
			return cfg, nil, ErrFileSourceAuthorizationRequired
		}
		cfg.sourceItem = item
		return cfg, nil, nil
	}
	composition, err := enviCompositionFromMeta(uint(execution.TenantID), cfg.Target.SourceEngineID, cfg.Target.ItemID, cfg.Target.ItemFingerprint, cfg.Target.Locator, item)
	if err != nil {
		return cfg, nil, err
	}
	if err := validateENVIRasterConfig(item, cfg.Raster); err != nil {
		return cfg, nil, err
	}
	header, raster, err := enviRasterVersion(item)
	if err != nil || header != cfg.Raster.HeaderSHA256 || raster != cfg.Raster.RasterSHA256 {
		return cfg, nil, ErrFileSourceAuthorizationRequired
	}
	var bound struct {
		Composition *authorizationfacts.FileCompositionBoundary `json:"source_composition"`
	}
	if json.Unmarshal(raw, &bound) != nil || !reflect.DeepEqual(bound.Composition, composition) {
		return cfg, nil, ErrFileSourceAuthorizationRequired
	}
	auth, err := newRasterCOGSourceAuthorization(ctx, s.sourceConsumer, execution, raw, cfg, composition)
	if err == nil {
		err = auth.Checkpoint(ctx)
	}
	return cfg, auth, err
}
