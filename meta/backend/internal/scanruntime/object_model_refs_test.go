package scanruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/scanflow"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanresource"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestObjectModelRelatedRefsPersistSizeAndClaims(t *testing.T) {
	testObjectModelRelatedRefsPersistSizeAndClaims(t, openObjectCatalogScanTestDB(t))
}

func TestObjectModelRelatedRefsAgainstPostgres(t *testing.T) {
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
	t.Cleanup(func() { _ = pool.Close() })
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
	testObjectModelRelatedRefsPersistSizeAndClaims(t, tx)
	t.Run("basic nested model discovery", func(t *testing.T) { testBasicObjectModelDiscovery(t, tx) })
}

func testObjectModelRelatedRefsPersistSizeAndClaims(t *testing.T, db *gorm.DB) {
	t.Helper()
	metaenrich.RegisterItemResolvers()
	obj := "mtllib model.mtl\no panel\nv 0 0 0\nv 2 0 0\nv 0 3 0\nvt 0 0\nvt 1 0\nvt 0 1\nusemtl paint\nf 1/1 2/2 3/3\n"
	files := map[string]string{
		"model.obj": obj,
		"model.mtl": "newmtl paint\nKd 1 1 1\nd 1\nmap_Kd texture.png\n",
		// The resolver only needs the texture's declared path and catalog size.
		"texture.png": "texture bytes",
	}
	reader := objectCatalogScanTestProvider{contentByPath: map[string]string{}}
	resources := []scanresource.StorageResource{}
	var total int64
	modified := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{"model.obj", "model.mtl", "texture.png"} {
		data := files[name]
		reader.contentByPath["addp/models/"+name] = data
		source := objectResourceForTest(9, "addp", "models/"+name, int64(len(data)), "")
		source.LastModified, source.ETag, source.ContentType = &modified, "primary-etag", "application/x-tgif"
		resources = append(resources, source)
		total += int64(len(data))
	}
	pluginRegisterForTest(t, reader)
	repo := metaRepo.NewScanRepository(db)
	bucket, err := repo.UpsertNode(1, 9, nil, "bucket", "addp", strPtr("addp"), scanresource.ObjectBucketNodeAttributes("addp"))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := repo.EnsureObjectCatalogPrefixPath(1, 9, bucket, "models")
	if err != nil {
		t.Fatal(err)
	}
	// A previous basic scan may have registered the MTL and PNG separately.
	for _, source := range resources[1:] {
		insertObjectItemForTest(t, repo, 1, 9, parent, "addp", source.Path, source.SizeBytes)
	}
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	var previousID uint
	for scan := 0; scan < 2; scan++ {
		count, _, err := runtime.persistObjectResources(context.Background(), &commonModels.Engine{ID: 9, EngineType: reader.Type()},
			1, 9, bucket, resources, newObjectCatalogScanState(), true, models.ScannedDepthDeep, true, "", map[string]bool{}, "object")
		if err != nil || count != 1 {
			t.Fatalf("scan %d: count=%d err=%v", scan, count, err)
		}
		var item models.MetaItem
		if err := db.Where("tenant_id = ? AND engine_id = ? AND full_name = ?", 1, 9, "addp/models/model.obj").First(&item).Error; err != nil {
			t.Fatal(err)
		}
		if item.SizeBytes == nil {
			t.Fatal("item size missing")
		}
		if *item.SizeBytes != total || commonJSON.Int64(item.Attributes, "storage", "total_size") != total {
			t.Fatalf("item size=%d storage total=%d, want component total %d", *item.SizeBytes, commonJSON.Int64(item.Attributes, "storage", "total_size"), total)
		}
		if commonJSON.String(item.Attributes, "item", "layout") != "single" || commonJSON.String(item.Attributes, "item", "format") != "obj" {
			t.Fatalf("unexpected item identity: %v", item.Attributes)
		}
		if item.DataUpdatedAt == nil || !item.DataUpdatedAt.Equal(modified) ||
			commonJSON.String(item.Attributes, "storage", "etag") != "primary-etag" ||
			commonJSON.String(item.Attributes, "storage", "content_type") != "application/x-tgif" ||
			commonJSON.String(item.Attributes, "storage", "content_hash") != fmt.Sprintf("%x", sha256.Sum256([]byte(obj))) {
			t.Fatalf("primary source facts/hash changed: %v", item.Attributes["storage"])
		}
		if scan > 0 && item.ID != previousID {
			t.Fatal("repeated scan changed model identity")
		}
		previousID = item.ID
		var active int64
		if err := db.Model(&models.MetaItem{}).Where("tenant_id = ? AND engine_id = ?", 1, 9).Count(&active).Error; err != nil || active != 1 {
			t.Fatalf("active items=%d err=%v; claimed MTL/texture must be removed", active, err)
		}
	}
}

