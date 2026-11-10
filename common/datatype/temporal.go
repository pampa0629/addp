package datatype

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// TemporalInfo describes the native integer representation of a time or timestamp.
type TemporalInfo struct {
	Unit     string `json:"unit"`
	Timezone string `json:"timezone,omitempty"`
}

func (t TemporalInfo) TicksPerSecond() int64 {
	switch t.Unit {
	case "s":
		return 1
	case "ms":
		return 1_000
	case "us":
		return 1_000_000
	case "ns":
		return 1_000_000_000
	default:
		return 0
	}
}

func (t TemporalInfo) ValidFor(kind FieldType) bool {
	if t.TicksPerSecond() == 0 {
		return false
	}
	switch kind {
	case FieldTypeTime:
		return t.Timezone == ""
	case FieldTypeTimestamp:
		// Zone identifiers are retained verbatim. The ticks already describe an instant,
		// so decoding does not depend on the server's timezone database.
		return len(t.Timezone) <= 255 && utf8.ValidString(t.Timezone) &&
			!strings.ContainsAny(t.Timezone, "[],") &&
			strings.IndexFunc(t.Timezone, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) < 0
	default:
		return false
	}
}

func (t TemporalInfo) ValidTicks(kind FieldType, ticks int64) bool {
	return kind != FieldTypeTime || (ticks >= 0 && ticks < 86400*t.TicksPerSecond())
}

func temporalJSONText(field FieldInfo, value interface{}) (string, error) {
	ticks, ok := value.(int64)
	t := *field.Temporal
	if !ok || !t.ValidFor(field.Type) || !t.ValidTicks(field.Type, ticks) {
		return "", fmt.Errorf("invalid native temporal value")
	}
	rate := t.TicksPerSecond()
	seconds, fraction := ticks/rate, ticks%rate
	if fraction < 0 {
		seconds--
		fraction += rate
	}
	layout := "15:04:05"
	if field.Type == FieldTypeTimestamp {
		if seconds < -62135596800 || seconds >= 253402300800 {
			return "", fmt.Errorf("timestamp is outside the supported preview calendar")
		}
		layout = "2006-01-02T" + layout
	}
	switch t.Unit {
	case "ms":
		layout += ".000"
	case "us":
		layout += ".000000"
	case "ns":
		layout += ".000000000"
	}
	if t.Timezone != "" {
		layout += "Z"
	}
	return time.Unix(seconds, fraction*(1_000_000_000/rate)).UTC().Format(layout), nil
}
