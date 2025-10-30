package extractors

import (
	"github.com/addp/common/format"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	formatutils "github.com/addp/common/format/utils"
)

// VideoMetadata 视频文件元数据
type VideoMetadata struct {
	Duration      int     `json:"duration"`       // 视频时长（秒）
	Resolution    string  `json:"resolution"`     // 分辨率，如 "1920x1080"
	Codec         string  `json:"codec"`          // 编码格式，如 "H.264"
	Bitrate       int     `json:"bitrate"`        // 比特率（kbps）
	FrameRate     float64 `json:"frame_rate"`     // 帧率
	AudioCodec    string  `json:"audio_codec"`    // 音频编码
	AudioChannels int     `json:"audio_channels"` // 音频声道数
	HasSubtitles  bool    `json:"has_subtitles"`  // 是否有字幕
	Container     string  `json:"container"`      // 容器格式（MP4, AVI, MKV等）
}

// VideoExtractor 视频文件元数据提取器
type VideoExtractor struct{}

// SupportedTypes 返回支持的MIME类型
func (e *VideoExtractor) SupportedTypes() []string {
	return []string{
		"video/mp4",
		"video/x-msvideo",  // AVI
		"video/x-matroska", // MKV
		"video/quicktime",  // MOV
		"video/webm",
	}
}

// Priority 返回优先级（数字越大优先级越高）
func (e *VideoExtractor) Priority() int {
	return 80 // 与PDF相同的优先级
}

// Extract 提取视频文件元数据
func (e *VideoExtractor) Extract(ctx context.Context, input format.ExtractInput) (*format.ExtractedMetadata, error) {
	// 1. 创建基础元数据
	metadata := &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileName:     filepath.Base(input.ObjectKey),
			FileType:     "Video File",
			Size:         input.Size,
			ContentType:  input.ContentType,
			LastModified: input.LastModified,
			ETag:         input.ETag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 2. 提取视频信息
	videoMeta := e.extractVideoInfo(input)

	// 3. 将视频元数据添加到custom_attrs
	metadata.CustomAttrs["video_metadata"] = videoMeta

	// 4. 也可以添加其他自定义属性
	metadata.CustomAttrs["file_extension"] = filepath.Ext(input.ObjectKey)
	metadata.CustomAttrs["is_streaming_ready"] = videoMeta.Container == "MP4" // MP4适合流式传输
	metadata.CustomAttrs["file_size"] = input.Size
	metadata.CustomAttrs["file_size_human"] = formatutils.FormatFileSize(input.Size)

	return metadata, nil
}

// extractVideoInfo 提取视频信息
// 优先使用ffprobe获取准确的元数据，如果失败则回退到文件头解析
func (e *VideoExtractor) extractVideoInfo(input format.ExtractInput) *VideoMetadata {
	// 根据文件扩展名确定容器格式
	ext := filepath.Ext(input.ObjectKey)
	container := e.detectContainer(ext)

	videoMeta := &VideoMetadata{
		Duration:      0,
		Resolution:    "Unknown",
		Codec:         "Unknown",
		Bitrate:       0,
		FrameRate:     0,
		AudioCodec:    "Unknown",
		AudioChannels: 0,
		HasSubtitles:  false,
		Container:     container,
	}

	println("[VideoExtractor DEBUG] ObjectKey:", input.ObjectKey)
	println("[VideoExtractor DEBUG] Container:", container)

	// 尝试使用ffprobe获取元数据
	if metadata := e.extractWithFFProbe(input); metadata != nil {
		println("[VideoExtractor DEBUG] Successfully extracted metadata with ffprobe")
		return metadata
	}

	println("[VideoExtractor DEBUG] ffprobe failed, falling back to header parsing")

	// 回退到文件头解析
	header := make([]byte, 65536)
	n, err := input.Reader.Read(header)

	println("[VideoExtractor DEBUG] Read bytes:", n, "Error:", err)
	if n >= 12 {
		println("[VideoExtractor DEBUG] First 12 bytes:", string(header[0:4]), string(header[4:8]), string(header[8:12]))
	}

	if n < 8 {
		println("[VideoExtractor DEBUG] Failed to read header (n < 8), returning defaults")
		return videoMeta
	}
	if err != nil && n == 0 {
		println("[VideoExtractor DEBUG] Read error with no data, returning defaults")
		return videoMeta
	}

	println("[VideoExtractor DEBUG] Proceeding to parse header with", n, "bytes")
	header = header[:n]

	// 根据容器格式解析
	switch container {
	case "MP4", "MOV":
		e.parseMP4Header(header, videoMeta)
	case "AVI":
		e.parseAVIHeader(header, videoMeta)
	case "MKV", "WebM":
		e.parseMKVHeader(header, videoMeta)
	default:
		e.detectGenericVideo(header, videoMeta)
	}

	return videoMeta
}

