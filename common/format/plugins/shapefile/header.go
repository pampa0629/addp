package shapefile

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/resource"
	"github.com/jonas-p/go-shp"
)

type shpHeaderInfo struct {
	ShapeType shp.ShapeType
	BBox      [4]float64
}

func readSHPHeader(path string) (*shpHeaderInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(32, io.SeekStart); err != nil {
		return nil, err
	}
	var shapeType shp.ShapeType
	if err := binary.Read(file, binary.LittleEndian, &shapeType); err != nil {
		return nil, err
	}
	var bbox [4]float64
	for i := range bbox {
		if err := binary.Read(file, binary.LittleEndian, &bbox[i]); err != nil {
			return nil, err
		}
	}
	return &shpHeaderInfo{ShapeType: shapeType, BBox: bbox}, nil
}

type dbfHeaderInfo struct {
	Version      byte
	RecordCount  int32
	HeaderLength int
	RecordLength int
	Fields       []FieldInfo
}

func readDBFHeader(path string, encodingName string) (*dbfHeaderInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var header [32]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return nil, err
	}
	recordCount := int32(binary.LittleEndian.Uint32(header[4:8]))
	headerLength := int(binary.LittleEndian.Uint16(header[8:10]))
	if headerLength < 33 {
		return nil, fmt.Errorf("invalid DBF header length: %d", headerLength)
	}
	fieldCount := (headerLength - 33) / 32
	fields := make([]FieldInfo, 0, fieldCount)
	for i := 0; i < fieldCount; i++ {
		var desc [32]byte
		if _, err := io.ReadFull(file, desc[:]); err != nil {
			return nil, err
		}
		var name [11]byte
		copy(name[:], desc[0:11])
		fieldType := desc[11]
		fields = append(fields, FieldInfo{
			Name:      decodeDBFName(name, encodingName),
			Type:      DecodeDBFFieldType(fieldType),
			RawType:   string(fieldType),
			Size:      int(desc[16]),
			Precision: int(desc[17]),
		})
	}
	return &dbfHeaderInfo{
		Version:      header[0],
		RecordCount:  recordCount,
		HeaderLength: headerLength,
		RecordLength: int(binary.LittleEndian.Uint16(header[10:12])),
		Fields:       fields,
	}, nil
}

func componentExtensions(components []resource.ComponentRef) []string {
	seen := map[string]bool{}
	extensions := make([]string, 0, len(components))
	for _, component := range components {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(component.Path)), ".")
		if ext == "" || seen[ext] {
			continue
		}
		seen[ext] = true
		extensions = append(extensions, ext)
	}
	sort.Strings(extensions)
	return extensions
}
