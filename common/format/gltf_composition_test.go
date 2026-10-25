package format_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/addp/common/format"
)

func TestGLTFCompositionSharedCorpus(t *testing.T) {
	raw, err := os.ReadFile("plugins/gltf/testdata/composition-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string   `json:"name"`
		Raw       string   `json:"raw"`
		Valid     bool     `json:"valid"`
		Paths     []string `json:"paths"`
		Resources []struct {
			Role       string `json:"role"`
			Index      int    `json:"index"`
			URI        string `json:"uri"`
			Path       string `json:"path"`
			ByteLength int64  `json:"byteLength"`
			MimeType   string `json:"mimeType"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			got, err := format.ParseGLTFComposition(context.Background(), []byte(test.Raw))
			if !test.Valid {
				if err == nil || got != nil {
					t.Fatal("invalid composition returned success or partial facts")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Resources == nil || got.Paths() == nil || !reflect.DeepEqual(got.Paths(), test.Paths) || len(got.Resources) != len(test.Resources) {
				t.Fatalf("resource set changed: %#v paths=%v", got.Resources, got.Paths())
			}
			for i, want := range test.Resources {
				r := got.Resources[i]
				if r.Role != want.Role || r.Index != want.Index || r.URI != want.URI || r.Path != want.Path || r.ByteLength != want.ByteLength || r.MimeType != want.MimeType {
					t.Fatalf("resource mapping changed: %#v want %#v", r, want)
				}
			}
		})
	}
}

func TestGLTFCompositionBudgetsAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := format.ParseGLTFComposition(ctx, []byte(`{"asset":{"version":"2.0"}}`)); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled parse returned facts", err)
	}
	if got, err := format.ParseGLTFComposition(context.Background(), make([]byte, format.MaxGLTFManifestBytes+1)); got != nil || err == nil {
		t.Fatal("oversized manifest accepted")
	}
	refs := []map[string]any{}
	for i := 0; i <= format.MaxGLTFExternalFiles; i++ {
		refs = append(refs, map[string]any{"uri": fmt.Sprintf("resource-%03d.mesh", i), "byteLength": 4})
	}
	raw, _ := json.Marshal(map[string]any{"asset": map[string]string{"version": "2.0"}, "buffers": refs})
	if got, err := format.ParseGLTFComposition(context.Background(), raw); got != nil || err == nil {
		t.Fatal("resource budget or URI boundary accepted")
	}
}
