package shapefile

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jonas-p/go-shp"
)

type reader struct {
	*shp.Reader
	encoding string
}

func open(filename string) (*reader, error) {
	return openWithEncoding(filename, readCPGEncoding(strings.TrimSuffix(filename, filepath.Ext(filename))))
}

func openWithEncoding(filename string, encodingName string) (*reader, error) {
	shpReader, err := shp.Open(filename)
	if err != nil {
		return nil, err
	}
	return &reader{Reader: shpReader, encoding: NormalizeDBFEncoding(encodingName)}, nil
}

func (r *reader) readAllFeatures(maxFeatures int) ([]feature, error) {
	if maxFeatures <= 0 {
		maxFeatures = 1000 // default limit
	}

	fields := r.Fields()
	features := make([]feature, 0, maxFeatures)

	recordIndex := 0
	for r.Next() {
		if len(features) >= maxFeatures {
			break
		}

		_, shape := r.Shape()

		// Read attributes
		properties := make(map[string]interface{}, len(fields))
		for i, field := range fields {
			fieldName := r.trimDBFFieldName(field.Name)
			rawValue := strings.TrimSpace(r.readAttributeDecoded(recordIndex, i))
			if rawValue == "" {
				properties[fieldName] = nil
				continue
			}
			properties[fieldName] = parseDBFAttribute(field.Fieldtype, rawValue)
		}

		features = append(features, feature{
			Geometry:   shape,
			Properties: properties,
		})

		recordIndex++
	}

	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("error reading shapefile: %w", err)
	}

	return features, nil
}

func (r *reader) schema() []dbfFieldInfo {
	fields := r.Fields()
	schema := make([]dbfFieldInfo, 0, len(fields))

	for _, field := range fields {
		schema = append(schema, dbfFieldInfo{
			Name:      r.trimDBFFieldName(field.Name),
			Type:      decodeDBFFieldType(field.Fieldtype),
			RawType:   string(field.Fieldtype),
			Size:      int(field.Size),
			Precision: int(field.Precision),
		})
	}

	return schema
}

func trimDBFFieldName(name [11]byte) string {
	return decodeDBFName(name, "")
}

func (r *reader) trimDBFFieldName(name [11]byte) string {
	if r == nil {
		return trimDBFFieldName(name)
	}
	return decodeDBFName(name, r.encoding)
}

func (r *reader) readAttributeDecoded(row int, field int) string {
	if r == nil {
		return ""
	}
	return DecodeDBFText(r.ReadAttribute(row, field), r.encoding)
}
