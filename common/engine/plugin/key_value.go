package plugin

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// KeyDisplayName derives a display-only label; the canonical token remains identity.
func KeyDisplayName(name string) string {
	raw, err := DecodeKeyName(name)
	if err != nil {
		return name
	}
	value := NewByteValue(string(raw))
	if value.Encoding == "base64" {
		return "base64:" + value.Value
	}
	return strconv.QuoteToGraphic(value.Value)
}

const EngineCatalogTermKey = "key"

// Key names have one canonical byte-preserving representation, including empty keys.
const MaxKeyNameBytes = 189

func EncodeKeyName(raw []byte) (string, error) {
	if len(raw) > MaxKeyNameBytes {
		return "", fmt.Errorf("key name exceeds catalog budget")
	}
	return "k:" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeKeyName(name string) ([]byte, error) {
	if !strings.HasPrefix(name, "k:") || len(name) > 254 {
		return nil, fmt.Errorf("canonical key name required")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(name[2:])
	if err != nil || len(raw) > MaxKeyNameBytes {
		return nil, fmt.Errorf("invalid canonical key name")
	}
	canonical, _ := EncodeKeyName(raw)
	if canonical != name {
		return nil, fmt.Errorf("invalid canonical key name")
	}
	return raw, nil
}

// KeyValueFacts are live engine facts, not a platform data type or table schema.
type KeyValueFacts struct {
	NativeType string `json:"native_type"`
	Length     int64  `json:"length"`
	TTLMillis  int64  `json:"ttl_millis"`
}

type ByteValue struct {
	Encoding   string `json:"encoding"`
	Value      string `json:"value"`
	ByteLength int    `json:"byte_length"`
}

func NewByteValue(raw string) ByteValue {
	if utf8.ValidString(raw) && !strings.ContainsRune(raw, 0) {
		return ByteValue{Encoding: "utf8", Value: raw, ByteLength: len(raw)}
	}
	return ByteValue{Encoding: "base64", Value: base64.StdEncoding.EncodeToString([]byte(raw)), ByteLength: len(raw)}
}

type KeyValueField struct {
	Name  ByteValue `json:"name"`
	Value ByteValue `json:"value"`
}

type KeyValueEntry struct {
	Index  *int64          `json:"index,omitempty"`
	Field  *ByteValue      `json:"field,omitempty"`
	Value  *ByteValue      `json:"value,omitempty"`
	Score  string          `json:"score,omitempty"`
	ID     string          `json:"id,omitempty"`
	Fields []KeyValueField `json:"fields,omitempty"`
}

type KeyValuePreview struct {
	Facts     KeyValueFacts   `json:"facts"`
	Value     *ByteValue      `json:"value,omitempty"`
	Entries   []KeyValueEntry `json:"entries"`
	Truncated bool            `json:"truncated"`
}

type KeyValueReadOptions struct {
	MaxEntries int
	MaxBytes   int
}

type KeyValueReadableProvider interface {
	StoreProvider
	ReadKeyValue(context.Context, ConnectionInfo, EngineCatalogPath, KeyValueReadOptions) (*KeyValuePreview, error)
}
