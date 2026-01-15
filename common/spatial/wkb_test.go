package spatial_test

import (
	"encoding/hex"
	"testing"

	"github.com/addp/common/spatial"
)

// TestConvertToStandardWKB_StandardWKB 测试标准 WKB 不被修改
func TestConvertToStandardWKB_StandardWKB(t *testing.T) {
	// 标准 WKB Point(1 2)
	// 格式: [字节序][类型][X坐标][Y坐标]
	standardWKB := []byte{
		0x01,       // 字节序: Little Endian
		0x01, 0x00, 0x00, 0x00, // 类型: Point (1)
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F, // X = 1.0
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, // Y = 2.0
	}

	result, err := spatial.ConvertToStandardWKB(standardWKB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(result) != string(standardWKB) {
		t.Errorf("standard WKB was modified:\ngot:  %x\nwant: %x", result, standardWKB)
	}
}

// TestConvertToStandardWKB_GPKGWKB 测试 GPKG WKB 转换
func TestConvertToStandardWKB_GPKGWKB(t *testing.T) {
	tests := []struct {
		name        string
		gpkgWKB     string // Hex string
		wantPrefix  string // Expected prefix of standard WKB (hex)
		description string
	}{
		{
			name: "GPKG WKB Point without envelope",
			// GPKG header (8 bytes):
			//   47 50: Magic "GP"
			//   00: Version
			//   01: Flags (bit 0=1: little endian, bits 1-3=000: no envelope)
			//   E6 10 00 00: SRID 4326 (0x10E6 in little endian)
			// Standard WKB Point (21 bytes): 01 01000000 + X (1.0) + Y (2.0)
			gpkgWKB:     "47500001E61000000101000000000000000000F03F0000000000000040",
			wantPrefix:  "01010000", // Little Endian + Point type
			description: "GPKG header 应被移除，保留标准 WKB",
		},
		{
			name: "GPKG WKB Point with XY envelope",
			// GPKG header (8 bytes) + XY envelope (32 bytes) + Standard WKB (21 bytes)
			//   47 50: Magic "GP"
			//   00: Version
			//   03: Flags (bit 0=1: little endian, bits 1-3=001: XY envelope)
			//   E6 10 00 00: SRID 4326
			// XY Envelope (32 bytes = 4 doubles): minX, maxX, minY, maxY
			gpkgWKB: "47500003E6100000" + // GPKG header (8 bytes)
				"000000000000F03F" + "000000000000F03F" + // minX=1.0, maxX=1.0
				"0000000000000040" + "0000000000000040" + // minY=2.0, maxY=2.0
				"0101000000000000000000F03F0000000000000040", // Standard WKB Point (21 bytes)
			wantPrefix:  "01010000",
			description: "GPKG header + XY envelope 应被移除",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpkgBytes, err := hex.DecodeString(tt.gpkgWKB)
			if err != nil {
				t.Fatalf("invalid test data: %v", err)
			}

			result, err := spatial.ConvertToStandardWKB(gpkgBytes)
			if err != nil {
				t.Fatalf("conversion failed: %v", err)
			}

			resultHex := hex.EncodeToString(result)
			if len(resultHex) < len(tt.wantPrefix) || resultHex[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Errorf("%s\ngot prefix:  %s\nwant prefix: %s\nfull result: %s",
					tt.description, resultHex[:min(8, len(resultHex))], tt.wantPrefix, resultHex)
			}

			t.Logf("✅ %s: GPKG (%d bytes) → Standard (%d bytes)", tt.name, len(gpkgBytes), len(result))
		})
	}
}

// TestConvertToStandardWKB_ISOWKB 测试 ISO WKB 清理维度标志
func TestConvertToStandardWKB_ISOWKB(t *testing.T) {
	tests := []struct {
		name           string
		isoWKB         []byte
		expectedType   uint32 // Expected geometry type after cleaning
		description    string
	}{
		{
			name: "PointZ (type 1001 → 1)",
			isoWKB: []byte{
				0x01,       // Little Endian
				0xE9, 0x03, 0x00, 0x00, // Type: 1001 (PointZ)
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F, // X = 1.0
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, // Y = 2.0
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x40, // Z = 3.0
			},
			expectedType: 1, // Point
			description:  "PointZ 的 Z 标志应被清除",
		},
		{
			name: "LineStringM (type 2002 → 2)",
			isoWKB: []byte{
				0x01,       // Little Endian
				0xD2, 0x07, 0x00, 0x00, // Type: 2002 (LineStringM)
				0x02, 0x00, 0x00, 0x00, // numPoints = 2
				// Point 1
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F, // X = 1.0
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, // Y = 2.0
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x40, // M = 3.0
				// Point 2
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x40, // X = 4.0
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x14, 0x40, // Y = 5.0
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x18, 0x40, // M = 6.0
			},
			expectedType: 2, // LineString
			description:  "LineStringM 的 M 标志应被清除",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := spatial.ConvertToStandardWKB(tt.isoWKB)
			if err != nil {
				t.Fatalf("conversion failed: %v", err)
			}

			// 解析结果的几何类型（Little Endian）
			if len(result) < 5 {
				t.Fatalf("result too short: %d bytes", len(result))
			}

			resultType := uint32(result[1]) | uint32(result[2])<<8 | uint32(result[3])<<16 | uint32(result[4])<<24

			if resultType != tt.expectedType {
				t.Errorf("%s\ngot type:  %d\nwant type: %d", tt.description, resultType, tt.expectedType)
			}

			t.Logf("✅ %s: ISO type %x → Standard type %d", tt.name, tt.isoWKB[1:5], resultType)
		})
	}
}

