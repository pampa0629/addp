package format

import (
	"context"
	"io"
	"reflect"
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
