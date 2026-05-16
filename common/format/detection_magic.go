package format

import (
	"bytes"
	"strings"
)

func needMagicValidation(format FormatType) bool {
	switch format {
	case FormatPDF, FormatSQLite, FormatGeoPackage, FormatJPEG, FormatPNG, FormatGIF:
		return true
	default:
		return false
	}
}

func validateMagicBytes(format FormatType, peek []byte) bool {
	magic := getMagicBytes(format)
	if len(magic) == 0 || len(peek) < len(magic) {
		return true
	}
	return bytes.HasPrefix(peek, magic)
}

func getMagicBytes(format FormatType) []byte {
	switch format {
	case FormatPDF:
		return []byte("%PDF")
	case FormatSQLite, FormatGeoPackage:
		return []byte("SQLite format 3")
	case FormatJPEG:
		return []byte{0xFF, 0xD8, 0xFF}
	case FormatPNG:
		return []byte{0x89, 0x50, 0x4E, 0x47}
	case FormatGIF:
		return []byte("GIF89a")
	default:
		return nil
	}
}

func detectByMagic(peek []byte) FormatType {
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
