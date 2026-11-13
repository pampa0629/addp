package image

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type tiffSidecarReader struct {
	files   map[string][]byte
	reads   []string
	failure error
}

func (r *tiffSidecarReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.reads = append(r.reads, ref.Path)
	if r.failure != nil && ref.Role != "main" {
		return nil, r.failure
	}
	data, found := r.files[ref.Path]
	if !found {
		return nil, contentio.ErrContentNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (*tiffSidecarReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	panic("TIFF description must not discover files")
}
func tiffSpatialRefs(roles ...string) []format.RelatedRef {
	refs := []format.RelatedRef{format.NewRelatedRef(contentio.NewRef("scene.tif", "main"), true, true)}
	for _, role := range roles {
		name := "scene.tfw"
		if role == "auxiliary_metadata" {
			name = "scene.tif.aux.xml"
		}
		refs = append(refs, format.NewRelatedRef(contentio.NewRef(name, role), false, false))
	}
	return refs
}
func requireTIFFSpatial(t *testing.T, info *format.MediaDescribeResult, extent datatype.BoundingBox, srid int) {
	t.Helper()
	if info.Spatial == nil || info.Spatial.Extent == nil || *info.Spatial.Extent != extent {
		t.Fatalf("extent=%#v want %v", info.Spatial, extent)
	}
	if srid > 0 && (info.Spatial.SRID == nil || *info.Spatial.SRID != srid) {
		t.Fatalf("CRS=%#v want %d", info.Spatial, srid)
	}
	if srid == 0 && (info.Spatial.SRID != nil || info.Spatial.CRSRef != "") {
		t.Fatalf("invented CRS: %#v", info.Spatial)
	}
}
func TestTIFFSidecarsDescribeCompleteSpatialFacts(t *testing.T) {
	provider := newTIFFPlugin()
	reader := &tiffSidecarReader{files: map[string][]byte{"scene.tif": testEncodedTIFF(t), "scene.tfw": []byte("10\n0\n0\n-10\n105\n195\n"), "scene.tif.aux.xml": []byte(`<PAMDataset><SRS>EPSG:4326</SRS></PAMDataset>`), "unregistered.prj": []byte("EPSG:3857")}}
	info, err := provider.DescribeMultiMedia(t.Context(), reader, tiffSpatialRefs("world_file", "auxiliary_metadata"), nil)
	if err != nil {
		t.Fatal(err)
	}
	requireTIFFSpatial(t, info, datatype.BoundingBox{100, 170, 120, 200}, 4326)
	if info.FormatInfo["profile"] != "plain_tiff" {
		t.Fatal("sidecar altered source layout profile")
	}
	if len(reader.reads) != 3 {
		t.Fatal("read outside registered spatial members", reader.reads)
	}
	reader.reads = nil
	info, err = provider.DescribeMultiMedia(t.Context(), reader, tiffSpatialRefs(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Spatial != nil || !reflect.DeepEqual(reader.reads, []string{"scene.tif"}) {
		t.Fatal("discovered unregistered sidecars")
	}
}
func TestTIFFSidecarsRotationAndPartialFacts(t *testing.T) {
	provider := newTIFFPlugin()
	reader := &tiffSidecarReader{files: map[string][]byte{"scene.tif": testEncodedTIFF(t), "scene.tfw": []byte("2\n1\n3\n-4\n10\n20\n"), "scene.tif.aux.xml": []byte(`<PAMDataset><SRS>EPSG:3857</SRS></PAMDataset>`)}}
	info, err := provider.DescribeMultiMedia(t.Context(), reader, tiffSpatialRefs("world_file"), nil)
	if err != nil {
		t.Fatal(err)
	}
	requireTIFFSpatial(t, info, datatype.BoundingBox{7.5, 9.5, 20.5, 23.5}, 0)
	info, err = provider.DescribeMultiMedia(t.Context(), reader, tiffSpatialRefs("auxiliary_metadata"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Spatial == nil || info.Spatial.SRID == nil || *info.Spatial.SRID != 3857 || info.Spatial.Extent != nil {
		t.Fatal("PAM CRS alone invented grid", info.Spatial)
	}
}
func TestTIFFSidecarsPriorityMatchesGDAL(t *testing.T) {
	provider := newTIFFPlugin()
	reader := &tiffSidecarReader{files: map[string][]byte{"scene.tif": testTIFFWithTags(t, []tiffTestTag{
		{tag: tagImageWidth, typ: tiffTypeLong, value: 2}, {tag: tagImageLength, typ: tiffTypeLong, value: 2},
		{tag: tagModelPixelScale, typ: tiffTypeDouble, data: doublesBytes([]float64{10, 10, 0})},
		{tag: tagModelTiepoint, typ: tiffTypeDouble, data: doublesBytes([]float64{0, 0, 0, 100, 200, 0})},
		{tag: tagGeoKeyDirectory, typ: tiffTypeShort, data: shortsBytes([]uint16{1, 1, 0, 1, geoKeyGeographicType, 0, 1, 4326})},
	}, 0), "scene.tfw": []byte("1\n0\n0\n-1\n0\n0\n"), "scene.tif.aux.xml": []byte(`<PAMDataset><SRS>EPSG:3857</SRS></PAMDataset>`)}}
	refs := tiffSpatialRefs("world_file", "auxiliary_metadata")
	info, err := provider.DescribeMultiMedia(t.Context(), reader, refs, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireTIFFSpatial(t, info, datatype.BoundingBox{100, 180, 120, 200}, 3857)
	if !reflect.DeepEqual(reader.reads, []string{"scene.tif", "scene.tif.aux.xml"}) {
		t.Fatal("read unused world file", reader.reads)
	}
	reader.files["scene.tif.aux.xml"] = []byte(`<PAMDataset><GeoTransform>10,2,1,20,0,-3</GeoTransform></PAMDataset>`)
	info, err = provider.DescribeMultiMedia(t.Context(), reader, refs, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireTIFFSpatial(t, info, datatype.BoundingBox{10, 14, 16, 20}, 4326)
	reader.files["scene.tif.aux.xml"] = []byte(`<PAMDataset><SRS>LOCAL_CS["survey",UNIT["metre",1]]</SRS></PAMDataset>`)
	info, err = provider.DescribeMultiMedia(t.Context(), reader, refs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Spatial.SRID != nil || len(info.Spatial.CRSDefinitions) != 1 || info.Spatial.CRSDefinitions[0].Source != datatype.CRSDefinitionSourceSidecarPAM || info.Spatial.CRSRef == "" {
		t.Fatal("custom PAM CRS lost or inherited internal EPSG", info.Spatial)
	}
}
func TestTIFFSidecarsWorldFileOrderIndependentOfRefs(t *testing.T) {
	reader := &tiffSidecarReader{files: map[string][]byte{"scene.tif": testEncodedTIFF(t)}}
	refs := tiffSpatialRefs()
	for _, ext := range []string{".wld", ".tifw", ".tfw"} {
		name := "scene" + ext
		reader.files[name] = []byte("10\n0\n0\n-10\n105\n195\n")
		refs = append(refs, format.NewRelatedRef(contentio.NewRef(name, "world_file"), false, false))
	}
	reader.files["scene.wld"] = []byte("1\n0\n0\n-1\n0\n0\n")
	reader.files["scene.tifw"] = reader.files["scene.wld"]
	info, err := newTIFFPlugin().DescribeMultiMedia(t.Context(), reader, refs, nil)
	if err != nil {
		t.Fatal(err)
	}
	requireTIFFSpatial(t, info, datatype.BoundingBox{100, 170, 120, 200}, 0)
	if !reflect.DeepEqual(reader.reads, []string{"scene.tif", "scene.tfw"}) {
		t.Fatal("world file priority changed", reader.reads)
	}
}
func TestTIFFSidecarsRejectInvalidSpatialInput(t *testing.T) {
	cases := []struct{ name, pam, world string }{
		{"xml", "<PAMDataset>", ""}, {"root", "<Other/>", ""}, {"trailing", "<PAMDataset/><Other/>", ""},
		{"external-entity", `<!DOCTYPE PAMDataset [<!ENTITY x SYSTEM "file:///private">]><PAMDataset><SRS>&x;</SRS></PAMDataset>`, ""},
		{"duplicate-srs", "<PAMDataset><SRS>EPSG:4326</SRS><SRS>EPSG:3857</SRS></PAMDataset>", ""},
		{"duplicate-transform", "<PAMDataset><GeoTransform>0,1,0,0,0,1</GeoTransform><GeoTransform>0,1,0,0,0,1</GeoTransform></PAMDataset>", ""},
		{"pam-short", "<PAMDataset><GeoTransform>1,2</GeoTransform></PAMDataset>", ""},
		{"pam-nan", "<PAMDataset><GeoTransform>NaN,1,0,0,0,-1</GeoTransform></PAMDataset>", ""},
		{"pam-singular", "<PAMDataset><GeoTransform>0,1,2,0,2,4</GeoTransform></PAMDataset>", ""},
		{"pam-budget", strings.Repeat(" ", 1<<20) + "<PAMDataset/>", ""},
		{"world-short", "", "1 0 0 -1 0"}, {"world-extra", "", "1 0 0 -1 0 0 3"},
		{"world-infinity", "", "1 0 0 -1 +Inf 0"}, {"world-singular", "", "1 2 2 4 0 0"},
		{"world-budget", "", strings.Repeat(" ", 4096) + "1 0 0 -1 0 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &tiffSidecarReader{files: map[string][]byte{"scene.tif": testEncodedTIFF(t)}}
			role := "world_file"
			if tc.pam != "" {
				role = "auxiliary_metadata"
				reader.files["scene.tif.aux.xml"] = []byte(tc.pam)
			} else {
				reader.files["scene.tfw"] = []byte(tc.world)
			}
			info, err := newTIFFPlugin().DescribeMultiMedia(t.Context(), reader, tiffSpatialRefs(role), nil)
			if err == nil || info != nil {
				t.Fatalf("invalid source returned partial success: %#v %v", info, err)
			}
		})
	}
}
func TestTIFFSidecarsPropagateReadAndCancellationFailures(t *testing.T) {
	sentinel := errors.New("source I/O failed")
	for _, failure := range []error{nil, sentinel} {
		reader := &tiffSidecarReader{files: map[string][]byte{"scene.tif": testEncodedTIFF(t)}, failure: failure}
		for _, role := range []string{"world_file", "auxiliary_metadata"} {
			_, err := newTIFFPlugin().DescribeMultiMedia(t.Context(), reader, tiffSpatialRefs(role), nil)
			want := failure
			if want == nil {
				want = contentio.ErrContentNotFound
			}
			if !errors.Is(err, want) {
				t.Fatalf("error=%v want %v", err, want)
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := newTIFFPlugin().DescribeMultiMedia(ctx, reader, tiffSpatialRefs(), nil); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}
