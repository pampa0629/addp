package jsonrecords

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

func WKBGeometryType(value string) string {
	geom, err := decodeWKBGeometry(value)
	if err != nil {
		return ""
	}
	typeName, _ := geom["type"].(string)
	return typeName
}

func wkbTypeName(typeCode uint32) string {
	switch typeCode {
	case 1:
		return "Point"
	case 2:
		return "LineString"
	case 3:
		return "Polygon"
	case 4:
		return "MultiPoint"
	case 5:
		return "MultiLineString"
	case 6:
		return "MultiPolygon"
	case 7:
		return "GeometryCollection"
	default:
		return ""
	}
}

type wkbReader struct {
	data []byte
	pos  int
}

type wkbHeader struct {
	order    binary.ByteOrder
	typeCode uint32
	hasSRID  bool
	srid     uint32
	hasZ     bool
	hasM     bool
}

func decodeWKBGeometry(value string) (map[string]interface{}, error) {
	hexValue := strings.TrimSpace(value)
	if len(hexValue) < 10 || len(hexValue)%2 != 0 {
		return nil, fmt.Errorf("invalid WKB hex length")
	}
	data, err := hex.DecodeString(hexValue)
	if err != nil {
		return nil, err
	}
	reader := &wkbReader{data: data}
	geom, err := reader.readGeometry()
	if err != nil {
		return nil, err
	}
	if reader.pos > len(reader.data) {
		return nil, fmt.Errorf("invalid WKB cursor")
	}
	if reader.pos != len(reader.data) {
		return nil, fmt.Errorf("unexpected trailing WKB data")
	}
	geom["wkb"] = hexValue
	return geom, nil
}

func (r *wkbReader) readGeometry() (map[string]interface{}, error) {
	header, err := r.readHeader()
	if err != nil {
		return nil, err
	}
	typeName := wkbTypeName(header.typeCode)
	if typeName == "" {
		return nil, fmt.Errorf("unsupported WKB geometry type %d", header.typeCode)
	}

	geom := map[string]interface{}{"type": typeName}
	if header.hasSRID {
		geom["srid"] = int64(header.srid)
	}

	switch header.typeCode {
	case 1:
		geom["coordinates"] = r.readPosition(header)
	case 2:
		coordinates, err := r.readPositionList(header)
		if err != nil {
			return nil, err
		}
		geom["coordinates"] = coordinates
	case 3:
		coordinates, err := r.readPolygon(header)
		if err != nil {
			return nil, err
		}
		geom["coordinates"] = coordinates
	case 4, 5, 6:
		coordinates, err := r.readMultiGeometryCoordinates(header)
		if err != nil {
			return nil, err
		}
		geom["coordinates"] = coordinates
	case 7:
		geometries, err := r.readGeometryCollection(header)
		if err != nil {
			return nil, err
		}
		geom["geometries"] = geometries
	}
	if r.pos > len(r.data) {
		return nil, fmt.Errorf("short WKB coordinate data")
	}
	return geom, nil
}

func (r *wkbReader) readHeader() (wkbHeader, error) {
	if r.remaining() < 5 {
		return wkbHeader{}, fmt.Errorf("short WKB header")
	}
	byteOrder := r.data[r.pos]
	r.pos++
	var order binary.ByteOrder = binary.BigEndian
	if byteOrder == 1 {
		order = binary.LittleEndian
	} else if byteOrder != 0 {
		return wkbHeader{}, fmt.Errorf("invalid WKB byte order")
	}
	rawType, err := r.readUint32(order)
	if err != nil {
		return wkbHeader{}, err
	}
	header := wkbHeader{order: order}
	if rawType&0x80000000 != 0 {
		header.hasZ = true
		rawType &^= 0x80000000
	}
	if rawType&0x40000000 != 0 {
		header.hasM = true
		rawType &^= 0x40000000
	}
	if rawType&0x20000000 != 0 {
		header.hasSRID = true
		rawType &^= 0x20000000
		srid, err := r.readUint32(order)
		if err != nil {
			return wkbHeader{}, err
		}
		header.srid = srid
	}
	switch {
	case rawType >= 3000 && rawType < 4000:
		header.hasZ = true
		header.hasM = true
		rawType -= 3000
	case rawType >= 2000 && rawType < 3000:
		header.hasM = true
		rawType -= 2000
	case rawType >= 1000 && rawType < 2000:
		header.hasZ = true
		rawType -= 1000
	}
	header.typeCode = rawType
	return header, nil
}