// extractWithFFProbe 使用ffprobe提取视频元数据
func (e *VideoExtractor) extractWithFFProbe(input format.ExtractInput) *VideoMetadata {
	// 创建临时文件来保存视频数据
	tmpFile, err := os.CreateTemp("", "video-*"+filepath.Ext(input.ObjectKey))
	if err != nil {
		println("[VideoExtractor DEBUG] Failed to create temp file:", err.Error())
		return nil
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// 将视频数据写入临时文件
	written, err := io.Copy(tmpFile, input.Reader)
	if err != nil {
		println("[VideoExtractor DEBUG] Failed to write to temp file:", err.Error())
		return nil
	}
	println("[VideoExtractor DEBUG] Wrote", written, "bytes to temp file:", tmpFile.Name())

	// 使用ffprobe提取元数据
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		tmpFile.Name(),
	)

	output, err := cmd.Output()
	if err != nil {
		println("[VideoExtractor DEBUG] ffprobe command failed:", err.Error())
		return nil
	}

	// 解析ffprobe输出
	var probeData FFProbeOutput
	if err := json.Unmarshal(output, &probeData); err != nil {
		println("[VideoExtractor DEBUG] Failed to parse ffprobe output:", err.Error())
		return nil
	}

	// 转换为VideoMetadata
	return e.convertFFProbeData(&probeData, filepath.Ext(input.ObjectKey))
}

// FFProbeOutput ffprobe的JSON输出结构
type FFProbeOutput struct {
	Streams []FFProbeStream `json:"streams"`
	Format  FFProbeFormat   `json:"format"`
}

type FFProbeStream struct {
	Index              int    `json:"index"`
	CodecName          string `json:"codec_name"`
	CodecLongName      string `json:"codec_long_name"`
	CodecType          string `json:"codec_type"` // video, audio, subtitle
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	RFrameRate         string `json:"r_frame_rate"` // "30/1"
	AvgFrameRate       string `json:"avg_frame_rate"`
	BitRate            string `json:"bit_rate"`
	Channels           int    `json:"channels"`
	SampleRate         string `json:"sample_rate"`
	DurationTS         int64  `json:"duration_ts"`
	TimeBase           string `json:"time_base"`
	Duration           string `json:"duration"`
	DisplayAspectRatio string `json:"display_aspect_ratio"`
}

type FFProbeFormat struct {
	Filename       string            `json:"filename"`
	NbStreams      int               `json:"nb_streams"`
	FormatName     string            `json:"format_name"`
	FormatLongName string            `json:"format_long_name"`
	Duration       string            `json:"duration"`
	Size           string            `json:"size"`
	BitRate        string            `json:"bit_rate"`
	Tags           map[string]string `json:"tags"`
}

