package datatype

const (
	ContentIndexKindSparseRow = "sparse_row_index"
	ContentIndexUnitRow       = "row"
	ContentIndexOffsetByte    = "byte"
)

// ContentIndex describes a generic access index for reading content windows.
type ContentIndex struct {
	Kind        string                 `json:"kind"`
	DataType    DataType               `json:"data_type,omitempty"`
	Format      string                 `json:"format,omitempty"`
	Unit        string                 `json:"unit,omitempty"`
	OffsetUnit  string                 `json:"offset_unit,omitempty"`
	Step        int64                  `json:"step,omitempty"`
	RowCount    int64                  `json:"row_count,omitempty"`
	HeaderBytes int64                  `json:"header_bytes,omitempty"`
	Source      map[string]interface{} `json:"source,omitempty"`
	Anchors     []ContentIndexAnchor   `json:"anchors,omitempty"`
}

// ContentIndexAnchor maps a logical row to a physical byte offset.
type ContentIndexAnchor struct {
	Row        int64 `json:"row"`
	ByteOffset int64 `json:"byte_offset"`
}

// NewSparseRowContentIndex builds the standard table sparse row index shape.
func NewSparseRowContentIndex(format string, step int64, headerBytes int64) *ContentIndex {
	return &ContentIndex{
		Kind:        ContentIndexKindSparseRow,
		DataType:    DataTypeTable,
		Format:      format,
		Unit:        ContentIndexUnitRow,
		OffsetUnit:  ContentIndexOffsetByte,
		Step:        step,
		HeaderBytes: headerBytes,
	}
}

// AddAnchor appends a sparse row anchor.
func (c *ContentIndex) AddAnchor(row, byteOffset int64) {
	if c == nil {
		return
	}
	c.Anchors = append(c.Anchors, ContentIndexAnchor{Row: row, ByteOffset: byteOffset})
}

// Clone returns a deep copy of ContentIndex.
func (c *ContentIndex) Clone() *ContentIndex {
	if c == nil {
		return nil
	}
	cloned := &ContentIndex{
		Kind:        c.Kind,
		DataType:    c.DataType,
		Format:      c.Format,
		Unit:        c.Unit,
		OffsetUnit:  c.OffsetUnit,
		Step:        c.Step,
		RowCount:    c.RowCount,
		HeaderBytes: c.HeaderBytes,
		Source:      cloneInterfaceMap(c.Source),
		Anchors:     append([]ContentIndexAnchor(nil), c.Anchors...),
	}
	return cloned
}

// IsSparseRowIndex reports whether the index is the standard table sparse row index.
func (c *ContentIndex) IsSparseRowIndex() bool {
	return c != nil &&
		c.Kind == ContentIndexKindSparseRow &&
		c.DataType == DataTypeTable &&
		c.Unit == ContentIndexUnitRow &&
		c.OffsetUnit == ContentIndexOffsetByte
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
