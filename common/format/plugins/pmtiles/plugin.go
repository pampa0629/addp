package pmtiles

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	archive "github.com/addp/common/pmtiles"
)

type Plugin struct{}

func init() {
	if err := format.RegisterFormatPlugin(&Plugin{}); err != nil {
		panic(fmt.Sprintf("failed to register PMTiles format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType { return format.FormatPMTiles }

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID: "builtin-pmtiles", Format: format.FormatPMTiles, I18nKey: "format.pmtiles", DataType: datatype.Media,
		Layouts: []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".pmtiles"}, MimeTypes: []string{"application/vnd.pmtiles"}, ContentSignatures: []string{"PMTiles\x03"},
		},
	}
}

func (p *Plugin) SniffFormat(peek []byte) bool {
	return len(peek) >= 8 && bytes.Equal(peek[:8], append([]byte("PMTiles"), 3))
}

func (p *Plugin) DescribeMedia(ctx context.Context, input io.Reader, _ *format.ParseOptions) (*format.MediaDescribeResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	headerBytes := make([]byte, archive.HeaderSize)
	if _, err := io.ReadFull(input, headerBytes); err != nil {
		return nil, fmt.Errorf("read PMTiles header: %w", err)
	}
	header, err := archive.ParseHeaderBytes(headerBytes)
	if err != nil {
		return nil, err
	}
	headerHash, err := archive.HeaderHash(headerBytes)
	if err != nil {
		return nil, err
	}
	bounds := archive.Bounds(header)
	srid := 4326
	extent := datatype.BoundingBox(bounds)
	formatInfo := archive.FormatInfo(header)
	formatInfo["header_hash"] = headerHash
	return &format.MediaDescribeResult{
		Media:      &datatype.MediaInfo{MIMEType: "application/vnd.pmtiles", Encoding: "pmtiles-v3"},
		Spatial:    &datatype.SpatialInfo{SRID: &srid, CRSRef: "EPSG:4326", Extent: &extent},
		FormatInfo: formatInfo,
	}, nil
}
