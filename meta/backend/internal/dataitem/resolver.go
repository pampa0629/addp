package dataitem

import commondataitem "github.com/addp/common/dataitem"

func InferFormat(fileName, contentType, explicitFormat string) string {
	return commondataitem.InferFormat(fileName, contentType, explicitFormat)
}

func InferDataType(formatName, contentType string) DataType {
	return commondataitem.InferDataType(formatName, contentType)
}
