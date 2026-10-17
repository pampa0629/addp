package jsonrecords

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// LooksLikeGeoJSONFeatureCollection reports whether the content prefix is a
// strict GeoJSON FeatureCollection root object with a features array.
func LooksLikeGeoJSONFeatureCollection(peek []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(peek))
	dec.UseNumber()

	token, err := dec.Token()
	if err != nil {
		return false
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return false
	}

	var hasFeatureCollectionType bool
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return false
		}
		key, ok := keyToken.(string)
		if !ok {
			return false
		}
		switch key {
		case "type":
			var value string
			if err := dec.Decode(&value); err != nil {
				return false
			}
			hasFeatureCollectionType = strings.EqualFold(strings.TrimSpace(value), "FeatureCollection")
		case "features":
			token, err := dec.Token()
			if err != nil {
				return false
			}
			arrayDelim, ok := token.(json.Delim)
			return ok && arrayDelim == '[' && hasFeatureCollectionType
		default:
			var skip interface{}
			if err := dec.Decode(&skip); err != nil && err != io.EOF {
				return false
			}
		}
	}
	return false
}

// LooksLikeTopology recognizes a root Topology tag and objects member without
// requiring the geometry payload to fit in the detection prefix. Full validity
// (including arcs) belongs to the reader, not format candidate identification.
func LooksLikeTopology(peek []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(peek))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return false
	}
	hasType, hasObjects := false, false
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return false
		}
		switch key {
		case "type":
			var kind string
			if dec.Decode(&kind) != nil || kind != "Topology" {
				return false
			}
			hasType = true
		case "objects":
			start, err := dec.Token()
			if err != nil || start != json.Delim('{') {
				return false
			}
			hasObjects = true
			if hasType {
				return true
			}
			depth := 1
			for depth > 0 {
				token, err := dec.Token()
				if err != nil {
					return false
				}
				if delim, ok := token.(json.Delim); ok {
					if delim == '{' || delim == '[' {
						depth++
					} else {
						depth--
					}
				}
			}
		default:
			var skip json.RawMessage
			if dec.Decode(&skip) != nil {
				return false
			}
		}
		if hasType && hasObjects {
			return true
		}
	}
	return false
}
