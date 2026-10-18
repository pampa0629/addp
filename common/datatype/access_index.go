package datatype

import (
	"encoding/json"
	"time"
)

const (
	AccessIndexKindSparseRow = "sparse_row_index"
	AccessIndexUnitRow       = "row"
	AccessIndexOffsetByte    = "byte"
)

// AccessIndex describes a generic access index for reading content windows.
// It is shared here for cross-module reuse; it is not a data type or type info.
type AccessIndex struct {
	Kind        string                 `json:"kind"`
	DataType    DataType               `json:"data_type,omitempty"`
	Format      string                 `json:"format,omitempty"`
	Unit        string                 `json:"unit,omitempty"`
	OffsetUnit  string                 `json:"offset_unit,omitempty"`
	Step        int64                  `json:"step,omitempty"`
	RowCount    int64                  `json:"row_count,omitempty"`
	HeaderBytes int64                  `json:"header_bytes,omitempty"`
	Source      map[string]interface{} `json:"source,omitempty"`
	Anchors     []AccessIndexAnchor    `json:"anchors,omitempty"`
}

// AccessIndexAnchor maps a logical row to a physical byte offset.
type AccessIndexAnchor struct {
	Row        int64 `json:"row"`
	ByteOffset int64 `json:"byte_offset"`
}

// NewSparseRowAccessIndex builds the standard table sparse row index shape.
func NewSparseRowAccessIndex(format string, step int64, headerBytes int64) *AccessIndex {
	return &AccessIndex{
		Kind:        AccessIndexKindSparseRow,
		DataType:    Table,
		Format:      format,
		Unit:        AccessIndexUnitRow,
		OffsetUnit:  AccessIndexOffsetByte,
		Step:        step,
		HeaderBytes: headerBytes,
	}
}

// AddAnchor appends a sparse row anchor.
func (c *AccessIndex) AddAnchor(row, byteOffset int64) {
	if c == nil {
		return
	}
	c.Anchors = append(c.Anchors, AccessIndexAnchor{Row: row, ByteOffset: byteOffset})
}

// Clone returns a deep copy of AccessIndex.
func (c *AccessIndex) Clone() *AccessIndex {
	if c == nil {
		return nil
	}
	cloned := &AccessIndex{
		Kind:        c.Kind,
		DataType:    c.DataType,
		Format:      c.Format,
		Unit:        c.Unit,
		OffsetUnit:  c.OffsetUnit,
		Step:        c.Step,
		RowCount:    c.RowCount,
		HeaderBytes: c.HeaderBytes,
		Source:      cloneInterfaceMap(c.Source),
		Anchors:     append([]AccessIndexAnchor(nil), c.Anchors...),
	}
	return cloned
}

// IsSparseRowIndex reports whether the index is the standard table sparse row index.
func (c *AccessIndex) IsSparseRowIndex() bool {
	return c != nil &&
		c.Kind == AccessIndexKindSparseRow &&
		c.DataType == Table &&
		c.Unit == AccessIndexUnitRow &&
		c.OffsetUnit == AccessIndexOffsetByte
}

// MatchesSource verifies every recorded storage version fact. Size alone is
// insufficient: an in-place replacement can preserve the byte length.
func (c *AccessIndex) MatchesSource(size int64, etag string, modifiedAt *time.Time) bool {
	if c == nil || c.Source == nil || size < 0 {
		return false
	}
	rawSize, err := json.Marshal(c.Source["size_bytes"])
	var recordedSize *int64
	if err != nil || json.Unmarshal(rawSize, &recordedSize) != nil || recordedSize == nil || *recordedSize != size {
		return false
	}
	versioned := false
	if value, present := c.Source["etag"]; present {
		recorded, ok := value.(string)
		if !ok || recorded == "" || recorded != etag {
			return false
		}
		versioned = true
	}
	if value, present := c.Source["last_modified_at"]; present {
		var recorded time.Time
		switch value := value.(type) {
		case string:
			recorded, err = time.Parse(time.RFC3339Nano, value)
		case time.Time:
			recorded = value
		case *time.Time:
			if value != nil {
				recorded = *value
			}
		}
		if err != nil || recorded.IsZero() || modifiedAt == nil || !recorded.Equal(*modifiedAt) {
			return false
		}
		versioned = true
	}
	return versioned
}

func cloneInterfaceMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
