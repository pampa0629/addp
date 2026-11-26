package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/workflowaccess"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
	managerprotection "github.com/addp/manager/internal/protection"
)

const (
	GRIBTemperatureMapProfile   = "ecmwf_ifs_01_temperature/v1"
	GRIBPrecipitationMapProfile = "ecmwf_ifs_01_precipitation/v1"
)

func IsGRIBMapProfile(profile string) bool {
	return profile == GRIBTemperatureMapProfile || profile == GRIBPrecipitationMapProfile
}

func validGRIBMapMessage(name string) bool {
	number, err := strconv.Atoi(strings.TrimPrefix(name, "message-"))
	return err == nil && number > 0 && number <= 128 && name == "message-"+strconv.Itoa(number)
}

// GRIBRasterQuickViewSource selects only a message admitted by the committed
// native inspection. Execution independently repeats that inspection on bytes.
func GRIBRasterQuickViewSource(attrs map[string]interface{}, message string) *RasterQuickViewSource {
	if !validGRIBMapMessage(message) || commonjson.String(attrs, "item", "format") != "grib" ||
		commonjson.String(attrs, "item", "layout") != "single" || commonjson.String(attrs, "item", "data_type") != "container" {
		return nil
	}
	info := commonjson.Section(attrs, "format_info.grib")
	version, _ := info["source_version"].(string)
	if info["scan_complete"] != true || info["encoding"] != "GRIB2" || !format.ValidContainerSourceVersion(version) {
		return nil
	}
	var container datatype.ContainerInfo
	payload, err := json.Marshal(commonjson.Section(attrs, "type_info.container"))
	if err != nil || json.Unmarshal(payload, &container) != nil || container.ResourceCount != 1 {
		return nil
	}
	for _, child := range container.Children {
		profile, _ := child.Native["map_profile"].(string)
		if child.Name == message && child.Format == "grib" && child.ChildKind == "array" && IsGRIBMapProfile(profile) {
			return &RasterQuickViewSource{Format: "grib", Layout: "single", Profile: profile,
				Message: message, SourceVersion: version, Width: 4096, Height: 2048, BandCount: 1,
				SourceCRS: "ADDP:ECMWF_SPHERE_6371229", Extent: []float64{-180, -90, 180, 90}}
		}
	}
	return nil
}

func gribCOGVersionMatches(result *models.RasterCOG, raster *RasterQuickViewSource) bool {
	return result != nil && raster != nil && result.SourceProfile == raster.Profile &&
		commonjson.String(result.Metadata, "grib_map", "message") == raster.Message &&
		commonjson.String(result.Metadata, "grib_map", "source_version") == raster.SourceVersion
}

func (e *ManagerRasterCOGExecutor) withGRIBSource(ctx context.Context, req RasterCOGExecutionRequest, consume func(context.Context, workflowaccess.Source) error) error {
	auth := req.SourceAuthorization
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if e == nil || req.Task == nil || auth == nil || auth.validateConfig(req.Config) != nil || req.inputOwner == nil || consume == nil || !ok || lease.ExecutionID != req.ExecutionID || lease.TenantID != int(req.Task.TenantID) {
		return ErrFileSourceAuthorizationRequired
	}
	composition := auth.Composition()
	if composition == nil || composition.Format != "grib" || len(composition.Refs) != 1 {
		return ErrFileSourceAuthorizationRequired
	}
	target, err := composition.PrimaryTarget()
	if err != nil {
		return ErrFileSourceAuthorizationRequired
	}
	reader, err := newRasterSourceReader(ctx, auth, req.Task.TenantID, req.Config.Target.SourceEngineID)
	if err != nil {
		return err
	}
	size, err := reader.stat(ctx, target)
	if err != nil {
		return err
	}
	if size <= 0 || size > 128<<20 {
		return ErrFileSourceAuthorizationRequired
	}
	file := fileStagingFile{RelativePath: path.Base(target.StringPath()), Role: composition.PrimaryRole(), SizeBytes: size, Open: func(ctx context.Context) (io.ReadCloser, error) {
		stream, err := reader.open(ctx, target)
		if err != nil {
			return nil, err
		}
		return &fileVersionReader{ReadCloser: stream, digest: sha256.New(), expected: strings.TrimPrefix(req.Config.Raster.SourceVersion, "sha256:"), mismatch: ErrFileSourceAuthorizationRequired}, nil
	}}
	store, ok := e.objectStore.(fileInputObjectStore)
	if !ok {
		return ErrFileSourceAuthorizationUnavailable
	}
	stager := fileSourceStager{objectStore: store, minioEndpoint: e.infraEndpoint, minioAccessKey: e.infraAccessKey, minioSecretKey: e.infraSecretKey, minioUseSSL: e.infraUseSSL, defaultBucket: e.defaultBucket, taskType: commonExecution.TaskTypeRasterCOGGeneration}
	return stager.withSource(ctx, lease, "grib", workflowaccess.KindFile, file.RelativePath, []fileStagingFile{file}, req.inputOwner, func(ctx context.Context, source workflowaccess.Source) error {
		if err := auth.Checkpoint(ctx); err != nil {
			return err
		}
		return consume(ctx, source)
	})
}

func (s *RasterCOGTaskService) SetGRIBMapProtection(gate managerprotection.UnmanagedDataItemGate) {
	s.protection = gate
}

func (s *RasterCOGTaskService) checkGRIBMapProtection(ctx context.Context, tenant uint, fingerprint string) error {
	return managerprotection.RequireUnmanagedDataItem(ctx, s.protection, tenant, fingerprint, time.Now().UTC())
}

func gribMapCOGFileName(raster RasterCOGRasterConfig) string {
	return raster.Message + "-" + strings.TrimPrefix(raster.SourceVersion, "sha256:") + ".tif"
}
