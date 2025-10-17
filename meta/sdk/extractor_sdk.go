// Package sdk provides the SDK for building third-party metadata extractors for ADDP Meta module.
//
// Third-party developers can import this package to create custom extractors
// without needing access to the entire ADDP codebase.
package sdk

import (
	"context"
	"io"
	"time"
)

// ExtractorSDKVersion is the current version of the extractor SDK
const ExtractorSDKVersion = "1.0.0"

// MetadataExtractor is the interface that all extractors must implement.
//
// Example implementation:
//
//	type MyExtractor struct{}
//
//	func (e *MyExtractor) SupportedTypes() []string {
//	    return []string{"application/x-my-format"}
//	}
//
//	func (e *MyExtractor) Priority() int {
//	    return 80
//	}
//
//	func (e *MyExtractor) Extract(ctx context.Context, input ExtractInput) (*Metadata, error) {
//	    // Your extraction logic here
//	    return &Metadata{...}, nil
//	}
type MetadataExtractor interface {
	// SupportedTypes returns a list of MIME types this extractor supports.
	// Supports wildcards: "image/*", "*/*"
	SupportedTypes() []string

	// Extract extracts metadata from the given input.
	// Returns structured metadata or an error.
	Extract(ctx context.Context, input ExtractInput) (*Metadata, error)

	// Priority returns the priority of this extractor (higher number = higher priority).
	// Useful when multiple extractors support the same MIME type.
	// Built-in extractors use priorities: 100 (high), 50 (medium), -100 (default fallback)
	Priority() int
}

// ExtractInput contains all input data for metadata extraction
type ExtractInput struct {
	ResourceID   uint              // Data source ID from system.resources
	ObjectKey    string            // Object key or file path (e.g., "bucket/path/to/file.shp")
	ContentType  string            // MIME type (e.g., "application/x-shapefile")
	Size         int64             // File size in bytes
	Reader       io.Reader         // Content reader (may be nil for lightweight extraction)
	Metadata     map[string]string // Basic metadata from storage system (S3/MinIO user metadata)
	LastModified time.Time         // Last modified timestamp
	ETag         string            // ETag or version identifier
}

// Metadata is the result of metadata extraction
type Metadata struct {
	BasicInfo   BasicMetadata          // Basic file information (required)
	SchemaInfo  *SchemaMetadata        // Schema information for structured data (optional)
	PreviewData interface{}            // Preview data for display (optional)
	CustomAttrs map[string]interface{} // Custom attributes specific to file type (optional)
}

// BasicMetadata contains basic file information
type BasicMetadata struct {
	FileName     string    // Base file name
	FileType     string    // Human-readable file type (e.g., "Shapefile", "GeoJSON")
	Size         int64     // File size in bytes
	ContentType  string    // MIME type
	Encoding     string    // Character encoding (e.g., "UTF-8")
	LastModified time.Time // Last modified timestamp
	Checksum     string    // Checksum (MD5, SHA256, etc.)
	ETag         string    // ETag from storage
}

// SchemaMetadata describes the structure of tabular/structured data
type SchemaMetadata struct {
	Columns    []ColumnInfo               // Column definitions
	RowCount   int64                      // Total number of rows (-1 if unknown)
	SampleData []map[string]interface{}   // Sample rows (first N rows)
	Extra      map[string]interface{}     // Additional schema-specific info
}

// ColumnInfo describes a single column/field
type ColumnInfo struct {
	Name        string      // Column name
	Type        string      // Data type (string, int, float, bool, geometry, date, etc.)
	Nullable    bool        // Whether null values are allowed
	Description string      // Column description (optional)
	Example     interface{} // Example value (optional)
}

