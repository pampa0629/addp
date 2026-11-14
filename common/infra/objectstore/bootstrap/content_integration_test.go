package bootstrap

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/infra/objectstore"
	objectcontent "github.com/addp/common/infra/objectstore/contentadapter"
	objectminio "github.com/addp/common/infra/objectstore/minio"
	sdk "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestManagerObjectAreaAgainstMinIO(t *testing.T) {
	if os.Getenv("ADDP_MANAGER_OBJECT_INTEGRATION") != "1" {
		t.Skip("requires make test-manager-postgres owned MinIO")
	}
	ctx := t.Context()
	client, err := sdk.New(os.Getenv("MANAGER_OBJECT_TEST_ENDPOINT"), &sdk.Options{Creds: credentials.NewStaticV4(os.Getenv("MANAGER_OBJECT_TEST_USER"), os.Getenv("MANAGER_OBJECT_TEST_PASSWORD"), "")})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manager", "manager-contract-other"} {
		if err := client.MakeBucket(ctx, name, sdk.MakeBucketOptions{}); err != nil && sdk.ToErrorResponse(err).Code != "BucketAlreadyOwnedByYou" {
			t.Fatal(err)
		}
	}
	manager, err := OpenContentArea(ctx, objectstore.Manager)
	if err != nil {
		t.Fatal(err)
	}
	other := objectminio.New(client, "manager-contract-other")
	key := "tenant_7/import/fixture/汉字 +%.bin"
	for _, item := range []struct {
		area objectstore.ContentArea
		data string
	}{{manager, "manager bytes"}, {other, "different bytes"}} {
		if err := item.area.Put(ctx, key, bytes.NewBufferString(item.data), int64(len(item.data)), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
		stream, err := item.area.Open(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil || string(got) != item.data {
			t.Fatalf("read %q %v", got, err)
		}
	}
	provider := &objectcontent.Provider{Name: objectstore.Manager, Store: manager}
	target := plugin.ObjectItemPath(0, "manager", "tenant_7/import/fixture/new.csv")
	writer, err := provider.CreateContent(ctx, nil, target, plugin.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "a,b\n1,2\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	stat, err := manager.Stat(ctx, "tenant_7/import/fixture/new.csv")
	if err != nil || stat.Size != 8 || !strings.HasPrefix(stat.ContentType, "text/csv") {
		t.Fatalf("stat %+v %v", stat, err)
	}
	stream, err := provider.OpenRange(ctx, nil, target, plugin.ReadOptions{Offset: 4, Length: 3})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(stream)
	_ = stream.Close()
	if err != nil || string(got) != "1,2" {
		t.Fatalf("range %q %v", got, err)
	}
	if _, err := provider.CreateContent(ctx, nil, target, plugin.WriteOptions{}); err == nil {
		t.Fatal("overwrote existing object without permission")
	}
	refs, err := manager.List(ctx, "tenant_7/import/fixture/")
	if err != nil || len(refs) != 2 {
		t.Fatalf("list %+v %v", refs, err)
	}
	t.Setenv("ADDP_INFRA_OBJECTSTORE_BINDINGS", "manager=manager-contract-other")
	if _, err := OpenContentArea(ctx, objectstore.Manager); err == nil {
		t.Fatal("manager rebinding must remain unavailable")
	}
}
