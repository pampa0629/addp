package datatype

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
