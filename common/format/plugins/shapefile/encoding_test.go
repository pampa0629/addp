package shapefile

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	"github.com/jonas-p/go-shp"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestDecodeDBFTextGBK(t *testing.T) {
	t.Parallel()

	encoded, err := simplifiedchinese.GBK.NewEncoder().String("北京")
	if err != nil {
		t.Fatalf("encode GBK failed: %v", err)
	}
	if got := DecodeDBFText(encoded, "GBK"); got != "北京" {
		t.Fatalf("DecodeDBFText() = %q, want 北京", got)
	}
}

func TestShapefileReaderUsesCPGForDBFAttributes(t *testing.T) {
	t.Parallel()

	base := createEncodedPointShapefile(t, "GBK", "北京")
	reader, err := Open(base + ".shp")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()

	features, err := reader.ReadAllFeatures(10)
	if err != nil {
		t.Fatalf("ReadAllFeatures() error = %v", err)
	}
	if len(features) != 1 {
		t.Fatalf("feature count = %d, want 1", len(features))
	}
	if got := features[0].Properties["NAME"]; got != "北京" {
		t.Fatalf("decoded property = %#v, want 北京", got)
	}
}

func TestShapefileParserUsesCPGForComponentSamples(t *testing.T) {
	t.Parallel()

	base := createEncodedPointShapefile(t, "GBK", "北京")
	components := newLocalComponentReader(base)
	parser := NewParser(nil)

	info, err := parser.DescribeTableComponents(context.Background(), components, nil)
	if err != nil {
		t.Fatalf("DescribeTableComponents() error = %v", err)
	}
	shpInfo, _ := info.GetExtension("shapefile").(*format.ShapefileInfo)
	if shpInfo == nil || shpInfo.Encoding != "gbk" {
		t.Fatalf("shapefile encoding = %#v, want gbk", shpInfo)
	}

	rows, err := parser.SampleTableComponents(context.Background(), components, 0, 10, nil)
	if err != nil {
		t.Fatalf("SampleTableComponents() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	if got := rows[0]["NAME"]; got != "北京" {
		t.Fatalf("decoded row value = %#v, want 北京", got)
	}
}

func createEncodedPointShapefile(t *testing.T, cpg string, value string) string {
	t.Helper()

	base := filepath.Join(t.TempDir(), "sample")
	writer, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{shp.StringField("NAME", 16)})
	row := writer.Write(&shp.Point{X: 1, Y: 2})
	if err := writer.WriteAttribute(int(row), 0, value); err != nil {
		t.Fatalf("write attribute failed: %v", err)
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
	patchFirstDBFAttribute(t, base+".dbf", encoded, 16)
	if err := os.WriteFile(base+".cpg", []byte(cpg), 0o644); err != nil {
		t.Fatalf("write cpg failed: %v", err)
	}
	return base
}

func patchFirstDBFAttribute(t *testing.T, path string, value string, width int) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dbf failed: %v", err)
	}
	if len(data) < 33 {
		t.Fatalf("dbf too small: %d", len(data))
	}
	headerLength := int(data[8]) | int(data[9])<<8
	if len(data) < headerLength+1+width {
		t.Fatalf("dbf length = %d, need at least %d", len(data), headerLength+1+width)
	}
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

type localComponentReader struct {
	base       string
	rangeReads int
	openReads  int
}

func newLocalComponentReader(base string) *localComponentReader {
	return &localComponentReader{base: base}
}

func (r *localComponentReader) Components() []resource.ComponentRef {
	return []resource.ComponentRef{
		{ResourceRef: resource.NewResourceRef(r.base+".shp", resource.ResourceRoleMain), ComponentRole: "main", Required: true},
		{ResourceRef: resource.NewResourceRef(r.base+".shx", resource.ResourceRoleComponent), ComponentRole: "index", Required: true},
		{ResourceRef: resource.NewResourceRef(r.base+".dbf", resource.ResourceRoleComponent), ComponentRole: "attributes", Required: true},
		{ResourceRef: resource.NewResourceRef(r.base+".cpg", resource.ResourceRoleComponent), ComponentRole: "encoding", Required: false},
	}
}

