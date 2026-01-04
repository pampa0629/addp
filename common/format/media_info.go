package format

import (
	"fmt"
	"time"
)

// ImageInfo 图片扩展信息
type ImageInfo struct {
	Width       int        // 宽度（像素）
	Height      int        // 高度（像素）
	Format      string     // 图片格式（jpeg, png, gif, webp, etc.）
	ColorSpace  string     // 色彩空间（RGB, CMYK, Grayscale, etc.）
	BitDepth    int        // 位深度
	HasAlpha    bool       // 是否有透明通道
	Orientation int        // 方向（EXIF Orientation）
	GPS         *GPSInfo   // GPS 信息（如果有）
	CaptureTime *time.Time // 拍摄时间
	CameraModel string     // 相机型号
}

func (i *ImageInfo) ExtensionType() string {
	return "image"
}

// AspectRatio 计算宽高比
func (i *ImageInfo) AspectRatio() float64 {
	if i.Height == 0 {
		return 0
	}
	return float64(i.Width) / float64(i.Height)
}

// GPSInfo GPS 信息
type GPSInfo struct {
	Latitude  float64 // 纬度
	Longitude float64 // 经度
	Altitude  float64 // 海拔（米）
}

// VideoInfo 视频扩展信息
type VideoInfo struct {
	Width      int     // 宽度（像素）
	Height     int     // 高度（像素）
	Duration   int64   // 时长（秒）
	Format     string  // 视频格式（mp4, avi, mkv, etc.）
	Codec      string  // 编码格式（h264, h265, vp9, etc.）
	Bitrate    int64   // 比特率（bps）
	FrameRate  float64 // 帧率（fps）
	AudioCodec string  // 音频编码
	HasAudio   bool    // 是否有音轨
}

func (v *VideoInfo) ExtensionType() string {
	return "video"
}

// DurationString 获取时长字符串表示（HH:MM:SS）
func (v *VideoInfo) DurationString() string {
	hours := v.Duration / 3600
	minutes := (v.Duration % 3600) / 60
	seconds := v.Duration % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// PDFInfo PDF 扩展信息
type PDFInfo struct {
	PageCount    int        // 页数
	Title        string     // 标题
	Author       string     // 作者
	Subject      string     // 主题
	Creator      string     // 创建软件
	Producer     string     // 生成软件
	CreationDate *time.Time // 创建时间
	ModDate      *time.Time // 修改时间
	Encrypted    bool       // 是否加密
}

func (p *PDFInfo) ExtensionType() string {
	return "pdf"
}
