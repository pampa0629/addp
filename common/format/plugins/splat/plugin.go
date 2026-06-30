package splat

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

const (
	splatRecordSize               = 32
	maxExactSplatBoundsRecords    = 100000
	splatSampledBoundsSampleCount = 8192
	splatAnisotropyWarningRatio   = 20
)

type Plugin struct{}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func init() {
	if err := format.RegisterFormatPlugin(NewPlugin()); err != nil {
		panic(fmt.Sprintf("failed to register SPLAT format plugin: %v", err))
	}
}

func (p *Plugin) Format() format.FormatType {
	return format.FormatSplat
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "builtin-splat",
		Format:   format.FormatSplat,
		I18nKey:  "format.splat",
		DataType: datatype.GaussianSplat,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".splat"},
			MimeTypes:  []string{"application/vnd.gaussian-splat"},
		},
	}
}

func (p *Plugin) DescribeGaussianSplat(ctx context.Context, input format.GaussianSplatDescribeInput, _ *format.ParseOptions) (*format.GaussianSplatDescribeResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	recordSize := int64(splatRecordSize)
	recordCount := int64(0)
	if input.SizeBytes >= recordSize {
		recordCount = input.SizeBytes / recordSize
	}
	info := &datatype.GaussianSplatInfo{
		Representation: datatype.GaussianSplatRepresentation3DGS,
	}
	if recordCount > 0 {
		info.SplatCount = int64Ptr(recordCount)
	}
	formatInfo := map[string]interface{}{
		"encoding":    "splat",
		"record_size": int64(splatRecordSize),
	}
	if recordCount > maxExactSplatBoundsRecords && input.RangeReader != nil {
		sampled, err := describeSplatSampledRecords(ctx, input.RangeReader, input.Ref, input.SizeBytes)
		if err != nil {
			return nil, err
		}
		if sampled != nil {
			info.SampledBounds3D = sampled.SampledBounds3D
			info.SampledBoundsMethod = sampled.SampledBoundsMethod
			info.SampledBoundsSampleCount = sampled.SampledBoundsSampleCount
			mergeSplatFormatInfo(formatInfo, sampled.FormatInfo)
		}
	} else {
		reader := input.Reader
		if reader == nil && input.RangeReader != nil {
			rc, err := input.RangeReader.Open(ctx, input.Ref)
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			reader = rc
		}
		exact, err := describeSplatRecords(ctx, reader)
		if err != nil {
			return nil, err
		}
		if exact.RecordCount > 0 {
			info.SplatCount = int64Ptr(exact.RecordCount)
		}
		info.Bounds3D = exact.Bounds
		mergeSplatFormatInfo(formatInfo, exact.FormatInfo)
	}
	return &format.GaussianSplatDescribeResult{
		GaussianSplat: info,
		FormatInfo:    formatInfo,
	}, nil
}

func describeSplatSampledRecords(ctx context.Context, reader contentio.RangeReader, ref contentio.Ref, sizeBytes int64) (*splatDescribeFacts, error) {
	recordSize := int64(splatRecordSize)
	if reader == nil || sizeBytes < recordSize {
		return nil, nil
	}
	recordCount := sizeBytes / recordSize
	indexes := uniformSplatSampleIndexes(recordCount, splatSampledBoundsSampleCount)
	if len(indexes) == 0 {
		return nil, nil
	}
	var bounds splatBounds
	stats := newSplatScaleStats()
	buffer := make([]byte, splatRecordSize)
	for _, index := range indexes {
		rc, err := reader.OpenRange(ctx, ref, index*recordSize, recordSize)
		if err != nil {
			return nil, err
		}
		n, readErr := io.ReadFull(rc, buffer)
		closeErr := rc.Close()
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if n < splatRecordSize {
			continue
		}
		x := float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[0:4])))
		y := float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[4:8])))
		z := float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[8:12])))
		bounds.add(x, y, z)
		stats.add(
			float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[12:16]))),
			float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[16:20]))),
			float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[20:24]))),
			buffer[27],
		)
	}
	result := bounds.result()
	if result == nil {
		return nil, nil
	}
	return &splatDescribeFacts{
		SampledBounds3D:          result,
		SampledBoundsMethod:      "sampled_splat_records",
		SampledBoundsSampleCount: int64Ptr(int64(len(indexes))),
		FormatInfo:               stats.formatInfo("sampled_splat_records"),
	}, nil
}

func describeSplatRecords(ctx context.Context, input io.Reader) (*splatDescribeFacts, error) {
	if input == nil {
		return &splatDescribeFacts{}, nil
	}
	var count int64
	var bounds splatBounds
	stats := newSplatScaleStats()
	buffer := make([]byte, splatRecordSize)
	for {
		_, err := io.ReadFull(input, buffer)
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if count%65536 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		x := float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[0:4])))
		y := float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[4:8])))
		z := float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[8:12])))
		bounds.add(x, y, z)
		stats.add(
			float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[12:16]))),
			float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[16:20]))),
			float64(math.Float32frombits(binary.LittleEndian.Uint32(buffer[20:24]))),
			buffer[27],
		)
		count++
	}
	return &splatDescribeFacts{
		RecordCount: count,
		Bounds:      bounds.result(),
		FormatInfo:  stats.formatInfo("exact_splat_records"),
	}, nil
}

type splatDescribeFacts struct {
	RecordCount              int64
	Bounds                   *datatype.Bounds3D
	SampledBounds3D          *datatype.Bounds3D
	SampledBoundsMethod      string
	SampledBoundsSampleCount *int64
	FormatInfo               map[string]interface{}
}

