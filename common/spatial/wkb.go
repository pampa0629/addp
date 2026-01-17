package spatial

import (
	"encoding/binary"
	"fmt"
	"log"
)

// ConvertToStandardWKB 将 GPKG WKB 或扩展 WKB 转换为标准 ISO WKB
//
// 支持的转换:
// 1. GPKG WKB → 标准 WKB（移除 GPKG header 和 envelope）
// 2. ISO SQL/MM WKB → 标准 WKB（清理维度标志）
//
// GPKG WKB 格式: [GP 2字节magic][标志字节][envelope字节][标准WKB]
// 详见: http://www.geopackage.org/spec/#gpb_format
//
// ISO WKB 格式: 类型码包含维度标志（+1000/+2000/+3000 或位标志）
// 标准 WKB 格式: 纯几何类型码（1-7）
func ConvertToStandardWKB(data []byte) ([]byte, error) {
	if len(data) < 8 {
		// 太短，可能不是有效的 WKB
		return data, nil
	}

	// 检测 GPKG WKB magic bytes: "GP" (0x47 0x50)
	if data[0] == 0x47 && data[1] == 0x50 {
		return convertGPKGWKB(data)
	}

	// 检查是否是包含 ISO 扩展标志的 WKB
	if len(data) >= 5 {
		return cleanISOWKBFlags(data)
	}

	// 不是 GPKG WKB 也不需要清理标志,直接返回
	return data, nil
}

// convertGPKGWKB 转换 GPKG WKB 为标准 WKB
// GPKG WKB 格式:
//   Byte 0-1: magic bytes "GP" (0x47 0x50)
//   Byte 2:   version (0x00)
//   Byte 3:   flags (包含 envelope type 和 byte order)
//   Byte 4-7: SRID (little-endian)
//   Byte 8+:  envelope (可选，大小取决于 envelope type)
//   Byte N:   标准 WKB
func convertGPKGWKB(data []byte) ([]byte, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("GPKG WKB too short: %d bytes", len(data))
	}

	flags := data[3]
	envelopeType := (flags >> 1) & 0x07 // bits 1-3

	// 计算 envelope 大小
	envelopeSize := 0
	switch envelopeType {
	case 0: // 无 envelope
		envelopeSize = 0
	case 1: // XY envelope
		envelopeSize = 32 // 4 doubles
	case 2: // XYZ envelope
		envelopeSize = 48 // 6 doubles
	case 3: // XYM envelope
		envelopeSize = 48 // 6 doubles
	case 4: // XYZM envelope
		envelopeSize = 64 // 8 doubles
	default:
		return nil, fmt.Errorf("unknown GPKG envelope type: %d", envelopeType)
	}

	// GPKG header: 8 bytes + envelope
	gpkgHeaderSize := 8 + envelopeSize
	if len(data) < gpkgHeaderSize {
		return nil, fmt.Errorf("GPKG WKB incomplete: expected %d bytes, got %d", gpkgHeaderSize, len(data))
	}

	// 提取标准 WKB (跳过 GPKG header)
	standardWKB := data[gpkgHeaderSize:]
	return standardWKB, nil
}

