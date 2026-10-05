package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const tiffMetadataReadLimit = 1 << 20
const imageHeaderReadLimit = 1 << 20

// Plugin 实现 Image 格式插件。
type Plugin struct {
	options    *format.ParseOptions
	formatType format.FormatType
}

type tiffPlugin struct {
	*Plugin
}

// NewPlugin 创建 Image 插件。
func NewPlugin(opts *format.ParseOptions) *Plugin {
	if opts == nil {
		opts = format.DefaultParseOptions()
	}
	return &Plugin{options: opts, formatType: format.FormatImage}
}

func newPlugin(formatType format.FormatType) *Plugin {
	return &Plugin{options: format.DefaultParseOptions(), formatType: formatType}
}

func newTIFFPlugin() *tiffPlugin {
	return &tiffPlugin{Plugin: newPlugin(format.FormatTIFF)}
}

// inferColorModel 推断颜色模型
func inferColorModel(cfg image.Config) string {
	if cfg.ColorModel == nil {
		return "unknown"
	}

	// 简化实现：返回通用的颜色模型描述
	// Go 的 image.Config 不直接暴露颜色模型类型信息
	// 实际应用中可以通过解码完整图像来准确判断
	return "RGB"
}

func (p *Plugin) Format() format.FormatType {
	if p.formatType == "" {
		return format.FormatImage
	}
	return p.formatType
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	descriptor := format.FormatDescriptor{
		ID:       "builtin-" + string(p.Format()),
		Format:   p.Format(),
		I18nKey:  "format." + string(p.Format()),
		DataType: datatype.Media,
		Layouts:  []string{format.LayoutSingle},
	}
	switch p.Format() {
	case format.FormatJPEG:
		descriptor.Identification = format.FormatIdentification{Extensions: []string{".jpg", ".jpeg"}, MimeTypes: []string{"image/jpeg"}}
	case format.FormatPNG:
		descriptor.Identification = format.FormatIdentification{Extensions: []string{".png"}, MimeTypes: []string{"image/png"}}
	case format.FormatGIF:
		descriptor.Identification = format.FormatIdentification{Extensions: []string{".gif"}, MimeTypes: []string{"image/gif"}}
	case format.FormatWebP:
		descriptor.Identification = format.FormatIdentification{Extensions: []string{".webp"}, MimeTypes: []string{"image/webp"}}
	case format.FormatBMP:
		descriptor.Identification = format.FormatIdentification{Extensions: []string{".bmp"}, MimeTypes: []string{"image/bmp", "image/x-ms-bmp"}}
	case format.FormatTIFF:
		descriptor.Identification = format.FormatIdentification{Extensions: []string{".tif", ".tiff"}, MimeTypes: []string{"image/tiff"}}
		descriptor.Layouts = []string{format.LayoutSingle, format.LayoutMulti}
	}
	return descriptor
}

func (p *tiffPlugin) RelatedRefSpecs() []format.RelatedRefSpec {
	return TIFFRelatedRefSpecs()
}

func (p *tiffPlugin) DescribeRefs(refs []format.RelatedRef) []format.RefDescriptor {
	return TIFFDescribeRefs(refs)
}

func TIFFRelatedRefSpecs() []format.RelatedRefSpec {
	return []format.RelatedRefSpec{
		{Extension: ".tif", Role: "main", Required: true, Primary: true},
		{Extension: ".tfw", Role: "world_file", Required: false},
		{Extension: ".tifw", Role: "world_file", Required: false},
		{Extension: ".wld", Role: "world_file", Required: false},
		{Extension: ".prj", Role: "crs", Required: false},
		{Extension: ".aux.xml", Role: "auxiliary_metadata", Required: false},
		{Extension: ".ovr", Role: "overview", Required: false},
		{Extension: ".hdr", Role: "header", Required: false},
	}
}

func TIFFDescribeRefs(refs []format.RelatedRef) []format.RefDescriptor {
	descriptors := make([]format.RefDescriptor, 0, len(refs))
	for _, ref := range refs {
		ext := strings.ToLower(format.NormalizeExtension(filepath.Ext(ref.Ref.Path)))
		role := strings.TrimSpace(ref.Ref.Role)
		if role == "" {
			role = tiffRoleForExtension(ext)
		}
		dataType, formatType := tiffRefTypeForRole(role, ext)
		descriptors = append(descriptors, format.RefDescriptor{
			Key:       role,
			Path:      ref.Ref.Path,
			Role:      role,
			Label:     tiffLabelForRole(role, ext, ref.Ref),
			Required:  ref.Required,
			Primary:   ref.Primary,
			DataType:  dataType,
			Format:    formatType,
			Extension: ext,
		})
	}
	return descriptors
}

func tiffRoleForExtension(ext string) string {
	for _, spec := range TIFFRelatedRefSpecs() {
		if strings.EqualFold(format.NormalizeExtension(spec.Extension), ext) {
			return spec.Role
		}
	}
	return strings.TrimPrefix(ext, ".")
}

