package ksplat

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const kplatHeaderSizeBytes = 4096

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register KSPLAT format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatKSplat
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-ksplat",
		Format:   format.FormatKSplat,
		I18nKey:  "format.ksplat",
		DataType: datatype.GaussianSplat,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".ksplat"},
			MimeTypes:  []string{"application/vnd.gaussian-ksplat"},
		},
	}
}

func (p *Plugin) DescribeGaussianSplat(ctx context.Context, input format.GaussianSplatDescribeInput, _ *format.ParseOptions) (*format.GaussianSplatDescribeResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	info := &datatype.GaussianSplatInfo{
		Representation: datatype.GaussianSplatRepresentation3DGS,
	}
	formatInfo := map[string]interface{}{
		"encoding": "ksplat",
	}
	if header, err := readKPlatHeader(input.Reader); err == nil && header != nil {
		if header.SplatCount > 0 {
			splatCount := int64(header.SplatCount)
			info.SplatCount = &splatCount
		}
		formatInfo["version_major"] = int(header.VersionMajor)
		formatInfo["version_minor"] = int(header.VersionMinor)
		formatInfo["section_count"] = int64(header.SectionCount)
		formatInfo["max_section_count"] = int64(header.MaxSectionCount)
		formatInfo["max_splat_count"] = int64(header.MaxSplatCount)
		formatInfo["compression_level"] = int(header.CompressionLevel)
		formatInfo["scene_center"] = []float64{
			float64(header.SceneCenter[0]),
			float64(header.SceneCenter[1]),
			float64(header.SceneCenter[2]),
		}
	}
	return &format.GaussianSplatDescribeResult{
		GaussianSplat: info,
		FormatInfo:    formatInfo,
	}, nil
}

type kplatHeader struct {
	VersionMajor     byte
	VersionMinor     byte
	MaxSectionCount  uint32
	SectionCount     uint32
	MaxSplatCount    uint32
	SplatCount       uint32
	CompressionLevel uint16
	SceneCenter      [3]float32
}

func readKPlatHeader(reader io.Reader) (*kplatHeader, error) {
	if reader == nil {
		return nil, nil
	}
	headerBytes, err := io.ReadAll(io.LimitReader(reader, kplatHeaderSizeBytes))
	if err != nil {
		return nil, err
	}
	if len(headerBytes) < kplatHeaderSizeBytes {
		return nil, nil
	}
	return &kplatHeader{
		VersionMajor:     headerBytes[0],
		VersionMinor:     headerBytes[1],
		MaxSectionCount:  binary.LittleEndian.Uint32(headerBytes[4:8]),
		SectionCount:     binary.LittleEndian.Uint32(headerBytes[8:12]),
		MaxSplatCount:    binary.LittleEndian.Uint32(headerBytes[12:16]),
		SplatCount:       binary.LittleEndian.Uint32(headerBytes[16:20]),
		CompressionLevel: binary.LittleEndian.Uint16(headerBytes[20:22]),
		SceneCenter: [3]float32{
			math.Float32frombits(binary.LittleEndian.Uint32(headerBytes[24:28])),
			math.Float32frombits(binary.LittleEndian.Uint32(headerBytes[28:32])),
			math.Float32frombits(binary.LittleEndian.Uint32(headerBytes[32:36])),
		},
	}, nil
}
