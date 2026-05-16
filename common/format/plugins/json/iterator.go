package jsonformat

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type iterator struct {
	decoder         *json.Decoder
	meta            Metadata
	structure       string
	dataStartOffset int64
}

func newRecordIterator(r io.Reader) (*iterator, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()

	token, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("json table: failed to read root token: %w", err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil, fmt.Errorf("json table: expected object or array start")
	}
	if delim == '[' {
		return &iterator{
			decoder:         dec,
			meta:            Metadata{},
			structure:       StructureObjectArray,
			dataStartOffset: dec.InputOffset(),
		}, nil
	}
	if delim != '{' {
		return nil, fmt.Errorf("json table: expected object or array start")
	}

	it := &iterator{
		decoder:   dec,
		meta:      Metadata{},
		structure: StructureGeoJSONFeatureSet,
	}

	var collectionType string
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("geojson: failed to read key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("geojson: invalid key token %v", keyToken)
		}

		switch key {
		case "type":
			valTok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("geojson: failed to read type: %w", err)
			}
			if typeStr, ok := valTok.(string); ok {
				collectionType = typeStr
			}
		case "bbox":
			var bbox []float64
			if err := dec.Decode(&bbox); err != nil {
				return nil, fmt.Errorf("geojson: failed to decode bbox: %w", err)
			}
			it.meta.BoundingBox = bbox
		case "crs":
			var crs rawCRS
			if err := dec.Decode(&crs); err != nil {
				return nil, fmt.Errorf("geojson: failed to decode crs: %w", err)
			}
			if name := crs.Name(); name != "" {
				it.meta.CoordinateSystem = name
			}
		case "features":
			arrayTok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("geojson: failed to read features token: %w", err)
			}
			arrayDelim, ok := arrayTok.(json.Delim)
			if !ok || arrayDelim != '[' {
				return nil, fmt.Errorf("geojson: expected features array")
			}

			if collectionType != "" && !strings.EqualFold(collectionType, "FeatureCollection") {
				return nil, fmt.Errorf("geojson: unsupported type %q", collectionType)
			}

			it.dataStartOffset = dec.InputOffset()
			return it, nil
		default:
			var skip interface{}
			if err := dec.Decode(&skip); err != nil {
				return nil, fmt.Errorf("geojson: failed to skip key %q: %w", key, err)
			}
		}
	}

	return nil, fmt.Errorf("json table: features array not found")
}

// Next 读取下一条 Feature。
func (it *iterator) Next() (*Feature, error) {
	if !it.decoder.More() {
		if err := it.finishArray(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}

	var raw map[string]interface{}
	if err := it.decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("json table: failed to decode record: %w", err)
	}

	if it.structure == StructureObjectArray {
		return featureFromObjectRecord(raw), nil
	}

	feature := featureFromGeoJSONFeature(raw)
	if feature == nil {
		return nil, fmt.Errorf("geojson: invalid feature record")
	}

	return feature, nil
}

func (it *iterator) finishArray() error {
	token, err := it.decoder.Token()
	if err != nil {
		return fmt.Errorf("geojson: failed to close features array: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != ']' {
		return fmt.Errorf("geojson: expected closing features array, got %v", token)
	}

	for {
		token, err := it.decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("geojson: failed to finish object: %w", err)
		}
		if delim, ok := token.(json.Delim); ok && delim == '}' {
			return nil
		}

		if key, ok := token.(string); ok {
			var skip interface{}
			if err := it.decoder.Decode(&skip); err != nil {
				return fmt.Errorf("geojson: failed to skip trailing key %q: %w", key, err)
			}
		}
	}
}

// Iterator 为外部提供流式读取能力。
type Iterator struct {
	inner *iterator
}

// NewFeatureIterator 创建新的 Feature 迭代器。
func NewFeatureIterator(r io.Reader) (*Iterator, error) {
	it, err := newRecordIterator(r)
	if err != nil {
		return nil, err
	}
	return &Iterator{inner: it}, nil
}

// Next 返回下一条 Feature（到末尾返回 io.EOF）。
func (i *Iterator) Next() (*Feature, error) {
	if i == nil || i.inner == nil {
		return nil, io.EOF
	}
	return i.inner.Next()
}

// Metadata 返回解析过程中收集的元信息。
func (i *Iterator) Metadata() Metadata {
	if i == nil || i.inner == nil {
		return Metadata{}
	}
	return i.inner.meta
}

// FeatureCollection 表示完整的 FeatureCollection 数据。
type FeatureCollection struct {
	Type string
	Metadata
	Features []Feature
}

// LoadFeatureCollection 读取并返回完整的 FeatureCollection。
func LoadFeatureCollection(r io.Reader) (*FeatureCollection, error) {
	it, err := newRecordIterator(r)
	if err != nil {
		return nil, err
	}

	collection := &FeatureCollection{
		Type:     "FeatureCollection",
		Metadata: it.meta,
		Features: make([]Feature, 0),
	}

	for {
		feature, err := it.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		collection.Features = append(collection.Features, *feature)
	}

	return collection, nil
}
