# ADDP Metadata Extractor Plugin Development Guide

## Overview

This guide shows how to build custom metadata extractors for ADDP Meta module **without needing access to the ADDP source code**. You only need to import the SDK package and implement the `MetadataExtractor` interface.

## Quick Start

### 1. Set Up Your Plugin Project

```bash
mkdir my-custom-extractor
cd my-custom-extractor
go mod init github.com/yourusername/my-custom-extractor

# Add SDK dependency
go get github.com/addp/meta-extractor-sdk@latest
```

### 2. Implement the Extractor Interface

Create `extractor.go`:

```go
package mycustomextractor

import (
    "context"
    "io"
    "path/filepath"

    sdk "github.com/addp/meta-extractor-sdk"
)

type MyCustomExtractor struct{}

func (e *MyCustomExtractor) SupportedTypes() []string {
    return []string{"application/x-my-format"}
}

func (e *MyCustomExtractor) Priority() int {
    return 80 // Higher than default (50), lower than built-in (100)
}

func (e *MyCustomExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    // Create metadata structure
    metadata := sdk.NewMetadata(
        filepath.Base(input.ObjectKey),
        "My Custom Format",
        input.Size,
    )

    // Extract basic information
    metadata.BasicInfo.ContentType = input.ContentType
    metadata.BasicInfo.LastModified = input.LastModified

    // If content is available, extract detailed metadata
    if input.Reader != nil {
        content, err := io.ReadAll(input.Reader)
        if err != nil {
            return nil, err
        }

        // Parse your format and extract metadata
        // ...

        metadata.CustomAttrs["my_custom_field"] = "value"
    }

    return metadata, nil
}

// Export function for registration
func GetExtractor() sdk.MetadataExtractor {
    return &MyCustomExtractor{}
}
```

### 3. Publish Your Plugin

Push your code to GitHub or any Git hosting service:

```bash
git init
git add .
git commit -m "Initial commit"
git remote add origin https://github.com/yourusername/my-custom-extractor
git push -u origin main
```

### 4. Use Your Plugin in ADDP

ADDP administrators add your plugin to `meta/backend/internal/scanner/plugins/plugins.go`:

```go
import (
    _ "github.com/yourusername/my-custom-extractor"
)
```

That's it! Your extractor will be automatically loaded when ADDP starts.

## Typed Metadata System

### Using Built-in Typed Metadata

The SDK provides pre-defined typed metadata for common cases:

#### Geospatial Data

```go
geoMeta := &sdk.GeoSpatialMetadata{
    GeometryType:     "Polygon",
    CoordinateSystem: "EPSG:4326",
    BoundingBox:      []float64{-180, -90, 180, 90},
    FeatureCount:     1000,
    Dimensions:       2,
    SpatialIndex:     true,
    Attributes:       []string{"name", "population", "area"},
}

// Add to metadata with automatic type information
metadata.AddTypedMetadata("geo_spatial", geoMeta)
```

**Stored in database as:**
```json
{
  "_type": "geo.spatial",
  "_schema": { ... },
  "data": {
    "geometry_type": "Polygon",
    "coordinate_system": "EPSG:4326",
    "bounding_box": [-180, -90, 180, 90],
    "feature_count": 1000,
    "dimensions": 2,
    "spatial_index": true,
    "attributes": ["name", "population", "area"]
  }
}
```

#### Image Data

```go
imageMeta := &sdk.ImageMetadata{
    Width:       1920,
    Height:      1080,
    Format:      "JPEG",
    ColorSpace:  "RGB",
    BitDepth:    8,
    HasAlpha:    false,
    Compression: "JPEG",
    DPI:         72,
}

metadata.AddTypedMetadata("image", imageMeta)
```

#### Document Data

```go
docMeta := &sdk.DocumentMetadata{
    Title:        "My Document",
    Author:       "John Doe",
    Subject:      "Technical Report",
    Keywords:     []string{"technology", "analysis"},
    PageCount:    50,
    WordCount:    10000,
    Language:     "en",
}

metadata.AddTypedMetadata("document", docMeta)
```

### Creating Custom Typed Metadata

For your custom data types, implement the `TypedMetadata` interface:

