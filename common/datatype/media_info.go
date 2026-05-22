package datatype

// MediaKind identifies the media family of a media data item.
type MediaKind string

const (
	MediaKindImage MediaKind = "image"
	MediaKindVideo MediaKind = "video"
	MediaKindAudio MediaKind = "audio"
)

// MediaInfo is the common type info for media data items.
type MediaInfo struct {
	Kind       MediaKind `json:"kind,omitempty"`
	MIMEType   string    `json:"mime_type,omitempty"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	DurationMS *int64    `json:"duration_ms,omitempty"`
	Encoding   string    `json:"encoding,omitempty"`
	ColorSpace string    `json:"color_space,omitempty"`
	SizeBytes  *int64    `json:"size_bytes,omitempty"`
}
