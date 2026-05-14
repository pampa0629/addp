package format

import "testing"

func TestInferPreviewHintUsesTextFallbackForTextBytes(t *testing.T) {
	hint := InferPreviewHint(PreviewHintInput{
		Name: "README",
		Peek: []byte("hello\nworld\n"),
	})
	if hint.Format != FormatText {
		t.Fatalf("Format = %q, want %q", hint.Format, FormatText)
	}
	if hint.DataType != FormatDataTypeDocument {
		t.Fatalf("DataType = %q, want %q", hint.DataType, FormatDataTypeDocument)
	}
	if hint.Material != PreviewMaterialText || hint.Renderer != "text" || !hint.Previewable {
		t.Fatalf("hint = %#v, want text preview hint", hint)
	}
}

func TestInferPreviewHintKeepsUnknownBinaryAsRawFile(t *testing.T) {
	hint := InferPreviewHint(PreviewHintInput{
		Name: "payload.bin",
		Peek: []byte{0x00, 0x01, 0x02, 0x03},
	})
	if hint.Format != FormatUnknown {
		t.Fatalf("Format = %q, want %q", hint.Format, FormatUnknown)
	}
	if hint.DataType != FormatDataTypeFile {
		t.Fatalf("DataType = %q, want %q", hint.DataType, FormatDataTypeFile)
	}
	if hint.Material != PreviewMaterialRawBinary || hint.Previewable {
		t.Fatalf("hint = %#v, want non-previewable raw binary hint", hint)
	}
}

func TestInferPreviewHintUsesImageRendererForMedia(t *testing.T) {
	hint := InferPreviewHint(PreviewHintInput{
		Name: "photo.png",
	})
	if hint.Format != FormatPNG {
		t.Fatalf("Format = %q, want %q", hint.Format, FormatPNG)
	}
	if hint.DataType != FormatDataTypeMedia {
		t.Fatalf("DataType = %q, want %q", hint.DataType, FormatDataTypeMedia)
	}
	if hint.Material != PreviewMaterialRawBinary || hint.Renderer != "image" || !hint.Previewable {
		t.Fatalf("hint = %#v, want image preview hint", hint)
	}
}
