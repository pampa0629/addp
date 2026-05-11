package format

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func providerTestDescribe(context.Context, io.Reader, *ParseOptions) (*TableInfo, error) {
	rowCount := int64(1)
	return &TableInfo{RowCount: &rowCount}, nil
}

func providerTestSample(context.Context, io.Reader, int64, int64, *ParseOptions) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"id": 1}}, nil
}

func TestRegisterTableProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewTableProvider(FormatType("provider_test"), providerTestDescribe, providerTestSample)

	if err := registry.RegisterTableProvider(provider); err != nil {
		t.Fatalf("RegisterTableProvider() error = %v", err)
	}

	got, err := registry.GetTableProvider(FormatType("provider_test"))
	if err != nil {
		t.Fatalf("GetTableProvider() error = %v", err)
	}
	if got.Format() != FormatType("provider_test") {
		t.Fatalf("provider format = %q, want provider_test", got.Format())
	}
}

func TestListTableProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	if err := registry.RegisterTableProvider(NewTableProvider(FormatType("zeta"), providerTestDescribe, providerTestSample)); err != nil {
		t.Fatalf("RegisterTableProvider(zeta) error = %v", err)
	}
	if err := registry.RegisterTableProvider(NewTableProvider(FormatType("alpha"), providerTestDescribe, providerTestSample)); err != nil {
		t.Fatalf("RegisterTableProvider(alpha) error = %v", err)
	}

	got := registry.ListTableProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTableProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestNewTableProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewTableProvider(FormatType("empty"), nil, nil)

	if _, err := provider.DescribeTable(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeTable() error = nil, want error")
	}
	if _, err := provider.SampleTable(context.Background(), strings.NewReader(""), 0, 1, nil); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("SampleTable() error = %v, want implementation error", err)
	}
}

func TestRegisterDocumentProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewDocumentProvider(
		FormatType("doc_test"),
		func(context.Context, io.Reader, *ParseOptions) (*DocumentInfo, error) {
			return &DocumentInfo{Format: FormatType("doc_test"), TextPreview: "hello"}, nil
		},
		func(context.Context, io.Reader, int64, *ParseOptions) (string, bool, error) {
			return "hello", false, nil
		},
	)

	if err := registry.RegisterDocumentProvider(provider); err != nil {
		t.Fatalf("RegisterDocumentProvider() error = %v", err)
	}

	got, err := registry.GetDocumentProvider(FormatType("doc_test"))
	if err != nil {
		t.Fatalf("GetDocumentProvider() error = %v", err)
	}
	if got.Format() != FormatType("doc_test") {
		t.Fatalf("provider format = %q, want doc_test", got.Format())
	}
}

func TestListDocumentProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewDocumentProvider(FormatType("zeta"), nil, nil)
	if err := registry.RegisterDocumentProvider(provider); err != nil {
		t.Fatalf("RegisterDocumentProvider(zeta) error = %v", err)
	}
	provider = NewDocumentProvider(FormatType("alpha"), nil, nil)
	if err := registry.RegisterDocumentProvider(provider); err != nil {
		t.Fatalf("RegisterDocumentProvider(alpha) error = %v", err)
	}

	got := registry.ListDocumentProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListDocumentProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestNewDocumentProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewDocumentProvider(FormatType("empty_doc"), nil, nil)

	if _, err := provider.DescribeDocument(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeDocument() error = nil, want error")
	}
	if _, _, err := provider.ExtractText(context.Background(), strings.NewReader(""), 1, nil); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("ExtractText() error = %v, want implementation error", err)
	}
}

func TestRegisterMediaProvider(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewMediaProvider(
		FormatType("media_test"),
		func(context.Context, io.Reader, *ParseOptions) (*MediaInfo, error) {
			return &MediaInfo{Format: FormatType("media_test"), MediaType: "image"}, nil
		},
	)

	if err := registry.RegisterMediaProvider(provider); err != nil {
		t.Fatalf("RegisterMediaProvider() error = %v", err)
	}

	got, err := registry.GetMediaProvider(FormatType("media_test"))
	if err != nil {
		t.Fatalf("GetMediaProvider() error = %v", err)
	}
	if got.Format() != FormatType("media_test") {
		t.Fatalf("provider format = %q, want media_test", got.Format())
	}
}

func TestListMediaProviderFormatsSorted(t *testing.T) {
	registry := NewProviderRegistry()
	provider := NewMediaProvider(FormatType("zeta"), nil)
	if err := registry.RegisterMediaProvider(provider); err != nil {
		t.Fatalf("RegisterMediaProvider(zeta) error = %v", err)
	}
	provider = NewMediaProvider(FormatType("alpha"), nil)
	if err := registry.RegisterMediaProvider(provider); err != nil {
		t.Fatalf("RegisterMediaProvider(alpha) error = %v", err)
	}

	got := registry.ListMediaProviderFormats()
	want := []FormatType{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListMediaProviderFormats() = %#v, want %#v", got, want)
	}
}

func TestNewMediaProviderHandlesMissingImplementation(t *testing.T) {
	provider := NewMediaProvider(FormatType("empty_media"), nil)

	if _, err := provider.DescribeMedia(context.Background(), strings.NewReader(""), nil); err == nil {
		t.Fatalf("DescribeMedia() error = nil, want error")
	}
}
