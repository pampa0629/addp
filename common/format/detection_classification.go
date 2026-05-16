package format

func IsGeospatialFormat(format FormatType) bool {
	if capability, ok := GetFormatCapability(format); ok {
		return capability.Spatial
	}
	switch format {
	case FormatShapefile, FormatGeoPackage, FormatKML, FormatKMZ:
		return true
	default:
		return false
	}
}

func IsDocumentFormat(format FormatType) bool {
	if capability, ok := GetFormatCapability(format); ok {
		return capability.DataType == FormatDataTypeDocument
	}
	switch format {
	case FormatPDF, FormatDOCX, FormatPPTX, FormatWPS, FormatText, FormatMarkdown:
		return true
	default:
		return false
	}
}

func IsImageFormat(format FormatType) bool {
	switch format {
	case FormatImage, FormatJPEG, FormatPNG, FormatGIF, FormatTIFF, FormatWebP, FormatBMP, FormatSVG, FormatAVIF, FormatHEIC:
		return true
	default:
		return false
	}
}

func IsTableFormat(format FormatType) bool {
	if capability, ok := GetFormatCapability(format); ok && capability.DataType == FormatDataTypeTable {
		return true
	}
	switch format {
	case FormatCSV, FormatExcel, FormatTSV:
		return true
	default:
		return false
	}
}
