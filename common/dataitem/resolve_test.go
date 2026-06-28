package dataitem

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestResolveItemsGroupsShapefileRefs(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindContainer,
		Candidates: []Candidate{
			{Path: "roads.shp", Name: "roads.shp", SizeBytes: &size},
			{Path: "roads.shx", Name: "roads.shx", SizeBytes: &size},
			{Path: "roads.dbf", Name: "roads.dbf", SizeBytes: &size},
			{Path: "roads.prj", Name: "roads.prj", SizeBytes: &size},
			{Path: "readme.md", Name: "readme.md", SizeBytes: &size},
		},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %#v, want shapefile + markdown", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutMulti || item.Format != "shapefile" || item.PrimaryContentPath != "roads.shp" {
		t.Fatalf("first item = %#v, want multi shapefile", item)
	}
	if len(item.RefList) != 4 {
		t.Fatalf("refs = %#v, want 4", item.RefList)
	}
	if !result.Claims["roads.shp"] || !result.Claims["roads.shx"] || !result.Claims["roads.dbf"] {
		t.Fatalf("claims = %#v, want shapefile refs claimed", result.Claims)
	}
	if result.Items[1].Layout != format.LayoutSingle || result.Items[1].DataType != datatype.Document {
		t.Fatalf("second item = %#v, want document single", result.Items[1])
	}
}

func TestResolveItemsGroupsGeoTIFFSidecars(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "geotiff",
		Candidates: []Candidate{
			{Path: "geotiff/srtm_40_01.tif", Name: "srtm_40_01.tif", SizeBytes: &size},
			{Path: "geotiff/srtm_40_01.tfw", Name: "srtm_40_01.tfw", SizeBytes: &size},
			{Path: "geotiff/srtm_40_01.hdr", Name: "srtm_40_01.hdr", SizeBytes: &size},
			{Path: "geotiff/srtm_40_01.tif.aux.xml", Name: "srtm_40_01.tif.aux.xml", SizeBytes: &size},
			{Path: "geotiff/readme.md", Name: "readme.md", SizeBytes: &size},
		},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %#v, want geotiff multi + markdown", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutMulti || item.Format != string(format.FormatTIFF) || item.PrimaryContentPath != "geotiff/srtm_40_01.tif" {
		t.Fatalf("first item = %#v, want multi tiff", item)
	}
	if len(item.RefList) != 4 {
		t.Fatalf("refs = %#v, want 4", item.RefList)
	}
	for _, path := range []string{
		"geotiff/srtm_40_01.tif",
		"geotiff/srtm_40_01.tfw",
		"geotiff/srtm_40_01.hdr",
		"geotiff/srtm_40_01.tif.aux.xml",
	} {
		if !result.Claims[path] {
			t.Fatalf("claims = %#v, want %s claimed", result.Claims, path)
		}
	}
}

func TestResolveItemsGroupsTIFFExtensionSidecars(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "geotiff",
		Candidates: []Candidate{
			{Path: "geotiff/srtm_40_01.tiff", Name: "srtm_40_01.tiff", SizeBytes: &size},
			{Path: "geotiff/srtm_40_01.tfw", Name: "srtm_40_01.tfw", SizeBytes: &size},
		},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %#v, want one geotiff multi", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutMulti || item.Format != string(format.FormatTIFF) || item.PrimaryContentPath != "geotiff/srtm_40_01.tiff" {
		t.Fatalf("item = %#v, want .tiff primary multi", item)
	}
}

func TestResolveItemsIgnoresSystemEntries(t *testing.T) {
	t.Parallel()

	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindContainer,
		Candidates: []Candidate{
			{Path: "__MACOSX/._data.csv", Name: "._data.csv"},
			{Path: ".DS_Store", Name: ".DS_Store"},
			{Path: "data.csv", Name: "data.csv"},
		},
		Options: ResolveOptions{IncludeIgnored: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Ignored) != 2 {
		t.Fatalf("ignored = %#v, want macOS entries ignored", result.Ignored)
	}
	if len(result.Items) != 1 || result.Items[0].Format != "csv" {
		t.Fatalf("items = %#v, want one csv item", result.Items)
	}
}

