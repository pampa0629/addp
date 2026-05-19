package shapefile

import (
	"encoding/binary"
	"fmt"
	"github.com/jonas-p/go-shp"
	"io"
	"math"
	"os"
)

type dbfHeaderInfo struct {
	Version      byte
	RecordCount  int32
	HeaderLength int
	RecordLength int
	Fields       []dbfFieldInfo
}

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
	data := make([]byte, 68)
	if _, err := io.ReadFull(file, data); err != nil {
		return nil, err
	}
	return parseSHPHeaderBytes(data)
}

func parseSHPHeaderBytes(data []byte) (*shpHeaderInfo, error) {
	if len(data) < 68 {
		return nil, fmt.Errorf("invalid SHP header length: %d", len(data)+32)
	}
	shapeType := shp.ShapeType(binary.LittleEndian.Uint32(data[0:4]))
	var bbox [4]float64
	for i := range bbox {
		start := 4 + i*8
		bbox[i] = math.Float64frombits(binary.LittleEndian.Uint64(data[start : start+8]))
	}
	return &shpHeaderInfo{ShapeType: shapeType, BBox: bbox}, nil
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
	headerLength := int(binary.LittleEndian.Uint16(header[8:10]))
	if headerLength < 33 {
		return nil, fmt.Errorf("invalid DBF header length: %d", headerLength)
	}
	data := make([]byte, headerLength)
	copy(data, header[:])
	if _, err := io.ReadFull(file, data[32:]); err != nil {
		return nil, err
	}
	return parseDBFHeaderBytes(data, encodingName)
}

func parseDBFHeaderBytes(data []byte, encodingName string) (*dbfHeaderInfo, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("invalid DBF header length: %d", len(data))
	}
	headerLength := int(binary.LittleEndian.Uint16(data[8:10]))
	if headerLength < 33 || len(data) < headerLength {
		return nil, fmt.Errorf("invalid DBF header length: %d", headerLength)
	}
	fieldCount := (headerLength - 33) / 32
	fields := make([]dbfFieldInfo, 0, fieldCount)
	for i := 0; i < fieldCount; i++ {
		start := 32 + i*32
		desc := data[start : start+32]
		var name [11]byte
		copy(name[:], desc[0:11])
		fieldType := desc[11]
		fields = append(fields, dbfFieldInfo{
			Name:      decodeDBFName(name, encodingName),
			RawType:   string(fieldType),
			Size:      int(desc[16]),
			Precision: int(desc[17]),
		})
	}
	return &dbfHeaderInfo{
		Version:      data[0],
		RecordCount:  int32(binary.LittleEndian.Uint32(data[4:8])),
		HeaderLength: headerLength,
		RecordLength: int(binary.LittleEndian.Uint16(data[10:12])),
		Fields:       fields,
	}, nil
}