func tiffRefTypeForRole(role, ext string) (datatype.DataType, format.FormatType) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "world_file", "crs", "header", "auxiliary_metadata":
		return datatype.Document, format.FormatText
	default:
		switch strings.ToLower(strings.TrimSpace(ext)) {
		case ".tfw", ".tifw", ".wld", ".prj", ".hdr", ".aux.xml":
			return datatype.Document, format.FormatText
		default:
			return datatype.Unknown, format.FormatUnknown
		}
	}
}

func tiffLabelForRole(role, ext string, ref contentio.Ref) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "main":
		return "主文件"
	case "world_file":
		return "世界文件"
	case "crs":
		return "坐标参考"
	case "auxiliary_metadata":
		return "辅助元数据"
	case "overview":
		return "金字塔概览"
	case "header":
		return "头信息"
	default:
		if ext != "" {
			return strings.ToUpper(strings.TrimPrefix(ext, ".")) + " 内容"
		}
		return contentio.BaseName(ref)
	}
}

func (p *Plugin) DescribeMedia(ctx context.Context, input io.Reader, _ *format.ParseOptions) (*format.MediaDescribeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.Format() == format.FormatTIFF {
		return p.describeTIFFMedia(ctx, input)
	}
	var signature [2]byte
	if _, err := io.ReadFull(input, signature[:]); err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}
	var limited *io.LimitedReader
	var reader io.Reader = input
	isJPEG := signature == [2]byte{0xff, 0xd8}
	isAnimationImage := p.Format() == format.FormatGIF || p.Format() == format.FormatWebP ||
		signature == [2]byte{'G', 'I'} || signature == [2]byte{'R', 'I'}
	isHeaderImage := p.Format() == format.FormatWebP || p.Format() == format.FormatBMP ||
		signature == [2]byte{'R', 'I'} || signature == [2]byte{'B', 'M'}
	if isJPEG {
		limited = &io.LimitedReader{R: input, N: jpegMetadataReadLimit - 2}
		reader = limited
	} else if isAnimationImage {
		limited = &io.LimitedReader{R: input, N: animationReadLimit - 2}
		reader = limited
	} else if isHeaderImage {
		limited = &io.LimitedReader{R: input, N: imageHeaderReadLimit - 2}
		reader = limited
	}
	reader = io.MultiReader(bytes.NewReader(signature[:]), reader)
	var header bytes.Buffer
	decoder := reader
	if isJPEG || isAnimationImage {
		decoder = io.TeeReader(reader, &header)
	}
	if isAnimationImage {
		decoder = io.LimitReader(decoder, imageHeaderReadLimit)
	}
	cfg, formatName, err := image.DecodeConfig(decoder)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		if (isAnimationImage && header.Len() == imageHeaderReadLimit) || (isHeaderImage && limited.N == 0) {
			return nil, fmt.Errorf("image header exceeds %d byte read budget: %w", imageHeaderReadLimit, err)
		}
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}
	if p.Format() == format.FormatWebP || p.Format() == format.FormatBMP || p.Format() == format.FormatGIF {
		if formatName != string(p.Format()) {
			return nil, fmt.Errorf("image format %s does not match declared format %s", formatName, p.Format())
		}
		if cfg.Width <= 0 || cfg.Height <= 0 {
			return nil, fmt.Errorf("invalid %s image dimensions: %dx%d", formatName, cfg.Width, cfg.Height)
		}
	}
	info := &datatype.MediaInfo{
		Kind:       datatype.MediaKindImage,
		MIMEType:   imageMIMEType(formatName),
		Width:      cfg.Width,
		Height:     cfg.Height,
		Encoding:   formatName,
		ColorSpace: inferColorModel(cfg),
	}
	// DecodeConfig may report a default color model, particularly for VP8X;
	// it does not establish the source ICC color space.
	if formatName == "webp" || formatName == "bmp" {
		info.ColorSpace = ""
	}
	result := &format.MediaDescribeResult{Media: info}
	if formatName == "jpeg" {
		result.FormatInfo, err = describeJPEGExif(ctx, io.MultiReader(bytes.NewReader(header.Bytes()), reader), limited)
		if err != nil {
			return nil, err
		}
	}
	if formatName == "gif" || formatName == "webp" {
		result.FormatInfo, err = describeAnimation(ctx, formatName, io.MultiReader(bytes.NewReader(header.Bytes()), reader), limited)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (p *Plugin) describeTIFFMedia(ctx context.Context, input io.Reader) (*format.MediaDescribeResult, error) {
	data, err := readTIFFMetadata(input, tiffMetadataReadLimit)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	width, height, ok := extractTIFFDimensions(data)
	if !ok {
		return &format.MediaDescribeResult{
			FormatInfo: extractTIFFFormatInfo(data, nil),
		}, nil
	}
	info := &datatype.MediaInfo{
		Kind:       datatype.MediaKindImage,
		MIMEType:   "image/tiff",
		Width:      width,
		Height:     height,
		Encoding:   "tiff",
		ColorSpace: "unknown",
	}
	spatialInfo := extractGeoTIFFSpatial(data, width, height)
	return &format.MediaDescribeResult{
		Media:      info,
		Spatial:    spatialInfo,
		FormatInfo: extractTIFFFormatInfo(data, spatialInfo),
	}, nil
}

func readLimitedMetadata(input io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = tiffMetadataReadLimit
	}
	limited := io.LimitReader(input, limit)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func readTIFFMetadata(input io.Reader, limit int64) (tiffMetadata, error) {
	if limit <= 0 {
		limit = tiffMetadataReadLimit
	}
	if seeker, ok := input.(io.ReadSeeker); ok {
		return readSeekableTIFFMetadata(seeker, limit)
	}
	head, err := readLimitedMetadata(input, limit)
	if err != nil {
		return tiffMetadata{}, err
	}
	return newTIFFMetadata(head, nil), nil
}

func readSeekableTIFFMetadata(input io.ReadSeeker, limit int64) (tiffMetadata, error) {
	head := make([]byte, limit)
	n, err := io.ReadFull(input, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return tiffMetadata{}, err
	}
	head = head[:n]
	size, err := input.Seek(0, io.SeekEnd)
	if err != nil {
		return newTIFFMetadata(head, nil), nil
	}
	if size <= int64(len(head)) {
		return newTIFFMetadata(head, nil), nil
	}
	tailSize := limit
	if size < tailSize {
		tailSize = size
	}
	if _, err := input.Seek(size-tailSize, io.SeekStart); err != nil {
		return newTIFFMetadata(head, nil), nil
	}
	tail := make([]byte, tailSize)
	n, err = io.ReadFull(input, tail)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return tiffMetadata{}, err
	}
	tail = tail[:n]
	return newTIFFMetadata(head, &tiffWindow{offset: uint64(size - tailSize), data: tail}), nil
}

