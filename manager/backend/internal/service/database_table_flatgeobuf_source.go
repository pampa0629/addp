package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/minio/minio-go/v7"
)

func (e *ManagerVectorTileCacheWorkflowExecutor) prepareDatabaseTableFlatGeobufSource(
	ctx context.Context,
	tenantID uint,
	executionID string,
	identity tileCacheTaskTargetIdentity,
	options commonModels.JSONMap,
) (string, commonModels.JSONMap, commonModels.JSONMap, func(context.Context) error, error) {
	noopCleanup := func(context.Context) error { return nil }
	engine, err := e.systemClient.GetEngineForTenant(ctx, tenantID, identity.EngineID)
	if err != nil {
		return "", nil, nil, noopCleanup, fmt.Errorf("get database table source engine: %w", err)
	}
	if err := engineaccess.EnsureAvailable(engine); err != nil {
		return "", nil, nil, noopCleanup, err
	}
	if engine.TenantID != nil && *engine.TenantID != tenantID {
		return "", nil, nil, noopCleanup, ErrEngineAccessDenied
	}
	engineType := strings.ToLower(strings.TrimSpace(engine.EngineType))
	if engineType != "mysql" && engineType != "oracle" {
		return "", nil, nil, noopCleanup, fmt.Errorf("database table engine %s does not support the FlatGeobuf workflow path", engine.EngineType)
	}
	plug, err := plugin.Get(engine.EngineType)
	if err != nil {
		return "", nil, nil, noopCleanup, err
	}
	sessionProvider, ok := plug.(plugin.TableReadSessionProvider)
	if !ok {
		return "", nil, nil, noopCleanup, fmt.Errorf("engine %s does not implement TableReadSessionProvider", engine.EngineType)
	}
	geometryColumn := strings.TrimSpace(stringFromConfig(options["geometry_column"]))
	if geometryColumn == "" {
		return "", nil, nil, noopCleanup, errors.New("database table FlatGeobuf source requires geometry_column")
	}
	if strings.TrimSpace(executionID) == "" {
		return "", nil, nil, noopCleanup, errors.New("database table FlatGeobuf source requires execution_id")
	}
	if strings.TrimSpace(e.defaultBucket) == "" {
		return "", nil, nil, noopCleanup, errors.New("Manager temporary source bucket is required")
	}
	catalogProvider, ok := plug.(plugin.CatalogModelProvider)
	if !ok {
		return "", nil, nil, noopCleanup, fmt.Errorf("engine %s does not declare a catalog model", engine.EngineType)
	}
	namespaceLevel, ok := plugin.CatalogFirstBusinessBranch(catalogProvider.CatalogModel())
	if !ok || strings.TrimSpace(namespaceLevel.Term) == "" {
		return "", nil, nil, noopCleanup, fmt.Errorf("engine %s does not declare a tabular namespace", engine.EngineType)
	}
	hints := map[string]interface{}{
		plugin.TableReadHintGeometryEncoding: string(format.GeometryEncodingEWKB),
		plugin.TableReadHintGeometryField:    geometryColumn,
	}
	attributes, err := stringSliceFromConfig(options["attributes"])
	if err != nil {
		return "", nil, nil, noopCleanup, err
	}
	if len(attributes) > 0 {
		selected := append([]string{geometryColumn}, attributes...)
		hints[format.FieldSelectionOptionKey] = &format.FieldSelectionOptions{Include: selected}
	}
	path := plugin.TabularItemPath(engine.ID, namespaceLevel.Term, identity.Schema, identity.Table)
	session, err := sessionProvider.OpenTableReadSession(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), path, plugin.TableReadSessionOptions{Hints: hints})
	if err != nil {
		return "", nil, nil, noopCleanup, fmt.Errorf("open %s FlatGeobuf source session: %w", engine.EngineType, err)
	}
	defer session.Close(context.Background())

	prefetchLimit := 512
	firstBatch, err := session.ReadBatch(ctx, prefetchLimit)
	if err != nil {
		return "", nil, nil, noopCleanup, fmt.Errorf("prefetch %s FlatGeobuf source: %w", engine.EngineType, err)
	}
	if firstBatch == nil || firstBatch.Spatial == nil {
		return "", nil, nil, noopCleanup, fmt.Errorf("%s FlatGeobuf source has no spatial facts", engine.EngineType)
	}
	geometryColumn = commonSpatial.ResolveFlatGeobufGeometryColumn(geometryColumn, firstBatch.Spatial, firstBatch.Fields)
	if geometryColumn == "" {
		return "", nil, nil, noopCleanup, fmt.Errorf("%s FlatGeobuf source geometry column is unavailable", engine.EngineType)
	}
	reader, flatOptions := commonSpatial.NewFlatGeobufBatchFeatureReader(
		func(readCtx context.Context, limit int) ([]map[string]interface{}, error) {
			batch, err := session.ReadBatch(readCtx, limit)
			if err != nil || batch == nil {
				return nil, err
			}
			return batch.Rows, nil
		},
		firstBatch.Rows,
		geometryColumn,
		firstBatch.Fields,
		firstBatch.Spatial,
		0,
	)
	flatOptions.Name = defaultVectorTileLayerName(identity)
	tempFile, err := os.CreateTemp("", "addp-manager-database-*.fgb")
	if err != nil {
		return "", nil, nil, noopCleanup, fmt.Errorf("create temporary %s FlatGeobuf: %w", engine.EngineType, err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	writeErr := commonSpatial.WriteFlatGeobuf(ctx, tempFile, reader, flatOptions)
	closeTempErr := tempFile.Close()
	if errors.Is(closeTempErr, os.ErrClosed) {
		closeTempErr = nil
	}
	if writeErr != nil || closeTempErr != nil {
		if writeErr != nil {
			writeErr = fmt.Errorf("materialize %s FlatGeobuf: %w", engine.EngineType, writeErr)
		}
		if closeTempErr != nil {
			closeTempErr = fmt.Errorf("close temporary %s FlatGeobuf: %w", engine.EngineType, closeTempErr)
		}
		return "", nil, nil, noopCleanup, errors.Join(writeErr, closeTempErr)
	}
	extent, hasExtent := reader.Extent()
	if !hasExtent {
		return "", nil, nil, noopCleanup, fmt.Errorf("%s FlatGeobuf source has no non-empty geometry extent", engine.EngineType)
	}
	file, err := os.Open(tempPath)
	if err != nil {
		return "", nil, nil, noopCleanup, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return "", nil, nil, noopCleanup, err
	}
	bucket := e.defaultBucket
	if err := e.ensureTargetBucket(ctx, bucket); err != nil {
		_ = file.Close()
		return "", nil, nil, noopCleanup, err
	}
	objectName := fmt.Sprintf("tenant_%d/executions/%s/source.fgb", tenantID, url.PathEscape(strings.TrimSpace(executionID)))
	_, putErr := e.objectStore.PutObject(ctx, bucket, objectName, file, info.Size(), minio.PutObjectOptions{ContentType: "application/flatgeobuf"})
	closeErr := file.Close()
	if putErr != nil || closeErr != nil {
		cleanupErr := e.objectStore.RemoveObject(context.Background(), bucket, objectName, minio.RemoveObjectOptions{})
		if putErr != nil {
			putErr = fmt.Errorf("upload temporary %s FlatGeobuf: %w", engine.EngineType, putErr)
		}
		if closeErr != nil {
			closeErr = fmt.Errorf("close temporary %s FlatGeobuf upload: %w", engine.EngineType, closeErr)
		}
		if cleanupErr != nil {
			cleanupErr = fmt.Errorf("clean failed %s FlatGeobuf upload: %w", engine.EngineType, cleanupErr)
		}
		return "", nil, nil, noopCleanup, errors.Join(putErr, closeErr, cleanupErr)
	}
	cleanup := func(cleanupCtx context.Context) error {
		return e.objectStore.RemoveObject(cleanupCtx, bucket, objectName, minio.RemoveObjectOptions{})
	}
	_, env, err := e.prepareTargetArchiveURI(bucket, objectName)
	if err != nil {
		cleanupErr := cleanup(context.Background())
		if cleanupErr != nil {
			cleanupErr = fmt.Errorf("clean unusable %s FlatGeobuf source: %w", engine.EngineType, cleanupErr)
		}
		return "", nil, nil, noopCleanup, errors.Join(err, cleanupErr)
	}
	facts := commonModels.JSONMap{
		"engine_type":      engine.EngineType,
		"access_method":    "temporary_flatgeobuf",
		"temporary":        true,
		"geometry_column":  geometryColumn,
		"source_srid":      flatOptions.SRID,
		"extent":           []float64{extent[0], extent[1], extent[2], extent[3]},
		"extent_srid":      flatOptions.SRID,
		"layer_name":       flatOptions.Name,
		"size_bytes":       info.Size(),
		"execution_scoped": true,
	}
	return "/vsis3/" + strings.Trim(bucket, "/") + "/" + strings.Trim(objectName, "/"), env, facts, cleanup, nil
}
