package bootstrap

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/infra/objectstore"
)

func TestInvalidObjectAreaConfiguration(t *testing.T) {
	for _, tc := range []struct{ provider, binding, area string }{
		{"other", "", "standard"}, {"minio", "manager=wrong", "standard"}, {"minio", "standard=", "standard"},
		{"minio", "standard=one,manager=two", "standard"}, {"minio", "manager=other", "manager"},
	} {
		t.Setenv("ADDP_INFRA_OBJECTSTORE_PROVIDER", tc.provider)
		t.Setenv("ADDP_INFRA_OBJECTSTORE_BINDINGS", tc.binding)
		if _, err := OpenObjectArea(context.Background(), tc.area); err == nil {
			t.Fatalf("accepted invalid configuration %+v", tc)
		}
	}
}

func TestStandardAreaAgainstMinIO(t *testing.T) {
	if os.Getenv("ADDP_STANDARD_MINIO_INTEGRATION") != "1" {
		t.Skip("enable through make test-standard-postgres")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const key = "binding/same-key.bin"
	var bindings []objectstore.Area
	contents := []string{"default\x00bytes", "remapped\xffbytes"}
	for i, mapping := range []string{"", "standard=project-a-standard-docs"} {
		t.Setenv("ADDP_INFRA_OBJECTSTORE_BINDINGS", mapping)
		area, err := OpenObjectArea(ctx, objectstore.Standard)
		if err != nil {
			t.Fatal(err)
		}
		if err := area.Put(ctx, key, strings.NewReader(contents[i]), int64(len(contents[i])), "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, area)
	}
	for i, area := range bindings {
		r, err := area.Open(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		closeErr := r.Close()
		if err != nil || closeErr != nil || string(b) != contents[i] {
			t.Fatalf("binding %d: read=%q err=%v close=%v", i, b, err, closeErr)
		}
		if err := area.Delete(ctx, key); err != nil {
			t.Fatal(err)
		}
	}
}