func TestResolveItemsDetectsWholeScopePartitionedTable(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "dataset",
		Candidates: []Candidate{
			{Path: "dataset/dt=2026-05-05/part-000.parquet", Name: "part-000.parquet", SizeBytes: &size},
			{Path: "dataset/dt=2026-05-06/part-001.parquet", Name: "part-001.parquet", SizeBytes: &size},
		},
		Options: ResolveOptions{AllowWholeScope: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %#v, want one whole scope item", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutWhole || item.Format != string(format.FormatParquet) || item.ScopePath != "dataset" {
		t.Fatalf("item = %#v, want parquet whole scope", item)
	}
	if !result.Exclusive {
		t.Fatal("whole scope table should be exclusive")
	}
	if !result.Claims["dataset/dt=2026-05-05/part-000.parquet"] || !result.Claims["dataset/dt=2026-05-06/part-001.parquet"] {
		t.Fatalf("claims = %#v, want parquet parts claimed", result.Claims)
	}
}

func TestResolveItemsDetectsRasterMosaicManifestWholeScope(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "mosaics/srtm",
		Candidates: []Candidate{
			{Path: "mosaics/srtm/mosaic.addp.json", Name: "mosaic.addp.json", SizeBytes: &size},
			{Path: "mosaics/srtm/index/source-index.json", Name: "source-index.json", SizeBytes: &size},
			{Path: "mosaics/srtm/overviews/overview.cog.tif", Name: "overview.cog.tif", SizeBytes: &size},
			{Path: "mosaics/srtm/leaf/a.cog.tif", Name: "a.cog.tif", SizeBytes: &size},
			{Path: "mosaics/srtm/leaf/b.cog.tif", Name: "b.cog.tif", SizeBytes: &size},
		},
		Options: ResolveOptions{AllowWholeScope: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %#v, want one raster mosaic item", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutWhole || item.Format != string(format.FormatRasterMosaic) || item.DataType != datatype.Media {
		t.Fatalf("item = %#v, want media raster_mosaic whole item", item)
	}
	if item.ScopePath != "mosaics/srtm" || item.FullName != "mosaics/srtm" {
		t.Fatalf("item scope/full_name = %q/%q, want mosaic root", item.ScopePath, item.FullName)
	}
	if !result.Exclusive {
		t.Fatal("raster mosaic whole scope should be exclusive")
	}
	for _, path := range []string{
		"mosaics/srtm/mosaic.addp.json",
		"mosaics/srtm/index/source-index.json",
		"mosaics/srtm/overviews/overview.cog.tif",
		"mosaics/srtm/leaf/a.cog.tif",
		"mosaics/srtm/leaf/b.cog.tif",
	} {
		if !result.Claims[path] {
			t.Fatalf("claims = %#v, want %s claimed", result.Claims, path)
		}
	}
}

