package text

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestTextProviderReadDocumentText(t *testing.T) {
	provider := NewProvider(format.FormatText)
	got, truncated, err := provider.ReadDocumentText(context.Background(), strings.NewReader("hello world"), 5, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText() error = %v", err)
	}
	if got != "hello" {
		t.Fatalf("ReadDocumentText() = %q, want hello", got)
	}
	if !truncated {
		t.Fatal("ReadDocumentText() truncated = false, want true")
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
	if info.Title != "" {
		t.Fatalf("Title = %q, want empty", info.Title)
	}
}

func TestTextProviderRemovesBOM(t *testing.T) {
	provider := NewProvider(format.FormatText)
	got, _, err := provider.ReadDocumentText(context.Background(), strings.NewReader("\ufeffhello"), 20, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText() error = %v", err)
	}
	if got != "hello" {
		t.Fatalf("ReadDocumentText() = %q, want hello", got)
	}
}
