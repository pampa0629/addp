package s3

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/infra/objectstore"
	sdk "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestPlatformAreaAgainstRustFS(t *testing.T) {
	if os.Getenv("ADDP_RUSTFS_INTEGRATION") != "1" {
		t.Skip("run make test-common-rustfs")
	}
	cfg := Config{
		Endpoint: os.Getenv("ADDP_RUSTFS_TEST_ENDPOINT"), AccessKey: os.Getenv("ADDP_RUSTFS_TEST_ACCESS_KEY"),
		SecretKey: os.Getenv("ADDP_RUSTFS_TEST_SECRET_KEY"),
	}
	client, err := sdk.New(cfg.Endpoint, &sdk.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, "")})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	key := "tenant_1/document.bin"
	buckets := [2]string{"platform-standard", "platform-standard-alt"}
	contents := [2][]byte{{0, 1, 2, 3, 255}, {0, 3, 2, 1, 255}}
	var areas [2]objectstore.ContentArea
	for i, bucket := range buckets {
		if err := client.MakeBucket(ctx, bucket, sdk.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
		area, err := Bind(ctx, objectstore.Standard, bucket, cfg)
		if err != nil {
			t.Fatal(err)
		}
		areas[i] = area
		data := contents[i]
		if err := area.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
	}
	for i, area := range areas {
		expected := contents[i]
		stat, err := area.Stat(ctx, key)
		if err != nil || stat.Size != int64(len(expected)) || stat.ContentType != "application/octet-stream" {
			t.Fatalf("unexpected Stat: %v, %v", stat, err)
		}
		stream, err := area.Open(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		assertBytes(t, stream, expected)
		stream, err = area.OpenRange(ctx, key, 1, 3)
		if err != nil {
			t.Fatal(err)
		}
		assertBytes(t, stream, expected[1:4])
		items, err := area.List(ctx, "tenant_1/")
		if err != nil || len(items) != 1 || items[0] != stat.Ref {
			t.Fatalf("unexpected listing: %v, %v", items, err)
		}
		if err := area.Delete(ctx, key); err != nil {
			t.Fatal(err)
		}
		if _, err := area.Stat(ctx, key); !errors.Is(err, contentio.ErrContentNotFound) {
			t.Fatalf("unexpected missing object error: %v", err)
		}
		if err := client.RemoveBucket(ctx, buckets[i]); err != nil {
			t.Fatal(err)
		}
	}
}

func assertBytes(t *testing.T, stream io.ReadCloser, expected []byte) {
	t.Helper()
	data, err := io.ReadAll(stream)
	closeErr := stream.Close()
	if err != nil || closeErr != nil || !bytes.Equal(data, expected) {
		t.Fatalf("object bytes=%v want=%v, read=%v close=%v", data, expected, err, closeErr)
	}
}