// TypedMetadata is an interface for type-specific metadata structures.
// Implement this interface to create custom metadata types that can be
// automatically serialized and validated.
type TypedMetadata interface {
	// TypeName returns the unique identifier for this metadata type
	// Example: "geo.spatial", "image.basic", "document.pdf"
	TypeName() string

	// Schema returns the JSON Schema for this metadata type
	// This enables validation and documentation
	Schema() map[string]interface{}

	// ToMap converts the metadata to a map for JSON serialization
	ToMap() map[string]interface{}

	// FromMap populates the metadata from a map
	FromMap(map[string]interface{}) error
}

// GeoSpatialMetadata describes geospatial data (common across GeoJSON, Shapefile, KML, etc.)
type GeoSpatialMetadata struct {
	GeometryType     string    // Point, LineString, Polygon, MultiPoint, etc.
	CoordinateSystem string    // Coordinate reference system (e.g., "EPSG:4326")
	BoundingBox      []float64 // [minX, minY, maxX, maxY] or [minX, minY, minZ, maxX, maxY, maxZ]
	FeatureCount     int       // Number of features
	Dimensions       int       // 2D, 3D, etc.
	SpatialIndex     bool      // Whether spatial index exists
	Attributes       []string  // List of attribute field names
}

func (g *GeoSpatialMetadata) TypeName() string {
	return "geo.spatial"
}

func (g *GeoSpatialMetadata) Schema() map[string]interface{} {
	return map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"geometry_type":      map[string]string{"type": "string"},
			"coordinate_system":  map[string]string{"type": "string"},
			"bounding_box":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "number"}},
			"feature_count":      map[string]string{"type": "integer"},
			"dimensions":         map[string]string{"type": "integer"},
			"spatial_index":      map[string]string{"type": "boolean"},
			"attributes":         map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
		},
	}
}

func (g *GeoSpatialMetadata) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"geometry_type":      g.GeometryType,
		"coordinate_system":  g.CoordinateSystem,
		"bounding_box":       g.BoundingBox,
		"feature_count":      g.FeatureCount,
		"dimensions":         g.Dimensions,
		"spatial_index":      g.SpatialIndex,
		"attributes":         g.Attributes,
	}
}

func (g *GeoSpatialMetadata) FromMap(m map[string]interface{}) error {
	if v, ok := m["geometry_type"].(string); ok {
		g.GeometryType = v
	}
	if v, ok := m["coordinate_system"].(string); ok {
		g.CoordinateSystem = v
	}
	if v, ok := m["bounding_box"].([]interface{}); ok {
		g.BoundingBox = make([]float64, len(v))
		for i, val := range v {
			if f, ok := val.(float64); ok {
				g.BoundingBox[i] = f
			}
		}
	}
	if v, ok := m["feature_count"].(float64); ok {
		g.FeatureCount = int(v)
	}
	if v, ok := m["dimensions"].(float64); ok {
		g.Dimensions = int(v)
	}
	if v, ok := m["spatial_index"].(bool); ok {
		g.SpatialIndex = v
	}
	if v, ok := m["attributes"].([]interface{}); ok {
		g.Attributes = make([]string, len(v))
		for i, val := range v {
			if s, ok := val.(string); ok {
				g.Attributes[i] = s
			}
		}
	}
	return nil
}

// ImageMetadata describes image file metadata
type ImageMetadata struct {
	Width       int    // Width in pixels
	Height      int    // Height in pixels
	Format      string // Image format (JPEG, PNG, TIFF, etc.)
	ColorSpace  string // Color space (RGB, CMYK, Grayscale, etc.)
	BitDepth    int    // Bits per channel
	HasAlpha    bool   // Whether alpha channel exists
	Compression string // Compression method
	DPI         int    // Dots per inch
}

func (i *ImageMetadata) TypeName() string {
	return "image.basic"
}

func (i *ImageMetadata) Schema() map[string]interface{} {
	return map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"width":       map[string]string{"type": "integer"},
			"height":      map[string]string{"type": "integer"},
			"format":      map[string]string{"type": "string"},
			"color_space": map[string]string{"type": "string"},
			"bit_depth":   map[string]string{"type": "integer"},
			"has_alpha":   map[string]string{"type": "boolean"},
			"compression": map[string]string{"type": "string"},
			"dpi":         map[string]string{"type": "integer"},
		},
	}
}