// cleanISOWKBFlags 清理 ISO SQL/MM WKB 中的维度标志
//
// SpatiaLite ST_AsBinary() 可能返回带有 ISO SQL/MM 标志的 WKB
// 格式: [字节序 1字节][类型码 4字节][坐标数据...]
// ISO WKB 类型码: 基本类型 + 1000 (Z), + 2000 (M), + 3000 (ZM)
// 或位标志: 0x80000000 (Z), 0x40000000 (M), 0x20000000 (SRID)
//
// 标准几何类型: 1=Point, 2=LineString, 3=Polygon, 4=MultiPoint,
//              5=MultiLineString, 6=MultiPolygon, 7=GeometryCollection
func cleanISOWKBFlags(data []byte) ([]byte, error) {
	byteOrder := data[0]
	var geomType uint32

	// 解析几何类型码（考虑字节序）
	if byteOrder == 0x00 { // 大端 (Big Endian)
		geomType = uint32(data[1])<<24 | uint32(data[2])<<16 | uint32(data[3])<<8 | uint32(data[4])
	} else { // 小端 (Little Endian)
		geomType = uint32(data[1]) | uint32(data[2])<<8 | uint32(data[3])<<16 | uint32(data[4])<<24
	}

	// 检查是否包含 ISO 维度标志 (值 > 7 表示可能是 ISO WKB)
	if geomType <= 7 {
		// 已经是标准类型码，无需清理
		return data, nil
	}

	var baseType uint32

	// 尝试 ISO 十进制模式 (类型 + 1000/2000/3000)
	if geomType >= 1000 && geomType < 4000 {
		baseType = geomType % 1000
	} else {
		// 尝试位标志模式 (0x80000000=Z, 0x40000000=M, 0x20000000=SRID)
		baseType = geomType & 0x000000FF // 保留低8位
	}

	// 验证基本类型是否合法 (1-7)
	if baseType < 1 || baseType > 7 {
		// 无法识别的类型码，保持原样
		return data, nil
	}

	// 创建新的 WKB 数据,替换类型码
	result := make([]byte, len(data))
	copy(result, data)

	// 根据字节序写入标准类型码
	if byteOrder == 0x00 { // 大端
		result[1] = 0
		result[2] = 0
		result[3] = 0
		result[4] = byte(baseType)
	} else { // 小端
		result[1] = byte(baseType)
		result[2] = 0
		result[3] = 0
		result[4] = 0
	}

	return result, nil
}

// FixInvalidRings 修复WKB中只有3个点的polygon环
// 对于只有3个点的环，复制第一个点作为第四个点来形成闭环
// 这样可以让SpatiaLite的数据兼容PostGIS的严格要求（环至少4个点）
func FixInvalidRings(data []byte) ([]byte, error) {
	if len(data) < 9 {
		// WKB太短，直接返回
		return data, nil
	}

	byteOrder := data[0]
	var order binary.ByteOrder
	if byteOrder == 0x00 { // Big Endian
		order = binary.BigEndian
	} else { // Little Endian
		order = binary.LittleEndian
	}

	geomType := order.Uint32(data[1:5])

	// 处理ISO WKB标志，获取基本类型
	baseType := geomType
	if geomType >= 1000 && geomType < 4000 {
		baseType = geomType % 1000
	} else if geomType > 7 {
		baseType = geomType & 0xFF
	}

	// 只处理Polygon(3)和MultiPolygon(6)
	if baseType != 3 && baseType != 6 {
		return data, nil
	}

	if baseType == 3 {
		// 单个Polygon
		return fixPolygon(data, order)
	} else {
		// MultiPolygon
		return fixMultiPolygon(data, order)
	}
}