func (r *localComponentReader) OpenComponent(ctx context.Context, component resource.ComponentRef) (io.ReadCloser, error) {
	r.openReads++
	return os.Open(r.base + filepath.Ext(component.Path))
}

func (r *localComponentReader) OpenComponentRange(ctx context.Context, component resource.ComponentRef, offset, length int64) (io.ReadCloser, error) {
	r.rangeReads++
	file, err := os.Open(r.base + filepath.Ext(component.Path))
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{
		Reader: io.LimitReader(file, length),
		Closer: file,
	}, nil
}

func (r *localComponentReader) OpenComponentRole(ctx context.Context, role string) (io.ReadCloser, error) {
	for _, component := range r.Components() {
		if strings.EqualFold(component.ComponentRole, role) {
			return r.OpenComponent(ctx, component)
		}
	}
	return nil, resource.ErrComponentNotFound
}

func TestShapefileParserUsesSHXIndexedComponentSample(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a", "b", "c"})
	components := newLocalComponentReader(base)
	parser := NewParser(nil)

	rows, err := parser.SampleTableComponents(context.Background(), components, 2, 1, nil)
	if err != nil {
		t.Fatalf("SampleTableComponents() error = %v", err)
	}
	if components.rangeReads == 0 {
		t.Fatalf("rangeReads = 0, want indexed component sample path")
	}
	if components.openReads != 0 {
		t.Fatalf("openReads = %d, want no full component reads for indexed sample path", components.openReads)
	}
	if components.rangeReads > 6 {
		t.Fatalf("rangeReads = %d, want page-level range reads", components.rangeReads)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	if got := rows[0]["NAME"]; got != "c" {
		t.Fatalf("NAME = %#v, want c", got)
	}
	if got := rows[0]["geometry"]; got != "POINT (3 4)" {
		t.Fatalf("geometry = %#v, want POINT (3 4)", got)
	}
}

func TestShapefileParserDoesNotFallbackWhenIndexedRequiredComponentReadFails(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a", "b"})
	components := newFailingRangeComponentReader(base, ".dbf")
	parser := NewParser(nil)

	if _, err := parser.SampleTableComponents(context.Background(), components, 0, 1, nil); err == nil {
		t.Fatal("SampleTableComponents() error = nil, want indexed read error")
	}
	if components.openReads != 0 {
		t.Fatalf("openReads = %d, want no full component fallback on indexed read failure", components.openReads)
	}
}

type failingRangeComponentReader struct {
	*localComponentReader
	failExt string
}

func newFailingRangeComponentReader(base string, failExt string) *failingRangeComponentReader {
	return &failingRangeComponentReader{
		localComponentReader: newLocalComponentReader(base),
		failExt:              failExt,
	}
}

func (r *failingRangeComponentReader) OpenComponentRange(ctx context.Context, component resource.ComponentRef, offset, length int64) (io.ReadCloser, error) {
	if strings.EqualFold(filepath.Ext(component.Path), r.failExt) {
		r.rangeReads++
		return nil, resource.ErrResourceNotFound
	}
	return r.localComponentReader.OpenComponentRange(ctx, component, offset, length)
}

func createPointShapefileRows(t *testing.T, values []string) string {
	t.Helper()

	base := filepath.Join(t.TempDir(), "rows")
	writer, err := shp.Create(base+".shp", shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{shp.StringField("NAME", 16)})
	for i, value := range values {
		row := writer.Write(&shp.Point{X: float64(i + 1), Y: float64(i + 2)})
		if err := writer.WriteAttribute(int(row), 0, value); err != nil {
			t.Fatalf("write attribute failed: %v", err)
		}
	}
	writer.Close()
	if _, err := os.Stat(base + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+".dbf"); err != nil {
			t.Fatalf("rename dbf failed: %v", err)
		}
	}
	return base
}
