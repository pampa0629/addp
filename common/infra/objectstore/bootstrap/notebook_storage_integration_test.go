package bootstrap

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/addp/common/infra/objectstore"
)

func TestNotebookObjectsAcrossLanguagesAgainstS3(t *testing.T) {
	if os.Getenv("ADDP_NOTEBOOK_OBJECT_INTEGRATION") != "1" {
		t.Skip("run the Develop MinIO or RustFS Notebook gate")
	}
	ctx := context.Background()
	key := "tenant_7/notebooks/跨语言 % key.ipynb"
	probe := func(phase string) {
		cmd := exec.Command(os.Getenv("NOTEBOOK_STORAGE_PYTHON"), os.Getenv("NOTEBOOK_STORAGE_PROBE"), phase)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Python %s: %v %s", phase, err, output)
		}
	}
	t.Cleanup(func() { probe("cleanup") })
	probe("prepare")
	open := func() objectstore.Area {
		area, err := OpenObjectArea(ctx, objectstore.Develop)
		if err != nil {
			t.Fatal(err)
		}
		return area
	}
	read := func(area objectstore.Area, want []byte) {
		r, err := area.Open(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("object bytes differ: %q %v", data, err)
		}
	}
	t.Setenv("ADDP_INFRA_OBJECTSTORE_BINDINGS", "")
	normal := open()
	read(normal, []byte(`{"cells":[],"metadata":{"origin":"default"},"nbformat":4,"nbformat_minor":5}`))
	t.Setenv("ADDP_INFRA_OBJECTSTORE_BINDINGS", "develop=notebook-exception")
	exception := open()
	read(exception, []byte(`{"cells":[],"metadata":{"origin":"exception"},"nbformat":4,"nbformat_minor":5}`))
	data := append([]byte(`{"cells":[],"metadata":{"origin":"Go 中"},"nbformat":4,"nbformat_minor":5}`), '\n')
	if err := exception.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/x-ipynb+json"); err != nil {
		t.Fatal(err)
	}
	probe("exchange")
	read(exception, append([]byte(`{"cells":[], "metadata":{"origin":"Python 文"},"nbformat":4,"nbformat_minor":5}`), '\n'))
	read(normal, []byte(`{"cells":[],"metadata":{"origin":"default"},"nbformat":4,"nbformat_minor":5}`))
	probe("save")
	read(exception, []byte(`{"cells":[],"metadata":{"edited":true},"nbformat":4,"nbformat_minor":5}`))
	read(normal, []byte(`{"cells":[],"metadata":{"origin":"default"},"nbformat":4,"nbformat_minor":5}`))
	if os.Getenv("NOTEBOOK_READONLY_USER") != "" {
		probe("save-failure")
	}
}
