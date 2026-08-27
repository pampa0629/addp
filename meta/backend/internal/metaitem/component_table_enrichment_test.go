package metaitem

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/jonas-p/go-shp"
)

func TestCommonDataItemResolverAdaptsMultiItems(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "farmland.shp", Path: "/shp/farmland.shp", Size: 10},
		{Name: "farmland.shx", Path: "/shp/farmland.shx", Size: 20},
		{Name: "farmland.dbf", Path: "/shp/farmland.dbf", Size: 30},
		{Name: "roads.shp", Path: "/shp/roads.shp", Size: 40},
		{Name: "roads.shx", Path: "/shp/roads.shx", Size: 50},
		{Name: "roads.dbf", Path: "/shp/roads.dbf", Size: 60},
		{Name: "readme.pdf", Path: "/shp/readme.pdf", Size: 70},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		DirPath: "/shp",
		Files:   files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result.Exclusive {
		t.Fatal("common data item adapter must not exclusively claim multi items")
	}
	if got, want := len(result.Items), 2; got != want {
		t.Fatalf("Items len = %d, want %d", got, want)
	}
	if result.Items[0].PrimaryContentPath != "/shp/farmland.shp" {
		t.Fatalf("first PrimaryContentPath = %q, want /shp/farmland.shp", result.Items[0].PrimaryContentPath)
	}
	if result.Items[1].PrimaryContentPath != "/shp/roads.shp" {
		t.Fatalf("second PrimaryContentPath = %q, want /shp/roads.shp", result.Items[1].PrimaryContentPath)
	}
	if result.Items[0].Layout != format.LayoutMulti || result.Items[0].DataType != datatype.Table {
		t.Fatalf("first item = %#v, want multi table", result.Items[0])
	}
	if result.Items[0].Format != string(format.FormatShapefile) {
		t.Fatalf("Format = %q, want shapefile from common/format registry", result.Items[0].Format)
	}
	if !result.Claims["/shp/farmland.dbf"] || !result.Claims["/shp/roads.shx"] {
		t.Fatalf("expected ref files to be claimed: %#v", result.Claims)
	}
	if result.Claims["/shp/readme.pdf"] {
		t.Fatalf("unrelated file must not be claimed: %#v", result.Claims)
	}
}

func TestCommonDataItemResolverAdaptsGeoTIFFSidecars(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "srtm_40_01.tif", Path: "/geotiff/srtm_40_01.tif", Size: 100},
		{Name: "srtm_40_01.tfw", Path: "/geotiff/srtm_40_01.tfw", Size: 10},
		{Name: "srtm_40_01.hdr", Path: "/geotiff/srtm_40_01.hdr", Size: 20},
		{Name: "srtm_40_01.tif.aux.xml", Path: "/geotiff/srtm_40_01.tif.aux.xml", Size: 30},
		{Name: "readme.pdf", Path: "/geotiff/readme.pdf", Size: 40},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		DirPath: "/geotiff",
		Files:   files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if got, want := len(result.Items), 1; got != want {
		t.Fatalf("Items len = %d, want %d", got, want)
	}
	item := result.Items[0]
	if item.PrimaryContentPath != "/geotiff/srtm_40_01.tif" {
		t.Fatalf("PrimaryContentPath = %q, want /geotiff/srtm_40_01.tif", item.PrimaryContentPath)
	}
	if item.Layout != format.LayoutMulti || item.DataType != datatype.Media || item.Format != string(format.FormatTIFF) {
		t.Fatalf("item = %#v, want multi media tiff", item)
	}
	if len(item.RefList) != 4 {
		t.Fatalf("refs = %#v, want 4 GeoTIFF refs", item.RefList)
	}
	if !result.Claims["/geotiff/srtm_40_01.tif"] || !result.Claims["/geotiff/srtm_40_01.tif.aux.xml"] || result.Claims["/geotiff/readme.pdf"] {
		t.Fatalf("claims = %#v, want only GeoTIFF refs claimed", result.Claims)
	}
}

