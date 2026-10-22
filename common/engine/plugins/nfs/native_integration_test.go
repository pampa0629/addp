package nfs

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

// The Hosted owner creates and destroys an exclusive export; this test never
// starts services, mounts the host filesystem or mutates a developer's data.
func TestIntegrationNFSOrdinaryFilesAndNoLinks(t *testing.T) {
	if os.Getenv("ADDP_NFS_INTEGRATION") != "1" {
		t.Skip("owned NFS integration fixture is not selected")
	}
	root := os.Getenv("ADDP_NFS_TEST_EXPORT")
	if root == "" {
		t.Fatal("missing owned export")
	}
	info := plugin.ConnectionInfo{"server": "127.0.0.1", "export_path": root}
	p := &NFSPlugin{}
	ctx := context.Background()
	target := plugin.FileItemPath(14, "models/ifc/model.ifc")
	facts, err := p.DescribeEngineCatalogFacts(ctx, info, target, plugin.EngineCatalogFactsOptions{})
	if err != nil || facts == nil || facts.Storage == nil || facts.Storage.SizeBytes == nil || *facts.Storage.SizeBytes <= 0 {
		t.Fatalf("file facts: %v", err)
	}
	stream, err := p.OpenContent(ctx, info, target, plugin.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) != *facts.Storage.SizeBytes || !strings.HasPrefix(string(data), "ISO-10303-21;") {
		t.Fatal("wrong ordinary source bytes")
	}
	ranged, err := p.OpenRange(ctx, info, target, plugin.ReadOptions{Offset: 4, Length: 8})
	if err != nil {
		t.Fatal(err)
	}
	part, err := io.ReadAll(ranged)
	if err != nil || string(part) != string(data[4:12]) {
		t.Fatalf("range=%q,%v", part, err)
	}
	ranged.Close()
	nodes, err := p.ListChildren(ctx, info, plugin.FileDirectoryPath(14, "unsupported"), plugin.ListOptions{})
	if err != nil || len(nodes) != 0 {
		t.Fatalf("links or special files entered catalog: %v,%v", nodes, err)
	}
	for _, path := range []string{"unsupported/inside-link", "unsupported/outside-link", "unsupported/fifo", "unsupported/directory-link/model.ifc"} {
		file := plugin.FileItemPath(14, path)
		if _, err := p.DescribeEngineCatalogFacts(ctx, info, file, plugin.EngineCatalogFactsOptions{}); err == nil {
			t.Fatalf("unsafe facts %q", path)
		}
		if reader, err := p.OpenContent(ctx, info, file, plugin.ReadOptions{}); err == nil {
			reader.Close()
			t.Fatalf("unsafe content %q", path)
		}
		if reader, err := p.OpenRange(ctx, info, file, plugin.ReadOptions{Offset: 0, Length: 1}); err == nil {
			reader.Close()
			t.Fatalf("unsafe range %q", path)
		}
	}
}
