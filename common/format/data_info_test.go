package format

import (
	"testing"
	"time"

	"github.com/addp/common/datatype"
)

func TestTableInfoCloneDeepCopiesMutableFields(t *testing.T) {
	rowCount := int64(10)
	sizeBytes := int64(20)
	createdAt := time.Unix(100, 0)
	updatedAt := time.Unix(200, 0)
	info := &TableInfo{
		TableInfo: datatype.TableInfo{
			Name:      "cities",
			RowCount:  &rowCount,
			SizeBytes: &sizeBytes,
			CreatedAt: &createdAt,
			UpdatedAt: &updatedAt,
			Fields: []datatype.FieldInfo{
				{Name: "id", Type: datatype.FieldTypeInt},
				{Name: "geom", Type: datatype.FieldTypeGeometry},
			},
			PrimaryKey: []string{"id"},
		},
		SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 2),
	}

	cloned := info.Clone()
	if cloned == nil || cloned == info {
		t.Fatalf("Clone() = %#v", cloned)
	}
	cloned.Fields[0].Name = "changed"
	cloned.PrimaryKey[0] = "changed"
	*cloned.RowCount = 99
	*cloned.SizeBytes = 88
	*cloned.CreatedAt = time.Unix(300, 0)
	*cloned.UpdatedAt = time.Unix(400, 0)
	*cloned.SpatialInfo.PrimaryGeometry().SRID = 3857

	if info.Fields[0].Name != "id" || info.PrimaryKey[0] != "id" {
		t.Fatalf("original field metadata changed: %#v %#v", info.Fields, info.PrimaryKey)
	}
	if *info.RowCount != 10 || *info.SizeBytes != 20 {
		t.Fatalf("original counts changed: row=%d size=%d", *info.RowCount, *info.SizeBytes)
	}
	if !info.CreatedAt.Equal(time.Unix(100, 0)) || !info.UpdatedAt.Equal(time.Unix(200, 0)) {
		t.Fatalf("original timestamps changed: %v %v", info.CreatedAt, info.UpdatedAt)
	}
	if info.SpatialInfo.PrimarySRIDValue() != 4326 {
		t.Fatalf("original spatial info changed: %#v", info.SpatialInfo)
	}
}
