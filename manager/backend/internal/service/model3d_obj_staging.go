package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"

	"github.com/addp/common/format/plugins/obj"
)

// The caller opens only an authorized manifest. Parsing and upload subsequently
// consume the same buffered bytes, never a second read of the source.
func bufferOBJStagingManifest(ctx context.Context, input io.Reader, size int64, material bool) ([]byte, []string, error) {
	if input == nil || size < 0 || size > obj.MaxCompositionBytes || (!material && size == 0) {
		return nil, nil, errors.New("OBJ manifest size is outside its bound")
	}
	raw, err := io.ReadAll(io.LimitReader(&sourceContextReader{ctx: ctx, reader: input}, size+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(raw)) != size {
		return nil, nil, errors.New("OBJ manifest size changed during preparation")
	}
	parse := obj.NewPlugin().ReadModel3DComposition
	if material {
		parse = obj.ReadMTLTextureRefs
	}
	refs, err := parse(ctx, bytes.NewReader(raw), size)
	if err != nil {
		return nil, nil, err
	}
	return raw, refs, nil
}

func sameOBJDeclarations(actual, expected []string) bool {
	actual, expected = slices.Clone(actual), slices.Clone(expected)
	slices.Sort(actual)
	slices.Sort(expected)
	return slices.Equal(actual, expected)
}