func (r *wkbReader) readMultiGeometryCoordinates(header wkbHeader) (interface{}, error) {
	count, err := r.readUint32(header.order)
	if err != nil {
		return nil, err
	}
	items := make([]interface{}, 0, count)
	for i := uint32(0); i < count; i++ {
		geom, err := r.readGeometry()
		if err != nil {
			return nil, err
		}
		childType, _ := geom["type"].(string)
		switch header.typeCode {
		case 4:
			if childType != "Point" {
				return nil, fmt.Errorf("invalid MultiPoint child %q", childType)
			}
			items = append(items, geom["coordinates"])
		case 5:
			if childType != "LineString" {
				return nil, fmt.Errorf("invalid MultiLineString child %q", childType)
			}
			items = append(items, geom["coordinates"])
		case 6:
			if childType != "Polygon" {
				return nil, fmt.Errorf("invalid MultiPolygon child %q", childType)
			}
			items = append(items, geom["coordinates"])
		}
	}
	return items, nil
}

func (r *wkbReader) readGeometryCollection(header wkbHeader) ([]interface{}, error) {
	count, err := r.readUint32(header.order)
	if err != nil {
		return nil, err
	}
	items := make([]interface{}, 0, count)
	for i := uint32(0); i < count; i++ {
		geom, err := r.readGeometry()
		if err != nil {
			return nil, err
		}
		items = append(items, geom)
	}
	return items, nil
}

func (r *wkbReader) readPolygon(header wkbHeader) ([]interface{}, error) {
	count, err := r.readUint32(header.order)
	if err != nil {
		return nil, err
	}
	rings := make([]interface{}, 0, count)
	for i := uint32(0); i < count; i++ {
		ring, err := r.readPositionList(header)
		if err != nil {
			return nil, err
		}
		rings = append(rings, ring)
	}
	return rings, nil
}

func (r *wkbReader) readPositionList(header wkbHeader) ([]interface{}, error) {
	count, err := r.readUint32(header.order)
	if err != nil {
		return nil, err
	}
	positions := make([]interface{}, 0, count)
	for i := uint32(0); i < count; i++ {
		positions = append(positions, r.readPosition(header))
		if r.pos > len(r.data) {
			return nil, fmt.Errorf("short WKB coordinate data")
		}
	}
	return positions, nil
}

func (r *wkbReader) readPosition(header wkbHeader) []interface{} {
	position := []interface{}{r.readFloat64(header.order), r.readFloat64(header.order)}
	if header.hasZ {
		position = append(position, r.readFloat64(header.order))
	}
	if header.hasM {
		_ = r.readFloat64(header.order)
	}
	return position
}

func (r *wkbReader) readUint32(order binary.ByteOrder) (uint32, error) {
	if order == nil {
		order = binary.LittleEndian
	}
	if r.remaining() < 4 {
		return 0, fmt.Errorf("short WKB uint32")
	}
	value := order.Uint32(r.data[r.pos : r.pos+4])
	r.pos += 4
	return value, nil
}

func (r *wkbReader) readFloat64(order binary.ByteOrder) float64 {
	if r.remaining() < 8 {
		r.pos = len(r.data) + 1
		return 0
	}
	bits := order.Uint64(r.data[r.pos : r.pos+8])
	r.pos += 8
	return math.Float64frombits(bits)
}

func (r *wkbReader) remaining() int {
	return len(r.data) - r.pos
}
