package scanprocessor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	parquetformat "github.com/addp/common/format/plugins/parquet"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
	parquetgo "github.com/parquet-go/parquet-go"
)

func TestParquetWholeScopeDeepScanAndRefresh(t *testing.T) {
	for _, storage := range []string{"object", "file"} {
		for _, operation := range []string{"deep_scan", "shallow_scan", "refresh", "corrupt_part", "precision_conflict", "scale_conflict", "listing_error"} {
			t.Run(storage+"/"+operation, func(t *testing.T) {
				db := openObjectCatalogProcessorTestDB(t)
				repo := metaRepo.NewScanRepository(db)
				root := "samples/result"
				parentAttrs := models.JSONMap{}
				if storage == "object" {
					root = "addp/" + root
					parentAttrs = scanresource.ObjectBucketNodeAttributes("addp")
				}
				parent, err := repo.UpsertNode(1, 7, nil, "directory", "samples", strPtr(path.Dir(root)), parentAttrs)
				if err != nil {
					t.Fatal(err)
				}
				catalogPathFor := plugin.FileItemPathForEngine(7)
				if storage == "object" {
					catalogPathFor = plugin.ObjectItemPathForBucketRef(7, "addp")
				}
				first := scopeDecimalParquet(t, 38, 18)
				second := scopeDecimalParquet(t, 38, 18)
				switch operation {
				case "corrupt_part":
					second = []byte("invalid parquet footer")
				case "precision_conflict":
					second = scopeDecimalParquet(t, 37, 18)
				case "scale_conflict":
					second = scopeDecimalParquet(t, 38, 17)
				}
				reader := &scopeCatalogContentReader{object: storage == "object", files: map[string][]byte{
					root + "/dt=2026-10-12/part-000.parquet": first,
					root + "/dt=2026-10-13/part-001.parquet": second,
				}}
				if operation == "listing_error" {
					reader.listErr = fmt.Errorf("scope listing failed")
				}
				detected := &metaitem.DetectedItem{ResolvedItem: dataitem.ResolvedItem{
					Layout: format.LayoutWhole, DataType: datatype.Table, Format: "parquet", ScopePath: root,
				}, PhysicalPath: root}
				attrs := metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(detected)))
				resource := &commonModels.Engine{ID: 7}
				in := input{
					Resource: resource, TenantID: 1, EngineID: 7, ParentNode: parent, ItemType: storage,
					ItemName: "result", FullName: root, Attributes: attrs, Detected: detected,
					ContentReader: reader, EngineCatalogPathFor: catalogPathFor, PhysicalPath: root,
					ScanDepth: models.ScannedDepthDeep, StrictDeepEnrich: true,
				}
				var old *models.MetaItem
				if operation == "shallow_scan" {
					in.ScanDepth = models.ScannedDepthBasic
				} else if operation != "deep_scan" {
					attrs["type_info"] = map[string]interface{}{"table": map[string]interface{}{
						"fields": []interface{}{map[string]interface{}{"name": "old_field", "type": "int"}}, "row_count": 99,
					}}
					rows, size := int64(99), int64(42)
					old, err = repo.UpsertItemWithDepth(1, 7, parent, storage, "result", root, attrs, &rows, &size, nil, models.ScannedDepthDeep)
					if err != nil {
						t.Fatal(err)
					}
					detected = scanflow.KnownItemDetectedItem(old, scanflow.KnownItemDescriptorFromAttributes(old.Attributes))
					in = KnownItemInput(resource, 1, parent, *old, old.Attributes, detected, reader, nil, catalogPathFor, root, "", root, size, "deep")
				}
				var before []byte
				if old != nil {
					before, err = json.Marshal(old.Attributes)
					if err != nil {
						t.Fatal(err)
					}
				}
				result, err := New(repo, nil, nil).Process(context.Background(), in)
				if old != nil && operation != "refresh" {
					if err == nil {
						t.Fatal("invalid scope refresh must fail")
					}
					var stored models.MetaItem
					if err := db.First(&stored, old.ID).Error; err != nil {
						t.Fatal(err)
					}
					after, err := json.Marshal(stored.Attributes)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(after, before) || stored.RowCount == nil || *stored.RowCount != 99 {
						t.Fatal("failed refresh changed the stored snapshot")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if operation == "shallow_scan" {
					if reader.opens != 0 || reader.lists != 0 || result.Item.RowCount != nil {
						t.Fatal("shallow scan performed deep content enrichment")
					}
					return
				}
				var stored models.MetaItem
				if err := db.First(&stored, result.Item.ID).Error; err != nil {
					t.Fatal(err)
				}
				fields := commonJSON.InterfaceSlice(commonJSON.Section(stored.Attributes, "type_info.table")["fields"])
				if len(fields) != 2 || result.Fields != 2 { // amount plus the dt partition column
					t.Fatalf("whole scope fields=%v result.Fields=%d", fields, result.Fields)
				}
				field := commonJSON.InterfaceMap(fields[0])
				if field["name"] != "amount" || field["type"] != string(datatype.FieldTypeDecimal) || commonJSON.InterfaceInt64(field["precision"]) != 38 || commonJSON.InterfaceInt64(field["scale"]) != 18 {
					t.Fatalf("decimal field facts=%v", field)
				}
				if stored.RowCount == nil || *stored.RowCount != 2 || commonJSON.Int64(stored.Attributes, "type_info.table", "row_count") != 2 {
					t.Fatalf("whole scope row count=%v", stored.RowCount)
				}
				if len(parquetformat.FileRowCountsFromAttributes(stored.Attributes)) != 2 || reader.opens != 2 {
					t.Fatalf("not all parts were described: opens=%d attrs=%v", reader.opens, stored.Attributes)
				}
				if stored.FullName != root || (old != nil && stored.ID != old.ID) {
					t.Fatal("refresh changed data item identity")
				}
			})
		}
	}
}