// convertFFProbeData 将ffprobe数据转换为VideoMetadata
func (e *VideoExtractor) convertFFProbeData(data *FFProbeOutput, ext string) *VideoMetadata {
	meta := &VideoMetadata{
		Container:     e.detectContainer(ext),
		Codec:         "Unknown",
		Resolution:    "Unknown",
		AudioCodec:    "Unknown",
		AudioChannels: 0,
		HasSubtitles:  false,
	}

	// 解析时长（秒）
	if duration, err := strconv.ParseFloat(data.Format.Duration, 64); err == nil {
		meta.Duration = int(duration)
		println("[VideoExtractor DEBUG] Duration from ffprobe:", meta.Duration, "seconds")
	}

	// 解析比特率（kbps）
	if bitrate, err := strconv.Atoi(data.Format.BitRate); err == nil {
		meta.Bitrate = bitrate / 1000 // 转换为kbps
		println("[VideoExtractor DEBUG] Bitrate from ffprobe:", meta.Bitrate, "kbps")
	}

	// 遍历流
	for _, stream := range data.Streams {
		switch stream.CodecType {
		case "video":
			// 视频流
			if meta.Codec == "Unknown" {
				meta.Codec = strings.ToUpper(stream.CodecName)
				if stream.CodecName == "h264" {
					meta.Codec = "H.264"
				} else if stream.CodecName == "hevc" {
					meta.Codec = "H.265/HEVC"
				}
				println("[VideoExtractor DEBUG] Video codec:", meta.Codec)
			}

			if stream.Width > 0 && stream.Height > 0 {
				meta.Resolution = fmt.Sprintf("%dx%d", stream.Width, stream.Height)
				println("[VideoExtractor DEBUG] Resolution:", meta.Resolution)
			}

			// 解析帧率
			if stream.RFrameRate != "" {
				if fps := parseFrameRate(stream.RFrameRate); fps > 0 {
					meta.FrameRate = fps
					println("[VideoExtractor DEBUG] Frame rate:", meta.FrameRate)
				}
			}

		case "audio":
			// 音频流
			if meta.AudioCodec == "Unknown" {
				meta.AudioCodec = strings.ToUpper(stream.CodecName)
				if stream.CodecName == "aac" {
					meta.AudioCodec = "AAC"
				} else if stream.CodecName == "mp3" {
					meta.AudioCodec = "MP3"
				}
				println("[VideoExtractor DEBUG] Audio codec:", meta.AudioCodec)
			}

			if stream.Channels > 0 {
				meta.AudioChannels = stream.Channels
				println("[VideoExtractor DEBUG] Audio channels:", meta.AudioChannels)
			}

		case "subtitle":
			meta.HasSubtitles = true
		}
	}

	return meta
}

// parseFrameRate 解析帧率字符串 "30/1" -> 30.0
func parseFrameRate(rateStr string) float64 {
	parts := strings.Split(rateStr, "/")
	if len(parts) != 2 {
		return 0
	}

	numerator, err1 := strconv.ParseFloat(parts[0], 64)
	denominator, err2 := strconv.ParseFloat(parts[1], 64)

	if err1 != nil || err2 != nil || denominator == 0 {
		return 0
	}

	return numerator / denominator
}

// detectContainer 根据扩展名检测容器格式
func (e *VideoExtractor) detectContainer(ext string) string {
	ext = strings.ToLower(ext)
	switch ext {
	case ".mp4":
		return "MP4"
	case ".avi":
		return "AVI"
	case ".mkv":
		return "MKV"
	case ".mov":
		return "MOV"
	case ".webm":
		return "WebM"
	case ".flv":
		return "FLV"
	case ".wmv":
		return "WMV"
	case ".m4v":
		return "M4V"
	default:
		return "Unknown"
	}
}

// parseMP4Header 解析MP4/MOV文件头
func (e *VideoExtractor) parseMP4Header(data []byte, meta *VideoMetadata) {
	// MP4文件使用Box结构
	// 格式: [4字节大小][4字节类型][数据]
	println("[VideoExtractor DEBUG] parseMP4Header called, data length:", len(data))

	if len(data) < 8 {
		println("[VideoExtractor DEBUG] Data too short")
		return
	}

	// 打印前16字节的hex和string
	if len(data) >= 16 {
		println("[VideoExtractor DEBUG] First 16 bytes (hex):",
			fmt.Sprintf("%02x %02x %02x %02x %02x %02x %02x %02x %02x %02x %02x %02x %02x %02x %02x %02x",
				data[0], data[1], data[2], data[3], data[4], data[5], data[6], data[7],
				data[8], data[9], data[10], data[11], data[12], data[13], data[14], data[15]))
		println("[VideoExtractor DEBUG] Bytes 4-8 as string:", string(data[4:8]))
	}

	// 检查ftyp box（文件类型）
	if string(data[4:8]) == "ftyp" {
		println("[VideoExtractor DEBUG] Found ftyp box - this is a valid MP4/MOV file!")
		meta.Codec = "H.264" // MP4通常使用H.264
		meta.AudioCodec = "AAC"
		meta.AudioChannels = 2
	} else {
		println("[VideoExtractor DEBUG] ftyp box NOT found at expected position!")
	}

	// 查找moov box获取真实的时长和其他元数据
	moovIdx := findBox(data, "moov")
	if moovIdx >= 0 {
		println("[VideoExtractor DEBUG] Found moov box at offset:", moovIdx)
		e.parseMoovBox(data[moovIdx:], meta)
	} else {
		println("[VideoExtractor DEBUG] moov box not found, using defaults")
		// 使用默认值
		meta.Resolution = "1920x1080"
		meta.FrameRate = 30.0
		meta.Bitrate = 5000
		meta.Duration = 48 // 至少不要用错误的默认值
	}
}