func (i *ImageMetadata) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"width":       i.Width,
		"height":      i.Height,
		"format":      i.Format,
		"color_space": i.ColorSpace,
		"bit_depth":   i.BitDepth,
		"has_alpha":   i.HasAlpha,
		"compression": i.Compression,
		"dpi":         i.DPI,
	}
}

func (i *ImageMetadata) FromMap(m map[string]interface{}) error {
	if v, ok := m["width"].(float64); ok {
		i.Width = int(v)
	}
	if v, ok := m["height"].(float64); ok {
		i.Height = int(v)
	}
	if v, ok := m["format"].(string); ok {
		i.Format = v
	}
	if v, ok := m["color_space"].(string); ok {
		i.ColorSpace = v
	}
	if v, ok := m["bit_depth"].(float64); ok {
		i.BitDepth = int(v)
	}
	if v, ok := m["has_alpha"].(bool); ok {
		i.HasAlpha = v
	}
	if v, ok := m["compression"].(string); ok {
		i.Compression = v
	}
	if v, ok := m["dpi"].(float64); ok {
		i.DPI = int(v)
	}
	return nil
}

// DocumentMetadata describes document file metadata
type DocumentMetadata struct {
	Title        string    // Document title
	Author       string    // Author name
	Subject      string    // Document subject
	Keywords     []string  // Keywords/tags
	Creator      string    // Creating application
	Producer     string    // PDF producer
	CreationDate time.Time // Creation date
	ModifiedDate time.Time // Last modified date
	PageCount    int       // Number of pages
	WordCount    int       // Number of words
	Language     string    // Document language
}

func (d *DocumentMetadata) TypeName() string {
	return "document.basic"
}

func (d *DocumentMetadata) Schema() map[string]interface{} {
	return map[string]interface{}{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]interface{}{
			"title":         map[string]string{"type": "string"},
			"author":        map[string]string{"type": "string"},
			"subject":       map[string]string{"type": "string"},
			"keywords":      map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
			"creator":       map[string]string{"type": "string"},
			"producer":      map[string]string{"type": "string"},
			"creation_date": map[string]string{"type": "string", "format": "date-time"},
			"modified_date": map[string]string{"type": "string", "format": "date-time"},
			"page_count":    map[string]string{"type": "integer"},
			"word_count":    map[string]string{"type": "integer"},
			"language":      map[string]string{"type": "string"},
		},
	}
}

func (d *DocumentMetadata) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"title":         d.Title,
		"author":        d.Author,
		"subject":       d.Subject,
		"keywords":      d.Keywords,
		"creator":       d.Creator,
		"producer":      d.Producer,
		"creation_date": d.CreationDate,
		"modified_date": d.ModifiedDate,
		"page_count":    d.PageCount,
		"word_count":    d.WordCount,
		"language":      d.Language,
	}
}

func (d *DocumentMetadata) FromMap(m map[string]interface{}) error {
	if v, ok := m["title"].(string); ok {
		d.Title = v
	}
	if v, ok := m["author"].(string); ok {
		d.Author = v
	}
	if v, ok := m["subject"].(string); ok {
		d.Subject = v
	}
	if v, ok := m["keywords"].([]interface{}); ok {
		d.Keywords = make([]string, len(v))
		for i, val := range v {
			if s, ok := val.(string); ok {
				d.Keywords[i] = s
			}
		}
	}
	if v, ok := m["creator"].(string); ok {
		d.Creator = v
	}
	if v, ok := m["producer"].(string); ok {
		d.Producer = v
	}
	if v, ok := m["page_count"].(float64); ok {
		d.PageCount = int(v)
	}
	if v, ok := m["word_count"].(float64); ok {
		d.WordCount = int(v)
	}
	if v, ok := m["language"].(string); ok {
		d.Language = v
	}
	return nil
}