// fixPolygon 修复单个Polygon的环
func fixPolygon(data []byte, order binary.ByteOrder) ([]byte, error) {
	if len(data) < 9 {
		return data, nil
	}

	// data[0] = byteOrder, data[1:5] = geomType
	offset := 5

	if offset+4 > len(data) {
		return data, nil
	}

	numRings := order.Uint32(data[offset : offset+4])
	offset += 4

	var result []byte
	result = append(result, data[:offset]...) // 复制header和numRings

	hasInvalidRing := false

	for i := uint32(0); i < numRings; i++ {
		if offset+4 > len(data) {
			// 数据不足，复制剩余部分
			result = append(result, data[offset:]...)
			break
		}

		numPoints := order.Uint32(data[offset : offset+4])
		pointsStart := offset + 4
		pointsSize := int(numPoints) * 16 // 每个点16字节 (2个double)
		pointsEnd := pointsStart + pointsSize

		if pointsEnd > len(data) {
			// 数据不足，复制剩余部分
			result = append(result, data[offset:]...)
			break
		}

		if numPoints == 3 {
			// 只有3个点，需要修复
			hasInvalidRing = true

			// 写入新的点数：4
			newNumPoints := make([]byte, 4)
			order.PutUint32(newNumPoints, 4)
			result = append(result, newNumPoints...)

			// 复制原来的3个点
			result = append(result, data[pointsStart:pointsEnd]...)

			// 复制第一个点作为第四个点（闭合环）
			firstPoint := data[pointsStart : pointsStart+16]
			result = append(result, firstPoint...)

			log.Printf("⚠️  修复了一个只有3个点的环，复制第一个点作为第四个点以形成闭环")
		} else {
			// 点数正常，直接复制
			result = append(result, data[offset:pointsEnd]...)
		}

		offset = pointsEnd
	}

	// 复制剩余数据（如果有）
	if offset < len(data) {
		result = append(result, data[offset:]...)
	}

	if hasInvalidRing {
		log.Printf("✓ Polygon修复完成，新WKB长度: %d bytes (原长度: %d bytes)", len(result), len(data))
	}

	return result, nil
}

// fixMultiPolygon 修复MultiPolygon中的所有Polygon
func fixMultiPolygon(data []byte, order binary.ByteOrder) ([]byte, error) {
	if len(data) < 9 {
		return data, nil
	}

	// data[0] = byteOrder, data[1:5] = geomType
	offset := 5

	if offset+4 > len(data) {
		return data, nil
	}

	numPolygons := order.Uint32(data[offset : offset+4])
	offset += 4

	var result []byte
	result = append(result, data[:offset]...) // 复制header和numPolygons

	totalInvalidRings := 0

	for p := uint32(0); p < numPolygons; p++ {
		if offset+5 > len(data) {
			// 数据不足，复制剩余部分
			result = append(result, data[offset:]...)
			break
		}

		// 每个polygon有自己的byteOrder和geomType
		polyByteOrder := data[offset]
		var polyOrder binary.ByteOrder
		if polyByteOrder == 0x00 {
			polyOrder = binary.BigEndian
		} else {
			polyOrder = binary.LittleEndian
		}

		// 复制polygon的byteOrder和geomType
		result = append(result, data[offset:offset+5]...)
		offset += 5

		if offset+4 > len(data) {
			result = append(result, data[offset:]...)
			break
		}

		numRings := polyOrder.Uint32(data[offset : offset+4])
		offset += 4

		// 写入numRings
		result = append(result, data[offset-4:offset]...)

		for r := uint32(0); r < numRings; r++ {
			if offset+4 > len(data) {
				result = append(result, data[offset:]...)
				break
			}

			numPoints := polyOrder.Uint32(data[offset : offset+4])
			pointsStart := offset + 4
			pointsSize := int(numPoints) * 16
			pointsEnd := pointsStart + pointsSize

			if pointsEnd > len(data) {
				result = append(result, data[offset:]...)
				break
			}

			if numPoints == 3 {
				// 只有3个点，需要修复
				totalInvalidRings++

				// 写入新的点数：4
				newNumPoints := make([]byte, 4)
				polyOrder.PutUint32(newNumPoints, 4)
				result = append(result, newNumPoints...)

				// 复制原来的3个点
				result = append(result, data[pointsStart:pointsEnd]...)

				// 复制第一个点作为第四个点
				firstPoint := data[pointsStart : pointsStart+16]
				result = append(result, firstPoint...)
			} else {
				// 点数正常，直接复制
				result = append(result, data[offset:pointsEnd]...)
			}

			offset = pointsEnd
		}
	}

	// 复制剩余数据
	if offset < len(data) {
		result = append(result, data[offset:]...)
	}

	if totalInvalidRings > 0 {
		log.Printf("✓ MultiPolygon修复完成，共修复 %d 个无效环，新WKB长度: %d bytes (原长度: %d bytes)",
			totalInvalidRings, len(result), len(data))
	}

	return result, nil
}
