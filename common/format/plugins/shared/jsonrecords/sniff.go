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
