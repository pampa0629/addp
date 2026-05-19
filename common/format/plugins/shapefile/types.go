package shapefile

import (
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/format"
	"github.com/jonas-p/go-shp"
)

type feature struct {
	Geometry   shp.Shape
	Properties map[string]interface{}
}

type dbfFieldInfo struct {
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

func RelatedRefSpecs() []format.RelatedRefSpec {
	return []format.RelatedRefSpec{
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

func decodeDBFFieldType(t byte) string {
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

func parseDBFAttribute(t byte, raw string) interface{} {
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

func almostEqual(a, b float64) bool {
	const epsilon = 1e-9
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}
