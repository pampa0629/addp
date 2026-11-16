package rustfs

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/objectstore"
)

func TestRustFSContract(t *testing.T) {
	p := &RustFSPlugin{}
	if p.Type() != "rustfs" || p.DisplayName() != "RustFS" || p.EngineOrigin() != "general" {
		t.Fatal("RustFS identity must be independent")
	}
	caps := p.Capabilities()
	if caps.EngineType != "rustfs" || caps.EngineFamily != "object" || caps.InstanceOverview || caps.Compute != nil {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
	if err := p.ValidateConnectionInfo(plugin.ConnectionInfo{"endpoint": "localhost:9004", "access_key": "ak"}); err == nil {
		t.Fatal("missing secret accepted")
	}
	cfg, err := objectstore.ParseClientConfig(plugin.ConnectionInfo{"endpoint": "localhost:9004", "access_key": "ak", "secret_key": "sk"}, false, false)
	if err != nil || cfg.UseSSL {
		t.Fatal(cfg, err)
	}
	if _, err := p.OpenRange(context.Background(), nil, plugin.ObjectItemPath(1, "data", "a.csv"), plugin.ReadOptions{}); err == nil {
		t.Fatal("unbounded range accepted")
	}
	if _, err := p.CreateContent(context.Background(), nil, plugin.ObjectRootPath(1), plugin.WriteOptions{}); err == nil {
		t.Fatal("write to structural root accepted")
	}
}

// The owned gate invokes the same native contracts before and after recreation.
func TestIntegrationRustFSProvider(t *testing.T) {
	if os.Getenv("ADDP_RUSTFS_INTEGRATION") != "1" {
		t.Skip("owned RustFS T2 required")
	}
	ctx := context.Background()
	p := &RustFSPlugin{}
	conn := plugin.ConnectionInfo{"endpoint": os.Getenv("ADDP_RUSTFS_TEST_ENDPOINT"), "access_key": os.Getenv("ADDP_RUSTFS_TEST_ACCESS_KEY"), "secret_key": os.Getenv("ADDP_RUSTFS_TEST_SECRET_KEY"), "use_ssl": false}
	if err := p.TestConnection(ctx, conn); err != nil {
		t.Fatal(err)
	}
	bad := plugin.ConnectionInfo{"endpoint": conn["endpoint"], "access_key": conn["access_key"], "secret_key": "incorrect-secret-for-negative-test", "use_ssl": false}
	if err := p.TestConnection(ctx, bad); err == nil {
		t.Fatal("wrong credentials accepted")
	}
	bucket := os.Getenv("ADDP_RUSTFS_TEST_BUCKET")
	target := plugin.ObjectItemPath(91, bucket, "样例/订单.csv")
	payload := "id,city,amount\n1,长沙,10\n2,株洲,20\n"
	if os.Getenv("ADDP_RUSTFS_TEST_PHASE") == "initial" {
		writer, err := p.CreateContent(ctx, conn, target, plugin.WriteOptions{ContentType: "text/csv"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := p.OpenContent(ctx, conn, target, plugin.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil || string(data) != payload {
		t.Fatalf("object bytes differ: read=%v close=%v", err, closeErr)
	}
	reader, err = p.OpenRange(ctx, conn, target, plugin.ReadOptions{Offset: 3, Length: 7})
	if err != nil {
		t.Fatal(err)
	}
	data, err = io.ReadAll(reader)
	closeErr = reader.Close()
	if err != nil || closeErr != nil || string(data) != payload[3:10] {
		t.Fatal("native range differs", err, closeErr)
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, conn, target, plugin.EngineCatalogFactsOptions{})
	if err != nil || facts.Storage == nil || facts.Storage.SizeBytes == nil || *facts.Storage.SizeBytes != int64(len(payload)) {
		t.Fatal(facts, err)
	}
	buckets, err := p.ListChildren(ctx, conn, plugin.ObjectRootPath(91), plugin.ListOptions{})
	found := false
	for _, entry := range buckets {
		if entry.Name == bucket {
			found = true
		}
	}
	if err != nil || !found {
		t.Fatal("native bucket missing", err)
	}
	entries, err := p.ListChildren(ctx, conn, plugin.ObjectDirectoryPath(91, bucket, ""), plugin.ListOptions{})
	if err != nil || len(entries) != 1 || entries[0].Name != "样例" {
		t.Fatal(entries, err)
	}
	entries, err = p.ListChildren(ctx, conn, plugin.ObjectDirectoryPath(91, bucket, "样例"), plugin.ListOptions{})
	if err != nil || len(entries) != 1 || entries[0].Name != "订单.csv" {
		t.Fatal(entries, err)
	}
	if _, err := p.ResolvePath(ctx, conn, target); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateContent(ctx, conn, target, plugin.WriteOptions{}); err == nil {
		t.Fatal("existing object overwritten without permission")
	}
	// Verify write/delete against a separate leaf so the scan fixture persists.
	removed := plugin.ObjectItemPath(91, bucket, "remove-me.txt")
	writer, err := p.CreateContent(ctx, conn, removed, plugin.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(writer, strings.NewReader("temporary")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteResource(ctx, conn, removed); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ResolvePath(ctx, conn, removed); err == nil {
		t.Fatal("deleted object remains")
	}
}