type tiffWindow struct {
	offset uint64
	data   []byte
}

type tiffMetadata struct {
	head []byte
	tail *tiffWindow
}

func newTIFFMetadata(head []byte, tail *tiffWindow) tiffMetadata {
	if tail == nil || tail.offset == 0 || len(tail.data) == 0 {
		return tiffMetadata{head: head}
	}
	if tail.offset <= uint64(len(head)) {
		merged := append([]byte(nil), head...)
		overlap := int(uint64(len(head)) - tail.offset)
		if overlap < len(tail.data) {
			merged = append(merged, tail.data[overlap:]...)
		}
		return tiffMetadata{head: merged}
	}
	return tiffMetadata{head: head, tail: tail}
}

func (m tiffMetadata) Len() int {
	size := len(m.head)
	if m.tail != nil {
		tailEnd := int(m.tail.offset) + len(m.tail.data)
		if tailEnd > size {
			size = tailEnd
		}
	}
	return size
}

func (m tiffMetadata) firstBytes(n int) []byte {
	if n <= 0 || len(m.head) < n {
		return nil
	}
	return m.head[:n]
}

func (m tiffMetadata) slice(offset, size uint64) ([]byte, bool) {
	if size == 0 {
		return []byte{}, true
	}
	end := offset + size
	if end < offset {
		return nil, false
	}
	if end <= uint64(len(m.head)) {
		return m.head[offset:end], true
	}
	if m.tail != nil && offset >= m.tail.offset && end <= m.tail.offset+uint64(len(m.tail.data)) {
		start := offset - m.tail.offset
		return m.tail.data[start : start+size], true
	}
	return nil, false
}

func (m tiffMetadata) byteOrder() (binary.ByteOrder, bool) {
	header := m.firstBytes(4)
	if len(header) < 4 {
		return nil, false
	}
	switch string(header[:2]) {
	case "II":
		return binary.LittleEndian, true
	case "MM":
		return binary.BigEndian, true
	default:
		return nil, false
	}
}

func imageMIMEType(formatName string) string {
	switch strings.ToLower(strings.TrimSpace(formatName)) {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "tiff":
		return "image/tiff"
	default:
		if formatName == "" {
			return ""
		}
		return "image/" + strings.ToLower(formatName)
	}
}

func init() {
	for _, formatType := range []format.FormatType{
		format.FormatJPEG,
		format.FormatPNG,
		format.FormatGIF,
		format.FormatWebP,
		format.FormatBMP,
	} {
		if err := format.RegisterFormatPlugin(newPlugin(formatType)); err != nil {
			panic(err)
		}
	}
	_ = format.RegisterFormatPlugin(newTIFFPlugin())
	_ = format.RegisterFormatPlugin(newPlugin(format.FormatImage))
}
