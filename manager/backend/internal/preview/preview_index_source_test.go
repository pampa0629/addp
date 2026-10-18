package preview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

type indexedSourcePlugin struct {
	recordingContentPlugin
	mtime             time.Time
	etag              string
	factsError        error
	factsCalls, reads int
	changeDuringRead  bool
}

func (p *indexedSourcePlugin) DescribeEngineCatalogFacts(_ context.Context, _ plugin.ConnectionInfo, path plugin.EngineCatalogPath, _ plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	p.factsCalls++
	if p.factsError != nil {
		return nil, p.factsError
	}
	size, modified := int64(len(p.content)), p.mtime
	return &plugin.EngineCatalogFacts{Path: path, UpdatedAt: &modified, Storage: &plugin.EngineCatalogStorageFacts{SizeBytes: &size, ETag: p.etag}}, nil
}

func (p *indexedSourcePlugin) OpenContent(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	p.reads++
	if p.changeDuringRead {
		p.mtime = p.mtime.Add(time.Second)
	}
	return io.NopCloser(bytes.NewReader(p.content)), nil
}

func (p *indexedSourcePlugin) OpenRange(_ context.Context, _ plugin.ConnectionInfo, _ plugin.EngineCatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
	p.reads++
	if p.changeDuringRead {
		p.mtime = p.mtime.Add(time.Second)
	}
	end := min(int64(len(p.content)), options.Offset+options.Length)
	if options.Offset < 0 || options.Offset > end {
		return nil, fmt.Errorf("invalid range")
	}
	return io.NopCloser(bytes.NewReader(p.content[options.Offset:end])), nil
}

func TestIndexedDelimitedPreviewRejectsChangedSource(t *testing.T) {
	for _, kind := range []string{"file", "object"} {
		for _, formatName := range []string{"csv", "tsv"} {
			for _, mutation := range []string{"size", "mtime", "etag", "unbound", "during_read", "facts_unavailable"} {
				t.Run(kind+"/"+formatName+"/"+mutation, func(t *testing.T) {
					delimiter := ","
					if formatName == "tsv" {
						delimiter = "\t"
					}
					data := strings.ReplaceAll("id,name\n1,Alice\n2,Bobby\n", ",", delimiter)
					stamp := time.Date(2026, 10, 18, 0, 0, 0, 123456789, time.UTC)
					storage := &indexedSourcePlugin{recordingContentPlugin: recordingContentPlugin{engineType: "indexed-delimited-source-test", content: []byte(data)}, mtime: stamp, etag: "v1"}
					plugin.Register(storage)
					t.Cleanup(func() { plugin.Unregister(storage.Type()) })
					source := map[string]interface{}{"size_bytes": len(data), "last_modified_at": stamp.Format(time.RFC3339Nano), "etag": "v1"}
					switch mutation {
					case "size":
						storage.content = append(storage.content, []byte("3,Carol\n")...)
					case "mtime":
						storage.mtime = stamp.Add(time.Second)
					case "etag":
						storage.etag = "v2"
					case "unbound":
						source = map[string]interface{}{"size_bytes": len(data)}
					case "during_read":
						storage.changeDuringRead = true
					case "facts_unavailable":
						storage.factsError = fmt.Errorf("private backend detail")
					}
					req := indexedDelimitedRequest(storage.Type(), kind, formatName, source, int64(len(data)))
					result, err := (&FileTablePreviewProvider{}).Preview(t.Context(), req)
					if err == nil || result != nil {
						t.Fatalf("changed/unverified source published rows or metadata: result=%#v err=%v", result, err)
					}
					want := ErrPreviewSourceRefreshRequired
					if mutation == "facts_unavailable" {
						want = ErrPreviewSourceUnavailable
					}
					if !errors.Is(err, want) {
						t.Fatalf("wrong failure classification: %v, want %v", err, want)
					}
					if mutation != "during_read" && storage.reads != 0 {
						t.Fatalf("read stale source %d times before rejection", storage.reads)
					}
				})
			}
		}
	}
}

func TestIndexedDelimitedPreviewResumesWithRebuiltSourceBinding(t *testing.T) {
	for _, kind := range []string{"file", "object"} {
		for _, name := range []string{"csv", "tsv"} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				data := "id,name\n1,Alice\n2,Bobby\n"
				if name == "tsv" {
					data = strings.ReplaceAll(data, ",", "\t")
				}
				stamp := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
				storage := &indexedSourcePlugin{recordingContentPlugin: recordingContentPlugin{engineType: "indexed-rebuilt-source-test", content: []byte(data)}, mtime: stamp, etag: "v2"}
				plugin.Register(storage)
				t.Cleanup(func() { plugin.Unregister(storage.Type()) })
				source := map[string]interface{}{"size_bytes": len(data), "last_modified_at": stamp.Format(time.RFC3339Nano), "etag": "v1"}
				req := indexedDelimitedRequest(storage.Type(), kind, name, source, int64(len(data)))
				if result, err := (&FileTablePreviewProvider{}).Preview(t.Context(), req); result != nil || !errors.Is(err, ErrPreviewSourceRefreshRequired) {
					t.Fatalf("old binding accepted: %#v %v", result, err)
				}
				source["etag"] = "v2"
				for page, label := range []string{"Alice", "Bobby"} {
					req.Page = page + 1
					result, err := (&FileTablePreviewProvider{}).Preview(t.Context(), req)
					if err != nil {
						t.Fatal(err)
					}
					if len(result.Rows) != 1 || result.Rows[0]["name"] != label || result.Total == nil || *result.Total != 2 || result.HasMore != (page == 0) {
						t.Fatalf("rebuilt page %d: %#v", page+1, result)
					}
				}
				if storage.reads != 2 || storage.factsCalls != 5 {
					t.Fatalf("unexpected I/O: reads=%d facts=%d", storage.reads, storage.factsCalls)
				}
			})
		}
	}
}

func indexedDelimitedRequest(engineType, kind, formatName string, source map[string]interface{}, size int64) *PreviewRequest {
	path := "samples/data." + formatName
	providerPath := plugin.FileItemPath(7, path)
	if kind == "object" {
		providerPath = plugin.ObjectItemPath(7, "samples", "data."+formatName)
	}
	return &PreviewRequest{Engine: &models.Engine{ID: 7, EngineType: engineType}, ItemType: kind, Schema: "samples", Table: "data." + formatName, PhysicalPath: path, ProviderPath: providerPath, Page: 1, PageSize: 1, Attributes: map[string]interface{}{
		"item":         map[string]interface{}{"format": formatName, "data_type": "table", "layout": "single"},
		"storage":      map[string]interface{}{"total_size": size},
		"type_info":    map[string]interface{}{"table": map[string]interface{}{"row_count": 2, "fields": []interface{}{map[string]interface{}{"name": "id", "type": "int"}, map[string]interface{}{"name": "name", "type": "string"}}}},
		"access_index": map[string]interface{}{"table": map[string]interface{}{"kind": "sparse_row_index", "data_type": "table", "format": formatName, "unit": "row", "offset_unit": "byte", "header_bytes": 8, "source": source, "anchors": []interface{}{map[string]interface{}{"row": 0, "byte_offset": 8}}}},
	}}
}
