package shapefile

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/jonas-p/go-shp"
)

// Reader wraps go-shp Reader with additional functionality
type Reader struct {
	*shp.Reader
	encoding string
}

// Open opens a shapefile for reading
func Open(filename string) (*Reader, error) {
	return OpenWithEncoding(filename, readCPGEncoding(strings.TrimSuffix(filename, filepath.Ext(filename))))
}

func OpenWithEncoding(filename string, encodingName string) (*Reader, error) {
	reader, err := shp.Open(filename)
	if err != nil {
		return nil, err
	}
	return &Reader{Reader: reader, encoding: NormalizeDBFEncoding(encodingName)}, nil
}

// ReadAllFeatures reads all features from the shapefile
func (r *Reader) ReadAllFeatures(maxFeatures int) ([]Feature, error) {
	if maxFeatures <= 0 {
		maxFeatures = 1000 // default limit
	}

	fields := r.Fields()
	features := make([]Feature, 0, maxFeatures)

	recordIndex := 0
	for r.Next() {
		if len(features) >= maxFeatures {
			break
		}

		_, shape := r.Shape()

		// Read attributes
		properties := make(map[string]interface{}, len(fields))
		for i, field := range fields {
			fieldName := r.TrimDBFFieldName(field.Name)
			rawValue := strings.TrimSpace(r.ReadAttributeDecoded(recordIndex, i))
			if rawValue == "" {
				properties[fieldName] = nil
				continue
			}
			properties[fieldName] = ParseDBFAttribute(field.Fieldtype, rawValue)
		}

		features = append(features, Feature{
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

// GetSchema returns the schema information
func (r *Reader) GetSchema() []FieldInfo {
	fields := r.Fields()
	schema := make([]FieldInfo, 0, len(fields))

	for _, field := range fields {
		schema = append(schema, FieldInfo{
			Name:      r.TrimDBFFieldName(field.Name),
			Type:      DecodeDBFFieldType(field.Fieldtype),
			RawType:   string(field.Fieldtype),
			Size:      int(field.Size),
			Precision: int(field.Precision),
		})
	}

	return schema
}

// TrimDBFFieldName converts [11]byte field name to trimmed string
func TrimDBFFieldName(name [11]byte) string {
	return decodeDBFName(name, "")
}

func (r *Reader) TrimDBFFieldName(name [11]byte) string {
	if r == nil {
		return TrimDBFFieldName(name)
	}
	return decodeDBFName(name, r.encoding)
}

func (r *Reader) ReadAttributeDecoded(row int, field int) string {
	if r == nil {
		return ""
	}
	return DecodeDBFText(r.ReadAttribute(row, field), r.encoding)
}

// DownloadToFile downloads a reader stream to a file
func DownloadToFile(opener func() (io.ReadCloser, error), target string) (int64, error) {
	if opener == nil {
		return 0, fmt.Errorf("opener is nil")
	}

	reader, err := opener()
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	file, err := CreateFile(target)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	return io.Copy(file, reader)
}
