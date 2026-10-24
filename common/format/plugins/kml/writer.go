package kml

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
	"github.com/addp/common/spatial"
	"github.com/twpayne/go-geom"
)

// The reader remains the bound GDAL container reader. Writing a single source
// table uses the standard streaming writer, with no native process or sidecar.
func (p *Plugin) OpenTableWriter(ctx context.Context, output io.Writer, table *datatype.TableInfo, options *format.WriteOptions) (format.TableWriter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if output == nil || table == nil {
		return nil, fmt.Errorf("KML writer requires output and table fields")
	}
	if options == nil || options.SpatialInfo == nil {
		return nil, fmt.Errorf("KML writer requires known EPSG:4326 spatial info")
	}
	if err := resume.RejectUnsupported(options.ResumeMarker, "kml.table_writer"); err != nil {
		return nil, err
	}
	s := options.SpatialInfo
	ref, srid := strings.ToUpper(strings.TrimSpace(s.PrimaryCRSRef())), s.PrimarySRIDValue()
	if (ref != "EPSG:4326" && ref != "OGC:CRS84" && !(ref == "" && srid == 4326)) || (srid != 0 && srid != 4326) {
		return nil, fmt.Errorf("KML writer requires EPSG:4326 geometry, got CRS %q SRID %d", ref, srid)
	}
	if (s.CRSRef != "" && !strings.EqualFold(s.CRSRef, "EPSG:4326") && !strings.EqualFold(s.CRSRef, "OGC:CRS84")) || (s.SRID != nil && *s.SRID != 4326) {
		return nil, fmt.Errorf("KML writer has conflicting spatial CRS declarations")
	}
	if len(s.GeometryColumns) != 1 || s.PrimaryGeometryName() == "" {
		return nil, fmt.Errorf("KML writer requires exactly one named geometry column")
	}
	if d := s.PrimaryGeometry().Dimension; d != nil && *d != 2 {
		return nil, fmt.Errorf("KML writer supports XY geometry only")
	}
	w := &tableWriter{output: output, fields: append([]datatype.FieldInfo(nil), table.Fields...), geometry: s.PrimaryGeometryName(), names: map[string]bool{}}
	var schema []simpleField
	geometryCount := 0
	for _, f := range w.fields {
		if strings.TrimSpace(f.Name) == "" || !validXMLText(f.Name) || w.names[f.Name] {
			return nil, fmt.Errorf("invalid or duplicate KML field name %q", f.Name)
		}
		w.names[f.Name] = true
		if f.Type == datatype.FieldTypeGeometry {
			geometryCount++
			if f.Name != w.geometry {
				return nil, fmt.Errorf("KML geometry field does not match spatial info")
			}
			continue
		}
		kmlType := map[datatype.FieldType]string{datatype.FieldTypeString: "string", datatype.FieldTypeBool: "bool", datatype.FieldTypeInt: "int", datatype.FieldTypeFloat: "float", datatype.FieldTypeDouble: "double"}[f.Type]
		if kmlType == "" {
			return nil, fmt.Errorf("unsupported KML field type %q for %q", f.Type, f.Name)
		}
		schema = append(schema, simpleField{Name: f.Name, Type: kmlType})
	}
	if geometryCount != 1 {
		return nil, fmt.Errorf("KML writer requires exactly one geometry field")
	}
	if !validXMLText(table.Name) {
		return nil, fmt.Errorf("invalid XML characters in KML layer name")
	}
	header, err := xml.Marshal(struct {
		XMLName xml.Name      `xml:"Schema"`
		ID      string        `xml:"id,attr"`
		Fields  []simpleField `xml:"SimpleField"`
	}{ID: "addp_schema", Fields: schema})
	if err != nil {
		return nil, err
	}
	name, err := xml.Marshal(struct {
		XMLName xml.Name `xml:"name"`
		Text    string   `xml:",chardata"`
	}{Text: table.Name})
	if err != nil {
		return nil, err
	}
	data := append([]byte(xml.Header+`<kml xmlns="http://www.opengis.net/kml/2.2"><Document>`), header...)
	data = append(data, []byte("<Folder>")...)
	data = append(data, name...)
	if err := w.write(data); err != nil {
		return nil, err
	}
	return w, nil
}