type objectModelStructureSpy struct {
	objectCatalogScanTestProvider
	primary      plugin.EngineCatalogPath
	data         []byte
	ranges       int
	contentCalls int
}

func (p *objectModelStructureSpy) OpenContent(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	p.contentCalls++
	return nil, fmt.Errorf("basic scan must not open content or dependencies")
}
func (p *objectModelStructureSpy) OpenRange(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if !reflect.DeepEqual(target, p.primary) || opts.Offset < 0 || opts.Length <= 0 || opts.Offset+opts.Length > int64(len(p.data)) {
		return nil, fmt.Errorf("range escaped typed primary")
	}
	p.ranges++
	return io.NopCloser(bytes.NewReader(p.data[opts.Offset : opts.Offset+opts.Length])), nil
}
func TestMinIOBasicObjectCompositionAtBucketRootAndPrefix(t *testing.T) {
	for _, prefix := range []string{"", "models/", "models/models/"} {
		for _, modelFormat := range []string{"3ds", "dae"} {
			for _, withTexture := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%v", prefix, modelFormat, withTexture), func(t *testing.T) {
					data := structure3DS("A.PNG")
					texture := "A.PNG"
					if !withTexture {
						data = make([]byte, 6)
						binary.LittleEndian.PutUint16(data, 0x4d4d)
						binary.LittleEndian.PutUint32(data[2:], 6)
					}
					if modelFormat == "dae" {
						texture = "A B.JpEg"
						ref := ""
						if withTexture {
							ref = "A%20B.JpEg"
						}
						data = []byte(objectDAEForTest(ref))
					}
					primary := prefix + "model." + modelFormat
					reader := &objectModelStructureSpy{primary: plugin.ObjectItemPath(9, "addp", primary), data: data}
					resources := []scanresource.StorageResource{objectResourceForTest(9, "addp", primary, int64(len(data)), "")}
					expectedRefs := 1
					if withTexture {
						resources = append(resources, objectResourceForTest(9, "addp", prefix+texture, 8, ""))
						expectedRefs++
					}
					resources = append(resources, objectResourceForTest(9, "addp", prefix+"unrelated.PNG", 10, ""))
					claims, items, warnings := scanflow.DetectObjectCatalogCompositeItems(t.Context(), reader, reader, nil, 9, resources, false)
					if len(warnings) != 0 || len(items) != 1 || reader.ranges == 0 || reader.contentCalls != 0 {
						t.Fatalf("items=%v warnings=%v ranges=%d", items, warnings, reader.ranges)
					}
					plan, ok := scanresource.PlanObjectCompositeItem(9, items[0], "object")
					if !ok || plan.FullName != "addp/"+primary || commonJSON.Section(plan.Attributes, "format_info."+modelFormat)["scan_complete"] != true {
						t.Fatalf("bad persisted object facts: %+v", plan)
					}
					if len(plan.DataItem.RefList) != expectedRefs || !claims[primary] || claims[prefix+"unrelated.PNG"] {
						t.Fatal("exact model composition missing")
					}
					for _, ref := range plan.DataItem.RefList {
						if ref.Path != "addp/"+primary && ref.Path != "addp/"+prefix+texture {
							t.Fatalf("unqualified or extra ref %s", ref.Path)
						}
					}
				})
			}
		}
	}
}

func objectDAEForTest(texture string) string {
	images := ""
	if texture != "" {
		images = "<library_images><image><init_from>" + texture + "</init_from></image></library_images>"
	}
	return `<COLLADA xmlns="http://www.collada.org/2005/11/COLLADASchema" version="1.4.1">` + images + `<library_geometries><geometry><mesh/></geometry></library_geometries></COLLADA>`
}

