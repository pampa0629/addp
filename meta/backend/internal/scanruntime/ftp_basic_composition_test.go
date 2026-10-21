package scanruntime

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"os"
	"reflect"
	"testing"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type ftpStructureFixture struct {
	filesystemScanTestProvider
	engineType                       string
	data                             []byte
	sequential, ranges, bytes, lists int
	rangeError                       error
	allowSequential                  bool
}

func (p *ftpStructureFixture) Type() string {
	if p.engineType != "" {
		return p.engineType
	}
	return "ftp"
}
func (p *ftpStructureFixture) OpenContent(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	p.sequential++
	if p.allowSequential && reflect.DeepEqual(target, plugin.FileItemPath(26, "models/model.3ds")) {
		return io.NopCloser(bytes.NewReader(p.data)), nil
	}
	if p.allowSequential && reflect.DeepEqual(target, plugin.FileItemPath(26, "models/A.PNG")) {
		if data, ok := p.contentByPath["models/A.PNG"]; ok {
			return io.NopCloser(bytes.NewBufferString(data)), nil
		}
	}
	return nil, fmt.Errorf("basic content stream must remain unavailable")
}
func (p *ftpStructureFixture) ListChildren(ctx context.Context, c plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	p.lists++
	return p.filesystemScanTestProvider.ListChildren(ctx, c, target, opts)
}

type ftpRangeStructureFixture struct{ *ftpStructureFixture }

func (p *ftpRangeStructureFixture) OpenRange(ctx context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !reflect.DeepEqual(target, plugin.FileItemPath(26, "models/model.3ds")) || opts.Offset < 0 || opts.Length <= 0 || opts.Offset+opts.Length > int64(len(p.data)) {
		return nil, fmt.Errorf("structure range escaped the primary or its size")
	}
	p.ranges++
	p.bytes += int(opts.Length)
	if p.rangeError != nil {
		return nil, p.rangeError
	}
	return io.NopCloser(bytes.NewReader(p.data[opts.Offset : opts.Offset+opts.Length])), nil
}

func structure3DS(ref string) []byte {
	chunk := func(id uint16, body []byte) []byte {
		out := make([]byte, 6)
		binary.LittleEndian.PutUint16(out, id)
		binary.LittleEndian.PutUint32(out[2:], uint32(len(body)+6))
		return append(out, body...)
	}
	return chunk(0x4d4d, chunk(0x3d3d, append(chunk(0x4000, bytes.Repeat([]byte{0xff}, 1024)), chunk(0xafff, chunk(0xa200, chunk(0xa300, []byte(ref+"\x00"))))...)))
}

func TestFTPBasicCompositionRuntime(t *testing.T) {
	for _, mode := range []string{"directory", "refs"} {
		t.Run(mode, func(t *testing.T) { testFTPBasicComposition(t, openObjectCatalogScanTestDB(t), mode) })
	}
}

func TestFTPModelCompositionIdentificationIsSharedAcrossDepths(t *testing.T) {
	p := &ftpRangeStructureFixture{&ftpStructureFixture{data: structure3DS("A.PNG"),
		filesystemScanTestProvider: filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{
			"models": {progressFile("models/model.3ds"), progressFile("models/A.PNG")},
		}}}}
	*p.entriesByPath["models"][0].Storage.SizeBytes = int64(len(p.data))
	db := openObjectCatalogScanTestDB(t)
	runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), metaRepo.NewScanRepository(db), nil)
	for _, depth := range []string{"basic", "deep"} {
		ctx, _ := scanflow.WithFileProgress(t.Context(), nil)
		plan, err := runtime.prepareFileScan(ctx, p, p, nil, &commonModels.Engine{ID: 26, EngineType: "ftp"}, []string{"models"}, depth)
		if err != nil {
			t.Fatal(err)
		}
		items := plan.directories["models"].detection.Items
		if len(items) != 1 || len(items[0].RelatedRefs()) != 2 ||
			commonJSON.Section(items[0].Attributes, "format_info.3ds")["scan_complete"] != true {
			t.Fatalf("%s identification lost the common composition: %+v", depth, items)
		}
	}
	// This verifies identification only. Deep content enrichment remains a later
	// independent stage and may reject the intentionally opaque geometry payload.
	if p.sequential != 0 || p.ranges == 0 {
		t.Fatal("composition identification depended on sequential/deep content")
	}
}

func TestFTPBasicCompositionAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	for _, mode := range []string{"directory", "refs"} {
		t.Run(mode, func(t *testing.T) {
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			t.Cleanup(func() { tx.Rollback() })
			if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS meta").Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}); err != nil {
				t.Fatal(err)
			}
			testFTPBasicComposition(t, tx, mode)
			testFTPDeepComposition(t, tx, mode)
		})
	}
}

func testFTPBasicComposition(t *testing.T, db *gorm.DB, mode string) {
	t.Helper()
	p := &ftpStructureFixture{filesystemScanTestProvider: filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{"models": {}}, contentByPath: map[string]string{}}}
	rangeProvider := &ftpRangeStructureFixture{p}
	previous, previousErr := plugin.Get("ftp")
	plugin.Register(rangeProvider)
	t.Cleanup(func() {
		plugin.Unregister("ftp")
		if previousErr == nil {
			plugin.Register(previous)
		}
	})
	repo := metaRepo.NewScanRepository(db)
	runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	engine := &commonModels.Engine{ID: 26, EngineType: "ftp"}
	var originalID uint
	var originalFingerprint string
	preservedDeep := false
	scan := func(raw []byte, want string, complete bool) models.MetaItem {
		t.Helper()
		p.data = raw
		files := []plugin.EngineCatalogEntry{progressFile("models/model.3ds"), progressFile("models/A.PNG"), progressFile("models/B.PNG"), progressFile("models/unrelated.csv")}
		*files[0].Storage.SizeBytes = int64(len(raw))
		p.entriesByPath["models"] = files
		p.contentByPath["models/model.3ds"] = string(raw)
		p.contentByPath["models/A.PNG"] = "not opened"
		p.contentByPath["models/B.PNG"] = "not opened"
		p.contentByPath["models/unrelated.csv"] = "not opened"
		var err error
		if mode == "directory" {
			_, err = runtime.ScanPaths(t.Context(), engine, 1, []string{"models"}, "basic", true, nil)
		} else {
			_, err = runtime.ScanRefGroups(t.Context(), engine, 1, []models.ScanRefGroup{{Primary: "models/model.3ds", Refs: []models.ScanRef{
				{Path: "models/model.3ds", Role: "model", Required: true}, {Path: "models/A.PNG", Role: "texture", Required: true}, {Path: "models/B.PNG", Role: "texture", Required: true},
			}}}, "basic", true, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		var item models.MetaItem
		if err := db.Where("tenant_id=? AND engine_id=? AND full_name=?", 1, 26, "models/model.3ds").First(&item).Error; err != nil {
			t.Fatal(err)
		}
		if originalID == 0 {
			originalID = item.ID
			originalFingerprint = item.Fingerprint
		}
		wantDepth := "basic"
		if preservedDeep {
			wantDepth = "deep"
		}
		if item.ID != originalID || item.Fingerprint != originalFingerprint || item.ScannedDepth != wantDepth {
			t.Fatalf("identity/depth changed: %+v", item)
		}
		native := commonJSON.Section(item.Attributes, "format_info.3ds")
		if native["scan_complete"] != complete {
			t.Fatalf("complete=%v want=%v attrs=%v", native["scan_complete"], complete, item.Attributes)
		}
		if commonJSON.String(item.Attributes, "storage", "physical_path") != "models/model.3ds" || p.sequential != 0 {
			t.Fatalf("basic content path escaped: attrs=%v sequential=%d", item.Attributes, p.sequential)
		}
		if !preservedDeep && len(commonJSON.Section(item.Attributes, "type_info.model_3d")) > 0 {
			t.Fatal("basic invented model statistics")
		}
		if preservedDeep && (commonJSON.Int64(item.Attributes, "type_info.model_3d", "mesh_count") != 123 || native["master_scale"] != float64(0.001)) {
			t.Fatal("basic composition refresh discarded existing deep facts")
		}
		refs := commonJSON.InterfaceSlice(commonJSON.Section(item.Attributes, "item")["refs"])
		wantRefs := 1
		if complete && (want == "A.PNG" || want == "B.PNG") {
			wantRefs = 2
		}
		if len(refs) != wantRefs {
			t.Fatalf("refs=%v want=%d", refs, wantRefs)
		}
		if complete {
			declarations := commonJSON.InterfaceSlice(native["texture_refs"])
			if len(declarations) != 1 || declarations[0] != want {
				t.Fatalf("declarations=%v want=%q", declarations, want)
			}
		} else if _, ok := native["texture_refs"]; ok {
			t.Fatal("failed observation retained old complete declarations")
		}
		return item
	}
	old := scan(structure3DS("A.PNG"), "A.PNG", true)
	old.Attributes["type_info"] = map[string]interface{}{"model_3d": map[string]interface{}{"mesh_count": 123}}
	commonJSON.Section(old.Attributes, "format_info.3ds")["master_scale"] = 0.001
	if err := db.Model(&old).Updates(map[string]interface{}{"attributes": old.Attributes, "scanned_depth": "deep"}).Error; err != nil {
		t.Fatal(err)
	}
	preservedDeep = true
	scan(structure3DS("B.PNG"), "B.PNG", true)
	scan(structure3DS("A.PNG"), "A.PNG", true)
	scan(structure3DS("missing.png"), "missing.png", true)
	scan(structure3DS("../outside.png"), "../outside.png", true)
	scan([]byte("broken"), "", false)
	scan(structure3DS("A.PNG"), "A.PNG", true)
	p.rangeError = fmt.Errorf("synthetic range failure")
	scan(structure3DS("B.PNG"), "", false)
	p.rangeError = nil
	plugin.Register(p)
	scan(structure3DS("B.PNG"), "", false)
	plugin.Register(rangeProvider)
	scan(structure3DS("A.PNG"), "A.PNG", true)
	if p.ranges == 0 || p.bytes > 4096 {
		t.Fatalf("structure I/O missing or read payload: ranges=%d bytes=%d", p.ranges, p.bytes)
	}
	if mode == "refs" && p.lists != 0 {
		t.Fatal("explicit refs enumerated a directory")
	}
}

func TestBasicModelStructureDoesNotExpandOtherEngines(t *testing.T) {
	for _, kind := range []string{"nfs", "s3"} {
		t.Run(kind, func(t *testing.T) {
			p := &ftpRangeStructureFixture{&ftpStructureFixture{engineType: kind, data: structure3DS("A.PNG"),
				filesystemScanTestProvider: filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{
					"models": {progressFile("models/model.3ds"), progressFile("models/A.PNG")},
				}}}}
			*p.entriesByPath["models"][0].Storage.SizeBytes = int64(len(p.data))
			previous, err := plugin.Get(kind)
			plugin.Register(p)
			t.Cleanup(func() {
				plugin.Unregister(kind)
				if err == nil {
					plugin.Register(previous)
				}
			})
			db := openObjectCatalogScanTestDB(t)
			repo := metaRepo.NewScanRepository(db)
			runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
			if _, err := runtime.ScanPaths(t.Context(), &commonModels.Engine{ID: 26, EngineType: kind}, 1, []string{"models"}, "basic", true, nil); err != nil {
				t.Fatal(err)
			}
			var item models.MetaItem
			if err := db.Where("tenant_id=? AND engine_id=? AND full_name=?", 1, 26, "models/model.3ds").First(&item).Error; err != nil {
				t.Fatal(err)
			}
			if p.ranges != 0 || p.sequential != 0 || len(commonJSON.Section(item.Attributes, "format_info.3ds")) != 0 {
				t.Fatal("FTP basic structure route expanded another engine")
			}
		})
	}
}

