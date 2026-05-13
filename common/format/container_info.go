package format

// ContainerInfo 描述容器格式内部对象的结构化元数据。
//
// 容器本身仍是一个 data item；Children 只描述容器内部可寻址对象，
// 不决定 Meta 是否把 child 物化为独立 item。
type ContainerInfo struct {
	Format        FormatType
	ChildCount    int
	DefaultChild  string
	ResourceCount int
	Children      []ContainerChildInfo
	FormatInfo    map[string]interface{}
}

// ContainerChildInfo 描述容器内部的一个子对象，例如 Excel sheet、SQLite table。
type ContainerChildInfo struct {
	Name        string
	Kind        string
	DataType    string
	RowCount    *int64
	ColumnCount *int
	HasHeader   *bool
	Fields      []FieldInfo
	Properties  map[string]interface{}
}
