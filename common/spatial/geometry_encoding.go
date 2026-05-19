package spatial

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
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
