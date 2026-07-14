package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/duckdb"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/service/internal/models"
)

func buildTableDependencySnapshot(item *commonModels.MetaItem, capturedAt time.Time) (*models.QueryServiceDependencySnapshot, error) {
	if item == nil {
		return nil, errors.New("meta item is required")
	}
	if item.ID == 0 {
		return nil, errors.New("meta item id is required")
	}
	if strings.TrimSpace(item.Fingerprint) == "" {
		return nil, errors.New("meta item fingerprint is required")
	}

	table := projectTableInfo(datatype.TableInfoFromPayload(
		commonJSON.Section(item.Attributes, "type_info.table"),
		item.Name,
	))
	spatial := datatype.SpatialInfoFromPayload(commonJSON.Section(item.Attributes, "capabilities.spatial"))

	var objectTable *dataitem.ItemDescriptor
	if descriptor, ok := duckdb.ObjectTableDescriptorFromMetaItem(*item); ok {
		projected := projectObjectTableDescriptor(descriptor)
		objectTable = &projected
	}

	snapshot := &models.QueryServiceDependencySnapshot{
		Source: &models.QueryServiceSourceRef{
			ItemID:          item.ID,
			ItemFingerprint: item.Fingerprint,
			ScannedAt:       cloneTime(item.ScannedAt),
			DataUpdatedAt:   cloneTime(item.DataUpdatedAt),
		},
		CapturedAt:         capturedAt.UTC(),
		VerificationStatus: "verified",
		Table:              table,
		Spatial:            spatial.Clone(),
		ObjectTable:        objectTable,
	}
	snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
	return snapshot, nil
}

func buildSQLDependencySnapshot(sql string, contract *models.QueryServiceOutputContract, capturedAt time.Time) *models.QueryServiceDependencySnapshot {
	snapshot := &models.QueryServiceDependencySnapshot{
		CapturedAt:         capturedAt.UTC(),
		VerificationStatus: "verified",
		QueryHash:          hashString(canonicalSQL(sql)),
	}
	if contract != nil {
		snapshot.Table = projectTableInfo(contract.Table)
		snapshot.Spatial = contract.Spatial.Clone()
	}
	snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
	return snapshot
}

func projectTableInfo(info *datatype.TableInfo) *datatype.TableInfo {
	if info == nil {
		return nil
	}
	projected := &datatype.TableInfo{
		Name:       strings.TrimSpace(info.Name),
		Kind:       strings.TrimSpace(info.Kind),
		Fields:     make([]datatype.FieldInfo, 0, len(info.Fields)),
		PrimaryKey: append([]string(nil), info.PrimaryKey...),
	}
	for _, field := range info.Fields {
		projected.Fields = append(projected.Fields, datatype.FieldInfo{
			Name:            strings.TrimSpace(field.Name),
			Type:            datatype.ParseFieldType(string(field.Type)),
			NativeType:      strings.TrimSpace(field.NativeType),
			Nullable:        field.Nullable,
			PrimaryKey:      field.PrimaryKey,
			Size:            field.Size,
			Precision:       field.Precision,
			Scale:           field.Scale,
			OrdinalPosition: field.OrdinalPosition,
		})
	}
	if projected.Name == "" && projected.Kind == "" && len(projected.Fields) == 0 && len(projected.PrimaryKey) == 0 {
		return nil
	}
	return projected
}

func projectObjectTableDescriptor(descriptor dataitem.ItemDescriptor) dataitem.ItemDescriptor {
	descriptor.Refs = append([]dataitem.ItemRef(nil), descriptor.Refs...)
	descriptor.SizeBytes = nil
	return descriptor
}

