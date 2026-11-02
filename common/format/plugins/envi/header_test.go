package envi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const basicHeader = "ENVI\nsamples = 4\nlines = 3\nbands = 2\nheader offset = 16\nfile type = ENVI Standard\ndata type = 12\ninterleave = bsq\nbyte order = 1\n"

func TestHeaderPreservesStorageAndSpectralFacts(t *testing.T) {
	for _, interleave := range []string{"bsq", "bil", "bip"} {
		raw := strings.Replace(basicHeader, "interleave = bsq", "interleave = "+interleave, 1) + "band names = {red,\ninfrared}\nwavelength = {600,850}\nwavelength units = Nanometers\nbbl = {1,0}\ndata ignore value = 65535\n"
		header, err := ParseHeader(t.Context(), []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(raw))
		if header.FileBytes != 64 || header.HeaderOffset != 16 || header.Samples != 4 || header.Lines != 3 || header.Bands != 2 || header.DataType != 12 || header.ByteOrder != 1 || header.Interleave != interleave || header.HeaderBytes != int64(len(raw)) || header.SHA256 != hex.EncodeToString(sum[:]) || header.Fields["band names"] != "red,\ninfrared" {
			t.Fatalf("incomplete storage facts: %#v", header)
		}
	}
}

func TestHeaderRejectsUnsupportedOrAmbiguousDeclarations(t *testing.T) {
	cases := map[string]string{
		"integer missing underflow": basicHeader + "data ignore value = -1\n",
		"integer missing overflow":  basicHeader + "data ignore value = 65536\n",
		"integer missing fraction":  basicHeader + "data ignore value = 1.25\n",
		"integer missing NaN":       basicHeader + "data ignore value = NaN\n",
		"hexadecimal spectral":      basicHeader + "wavelength = {0x1p2,850}\n",
		"separated digits":          basicHeader + "data ignore value = 1_000\n",
		"scalar block":              strings.Replace(basicHeader, "samples = 4", "samples = {4}", 1),
		"missing list block":        basicHeader + "band names = red,infrared\n",
		"signature":                 strings.Replace(basicHeader, "ENVI\n", "ENVI unknown\n", 1),
		"missing required":          strings.Replace(basicHeader, "byte order = 1\n", "", 1),
		"duplicate":                 basicHeader + "Samples = 4\n",
		"alias duplicate":           basicHeader + "data_type = 12\n",
		"noncanonical spacing":      strings.Replace(basicHeader, "data type", "data  type", 1),
		"external DEM":              basicHeader + "dem file = ../secret.img\n",
		"unknown field":             basicHeader + "unexplained = value\n",
		"compression":               basicHeader + "file compression = 1\n",
		"classification":            strings.Replace(basicHeader, "ENVI Standard", "ENVI Classification", 1),
		"complex":                   strings.Replace(basicHeader, "data type = 12", "data type = 6", 1),
		"zero dimension":            strings.Replace(basicHeader, "lines = 3", "lines = 0", 1),
		"fractional dimension":      strings.Replace(basicHeader, "lines = 3", "lines = 3.0", 1),
		"negative offset":           strings.Replace(basicHeader, "header offset = 16", "header offset = -1", 1),
		"byte order":                strings.Replace(basicHeader, "byte order = 1", "byte order = 2", 1),
		"trailing statement":        basicHeader + "description = {ok} ignored\n",
		"unterminated":              basicHeader + "description = {missing\n",
		"nested":                    basicHeader + "description = {{nested}}\n",
		"unparsed":                  basicHeader + "bare record\n",
		"band count":                basicHeader + "wavelength = {500}\n",
		"bad band flag":             basicHeader + "bbl = {1,2}\n",
		"bad default band":          basicHeader + "default bands = {3}\n",
		"nonfinite spectral":        basicHeader + "wavelength = {500,NaN}\n",
		"nonfinite missing":         basicHeader + "data ignore value = inf\n",
		"external CRS":              basicHeader + "coordinate system string = {PROJCS[\"x\",EXTENSION[\"PROJ4\",\"+nadgrids=/private/grid\"]]}\n",
		"NUL":                       basicHeader + "\x00",
		"invalid UTF8":              basicHeader + "\xff",
		"oversize":                  basicHeader + strings.Repeat(" ", MaxHeaderBytes),
		"size overflow":             strings.NewReplacer("samples = 4", "samples = 2147483647", "lines = 3", "lines = 2147483647", "bands = 2", "bands = 2147483647").Replace(basicHeader),
		"offset overflow":           strings.Replace(basicHeader, "header offset = 16", "header offset = 9223372036854775807", 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseHeader(t.Context(), []byte(raw)); err == nil {
				t.Fatal("unsupported header accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ParseHeader(ctx, []byte(basicHeader)); err != context.Canceled {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestHeaderNumericWidthsAndCRLF(t *testing.T) {
	for code, bytes := range map[string]int64{"1": 1, "2": 2, "3": 4, "4": 4, "5": 8, "12": 2, "13": 4} {
		raw := strings.Replace(basicHeader, "data type = 12", "data type = "+code, 1)
		header, err := ParseHeader(t.Context(), []byte(strings.ReplaceAll(raw, "\n", "\r\n")))
		if err != nil || header.FileBytes != 16+4*3*2*bytes {
			t.Fatalf("type %s: %#v %v", code, header, err)
		}
	}
}
