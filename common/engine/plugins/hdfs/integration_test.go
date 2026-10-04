package hdfs_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/hdfs"
)

func TestIntegrationHDFS(t *testing.T) {
	if os.Getenv("ADDP_HDFS_INTEGRATION") != "1" {
		t.Skip("owned HDFS gate required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	info := plugin.ConnectionInfo{"webhdfs_endpoint": "http://hdfs-namenode:9870", "rpc_uri": "hdfs://hdfs-namenode:8020", "root_path": "/addp", "authentication": "simple", "user": "addp_business_reader"}
	p := &hdfs.HDFSPlugin{}
	if err := p.TestConnection(ctx, info); err != nil {
		t.Fatal(err)
	}
	entries, err := p.ListChildren(ctx, info, plugin.FileRootPath(7), plugin.ListOptions{Recursive: true})
	if err != nil || len(entries) != 5 {
		t.Fatalf("catalog=%#v err=%v", entries, err)
	}
	rootFile := plugin.FileItemPath(7, "订单 100%.csv")
	reader, err := p.OpenContent(ctx, info, rootFile, plugin.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rootBody, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || strings.Count(string(rootBody), "\n") != 21 {
		t.Fatal("root leaf/original filename read failed", err)
	}
	for _, format := range []string{"csv", "json", "parquet"} {
		target := plugin.FileItemPath(7, "samples/orders."+format)
		f, err := p.DescribeEngineCatalogFacts(ctx, info, target, plugin.EngineCatalogFactsOptions{})
		if err != nil || *f.Storage.SizeBytes <= 0 {
			t.Fatalf("facts=%#v err=%v", f, err)
		}
		r, err := p.OpenContent(ctx, info, target, plugin.ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if format == "csv" && strings.Count(string(body), "\n") != 21 {
			t.Fatal("CSV sample row count")
		}
		if format == "json" && strings.Count(string(body), "\n") != 20 {
			t.Fatal("JSON sample row count")
		}
		if format == "parquet" && string(body[:4]) != "PAR1" {
			t.Fatal("Parquet magic")
		}
		// Export provider bytes to the native host format consumers.
		sampleDir := os.Getenv("ADDP_HDFS_SAMPLE_DIR")
		if sampleDir == "" {
			t.Fatal("owned HDFS sample directory required")
		}
		if err := os.WriteFile(filepath.Join(sampleDir, "orders."+format), body, 0644); err != nil {
			t.Fatal(err)
		}
		r, err = p.OpenRange(ctx, info, target, plugin.ReadOptions{Offset: 3, Length: 7})
		if err != nil {
			t.Fatal(err)
		}
		rangeBody, err := io.ReadAll(r)
		r.Close()
		if err != nil || string(rangeBody) != string(body[3:10]) {
			t.Fatalf("real range: %q %v", rangeBody, err)
		}
		r, err = p.OpenRange(ctx, info, target, plugin.ReadOptions{Offset: *f.Storage.SizeBytes, Length: 7})
		if err != nil {
			t.Fatal(err)
		}
		eof, _ := io.ReadAll(r)
		r.Close()
		if len(eof) != 0 {
			t.Fatal("EOF range not empty")
		}
	}
	if _, err := p.ResolvePath(ctx, info, plugin.FileDirectoryPath(7, "missing")); !plugin.IsEngineCatalogErrorKind(err, plugin.EngineCatalogErrorNotFound) {
		t.Fatalf("missing directory: %v", err)
	}
	if _, err := p.ResolvePath(ctx, info, plugin.FileItemPath(7, "../outside")); err == nil {
		t.Fatal("boundary traversal accepted")
	}
}