func queryServiceDependencyHash(snapshot *models.QueryServiceDependencySnapshot) string {
	if snapshot == nil {
		return ""
	}
	spatial := snapshot.Spatial.Clone()
	if spatial != nil {
		spatial.Extent = nil
		spatial.HasSpatialIndex = nil
		spatial.IndexName = ""
		sort.Slice(spatial.CRSDefinitions, func(i, j int) bool {
			return spatial.CRSDefinitions[i].ID < spatial.CRSDefinitions[j].ID
		})
		sort.Slice(spatial.GeometryColumns, func(i, j int) bool {
			return spatial.GeometryColumns[i].Name < spatial.GeometryColumns[j].Name
		})
	}
	var objectTable *dataitem.ItemDescriptor
	if snapshot.ObjectTable != nil {
		projected := projectObjectTableDescriptor(*snapshot.ObjectTable)
		objectTable = &projected
	}
	payload := struct {
		QueryHash             string                       `json:"query_hash,omitempty"`
		Table                 *datatype.TableInfo          `json:"table,omitempty"`
		Spatial               *datatype.SpatialInfo        `json:"spatial,omitempty"`
		ObjectTable           *dataitem.ItemDescriptor     `json:"object_table,omitempty"`
		FederatedObjectTables map[string]map[string]string `json:"federated_object_tables,omitempty"`
	}{
		QueryHash:             snapshot.QueryHash,
		Table:                 projectTableInfo(snapshot.Table),
		Spatial:               spatial,
		ObjectTable:           objectTable,
		FederatedObjectTables: cloneObjectTableMap(snapshot.FederatedObjectTables),
	}
	encoded, _ := json.Marshal(payload)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func cloneObjectTableMap(input map[string]map[string]string) map[string]map[string]string {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]map[string]string, len(input))
	for engineName, tables := range input {
		clonedTables := make(map[string]string, len(tables))
		for tableName, physicalPath := range tables {
			clonedTables[tableName] = physicalPath
		}
		result[engineName] = clonedTables
	}
	return result
}

func queryServiceSnapshotPayload(snapshot *models.QueryServiceDependencySnapshot) map[string]interface{} {
	return commonJSON.MapFromStruct(snapshot)
}

func canonicalSQL(sql string) string {
	normalized := strings.ReplaceAll(sql, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.TrimSpace(normalized)
	for strings.HasSuffix(normalized, ";") {
		normalized = strings.TrimSpace(strings.TrimSuffix(normalized, ";"))
	}
	return normalized
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func queryServiceSnapshotDiff(serviceID uint, published, current *models.QueryServiceDependencySnapshot) *models.QueryServiceSnapshotDiff {
	diff := &models.QueryServiceSnapshotDiff{
		ServiceID:         serviceID,
		Status:            "changed",
		PublishedSnapshot: published,
		CurrentSnapshot:   current,
	}
	if published != nil {
		diff.PublishedDependencyHash = published.DependencyHash
	}
	if current != nil {
		diff.CurrentDependencyHash = current.DependencyHash
	}
	if published == nil || current == nil {
		return diff
	}
	diff.SourceChanged = !reflect.DeepEqual(sourceIdentity(published.Source), sourceIdentity(current.Source))
	diff.TableChanged = !reflect.DeepEqual(projectTableInfo(published.Table), projectTableInfo(current.Table))
	diff.SpatialChanged = !reflect.DeepEqual(spatialDependencyProjection(published.Spatial), spatialDependencyProjection(current.Spatial))
	diff.ObjectTableChanged = !reflect.DeepEqual(published.ObjectTable, current.ObjectTable)
	if published.DependencyHash == current.DependencyHash && !diff.SourceChanged {
		diff.Status = "current"
	}
	return diff
}

func sourceIdentity(source *models.QueryServiceSourceRef) any {
	if source == nil {
		return nil
	}
	return struct {
		ItemID          uint
		ItemFingerprint string
	}{source.ItemID, source.ItemFingerprint}
}

func spatialDependencyProjection(info *datatype.SpatialInfo) *datatype.SpatialInfo {
	projected := info.Clone()
	if projected == nil {
		return nil
	}
	projected.Extent = nil
	projected.HasSpatialIndex = nil
	projected.IndexName = ""
	return projected
}