func TestCommonDataItemResolverAdaptsRasterMosaicWholeScope(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "mosaic.addp.json", Path: "mosaics/srtm/mosaic.addp.json", Size: 10},
		{Name: "source-index.json", Path: "mosaics/srtm/index/source-index.json", Size: 20},
		{Name: "overview.cog.tif", Path: "mosaics/srtm/overviews/overview.cog.tif", Size: 30},
		{Name: "a.cog.tif", Path: "mosaics/srtm/leaf/a.cog.tif", Size: 40},
		{Name: "b.cog.tif", Path: "mosaics/srtm/leaf/b.cog.tif", Size: 50},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"mosaics/srtm/mosaic.addp.json": []byte(`{"schema_version":"addp.raster_mosaic.v1","data_type":"media","format":"raster_mosaic","layout":"whole","refs":{"index":"index/source-index.json","overview":"overviews/overview.cog.tif"},"summary":{"leaf_count":2,"source_count":2,"extent":[0,1,2,3],"source_crs":"EPSG:4326","overview_width":16,"overview_height":8}}`),
		}},
		DirPath:        "mosaics/srtm",
		Files:          files[:1],
		RecursiveFiles: files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if !result.Exclusive {
		t.Fatal("raster mosaic should exclusively claim the whole scope")
	}
	if got, want := len(result.Items), 1; got != want {
		t.Fatalf("Items len = %d, want %d", got, want)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutWhole || item.DataType != datatype.Media || item.Format != string(format.FormatRasterMosaic) {
		t.Fatalf("item = %#v, want media raster_mosaic whole item", item)
	}
	if item.PhysicalPath != "mosaics/srtm" || item.ScopePath != "mosaics/srtm" {
		t.Fatalf("physical/scope path = %q/%q, want mosaic root", item.PhysicalPath, item.ScopePath)
	}
	mosaicInfo := commonJSON.Section(item.Attributes, "format_info.raster_mosaic")
	if mosaicInfo["manifest_ref"] != "mosaic.addp.json" || mosaicInfo["index_ref"] != "index/source-index.json" || mosaicInfo["overview_ref"] != "overviews/overview.cog.tif" {
		t.Fatalf("format_info.raster_mosaic = %#v, want manifest/index/overview refs", mosaicInfo)
	}
	if commonJSON.InterfaceInt64(mosaicInfo["leaf_count"]) != 2 || commonJSON.InterfaceInt64(mosaicInfo["overview_width"]) != 16 {
		t.Fatalf("format_info.raster_mosaic = %#v, want leaf_count and overview size", mosaicInfo)
	}
	spatial := commonJSON.Section(item.Attributes, "capabilities.spatial")
	if spatial["crs"] != "EPSG:4326" {
		t.Fatalf("capabilities.spatial = %#v, want CRS", spatial)
	}
	for _, file := range files {
		if !result.Claims[file.Path] {
			t.Fatalf("claims = %#v, want %s claimed", result.Claims, file.Path)
		}
	}
}

func TestCommonDataItemResolverRejectsRasterMosaicManifestByNameOnly(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "mosaic.addp.json", Path: "mosaics/not-mosaic/mosaic.addp.json", Size: 10},
		{Name: "readme.txt", Path: "mosaics/not-mosaic/readme.txt", Size: 20},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"mosaics/not-mosaic/mosaic.addp.json": []byte(`{"schema_version":"not-addp","format":"json"}`),
		}},
		DirPath:        "mosaics/not-mosaic",
		Files:          files[:1],
		RecursiveFiles: files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result.Exclusive || len(result.Items) != 0 || len(result.Claims) != 0 {
		t.Fatalf("ResolveItems() = %#v, want no raster mosaic from manifest name only", result)
	}
}

func TestCommonDataItemResolverAdapts3DTilesWholeScope(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "tileset.json", Path: "models/city/tileset.json", Size: 10},
		{Name: "root.b3dm", Path: "models/city/root.b3dm", Size: 20},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"models/city/tileset.json": []byte(`{"asset":{"version":"1.1"},"geometricError":200,"root":{"boundingVolume":{"region":[1,0.5,1.1,0.6,0,120]},"geometricError":0,"content":{"uri":"root.b3dm"}}}`),
		}},
		DirPath:        "models/city",
		Files:          files[:1],
		RecursiveFiles: files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if !result.Exclusive {
		t.Fatal("3D Tiles should exclusively claim the whole scope")
	}
	if got, want := len(result.Items), 1; got != want {
		t.Fatalf("Items len = %d, want %d", got, want)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutWhole || item.DataType != datatype.Model3D || item.Format != string(format.Format3DTiles) {
		t.Fatalf("item = %#v, want model_3d 3dtiles whole item", item)
	}
	modelInfo := commonJSON.Section(item.Attributes, "type_info.model_3d")
	if modelInfo["model_kind"] != datatype.Model3DKindTiledScene {
		t.Fatalf("type_info.model_3d = %#v, want tiled_scene", modelInfo)
	}
	formatInfo := commonJSON.Section(item.Attributes, "format_info.3dtiles")
	if formatInfo["manifest_ref"] != "tileset.json" || commonJSON.InterfaceInt64(formatInfo["tile_count"]) != 1 {
		t.Fatalf("format_info.3dtiles = %#v, want manifest and tile_count", formatInfo)
	}
	spatial := commonJSON.Section(item.Attributes, "capabilities.spatial")
	if commonJSON.InterfaceInt64(spatial["srid"]) != 4326 {
		t.Fatalf("capabilities.spatial = %#v, want EPSG:4326", spatial)
	}
	for _, file := range files {
		if !result.Claims[file.Path] {
			t.Fatalf("claims = %#v, want %s claimed", result.Claims, file.Path)
		}
	}
}