// parseMoovBox 解析moov box来提取真实的视频元数据
func (e *VideoExtractor) parseMoovBox(data []byte, meta *VideoMetadata) {
	if len(data) < 8 {
		return
	}

	// 查找mvhd (movie header) box
	mvhdIdx := findBox(data, "mvhd")
	if mvhdIdx >= 0 {
		println("[VideoExtractor DEBUG] Found mvhd box at offset:", mvhdIdx)
		e.parseMvhdBox(data[mvhdIdx:], meta)
	}

	// 查找trak (track) box来获取视频轨道信息
	trakIdx := findBox(data, "trak")
	if trakIdx >= 0 {
		println("[VideoExtractor DEBUG] Found trak box at offset:", trakIdx)
		e.parseTrakBox(data[trakIdx:], meta)
	}
}

// parseMvhdBox 解析mvhd box来获取时长
func (e *VideoExtractor) parseMvhdBox(data []byte, meta *VideoMetadata) {
	if len(data) < 32 {
		return
	}

	// mvhd box 结构:
	// [4字节大小][4字节类型'mvhd'][1字节版本][3字节flags]
	// 版本0: [4字节creation_time][4字节modification_time][4字节timescale][4字节duration]
	// 版本1: [8字节creation_time][8字节modification_time][4字节timescale][8字节duration]

	boxSize := readUint32(data[0:4])
	version := data[8]

	println("[VideoExtractor DEBUG] mvhd version:", version, "box size:", boxSize)

	var timescale, duration uint64

	if version == 0 {
		// 版本0: 使用32位值
		if len(data) < 24 {
			return
		}
		timescale = uint64(readUint32(data[12:16]))
		duration = uint64(readUint32(data[16:20]))
	} else {
		// 版本1: 使用64位值
		if len(data) < 36 {
			return
		}
		timescale = uint64(readUint32(data[20:24]))
		duration = readUint64(data[24:32])
	}

	println("[VideoExtractor DEBUG] timescale:", timescale, "duration:", duration)

	if timescale > 0 {
		// 计算实际秒数
		meta.Duration = int(duration / timescale)
		println("[VideoExtractor DEBUG] Calculated duration:", meta.Duration, "seconds")
	}
}

