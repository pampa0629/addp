package rastermosaic

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	ManifestFileName         = "mosaic.addp.json"
	ManifestSchemaVersion    = "addp.raster_mosaic.v1"
	SourceIndexSchemaVersion = "addp.raster_mosaic.source_index.v1"
	SourceIndexRef           = "index/source-index.json"
	DefaultOverviewRef       = "overviews/overview.cog.tif"
)

type Manifest struct {
	SchemaVersion string                 `json:"schema_version"`
	DataType      string                 `json:"data_type"`
	Format        string                 `json:"format"`
	Layout        string                 `json:"layout"`
	DatasetName   string                 `json:"dataset_name,omitempty"`
	GeneratedAt   string                 `json:"generated_at,omitempty"`
	Refs          ManifestRefs           `json:"refs,omitempty"`
	Summary       ManifestSummary        `json:"summary,omitempty"`
	Capabilities  map[string]interface{} `json:"capabilities,omitempty"`
}

type ManifestRefs struct {
	Index    string `json:"index,omitempty"`
	Overview string `json:"overview,omitempty"`
}

type ManifestSummary struct {
	LeafCount      int64     `json:"leaf_count,omitempty"`
	SourceCount    int64     `json:"source_count,omitempty"`
	FailedCount    int64     `json:"failed_count,omitempty"`
	Extent         []float64 `json:"extent,omitempty"`
	SourceCRS      string    `json:"source_crs,omitempty"`
	VRTWidth       int64     `json:"vrt_width,omitempty"`
	VRTHeight      int64     `json:"vrt_height,omitempty"`
	OverviewWidth  int64     `json:"overview_width,omitempty"`
	OverviewHeight int64     `json:"overview_height,omitempty"`
}

type SourceIndex struct {
	SchemaVersion string                   `json:"schema_version"`
	GeneratedAt   string                   `json:"generated_at,omitempty"`
	LeafCount     int64                    `json:"leaf_count,omitempty"`
	Leaves        []map[string]interface{} `json:"leaves,omitempty"`
}

func NewManifest(datasetName, generatedAt, indexRef, overviewRef string, summary ManifestSummary, capabilities map[string]interface{}) Manifest {
	return Manifest{
		SchemaVersion: ManifestSchemaVersion,
		DataType:      string(datatype.Media),
		Format:        string(format.FormatRasterMosaic),
		Layout:        string(format.LayoutWhole),
		DatasetName:   strings.TrimSpace(datasetName),
		GeneratedAt:   strings.TrimSpace(generatedAt),
		Refs: ManifestRefs{
			Index:    strings.TrimSpace(indexRef),
			Overview: strings.TrimSpace(overviewRef),
		},
		Summary:      summary,
		Capabilities: capabilities,
	}
}

func NewSourceIndex(generatedAt string, leaves []map[string]interface{}) SourceIndex {
	return SourceIndex{
		SchemaVersion: SourceIndexSchemaVersion,
		GeneratedAt:   strings.TrimSpace(generatedAt),
		LeafCount:     int64(len(leaves)),
		Leaves:        leaves,
	}
}

func DecodeManifest(reader io.Reader, maxBytes int64) (Manifest, error) {
	if reader == nil {
		return Manifest{}, fmt.Errorf("raster mosaic manifest reader is required")
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	var manifest Manifest
	if err := json.NewDecoder(io.LimitReader(reader, maxBytes)).Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ValidateManifest(manifest Manifest) error {
	if strings.TrimSpace(manifest.SchemaVersion) != ManifestSchemaVersion {
		return fmt.Errorf("unsupported raster mosaic manifest schema_version %q", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.DataType) != string(datatype.Media) {
		return fmt.Errorf("raster mosaic manifest data_type must be %s", datatype.Media)
	}
	if strings.TrimSpace(manifest.Format) != string(format.FormatRasterMosaic) {
		return fmt.Errorf("raster mosaic manifest format must be %s", format.FormatRasterMosaic)
	}
	if strings.TrimSpace(manifest.Layout) != string(format.LayoutWhole) {
		return fmt.Errorf("raster mosaic manifest layout must be %s", format.LayoutWhole)
	}
	if strings.TrimSpace(manifest.Refs.Index) == "" {
		return fmt.Errorf("raster mosaic manifest refs.index is required")
	}
	return nil
}