func TestResolveItemsDetects3DTilesManifestWholeScope(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "models/city",
		Candidates: []Candidate{
			{Path: "models/city/tileset.json", Name: "tileset.json", SizeBytes: &size},
			{Path: "models/city/0/0.b3dm", Name: "0.b3dm", SizeBytes: &size},
			{Path: "models/city/0/1.b3dm", Name: "1.b3dm", SizeBytes: &size},
		},
		Options: ResolveOptions{AllowWholeScope: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %#v, want one 3D Tiles item", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutWhole || item.Format != string(format.Format3DTiles) || item.DataType != datatype.Model3D {
		t.Fatalf("item = %#v, want model_3d 3dtiles whole item", item)
	}
	if item.ScopePath != "models/city" || item.FullName != "models/city" {
		t.Fatalf("item scope/full_name = %q/%q, want tileset root", item.ScopePath, item.FullName)
	}
	if item.PrimaryContentPath != "models/city/tileset.json" {
		t.Fatalf("PrimaryContentPath = %q, want tileset manifest", item.PrimaryContentPath)
	}
	if item.RefList[0].Path != "models/city/tileset.json" || item.RefList[0].Role != "manifest" || !item.RefList[0].Primary {
		t.Fatalf("refs = %#v, want primary manifest", item.RefList)
	}
	if !result.Exclusive {
		t.Fatal("3D Tiles whole scope should be exclusive")
	}
	for _, path := range []string{
		"models/city/tileset.json",
		"models/city/0/0.b3dm",
		"models/city/0/1.b3dm",
	} {
		if !result.Claims[path] {
			t.Fatalf("claims = %#v, want %s claimed", result.Claims, path)
		}
	}
}

func TestResolveItemsDetectsOSGBMetadataWholeScope(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "models/osgb",
		Candidates: []Candidate{
			{Path: "models/osgb/metadata.xml", Name: "metadata.xml", SizeBytes: &size},
			{Path: "models/osgb/Data/Tile_1/Tile_1_L14_0.osgb", Name: "Tile_1_L14_0.osgb", SizeBytes: &size},
			{Path: "models/osgb/Data/Tile_1/Tile_1_L15_00.osgb", Name: "Tile_1_L15_00.osgb", SizeBytes: &size},
		},
		Options: ResolveOptions{AllowWholeScope: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %#v, want one OSGB item", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutWhole || item.Format != string(format.FormatOSGB) || item.DataType != datatype.Model3D {
		t.Fatalf("item = %#v, want model_3d osgb whole item", item)
	}
	if item.ScopePath != "models/osgb" || item.FullName != "models/osgb" {
		t.Fatalf("item scope/full_name = %q/%q, want OSGB root", item.ScopePath, item.FullName)
	}
	if len(item.RefList) != 1 || item.RefList[0].Path != "models/osgb/metadata.xml" || item.RefList[0].Role != "manifest" || !item.RefList[0].Primary {
		t.Fatalf("refs = %#v, want primary metadata manifest only", item.RefList)
	}
	if !result.Exclusive {
		t.Fatal("OSGB whole scope should be exclusive")
	}
	for _, path := range []string{
		"models/osgb/metadata.xml",
		"models/osgb/Data/Tile_1/Tile_1_L14_0.osgb",
		"models/osgb/Data/Tile_1/Tile_1_L15_00.osgb",
	} {
		if !result.Claims[path] {
			t.Fatalf("claims = %#v, want %s claimed", result.Claims, path)
		}
	}
}

