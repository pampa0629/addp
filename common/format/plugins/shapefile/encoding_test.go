package shapefile

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
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

func TestNormalizeDBFEncodingHandlesBOMAndGB18030(t *testing.T) {
	t.Parallel()

	if got := NormalizeDBFEncoding("\ufeffUTF-8"); got != "utf-8" {
		t.Fatalf("NormalizeDBFEncoding(UTF-8 BOM) = %q, want utf-8", got)
	}
	if got := NormalizeDBFEncoding("GB18030"); got != "gb18030" {
		t.Fatalf("NormalizeDBFEncoding(GB18030) = %q, want gb18030", got)
	}
}

func TestDecodeDBFTextGB18030(t *testing.T) {
	t.Parallel()

	encoded, err := simplifiedchinese.GB18030.NewEncoder().String("规划用地")
	if err != nil {
		t.Fatalf("encode GB18030 failed: %v", err)
	}
	if got := DecodeDBFText(encoded, "GB18030"); got != "规划用地" {
		t.Fatalf("DecodeDBFText() = %q, want 规划用地", got)
	}
}

func TestShapefileSingleStreamTableProviderIsRejected(t *testing.T) {
	t.Parallel()

	plugin := NewPlugin(nil)
	if _, err := plugin.DescribeTable(context.Background(), strings.NewReader(""), nil); err == nil || !strings.Contains(err.Error(), "requires multi-ref input") {
		t.Fatalf("DescribeTable() error = %v, want multi-ref input error", err)
	}
	if _, err := plugin.SampleTable(context.Background(), strings.NewReader(""), 0, 1, nil); err == nil || !strings.Contains(err.Error(), "requires multi-ref input") {
		t.Fatalf("SampleTable() error = %v, want multi-ref input error", err)
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

func TestShapefilePluginUsesCPGForRefSamples(t *testing.T) {
	t.Parallel()

	base := createEncodedPointShapefile(t, "GBK", "北京")
	reader := newLocalRefReader(base)
	refs := reader.refs()
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeMultiTable(context.Background(), reader, refs, nil)
	if err != nil {
		t.Fatalf("DescribeMultiTable() error = %v", err)
	}
	shpAttrs, _ := info.FormatInfo["shapefile"].(map[string]interface{})
	if shpAttrs["encoding"] != "gbk" {
		t.Fatalf("shapefile encoding = %#v, want gbk", shpAttrs["encoding"])
	}

	rows, err := plugin.SampleMultiTable(context.Background(), reader, refs, 0, 10, nil)
	if err != nil {
		t.Fatalf("SampleMultiTable() error = %v", err)
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

type localRefReader struct {
	base       string
	rangeReads int
	openReads  int
}

func newLocalRefReader(base string) *localRefReader {
	return &localRefReader{base: base}
}

func (r *localRefReader) refs() []format.RelatedRef {
	return []format.RelatedRef{
		format.NewRelatedRef(contentio.NewRef(r.base+".shp", contentio.RoleMain), true, true),
		format.NewRelatedRef(contentio.NewRef(r.base+".shx", "index"), true, false),
		format.NewRelatedRef(contentio.NewRef(r.base+".dbf", "attributes"), true, false),
		format.NewRelatedRef(contentio.NewRef(r.base+".cpg", "encoding"), false, false),
	}
}

func (r *localRefReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}

func (r *localRefReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	r.openReads++
	return os.Open(r.base + filepath.Ext(ref.Path))
}

func (r *localRefReader) OpenRange(ctx context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	r.rangeReads++
	file, err := os.Open(r.base + filepath.Ext(ref.Path))
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

func TestShapefilePluginUsesSHXIndexedRefSample(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a", "b", "c"})
	reader := newLocalRefReader(base)
	refs := reader.refs()
	plugin := NewPlugin(nil)

	rows, err := plugin.SampleMultiTable(context.Background(), reader, refs, 2, 1, nil)
	if err != nil {
		t.Fatalf("SampleMultiTable() error = %v", err)
	}
	if reader.rangeReads == 0 {
		t.Fatalf("rangeReads = 0, want indexed ref sample path")
	}
	if reader.openReads != 0 {
		t.Fatalf("openReads = %d, want no full ref reads for indexed sample path", reader.openReads)
	}
	if reader.rangeReads > 6 {
		t.Fatalf("rangeReads = %d, want page-level range reads", reader.rangeReads)
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

func TestShapefilePluginDoesNotFallbackWhenIndexedRequiredRefReadFails(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a", "b"})
	reader := newFailingRangeRefReader(base, ".dbf")
	refs := reader.refs()
	plugin := NewPlugin(nil)

	if _, err := plugin.SampleMultiTable(context.Background(), reader, refs, 0, 1, nil); err == nil {
		t.Fatal("SampleMultiTable() error = nil, want indexed read error")
	}
	if reader.openReads != 0 {
		t.Fatalf("openReads = %d, want no full ref fallback on indexed read failure", reader.openReads)
	}
}

func TestShapefilePluginReportsMissingRequiredRef(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a"})
	reader := newMissingRefReader(base, ".dbf")
	refs := reader.refs()
	plugin := NewPlugin(nil)

	_, err := plugin.DescribeMultiTable(context.Background(), reader, refs, nil)
	if err == nil {
		t.Fatal("DescribeMultiTable() error = nil, want missing required ref error")
	}
	if !strings.Contains(err.Error(), "failed to read required ref") || !strings.Contains(err.Error(), ".dbf") {
		t.Fatalf("DescribeMultiTable() error = %v, want missing required .dbf ref", err)
	}
}

type failingRangeRefReader struct {
	*localRefReader
	failExt string
}

func newFailingRangeRefReader(base string, failExt string) *failingRangeRefReader {
	return &failingRangeRefReader{
		localRefReader: newLocalRefReader(base),
		failExt:        failExt,
	}
}

func (r *failingRangeRefReader) OpenRange(ctx context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	if strings.EqualFold(filepath.Ext(ref.Path), r.failExt) {
		r.rangeReads++
		return nil, contentio.ErrContentNotFound
	}
	return r.localRefReader.OpenRange(ctx, ref, offset, length)
}

type missingRefReader struct {
	*localRefReader
	missingExt string
}

func newMissingRefReader(base string, missingExt string) *missingRefReader {
	return &missingRefReader{
		localRefReader: newLocalRefReader(base),
		missingExt:     missingExt,
	}
}

func (r *missingRefReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	if strings.EqualFold(filepath.Ext(ref.Path), r.missingExt) {
		r.openReads++
		return nil, errors.New("ref missing")
	}
	return r.localRefReader.Open(ctx, ref)
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
