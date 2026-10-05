package plugin

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"
)

const EngineCatalogTermKeyspace = "keyspace"

// Key names have one canonical byte-preserving representation, including empty keys.
const MaxKeyNameBytes = 64 << 10

func EncodeKeyName(raw []byte) (string, error) {
	if len(raw) > MaxKeyNameBytes {
		return "", fmt.Errorf("key name exceeds content budget")
	}
	return "k:" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeKeyName(name string) ([]byte, error) {
	if !strings.HasPrefix(name, "k:") || len(name) > 2+(MaxKeyNameBytes*4+2)/3 {
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

type KeyspaceFacts struct {
	Database int `json:"database"`
}

type KeyValueSummary struct {
	Key   string        `json:"key"`
	Name  ByteValue     `json:"name"`
	Facts KeyValueFacts `json:"facts"`
}

type KeyValueDatasetPreview struct {
	Database   int               `json:"database"`
	Keys       []KeyValueSummary `json:"keys"`
	NextCursor string            `json:"next_cursor"`
	Complete   bool              `json:"complete"`
}

type KeyValueReadOptions struct {
	Key        string
	Cursor     string
	MaxEntries int
	MaxBytes   int
}

type KeyValueReadableProvider interface {
	StoreProvider
	ListKeyValues(context.Context, ConnectionInfo, EngineCatalogPath, KeyValueReadOptions) (*KeyValueDatasetPreview, error)
	ReadKeyValue(context.Context, ConnectionInfo, EngineCatalogPath, KeyValueReadOptions) (*KeyValuePreview, error)
}
