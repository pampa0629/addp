package shapefile

import (
	"os"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

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
	data, err := os.ReadFile(basePath + ".cpg")
	if err != nil {
		return ""
	}
	return NormalizeDBFEncoding(strings.TrimSpace(string(data)))
}
