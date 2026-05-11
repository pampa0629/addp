package text

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestTextProviderExtractText(t *testing.T) {
	provider := NewProvider(format.FormatText)
	got, truncated, err := provider.ExtractText(context.Background(), strings.NewReader("hello world"), 5, nil)
	if err != nil {
		t.Fatalf("ExtractText() error = %v", err)
	}
	if got != "hello" {
		t.Fatalf("ExtractText() = %q, want hello", got)
	}
	if !truncated {
		t.Fatal("ExtractText() truncated = false, want true")
	}
}

func TestTextProviderDescribeDocument(t *testing.T) {
	provider := NewProvider(format.FormatMarkdown)
	info, err := provider.DescribeDocument(context.Background(), strings.NewReader("# Title\n\nbody"), nil)
	if err != nil {
		t.Fatalf("DescribeDocument() error = %v", err)
	}
	if info.Format != format.FormatMarkdown {
		t.Fatalf("Format = %q, want markdown", info.Format)
	}
	if info.Encoding != "utf-8" {
		t.Fatalf("Encoding = %q, want utf-8", info.Encoding)
	}
	if info.TextPreview == "" {
		t.Fatal("TextPreview should not be empty")
	}
}

func TestTextProviderRemovesBOM(t *testing.T) {
	provider := NewProvider(format.FormatText)
	got, _, err := provider.ExtractText(context.Background(), strings.NewReader("\ufeffhello"), 20, nil)
	if err != nil {
		t.Fatalf("ExtractText() error = %v", err)
	}
	if got != "hello" {
		t.Fatalf("ExtractText() = %q, want hello", got)
	}
}