```go
// Define your custom metadata structure
type VideoMetadata struct {
    Duration    int    // seconds
    Codec       string
    Resolution  string // e.g., "1920x1080"
    Framerate   float64
    Bitrate     int
    AudioCodec  string
    Subtitles   []string
}

// Implement TypedMetadata interface
func (v *VideoMetadata) TypeName() string {
    return "video.basic"
}

func (v *VideoMetadata) Schema() map[string]interface{} {
    return map[string]interface{}{
        "$schema": "http://json-schema.org/draft-07/schema#",
        "type":    "object",
        "properties": map[string]interface{}{
            "duration":     map[string]string{"type": "integer"},
            "codec":        map[string]string{"type": "string"},
            "resolution":   map[string]string{"type": "string"},
            "framerate":    map[string]string{"type": "number"},
            "bitrate":      map[string]string{"type": "integer"},
            "audio_codec":  map[string]string{"type": "string"},
            "subtitles":    map[string]interface{}{
                "type":  "array",
                "items": map[string]string{"type": "string"},
            },
        },
    }
}

func (v *VideoMetadata) ToMap() map[string]interface{} {
    return map[string]interface{}{
        "duration":    v.Duration,
        "codec":       v.Codec,
        "resolution":  v.Resolution,
        "framerate":   v.Framerate,
        "bitrate":     v.Bitrate,
        "audio_codec": v.AudioCodec,
        "subtitles":   v.Subtitles,
    }
}

func (v *VideoMetadata) FromMap(m map[string]interface{}) error {
    if duration, ok := m["duration"].(float64); ok {
        v.Duration = int(duration)
    }
    if codec, ok := m["codec"].(string); ok {
        v.Codec = codec
    }
    // ... map other fields ...
    return nil
}

// Register your custom type
func init() {
    sdk.RegisterMetadataType(&VideoMetadata{})
}

// Use in your extractor
videoMeta := &VideoMetadata{
    Duration:   3600,
    Codec:      "H.264",
    Resolution: "1920x1080",
    Framerate:  30.0,
    Bitrate:    5000000,
}

metadata.AddTypedMetadata("video", videoMeta)
```

## Storage Format

### How Typed Metadata is Stored

When you use `metadata.AddTypedMetadata()`, the data is stored in PostgreSQL's JSONB field with full type information:

**Database Structure:**
```sql
-- meta_item table
CREATE TABLE meta_item (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255),
    attributes JSONB,  -- Typed metadata stored here
    ...
);
```

**Example Storage:**
```json
{
  "bucket": "my-bucket",
  "path": "data/buildings.shp",
  "geo_spatial": {
    "_type": "geo.spatial",
    "_schema": {
      "$schema": "http://json-schema.org/draft-07/schema#",
      "type": "object",
      "properties": {
        "geometry_type": {"type": "string"},
        "coordinate_system": {"type": "string"},
        "bounding_box": {"type": "array", "items": {"type": "number"}},
        "feature_count": {"type": "integer"}
      }
    },
    "data": {
      "geometry_type": "Polygon",
      "coordinate_system": "EPSG:4326",
      "bounding_box": [-122.5, 37.7, -122.3, 37.9],
      "feature_count": 5000,
      "dimensions": 2,
      "spatial_index": true,
      "attributes": ["building_id", "height", "year_built"]
    }
  }
}
```

### Benefits of This Approach

1. **Type Safety**: Each metadata type has a schema for validation
2. **Queryability**: PostgreSQL JSONB supports indexing and querying
3. **Extensibility**: New types can be added without database migrations
4. **Self-Documenting**: Schema is embedded with the data
5. **Version Control**: Schema changes are tracked automatically

### Querying Typed Metadata

```sql
-- Find all Polygon geometries
SELECT name, attributes->'geo_spatial'->'data'->>'geometry_type' as geom_type
FROM meta_item
WHERE attributes->'geo_spatial'->'data'->>'geometry_type' = 'Polygon';

-- Find files with specific coordinate system
SELECT name FROM meta_item
WHERE attributes->'geo_spatial'->'data'->>'coordinate_system' = 'EPSG:4326';

-- Find large images
SELECT name FROM meta_item
WHERE (attributes->'image'->'data'->>'width')::int > 3000
  AND (attributes->'image'->'data'->>'height')::int > 2000;
```

## Complete Example: Shapefile Extractor

See the [Shapefile Extractor](../../plugins/shapefile-extractor/) for a complete working example.

**Key Points:**
- Uses `go-shp` library for Shapefile parsing
- Implements `sdk.MetadataExtractor` interface
- Uses typed `GeoSpatialMetadata` for spatial information
- Handles multiple file components (.shp, .dbf, .prj)
- Provides fallback for missing components

## Best Practices

### 1. Error Handling

Always return partial metadata on error, don't fail completely:

