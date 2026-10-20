//go:build ftp_formats

package ftp_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/contentadapter"
	"github.com/addp/common/engine/plugin"
	ftppkg "github.com/addp/common/engine/plugins/ftp"
	fileformat "github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/json"
	parquetformat "github.com/addp/common/format/plugins/parquet"
	_ "github.com/addp/common/format/plugins/threeds"
)

type rangeOnly struct {
	*ftppkg.FTPPlugin
	calls atomic.Int64
}

func (r *rangeOnly) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("Parquet must use native ranges")
}
func (r *rangeOnly) OpenRange(ctx context.Context, info plugin.ConnectionInfo, p plugin.EngineCatalogPath, o plugin.ReadOptions) (io.ReadCloser, error) {
	r.calls.Add(1)
	return r.FTPPlugin.OpenRange(ctx, info, p, o)
}

func TestIntegrationProviderSharedFormats(t *testing.T) {
	info := ownedInfo(t, "explicit_tls")
	p := &ftppkg.FTPPlugin{}
	scope := plugin.FileDirectoryCatalogEntry(plugin.FileRootPath(99), "examples", "examples").Path
	entries, err := p.ListChildren(t.Context(), info, scope, plugin.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ name, kind string }{{"客户 中文%.csv", "csv"}, {"订单 示例.json", "json"}, {"客户 示例.parquet", "parquet"}} {
		t.Run(row.kind, func(t *testing.T) {
			var target plugin.EngineCatalogPath
			for _, entry := range entries {
				if entry.Name == row.name {
					target = entry.Path
				}
			}
			stream, err := p.OpenContent(t.Context(), info, target, plugin.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(stream)
			stream.Close()
			if err != nil {
				t.Fatal(err)
			}
			reader, err := fileformat.GetTableSampleReader(fileformat.FormatType(row.kind))
			if err != nil {
				t.Fatal(err)
			}
			rows, err := reader.SampleTable(t.Context(), bytes.NewReader(body), 0, 10, nil)
			if err != nil || len(rows) != 3 {
				t.Fatal("shared preview rows differ", err)
			}
			for i, result := range rows {
				if fmt.Sprint(result["id"]) != fmt.Sprint(i+1) {
					t.Fatal("shared parser lost record identity")
				}
			}
			describer, err := fileformat.GetTableInfoProvider(fileformat.FormatType(row.kind))
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := describer.DescribeTable(t.Context(), bytes.NewReader(body), nil)
			if err != nil || metadata.Table == nil || len(metadata.Table.Fields) != 3 {
				t.Fatal("shared scan facts differ", err)
			}
			if row.kind == "parquet" {
				audit := &rangeOnly{FTPPlugin: p}
				content := contentadapter.NewMappedReader(audit, info, contentadapter.ScopePathMapper(scope), plugin.ReadOptions{})
				rows, err = parquetformat.NewPlugin().SampleTableScope(t.Context(), content, contentio.NewRef(scope.StringPath(), contentio.RoleScope), 0, 10, nil)
				if err != nil || len(rows) != 3 || audit.calls.Load() == 0 {
					t.Fatal("Parquet preview did not consume native FTP ranges", err)
				}
			}
		})
	}
}

// These are raw Provider/format protocol checks on an owned synthetic FTP
// fixture. They do not exercise Manager authorization, conversion or rendering.
func TestIntegrationProviderModelInputs(t *testing.T) {
	info := ownedInfo(t, "explicit_tls")
	provider := &ftppkg.FTPPlugin{}
	for _, sample := range []struct{ target, source string }{
		{"models/ifc/model.ifc", "addp_online_model_fixture.ifc"},
		{"models/3ds/model.3ds", "addp_online_model_fixture.3ds"},
		{"models/3ds/texture.png", "addp_online_exchange_texture.png"},
	} {
		t.Run(sample.target, func(t *testing.T) {
			want, err := os.ReadFile("../../../../business/fixtures/manager/" + sample.source)
			if err != nil {
				t.Fatal(err)
			}
			target := plugin.FileRootPath(99)
			parts := strings.Split(sample.target, "/")
			for i, part := range parts[:len(parts)-1] {
				target = plugin.FileDirectoryCatalogEntry(target, part, strings.Join(parts[:i+1], "/")).Path
			}
			target = plugin.FileLeafCatalogEntry(target, plugin.StorageObjectFacts{Name: path.Base(sample.target), Path: sample.target}).Path
			facts, err := provider.DescribeEngineCatalogFacts(t.Context(), info, target, plugin.EngineCatalogFactsOptions{})
			if err != nil || facts == nil || facts.Storage == nil || facts.Storage.SizeBytes == nil ||
				facts.Storage.Path != sample.target || *facts.Storage.SizeBytes != int64(len(want)) || facts.Storage.ETag != "" {
				t.Fatal("exact model file MLST facts differ", err)
			}
			stream, err := provider.OpenContent(t.Context(), info, target, plugin.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			var actual bytes.Buffer
			if strings.HasSuffix(sample.target, ".3ds") {
				parser, parseErr := fileformat.GetModel3DInfoProvider(fileformat.Format3DS)
				if parseErr != nil {
					stream.Close()
					t.Fatal(parseErr)
				}
				described, parseErr := parser.DescribeModel3D(t.Context(), io.TeeReader(stream, &actual), nil)
				closeErr := stream.Close()
				if parseErr != nil || closeErr != nil || described == nil || described.FormatInfo["scan_complete"] != true {
					t.Fatal("native 3DS stream did not produce complete declarations", parseErr, closeErr)
				}
				refs, ok := described.FormatInfo["texture_refs"].([]string)
				if !ok || len(refs) != 1 || refs[0] != "texture.png" {
					t.Fatal("3DS native texture declaration changed")
				}
			} else {
				_, readErr := io.Copy(&actual, stream)
				if closeErr := stream.Close(); readErr != nil || closeErr != nil {
					t.Fatal(readErr, closeErr)
				}
			}
			if !bytes.Equal(actual.Bytes(), want) {
				t.Fatal("FTP model input differs from its fixed repository source")
			}
		})
	}
}
