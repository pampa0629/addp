// Package envi owns the bounded ENVI Standard header and composition profile.
// Pixel decoding belongs exclusively to the GeoPython GDAL runtime.
package envi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	Profile         = "envi-standard-pair/v1"
	MaxHeaderBytes  = 1 << 20
	MaxRasterBytes  = 512 << 20
	maxHeaderFields = 64
)

var headerNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// Header contains physical storage facts, not permission or decoded pixels.
type Header struct {
	Samples, Lines, Bands   int64
	DataType, ByteOrder     int
	HeaderOffset, FileBytes int64
	Interleave              string
	SHA256                  string
	HeaderBytes             int64
	Fields                  map[string]string
}

// ParseHeader consumes the entire header. Unknown fields are unsupported,
// rather than silently assumed to contain no additional resource semantics.
func ParseHeader(ctx context.Context, raw []byte) (*Header, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > MaxHeaderBytes || !utf8.Valid(raw) || bytes.ContainsRune(raw, 0) {
		return nil, fmt.Errorf("ENVI header exceeds the supported text profile")
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	if lines[0] != "ENVI" {
		return nil, fmt.Errorf("ENVI header signature is missing")
	}
	fields := map[string]string{}
	for index := 1; index < len(lines); index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := strings.TrimSpace(lines[index])
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("unparsed ENVI header record")
		}
		key = strings.ToLower(strings.TrimSpace(key))
		key = strings.ReplaceAll(key, "_", " ")
		if key != strings.Join(strings.Fields(key), " ") || !supportedField(key) {
			return nil, fmt.Errorf("unsupported ENVI header field %q", key)
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("duplicate ENVI header field %q", key)
		}
		if len(fields) >= maxHeaderFields {
			return nil, fmt.Errorf("ENVI header field budget exceeded")
		}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "{") {
			if !blockField(key) {
				return nil, fmt.Errorf("unexpected ENVI scalar block")
			}
			var block strings.Builder
			block.WriteString(value)
			for !strings.Contains(value, "}") && index+1 < len(lines) {
				index++
				value = lines[index]
				block.WriteByte('\n')
				block.WriteString(value)
			}
			value = block.String()
			if !strings.HasSuffix(strings.TrimSpace(value), "}") || strings.Count(value, "{") != 1 || strings.Count(value, "}") != 1 {
				return nil, fmt.Errorf("unsupported ENVI header block")
			}
			value = strings.TrimSpace(strings.TrimSpace(value)[1 : len(strings.TrimSpace(value))-1])
		} else if strings.ContainsAny(value, "{}") {
			return nil, fmt.Errorf("unparsed ENVI header block")
		} else if blockField(key) && key != "description" {
			return nil, fmt.Errorf("missing ENVI list block")
		}
		if value == "" {
			return nil, fmt.Errorf("empty ENVI header field %q", key)
		}
		fields[key] = value
	}
	for _, key := range []string{"samples", "lines", "bands", "header offset", "file type", "data type", "interleave", "byte order"} {
		if _, exists := fields[key]; !exists {
			return nil, fmt.Errorf("missing ENVI header field %q", key)
		}
	}
	if fields["file type"] != "ENVI Standard" {
		return nil, fmt.Errorf("unsupported ENVI file type")
	}
	if fields["interleave"] != "bsq" && fields["interleave"] != "bil" && fields["interleave"] != "bip" {
		return nil, fmt.Errorf("unsupported ENVI interleave")
	}
	values := map[string]int64{}
	for _, key := range []string{"samples", "lines", "bands", "header offset", "data type", "byte order"} {
		text := fields[key]
		if len(text) > 19 || strings.IndexFunc(text, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return nil, fmt.Errorf("invalid ENVI integer field %q", key)
		}
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ENVI integer field %q", key)
		}
		values[key] = number
	}
	if values["samples"] <= 0 || values["samples"] > math.MaxInt32 || values["lines"] <= 0 || values["lines"] > math.MaxInt32 || values["bands"] <= 0 || values["bands"] > math.MaxInt32 || values["byte order"] > 1 {
		return nil, fmt.Errorf("ENVI dimensions or byte order outside profile")
	}
	bytesPerSample := map[int64]int64{1: 1, 2: 2, 3: 4, 4: 4, 5: 8, 12: 2, 13: 4}[values["data type"]]
	if bytesPerSample == 0 {
		return nil, fmt.Errorf("unsupported ENVI numeric type")
	}
	size := bytesPerSample
	for _, key := range []string{"samples", "lines", "bands"} {
		if size > math.MaxInt64/values[key] {
			return nil, fmt.Errorf("ENVI data size overflow")
		}
		size *= values[key]
	}
	if values["header offset"] > math.MaxInt64-size {
		return nil, fmt.Errorf("ENVI data offset overflow")
	}
	if size+values["header offset"] > MaxRasterBytes {
		return nil, fmt.Errorf("ENVI raster exceeds the first-phase byte budget")
	}
	if err := validateOptionalFields(fields, values["bands"], values["data type"]); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	return &Header{Samples: values["samples"], Lines: values["lines"], Bands: values["bands"], DataType: int(values["data type"]), ByteOrder: int(values["byte order"]), HeaderOffset: values["header offset"], FileBytes: size + values["header offset"], Interleave: fields["interleave"], SHA256: hex.EncodeToString(digest[:]), HeaderBytes: int64(len(raw)), Fields: fields}, nil
}