```go
func (e *MyExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    metadata := sdk.NewMetadata(filepath.Base(input.ObjectKey), "My Format", input.Size)

    // Try to extract detailed info
    if input.Reader != nil {
        if err := e.extractDetails(input.Reader, metadata); err != nil {
            // Log error but return basic metadata
            metadata.CustomAttrs["extraction_error"] = err.Error()
        }
    }

    return metadata, nil // Return metadata even if extraction partially failed
}
```

### 2. Performance

For large files, avoid reading entire content:

```go
// Bad: Reads entire file
content, _ := io.ReadAll(input.Reader)

// Good: Read only what you need
header := make([]byte, 1024)
io.ReadFull(input.Reader, header)
// Parse header to get metadata
```

### 3. Content Type Detection

Don't rely solely on MIME types - verify file format:

```go
func (e *MyExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    // Check file extension as fallback
    ext := strings.ToLower(filepath.Ext(input.ObjectKey))
    if ext != ".myformat" {
        return nil, fmt.Errorf("not a .myformat file")
    }

    // Verify magic bytes if possible
    if input.Reader != nil {
        header := make([]byte, 4)
        io.ReadFull(input.Reader, header)
        if !bytes.Equal(header, []byte("MYFM")) {
            return nil, fmt.Errorf("invalid file signature")
        }
    }

    // Proceed with extraction
    ...
}
```

### 4. Schema Extraction

For structured data, always provide schema information:

```go
// Extract column information
columns := []sdk.ColumnInfo{
    {Name: "id", Type: "integer", Nullable: false},
    {Name: "name", Type: "string", Nullable: false},
    {Name: "score", Type: "number", Nullable: true},
}

metadata.SchemaInfo = &sdk.SchemaMetadata{
    Columns:  columns,
    RowCount: 1000,
    SampleData: []map[string]interface{}{
        {"id": 1, "name": "Alice", "score": 95.5},
        {"id": 2, "name": "Bob", "score": 87.0},
    },
}
```

## Testing Your Plugin

### Unit Tests

```go
package mycustomextractor

import (
    "context"
    "strings"
    "testing"

    sdk "github.com/addp/meta-extractor-sdk"
)

func TestExtractor(t *testing.T) {
    extractor := &MyCustomExtractor{}

    // Test input
    input := sdk.ExtractInput{
        ObjectKey:   "test.myformat",
        ContentType: "application/x-my-format",
        Size:        1024,
        Reader:      strings.NewReader("test content"),
    }

    // Extract metadata
    metadata, err := extractor.Extract(context.Background(), input)
    if err != nil {
        t.Fatalf("Extract failed: %v", err)
    }

    // Verify results
    if metadata.BasicInfo.FileName != "test.myformat" {
        t.Errorf("Expected filename test.myformat, got %s", metadata.BasicInfo.FileName)
    }
}
```

### Integration Testing

Test with actual ADDP:

1. Build your plugin
2. Add import to ADDP's plugins.go
3. Rebuild ADDP
4. Upload test file to MinIO
5. Trigger metadata scan
6. Verify metadata in database

## Distribution

### Option 1: Public GitHub Repository

```bash
# Users install with:
go get github.com/yourusername/my-custom-extractor@latest
```

### Option 2: Private Repository

```bash
# Users configure private repo:
export GOPRIVATE=github.com/yourcompany/*
go get github.com/yourcompany/private-extractor@latest
```

### Option 3: Vendoring

Include your plugin directly in ADDP codebase:

```
addp/
├── plugins/
│   └── my-custom-extractor/
│       ├── go.mod
│       └── extractor.go
```

## FAQ

### Q: Do I need the entire ADDP source code?

**No!** You only need the SDK package. Develop your plugin independently.

### Q: How are plugins loaded?

Through Go's import system. When ADDP imports your package, your `init()` function registers the extractor.

### Q: Can I use external libraries?

**Yes!** Use any Go library you need. Just declare it in your `go.mod`.

### Q: What happens if extraction fails?

Return partial metadata with error information in `CustomAttrs["extraction_error"]`. Don't fail completely.

### Q: How do I handle multiple related files (like Shapefile)?

Either:
1. Extract what you can from single file
2. Provide helper function for complete extraction when all files available
3. Add note in metadata about missing components

### Q: Can I update metadata schema later?

**Yes!** Use schema versioning:

```go
metadata.CustomAttrs["_schema_version"] = "2.0"
```

ADDP can detect old versions and trigger re-extraction.

## Support

- SDK Documentation: [extractor_sdk.go](extractor_sdk.go)
- Example Plugin: [Shapefile Extractor](../../plugins/shapefile-extractor/)
- Issues: https://github.com/addp/addp/issues

## License

The SDK is licensed under Apache 2.0. Your plugins can use any license.
