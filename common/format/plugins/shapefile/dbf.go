package shapefile

import (
	"fmt"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/jonas-p/go-shp"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"os"
	"strconv"
	"strings"
	"time"
)

type dbfFieldInfo struct {
	Name      string
	RawType   string
	Size      int
	Precision int
}

func parseDBFAttributeWithInfo(field dbfFieldInfo, raw string) interface{} {
	t := byte(0)
	if field.RawType != "" {
		t = field.RawType[0]
	}
	switch t {
	case 'N', 'F':
		if field.Precision > 0 || strings.Contains(raw, ".") {
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

type shapefileDBFSchemaInfo struct {
	fields        []shp.Field
	originalNames []string
}

func shapefileDBFSchema(tableInfo *datatype.TableInfo, geometryField string) shapefileDBFSchemaInfo {
	info := shapefileDBFSchemaInfo{
		fields:        make([]shp.Field, 0, len(tableInfo.Fields)),
		originalNames: make([]string, 0, len(tableInfo.Fields)),
	}
	used := map[string]int{}
	for _, field := range tableInfo.Fields {
		if strings.EqualFold(field.Name, geometryField) || datatype.IsSpatialFieldType(field.Type) {
			continue
		}
		dbfType, size, precision := commonTypeToDBFNative(field.Type)
		if datatype.IsNumericFieldType(field.Type) {
			if field.Precision > 0 {
				size = field.Precision
			}
			if field.Scale > 0 {
				precision = field.Scale
			}
		} else if field.Size > 0 {
			size = field.Size
		}
		name := uniqueDBFFieldName(field.Name, used)
		info.fields = append(info.fields, dbfField(name, dbfType, size, precision))
		info.originalNames = append(info.originalNames, field.Name)
	}
	return info
}

func normalizeDBFValue(value interface{}, field shp.Field) interface{} {
	if value == nil {
		return strings.Repeat(" ", int(field.Size))
	}
	switch field.Fieldtype {
	case 'C':
		text := fmt.Sprint(value)
		if len(text) > int(field.Size) {
			return text[:field.Size]
		}
		return text + strings.Repeat(" ", int(field.Size)-len(text))
	case 'N':
		if parsed, ok := int64DBFValue(value); ok {
			return fitDBFText(fmt.Sprintf("%*d", int(field.Size), parsed), int(field.Size))
		}
	case 'F':
		if parsed, ok := floatDBFValue(value); ok {
			text := strconv.FormatFloat(parsed, 'f', int(field.Precision), 64)
			return fitDBFText(fmt.Sprintf("%*s", int(field.Size), text), int(field.Size))
		}
	case 'D':
		switch v := value.(type) {
		case time.Time:
			return v.Format("20060102")
		case string:
			text := strings.TrimSpace(v)
			if len(text) >= 10 && text[4] == '-' && text[7] == '-' {
				return strings.ReplaceAll(text[:10], "-", "")
			}
			return fitDBFText(text, int(field.Size))
		}
	case 'L':
		switch v := value.(type) {
		case bool:
			if v {
				return "T"
			}
			return "F"
		}
	}
	return value
}

func fitDBFText(text string, size int) string {
	if size <= 0 {
		return text
	}
	if len(text) > size {
		return text[:size]
	}
	return strings.Repeat(" ", size-len(text)) + text
}

func int64DBFValue(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return uint64ToInt64(uint64(v))
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		return uint64ToInt64(v)
	case float32:
		return int64(v), true
	case float64:
		return int64(v), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func uint64ToInt64(value uint64) (int64, bool) {
	const maxInt64 = uint64(^uint64(0) >> 1)
	if value > maxInt64 {
		return 0, false
	}
	return int64(value), true
}

func floatDBFValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func valueByName(row map[string]interface{}, name string) (interface{}, bool) {
	if value, ok := row[name]; ok {
		return value, true
	}
	lowerName := strings.ToLower(name)
	for key, value := range row {
		if strings.ToLower(key) == lowerName {
			return value, true
		}
	}
	return nil, false
}

func uniqueDBFFieldName(name string, used map[string]int) string {
	normalized := strings.ToUpper(strings.TrimSpace(name))
	if normalized == "" {
		normalized = "FIELD"
	}
	normalized = strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_':
			return r
		default:
			return '_'
		}
	}, normalized)
	if len(normalized) > 10 {
		normalized = normalized[:10]
	}
	count := used[normalized]
	used[normalized] = count + 1
	if count == 0 {
		return normalized
	}
	suffix := fmt.Sprintf("_%d", count+1)
	prefixLen := 10 - len(suffix)
	if prefixLen < 1 {
		prefixLen = 1
	}
	if len(normalized) > prefixLen {
		normalized = normalized[:prefixLen]
	}
	return normalized + suffix
}

func dbfField(name, dbfType string, size, precision int) shp.Field {
	if size <= 0 {
		size = 254
	}
	if size > 254 {
		size = 254
	}
	if precision < 0 {
		precision = 0
	}
	if precision > 15 {
		precision = 15
	}
	switch strings.ToUpper(dbfType) {
	case "N":
		return shp.NumberField(name, uint8(size))
	case "F":
		return shp.FloatField(name, uint8(size), uint8(precision))
	case "D":
		return shp.DateField(name)
	case "L":
		field := shp.StringField(name, 1)
		field.Fieldtype = 'L'
		return field
	default:
		return shp.StringField(name, uint8(size))
	}
}

// TypeMapper Shapefile DBF类型映射器
type TypeMapper struct{}

// Name 返回映射器名称
func (m *TypeMapper) Name() string {
	return "shapefile"
}

// ToCommon 将Shapefile DBF类型转换为通用类型
// nativeType 为单字符字符串（如 "C", "N", "F", "L", "D", "M"）
func (m *TypeMapper) ToCommon(nativeType string) datatype.FieldType {
	return dbfNativeTypeToCommon(nativeType)
}

// FromCommon 将通用类型转换为Shapefile DBF类型
// 返回: (DBF类型字符, 字段长度, 小数位数)
func (m *TypeMapper) FromCommon(commonType datatype.FieldType) (string, int, int) {
	return commonTypeToDBFNative(commonType)
}

func dbfNativeTypeToCommon(nativeType string) datatype.FieldType {
	if len(nativeType) == 0 {
		return datatype.FieldTypeUnknown
	}

	switch nativeType[0] {
	case 'C':
		return datatype.FieldTypeString
	case 'N':
		return datatype.FieldTypeFloat
	case 'F':
		return datatype.FieldTypeFloat
	case 'L':
		return datatype.FieldTypeBool
	case 'D':
		return datatype.FieldTypeDate
	case 'M':
		return datatype.FieldTypeString
	default:
		return datatype.FieldTypeUnknown
	}
}

func dbfFieldToCommonType(field dbfFieldInfo) datatype.FieldType {
	switch strings.ToUpper(strings.TrimSpace(field.RawType)) {
	case "N":
		if field.Precision > 0 {
			return datatype.FieldTypeDecimal
		}
		if field.Size > 10 {
			return datatype.FieldTypeBigInt
		}
		return datatype.FieldTypeInt
	case "F":
		if field.Size > 13 || field.Precision > 6 {
			return datatype.FieldTypeDouble
		}
		return datatype.FieldTypeFloat
	default:
		return dbfNativeTypeToCommon(field.RawType)
	}
}

func commonTypeToDBFNative(commonType datatype.FieldType) (string, int, int) {
	switch commonType {
	case datatype.FieldTypeString:
		return "C", 254, 0 // Character, 最大254字节
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		return "N", 18, 0 // Numeric, 18位整数
	case datatype.FieldTypeFloat:
		return "F", 13, 6 // Float, 单精度，13位总长度，6位小数
	case datatype.FieldTypeDouble:
		return "F", 20, 8 // Float, 双精度，20位总长度，8位小数
	case datatype.FieldTypeDecimal:
		return "N", 20, 8 // Numeric, 高精度小数
	case datatype.FieldTypeBool:
		return "L", 1, 0 // Logical
	case datatype.FieldTypeDate:
		return "D", 8, 0 // Date (YYYYMMDD)
	default:
		return "C", 254, 0 // 默认为Character
	}
}

// init 自动注册Shapefile类型映射器
func init() {
	format.RegisterTypeMapper(&TypeMapper{})
}

func DecodeDBFText(value string, encodingName string) string {
	decoder := dbfEncodingDecoder(encodingName)
	if decoder == nil {
		return value
	}
	decoded, err := decoder.String(value)
	if err != nil {
		return value
	}
	return decoded
}

func NormalizeDBFEncoding(encodingName string) string {
	name := strings.ToLower(strings.TrimSpace(encodingName))
	name = strings.TrimPrefix(name, "\ufeff")
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "")
	switch name {
	case "", "utf8", "utf-8", "65001":
		return "utf-8"
	case "gbk", "cp936", "ms936", "windows-936", "936":
		return "gbk"
	case "gb18030", "cp54936", "54936":
		return "gb18030"
	case "gb2312", "cp20936", "20936":
		return "gb2312"
	case "big5", "big-5", "cp950", "windows-950", "950":
		return "big5"
	case "latin1", "latin-1", "iso8859-1", "iso-8859-1", "cp1252", "windows-1252", "1252":
		return "windows-1252"
	default:
		return name
	}
}

func dbfEncodingDecoder(encodingName string) *encoding.Decoder {
	switch NormalizeDBFEncoding(encodingName) {
	case "", "utf-8":
		return nil
	case "gbk", "gb2312":
		return simplifiedchinese.GBK.NewDecoder()
	case "gb18030":
		return simplifiedchinese.GB18030.NewDecoder()
	case "big5":
		return traditionalchinese.Big5.NewDecoder()
	case "windows-1252":
		return charmap.Windows1252.NewDecoder()
	default:
		return nil
	}
}

func decodeDBFName(name [11]byte, encodingName string) string {
	raw := string(name[:])
	raw = strings.TrimRight(raw, "\x00")
	return strings.TrimSpace(DecodeDBFText(raw, encodingName))
}

func readCPGEncoding(basePath string) string {
	if basePath == "" {
		return ""
	}
	data, err := os.ReadFile(basePath + extCPG)
	if err != nil {
		return ""
	}
	return NormalizeDBFEncoding(strings.TrimSpace(string(data)))
}
