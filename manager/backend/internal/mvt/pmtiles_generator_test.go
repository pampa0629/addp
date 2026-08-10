package mvt

import (
	"context"
	"os"
	"testing"

	commonPMTiles "github.com/addp/common/format/pmtiles"
)

type fakePMTilesTileSource struct {
	params []TileGenerationParams
}

func (s *fakePMTilesTileSource) QuerySourceSRID(context.Context, uint, uint, string, string, string) (int, error) {
	return 4326, nil
}

func (s *fakePMTilesTileSource) GetSpatialExtent(context.Context, uint, uint, string, string, string) ([]float64, error) {
	return []float64{110, 20, 120, 30}, nil
}

func (s *fakePMTilesTileSource) GenerateTile(_ context.Context, params TileGenerationParams) ([]byte, error) {
	s.params = append(s.params, params)
	return []byte{0x1a, 0x02, 0x08, 0x01}, nil
}

func TestPMTilesGeneratorBuildsArchiveFromPostGISTiles(t *testing.T) {
	source := &fakePMTilesTileSource{}
	generator := NewPMTilesGenerator(source)
	archive, err := generator.Generate(context.Background(), QuickViewConfig{
		EngineID: 11, TenantID: 7, Schema: "public", Table: "roads", GeomColumn: "shape",
		SRID: 4326, Extent: []float64{110, 20, 120, 30}, ExtentSRID: 4326,
		MinZoom: 0, MaxZoom: 0, LayerName: "roads", Concurrency: 2,
	}, nil)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	defer archive.Close()
	if archive.Result.GeneratedTiles != 1 || archive.Result.StopReason != "postgis_st_asmvt_pmtiles" {
		t.Fatalf("result = %#v", archive.Result)
	}
	if len(source.params) != 1 || source.params[0].Schema != "public" || source.params[0].GeomColumn != "shape" {
		t.Fatalf("tile params = %#v", source.params)
	}
	data, err := os.ReadFile(archive.Path)
	if err != nil {
		t.Fatal(err)
	}
	header, err := commonPMTiles.ParseHeaderBytes(data)
	if err != nil {
		t.Fatalf("ParseHeaderBytes() error = %v", err)
	}
	pmArchive, err := commonPMTiles.NewArchive(header, func(_ context.Context, offset, length int64) ([]byte, error) {
		return append([]byte(nil), data[offset:offset+length]...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pmArchive.ValidateDirectories(context.Background()); err != nil {
		t.Fatalf("ValidateDirectories() error = %v", err)
	}
	if _, err := pmArchive.GetTile(context.Background(), 0, 0, 0); err != nil {
		t.Fatalf("GetTile() error = %v", err)
	}
}

func TestPMTilesZoomRangesKeepLargeExtentPlanningBounded(t *testing.T) {
	ranges, total := pmtilesZoomRanges([]float64{108.55648171959794, 24.52585476646484, 114.3433679860587, 30.244050172136756}, 4, 18)
	if len(ranges) != 15 {
		t.Fatalf("ranges = %d, want one bounded descriptor per zoom", len(ranges))
	}
	if total < 20_000_000 {
		t.Fatalf("total = %d, want regression extent to cover a large theoretical tile range", total)
	}
}