func scopeDecimalParquet(t *testing.T, precision, scale int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	schema := parquetgo.NewSchema("spark_decimal", parquetgo.Group{"amount": parquetgo.Decimal(scale, precision, parquetgo.FixedLenByteArrayType(16))})
	writer := parquetgo.NewWriter(&buffer, schema)
	if _, err := writer.WriteRows([]parquetgo.Row{{parquetgo.FixedLenByteArrayValue(make([]byte, 16)).Level(0, 0, 0)}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type scopeCatalogContentReader struct {
	staticObjectContentReader
	object       bool
	files        map[string][]byte
	listErr      error
	opens, lists int
}

func (r *scopeCatalogContentReader) OpenContent(_ context.Context, _ plugin.ConnectionInfo, p plugin.EngineCatalogPath, _ plugin.ReadOptions) (io.ReadCloser, error) {
	r.opens++
	data, ok := r.files[p.StringPath()]
	if !ok {
		return nil, fmt.Errorf("unexpected content path: %s", p.StringPath())
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (r *scopeCatalogContentReader) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return nil, fmt.Errorf("unexpected stat call")
}

func (r *scopeCatalogContentReader) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.EngineCatalogPath, _ plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	r.lists++
	if r.listErr != nil {
		return nil, r.listErr
	}
	last := parent.Segments[len(parent.Segments)-1]
	if last.Kind != plugin.EngineCatalogKindDirectory && last.Kind != plugin.EngineCatalogKindPrefix {
		return nil, fmt.Errorf("scope must be a catalog branch: %v", parent)
	}
	prefix := parent.StringPath() + "/"
	children := map[string]bool{}
	for key := range r.files {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		child, _, branch := strings.Cut(strings.TrimPrefix(key, prefix), "/")
		children[prefix+child] = branch
	}
	keys := make([]string, 0, len(children))
	for key := range children {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	nodes := make([]plugin.EngineCatalogEntry, 0, len(keys))
	for _, key := range keys {
		branch := children[key]
		p := plugin.FileItemPath(7, key)
		if branch {
			p = plugin.FileDirectoryPath(7, key)
		}
		if r.object {
			bucket, objectPath, _ := strings.Cut(key, "/")
			p = plugin.ObjectItemPath(7, bucket, objectPath)
			if branch {
				p = plugin.ObjectDirectoryPath(7, bucket, objectPath)
			}
		}
		role := plugin.EngineCatalogRoleLeaf
		if branch {
			role = plugin.EngineCatalogRoleBranch
		}
		nodes = append(nodes, plugin.EngineCatalogEntry{Path: p, Role: role})
	}
	return nodes, nil
}