func TestCommonDataItemResolverAdaptsEPTWholeScope(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "ept.json", Path: "pointcloud/ept/ept.json", Size: 10},
		{Name: "0-0-0-0.laz", Path: "pointcloud/ept/ept-data/0-0-0-0.laz", Size: 20},
		{Name: "0-0-0-0.json", Path: "pointcloud/ept/ept-hierarchy/0-0-0-0.json", Size: 30},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"pointcloud/ept/ept.json": []byte(`{
				"version":"1.1.0",
				"dataType":"laszip",
				"points":42,
				"bounds":[0,1,2,10,20,30],
				"boundsConforming":[1,2,3,9,19,29],
				"schema":[
					{"name":"X","type":"signed","size":4,"scale":0.01,"offset":0},
					{"name":"Y","type":"signed","size":4,"scale":0.01,"offset":0},
					{"name":"Z","type":"signed","size":4,"scale":0.01,"offset":0},
					{"name":"Intensity","type":"unsigned","size":2},
					{"name":"Classification","type":"unsigned","size":1},
					{"name":"Red","type":"unsigned","size":2},
					{"name":"Green","type":"unsigned","size":2},
					{"name":"Blue","type":"unsigned","size":2}
				],
				"span":128,
				"hierarchyType":"json",
				"srs":{"authority":"EPSG","horizontal":"4978"}
			}`),
		}},
		DirPath:        "pointcloud/ept",
		Files:          files[:1],
		RecursiveFiles: files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if !result.Exclusive {
		t.Fatal("EPT should exclusively claim the whole scope")
	}
	if got, want := len(result.Items), 1; got != want {
		t.Fatalf("Items len = %d, want %d", got, want)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutWhole || item.DataType != datatype.PointCloud || item.Format != string(format.FormatEPT) {
		t.Fatalf("item = %#v, want point_cloud ept whole item", item)
	}
	if item.ScopePath != "pointcloud/ept" || item.PrimaryContentPath != "pointcloud/ept/ept.json" {
		t.Fatalf("scope/primary = %q/%q, want EPT root and manifest", item.ScopePath, item.PrimaryContentPath)
	}
	pointCloud := commonJSON.Section(item.Attributes, "type_info.point_cloud")
	if pointCloud["point_cloud_kind"] != datatype.PointCloudKindTiledPointCloud || commonJSON.InterfaceInt64(pointCloud["point_count"]) != 42 {
		t.Fatalf("type_info.point_cloud = %#v, want tiled point cloud count", pointCloud)
	}
	if commonJSON.InterfaceInt64(pointCloud["dimension_count"]) != 8 || !commonJSON.InterfaceBool(pointCloud["has_color"]) {
		t.Fatalf("type_info.point_cloud = %#v, want dimensions and color capability", pointCloud)
	}
	formatInfo := commonJSON.Section(item.Attributes, "format_info.ept")
	if formatInfo["manifest_ref"] != "ept.json" || formatInfo["hierarchy_type"] != "json" || commonJSON.InterfaceInt64(formatInfo["span"]) != 128 {
		t.Fatalf("format_info.ept = %#v, want manifest facts", formatInfo)
	}
	spatial := commonJSON.Section(item.Attributes, "capabilities.spatial")
	if commonJSON.InterfaceInt64(spatial["srid"]) != 4978 {
		t.Fatalf("capabilities.spatial = %#v, want EPSG:4978", spatial)
	}
	for _, file := range files {
		if !result.Claims[file.Path] {
			t.Fatalf("claims = %#v, want %s claimed", result.Claims, file.Path)
		}
	}
}

