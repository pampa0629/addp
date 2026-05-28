package datatype

// ContainerInfo is the common type info for container data items.
type ContainerInfo struct {
	ChildCount    int                  `json:"child_count,omitempty"`
	DefaultChild  string               `json:"default_child,omitempty"`
	ResourceCount int                  `json:"resource_count,omitempty"`
	Children      []ContainerChildInfo `json:"children,omitempty"`
}

// Clone returns a deep copy of ContainerInfo.
func (c *ContainerInfo) Clone() *ContainerInfo {
	if c == nil {
		return nil
	}
	cloned := *c
	cloned.Children = cloneContainerChildren(c.Children)
	return &cloned
}

// ContainerChildInfo describes an addressable child inside a container.
type ContainerChildInfo struct {
	Name        string                 `json:"name,omitempty"`
	ChildKind   string                 `json:"child_kind,omitempty"`
	DataType    DataType               `json:"data_type,omitempty"`
	Format      string                 `json:"format,omitempty"`
	RowCount    *int64                 `json:"row_count,omitempty"`
	ColumnCount *int                   `json:"column_count,omitempty"`
	HasHeader   *bool                  `json:"has_header,omitempty"`
	Fields      []FieldInfo            `json:"fields,omitempty"`
	Refs        []ContainerChildRef    `json:"refs,omitempty"`
	Native      map[string]interface{} `json:"native,omitempty"`
}

// ContainerChildRef describes a content reference used by a container child.
type ContainerChildRef struct {
	Role      string `json:"role,omitempty"`
	Path      string `json:"path,omitempty"`
	Required  bool   `json:"required,omitempty"`
	Primary   bool   `json:"primary,omitempty"`
	Extension string `json:"extension,omitempty"`
}

func cloneContainerChildren(input []ContainerChildInfo) []ContainerChildInfo {
	if len(input) == 0 {
		return nil
	}
	output := make([]ContainerChildInfo, len(input))
	for i, child := range input {
		output[i] = child
		if child.RowCount != nil {
			rowCount := *child.RowCount
			output[i].RowCount = &rowCount
		}
		if child.ColumnCount != nil {
			columnCount := *child.ColumnCount
			output[i].ColumnCount = &columnCount
		}
		if child.HasHeader != nil {
			hasHeader := *child.HasHeader
			output[i].HasHeader = &hasHeader
		}
		output[i].Fields = append([]FieldInfo(nil), child.Fields...)
		output[i].Refs = append([]ContainerChildRef(nil), child.Refs...)
		output[i].Native = cloneInterfaceMap(child.Native)
	}
	return output
}
