package datatype

// TypeInfo is implemented by common type-info structures that map to
// attributes.type_info.<data_type>.
type TypeInfo interface {
	TypeInfoDataType() DataType
}

// TypeInfoDataType reports that TableInfo maps to attributes.type_info.table.
func (*TableInfo) TypeInfoDataType() DataType { return DataTypeTable }

// TypeInfoDataType reports that DocumentInfo maps to attributes.type_info.document.
func (*DocumentInfo) TypeInfoDataType() DataType { return DataTypeDocument }

// TypeInfoDataType reports that MediaInfo maps to attributes.type_info.media.
func (*MediaInfo) TypeInfoDataType() DataType { return DataTypeMedia }

// TypeInfoDataType reports that ContainerInfo maps to attributes.type_info.container.
func (*ContainerInfo) TypeInfoDataType() DataType { return DataTypeContainer }

// TypeInfoDataType reports that GraphInfo maps to attributes.type_info.graph.
func (*GraphInfo) TypeInfoDataType() DataType { return DataTypeGraph }
