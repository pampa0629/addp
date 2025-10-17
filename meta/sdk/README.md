# ADDP Metadata Extractor SDK

**Version 1.0.0**

A Software Development Kit (SDK) for building third-party metadata extractors for the ADDP (All Domain Data Platform) Meta module.

## What is This?

This SDK allows developers to create custom metadata extractors for ADDP **without needing access to the ADDP source code**. You can:

- Extract metadata from any file format
- Use any Go libraries you need
- Distribute your extractor independently
- Integrate with ADDP through simple import

## Quick Start

```go
package myextractor

import (
    "context"
    sdk "github.com/addp/meta-extractor-sdk"
)

type MyExtractor struct{}

func (e *MyExtractor) SupportedTypes() []string {
    return []string{"application/x-myformat"}
}

func (e *MyExtractor) Priority() int {
    return 80
}

func (e *MyExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    metadata := sdk.NewMetadata("file.ext", "My Format", 1024)
    // Extract your metadata here
    return metadata, nil
}

func GetExtractor() sdk.MetadataExtractor {
    return &MyExtractor{}
}
```

## Key Features

### 1. Interface-Based Design

Implement the `MetadataExtractor` interface:
- `SupportedTypes()` - MIME types you handle
- `Extract()` - Your extraction logic
- `Priority()` - Precedence when multiple extractors match

### 2. Typed Metadata System

Pre-defined types for common data:
- **GeoSpatialMetadata**: Geometry type, CRS, bounding box, features
- **ImageMetadata**: Width, height, format, color space
- **DocumentMetadata**: Title, author, pages, keywords

Each type includes:
- JSON Schema for validation
- Serialization/deserialization
- Type information stored with data

### 3. Flexible Storage

Metadata stored in PostgreSQL JSONB:
```json
{
  "_type": "geo.spatial",
  "_schema": { ... },
  "data": {
    "geometry_type": "Polygon",
    "coordinate_system": "EPSG:4326",
    "bounding_box": [-180, -90, 180, 90]
  }
}
```

Benefits:
- **Queryable**: PostgreSQL JSONB indexing
- **Versionable**: Schema evolution support
- **Self-documenting**: Schema embedded with data
- **No migrations**: Add new types anytime

## Architecture

```
Third-Party Plugin              ADDP Meta Module
┌─────────────────────┐        ┌──────────────────────┐
│ Your Extractor      │        │ Scanner Registry     │
│                     │        │                      │
│ - Implements SDK    │◄───────┤ SDK Adapter          │
│   Interface         │        │                      │
│ - Uses SDK Types    │        │ - Bridges SDK types  │
│ - No ADDP imports   │        │ - Auto-registers     │
└─────────────────────┘        └──────────────────────┘
         │                              │
         │  Import via plugins.go       │
         └──────────────────────────────┘
```

### How It Works

1. **You develop**: Build extractor using only SDK
2. **You publish**: Push to GitHub (public/private)
3. **ADDP imports**: Add one line to `plugins.go`
4. **Auto-register**: Your `init()` function runs
5. **Ready to use**: Files automatically extracted

## Examples

### Basic Extractor

```go
func (e *SimpleExtractor) Extract(ctx context.Context, input sdk.ExtractInput) (*sdk.Metadata, error) {
    metadata := sdk.NewMetadata(
        filepath.Base(input.ObjectKey),
        "Simple Format",
        input.Size,
    )

    metadata.BasicInfo.ContentType = input.ContentType
    metadata.CustomAttrs["custom_field"] = "value"

    return metadata, nil
}
```

### Geospatial Extractor

```go
geoMeta := &sdk.GeoSpatialMetadata{
    GeometryType:     "Polygon",
    CoordinateSystem: "EPSG:4326",
    BoundingBox:      []float64{-122.5, 37.7, -122.3, 37.9},
    FeatureCount:     1000,
    Dimensions:       2,
}

metadata.AddTypedMetadata("geo_spatial", geoMeta)
```

### With Schema Info

```go
metadata.SchemaInfo = &sdk.SchemaMetadata{
    Columns: []sdk.ColumnInfo{
        {Name: "id", Type: "integer"},
        {Name: "name", Type: "string"},
        {Name: "value", Type: "number"},
    },
    RowCount: 500,
    SampleData: []map[string]interface{}{
        {"id": 1, "name": "Alice", "value": 42.0},
    },
}
```

## Installation

### As Plugin Developer