func TestCommonDataItemResolverAdaptsOSGBWholeScope(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "metadata.xml", Path: "models/osgb/metadata.xml", Size: 10},
		{Name: "Tile_1_L14_0.osgb", Path: "models/osgb/Data/Tile_1/Tile_1_L14_0.osgb", Size: 20},
		{Name: "Tile_1_L15_00.osgb", Path: "models/osgb/Data/Tile_1/Tile_1_L15_00.osgb", Size: 30},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"models/osgb/metadata.xml": []byte(`<?xml version="1.0" encoding="utf-8"?>
<ModelMetadata version="1">
	<SRS>EPSG:4549</SRS>
	<SRSOrigin>381180,4897399,0</SRSOrigin>
	<Texture><ColorSource>Visible</ColorSource></Texture>
</ModelMetadata>`),
		}},
		DirPath:        "models/osgb",
		Files:          files[:1],
		RecursiveFiles: files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if !result.Exclusive {
		t.Fatal("OSGB should exclusively claim the whole scope")
	}
	if got, want := len(result.Items), 1; got != want {
		t.Fatalf("Items len = %d, want %d", got, want)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutWhole || item.DataType != datatype.Model3D || item.Format != string(format.FormatOSGBScene) {
		t.Fatalf("item = %#v, want model_3d osgb_scene whole item", item)
	}
	if len(item.RefList) != 1 || item.RefList[0].Path != "models/osgb/metadata.xml" {
		t.Fatalf("refs = %#v, want metadata manifest only", item.RefList)
	}
	modelInfo := commonJSON.Section(item.Attributes, "type_info.model_3d")
	if modelInfo["model_kind"] != datatype.Model3DKindPhotogrammetryScene || commonJSON.InterfaceInt64(modelInfo["size_bytes"]) != 60 {
		t.Fatalf("type_info.model_3d = %#v, want photogrammetry_scene and size", modelInfo)
	}
	formatInfo := commonJSON.Section(item.Attributes, "format_info.osgb_scene")
	if formatInfo["manifest_ref"] != "metadata.xml" || formatInfo["color_source"] != "Visible" {
		t.Fatalf("format_info.osgb_scene = %#v, want manifest and color source", formatInfo)
	}
	spatial := commonJSON.Section(item.Attributes, "capabilities.spatial")
	if commonJSON.InterfaceInt64(spatial["srid"]) != 4549 {
		t.Fatalf("capabilities.spatial = %#v, want EPSG:4549", spatial)
	}
	for _, file := range files {
		if !result.Claims[file.Path] {
			t.Fatalf("claims = %#v, want %s claimed", result.Claims, file.Path)
		}
	}
}

func TestCommonDataItemResolverKeepsInvalidOSGBManifestAsStandaloneXML(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "metadata.xml", Path: "documents/metadata.xml", Size: 32, ContentType: "application/xml"},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"documents/metadata.xml": []byte(`<catalog><title>Independent XML</title></catalog>`),
		}},
		DirPath:        "documents",
		Files:          files,
		RecursiveFiles: files,
		Options:        ResolveOptions{IncludeSingleResources: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result.Exclusive {
		t.Fatal("plain metadata.xml must not exclusively claim the directory as OSGB scene")
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %#v, want one standalone XML item", result.Items)
	}
	item := result.Items[0]
	if item.Format != string(format.FormatXML) || item.DataType != datatype.Document || item.Layout != format.LayoutSingle {
		t.Fatalf("item = %#v, want standalone XML document", item)
	}
}

