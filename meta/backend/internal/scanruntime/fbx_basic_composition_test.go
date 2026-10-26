package scanruntime

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/fbx"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"gorm.io/gorm"
)

func TestFBXBasicCompositionRuntime(t *testing.T) {
	for _, kind := range []string{"ftp", "nfs"} {
		t.Run(kind, func(t *testing.T) { testFBXBasicComposition(t, openObjectCatalogScanTestDB(t), kind) })
	}
}

// Exercise the scan runtime and persisted replacement, rather than supplying
// a complete flag directly to the authorization projection.
func testFBXBasicComposition(t *testing.T, db *gorm.DB, kind string) {
	t.Helper()
	const primary = "models/strict.fbx"
	raw, err := os.ReadFile("../../../../business/fixtures/manager/addp_online_model_fixture.fbx")
	if err != nil {
		t.Fatal(err)
	}
	proof, err := format.GetModel3DCompositionProvider(format.FormatFBX)
	if err != nil {
		t.Fatal(err)
	}
	if refs, err := proof.ReadModel3DComposition(t.Context(), bytes.NewReader(raw), int64(len(raw))); err != nil || refs == nil || len(refs) != 0 {
		t.Fatalf("fixed FBX fixture does not prove the profile before source I/O: %v", err)
	}
	p := &ftpRangeStructureFixture{&ftpStructureFixture{engineType: kind, primary: primary,
		filesystemScanTestProvider: filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{}}}}
	previous, previousErr := plugin.Get(kind)
	plugin.Register(p)
	t.Cleanup(func() {
		plugin.Unregister(kind)
		if previousErr == nil {
			plugin.Register(previous)
		}
	})
	runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), metaRepo.NewScanRepository(db), nil)
	engine := &commonModels.Engine{ID: 26, EngineType: kind}
	var originalID uint
	var fingerprint string
	for _, fault := range []string{"none", "unsupported version", "oversize", "restored"} {
		p.data = append([]byte{}, raw...)
		if fault == "unsupported version" {
			binary.LittleEndian.PutUint32(p.data[23:27], 7500)
		}
		file := progressFile(primary)
		*file.Storage.SizeBytes = int64(len(p.data))
		if fault == "oversize" {
			*file.Storage.SizeBytes = fbx.MaxCompositionBytes + 1
		}
		p.entriesByPath["models"] = []plugin.EngineCatalogEntry{file, progressFile("models/unrelated.png")}
		beforeRanges := p.ranges
		if _, err := runtime.ScanPaths(t.Context(), engine, 1, []string{"models"}, "basic", true, nil); err != nil {
			t.Fatal(err)
		}
		var item models.MetaItem
		if err := db.Where("tenant_id=? AND engine_id=? AND full_name=?", 1, 26, primary).First(&item).Error; err != nil {
			t.Fatal(err)
		}
		if originalID == 0 {
			originalID, fingerprint = item.ID, item.Fingerprint
		}
		complete := fault == "none" || fault == "restored"
		native := commonJSON.Section(item.Attributes, "format_info.fbx")
		profile := interface{}(nil)
		if complete {
			profile = fbx.Profile
		}
		refs := commonJSON.InterfaceSlice(commonJSON.Section(item.Attributes, "item")["refs"])
		if item.ID != originalID || item.Fingerprint != fingerprint || item.ScannedDepth != "basic" ||
			native["composition_complete"] != complete || native["profile"] != profile || native["texture_refs"] != nil ||
			len(refs) != 1 || commonJSON.String(item.Attributes, "storage", "physical_path") != primary || p.sequential != 0 {
			t.Fatalf("%s scan retained stale or expanded composition: %+v", fault, item.Attributes)
		}
		if len(commonJSON.Section(item.Attributes, "type_info.model_3d")) != 0 {
			t.Fatal("basic invented geometry statistics")
		}
		if fault == "oversize" && p.ranges != beforeRanges {
			t.Fatal("oversize primary was opened")
		}
	}
}
