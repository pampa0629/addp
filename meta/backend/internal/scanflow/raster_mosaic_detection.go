package scanflow

import (
	"context"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/format/rastermosaic"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaitem"
)

const rasterMosaicManifestMaxBytes int64 = 1 << 20

func detectRasterMosaicCompositeItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	resources []metacatalog.StorageResource,
	claimed map[string]bool,
) ([]metacatalog.ObjectCatalogCompositeItem, []ObjectCatalogCompositeDetectionError) {
	if contentReader == nil || len(resources) == 0 {
		return nil, nil
	}
	items := make([]metacatalog.ObjectCatalogCompositeItem, 0)
	warnings := make([]ObjectCatalogCompositeDetectionError, 0)
	for _, resource := range resources {
		if resource.NodeType != plugin.CatalogKindObject || claimed[resource.Path] {
			continue
		}
		if path.Base(strings.Trim(resource.Path, "/")) != rastermosaic.ManifestFileName {
			continue
		}
		manifest, err := readRasterMosaicManifest(ctx, contentReader, connInfo, engineID, resource)
		if err != nil {
			warnings = append(warnings, ObjectCatalogCompositeDetectionError{
				Bucket: resource.RootName,
				Prefix: strings.Trim(path.Dir(strings.Trim(resource.Path, "/")), "/"),
				Err:    err,
			})
			continue
		}
		prefix := strings.Trim(path.Dir(strings.Trim(resource.Path, "/")), "/")
		group := resourcesUnderObjectPrefix(resources, resource.RootName, prefix)
		if len(group) == 0 {
			continue
		}
		claims := metaitem.ResourceClaimSet{}
		totalSize := int64(0)
		for _, member := range group {
			claims[member.Path] = true
			claimed[member.Path] = true
			totalSize += member.SizeBytes
		}
		items = append(items, metacatalog.ObjectCatalogCompositeItem{
			Bucket: resource.RootName,
			Prefix: prefix,
			Item:   rasterMosaicDetectedItem(prefix, resource.Path, manifest, totalSize),
			Claims: claims,
		})
	}
	return items, warnings
}

func readRasterMosaicManifest(ctx context.Context, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, resource metacatalog.StorageResource) (rastermosaic.Manifest, error) {
	reader, err := contentReader.OpenContent(ctx, connInfo, plugin.ObjectItemPath(engineID, resource.RootName, resource.Path), plugin.ReadOptions{})
	if err != nil {
		return rastermosaic.Manifest{}, fmt.Errorf("open raster mosaic manifest: %w", err)
	}
	defer reader.Close()
	manifest, err := rastermosaic.DecodeManifest(reader, rasterMosaicManifestMaxBytes)
	if err != nil {
		return rastermosaic.Manifest{}, fmt.Errorf("decode raster mosaic manifest: %w", err)
	}
	return manifest, nil
}

func rasterMosaicDetectedItem(prefix, manifestPath string, manifest rastermosaic.Manifest, totalSize int64) *metaitem.DetectedItem {
	formatInfo := map[string]interface{}{
		"manifest_ref":    rastermosaic.ManifestFileName,
		"index_ref":       strings.TrimSpace(manifest.Refs.Index),
		"overview_ref":    strings.TrimSpace(manifest.Refs.Overview),
		"leaf_count":      manifest.Summary.LeafCount,
		"source_count":    manifest.Summary.SourceCount,
		"failed_count":    manifest.Summary.FailedCount,
		"overview_width":  manifest.Summary.OverviewWidth,
		"overview_height": manifest.Summary.OverviewHeight,
		"vrt_width":       manifest.Summary.VRTWidth,
		"vrt_height":      manifest.Summary.VRTHeight,
	}
	attrs := map[string]interface{}{
		"format_info": map[string]interface{}{
			string(format.FormatRasterMosaic): formatInfo,
		},
		"type_info": map[string]interface{}{
			"media": map[string]interface{}{
				"kind":     string(datatype.MediaKindImage),
				"encoding": string(format.FormatRasterMosaic),
				"width":    manifest.Summary.OverviewWidth,
				"height":   manifest.Summary.OverviewHeight,
			},
		},
	}
	spatial := map[string]interface{}{}
	sourceCRS := strings.TrimSpace(manifest.Summary.SourceCRS)
	if sourceCRS == "" && looksLikeGeographicExtent(manifest.Summary.Extent) {
		sourceCRS = "EPSG:4326"
	}
	if srid := sridFromCRS(sourceCRS); srid > 0 {
		spatial["srid"] = srid
		spatial["extent_srid"] = srid
	}
	if sourceCRS != "" {
		spatial["crs_ref"] = sourceCRS
	}
	if len(manifest.Summary.Extent) == 4 {
		spatial["extent"] = manifest.Summary.Extent
	}
	capabilities := map[string]interface{}{}
	if len(spatial) > 0 {
		capabilities["spatial"] = spatial
	}
	if len(manifest.Capabilities) > 0 {
		capabilities["raster_mosaic"] = manifest.Capabilities
	}
	if len(capabilities) > 0 {
		attrs["capabilities"] = capabilities
	}
	refs := []dataitem.ItemRef{{
		Role:      "manifest",
		Path:      manifestPath,
		Required:  true,
		Primary:   true,
		Extension: ".json",
	}}
	if manifest.Refs.Index != "" {
		refs = append(refs, dataitem.ItemRef{Role: "index", Path: path.Join(prefix, manifest.Refs.Index), Required: true, Extension: ".json"})
	}
	if manifest.Refs.Overview != "" {
		refs = append(refs, dataitem.ItemRef{Role: "overview", Path: path.Join(prefix, manifest.Refs.Overview), Required: true, Extension: ".tif"})
	}
	return &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             format.LayoutWhole,
			DataType:           datatype.Media,
			Format:             string(format.FormatRasterMosaic),
			PrimaryContentPath: manifestPath,
			ScopePath:          prefix,
			SizeBytes:          &totalSize,
			RefList:            refs,
		},
		PhysicalPath: prefix,
		Attributes:   attrs,
	}
}

func resourcesUnderObjectPrefix(resources []metacatalog.StorageResource, bucket, prefix string) []metacatalog.StorageResource {
	prefix = strings.Trim(prefix, "/")
	result := make([]metacatalog.StorageResource, 0)
	for _, resource := range resources {
		if resource.RootName != bucket || resource.NodeType != plugin.CatalogKindObject {
			continue
		}
		resourcePath := strings.Trim(resource.Path, "/")
		if resourcePath == prefix || strings.HasPrefix(resourcePath, prefix+"/") {
			result = append(result, resource)
		}
	}
	return result
}

func sridFromCRS(value string) int {
	value = strings.TrimSpace(strings.ToUpper(value))
	if !strings.HasPrefix(value, "EPSG:") {
		return 0
	}
	srid, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(value, "EPSG:")))
	return srid
}

func looksLikeGeographicExtent(extent []float64) bool {
	if len(extent) != 4 {
		return false
	}
	for _, value := range extent {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return -180 <= extent[0] && extent[0] < extent[2] && extent[2] <= 180 &&
		-90 <= extent[1] && extent[1] < extent[3] && extent[3] <= 90
}
