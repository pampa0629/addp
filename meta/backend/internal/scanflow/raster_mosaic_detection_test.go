package scanflow

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/scanresource"
)

type mosaicManifestReader struct {
	plugin.StoreProvider
	data            string
	err             error
	ranges, streams int
	target          plugin.EngineCatalogPath
	options         plugin.ReadOptions
}

func (r *mosaicManifestReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	r.streams++
	return nil, io.ErrUnexpectedEOF
}

func (r *mosaicManifestReader) OpenRange(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
	r.ranges++
	r.target, r.options = target, options
	if r.err != nil {
		return nil, r.err
	}
	return io.NopCloser(strings.NewReader(r.data)), nil
}

// Hiding Range models an engine with only sequential content access.
type sequentialMosaicManifestReader struct{ plugin.ContentReadableProvider }

func TestMosaicManifestStructuralReadIsBoundedAndNeverFallsBack(t *testing.T) {
	manifest := `{"schema_version":"addp.raster_mosaic.v1","data_type":"media","format":"raster_mosaic","layout":"whole","refs":{"index":"index/source-index.json"}}`
	for _, name := range []string{"valid", "oversize catalog", "oversize response", "range failure", "no Range"} {
		t.Run(name, func(t *testing.T) {
			reader := &mosaicManifestReader{data: manifest}
			var content plugin.ContentReadableProvider = reader
			resource := scanresource.StorageResource{RootName: "bucket", Path: "same %/mosaic.addp.json", SizeBytes: int64(len(manifest))}
			switch name {
			case "oversize catalog":
				resource.SizeBytes = rasterMosaicManifestMaxBytes + 1
			case "oversize response":
				reader.data += strings.Repeat(" ", int(rasterMosaicManifestMaxBytes))
			case "range failure":
				reader.err = io.ErrUnexpectedEOF
			case "no Range":
				content = sequentialMosaicManifestReader{reader}
			}
			_, err := readRasterMosaicManifest(t.Context(), content, nil, 9, resource)
			if (err == nil) != (name == "valid") || reader.streams != 0 {
				t.Fatalf("err=%v streams=%d", err, reader.streams)
			}
			if name == "oversize catalog" || name == "no Range" {
				if reader.ranges != 0 {
					t.Fatal("invalid boundary reached source I/O")
				}
			} else if reader.ranges != 1 || reader.target.StringPath() != "bucket/same %/mosaic.addp.json" || reader.options.Offset != 0 || reader.options.Length != rasterMosaicManifestMaxBytes+1 {
				t.Fatalf("unexpected structural read: %s %+v", reader.target.StringPath(), reader.options)
			}
		})
	}
}
