package scanruntime

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

type enviFileRuntimeFixture struct {
	filesystemScanTestProvider
	files map[string][]byte
}

func (p *enviFileRuntimeFixture) Type() string { return "nfs" }
func (p *enviFileRuntimeFixture) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	panic("basic ENVI used sequential content")
}
func (p *enviFileRuntimeFixture) OpenRange(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, options plugin.ReadOptions) (io.ReadCloser, error) {
	raw, ok := p.files[target.StringPath()]
	if !ok || options.Offset < 0 || options.Length <= 0 || options.Offset > int64(len(raw)) {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(raw[options.Offset:min(int64(len(raw)), options.Offset+options.Length)])), nil
}

func TestENVIBasicRuntimePersistsCompletePairAndInvalidatesOldProof(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	const main, header = "imagery/grid.img", "imagery/grid.HDR"
	const text = "ENVI\nsamples = 4\nlines = 1\nbands = 1\nheader offset = 0\nfile type = ENVI Standard\ndata type = 1\ninterleave = bsq\nbyte order = 0\n"
	provider := &enviFileRuntimeFixture{filesystemScanTestProvider: filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{}}, files: map[string][]byte{main: {1, 2, 3, 4}, header: []byte(text)}}
	previous, err := plugin.Get("nfs")
	plugin.Register(provider)
	t.Cleanup(func() {
		plugin.Unregister("nfs")
		if err == nil {
			plugin.Register(previous)
		}
	})
	runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), metaRepo.NewScanRepository(db), nil)
	engine := &commonModels.Engine{ID: 26, EngineType: "nfs"}
	var id uint
	for _, fault := range []string{"complete", "external header", "restored"} {
		raw := []byte(text)
		if fault == "external header" {
			raw = append(raw, []byte("dem file = ../secret.img\n")...)
		}
		provider.files[header] = raw
		entries := []plugin.EngineCatalogEntry{progressFile(main), progressFile(header)}
		*entries[0].Storage.SizeBytes = 4
		*entries[1].Storage.SizeBytes = int64(len(raw))
		provider.entriesByPath["imagery"] = entries
		if _, err := runtime.ScanPaths(t.Context(), engine, 1, []string{"imagery"}, "basic", true, nil); err != nil {
			t.Fatal(err)
		}
		var item models.MetaItem
		if err := db.Where("tenant_id=? AND engine_id=? AND full_name=?", 1, 26, main).First(&item).Error; err != nil {
			t.Fatal(err)
		}
		if id == 0 {
			id = item.ID
		}
		if item.ID != id {
			t.Fatal("source identity changed")
		}
		facts := commonJSON.Section(item.Attributes, "format_info.envi")
		if fault == "external header" {
			if commonJSON.String(item.Attributes, "item", "format") != "envi" || commonJSON.String(item.Attributes, "item", "layout") != "multi" || facts["composition_complete"] != false || facts["header_sha256"] != nil {
				t.Fatal("old approved composition survived unsupported header", item.Attributes)
			}
			continue
		}
		if commonJSON.String(item.Attributes, "item", "format") != "envi" || commonJSON.String(item.Attributes, "item", "layout") != "multi" || facts["composition_complete"] != true || (item.SizeBytes == nil || *item.SizeBytes != int64(4+len(raw))) {
			t.Fatal("persisted pair proof/complete size lost", item.Attributes, item.SizeBytes)
		}
		refs := commonJSON.InterfaceSlice(commonJSON.Section(item.Attributes, "item")["refs"])
		if len(refs) != 2 {
			t.Fatal("persisted pair lost exact refs", refs)
		}
		var headerCount int64
		if err := db.Model(&models.MetaItem{}).Where("tenant_id=? AND engine_id=? AND full_name=?", 1, 26, header).Count(&headerCount).Error; err != nil || headerCount != 0 {
			t.Fatal("claimed header remained an independent item", headerCount, err)
		}
	}
}