type simpleField struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}
type simpleData struct {
	Name string `xml:"name,attr"`
	Text string `xml:",chardata"`
}
type schemaData struct {
	URL    string       `xml:"schemaUrl,attr"`
	Values []simpleData `xml:"SimpleData"`
}
type placemark struct {
	XMLName  xml.Name   `xml:"Placemark"`
	Data     schemaData `xml:"ExtendedData>SchemaData"`
	Geometry *geometryElement
}

// MarshalXML deliberately emits the geometry's KML element name, not the Go
// struct field name. Coordinates are already validated before serialization.
type geometryElement struct {
	name        string
	coordinates string
	children    []*geometryElement
}

func (g *geometryElement) MarshalXML(e *xml.Encoder, _ xml.StartElement) error {
	start := xml.StartElement{Name: xml.Name{Local: g.name}}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	if g.coordinates != "" {
		if err := e.EncodeElement(g.coordinates, xml.StartElement{Name: xml.Name{Local: "coordinates"}}); err != nil {
			return err
		}
	}
	for _, child := range g.children {
		if err := e.Encode(child); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}

type tableWriter struct {
	output   io.Writer
	fields   []datatype.FieldInfo
	names    map[string]bool
	geometry string
	closed   bool
	failure  error
}

func (w *tableWriter) WriteRows(ctx context.Context, rows []map[string]interface{}) (err error) {
	if w.failure != nil {
		return w.failure
	}
	if w.closed {
		return fmt.Errorf("KML writer is closed")
	}
	if err = ctx.Err(); err != nil {
		w.failure = err
		return err
	}
	defer func() {
		if err != nil {
			w.failure = err
		}
	}()
	for _, row := range rows {
		if err = ctx.Err(); err != nil {
			return err
		}
		for name := range row {
			if !w.names[name] {
				return fmt.Errorf("undeclared KML field %q", name)
			}
		}
		item := placemark{Data: schemaData{URL: "#addp_schema"}}
		for _, f := range w.fields {
			value := row[f.Name]
			if value == nil {
				if !f.Nullable {
					return fmt.Errorf("KML field %q is not nullable", f.Name)
				}
				continue
			}
			if f.Type == datatype.FieldTypeGeometry {
				g, parseErr := spatial.ParseGeometryValue(value)
				if parseErr != nil {
					return fmt.Errorf("KML field %q: %w", f.Name, parseErr)
				}
				item.Geometry, err = encodeGeometry(g, 0)
			} else {
				var text string
				text, err = scalarText(f.Type, value)
				item.Data.Values = append(item.Data.Values, simpleData{Name: f.Name, Text: text})
			}
			if err != nil {
				return fmt.Errorf("KML field %q: %w", f.Name, err)
			}
		}
		data, marshalErr := xml.Marshal(item)
		if marshalErr != nil {
			return marshalErr
		}
		if err = w.write(data); err != nil {
			return err
		}
	}
	return nil
}
func (w *tableWriter) write(data []byte) error {
	n, err := w.output.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.failure = err
	}
	return err
}
func (w *tableWriter) Close(ctx context.Context) error {
	if w.failure != nil {
		return w.failure
	}
	if w.closed {
		return nil
	}
	if err := ctx.Err(); err != nil {
		w.failure = err
		return err
	}
	if err := w.write([]byte("</Folder></Document></kml>")); err != nil {
		return err
	}
	w.closed = true
	return nil
}
func (w *tableWriter) Abort(context.Context) error {
	w.closed = true
	if w.failure == nil {
		w.failure = fmt.Errorf("KML writer was aborted")
	}
	return nil
}

