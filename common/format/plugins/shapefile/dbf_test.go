package shapefile

import (
	"github.com/addp/common/datatype"
	"github.com/jonas-p/go-shp"
	"golang.org/x/text/encoding/simplifiedchinese"
	"testing"
)

func TestParseDBFAttributeWithInfoUsesPrecisionForNumericValues(t *testing.T) {
	t.Parallel()

	got := parseDBFAttributeWithInfo(dbfFieldInfo{RawType: "N", Size: 10, Precision: 2}, "42")
	if _, ok := got.(float64); !ok {
		t.Fatalf("parseDBFAttributeWithInfo() = %T(%#v), want float64 for decimal numeric field", got, got)
	}
}

func TestParseDBFAttributeWithInfoKeepsIntegerNumericValues(t *testing.T) {
	t.Parallel()

	got := parseDBFAttributeWithInfo(dbfFieldInfo{RawType: "N", Size: 10, Precision: 0}, "42")
	if _, ok := got.(int64); !ok {
		t.Fatalf("parseDBFAttributeWithInfo() = %T(%#v), want int64 for integer numeric field", got, got)
	}
}

func TestNormalizeDBFValueFormatsInt64WithoutIntNarrowing(t *testing.T) {
	t.Parallel()

	field := shp.NumberField("BIG_ID", 18)
	got := normalizeDBFValue(int64(922337203685477580), field)
	want := "922337203685477580"
	if got != want {
		t.Fatalf("normalizeDBFValue() = %#v, want %q", got, want)
	}
}

func TestNormalizeDBFValueRejectsUint64OverflowForNumericDBF(t *testing.T) {
	t.Parallel()

	field := shp.NumberField("BIG_ID", 18)
	value := uint64(^uint64(0))
	if got := normalizeDBFValue(value, field); got != value {
		t.Fatalf("normalizeDBFValue() = %#v, want original value for overflow", got)
	}
}

func TestDBFFieldToCommonTypeUsesHeaderContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field dbfFieldInfo
		want  datatype.FieldType
	}{
		{name: "numeric integer", field: dbfFieldInfo{RawType: "N", Size: 9, Precision: 0}, want: datatype.FieldTypeInt},
		{name: "numeric big integer", field: dbfFieldInfo{RawType: "N", Size: 18, Precision: 0}, want: datatype.FieldTypeBigInt},
		{name: "numeric decimal", field: dbfFieldInfo{RawType: "N", Size: 20, Precision: 8}, want: datatype.FieldTypeDecimal},
		{name: "float", field: dbfFieldInfo{RawType: "F", Size: 13, Precision: 6}, want: datatype.FieldTypeFloat},
		{name: "double", field: dbfFieldInfo{RawType: "F", Size: 20, Precision: 8}, want: datatype.FieldTypeDouble},
		{name: "character", field: dbfFieldInfo{RawType: "C", Size: 32}, want: datatype.FieldTypeString},
		{name: "logical", field: dbfFieldInfo{RawType: "L", Size: 1}, want: datatype.FieldTypeBool},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := dbfFieldToCommonType(tt.field); got != tt.want {
				t.Fatalf("dbfFieldToCommonType(%#v) = %s, want %s", tt.field, got, tt.want)
			}
		})
	}
}

func TestDecodeDBFTextGBK(t *testing.T) {
	t.Parallel()

	encoded, err := simplifiedchinese.GBK.NewEncoder().String("北京")
	if err != nil {
		t.Fatalf("encode GBK failed: %v", err)
	}
	if got := DecodeDBFText(encoded, "GBK"); got != "北京" {
		t.Fatalf("DecodeDBFText() = %q, want 北京", got)
	}
}

func TestNormalizeDBFEncodingHandlesBOMAndGB18030(t *testing.T) {
	t.Parallel()

	if got := NormalizeDBFEncoding("\ufeffUTF-8"); got != "utf-8" {
		t.Fatalf("NormalizeDBFEncoding(UTF-8 BOM) = %q, want utf-8", got)
	}
	if got := NormalizeDBFEncoding("GB18030"); got != "gb18030" {
		t.Fatalf("NormalizeDBFEncoding(GB18030) = %q, want gb18030", got)
	}
}

func TestDecodeDBFTextGB18030(t *testing.T) {
	t.Parallel()

	encoded, err := simplifiedchinese.GB18030.NewEncoder().String("规划用地")
	if err != nil {
		t.Fatalf("encode GB18030 failed: %v", err)
	}
	if got := DecodeDBFText(encoded, "GB18030"); got != "规划用地" {
		t.Fatalf("DecodeDBFText() = %q, want 规划用地", got)
	}
}
