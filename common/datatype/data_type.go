package datatype

import "strings"

// DataType is the high-level semantic type of a data item.
type DataType string

const (
	Unknown       DataType = "unknown"
	Table         DataType = "table"
	Document      DataType = "document"
	Media         DataType = "media"
	Container     DataType = "container"
	Graph         DataType = "graph"
	Model3D       DataType = "model_3d"
	PointCloud    DataType = "point_cloud"
	GaussianSplat DataType = "gaussian_splat"
)

var knownDataTypes = map[DataType]struct{}{
	Unknown:       {},
	Table:         {},
	Document:      {},
	Media:         {},
	Container:     {},
	Graph:         {},
	Model3D:       {},
	PointCloud:    {},
	GaussianSplat: {},
}

// ParseDataType normalizes a string into a known ADDP data type.
func ParseDataType(value string) DataType {
	dataType := DataType(strings.ToLower(strings.TrimSpace(value)))
	if IsKnownDataType(dataType) {
		return dataType
	}
	return Unknown
}

// IsKnownDataType reports whether dataType is one of the standard ADDP data types.
func IsKnownDataType(dataType DataType) bool {
	_, ok := knownDataTypes[dataType]
	return ok
}

// IsConcreteDataType reports whether dataType is known and not unknown.
func IsConcreteDataType(dataType DataType) bool {
	return dataType != Unknown && IsKnownDataType(dataType)
}