// fullSummary3DS is valid for both the structural observer and the independent
// complete-summary reader. No image content is needed or opened by these tests.
func fullSummary3DS(ref string) []byte {
	chunk := func(id uint16, body []byte) []byte {
		out := make([]byte, 6)
		binary.LittleEndian.PutUint16(out, id)
		binary.LittleEndian.PutUint32(out[2:], uint32(len(body)+6))
		return append(out, body...)
	}
	vertices := make([]byte, 2+36)
	binary.LittleEndian.PutUint16(vertices, 3)
	object := chunk(0x4000, append([]byte("mesh\x00"), chunk(0x4100, chunk(0x4110, vertices))...))
	if ref != "" {
		object = append(object, chunk(0xafff, chunk(0xa200, chunk(0xa300, []byte(ref+"\x00"))))...)
	}
	return chunk(0x4d4d, chunk(0x3d3d, object))
}

func TestFTPDeepCompositionRuntime(t *testing.T) {
	for _, mode := range []string{"directory", "refs"} {
		t.Run(mode, func(t *testing.T) { testFTPDeepComposition(t, openObjectCatalogScanTestDB(t), mode) })
	}
}

func testFTPDeepComposition(t *testing.T, db *gorm.DB, mode string) {
	t.Helper()
	p := &ftpRangeStructureFixture{&ftpStructureFixture{allowSequential: true,
		filesystemScanTestProvider: filesystemScanTestProvider{entriesByPath: map[string][]plugin.EngineCatalogEntry{}, contentByPath: map[string]string{}}}}
	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	p.contentByPath["models/A.PNG"] = imageBytes.String()
	previous, previousErr := plugin.Get("ftp")
	plugin.Register(p)
	t.Cleanup(func() {
		plugin.Unregister("ftp")
		if previousErr == nil {
			plugin.Register(previous)
		}
	})
	repo := metaRepo.NewScanRepository(db)
	runtime := NewFilesystemCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	engine := &commonModels.Engine{ID: 26, EngineType: "ftp"}
	read := func() models.MetaItem {
		t.Helper()
		var item models.MetaItem
		if err := db.Where("tenant_id=? AND engine_id=? AND full_name=?", 1, 26, "models/model.3ds").First(&item).Error; err != nil {
			t.Fatal(err)
		}
		return item
	}
	scan := func(ref, depth string, failRange bool) models.MetaItem {
		t.Helper()
		p.data = fullSummary3DS(ref)
		p.rangeError = nil
		if failRange {
			p.rangeError = fmt.Errorf("synthetic native range failure")
		}
		entries := []plugin.EngineCatalogEntry{progressFile("models/model.3ds")}
		refs := []models.ScanRef{{Path: "models/model.3ds", Role: "model", Required: true}}
		if ref != "" {
			entries = append(entries, progressFile("models/A.PNG"))
			refs = append(refs, models.ScanRef{Path: "models/A.PNG", Role: "texture", Required: true})
		}
		*entries[0].Storage.SizeBytes = int64(len(p.data))
		p.entriesByPath["models"] = entries
		p.contentByPath["models/model.3ds"] = string(p.data)
		var err error
		if mode == "directory" {
			_, err = runtime.ScanPaths(t.Context(), engine, 1, []string{"models"}, depth, true, nil)
		} else {
			_, err = runtime.ScanRefGroups(t.Context(), engine, 1, []models.ScanRefGroup{{Primary: "models/model.3ds", Refs: refs}}, depth, true, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		return read()
	}
	// Ordinary texture names match exact observed files. Raw whitespace in an
	// unresolved declaration remains verbatim; this does not claim physical
	// whitespace-name support from the existing filesystem path normalizers.
	for _, declaration := range []string{"A.PNG", " A.PNG "} {
		basic := scan(declaration, "basic", false)
		deep := scan(declaration, "deep", false)
		for _, item := range []models.MetaItem{basic, deep} {
			native := commonJSON.Section(item.Attributes, "format_info.3ds")
			declarations := commonJSON.InterfaceSlice(native["texture_refs"])
			if native["scan_complete"] != true || len(declarations) != 1 || declarations[0] != declaration {
				t.Fatalf("lost exact declaration: %v", native)
			}
			refs := commonJSON.InterfaceSlice(commonJSON.Section(item.Attributes, "item")["refs"])
			wantRefs := 1
			if declaration == "A.PNG" {
				wantRefs = 2
			}
			if len(refs) != wantRefs {
				t.Fatalf("declaration %q claimed wrong files: %v", declaration, refs)
			}
		}
		if basic.ID != deep.ID || deep.ScannedDepth != "deep" || commonJSON.Int64(deep.Attributes, "type_info.model_3d", "vertex_count") != 3 {
			t.Fatalf("deep summary did not persist independently: %v", deep)
		}
	}
	// Range failure plus successful sequential no-texture summary must remain
	// incomplete, removing the previous declarations rather than inventing [].
	failed := scan("", "deep", true)
	native := commonJSON.Section(failed.Attributes, "format_info.3ds")
	if native["scan_complete"] != false {
		t.Fatalf("deep upgraded failed structure: %v", native)
	}
	if _, present := native["texture_refs"]; present {
		t.Fatalf("failed structure retained/invented refs: %v", native)
	}
	if commonJSON.Int64(failed.Attributes, "type_info.model_3d", "vertex_count") != 3 {
		t.Fatal("negative did not exercise successful full summary")
	}
	// Known-item deep refresh cannot invent a missing composition snapshot or
	// replace a prior exact declaration, and must not enumerate/range-read.
	for _, snapshot := range []map[string]interface{}{{}, {"scan_complete": false}, {"scan_complete": true, "texture_refs": []string{" prior.PNG "}}} {
		old := read()
		old.Attributes["format_info"] = map[string]interface{}{"3ds": snapshot}
		if err := db.Model(&old).Update("attributes", old.Attributes).Error; err != nil {
			t.Fatal(err)
		}
		var parent models.MetaNode
		if err := db.First(&parent, old.NodeID).Error; err != nil {
			t.Fatal(err)
		}
		detected := scanflow.KnownItemDetectedItem(&old, scanflow.KnownItemDescriptorFromAttributes(old.Attributes))
		ranges, lists := p.ranges, p.lists
		_, err := scanprocessor.New(repo, nil, nil).Process(t.Context(), scanprocessor.KnownItemInput(engine, 1, &parent, old, old.Attributes, detected, p, nil, plugin.FileItemPathForEngine(26), "models/model.3ds", "", "models/model.3ds", int64(len(p.data)), "deep"))
		if err != nil {
			t.Fatal(err)
		}
		updated := read()
		got := commonJSON.Section(updated.Attributes, "format_info.3ds")
		for _, key := range []string{"scan_complete", "texture_refs"} {
			want, wasPresent := snapshot[key]
			actual, present := got[key]
			if wasPresent != present || (present && fmt.Sprint(want) != fmt.Sprint(actual)) {
				t.Fatalf("known deep rewrote %s: got=%v want=%v", key, got, snapshot)
			}
		}
		if ranges != p.ranges || lists != p.lists {
			t.Fatal("known-item deep rediscovered the composition")
		}
	}
	if p.sequential == 0 {
		t.Fatal("deep regression never opened complete content")
	}
}