func TestMinIOBasicDAEDoesNotClaimTexturesFromIncompleteXML(t *testing.T) {
	for _, raw := range []string{
		strings.TrimSuffix(objectDAEForTest("texture.png"), "</COLLADA>"),
		`<!DOCTYPE COLLADA [<!ENTITY image "texture.png">]>` + objectDAEForTest("&image;"),
		strings.Replace(objectDAEForTest("texture.png"), "</COLLADA>", `<instance_node url="other.dae#mesh"/></COLLADA>`, 1),
	} {
		reader := &objectModelStructureSpy{primary: plugin.ObjectItemPath(9, "addp", "broken/model.dae"), data: []byte(raw)}
		resources := []scanresource.StorageResource{objectResourceForTest(9, "addp", "broken/model.dae", int64(len(raw)), ""), objectResourceForTest(9, "addp", "broken/texture.png", 73, "")}
		claims, items, warnings := scanflow.DetectObjectCatalogCompositeItems(t.Context(), reader, reader, nil, 9, resources, false)
		if len(warnings) != 0 || len(items) != 1 || reader.contentCalls != 0 {
			t.Fatalf("incomplete observation: items=%v warnings=%v", items, warnings)
		}
		plan, ok := scanresource.PlanObjectCompositeItem(9, items[0], "object")
		if !ok || commonJSON.Section(plan.Attributes, "format_info.dae")["scan_complete"] != false || len(plan.DataItem.RefList) != 1 || claims["broken/texture.png"] {
			t.Fatalf("incomplete DAE claimed texture: %+v claims=%v", plan, claims)
		}
	}
}

func TestMinIODAEDeepSummaryPreservesStructuralComposition(t *testing.T) {
	for _, state := range []string{"absent", "incomplete", "complete"} {
		t.Run(state, func(t *testing.T) {
			native := map[string]interface{}{}
			var expectedRefs interface{}
			if state != "absent" {
				native["scan_complete"] = state == "complete"
			}
			if state == "complete" {
				expectedRefs = []string{"approved.png"}
				native["texture_refs"] = []string{"approved.png"}
			}
			attrs := models.JSONMap{"format_info": map[string]interface{}{"dae": native}}
			reader := objectCatalogScanTestProvider{contentByPath: map[string]string{"addp/models/model.dae": objectDAEForTest("deep-added.png")}}
			item := &metaitem.DetectedItem{}
			item.DataType = datatype.Model3D
			item.Format = "dae"
			err := metaenrich.EnrichSingleModel3DItem(t.Context(), attrs, reader, nil, 9, &commonModels.Engine{ID: 9, EngineType: "minio"}, item, "models/model.dae", plugin.ObjectItemPathForBucket(9, "addp"))
			if err != nil {
				t.Fatal(err)
			}
			after := commonJSON.Section(attrs, "format_info.dae")
			complete, observed := after["scan_complete"]
			if observed != (state != "absent") || (observed && complete != (state == "complete")) || !reflect.DeepEqual(after["texture_refs"], expectedRefs) || commonJSON.Int64(commonJSON.Section(attrs, "format_info"), "dae", "mesh_count") != 1 {
				t.Fatalf("deep summary replaced structural facts: state=%s native=%v", state, after)
			}
		})
	}
}

type nestedBasicModelProvider struct {
	*objectModelStructureSpy
	entries []plugin.EngineCatalogEntry
	listed  []plugin.EngineCatalogPath
	dae     []byte
}

func (p *nestedBasicModelProvider) OpenRange(ctx context.Context, conn plugin.ConnectionInfo, target plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if reflect.DeepEqual(target, plugin.ObjectItemPath(9, "addp", "discover/dae/model.dae")) {
		if opts.Offset < 0 || opts.Length <= 0 || opts.Offset+opts.Length > int64(len(p.dae)) {
			return nil, fmt.Errorf("DAE range escaped primary")
		}
		p.ranges++
		return io.NopCloser(bytes.NewReader(p.dae[opts.Offset : opts.Offset+opts.Length])), nil
	}
	return p.objectModelStructureSpy.OpenRange(ctx, conn, target, opts)
}

