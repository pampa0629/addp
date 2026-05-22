package datatype

import "time"

// TableInfo is the common type info for table data items.
type TableInfo struct {
	Name       string      `json:"name,omitempty"`
	RowCount   *int64      `json:"row_count,omitempty"`
	SizeBytes  *int64      `json:"size_bytes,omitempty"`
	CreatedAt  *time.Time  `json:"created_at,omitempty"`
	UpdatedAt  *time.Time  `json:"updated_at,omitempty"`
	Fields     []FieldInfo `json:"fields,omitempty"`
	PrimaryKey []string    `json:"primary_key,omitempty"`
}

// FieldInfo describes a common field or property semantic model.
type FieldInfo struct {
	Name                 string    `json:"name,omitempty"`
	Type                 FieldType `json:"type,omitempty"`
	NativeType           string    `json:"native_type,omitempty"`
	Nullable             bool      `json:"nullable"`
	PrimaryKey           bool      `json:"primary_key,omitempty"`
	Comment              string    `json:"comment,omitempty"`
	Size                 int       `json:"size,omitempty"`
	Precision            int       `json:"precision,omitempty"`
	Scale                int       `json:"scale,omitempty"`
	OrdinalPosition      int       `json:"ordinal_position,omitempty"`
	DefaultExpression    string    `json:"default_expression,omitempty"`
	Generated            bool      `json:"generated,omitempty"`
	GenerationExpression string    `json:"generation_expression,omitempty"`
}

// FieldNames returns the table field names in declared order.
func (t *TableInfo) FieldNames() []string {
	if t == nil || len(t.Fields) == 0 {
		return nil
	}
	names := make([]string, len(t.Fields))
	for i, field := range t.Fields {
		names[i] = field.Name
	}
	return names
}

// GetField returns the first field with the given name.
func (t *TableInfo) GetField(name string) *FieldInfo {
	if t == nil {
		return nil
	}
	for i := range t.Fields {
		if t.Fields[i].Name == name {
			return &t.Fields[i]
		}
	}
	return nil
}

// HasField reports whether the table contains a field with the given name.
func (t *TableInfo) HasField(name string) bool {
	return t.GetField(name) != nil
}
