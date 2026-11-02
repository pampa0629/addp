package metaitem

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
)

const enviHeaderFixture = "ENVI\nsamples = 4\nlines = 3\nbands = 2\nheader offset = 16\nfile type = ENVI Standard\ndata type = 12\ninterleave = bil\nbyte order = 1\n"

type enviStructureFixture struct {
	refMapContentReader
	kind   string
	opened []string
}

func (r *enviStructureFixture) Type() string { return r.kind }
func (r *enviStructureFixture) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return nil, fmt.Errorf("basic ENVI cannot open an unbounded stream")
}
func (r *enviStructureFixture) OpenRange(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	name := strings.TrimPrefix(target.StringPath(), "bucket/")
	r.opened = append(r.opened, name)
	raw, found := r.content[name]
	if !found || opts.Offset < 0 || opts.Length < 0 || opts.Offset > int64(len(raw)) {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(raw[opts.Offset:min(int64(len(raw)), opts.Offset+opts.Length)])), nil
}

func TestENVIBasicAndDeepUseSameExactPair(t *testing.T) {
	for _, kind := range []string{"nfs", "minio"} {
		for _, deep := range []bool{false, true} {
			raw := []byte(enviHeaderFixture)
			reader := &enviStructureFixture{kind: kind, refMapContentReader: refMapContentReader{content: map[string][]byte{"imagery/ 原名 %.HDR": raw, "imagery/ 原名 %.img": bytes.Repeat([]byte{17}, 64), "imagery/unrelated.txt": []byte("never read")}}}
			input := DirectoryResolveInput{EngineID: 14, DirPath: "imagery", StructureReader: reader, Options: ResolveOptions{IncludeSingleResources: true}}
			if deep {
				input.ContentReader = reader
			}
			if kind == "minio" {
				input.EngineCatalogPathFor = plugin.ObjectItemPathForBucket(14, "bucket")
			}
			for name, data := range reader.content {
				input.Files = append(input.Files, StorageFileRef{Name: path.Base(name), Path: name, Size: int64(len(data))})
			}
			result, err := (&commonDataItemResolver{}).ResolveItems(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			var found *DetectedItem
			for _, item := range result.Items {
				if item.Format == "envi" {
					if found != nil {
						t.Fatal("duplicate ENVI pair")
					}
					found = item
				}
			}
			if found == nil || found.Layout != "multi" || found.PrimaryContentPath != "imagery/ 原名 %.img" || len(found.RefList) != 2 || !result.Claims["imagery/ 原名 %.HDR"] || !result.Claims["imagery/ 原名 %.img"] || result.Claims["imagery/unrelated.txt"] {
				t.Fatalf("incomplete ENVI grouping: %+v", result)
			}
			for _, ref := range found.RefList {
				if !ref.Required || (ref.Primary && (ref.Role != "raster" || ref.Path != "imagery/ 原名 %.img")) || (!ref.Primary && (ref.Role != "header" || ref.Path != "imagery/ 原名 %.HDR")) {
					t.Fatal("incorrect exact constituent", ref)
				}
			}
			for _, name := range reader.opened {
				if name == "imagery/unrelated.txt" {
					t.Fatal("ENVI opened an undeclared file")
				}
			}
		}
	}
}

func TestENVIUnprovenCompositionCannotDowngradeToSingleIMG(t *testing.T) {
	for _, fault := range []string{"missing", "two headers", "two rasters", "external", "wrong size", "strong TIFF", "strong HFA", "wrong engine", "no structure", "wrong mapping", "oversized header", "sidecar"} {
		t.Run(fault, func(t *testing.T) {
			header := []byte(enviHeaderFixture)
			if fault == "external" {
				header = append(header, []byte("dem file = ../secret.img\n")...)
			}
			reader := &enviStructureFixture{kind: "nfs", refMapContentReader: refMapContentReader{content: map[string][]byte{"imagery/field.hdr": header, "imagery/field.img": bytes.Repeat([]byte{17}, 64)}}}
			if fault == "missing" {
				delete(reader.content, "imagery/field.img")
			}
			if fault == "two headers" {
				reader.content["imagery/field.img.hdr"] = header
			}
			if fault == "two rasters" {
				reader.content["imagery/field.bsq"] = bytes.Repeat([]byte{17}, 64)
			}
			if fault == "oversized header" {
				reader.content["imagery/field.hdr"] = append(header, bytes.Repeat([]byte(" "), 1<<20)...)
			}
			if fault == "sidecar" {
				reader.content["imagery/field.img.aux.xml"] = []byte("unapproved")
			}
			if fault == "wrong size" {
				reader.content["imagery/field.img"] = []byte{1}
			}
			if fault == "strong TIFF" {
				copy(reader.content["imagery/field.img"], []byte("II*\x00"))
			}
			if fault == "strong HFA" {
				copy(reader.content["imagery/field.img"], []byte("EHFA_HEADER_TAG\x00"))
			}
			if fault == "wrong engine" {
				reader.kind = "ftp"
			}
			input := DirectoryResolveInput{EngineID: 14, DirPath: "imagery", StructureReader: reader}
			if fault == "no structure" {
				input.StructureReader = nil
				input.ContentReader = reader
			}
			if fault == "wrong mapping" {
				input.EngineCatalogPathFor = func(string) plugin.EngineCatalogPath { return plugin.FileItemPath(14, "other/field.hdr") }
			}
			for name, data := range reader.content {
				input.Files = append(input.Files, StorageFileRef{Name: path.Base(name), Path: name, Size: int64(len(data))})
			}

			items := resolveENVIPairs(t.Context(), input, input.Files)
			incomplete := fault == "two headers" || fault == "two rasters" || fault == "external" || fault == "wrong size" || fault == "oversized header" || fault == "sidecar"
			if !incomplete {
				if len(items) != 0 {
					t.Fatal("unidentified ENVI claimed", items)
				}
				return
			}
			expected := 1
			if fault == "two rasters" {
				expected = 2
			}
			if len(items) != expected {
				t.Fatal("known ENVI downgraded to ordinary single file", items)
			}
			for _, item := range items {
				facts := commonJSON.Section(item.Attributes, "format_info.envi")
				if item.Format != "envi" || item.Layout != "multi" || facts["composition_complete"] != false || len(facts) != 2 || len(item.RefList) != 1 || item.RefList[0].Role != "raster" || !item.RefList[0].Primary || len(item.ClaimPaths) != 1 || item.ClaimPaths[0] != item.PrimaryContentPath {
					t.Fatal("unproven ENVI admitted a complete pair", item)
				}
			}
		})
	}
}
