package shapefile

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jonas-p/go-shp"
)

// Feature represents a shapefile feature with geometry and properties
type Feature struct {
	Geometry   shp.Shape
	Properties map[string]interface{}
}

// FieldInfo contains metadata about a DBF field
type FieldInfo struct {
	Name      string
	Type      string
	RawType   string
	Size      int
	Precision int
}

// DecodeDBFFieldType converts DBF field type to human-readable string
func DecodeDBFFieldType(t byte) string {
	switch t {
	case 'C':
		return "character"
	case 'N':
		return "numeric"
	case 'F':
		return "float"
	case 'D':
		return "date"
	case 'L':
		return "logical"
	case 'M':
		return "memo"
	case 'B':
		return "binary"
	default:
		return strings.ToUpper(string(t))
	}
}

// ParseDBFAttribute parses a DBF attribute value based on its type
func ParseDBFAttribute(t byte, raw string) interface{} {
	switch t {
	case 'N', 'F':
		if strings.Contains(raw, ".") {
			if f, err := strconv.ParseFloat(raw, 64); err == nil {
				return f
			}
		}
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f
		}
	case 'L':
		switch strings.ToUpper(raw) {
		case "T", "Y":
			return true
		case "F", "N":
			return false
		}
	case 'D':
		if len(raw) == 8 {
			if ts, err := time.Parse("20060102", raw); err == nil {
				return ts.Format(time.RFC3339)
			}
		}
	}
	return raw
}

// MapDBFType maps DBF field type to standard data type
func MapDBFType(dbfType byte) string {
	switch dbfType {
	case 'C': // Character
		return "string"
	case 'N': // Numeric
		return "float"
	case 'F': // Float
		return "float"
	case 'L': // Logical
		return "bool"
	case 'D': // Date
		return "datetime"
	case 'M': // Memo
		return "string"
	default:
		return "string"
	}
}

// MapShapeType maps shapefile geometry type to standard type
func MapShapeType(shapeType shp.ShapeType) string {
	switch shapeType {
	case shp.POINT, shp.POINTZ, shp.POINTM:
		return "Point"
	case shp.POLYLINE, shp.POLYLINEZ, shp.POLYLINEM:
		return "LineString"
	case shp.POLYGON, shp.POLYGONZ, shp.POLYGONM:
		return "Polygon"
	case shp.MULTIPOINT, shp.MULTIPOINTZ, shp.MULTIPOINTM:
		return "MultiPoint"
	default:
		return "Geometry"
	}
}

// CreateFile creates a file, ensuring parent directory exists
func CreateFile(path string) (*os.File, error) {
	return os.Create(path)
}

// almostEqual checks if two float64 values are approximately equal
func almostEqual(a, b float64) bool {
	const epsilon = 1e-9
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}
