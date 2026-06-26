package spatial

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	geomgeojson "github.com/twpayne/go-geom/encoding/geojson"
	"github.com/twpayne/go-geom/encoding/wkb"
	"github.com/twpayne/go-geom/encoding/wkbcommon"
	"github.com/twpayne/go-geom/encoding/wkt"
)

// GeomToWKT encodes a geometry as WKT text.
func GeomToWKT(geometry geom.T) (string, error) {
	if geometry == nil {
		return "", fmt.Errorf("geometry is nil")
	}
	return wkt.Marshal(geometry)
}

// GeomToWKB encodes a geometry as standard WKB bytes.
func GeomToWKB(geometry geom.T) ([]byte, error) {
	if geometry == nil {
		return nil, fmt.Errorf("geometry is nil")
	}
	return wkb.Marshal(geometry, wkb.NDR)
}

// GeomToEWKB encodes a geometry as EWKB bytes. When srid > 0, the encoded
// geometry carries that SRID without mutating the caller's geometry.
func GeomToEWKB(geometry geom.T, srid int) ([]byte, error) {
	if geometry == nil {
		return nil, fmt.Errorf("geometry is nil")
	}
	if srid > 0 && geometry.SRID() != srid {
		cloned, err := cloneGeometry(geometry)
		if err != nil {
			return nil, err
		}
		geometry, err = geom.SetSRID(cloned, srid)
		if err != nil {
			return nil, err
		}
	}
	return ewkb.Marshal(geometry, ewkb.NDR)
}

// EncodeGeometryBytesAsEWKB parses WKB/EWKB geometry values and re-encodes
// them as EWKB bytes. It does not reproject coordinates; srid only controls
// the SRID embedded in the EWKB envelope when positive.
func EncodeGeometryBytesAsEWKB(values [][]byte, srid int) ([][]byte, error) {
	result := make([][]byte, 0, len(values))
	for i, value := range values {
		if value == nil {
			result = append(result, nil)
			continue
		}
		geometry, err := ParseGeometryBytes(value)
		if err != nil {
			return nil, fmt.Errorf("parse geometry[%d]: %w", i, err)
		}
		encoded, err := GeomToEWKB(geometry, srid)
		if err != nil {
			return nil, fmt.Errorf("encode geometry[%d] as EWKB: %w", i, err)
		}
		result = append(result, encoded)
	}
	return result, nil
}

// GeomToGeoJSONGeometry encodes a geometry as a GeoJSON geometry object.
func GeomToGeoJSONGeometry(geometry geom.T) (map[string]interface{}, error) {
	if geometry == nil {
		return nil, fmt.Errorf("geometry is nil")
	}
	data, err := geomgeojson.Marshal(geometry)
	if err != nil {
		return nil, fmt.Errorf("marshal GeoJSON geometry: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode GeoJSON geometry: %w", err)
	}
	return result, nil
}

// GeoJSONGeometryToGeom decodes a GeoJSON geometry object.
func GeoJSONGeometryToGeom(value map[string]interface{}, srid int) (geom.T, error) {
	if value == nil {
		return nil, fmt.Errorf("GeoJSON geometry is nil")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode GeoJSON geometry: %w", err)
	}
	var geometry geom.T
	if err := geomgeojson.Unmarshal(data, &geometry); err != nil {
		return nil, fmt.Errorf("parse GeoJSON geometry: %w", err)
	}
	if srid > 0 && geometry != nil && geometry.SRID() != srid {
		geometry, err = geom.SetSRID(geometry, srid)
		if err != nil {
			return nil, err
		}
	}
	return geometry, nil
}

// ParseGeometryValue decodes geometry values represented as geom.T, WKT text,
// WKB/EWKB bytes, or hex WKB/EWKB text.
func ParseGeometryValue(value interface{}) (geom.T, error) {
	switch v := value.(type) {
	case geom.T:
		if v == nil {
			return nil, fmt.Errorf("geometry is nil")
		}
		return v, nil
	case []byte:
		return ParseGeometryBytes(v)
	case string:
		return ParseGeometryText(v)
	case map[string]interface{}:
		return GeoJSONGeometryToGeom(v, 0)
	default:
		return nil, fmt.Errorf("unsupported geometry value type: %T", value)
	}
}

// ParseGeometryText decodes WKT text or hex WKB/EWKB text.
func ParseGeometryText(text string) (geom.T, error) {
	data := strings.TrimSpace(text)
	if data == "" {
		return nil, fmt.Errorf("geometry text is empty")
	}

	if strings.HasPrefix(data, "0x") || strings.HasPrefix(data, "0X") {
		decoded, err := hex.DecodeString(data[2:])
		if err != nil {
			return nil, fmt.Errorf("decode hex geometry: %w", err)
		}
		return ParseGeometryBytes(decoded)
	}

	if decoded, err := hex.DecodeString(data); err == nil && len(decoded) > 0 {
		if geometry, parseErr := ParseGeometryBytes(decoded); parseErr == nil {
			return geometry, nil
		}
	}

	geometry, err := wkt.Unmarshal(data)
	if err != nil {
		return nil, fmt.Errorf("parse WKT geometry: %w", err)
	}
	return geometry, nil
}

// ParseGeometryBytes decodes WKB, EWKB, GPKG WKB, or ISO SQL/MM WKB bytes.
func ParseGeometryBytes(data []byte) (geom.T, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("geometry bytes are empty")
	}

	geometry, err := wkb.Unmarshal(data)
	if err == nil {
		return geometry, nil
	}
	if shouldParseAsEWKB(err) {
		if geometry, ewkbErr := ewkb.Unmarshal(data); ewkbErr == nil {
			return geometry, nil
		}
	}

	standard, standardErr := ConvertToStandardWKB(data)
	if standardErr != nil {
		return nil, standardErr
	}
	if string(standard) != string(data) {
		if geometry, standardParseErr := wkb.Unmarshal(standard); standardParseErr == nil {
			return geometry, nil
		}
	}

	return nil, err
}

func shouldParseAsEWKB(err error) bool {
	var unknownType wkbcommon.ErrUnknownType
	if errors.As(err, &unknownType) {
		return true
	}

	var unsupportedType wkbcommon.ErrUnsupportedType
	if errors.As(err, &unsupportedType) {
		return true
	}

	return false
}

func cloneGeometry(geometry geom.T) (geom.T, error) {
	switch g := geometry.(type) {
	case *geom.Point:
		return g.Clone(), nil
	case *geom.LineString:
		return g.Clone(), nil
	case *geom.LinearRing:
		return g.Clone(), nil
	case *geom.Polygon:
		return g.Clone(), nil
	case *geom.MultiPoint:
		return g.Clone(), nil
	case *geom.MultiLineString:
		return g.Clone(), nil
	case *geom.MultiPolygon:
		return g.Clone(), nil
	case *geom.GeometryCollection:
		cloned := geom.NewGeometryCollection().SetSRID(g.SRID())
		if err := cloned.SetLayout(g.Layout()); err != nil {
			return nil, err
		}
		for i := 0; i < g.NumGeoms(); i++ {
			child, err := cloneGeometry(g.Geom(i))
			if err != nil {
				return nil, err
			}
			if err := cloned.Push(child); err != nil {
				return nil, err
			}
		}
		return cloned, nil
	default:
		return nil, fmt.Errorf("unsupported geometry type: %T", geometry)
	}
}
