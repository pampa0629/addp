package service

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/dataitem"
	"github.com/addp/meta/internal/metaitem"
)

func TestEnrichObjectStorageJSONTableUpdatesItemDataType(t *testing.T) {
	t.Parallel()

	svc := &ObjectStorageScanService{log: slog.Default()}
	meta := format.ObjectMetadata{
		Bucket:    "addp",
		Path:      "datasets/converted.json",
		SizeBytes: 64,
		FileType:  string(format.FormatJSON),
	}
	item := metaitemForJSONDocument(meta)

	attrs, err := svc.enrichObjectStorageTableFileAttributes(
		context.Background(),
		staticObjectContentReader{content: `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`},
		nil,
		7,
		meta,
		item,
		false,
	)
	if err != nil {
		t.Fatalf("enrichObjectStorageTableFileAttributes() error = %v", err)
	}
	itemAttrs := attrs["item"].(map[string]interface{})
	if itemAttrs["data_type"] != string(dataitem.DataTypeTable) || itemAttrs["format"] != string(format.FormatJSON) {
		t.Fatalf("item attrs = %#v, want json table", itemAttrs)
	}
	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	if table["fields"] == nil {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
}

func metaitemForJSONDocument(meta format.ObjectMetadata) *metaitem.DetectedItem {
	return metaitem.InferObjectStorageDataItem(meta, "converted.json")
}

type staticObjectContentReader struct {
	content string
}

func (r staticObjectContentReader) Type() string         { return "static" }
func (r staticObjectContentReader) DisplayName() string  { return "static" }
func (r staticObjectContentReader) EngineOrigin() string { return "general" }
func (r staticObjectContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r staticObjectContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r staticObjectContentReader) DefaultPort() int                                   { return 0 }
func (r staticObjectContentReader) RequiredFields() []string                           { return nil }
func (r staticObjectContentReader) SensitiveFields() []string                          { return nil }
func (r staticObjectContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r staticObjectContentReader) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemantics{}
}
func (r staticObjectContentReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(r.content)), nil
}
