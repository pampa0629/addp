package datatype

import "strings"

// DataType is the high-level semantic type of a data item.
type DataType string

const (
	DataTypeUnknown   DataType = "unknown"
	DataTypeTable     DataType = "table"
	DataTypeDocument  DataType = "document"
	DataTypeMedia     DataType = "media"
	DataTypeContainer DataType = "container"
	DataTypeGraph     DataType = "graph"
	DataTypeFile      DataType = "file"
)

var knownDataTypes = map[DataType]struct{}{
	DataTypeUnknown:   {},
	DataTypeTable:     {},
	DataTypeDocument:  {},
	DataTypeMedia:     {},
	DataTypeContainer: {},
	DataTypeGraph:     {},
	DataTypeFile:      {},
}

// ParseDataType normalizes a string into a known ADDP data type.
func ParseDataType(value string) DataType {
	dataType := DataType(strings.ToLower(strings.TrimSpace(value)))
	if IsKnownDataType(dataType) {
		return dataType
	}
	return DataTypeUnknown
}

// IsKnownDataType reports whether dataType is one of the standard ADDP data types.
func IsKnownDataType(dataType DataType) bool {
	_, ok := knownDataTypes[dataType]
	return ok
}

// IsConcreteDataType reports whether dataType is known and not unknown.
func IsConcreteDataType(dataType DataType) bool {
	return dataType != DataTypeUnknown && IsKnownDataType(dataType)
}