func TestCommonDataItemResolverAdaptsGLTFManifestMultiItem(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "scene.gltf", Path: "models/building/scene.gltf", Size: 10},
		{Name: "geometry.bin", Path: "models/building/buffers/geometry.bin", Size: 20},
		{Name: "baseColor.png", Path: "models/building/textures/baseColor.png", Size: 30},
		{Name: "normal.ktx2", Path: "models/building/textures/normal.ktx2", Size: 40},
		{Name: "readme.txt", Path: "models/building/readme.txt", Size: 50},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"models/building/scene.gltf": []byte(`{
				"asset":{"version":"2.0","generator":"ADDP test"},
				"buffers":[{"uri":"buffers/geometry.bin","byteLength":256}],
				"images":[{"uri":"textures/baseColor.png"},{"uri":"textures/normal.ktx2"},{"uri":"data:image/png;base64,AAAA"}]
			}`),
		}},
		DirPath: "models/building",
		Files:   files,
		Options: ResolveOptions{IncludeSingleResources: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result.Exclusive {
		t.Fatal("glTF multi item must not exclusively claim the scope")
	}
	if got, want := len(result.Items), 2; got != want {
		t.Fatalf("Items = %#v, want glTF item plus readme", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutMulti || item.DataType != datatype.Model3D || item.Format != string(format.FormatGLTF) {
		t.Fatalf("item = %#v, want model_3d gltf multi item", item)
	}
	if item.PrimaryContentPath != "models/building/scene.gltf" {
		t.Fatalf("PrimaryContentPath = %q, want glTF manifest", item.PrimaryContentPath)
	}
	if got, want := len(item.RefList), 4; got != want {
		t.Fatalf("refs = %#v, want manifest, buffer and two images", item.RefList)
	}
	hasManifest := false
	for _, ref := range item.RefList {
		if ref.Path == "models/building/scene.gltf" && ref.Role == "manifest" && ref.Primary {
			hasManifest = true
		}
	}
	if !hasManifest {
		t.Fatalf("refs = %#v, want primary manifest ref", item.RefList)
	}
	if !result.Claims["models/building/scene.gltf"] ||
		!result.Claims["models/building/buffers/geometry.bin"] ||
		!result.Claims["models/building/textures/baseColor.png"] ||
		!result.Claims["models/building/textures/normal.ktx2"] {
		t.Fatalf("claims = %#v, want glTF local resources claimed", result.Claims)
	}
	if result.Claims["models/building/readme.txt"] {
		t.Fatalf("claims = %#v, readme must not be claimed by glTF", result.Claims)
	}
	if result.Items[1].PrimaryContentPath != "models/building/readme.txt" {
		t.Fatalf("second item = %#v, want readme single item", result.Items[1])
	}
}

func TestCommonDataItemResolverRejectsIncompleteGLTFRefs(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "scene.gltf", Path: "models/incomplete/scene.gltf", Size: 10},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"models/incomplete/scene.gltf": []byte(`{"asset":{"version":"2.0"},"buffers":[{"uri":"missing.bin","byteLength":256}]}`),
		}},
		DirPath: "models/incomplete",
		Files:   files,
		Options: ResolveOptions{IncludeSingleResources: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 0 || len(result.Claims) != 0 {
		t.Fatalf("ResolveItems() = %#v, want no glTF item with missing local refs", result)
	}
}

func TestCommonDataItemResolverClaimsOBJMaterialLibraryRefs(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "rifle.obj", Path: "models/obj/rifle.obj", Size: 100},
		{Name: "rifle.mtl", Path: "models/obj/rifle.mtl", Size: 20},
		{Name: "albedo.png", Path: "models/obj/textures/albedo.png", Size: 30},
		{Name: "readme.txt", Path: "models/obj/readme.txt", Size: 10},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"models/obj/rifle.obj":           []byte("mtllib rifle.mtl\nv 0 0 0\n"),
			"models/obj/rifle.mtl":           []byte("newmtl default\nmap_Kd textures/albedo.png\n"),
			"models/obj/textures/albedo.png": []byte("png"),
			"models/obj/readme.txt":          []byte("readme"),
		}},
		DirPath: "models/obj",
		Files:   files,
		Options: ResolveOptions{IncludeSingleResources: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result.Exclusive {
		t.Fatal("OBJ material refs must not exclusively claim the scope")
	}
	if got, want := len(result.Items), 2; got != want {
		t.Fatalf("Items = %#v, want OBJ item plus readme", result.Items)
	}
	item := result.Items[0]
	if item.Layout != format.LayoutSingle || item.DataType != datatype.Model3D || item.Format != string(format.FormatOBJ) {
		t.Fatalf("item = %#v, want model_3d obj single item", item)
	}
	if item.PrimaryContentPath != "models/obj/rifle.obj" {
		t.Fatalf("PrimaryContentPath = %q, want OBJ file", item.PrimaryContentPath)
	}
	if got, want := len(item.RefList), 3; got != want {
		t.Fatalf("refs = %#v, want OBJ, MTL and texture refs", item.RefList)
	}
	if !result.Claims["models/obj/rifle.obj"] ||
		!result.Claims["models/obj/rifle.mtl"] ||
		!result.Claims["models/obj/textures/albedo.png"] {
		t.Fatalf("claims = %#v, want OBJ local resources claimed", result.Claims)
	}
	if result.Claims["models/obj/readme.txt"] {
		t.Fatalf("claims = %#v, readme must not be claimed by OBJ", result.Claims)
	}
	if result.Items[1].PrimaryContentPath != "models/obj/readme.txt" {
		t.Fatalf("second item = %#v, want readme single item", result.Items[1])
	}
}

