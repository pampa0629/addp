package media

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type Plugin struct {
	formatType format.FormatType
	i18nKey    string
	extensions []string
	mimeTypes  []string
}

func NewPlugin(formatType format.FormatType, i18nKey string, extensions, mimeTypes []string) *Plugin {
	return &Plugin{
		formatType: formatType,
		i18nKey:    i18nKey,
		extensions: extensions,
		mimeTypes:  mimeTypes,
	}
}

func (p *Plugin) Format() format.FormatType {
	return p.formatType
}

func (p *Plugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:            "builtin-" + string(p.formatType),
		Format:        p.formatType,
		I18nKey:       p.i18nKey,
		DataType:      datatype.DataTypeMedia,
		Layouts:       []string{format.LayoutSingle},
		ProviderHints: []string{format.FormatProviderMedia},
		Identification: format.FormatIdentification{
			Extensions: p.extensions,
			MimeTypes:  p.mimeTypes,
		},
		ContentReaders: []string{
			string(format.ContentReaderRawContent),
			string(format.ContentReaderRangeContent),
		},
	}
}

func init() {
	plugins := []*Plugin{
		NewPlugin(format.FormatWebP, "format.webp", []string{".webp"}, []string{"image/webp"}),
		NewPlugin(format.FormatBMP, "format.bmp", []string{".bmp"}, []string{"image/bmp", "image/x-ms-bmp"}),
		NewPlugin(format.FormatSVG, "format.svg", []string{".svg", ".svgz"}, []string{"image/svg+xml"}),
		NewPlugin(format.FormatAVIF, "format.avif", []string{".avif"}, []string{"image/avif"}),
		NewPlugin(format.FormatHEIC, "format.heic", []string{".heic", ".heif"}, []string{"image/heic", "image/heif", "image/heic-sequence", "image/heif-sequence"}),
		NewPlugin(format.FormatVideo, "format.video", nil, []string{"video/*"}),
		NewPlugin(format.FormatMP4, "format.mp4", []string{".mp4", ".m4v"}, []string{"video/mp4", "application/mp4"}),
		NewPlugin(format.FormatMOV, "format.mov", []string{".mov", ".qt"}, []string{"video/quicktime"}),
		NewPlugin(format.FormatMKV, "format.mkv", []string{".mkv"}, []string{"video/x-matroska", "video/matroska"}),
		NewPlugin(format.FormatAVI, "format.avi", []string{".avi"}, []string{"video/x-msvideo", "video/avi"}),
		NewPlugin(format.FormatWebM, "format.webm", []string{".webm"}, []string{"video/webm"}),
		NewPlugin(format.FormatAudio, "format.audio", nil, []string{"audio/*"}),
		NewPlugin(format.FormatMP3, "format.mp3", []string{".mp3"}, []string{"audio/mpeg", "audio/mp3"}),
		NewPlugin(format.FormatWAV, "format.wav", []string{".wav"}, []string{"audio/wav", "audio/wave", "audio/x-wav"}),
		NewPlugin(format.FormatFLAC, "format.flac", []string{".flac"}, []string{"audio/flac"}),
		NewPlugin(format.FormatAAC, "format.aac", []string{".aac", ".m4a"}, []string{"audio/aac", "audio/mp4", "audio/x-m4a"}),
		NewPlugin(format.FormatOGG, "format.ogg", []string{".ogg", ".oga", ".opus"}, []string{"audio/ogg", "audio/opus"}),
	}
	for _, plugin := range plugins {
		if err := format.RegisterFormatPlugin(plugin); err != nil {
			panic(err)
		}
	}
}
