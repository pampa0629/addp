package tilepyramid

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

const (
	ManifestFileName      = "tiles.addp.json"
	ManifestSchemaVersion = "addp.tile_pyramid.v1"
	DefaultMatrixSet      = "WebMercatorQuad"
	SchemeXYZ             = "xyz"
	TileKindVector        = "vector"
	TileKindRaster        = "raster"
)

var supportedFormats = map[string]string{
	"mvt":  "application/vnd.mapbox-vector-tile",
	"png":  "image/png",
	"jpeg": "image/jpeg",
	"webp": "image/webp",
}

type Manifest struct {
	SchemaVersion   string   `json:"schema_version"`
	DataType        string   `json:"data_type"`
	Format          string   `json:"format"`
	Layout          string   `json:"layout"`
	DatasetName     string   `json:"dataset_name,omitempty"`
	TileKind        string   `json:"tile_kind"`
	TileFormat      string   `json:"tile_format"`
	Scheme          string   `json:"scheme"`
	TileMatrixSet   string   `json:"tile_matrix_set"`
	TileTemplate    string   `json:"tile_template"`
	ContentType     string   `json:"content_type"`
	ContentEncoding string   `json:"content_encoding,omitempty"`
	MinZoom         int      `json:"min_zoom"`
	MaxZoom         int      `json:"max_zoom"`
	TileCount       int64    `json:"tile_count,omitempty"`
	TotalSizeBytes  int64    `json:"total_size_bytes,omitempty"`
	Spatial         *Spatial `json:"spatial,omitempty"`
}

type Spatial struct {
	SRID   int       `json:"srid,omitempty"`
	CRSRef string    `json:"crs_ref,omitempty"`
	Extent []float64 `json:"extent,omitempty"`
}

func DecodeManifest(reader io.Reader, maxBytes int64) (Manifest, error) {
	if reader == nil {
		return Manifest{}, fmt.Errorf("tile pyramid manifest reader is required")
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	limited := io.LimitReader(reader, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return Manifest{}, fmt.Errorf("read tile pyramid manifest: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return Manifest{}, fmt.Errorf("tile pyramid manifest exceeds %d bytes", maxBytes)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse tile pyramid manifest: %w", err)
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return normalizeManifest(manifest), nil
}

func ValidateManifest(manifest Manifest) error {
	if strings.TrimSpace(manifest.SchemaVersion) != ManifestSchemaVersion {
		return fmt.Errorf("unsupported tile pyramid schema_version %q", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.DataType) != "media" || strings.TrimSpace(manifest.Format) != "tile_pyramid" || strings.TrimSpace(manifest.Layout) != "whole" {
		return fmt.Errorf("tile pyramid manifest identity must be media/tile_pyramid/whole")
	}
	tileKind := strings.ToLower(strings.TrimSpace(manifest.TileKind))
	if tileKind != TileKindVector && tileKind != TileKindRaster {
		return fmt.Errorf("tile_kind must be vector or raster")
	}
	tileFormat := strings.ToLower(strings.TrimSpace(manifest.TileFormat))
	if _, ok := supportedFormats[tileFormat]; !ok {
		return fmt.Errorf("unsupported tile_format %q", manifest.TileFormat)
	}
	if tileKind == TileKindVector && tileFormat != "mvt" {
		return fmt.Errorf("vector tile pyramid requires tile_format=mvt")
	}
	if tileKind == TileKindRaster && tileFormat == "mvt" {
		return fmt.Errorf("raster tile pyramid cannot use tile_format=mvt")
	}
	if strings.ToLower(strings.TrimSpace(manifest.Scheme)) != SchemeXYZ {
		return fmt.Errorf("tile pyramid scheme must be xyz")
	}
	if strings.TrimSpace(manifest.TileMatrixSet) != DefaultMatrixSet {
		return fmt.Errorf("tile_matrix_set must be %s", DefaultMatrixSet)
	}
	if err := validateTemplate(manifest.TileTemplate); err != nil {
		return err
	}
	if manifest.MinZoom < 0 || manifest.MaxZoom < manifest.MinZoom || manifest.MaxZoom > 30 {
		return fmt.Errorf("invalid zoom range [%d,%d]", manifest.MinZoom, manifest.MaxZoom)
	}
	if encoding := strings.ToLower(strings.TrimSpace(manifest.ContentEncoding)); encoding != "" && encoding != "gzip" {
		return fmt.Errorf("unsupported content_encoding %q", manifest.ContentEncoding)
	}
	if manifest.TileCount < 0 || manifest.TotalSizeBytes < 0 {
		return fmt.Errorf("tile_count and total_size_bytes cannot be negative")
	}
	if manifest.Spatial != nil && len(manifest.Spatial.Extent) != 0 && len(manifest.Spatial.Extent) != 4 {
		return fmt.Errorf("spatial.extent must contain four numbers")
	}
	return nil
}

func ResolveTilePath(template string, z, x, y int) (string, error) {
	if z < 0 || x < 0 || y < 0 {
		return "", fmt.Errorf("tile coordinates cannot be negative")
	}
	if err := validateTemplate(template); err != nil {
		return "", err
	}
	resolved := strings.NewReplacer(
		"{z}", strconv.Itoa(z),
		"{x}", strconv.Itoa(x),
		"{y}", strconv.Itoa(y),
	).Replace(strings.TrimSpace(template))
	cleaned := path.Clean(resolved)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("resolved tile path escapes dataset scope")
	}
	return cleaned, nil
}

func ContentType(tileFormat, declared string) string {
	if value := strings.TrimSpace(declared); value != "" {
		return value
	}
	return supportedFormats[strings.ToLower(strings.TrimSpace(tileFormat))]
}

func normalizeManifest(manifest Manifest) Manifest {
	manifest.TileKind = strings.ToLower(strings.TrimSpace(manifest.TileKind))
	manifest.TileFormat = strings.ToLower(strings.TrimSpace(manifest.TileFormat))
	manifest.Scheme = SchemeXYZ
	manifest.TileMatrixSet = DefaultMatrixSet
	manifest.TileTemplate = strings.TrimSpace(manifest.TileTemplate)
	manifest.ContentType = ContentType(manifest.TileFormat, manifest.ContentType)
	manifest.ContentEncoding = strings.ToLower(strings.TrimSpace(manifest.ContentEncoding))
	manifest.DatasetName = strings.TrimSpace(manifest.DatasetName)
	return manifest
}

func validateTemplate(template string) error {
	template = strings.TrimSpace(template)
	if template == "" || strings.HasPrefix(template, "/") {
		return fmt.Errorf("tile_template must be a relative path")
	}
	for _, token := range []string{"{z}", "{x}", "{y}"} {
		if strings.Count(template, token) != 1 {
			return fmt.Errorf("tile_template must contain %s exactly once", token)
		}
	}
	withoutTokens := strings.NewReplacer("{z}", "0", "{x}", "0", "{y}", "0").Replace(template)
	if clean := path.Clean(withoutTokens); clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("tile_template cannot escape dataset scope")
	}
	if strings.Contains(withoutTokens, "{") || strings.Contains(withoutTokens, "}") {
		return fmt.Errorf("tile_template contains unsupported placeholders")
	}
	return nil
}