func TestCommonDataItemResolverDoesNotClaimOBJDirectoryImagesWithoutMTL(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "mesh.obj", Path: "models/obj/mesh.obj", Size: 100},
		{Name: "mesh_001.jpg", Path: "models/obj/mesh_001.jpg", Size: 30},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"models/obj/mesh.obj": []byte("mtllib missing.mtl\nusemtl material_0\nv 0 0 0\n"),
		}},
		DirPath: "models/obj",
		Files:   files,
		Options: ResolveOptions{IncludeSingleResources: true},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if got, want := len(result.Items), 2; got != want {
		t.Fatalf("Items = %#v, want OBJ and JPG as independent items when MTL is missing", result.Items)
	}
	if result.Claims["models/obj/mesh_001.jpg"] {
		t.Fatalf("claims = %#v, JPG must not be claimed without MTL map_Kd evidence", result.Claims)
	}
}

func TestCommonDataItemResolverRejects3DTilesManifestByNameOnly(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "tileset.json", Path: "models/not-tiles/tileset.json", Size: 10},
		{Name: "readme.txt", Path: "models/not-tiles/readme.txt", Size: 20},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader: refMapContentReader{content: map[string][]byte{
			"models/not-tiles/tileset.json": []byte(`{"format":"json"}`),
		}},
		DirPath:        "models/not-tiles",
		Files:          files[:1],
		RecursiveFiles: files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if result.Exclusive || len(result.Items) != 0 || len(result.Claims) != 0 {
		t.Fatalf("ResolveItems() = %#v, want no 3D Tiles from manifest name only", result)
	}
}

func TestCommonDataItemResolverRejectsIncompleteMultiRefs(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "roads.shp", Path: "bucket/roads/roads.shp"},
		{Name: "roads.dbf", Path: "bucket/roads/roads.dbf"},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{DirPath: "bucket/roads", Files: files})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 0 || len(result.Claims) != 0 {
		t.Fatalf("ResolveItems() = %#v, want no multi item", result)
	}
}

func TestCommonDataItemResolverRejectsCrossDirectoryRefs(t *testing.T) {
	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "roads.shp", Path: "dataset/roads/roads.shp"},
		{Name: "roads.shx", Path: "dataset/roads/roads.shx"},
		{Name: "roads.dbf", Path: "dataset/roads/attributes/roads.dbf"},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{DirPath: "dataset/roads", Files: files})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 0 || len(result.Claims) != 0 {
		t.Fatalf("ResolveItems() = %#v, want no multi item", result)
	}
}

