package embedding

import (
	"context"
	"time"
)

type Modality string

const (
	ModalityText     Modality = "text"
	ModalityImage    Modality = "image"
	ModalityAudio    Modality = "audio"
	ModalityVideo    Modality = "video"
	ModalityDocument Modality = "document"
)

type Embedding struct {
	ID        string
	Vector    []float32
	Model     string
	Dimension int
	Metadata  map[string]string
}

type Usage struct {
	PromptTokens int
	TotalTokens  int
	Latency      time.Duration
}

type BatchResult struct {
	Embeddings []Embedding
	Usage      *Usage
}

type TextInput struct {
	ID       string
	Text     string
	Language string
	Metadata map[string]string
}

type DocumentInput struct {
	ID       string
	Content  string
	Title    string
	Language string
	Metadata map[string]string
}

type ImageInput struct {
	ID       string
	Data     []byte
	MIMEType string
	Metadata map[string]string
}

type AudioInput struct {
	ID         string
	Data       []byte
	SampleRate int
	Channels   int
	Metadata   map[string]string
}

type VideoInput struct {
	ID        string
	Data      []byte
	FrameRate float32
	MIMEType  string
	Metadata  map[string]string
}

type TextEmbedder interface {
	EmbedText(ctx context.Context, inputs []TextInput) (*BatchResult, error)
}

type DocumentEmbedder interface {
	EmbedDocument(ctx context.Context, inputs []DocumentInput) (*BatchResult, error)
}

type ImageEmbedder interface {
	EmbedImage(ctx context.Context, inputs []ImageInput) (*BatchResult, error)
}

type AudioEmbedder interface {
	EmbedAudio(ctx context.Context, inputs []AudioInput) (*BatchResult, error)
}

type VideoEmbedder interface {
	EmbedVideo(ctx context.Context, inputs []VideoInput) (*BatchResult, error)
}

type MultiModalEmbedder interface {
	TextEmbedder
	DocumentEmbedder
	ImageEmbedder
	AudioEmbedder
	VideoEmbedder
}
