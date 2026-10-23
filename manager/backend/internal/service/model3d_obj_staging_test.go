package service

import (
	"context"
	"strings"
	"testing"

	"github.com/addp/common/engine/workflowaccess"
	commonModels "github.com/addp/common/models"
)

func TestOBJStagingManifestPreservesObservedBytesAndKnownEmptyMaterial(t *testing.T) {
	for _, material := range []bool{false, true} {
		input := "mtllib paint red.mtl\nv 0 0 0\n"
		if material {
			input = "newmtl paint\nmap_Kd textures/red.png\n"
		}
		raw, refs, err := bufferOBJStagingManifest(t.Context(), strings.NewReader(input), int64(len(input)), material)
		if err != nil || string(raw) != input || len(refs) != 1 {
			t.Fatalf("%q %#v %v", raw, refs, err)
		}
		if raw, refs, err := bufferOBJStagingManifest(t.Context(), strings.NewReader(input), int64(len(input)+1), material); err == nil || raw != nil || refs != nil {
			t.Fatal("partial manifest accepted")
		}
	}
	raw, refs, err := bufferOBJStagingManifest(t.Context(), strings.NewReader(""), 0, true)
	if err != nil || len(raw) != 0 || refs == nil || len(refs) != 0 {
		t.Fatalf("empty MTL: %q %#v %v", raw, refs, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := bufferOBJStagingManifest(ctx, strings.NewReader("v 0 0 0\n"), 8, false); err == nil {
		t.Fatal("canceled manifest read")
	}
}

func TestOBJChangedMaterialStopsBeforeAnyTextureReadOrTemporaryUpload(t *testing.T) {
	for _, engineType := range []string{"ftp", "nfs", "minio"} {
		t.Run(engineType, func(t *testing.T) {
			ctx, request, engine, spy, _ := fileModelAuthorizedFixture(t, engineType, "obj", nil)
			spy.files["models/model.mtl"] = []byte("newmtl paint\nmap_Kd OTHER.PNG\n")
			store := &stagingModelStore{}
			called := false
			err := stagingModelExecutor(store).withFileModel3DSource(ctx, request, engine, func(context.Context, workflowaccess.Source, commonModels.JSONMap) error { called = true; return nil })
			if err == nil || called || store.puts != 0 {
				t.Fatalf("unapproved closure progressed: %v %v %d", err, called, store.puts)
			}
			for _, name := range append(spy.facts, spy.opens...) {
				if strings.HasSuffix(name, ".PNG") {
					t.Fatal("changed MTL caused texture access")
				}
			}
		})
	}
}
