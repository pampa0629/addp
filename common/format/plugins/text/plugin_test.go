package text

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestTextPluginReadDocumentText(t *testing.T) {
	plugin := NewPlugin(format.FormatText)
	got, truncated, err := plugin.ReadDocumentText(context.Background(), strings.NewReader("hello world"), 5, nil)
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

func TestTextPluginDescribeDocument(t *testing.T) {
	plugin := NewPlugin(format.FormatMarkdown)
	info, err := plugin.DescribeDocument(context.Background(), strings.NewReader("# Title\n\nbody"), nil)
	if err != nil {
		t.Fatalf("DescribeDocument() error = %v", err)
	}
	if info.Encoding != "utf-8" {
		t.Fatalf("Encoding = %q, want utf-8", info.Encoding)
	}
	if info.Title != "" {
		t.Fatalf("Title = %q, want empty", info.Title)
	}
}

func TestTextPluginRemovesBOM(t *testing.T) {
	plugin := NewPlugin(format.FormatText)
	got, _, err := plugin.ReadDocumentText(context.Background(), strings.NewReader("\ufeffhello"), 20, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText() error = %v", err)
	}
	if got != "hello" {
		t.Fatalf("ReadDocumentText() = %q, want hello", got)
	}
}