func validXMLText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r != 9 && r != 10 && r != 13 && !(r >= 0x20 && r <= 0xD7FF) && !(r >= 0xE000 && r <= 0xFFFD) && !(r >= 0x10000 && r <= 0x10FFFF) {
			return false
		}
	}
	return true
}
func scalarText(t datatype.FieldType, value interface{}) (string, error) {
	switch t {
	case datatype.FieldTypeString:
		s, ok := value.(string)
		if ok && validXMLText(s) {
			return s, nil
		}
	case datatype.FieldTypeBool:
		if b, ok := value.(bool); ok {
			return strconv.FormatBool(b), nil
		}
	case datatype.FieldTypeInt:
		var n int64
		if v, ok := value.(json.Number); ok {
			parsed, err := v.Int64()
			if err != nil {
				return "", err
			}
			n = parsed
		} else {
			v := reflect.ValueOf(value)
			if v.Kind() < reflect.Int || v.Kind() > reflect.Int64 {
				return "", fmt.Errorf("expected signed integer, got %T", value)
			}
			n = v.Int()
		}
		if n >= math.MinInt32 && n <= math.MaxInt32 {
			return strconv.FormatInt(n, 10), nil
		}
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble:
		var n float64
		switch v := value.(type) {
		case float32:
			n = float64(v)
		case float64:
			n = v
		case json.Number:
			var err error
			n, err = v.Float64()
			if err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("expected floating point number, got %T", value)
		}
		if !math.IsInf(n, 0) && !math.IsNaN(n) {
			if t == datatype.FieldTypeFloat {
				if float64(float32(n)) != n {
					return "", fmt.Errorf("value is not exactly representable as float32")
				}
				return strconv.FormatFloat(n, 'g', -1, 32), nil
			}
			return strconv.FormatFloat(n, 'g', -1, 64), nil
		}
	}
	return "", fmt.Errorf("invalid or out-of-range %s value (%T)", t, value)
}
func encodeGeometry(g geom.T, depth int) (*geometryElement, error) {
	if g == nil || g.Empty() || depth > 32 {
		return nil, fmt.Errorf("empty or excessively nested KML geometry")
	}
	if g.SRID() != 0 && g.SRID() != 4326 {
		return nil, fmt.Errorf("geometry SRID %d is not EPSG:4326", g.SRID())
	}
	if _, ok := g.(*geom.GeometryCollection); !ok && g.Layout() != geom.XY {
		return nil, fmt.Errorf("KML export supports XY geometry only")
	}
	switch v := g.(type) {
	case *geom.Point:
		return coordinateElement("Point", v.Coords(), 1)
	case *geom.LineString:
		return coordinateElement("LineString", v.FlatCoords(), 2)
	case *geom.Polygon:
		polygon := &geometryElement{name: "Polygon"}
		for i := 0; i < v.NumLinearRings(); i++ {
			ring := v.LinearRing(i)
			coords := ring.FlatCoords()
			if len(coords) < 8 || coords[0] != coords[len(coords)-2] || coords[1] != coords[len(coords)-1] {
				return nil, fmt.Errorf("KML polygon ring must be closed with at least four positions")
			}
			e, err := coordinateElement("LinearRing", coords, 4)
			if err != nil {
				return nil, err
			}
			boundary := "innerBoundaryIs"
			if i == 0 {
				boundary = "outerBoundaryIs"
			}
			polygon.children = append(polygon.children, &geometryElement{name: boundary, children: []*geometryElement{e}})
		}
		return polygon, nil
	case *geom.MultiPoint:
		children := make([]geom.T, v.NumPoints())
		for i := range children {
			children[i] = v.Point(i)
		}
		return encodeMulti(children, depth)
	case *geom.MultiLineString:
		children := make([]geom.T, v.NumLineStrings())
		for i := range children {
			children[i] = v.LineString(i)
		}
		return encodeMulti(children, depth)
	case *geom.MultiPolygon:
		children := make([]geom.T, v.NumPolygons())
		for i := range children {
			children[i] = v.Polygon(i)
		}
		return encodeMulti(children, depth)
	case *geom.GeometryCollection:
		return encodeMulti(v.Geoms(), depth)
	default:
		return nil, fmt.Errorf("unsupported KML geometry %T", g)
	}
}
func encodeMulti(children []geom.T, depth int) (*geometryElement, error) {
	multi := &geometryElement{name: "MultiGeometry"}
	for _, child := range children {
		e, err := encodeGeometry(child, depth+1)
		if err != nil {
			return nil, err
		}
		multi.children = append(multi.children, e)
	}
	return multi, nil
}
func coordinateElement(name string, coords []float64, minPositions int) (*geometryElement, error) {
	if len(coords)%2 != 0 || len(coords) < minPositions*2 {
		return nil, fmt.Errorf("invalid KML %s coordinate count", name)
	}
	var b bytes.Buffer
	for i := 0; i < len(coords); i += 2 {
		x, y := coords[i], coords[i+1]
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || x < -180 || x > 180 || y < -90 || y > 90 {
			return nil, fmt.Errorf("invalid WGS84 KML coordinate")
		}
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
		b.WriteByte(',')
		b.WriteString(strconv.FormatFloat(y, 'g', -1, 64))
	}
	return &geometryElement{name: name, coordinates: b.String()}, nil
}