type splatBounds struct {
	minX float64
	minY float64
	minZ float64
	maxX float64
	maxY float64
	maxZ float64
	seen bool
}

type splatScaleStats struct {
	sampleCount          int64
	invalidScaleCount    int64
	lowAlphaCount        int64
	anisotropicCount     int64
	maxScale             float64
	maxAnisotropyRatio   float64
	anisotropyRatios     []float64
	maxScaleDistribution []float64
}

func newSplatScaleStats() *splatScaleStats {
	return &splatScaleStats{
		anisotropyRatios:     make([]float64, 0, 1024),
		maxScaleDistribution: make([]float64, 0, 1024),
	}
}

func (s *splatScaleStats) add(scaleX, scaleY, scaleZ float64, alpha byte) {
	if s == nil {
		return
	}
	s.sampleCount++
	if alpha < 25 {
		s.lowAlphaCount++
	}
	scales := []float64{scaleX, scaleY, scaleZ}
	for _, scale := range scales {
		if math.IsNaN(scale) || math.IsInf(scale, 0) || scale < 0 {
			s.invalidScaleCount++
			return
		}
	}
	sort.Float64s(scales)
	maxScale := scales[2]
	minScale := math.Max(scales[0], 1e-12)
	ratio := maxScale / minScale
	if ratio > splatAnisotropyWarningRatio {
		s.anisotropicCount++
	}
	if maxScale > s.maxScale {
		s.maxScale = maxScale
	}
	if ratio > s.maxAnisotropyRatio {
		s.maxAnisotropyRatio = ratio
	}
	s.anisotropyRatios = append(s.anisotropyRatios, ratio)
	s.maxScaleDistribution = append(s.maxScaleDistribution, maxScale)
}

func (s *splatScaleStats) formatInfo(method string) map[string]interface{} {
	if s == nil || s.sampleCount == 0 {
		return nil
	}
	scaleStats := map[string]interface{}{
		"method":                    method,
		"sample_count":              s.sampleCount,
		"invalid_scale_count":       s.invalidScaleCount,
		"low_alpha_count":           s.lowAlphaCount,
		"anisotropic_count":         s.anisotropicCount,
		"anisotropic_ratio_percent": percentOf(s.anisotropicCount, s.sampleCount),
		"max_scale":                 s.maxScale,
		"max_anisotropy_ratio":      s.maxAnisotropyRatio,
		"anisotropy_ratio_p50":      percentileFloat64(s.anisotropyRatios, 50),
		"anisotropy_ratio_p95":      percentileFloat64(s.anisotropyRatios, 95),
		"anisotropy_ratio_p99":      percentileFloat64(s.anisotropyRatios, 99),
		"max_scale_p95":             percentileFloat64(s.maxScaleDistribution, 95),
		"max_scale_p99":             percentileFloat64(s.maxScaleDistribution, 99),
	}
	info := map[string]interface{}{
		"scale_stats": scaleStats,
	}
	if s.sampleCount > 0 && percentOf(s.anisotropicCount, s.sampleCount) >= 20 {
		info["render_diagnostic"] = map[string]interface{}{
			"extreme_anisotropy":      true,
			"recommended_render_mode": "2d",
		}
	}
	return info
}

func mergeSplatFormatInfo(target map[string]interface{}, source map[string]interface{}) {
	if len(source) == 0 {
		return
	}
	for key, value := range source {
		target[key] = value
	}
}

func percentileFloat64(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copied := append([]float64(nil), values...)
	sort.Float64s(copied)
	if percentile <= 0 {
		return copied[0]
	}
	if percentile >= 100 {
		return copied[len(copied)-1]
	}
	position := (percentile / 100) * float64(len(copied)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return copied[lower]
	}
	weight := position - float64(lower)
	return copied[lower]*(1-weight) + copied[upper]*weight
}

func percentOf(count, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(count) * 100 / float64(total)
}

func (b *splatBounds) add(x, y, z float64) {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(z) ||
		math.IsInf(x, 0) || math.IsInf(y, 0) || math.IsInf(z, 0) {
		return
	}
	if !b.seen {
		b.minX, b.maxX = x, x
		b.minY, b.maxY = y, y
		b.minZ, b.maxZ = z, z
		b.seen = true
		return
	}
	if x < b.minX {
		b.minX = x
	}
	if y < b.minY {
		b.minY = y
	}
	if z < b.minZ {
		b.minZ = z
	}
	if x > b.maxX {
		b.maxX = x
	}
	if y > b.maxY {
		b.maxY = y
	}
	if z > b.maxZ {
		b.maxZ = z
	}
}

func (b *splatBounds) result() *datatype.Bounds3D {
	if b == nil || !b.seen {
		return nil
	}
	return &datatype.Bounds3D{
		MinX: float64Ptr(b.minX),
		MinY: float64Ptr(b.minY),
		MinZ: float64Ptr(b.minZ),
		MaxX: float64Ptr(b.maxX),
		MaxY: float64Ptr(b.maxY),
		MaxZ: float64Ptr(b.maxZ),
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}

func uniformSplatSampleIndexes(count int64, maxSamples int) []int64 {
	if count <= 0 || maxSamples <= 0 {
		return nil
	}
	if count <= int64(maxSamples) {
		indexes := make([]int64, 0, count)
		for i := int64(0); i < count; i++ {
			indexes = append(indexes, i)
		}
		return indexes
	}
	indexes := make([]int64, 0, maxSamples)
	last := int64(-1)
	for i := 0; i < maxSamples; i++ {
		index := int64(math.Round(float64(i) * float64(count-1) / float64(maxSamples-1)))
		if index == last {
			continue
		}
		indexes = append(indexes, index)
		last = index
	}
	return indexes
}
