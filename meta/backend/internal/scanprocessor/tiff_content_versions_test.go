package scanprocessor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type tiffVersionReader struct {
	staticObjectContentReader
	files map[string]string
	reads []string
}

func TestTIFFVersionsCoverEveryScannedComponent(t *testing.T) {
	aux := []dataitem.ItemRef{{Path: "images/a.tfw", Role: "world_file"}, {Path: "images/a.hdr", Role: "header"}, {Path: "images/a.tif.aux.xml", Role: "auxiliary_metadata"}, {Path: "images/a.prj", Role: "crs"}, {Path: "images/a.tif.ovr", Role: "overview"}, {Path: "images/a.tif.msk", Role: "mask"}}
	for mask := 0; mask < 1<<len(aux); mask++ {
		refs := []dataitem.ItemRef{{Path: "images/a.tif", Role: "main", Primary: true, Required: true}}
		reader := &tiffVersionReader{files: map[string]string{"images/a.tif": "TIFF", "images/unregistered.xml": "private"}}
		for i, ref := range aux {
			if mask&(1<<i) != 0 {
				refs = append(refs, ref)
				reader.files[ref.Path] = ref.Role
			}
		}
		layout := format.LayoutSingle
		if len(refs) > 1 {
			layout = format.LayoutMulti
		}
		in := &input{PhysicalPath: "images/a.tif", ContentReader: reader, EngineCatalogPathFor: func(name string) plugin.EngineCatalogPath { return plugin.FileItemPath(7, name) },
			Detected: &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Format: "tiff", Layout: layout, RefList: refs}}}
		attrs := models.JSONMap{"storage": map[string]interface{}{}}
		if _, err := (Processor{}).extractDeepContent(t.Context(), in, attrs, true); err != nil {
			t.Fatal(err)
		}
		versions := attrs["storage"].(map[string]interface{})["content_versions"].([]dataitem.ContentVersion)
		names := make([]string, 0, len(refs))
		for _, ref := range refs {
			names = append(names, ref.Path)
		}
		sort.Strings(names)
		if !reflect.DeepEqual(names, reader.reads) || len(versions) != len(refs) {
			t.Fatal("incomplete composition read", mask, reader.reads, versions)
		}
		for i, version := range versions {
			content := reader.files[names[i]]
			if version.Path != names[i] || version.SizeBytes != int64(len(content)) || version.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(content))) {
				t.Fatal("wrong component version", version)
			}
		}
		delete(reader.files, names[len(names)-1])
		if _, err := (Processor{}).extractDeepContent(t.Context(), in, attrs, true); err == nil || !reflect.DeepEqual(attrs["storage"].(map[string]interface{})["content_versions"], versions) {
			t.Fatal("missing component committed partial versions", mask)
		}
	}
}

func (r *tiffVersionReader) OpenContent(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	name := target.StringPath()
	r.reads = append(r.reads, name)
	content, found := r.files[name]
	if !found {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func TestTIFFVersionsUseOnlyScannedComponentsAndBindPAMBytes(t *testing.T) {
	for _, object := range []bool{false, true} {
		t.Run(fmt.Sprint(object), func(t *testing.T) {
			prefix := "images/"
			mapper := func(path string) plugin.EngineCatalogPath { return plugin.FileItemPath(7, path) }
			if object {
				prefix = "bucket/images/"
				mapper = func(path string) plugin.EngineCatalogPath {
					return plugin.ObjectItemPath(7, "bucket", strings.TrimPrefix(path, "bucket/"))
				}
			}
			main := prefix + "scene.tif"
			pam := main + ".aux.xml"
			reader := &tiffVersionReader{files: map[string]string{main: "exact TIFF bytes", pam: "<PAMDataset/>", prefix + "unrelated.tif": "private"}}
			in := &input{PhysicalPath: main, ContentReader: reader, EngineCatalogPathFor: mapper,
				Detected: &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{Format: "tiff",
					Layout: format.LayoutMulti, RefList: []dataitem.ItemRef{
						{Path: pam, Role: "auxiliary_metadata"}, {Path: main, Role: "main", Primary: true, Required: true},
					}}}}
			attrs := models.JSONMap{"storage": map[string]interface{}{"content_hash": "old", "content_hash_algorithm": "sha256"}}
			if _, err := (Processor{}).extractDeepContent(t.Context(), in, attrs, true); err != nil {
				t.Fatal(err)
			}
			storage := attrs["storage"].(map[string]interface{})
			versions := storage["content_versions"].([]dataitem.ContentVersion)
			expected := []dataitem.ContentVersion{
				{Path: main, SizeBytes: int64(len(reader.files[main])), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(reader.files[main])))},
				{Path: pam, SizeBytes: int64(len(reader.files[pam])), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(reader.files[pam])))},
			}
			if !reflect.DeepEqual(versions, expected) || !reflect.DeepEqual(reader.reads, []string{main, pam}) ||
				storage["content_hash"] != nil || storage["content_hash_algorithm"] != nil {
				t.Fatalf("versions=%#v reads=%v", storage, reader.reads)
			}
			reader.files[pam] = "<PAMDataset><SRS>EPSG:3857</SRS></PAMDataset>"
			if _, err := (Processor{}).extractDeepContent(t.Context(), in, attrs, true); err != nil {
				t.Fatal(err)
			}
			changed := storage["content_versions"].([]dataitem.ContentVersion)
			if changed[0] != expected[0] || changed[1].SHA256 == expected[1].SHA256 {
				t.Fatal("PAM changes did not invalidate its own version")
			}
			delete(reader.files, pam)
			if _, err := (Processor{}).extractDeepContent(t.Context(), in, attrs, true); err == nil ||
				!reflect.DeepEqual(storage["content_versions"], changed) {
				t.Fatal("missing registered component published partial versions")
			}
			reader.reads = nil
			if _, err := (Processor{}).extractDeepContent(t.Context(), in, attrs, false); err != nil || len(reader.reads) != 0 {
				t.Fatal("basic scan read content")
			}
		})
	}
}

func TestTIFFContentVersionsRefreshAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("META_POSTGRES_TEST_DSN is not set")
	}
	database, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	db := database.Begin()
	if db.Error != nil {
		t.Fatal(db.Error)
	}
	t.Cleanup(func() {
		if err := db.Rollback().Error; err != nil {
			t.Errorf("rollback TIFF version fixture: %v", err)
		}
	})
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}); err != nil {
		t.Fatal(err)
	}
	repo := metaRepo.NewScanRepository(db)
	parent, err := repo.UpsertNode(1, 7, nil, "bucket", "addp", strPtr("addp"), scanresource.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatal(err)
	}
	main := "addp/scene.tif"
	pam := main + ".aux.xml"
	header, world := "addp/scene.hdr", "addp/scene.tfw"
	mask := main + ".msk"
	raw, err := os.ReadFile("../../../../common/format/plugins/image/testdata/unlocated-cog.tif")
	if err != nil {
		t.Fatal(err)
	}
	reader := &tiffVersionReader{files: map[string]string{main: string(raw), pam: "<PAMDataset/>", header: "header", world: "world", mask: "mask"}}
	size := int64(len(raw) + len(reader.files[pam]) + len(reader.files[header]) + len(reader.files[world]) + len(reader.files[mask]))
	item, err := repo.UpsertItemWithDepth(1, 7, parent, "object", "scene.tif", main, models.JSONMap{
		"item": map[string]interface{}{"format": "tiff", "data_type": "media", "layout": "multi", "refs": []map[string]interface{}{
			{"path": main, "role": "main", "primary": true, "required": true},
			{"path": pam, "role": "auxiliary_metadata", "primary": false, "required": false},
			{"path": header, "role": "header", "primary": false, "required": false},
			{"path": world, "role": "world_file", "primary": false, "required": false},
			{"path": mask, "role": "mask", "primary": false, "required": false},
		}},
		"storage": map[string]interface{}{"physical_path": main, "content_hash": "obsolete", "content_hash_algorithm": "sha256"},
	}, nil, &size, nil, models.ScannedDepthDeep)
	if err != nil {
		t.Fatal(err)
	}
	refresh := func() error {
		detected := scanflow.KnownItemDetectedItem(item, scanflow.KnownItemDescriptorFromAttributes(item.Attributes))
		_, err := New(repo, nil, nil).Process(t.Context(), KnownItemInput(
			&commonModels.Engine{ID: 7}, 1, parent, *item, item.Attributes, detected, reader, nil,
			func(name string) plugin.EngineCatalogPath {
				return plugin.ObjectItemPath(7, "addp", strings.TrimPrefix(name, "addp/"))
			},
			main, "addp", "scene.tif", size, "deep",
		))
		return err
	}
	if err := refresh(); err != nil {
		t.Fatal(err)
	}
	var stored models.MetaItem
	if err := db.First(&stored, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	versions, err := dataitem.TIFFContentVersionsFromAttributes(stored.Attributes)
	if err != nil || len(versions) != 5 || versions[3].SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(reader.files[pam]))) || versions[4].SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(reader.files[mask]))) {
		t.Fatalf("persisted versions=%#v, err=%v", versions, err)
	}
	if stored.FullName != main || stored.Fingerprint != item.Fingerprint {
		t.Fatal("content hashing changed the committed identity")
	}
	reader.files[pam] = "<PAMDataset><SRS>EPSG:3857</SRS></PAMDataset>"
	if err := refresh(); err != nil {
		t.Fatal(err)
	}
	var changed models.MetaItem
	if err := db.First(&changed, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	current, err := dataitem.TIFFContentVersionsFromAttributes(changed.Attributes)
	if err != nil || !reflect.DeepEqual(current[:3], versions[:3]) || current[3].SHA256 == versions[3].SHA256 {
		t.Fatal("PAM-only edit did not change the persisted version")
	}
	delete(reader.files, mask)
	if err := refresh(); err == nil {
		t.Fatal("missing registered mask did not fail the refresh")
	}
	var afterFailure models.MetaItem
	if err := db.First(&afterFailure, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterFailure.Attributes, changed.Attributes) || !reflect.DeepEqual(afterFailure.ScannedAt, changed.ScannedAt) {
		t.Fatal("failed refresh overwrote the committed snapshot")
	}
	// A basic rediscovery can change the registered composition, but cannot
	// reuse the former composition's content versions or read fresh source bytes.
	basic := changed.Attributes.Clone()
	itemAttrs := basic["item"].(map[string]interface{})
	itemAttrs["layout"] = "single"
	delete(itemAttrs, "refs")
	detected := scanflow.KnownItemDetectedItem(&changed, scanflow.KnownItemDescriptorFromAttributes(basic))
	reader.reads = nil
	_, err = New(repo, nil, nil).Process(t.Context(), KnownItemInput(
		&commonModels.Engine{ID: 7}, 1, parent, changed, basic, detected, reader, nil,
		func(name string) plugin.EngineCatalogPath {
			return plugin.ObjectItemPath(7, "addp", strings.TrimPrefix(name, "addp/"))
		},
		main, "addp", "scene.tif", int64(len(raw)), "basic",
	))
	if err != nil {
		t.Fatal(err)
	}
	var rediscovered models.MetaItem
	if err := db.First(&rediscovered, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(reader.reads) != 0 || rediscovered.Attributes["storage"].(map[string]interface{})["content_versions"] != nil {
		t.Fatal("basic composition change retained old versions or read source bytes")
	}
}