func blockField(key string) bool {
	switch key {
	case "description", "map info", "coordinate system string", "band names", "wavelength", "fwhm", "bbl", "default bands":
		return true
	}
	return false
}

func supportedField(key string) bool {
	switch key {
	case "samples", "lines", "bands", "header offset", "file type", "data type", "interleave", "byte order",
		"description", "map info", "coordinate system string", "data ignore value", "band names", "sensor type", "acquisition time",
		"wavelength", "wavelength units", "fwhm", "bbl", "default bands":
		return true
	}
	return false
}

func validateOptionalFields(fields map[string]string, bands, dataType int64) error {
	for _, key := range []string{"band names", "wavelength", "fwhm", "bbl"} {
		value, exists := fields[key]
		if !exists {
			continue
		}
		parts := strings.Split(value, ",")
		if int64(len(parts)) != bands {
			return fmt.Errorf("ENVI %s count does not match bands", key)
		}
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				return fmt.Errorf("empty ENVI band metadata")
			}
			if key == "band names" {
				continue
			}
			number, err := parseHeaderNumber(part, false)
			if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || (key == "bbl" && part != "0" && part != "1") {
				return fmt.Errorf("invalid ENVI band metadata")
			}
		}
	}
	if value, exists := fields["default bands"]; exists {
		parts := strings.Split(value, ",")
		if len(parts) != 1 && len(parts) != 3 {
			return fmt.Errorf("unsupported ENVI default bands")
		}
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if len(part) > 20 {
				return fmt.Errorf("ENVI default band is out of range")
			}
			n, err := strconv.ParseInt(part, 10, 64)
			if err != nil || n <= 0 || n > bands {
				return fmt.Errorf("ENVI default band is out of range")
			}
		}
	}
	if value, exists := fields["data ignore value"]; exists {
		n, err := parseHeaderNumber(value, true)
		if err != nil || math.IsInf(n, 0) {
			return fmt.Errorf("unsupported ENVI missing value")
		}
		if bounds, integer := map[int64][2]float64{1: {0, 255}, 2: {-32768, 32767}, 3: {-2147483648, 2147483647}, 12: {0, 65535}, 13: {0, 4294967295}}[dataType]; integer {
			if math.IsNaN(n) || n != math.Trunc(n) || n < bounds[0] || n > bounds[1] {
				return fmt.Errorf("ENVI missing value is not representable by the declared integer type")
			}
		} else if dataType == 4 && !math.IsNaN(n) && (math.Abs(n) > math.MaxFloat32 || (n != 0 && float32(n) == 0)) {
			return fmt.Errorf("ENVI missing value is not representable by Float32")
		}
	}
	// Ordinary WKT1 declarations are metadata. Resource-bearing and extended
	// CRS grammars are not admitted by this first storage-composition profile.
	if value, exists := fields["coordinate system string"]; exists {
		upper := strings.ToUpper(value)
		if !strings.HasPrefix(upper, "GEOGCS[") && !strings.HasPrefix(upper, "PROJCS[") {
			return fmt.Errorf("unsupported ENVI CRS grammar")
		}
		for _, token := range []string{"EXTENSION", "BOUNDCRS", "PROJ4", "PARAMETERFILE", "VERT_CS", "COMPD_CS"} {
			if strings.Contains(upper, token) {
				return fmt.Errorf("unsupported ENVI CRS resource semantics")
			}
		}
	}
	return nil
}

func parseHeaderNumber(text string, nan bool) (float64, error) {
	if nan && strings.EqualFold(text, "nan") {
		return math.NaN(), nil
	}
	if !headerNumber.MatchString(text) {
		return 0, fmt.Errorf("unsupported ENVI decimal number")
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, err
	}
	mantissa := strings.SplitN(strings.ToLower(text), "e", 2)[0]
	if value == 0 && strings.Trim(mantissa, "0.+-") != "" {
		return 0, fmt.Errorf("ENVI decimal number underflows")
	}
	return value, nil
}