func (p *nestedBasicModelProvider) Type() string { return "minio" }
func (p *nestedBasicModelProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	p.listed = append(p.listed, parent)
	if parent.StringPath() != "addp/discover" {
		return nil, fmt.Errorf("outside selected prefix: %s", parent.StringPath())
	}
	if !opts.Recursive {
		return []plugin.EngineCatalogEntry{plugin.ObjectPrefixCatalogEntry(parent, "ifc", "addp/discover/ifc/"), plugin.ObjectPrefixCatalogEntry(parent, "3ds", "addp/discover/3ds/"), plugin.ObjectPrefixCatalogEntry(parent, "dae", "addp/discover/dae/")}, nil
	}
	return p.entries, nil
}
func (p *nestedBasicModelProvider) ResolvePath(_ context.Context, _ plugin.ConnectionInfo, target plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	if target.StringPath() == "addp/discover" && target.Segments[len(target.Segments)-1].Term == "prefix" {
		return &plugin.EngineCatalogEntry{Name: "discover", Path: target, Role: plugin.EngineCatalogRoleBranch, Term: "prefix", Kind: "prefix"}, nil
	}
	for _, entry := range p.entries {
		if reflect.DeepEqual(entry.Path, target) {
			return &entry, nil
		}
	}
	return nil, fmt.Errorf("not an observed object")
}
func TestBasicObjectScanDiscoversNestedModelsWithoutContentEnhancement(t *testing.T) {
	testBasicObjectModelDiscovery(t, openObjectCatalogScanTestDB(t))
}
func testBasicObjectModelDiscovery(t *testing.T, db *gorm.DB) {
	t.Helper()
	metaenrich.RegisterItemResolvers()
	data := structure3DS("texture.png")
	dae := []byte(objectDAEForTest("texture%20A.PNG"))
	reader := &nestedBasicModelProvider{objectModelStructureSpy: &objectModelStructureSpy{primary: plugin.ObjectItemPath(9, "addp", "discover/3ds/model.3ds"), data: data}, dae: dae}
	for _, row := range []struct {
		key  string
		size int64
	}{{"discover/ifc/model.ifc", 908}, {"discover/3ds/model.3ds", int64(len(data))}, {"discover/3ds/texture.png", 73}, {"discover/dae/model.dae", int64(len(dae))}, {"discover/dae/texture A.PNG", 73}} {
		reader.entries = append(reader.entries, plugin.EngineCatalogEntry{Name: row.key[len("discover/"):], Path: plugin.ObjectItemPath(9, "addp", row.key), Role: plugin.EngineCatalogRoleLeaf, Term: "object", Kind: "object", Storage: &plugin.EngineCatalogStorageFacts{Path: "addp/" + row.key, SizeBytes: &row.size}})
		reader.entries[len(reader.entries)-1].Name = reader.entries[len(reader.entries)-1].Path.Segments[len(reader.entries[len(reader.entries)-1].Path.Segments)-1].Name
	}
	original, err := plugin.Get("minio")
	if err == nil {
		plugin.Unregister("minio")
	}
	plugin.Register(reader)
	t.Cleanup(func() {
		plugin.Unregister("minio")
		if original != nil {
			plugin.Register(original)
		}
	})
	runtime := NewObjectStorageCatalogRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), metaRepo.NewScanRepository(db), nil)
	result, err := runtime.ScanPaths(t.Context(), &commonModels.Engine{ID: 9, Name: "MinIO", EngineType: "minio"}, 1, []string{"addp/discover"}, nil, models.ScannedDepthBasic, false, nil)
	if err != nil || result.Items != 3 {
		t.Fatalf("basic nested model discovery: items=%d err=%v", result.Items, err)
	}
	if len(reader.listed) != 1 || reader.listed[0].StringPath() != "addp/discover" || reader.ranges < 2 || reader.contentCalls != 0 {
		t.Fatal("discovery scope or structural read missing")
	}
	for _, format := range []string{"ifc", "3ds", "dae"} {
		var item models.MetaItem
		if err := db.Where("engine_id=? AND tenant_id=? AND full_name=?", 9, 1, "addp/discover/"+format+"/model."+format).First(&item).Error; err != nil {
			t.Fatal(err)
		}
		if item.ScannedDepth != "basic" || commonJSON.String(item.Attributes, "item", "format") != format || commonJSON.String(item.Attributes, "item", "data_type") != "model_3d" {
			t.Fatalf("wrong basic identity: %+v", item)
		}
		if format != "ifc" && (commonJSON.Section(item.Attributes, "format_info."+format)["scan_complete"] != true || len(commonJSON.InterfaceSlice(commonJSON.Section(item.Attributes, "item")["refs"])) != 2) {
			t.Fatal("basic lost exact object composition")
		}
	}
}