```bash
# Create your plugin
mkdir my-extractor && cd my-extractor
go mod init github.com/yourusername/my-extractor

# Add SDK
go get github.com/addp/meta-extractor-sdk@latest
```

### As ADDP Administrator

In `meta/backend/internal/scanner/plugins/plugins.go`:

```go
import (
    _ "github.com/yourusername/my-extractor"
)
```

Or programmatically:

```go
import myextractor "github.com/yourusername/my-extractor"

func init() {
    scanner.RegisterSDKExtractor(myextractor.GetExtractor())
}
```

## Documentation

- **[Plugin Development Guide](PLUGIN_DEVELOPMENT_GUIDE.md)** - Complete tutorial
- **[API Reference](extractor_sdk.go)** - SDK interfaces and types
- **[Example: Shapefile Extractor](../../plugins/shapefile-extractor/)** - Working example

## Design Principles

### 1. Zero Dependencies on ADDP

Plugins depend **only** on the SDK, not on ADDP internals. This means:
- Develop without ADDP source code
- No version coupling
- Independent testing
- Separate deployment

### 2. Type Safety with Flexibility

Typed metadata provides:
- Compile-time type checking
- Runtime schema validation
- Query optimization
- Self-documentation

But you can also use untyped `CustomAttrs` for ad-hoc fields.

### 3. Progressive Enhancement

Start simple, add complexity as needed:

**Level 1**: Basic file info
```go
metadata := sdk.NewMetadata("file.txt", "Text", 1024)
```

**Level 2**: Custom attributes
```go
metadata.CustomAttrs["line_count"] = 100
```

**Level 3**: Typed metadata
```go
metadata.AddTypedMetadata("document", &sdk.DocumentMetadata{...})
```

**Level 4**: Schema extraction
```go
metadata.SchemaInfo = &sdk.SchemaMetadata{...}
```

## Use Cases

### Geospatial Formats
- Shapefile, GeoJSON, KML, GPX
- Raster formats (GeoTIFF, NetCDF)
- CAD formats (DWG, DXF)

### Scientific Data
- HDF5, NetCDF, GRIB
- FITS (astronomy)
- Medical imaging (DICOM)

### Office Documents
- Excel, Word, PowerPoint
- OpenOffice formats
- iWork formats

### Media Files
- Video (MP4, AVI, MKV)
- Audio (MP3, FLAC, WAV)
- 3D models (OBJ, STL, FBX)

### Database Exports
- SQL dumps
- Parquet, Avro, ORC
- MongoDB exports

## Performance Tips

1. **Partial Reading**: Read only what you need
   ```go
   header := make([]byte, 1024)
   io.ReadFull(input.Reader, header)
   ```

2. **Streaming**: Process large files incrementally
   ```go
   scanner := bufio.NewScanner(input.Reader)
   for scanner.Scan() {
       // Process line by line
   }
   ```

3. **Caching**: Store expensive computations
   ```go
   metadata.CustomAttrs["_cache_computed_at"] = time.Now()
   ```

4. **Lazy Extraction**: Return quickly, mark for later
   ```go
   metadata.CustomAttrs["_needs_full_extraction"] = true
   ```

## Testing

```go
func TestMyExtractor(t *testing.T) {
    extractor := &MyExtractor{}

    input := sdk.ExtractInput{
        ObjectKey: "test.ext",
        Size:      1024,
        Reader:    strings.NewReader("test content"),
    }

    metadata, err := extractor.Extract(context.Background(), input)
    require.NoError(t, err)
    assert.Equal(t, "test.ext", metadata.BasicInfo.FileName)
}
```

## Versioning

SDK follows semantic versioning:
- **Major**: Breaking API changes
- **Minor**: New features, backward compatible
- **Patch**: Bug fixes

Check compatibility:
```go
import sdk "github.com/addp/meta-extractor-sdk"

func init() {
    if sdk.ExtractorSDKVersion != "1.0.0" {
        log.Warn("SDK version mismatch")
    }
}
```

## Support

- **Issues**: https://github.com/addp/addp/issues
- **Discussions**: https://github.com/addp/addp/discussions
- **Examples**: [plugins/](../../plugins/)

## License

Apache 2.0 - See LICENSE file

## Contributing

Contributions welcome! Please:
1. Fork the repo
2. Create feature branch
3. Add tests
4. Submit PR

---

**Ready to build your first extractor?** Check out the [Plugin Development Guide](PLUGIN_DEVELOPMENT_GUIDE.md)!
