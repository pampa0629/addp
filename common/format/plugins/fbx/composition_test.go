package fbx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"testing"
)

type corpusCase struct {
	Name   string `json:"name"`
	Base64 string `json:"base64"`
	Valid  bool   `json:"valid"`
	Meshes int64  `json:"mesh_count"`
}

func cases(t *testing.T) []corpusCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/composition-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Profile string       `json:"profile"`
		Cases   []corpusCase `json:"cases"`
	}
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Profile != Profile || len(corpus.Cases) < 15 {
		t.Fatal("incomplete shared corpus")
	}
	return corpus.Cases
}

func TestSharedStaticProfileCorpus(t *testing.T) {
	for _, test := range cases(t) {
		t.Run(test.Name, func(t *testing.T) {
			raw, err := base64.StdEncoding.DecodeString(test.Base64)
			if err != nil {
				t.Fatal(err)
			}
			refs, err := NewPlugin().ReadModel3DComposition(context.Background(), bytes.NewReader(raw), int64(len(raw)))
			if !test.Valid {
				if err == nil || refs != nil {
					t.Fatal("unsupported FBX returned a composition", refs, err)
				}
				return
			}
			if err != nil || refs == nil || len(refs) != 0 {
				t.Fatal(refs, err)
			}
			facts, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader(raw), nil)
			if err != nil {
				t.Fatal(err)
			}
			if facts.Model3D.MeshCount == nil || *facts.Model3D.MeshCount != test.Meshes || facts.FormatInfo["profile"] != Profile || facts.FormatInfo["encoding"] != "binary" || facts.FormatInfo["version"] != 7400 {
				t.Fatal("invented or different facts", facts)
			}
		})
	}
}

type observedReader struct {
	raw    []byte
	calls  int
	cancel context.CancelFunc
	short  bool
}

func (r *observedReader) ReadAt(p []byte, offset int64) (int, error) {
	r.calls++
	if r.cancel != nil {
		r.cancel()
	}
	if offset != 0 {
		panic("reopened FBX primary")
	}
	n := copy(p, r.raw)
	if r.short {
		return n - 1, io.ErrUnexpectedEOF
	}
	return n, nil
}

func TestCompositionUsesOneBoundedReadAndNeverReturnsPartialFacts(t *testing.T) {
	var raw []byte
	for _, c := range cases(t) {
		if c.Valid {
			raw, _ = base64.StdEncoding.DecodeString(c.Base64)
			break
		}
	}
	spy := &observedReader{raw: raw}
	refs, err := NewPlugin().ReadModel3DComposition(context.Background(), spy, int64(len(raw)))
	if err != nil || refs == nil || spy.calls != 1 {
		t.Fatal(refs, err, spy.calls)
	}
	for _, size := range []int64{-1, 0, MaxCompositionBytes + 1} {
		spy.calls = 0
		if refs, err := NewPlugin().ReadModel3DComposition(context.Background(), spy, size); err == nil || refs != nil || spy.calls != 0 {
			t.Fatal("budget performed IO", size, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	spy = &observedReader{raw: raw, cancel: cancel}
	if refs, err := NewPlugin().ReadModel3DComposition(ctx, spy, int64(len(raw))); err != context.Canceled || refs != nil || spy.calls != 1 {
		t.Fatal(refs, err, spy.calls)
	}
	spy = &observedReader{raw: raw, short: true}
	if refs, err := NewPlugin().ReadModel3DComposition(context.Background(), spy, int64(len(raw))); err == nil || refs != nil || spy.calls != 1 {
		t.Fatal(refs, err, spy.calls)
	}
	spy.calls = 0
	if refs, err := NewPlugin().ReadModel3DComposition(ctx, spy, int64(len(raw))); err != context.Canceled || refs != nil || spy.calls != 0 {
		t.Fatal("cancelled read was opened", err, spy.calls)
	}
}
