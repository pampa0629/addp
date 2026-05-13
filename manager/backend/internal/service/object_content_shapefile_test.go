package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonas-p/go-shp"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestShapefileContentHandler_Transforms3857ToWGS84(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	base := filepath.Join(tmpDir, "sample")

	writer, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{
		shp.StringField("NAME", 16),
	})
	row := writer.Write(&shp.Point{X: 12958412.49, Y: 4852030.63})
	if err := writer.WriteAttribute(int(row), 0, "beijing"); err != nil {
		t.Fatalf("write dbf attribute failed: %v", err)
	}
	writer.Close()
	if _, err := os.Stat(base + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
			t.Fatalf("rename dbf failed: %v", err)
		}
	}

	prj := `PROJCS["WGS 84 / Pseudo-Mercator",GEOGCS["WGS 84",DATUM["WGS_1984",SPHEROID["WGS 84",6378137,298.257223563]],PRIMEM["Greenwich",0],UNIT["degree",0.0174532925199433]],PROJECTION["Mercator_1SP"],PARAMETER["central_meridian",0],PARAMETER["scale_factor",1],PARAMETER["false_easting",0],PARAMETER["false_northing",0],UNIT["metre",1],AXIS["X",EAST],AXIS["Y",NORTH],AUTHORITY["EPSG","3857"]]`
	if err := os.WriteFile(base+".prj", []byte(prj), 0o644); err != nil {
		t.Fatalf("write prj failed: %v", err)
	}

	baseStreamer := func() (io.ReadCloser, error) {
		return os.Open(base + ".shp")
	}
	siblingProvider := func(path string) (io.ReadCloser, error) {
		target := ""
		switch filepath.Ext(path) {
		case ".shx":
			target = base + ".shx"
		case ".dbf":
			target = base + ".dbf"
		case ".prj":
			target = base + ".prj"
		default:
			return nil, fs.ErrNotExist
		}
		return os.Open(target)
	}

	handler := &shapefileContentHandler{maxFeatures: 10}
	content, truncated, err := handler.HandleCompositeStream(context.Background(), &ObjectContentRequest{
		Bucket:      "test",
		Path:        "spatial/",
		Name:        "sample.shp",
		Extension:   ".shp",
		ContentType: "application/x-esri-shapefile",
		Size:        1024,
	}, baseStreamer, siblingProvider)
	if err != nil {
		t.Fatalf("handle composite stream failed: %v", err)
	}
	if truncated {
		t.Fatalf("expected non-truncated preview")
	}
	if content == nil {
		t.Fatalf("expected preview content")
	}
	if content.Metadata["transform_engine"] != "proj" && content.Metadata["transform_engine"] != "pure_go" && content.Metadata["transform_engine"] != "duckdb" {
		t.Fatalf("unexpected transform engine: %#v", content.Metadata["transform_engine"])
	}
	if got := fmt.Sprint(content.Metadata["transform_status"]); got != "transformed" {
		t.Fatalf("expected transformed status, got %s", got)
	}
	if got := fmt.Sprint(content.Metadata["source_srid"]); got != "3857" {
		t.Fatalf("expected source_srid 3857, got %s", got)
	}
	if got := fmt.Sprint(content.Metadata["render_srid"]); got != "4326" {
		t.Fatalf("expected render_srid 4326, got %s", got)
	}

	featureCollection, ok := content.GeoJSON.(map[string]interface{})
	if !ok {
		t.Fatalf("expected geojson map, got %T", content.GeoJSON)
	}
	features, ok := featureCollection["features"].([]map[string]interface{})
	if ok && len(features) > 0 {
		geometry := features[0]["geometry"].(map[string]interface{})
		coords := geometry["coordinates"].([]interface{})
		lon := coords[0].(float64)
		lat := coords[1].(float64)
		if lon < 116.3 || lon > 116.5 || lat < 39.8 || lat > 40.0 {
			t.Fatalf("unexpected transformed coords: %v", coords)
		}
		return
	}

	rawFeatures, ok := featureCollection["features"].([]interface{})
	if !ok || len(rawFeatures) == 0 {
		t.Fatalf("expected non-empty features, got %#v", featureCollection["features"])
	}
	first, ok := rawFeatures[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected feature map, got %T", rawFeatures[0])
	}
	geometry, ok := first["geometry"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected geometry map, got %T", first["geometry"])
	}
	coords, ok := geometry["coordinates"].([]interface{})
	if !ok || len(coords) < 2 {
		t.Fatalf("unexpected coordinates: %#v", geometry["coordinates"])
	}
	lon, okLon := coords[0].(float64)
	lat, okLat := coords[1].(float64)
	if !okLon || !okLat {
		t.Fatalf("unexpected coordinate types: %#v", coords)
	}
	if lon < 116.3 || lon > 116.5 || lat < 39.8 || lat > 40.0 {
		t.Fatalf("unexpected transformed coords: %v", coords)
	}
}

