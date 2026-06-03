package datatype

// TypeInfo is implemented by common type-info structures.
type TypeInfo interface {
	TypeInfoDataType() DataType
}

// TypeInfoDataType reports that TableInfo describes table data.
func (*TableInfo) TypeInfoDataType() DataType { return Table }

// TypeInfoDataType reports that DocumentInfo describes document data.
func (*DocumentInfo) TypeInfoDataType() DataType { return Document }

// TypeInfoDataType reports that MediaInfo describes media data.
func (*MediaInfo) TypeInfoDataType() DataType { return Media }

// TypeInfoDataType reports that ContainerInfo describes container data.
func (*ContainerInfo) TypeInfoDataType() DataType { return Container }

// TypeInfoDataType reports that GraphInfo describes graph data.
func (*GraphInfo) TypeInfoDataType() DataType { return Graph }
