package gltf

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"
)

type shortManifest struct{}

func (shortManifest) ReadAt(p []byte, offset int64) (int, error) { return 0, io.EOF }

func TestCompositionProviderConsumesBoundPrimaryAndReturnsNoPartialFacts(t *testing.T) {
	plugin := NewPlugin()
	valid := []byte(`{"asset":{"version":"2.0"},"buffers":[{"uri":"folder/%20name%2520.mesh","byteLength":4}]}`)
	refs, err := plugin.ReadModel3DComposition(context.Background(), bytes.NewReader(valid), int64(len(valid)))
	if err != nil || !reflect.DeepEqual(refs, []string{"folder/ name%20.mesh"}) {
		t.Fatal(refs, err)
	}
	empty := []byte(`{"asset":{"version":"2.0"}}`)
	refs, err = plugin.ReadModel3DComposition(context.Background(), bytes.NewReader(empty), int64(len(empty)))
	if err != nil || refs == nil || len(refs) != 0 {
		t.Fatal("empty external set was not proven", refs, err)
	}
	if refs, err := plugin.ReadModel3DComposition(context.Background(), shortManifest{}, int64(len(valid))); err == nil || refs != nil {
		t.Fatal("truncated manifest returned facts")
	}
	invalid := append(append([]byte{}, valid...), []byte(` {}`)...)
	if refs, err := plugin.ReadModel3DComposition(context.Background(), bytes.NewReader(invalid), int64(len(invalid))); err == nil || refs != nil {
		t.Fatal("trailing or partial manifest returned facts")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if refs, err := plugin.ReadModel3DComposition(ctx, nil, 0); err == nil || refs != nil {
		t.Fatal("cancelled composition returned facts")
	}
}