func TestShapefileContentHandlerUsesMetaComponentFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	base := filepath.Join(tmpDir, "roads")

	writer, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{shp.StringField("NAME", 16)})
	row := writer.Write(&shp.Point{X: 116.4, Y: 39.9})
	if err := writer.WriteAttribute(int(row), 0, "beijing"); err != nil {
		t.Fatalf("write dbf attribute failed: %v", err)
	}
	writer.Close()
	if _, err := os.Stat(base + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
			t.Fatalf("rename dbf failed: %v", err)
		}
	}

	baseStreamerCalled := false
	baseStreamer := func() (io.ReadCloser, error) {
		baseStreamerCalled = true
		return nil, fs.ErrNotExist
	}
	siblingProvider := func(path string) (io.ReadCloser, error) {
		if strings.HasPrefix(path, "meta/components/") {
			return os.Open(base + filepath.Ext(path))
		}
		return nil, fs.ErrNotExist
	}

	handler := &shapefileContentHandler{maxFeatures: 10}
	content, truncated, err := handler.HandleCompositeStream(context.Background(), &ObjectContentRequest{
		Bucket:      "test",
		Path:        "wrong/",
		Name:        "wrong.shp",
		Extension:   ".shp",
		ContentType: "application/x-esri-shapefile",
		Size:        1024,
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"component_files": []interface{}{
					"meta/components/roads.dbf",
					"meta/components/roads.shp",
					"meta/components/roads.shx",
				},
			},
		},
	}, baseStreamer, siblingProvider)
	if err != nil {
		t.Fatalf("handle composite stream failed: %v", err)
	}
	if baseStreamerCalled {
		t.Fatalf("base streamer should not be used when component_files are available")
	}
	if truncated {
		t.Fatalf("expected non-truncated preview")
	}
	if content == nil || content.GeoJSON == nil {
		t.Fatalf("expected shapefile geojson content")
	}
}

func TestShapefileContentHandlerUsesCPGForDBFAttributes(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	base := filepath.Join(tmpDir, "sample")
	writeEncodedShapefileForPreview(t, base, "GBK", "北京")

	baseStreamer := func() (io.ReadCloser, error) {
		return os.Open(base + ".shp")
	}
	siblingProvider := func(path string) (io.ReadCloser, error) {
		switch filepath.Ext(path) {
		case ".shx":
			return os.Open(base + ".shx")
		case ".dbf":
			return os.Open(base + ".dbf")
		case ".cpg":
			return os.Open(base + ".cpg")
		default:
			return nil, fs.ErrNotExist
		}
	}

	handler := &shapefileContentHandler{maxFeatures: 10}
	content, _, err := handler.HandleCompositeStream(context.Background(), &ObjectContentRequest{
		Bucket:      "test",
		Path:        "data/",
		Name:        "sample.shp",
		Extension:   ".shp",
		ContentType: "application/x-esri-shapefile",
		Size:        1024,
	}, baseStreamer, siblingProvider)
	if err != nil {
		t.Fatalf("handle composite stream failed: %v", err)
	}
	featureCollection, ok := content.GeoJSON.(map[string]interface{})
	if !ok {
		t.Fatalf("expected geojson map, got %T", content.GeoJSON)
	}
	features, ok := featureCollection["features"].([]map[string]interface{})
	if !ok || len(features) != 1 {
		t.Fatalf("features = %#v", featureCollection["features"])
	}
	props, ok := features[0]["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties = %#v", features[0]["properties"])
	}
	if got := props["NAME"]; got != "北京" {
		t.Fatalf("decoded property = %#v, want 北京", got)
	}
	if got := fmt.Sprint(content.Metadata["code_page"]); got != "GBK" {
		t.Fatalf("code page metadata = %q, want GBK", got)
	}
}

