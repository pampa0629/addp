package format

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestReadBinaryContent(t *testing.T) {
	t.Parallel()

	content, err := ReadBinaryContent(context.Background(), bytes.NewReader([]byte{0x00, 0x01, 0x02}), 16, nil)
	if err != nil {
		t.Fatalf("ReadBinaryContent() error = %v", err)
	}
	if content.Truncated {
		t.Fatalf("Truncated = true, want false")
	}
	if !bytes.Equal(content.Bytes, []byte{0x00, 0x01, 0x02}) {
		t.Fatalf("Bytes = %#v, want raw bytes", content.Bytes)
	}
}

func TestReadBinaryContentTruncates(t *testing.T) {
	t.Parallel()

	content, err := ReadBinaryContent(context.Background(), strings.NewReader("abcdef"), 3, nil)
	if err != nil {
		t.Fatalf("ReadBinaryContent() error = %v", err)
	}
	if !content.Truncated {
		t.Fatalf("Truncated = false, want true")
	}
	if got := string(content.Bytes); got != "abc" {
		t.Fatalf("Bytes = %q, want abc", got)
	}
}

func TestReadBinaryContentReadsEmpty(t *testing.T) {
	t.Parallel()

	content, err := ReadBinaryContent(context.Background(), bytes.NewReader(nil), 16, nil)
	if err != nil {
		t.Fatalf("ReadBinaryContent() error = %v", err)
	}
	if content.Truncated {
		t.Fatalf("Truncated = true, want false")
	}
	if len(content.Bytes) != 0 {
		t.Fatalf("Bytes length = %d, want 0", len(content.Bytes))
	}
}

func TestReadBinaryContentUsesOptions(t *testing.T) {
	t.Parallel()

	content, err := ReadBinaryContent(context.Background(), strings.NewReader("abc"), 16, &ParseOptions{
		ExtraParams: map[string]interface{}{
			"size_bytes": int64(42),
			"mime_type":  "application/octet-stream",
		},
	})
	if err != nil {
		t.Fatalf("ReadBinaryContent() error = %v", err)
	}
	if content.SizeBytes == nil || *content.SizeBytes != 42 {
		t.Fatalf("SizeBytes = %#v, want 42", content.SizeBytes)
	}
	if content.MIMEType != "application/octet-stream" {
		t.Fatalf("MIMEType = %q, want application/octet-stream", content.MIMEType)
	}
}
