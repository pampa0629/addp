//go:build ftp_formats

package ftp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/contentadapter"
	"github.com/addp/common/engine/plugin"
	fileformat "github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/json"
	parquetformat "github.com/addp/common/format/plugins/parquet"
)

type rangeOnly struct {
	*FTPPlugin
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
	p := &FTPPlugin{}
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