func TestDownloadSiblingText_CaseVariants(t *testing.T) {
	t.Parallel()

	provider := func(path string) (io.ReadCloser, error) {
		if path == "demo.PRJ" {
			return io.NopCloser(bytes.NewBufferString("EPSG:3857")), nil
		}
		return nil, fs.ErrNotExist
	}

	text, err := downloadSiblingText("demo.shp", ".prj", provider)
	if err != nil {
		t.Fatalf("downloadSiblingText failed: %v", err)
	}
	if text != "EPSG:3857" {
		t.Fatalf("unexpected text: %s", text)
	}
}

func writeEncodedShapefileForPreview(t *testing.T, base string, cpg string, value string) {
	t.Helper()

	writer, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{shp.StringField("NAME", 16)})
	row := writer.Write(&shp.Point{X: 116.4, Y: 39.9})
	if err := writer.WriteAttribute(int(row), 0, value); err != nil {
		t.Fatalf("write dbf attribute failed: %v", err)
	}
	writer.Close()
	if _, err := os.Stat(base + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
			t.Fatalf("rename dbf failed: %v", err)
		}
	}
	encoded, err := simplifiedchinese.GBK.NewEncoder().String(value)
	if err != nil {
		t.Fatalf("encode GBK failed: %v", err)
	}
	patchDBFPreviewAttribute(t, base+".dbf", encoded, 16)
	if err := os.WriteFile(base+".cpg", []byte(cpg), 0o644); err != nil {
		t.Fatalf("write cpg failed: %v", err)
	}
}

func patchDBFPreviewAttribute(t *testing.T, path string, value string, width int) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dbf failed: %v", err)
	}
	headerLength := int(data[8]) | int(data[9])<<8
	field := make([]byte, width)
	copy(field, []byte(value))
	for i := len(value); i < width; i++ {
		field[i] = ' '
	}
	copy(data[headerLength+1:headerLength+1+width], field)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write dbf failed: %v", err)
	}
}

func TestShapefileContentHandler_RecognizesUTM50NFromPRJ(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	base := filepath.Join(tmpDir, "farmland")

	writer, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{
		shp.StringField("CITY", 16),
	})
	row := writer.Write(&shp.Point{X: 500000, Y: 3550000})
	if err := writer.WriteAttribute(int(row), 0, "luoyang"); err != nil {
		t.Fatalf("write dbf attribute failed: %v", err)
	}
	writer.Close()
	if _, err := os.Stat(base + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
			t.Fatalf("rename dbf failed: %v", err)
		}
	}

	prj := `PROJCS["WGS_1984_UTM_Zone_50N",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",117.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`
	if err := os.WriteFile(base+".prj", []byte(prj), 0o644); err != nil {
		t.Fatalf("write prj failed: %v", err)
	}

	baseStreamer := func() (io.ReadCloser, error) {
		return os.Open(base + ".shp")
	}
	siblingProvider := func(path string) (io.ReadCloser, error) {
		target := ""
		switch filepath.Ext(path) {
		case ".shx":
			target = base + ".shx"
		case ".dbf":
			target = base + ".dbf"
		case ".prj":
			target = base + ".prj"
		default:
			return nil, fs.ErrNotExist
		}
		return os.Open(target)
	}

	handler := &shapefileContentHandler{maxFeatures: 10}
	content, truncated, err := handler.HandleCompositeStream(context.Background(), &ObjectContentRequest{
		Bucket:      "gischain",
		Path:        "data/",
		Name:        "farmland.shp",
		Extension:   ".shp",
		ContentType: "application/x-esri-shapefile",
		Size:        2048,
	}, baseStreamer, siblingProvider)
	if err != nil {
		t.Fatalf("handle composite stream failed: %v", err)
	}
	if truncated {
		t.Fatalf("expected non-truncated preview")
	}
	if content == nil {
		t.Fatalf("expected preview content")
	}
	if got := fmt.Sprint(content.Metadata["source_srid"]); got != "32650" {
		t.Fatalf("expected source_srid 32650, got %s", got)
	}
	if got := fmt.Sprint(content.Metadata["spatial_ref_sys"]); got != "EPSG:32650" {
		t.Fatalf("expected spatial_ref_sys EPSG:32650, got %s", got)
	}
	if got := fmt.Sprint(content.Metadata["transform_status"]); got != "transformed" {
		t.Fatalf("expected transformed status, got %s", got)
	}
	if got := fmt.Sprint(content.Metadata["render_srid"]); got != "4326" {
		t.Fatalf("expected render_srid 4326, got %s", got)
	}
}
