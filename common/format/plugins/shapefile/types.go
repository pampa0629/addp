package shapefile

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/contentio"
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

type Info struct {
	BaseName      string   `json:"base_name,omitempty"`
	RefExtensions []string `json:"ref_extensions,omitempty"`
	HasPRJ        bool     `json:"has_prj,omitempty"`
	HasCPG        bool     `json:"has_cpg,omitempty"`
	ShapeType     string   `json:"shape_type,omitempty"`
	DBFVersion    byte     `json:"dbf_version,omitempty"`
	Encoding      string   `json:"encoding,omitempty"`
}

func RelatedRefSpecs() []contentio.RelatedRefSpec {
	return []contentio.RelatedRefSpec{
		{Extension: ".shp", Role: "main", Required: true, Primary: true},
		{Extension: ".shx", Role: "index", Required: true},
		{Extension: ".dbf", Role: "attributes", Required: true},
		{Extension: ".prj", Role: "projection", Required: false},
		{Extension: ".cpg", Role: "encoding", Required: false},
		{Extension: ".sbn", Role: "spatial_index", Required: false},
		{Extension: ".sbx", Role: "spatial_index", Required: false},
	}
}

func (i *Info) FormatAttributes() map[string]interface{} {
	if i == nil {
		return nil
	}
	attrs := map[string]interface{}{}
	if i.BaseName != "" {
		attrs["base_name"] = i.BaseName
	}
	if len(i.RefExtensions) > 0 {
		attrs["ref_extensions"] = append([]string(nil), i.RefExtensions...)
	}
	attrs["has_prj"] = i.HasPRJ
	attrs["has_cpg"] = i.HasCPG
	if i.ShapeType != "" {
		attrs["shape_type"] = i.ShapeType
	}
	if i.DBFVersion != 0 {
		attrs["dbf_version"] = i.DBFVersion
	}
	if i.Encoding != "" {
		attrs["encoding"] = i.Encoding
	}
	return attrs
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

// AlmostEqual checks if two float64 values are approximately equal
func AlmostEqual(a, b float64) bool {
	const epsilon = 1e-9
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}
