// Package shapefile provides a metadata extractor for ESRI Shapefile format.
//
// This is an example of a third-party extractor plugin for ADDP Meta module.
// It demonstrates how to build an extractor without access to ADDP source code.
package shapefile

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	sdk "github.com/addp/meta-extractor-sdk"
)

// ShapefileExtractor extracts metadata from ESRI Shapefile format
type ShapefileExtractor struct{}

// SupportedTypes returns the MIME types this extractor supports
func (e *ShapefileExtractor) SupportedTypes() []string {
	return []string{
		"application/x-shapefile",
		"application/octet-stream", // Shapefiles may not have specific MIME type
	}
}

// Priority returns the priority of this extractor
// Use higher priority (90) since we want to match before generic handlers
func (e *ShapefileExtractor) Priority() int {
	return 90
}

// Extract extracts metadata from a Shapefile
func (e *ShapefileExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
	// Check if this is actually a shapefile by extension
	ext := strings.ToLower(filepath.Ext(input.ObjectKey))
	if ext != ".shp" && ext != ".shx" && ext != ".dbf" {
		return nil, fmt.Errorf("not a shapefile: %s", input.ObjectKey)
	}

	// Create metadata structure
	metadata := sdk.NewMetadata(
		filepath.Base(input.ObjectKey),
		"ESRI Shapefile",
		input.Size,
	)
	metadata.BasicInfo.ContentType = input.ContentType
	metadata.BasicInfo.LastModified = input.LastModified
	metadata.BasicInfo.ETag = input.ETag

	// If we have a reader, extract detailed metadata
	if input.Reader != nil {
		if err := e.extractDetailedMetadata(input, metadata); err != nil {
			// Log warning but don't fail - return basic metadata
			metadata.CustomAttrs["extraction_error"] = err.Error()
		}
	}

	return metadata, nil
}

// extractDetailedMetadata extracts detailed information from shapefile
func (e *ShapefileExtractor) extractDetailedMetadata(input sdk.ExtractInput, metadata *sdk.Metadata) error {
	// Only process .shp files (main geometry file)
	ext := strings.ToLower(filepath.Ext(input.ObjectKey))
	if ext != ".shp" {
		return fmt.Errorf("detailed extraction only for .shp files")
	}

	// Read shapefile header (100 bytes)
	header := make([]byte, 100)
	n, err := input.Reader.Read(header)
	if err != nil || n < 100 {
		return fmt.Errorf("failed to read shapefile header: %w", err)
	}

	// Parse basic information from header
	// Shapefile header structure (bytes):
	// 0-3: File code (9994)
	// 24-27: File length
	// 28-31: Version (1000)
	// 32-35: Shape type
	// 36-67: Bounding box (8 doubles: minX, minY, maxX, maxY, minZ, maxZ, minM, maxM)

	// Extract shape type (byte 32-35, little endian)
	shapeType := int32(header[32]) | int32(header[33])<<8 | int32(header[34])<<16 | int32(header[35])<<24

	// Extract bounding box (bytes 36-67, little endian doubles)
	// For simplicity, we'll just note that a proper implementation would parse these

	// Extract geometric information
	geoMeta := &sdk.GeoSpatialMetadata{
		GeometryType:     e.getGeometryTypeName(shapeType),
		FeatureCount:     0, // Would need to count records in the file
		Dimensions:       2,
		CoordinateSystem: "Unknown (check .prj file)",
		SpatialIndex:     false,
	}

	// Note: Full extraction would require parsing the entire file
	metadata.CustomAttrs["prj_file_required"] = true
	metadata.CustomAttrs["associated_files_required"] = []string{
		filepath.Base(strings.Replace(input.ObjectKey, ".shp", ".dbf", 1)),
		filepath.Base(strings.Replace(input.ObjectKey, ".shp", ".shx", 1)),
		filepath.Base(strings.Replace(input.ObjectKey, ".shp", ".prj", 1)),
	}

	// Add geo-spatial metadata using the typed metadata system
	metadata.AddTypedMetadata("geo_spatial", geoMeta)

	metadata.CustomAttrs["note"] = "Full metadata extraction requires access to all shapefile components"

	return nil
}

// Note: The go-shp library doesn't support reading from io.Reader directly.
// In a production implementation, you would either:
// 1. Use a different library that supports io.Reader
// 2. Write to a temporary file and use go-shp's file-based API
// 3. Implement a complete shapefile parser
//
// For this plugin example, we demonstrate manual header parsing
// which is sufficient to show the plugin architecture concept.

// getGeometryTypeName converts shape type to readable name
func (e *ShapefileExtractor) getGeometryTypeName(shapeType int32) string {
	switch shapeType {
	case 0:
		return "Null"
	case 1:
		return "Point"
	case 3:
		return "PolyLine"
	case 5:
		return "Polygon"
	case 8:
		return "MultiPoint"
	case 11:
		return "PointZ"
	case 13:
		return "PolyLineZ"
	case 15:
		return "PolygonZ"
	case 18:
		return "MultiPointZ"
	case 21:
		return "PointM"
	case 23:
		return "PolyLineM"
	case 25:
		return "PolygonM"
	case 28:
		return "MultiPointM"
	case 31:
		return "MultiPatch"
	default:
		return fmt.Sprintf("Unknown (%d)", shapeType)
	}
}

// ExtractFromMultipleFiles extracts metadata when you have access to all shapefile components
// This is a helper method for extractors that can access .shp, .dbf, .prj files together
func ExtractFromMultipleFiles(ctx context.Context, shpReader io.Reader, dbfReader io.Reader, prjReader io.Reader, objectKey string, size int64) (*sdk.Metadata, error) {
	// This would be a more complete implementation
	// For now, just demonstrate the structure
	metadata := sdk.NewMetadata(
		filepath.Base(objectKey),
		"ESRI Shapefile (Complete)",
		size,
	)

	// Extract from .shp file
	// ... (geometry information)

	// Extract from .dbf file
	// ... (attribute table schema and data)

	// Extract from .prj file
	// ... (coordinate reference system)

	return metadata, nil
}