// TestConvertToStandardWKB_EmptyOrShort 测试边界情况
func TestConvertToStandardWKB_EmptyOrShort(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr bool
	}{
		{"empty", []byte{}, false},
		{"1 byte", []byte{0x01}, false},
		{"4 bytes", []byte{0x01, 0x02, 0x03, 0x04}, false},
		{"7 bytes (< 8)", []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := spatial.ConvertToStandardWKB(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
			if err == nil && string(result) != string(tt.input) {
				t.Errorf("short input was modified")
			}
		})
	}
}

// TestConvertToStandardWKB_InvalidGPKG 测试无效 GPKG 数据
func TestConvertToStandardWKB_InvalidGPKG(t *testing.T) {
	tests := []struct {
		name    string
		input   string // Hex string
		wantErr bool
	}{
		{
			name:    "GPKG header too short (< 8 bytes)",
			input:   "4750", // Only "GP" magic bytes (2 bytes)
			wantErr: false, // < 8 bytes, returned as-is without error
		},
		{
			name: "GPKG header complete but no WKB data",
			// 8-byte header with no envelope, but no WKB data after
			input:   "47500001E6100000",
			wantErr: false, // Returns empty WKB without error
		},
		{
			name: "GPKG with XY envelope but incomplete envelope data",
			// Header indicates XY envelope (32 bytes) but only partial envelope data
			input:   "47500003E6100000FFFF", // flags=0x03 (envelope type=1) but only 2 envelope bytes
			wantErr: true,                    // Should fail: expected 40 bytes total, got 10
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := hex.DecodeString(tt.input)
			_, err := spatial.ConvertToStandardWKB(input)
			if (err != nil) != tt.wantErr {
				t.Errorf("wantErr=%v, got err=%v", tt.wantErr, err)
			}
		})
	}
}

// TestConvertToStandardWKB_Consistency 测试一致性：相同输入产生相同输出
func TestConvertToStandardWKB_Consistency(t *testing.T) {
	// Complete GPKG WKB: 8-byte header + 21-byte standard WKB Point
	gpkgWKB, _ := hex.DecodeString("47500001E61000000101000000000000000000F03F0000000000000040")

	result1, err1 := spatial.ConvertToStandardWKB(gpkgWKB)
	result2, err2 := spatial.ConvertToStandardWKB(gpkgWKB)

	if err1 != nil || err2 != nil {
		t.Fatalf("conversion failed: err1=%v, err2=%v", err1, err2)
	}

	if string(result1) != string(result2) {
		t.Errorf("inconsistent results:\nfirst:  %x\nsecond: %x", result1, result2)
	}

	t.Logf("✅ Consistency verified: %d bytes", len(result1))
}

// BenchmarkConvertToStandardWKB_StandardWKB 基准测试：标准 WKB（无操作）
func BenchmarkConvertToStandardWKB_StandardWKB(b *testing.B) {
	standardWKB := []byte{
		0x01, 0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = spatial.ConvertToStandardWKB(standardWKB)
	}
}

// BenchmarkConvertToStandardWKB_GPKGWKB 基准测试：GPKG WKB 转换
func BenchmarkConvertToStandardWKB_GPKGWKB(b *testing.B) {
	// Complete GPKG WKB: 8-byte header + 21-byte standard WKB Point
	gpkgWKB, _ := hex.DecodeString("47500001E61000000101000000000000000000F03F0000000000000040")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = spatial.ConvertToStandardWKB(gpkgWKB)
	}
}

// BenchmarkConvertToStandardWKB_ISOWKB 基准测试：ISO WKB 清理
func BenchmarkConvertToStandardWKB_ISOWKB(b *testing.B) {
	isoWKB := []byte{
		0x01, 0xE9, 0x03, 0x00, 0x00, // PointZ
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x40,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = spatial.ConvertToStandardWKB(isoWKB)
	}
}

// min helper function for Go < 1.21
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