func TestCommonDataItemResolverEnrichesRefTableViaFormatPlugin(t *testing.T) {
	t.Parallel()

	base := createMetaTestShapefile(t)
	content := map[string][]byte{}
	for _, ext := range []string{".shp", ".shx", ".dbf"} {
		data, err := os.ReadFile(base + ext)
		if err != nil {
			t.Fatalf("read %s: %v", ext, err)
		}
		content["bucket/gis/roads"+ext] = data
	}

	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "roads.shp", Path: "bucket/gis/roads.shp", Size: int64(len(content["bucket/gis/roads.shp"]))},
		{Name: "roads.shx", Path: "bucket/gis/roads.shx", Size: int64(len(content["bucket/gis/roads.shx"]))},
		{Name: "roads.dbf", Path: "bucket/gis/roads.dbf", Size: int64(len(content["bucket/gis/roads.dbf"]))},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader:        refMapContentReader{content: content},
		EngineID:             1,
		EngineCatalogPathFor: plugin.ObjectItemPathForBucket(1, "bucket"),
		DirPath:              "bucket/gis",
		Files:                files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("Items = %#v, want one multi item", result.Items)
	}
	attrs := result.Items[0].Attributes
	if rowCount := commonJSON.Int64(attrs, "type_info.table", "row_count"); rowCount != 2 {
		t.Fatalf("row_count = %d, want 2", rowCount)
	}
	spatial := commonJSON.Section(attrs, "capabilities.spatial")
	if extent, ok := spatial["extent"].([]float64); !ok || len(extent) != 4 || extent[0] != 1 || extent[1] != 2 || extent[2] != 3 || extent[3] != 4 {
		t.Fatalf("extent = %#v, want shp header bbox", spatial["extent"])
	}
	tableNative := commonJSON.Section(attrs, "type_info.table.native")
	if tableNative["shape_type"] != "Point" {
		t.Fatalf("type_info.table.native = %#v, want shape_type Point", tableNative)
	}
	formatInfo := commonJSON.Section(attrs, "format_info.shapefile")
	if formatInfo["shape_type"] != nil {
		t.Fatalf("format_info.shapefile should not contain table native facts: %#v", formatInfo)
	}
	if accessIndex := commonJSON.Section(attrs, "access_index.table"); len(accessIndex) != 0 {
		t.Fatalf("access_index.table = %#v, want no shapefile access index metadata", accessIndex)
	}
}

func TestCommonDataItemResolverEnrichesObjectRefsWithBucketRelativePaths(t *testing.T) {
	t.Parallel()

	base := createMetaTestShapefile(t)
	content := map[string][]byte{}
	for _, ext := range []string{".shp", ".shx", ".dbf"} {
		data, err := os.ReadFile(base + ext)
		if err != nil {
			t.Fatalf("read %s: %v", ext, err)
		}
		content["gis/roads"+ext] = data
	}

	d := &commonDataItemResolver{}
	files := []StorageFileRef{
		{Name: "roads.shp", Path: "gis/roads.shp", Size: int64(len(content["gis/roads.shp"]))},
		{Name: "roads.shx", Path: "gis/roads.shx", Size: int64(len(content["gis/roads.shx"]))},
		{Name: "roads.dbf", Path: "gis/roads.dbf", Size: int64(len(content["gis/roads.dbf"]))},
	}

	result, err := d.ResolveItems(context.Background(), DirectoryResolveInput{
		ContentReader:        refMapContentReader{content: content},
		EngineID:             1,
		EngineCatalogPathFor: plugin.ObjectItemPathForBucket(1, "bucket"),
		DirPath:              "gis",
		Files:                files,
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("Items = %#v, want one multi item", result.Items)
	}
	if rowCount := commonJSON.Int64(result.Items[0].Attributes, "type_info.table", "row_count"); rowCount != 2 {
		t.Fatalf("row_count = %d, want 2", rowCount)
	}
}

type refMapContentReader struct {
	content map[string][]byte
}

func (r refMapContentReader) Type() string         { return "map" }
func (r refMapContentReader) DisplayName() string  { return "map" }
func (r refMapContentReader) EngineOrigin() string { return "general" }
func (r refMapContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r refMapContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error {
	return nil
}
func (r refMapContentReader) DefaultPort() int          { return 0 }
func (r refMapContentReader) RequiredFields() []string  { return nil }
func (r refMapContentReader) SensitiveFields() []string { return nil }
func (r refMapContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r refMapContentReader) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (r refMapContentReader) OpenContent(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	key := path.StringPath()
	businessPath := plugin.EngineCatalogPathWithoutRoot(path)
	if len(businessPath.Segments) > 0 && businessPath.Segments[0].Name == "bucket" {
		key = strings.TrimPrefix(key, "bucket/")
	}
	data, ok := r.content[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func createMetaTestShapefile(t *testing.T) string {
	t.Helper()
	base := filepath.Join(t.TempDir(), "roads")
	writer, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{shp.StringField("NAME", 16)})
	row := writer.Write(&shp.Point{X: 1, Y: 2})
	if err := writer.WriteAttribute(int(row), 0, "a"); err != nil {
		t.Fatalf("write attribute failed: %v", err)
	}
	row = writer.Write(&shp.Point{X: 3, Y: 4})
	if err := writer.WriteAttribute(int(row), 0, "b"); err != nil {
		t.Fatalf("write attribute failed: %v", err)
	}
	writer.Close()
	if _, err := os.Stat(base + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
			t.Fatalf("rename dbf failed: %v", err)
		}
	}
	return base
}
