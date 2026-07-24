package format

import (
	"bytes"
	"encoding/hex"
	"strings"
)

func needMagicValidation(format FormatType) bool {
	switch format {
	case FormatPDF, FormatSQLite, FormatUDBX, FormatGeoPackage, FormatJPEG, FormatPNG, FormatGIF, FormatGLB, FormatLAS, FormatLAZ, FormatCOPC, FormatE57, FormatDWG, FormatDXF, FormatPMTiles:
		return true
	default:
		return false
	}
}

func validateMagicBytes(format FormatType, peek []byte) bool {
	if len(peek) == 0 {
		return true
	}
	if format == FormatDWG {
		return isDWGHeader(peek)
	}
	if format == FormatDXF {
		return isDXFHeader(peek)
	}
	if descriptor, ok := GetFormatDescriptor(format); ok && len(descriptor.Identification.ContentSignatures) > 0 {
		for _, signature := range descriptor.Identification.ContentSignatures {
			if contentSignatureMatches(signature, peek) {
				return true
			}
		}
		return false
	}
	magic := getMagicBytes(format)
	return len(magic) == 0 || len(peek) < len(magic) || bytes.HasPrefix(peek, magic)
}

func getMagicBytes(format FormatType) []byte {
	switch format {
	case FormatPDF:
		return []byte("%PDF")
	case FormatSQLite, FormatUDBX, FormatGeoPackage:
		return []byte("SQLite format 3")
	case FormatJPEG:
		return []byte{0xFF, 0xD8, 0xFF}
	case FormatPNG:
		return []byte{0x89, 0x50, 0x4E, 0x47}
	case FormatGIF:
		return []byte("GIF89a")
	case FormatGLB:
		return []byte("glTF")
	case FormatLAS, FormatLAZ, FormatCOPC:
		return []byte("LASF")
	case FormatE57:
		return []byte("ASTM-E57")
	default:
		return nil
	}
}

func detectByMagic(peek []byte) FormatType {
	if isDWGHeader(peek) {
		return FormatDWG
	}
	if isDXFHeader(peek) {
		return FormatDXF
	}
	lowerPeek := bytes.ToLower(bytes.TrimSpace(peek))
	if bytes.HasPrefix(lowerPeek, []byte("<svg")) ||
		(bytes.HasPrefix(lowerPeek, []byte("<?xml")) && bytes.Contains(lowerPeek[:minInt(len(lowerPeek), 4096)], []byte("<svg"))) {
		return FormatSVG
	}

	if len(peek) >= 12 && bytes.HasPrefix(peek, []byte("RIFF")) {
		switch string(peek[8:12]) {
		case "WEBP":
			return FormatWebP
		case "WAVE":
			return FormatWAV
		case "AVI ":
			return FormatAVI
		}
	}
	if len(peek) >= 12 && string(peek[4:8]) == "ftyp" {
		brand := strings.ToLower(string(peek[8:minInt(len(peek), 12)]))
		switch brand {
		case "avif":
			return FormatAVIF
		case "heic", "heix", "hevc", "hevx", "mif1", "msf1":
			return FormatHEIC
		case "qt  ":
			return FormatMOV
		default:
			return FormatMP4
		}
	}

	magicMap := map[string]FormatType{
		"%PDF":             FormatPDF,
		"SQLite format 3":  FormatSQLite,
		"\xFF\xD8\xFF":     FormatJPEG,
		"\x89PNG":          FormatPNG,
		"GIF89a":           FormatGIF,
		"GIF87a":           FormatGIF,
		"BM":               FormatBMP,
		"ID3":              FormatMP3,
		"OggS":             FormatOGG,
		"fLaC":             FormatFLAC,
		"glTF":             FormatGLB,
		"LASF":             FormatLAS,
		"\x1A\x45\xDF\xA3": FormatMKV,
		"PK\x03\x04":       FormatUnknown,
		"0000":             FormatShapefile,
		"9994":             FormatShapefile,
	}
	for magic, format := range magicMap {
		if bytes.HasPrefix(peek, []byte(magic)) {
			return format
		}
	}
	return FormatUnknown
}

func isDWGHeader(peek []byte) bool {
	return len(peek) >= 6 && bytes.HasPrefix(peek, []byte("AC10")) &&
		peek[4] >= '0' && peek[4] <= '9' && peek[5] >= '0' && peek[5] <= '9'
}

func isDXFHeader(peek []byte) bool {
	if bytes.HasPrefix(peek, []byte("AutoCAD Binary DXF\r\n\x1a\x00")) {
		return true
	}
	normalized := bytes.ReplaceAll(bytes.TrimSpace(bytes.TrimPrefix(peek, []byte{0xEF, 0xBB, 0xBF})), []byte("\r\n"), []byte("\n"))
	lines := bytes.Split(normalized, []byte("\n"))
	return len(lines) >= 2 && string(bytes.TrimSpace(lines[0])) == "0" && strings.EqualFold(string(bytes.TrimSpace(lines[1])), "SECTION")
}

func detectByDescriptorSignature(peek []byte) FormatType {
	for _, descriptor := range ListFormatDescriptors() {
		for _, signature := range descriptor.Identification.ContentSignatures {
			if contentSignatureMatches(signature, peek) {
				return descriptor.Format
			}
		}
	}
	return FormatUnknown
}

func detectByPluginSniffer(peek []byte) FormatType {
	for _, formatType := range ListFormatPluginFormats() {
		plugin, err := GetFormatPlugin(formatType)
		if err != nil {
			continue
		}
		sniffer, ok := plugin.(ContentSniffer)
		if !ok || !sniffer.SniffFormat(peek) {
			continue
		}
		return formatType
	}
	return FormatUnknown
}

func contentSignatureMatches(signature string, peek []byte) bool {
	signature = strings.ToLower(strings.TrimSpace(signature))
	if signature == "" {
		return false
	}
	if hexValue, ok := strings.CutPrefix(signature, "hex:"); ok {
		magic, err := hex.DecodeString(strings.TrimSpace(hexValue))
		return err == nil && len(peek) >= len(magic) && bytes.HasPrefix(peek, magic)
	}
	return bytes.HasPrefix(bytes.ToLower(peek), []byte(signature))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