func TestResolveItemsKeepsSiblingTablesAsSingles(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "dataset",
		Candidates: []Candidate{
			{Path: "dataset/sales.parquet", Name: "sales.parquet", SizeBytes: &size},
			{Path: "dataset/customers.parquet", Name: "customers.parquet", SizeBytes: &size},
		},
		Options: ResolveOptions{AllowWholeScope: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result.Exclusive {
		t.Fatal("independent sibling tables should not become an exclusive whole scope")
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %#v, want two single table items", result.Items)
	}
	for _, item := range result.Items {
		if item.Layout != format.LayoutSingle {
			t.Fatalf("item = %#v, want single layout", item)
		}
	}
}

func TestResolveItemsDoesNotFoldClaimedMultiRefsIntoWholeScope(t *testing.T) {
	t.Parallel()

	size := int64(10)
	result, err := ResolveItems(ResolveInput{
		ScopeKind: ScopeKindDirectory,
		ScopePath: "dataset",
		Candidates: []Candidate{
			{Path: "dataset/roads.shp", Name: "roads.shp", SizeBytes: &size},
			{Path: "dataset/roads.shx", Name: "roads.shx", SizeBytes: &size},
			{Path: "dataset/roads.dbf", Name: "roads.dbf", SizeBytes: &size},
			{Path: "dataset/part-000.parquet", Name: "part-000.parquet", SizeBytes: &size},
			{Path: "dataset/part-001.parquet", Name: "part-001.parquet", SizeBytes: &size},
		},
		Options: ResolveOptions{AllowWholeScope: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items = %#v, want shapefile multi + parquet whole scope", result.Items)
	}
	if result.Items[0].Layout != format.LayoutMulti || result.Items[1].Layout != format.LayoutWhole {
		t.Fatalf("items = %#v, want multi before whole", result.Items)
	}
	if result.Items[1].SizeBytes == nil || *result.Items[1].SizeBytes != 20 {
		t.Fatalf("whole item size = %#v, want only parquet ref sizes", result.Items[1].SizeBytes)
	}
	if !result.Claims["dataset/roads.shp"] || !result.Claims["dataset/part-001.parquet"] {
		t.Fatalf("claims = %#v, want both multi and whole refs claimed", result.Claims)
	}
}

func TestScanTargetsFromAttributesDoesNotUseMultiRefsAsScanTargets(t *testing.T) {
	t.Parallel()

	targets := ScanTargetsFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"layout": "multi",
			"refs": []map[string]interface{}{
				{"path": "roads.shp"},
				{"path": "roads.shx"},
				{"path": "roads.shp"},
			},
		},
	})

	if len(targets) != 0 {
		t.Fatalf("targets = %#v, want no ref-based scan targets", targets)
	}
}

func TestScanTargetsFromAttributesRestoresPhysicalPath(t *testing.T) {
	t.Parallel()

	targets := ScanTargetsFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"layout": "whole",
		},
		"storage": map[string]interface{}{
			"physical_path": "/lake/sales",
		},
	})

	if len(targets) != 1 || targets[0].Path != "lake/sales" {
		t.Fatalf("targets = %#v, want physical path", targets)
	}
}

func TestScanTargetsFromAttributesFallsBackToStoragePath(t *testing.T) {
	t.Parallel()

	targets := ScanTargetsFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"layout": "single",
		},
		"storage": map[string]interface{}{
			"path": "/bucket/docs/readme.md",
		},
	})

	if len(targets) != 1 || targets[0].Path != "bucket/docs/readme.md" {
		t.Fatalf("targets = %#v, want storage path", targets)
	}
}

func TestDescriptorFromAttributesRestoresRelatedRefs(t *testing.T) {
	t.Parallel()

	descriptor := DescriptorFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"refs": []map[string]interface{}{
				{"path": "roads.shp", "role": "main", "required": true, "primary": true},
				{"path": "roads.dbf", "extension": ".dbf", "required": true},
			},
		},
	})

	refs := descriptor.RelatedRefs()
	if len(refs) != 2 {
		t.Fatalf("refs = %#v, want 2", refs)
	}
	if refs[0].Ref.Path != "roads.shp" || refs[0].Ref.Role != "main" || !refs[0].Primary {
		t.Fatalf("primary ref = %#v, want restored primary ref", refs[0])
	}
	if refs[1].Ref.Path != "roads.dbf" || refs[1].Ref.Role != "dbf" || !refs[1].Required {
		t.Fatalf("secondary ref = %#v, want extension-derived role", refs[1])
	}
}

func TestDescriptorFromAttributesNormalizesFormat(t *testing.T) {
	t.Parallel()

	unknown := DescriptorFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"format": "yml",
		},
	})
	if unknown.Format != string(format.FormatUnknown) {
		t.Fatalf("legacy yml format = %q, want unknown", unknown.Format)
	}

	csv := DescriptorFromAttributes(map[string]interface{}{
		"item": map[string]interface{}{
			"format": ".csv",
		},
	})
	if csv.Format != string(format.FormatCSV) {
		t.Fatalf("csv format = %q, want csv", csv.Format)
	}
}