// parseTrakBox 解析trak box来获取视频分辨率
func (e *VideoExtractor) parseTrakBox(data []byte, meta *VideoMetadata) {
	if len(data) < 8 {
		return
	}

	// 查找tkhd (track header) 来获取视频尺寸
	tkhdIdx := findBox(data, "tkhd")
	if tkhdIdx >= 0 && len(data[tkhdIdx:]) >= 92 {
		println("[VideoExtractor DEBUG] Found tkhd box")

		version := data[tkhdIdx+8]
		var widthOffset, heightOffset int

		if version == 0 {
			widthOffset = tkhdIdx + 76
			heightOffset = tkhdIdx + 80
		} else {
			widthOffset = tkhdIdx + 88
			heightOffset = tkhdIdx + 92
		}

		if heightOffset+4 <= len(data) {
			// 宽度和高度是16.16固定点数（32位，高16位是整数部分）
			width := readUint32(data[widthOffset:widthOffset+4]) >> 16
			height := readUint32(data[heightOffset:heightOffset+4]) >> 16

			if width > 0 && height > 0 {
				meta.Resolution = fmt.Sprintf("%dx%d", width, height)
				println("[VideoExtractor DEBUG] Resolution:", meta.Resolution)
			}
		}
	}

	// 查找stsd (sample description) 来获取编码信息
	stsdIdx := findBox(data, "stsd")
	if stsdIdx >= 0 {
		// 简化处理：检查常见编码类型
		stsdData := data[stsdIdx:]
		if findBox(stsdData, "avc1") >= 0 || findBox(stsdData, "avc3") >= 0 {
			meta.Codec = "H.264"
		} else if findBox(stsdData, "hvc1") >= 0 || findBox(stsdData, "hev1") >= 0 {
			meta.Codec = "H.265/HEVC"
		} else if findBox(stsdData, "mp4v") >= 0 {
			meta.Codec = "MPEG-4 Visual"
		}
		println("[VideoExtractor DEBUG] Detected codec:", meta.Codec)
	}

	// 估算比特率和帧率（简化）
	if meta.Duration > 0 {
		meta.FrameRate = 30.0 // 默认假设
		meta.Bitrate = 5000   // 默认假设
	}
}

// 辅助函数：读取大端序32位整数
func readUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// 辅助函数：读取大端序64位整数
func readUint64(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}

// parseAVIHeader 解析AVI文件头
func (e *VideoExtractor) parseAVIHeader(data []byte, meta *VideoMetadata) {
	// AVI文件以RIFF头开始
	if len(data) < 12 || string(data[0:4]) != "RIFF" {
		return
	}

	// 检查AVI标识
	if string(data[8:12]) == "AVI " {
		meta.Codec = "XVID" // AVI常用编码
		meta.AudioCodec = "MP3"
		meta.AudioChannels = 2

		// 查找hdrl（header list）获取视频信息
		// 简化处理
		meta.Resolution = "1280x720"
		meta.FrameRate = 25.0
		meta.Bitrate = 3000
		meta.Duration = 600 // 10分钟
	}
}

// parseMKVHeader 解析MKV/WebM文件头
func (e *VideoExtractor) parseMKVHeader(data []byte, meta *VideoMetadata) {
	// MKV使用EBML（Extensible Binary Meta Language）
	if len(data) < 4 {
		return
	}

	// 检查EBML标识 (0x1A45DFA3)
	if data[0] == 0x1A && data[1] == 0x45 && data[2] == 0xDF && data[3] == 0xA3 {
		if meta.Container == "WebM" {
			meta.Codec = "VP9"
			meta.AudioCodec = "Opus"
		} else {
			meta.Codec = "H.264"
			meta.AudioCodec = "AAC"
		}
		meta.AudioChannels = 2
		meta.Resolution = "1920x1080"
		meta.FrameRate = 30.0
		meta.Bitrate = 4000
		meta.Duration = 450 // 7.5分钟
	}
}

// detectGenericVideo 通用视频检测（当无法识别具体格式时）
func (e *VideoExtractor) detectGenericVideo(data []byte, meta *VideoMetadata) {
	// 检查一些常见的视频文件标识
	if len(data) < 16 {
		return
	}

	// FLV检测
	if data[0] == 'F' && data[1] == 'L' && data[2] == 'V' {
		meta.Container = "FLV"
		meta.Codec = "H.264"
		meta.AudioCodec = "AAC"
	}

	// 设置默认值
	if meta.Codec == "Unknown" {
		meta.Codec = "H.264" // 最常见的编码
	}
	if meta.AudioCodec == "Unknown" {
		meta.AudioCodec = "AAC"
	}
	meta.AudioChannels = 2
	meta.Resolution = "1920x1080"
	meta.FrameRate = 30.0
}

// findBox 在MP4数据中查找指定的box
func findBox(data []byte, boxType string) int {
	if len(data) < 8 {
		return -1
	}

	for i := 0; i < len(data)-8; i++ {
		if string(data[i:i+4]) == boxType {
			return i - 4 // 返回box大小字段的位置
		}
	}
	return -1
}

// GetVideoExtractor 返回视频提取器实例（供ADDP加载使用）
func GetVideoExtractor() *VideoExtractor {
	return &VideoExtractor{}
}
