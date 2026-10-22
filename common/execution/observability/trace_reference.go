// Package observability contains only bounded execution correlation values.
// It has no execution, authorization, HTTP, database or SDK dependency.
package observability

import (
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/addp/common/jsonmap"
)

const Schema = "addp.execution-trace/v1"
const MaxReferenceBytes = 2048

var ErrReference = errors.New("execution_trace_reference_invalid")
var moduleName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,49}$`)
var instanceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`)

// TraceReference is correlation, never an execution identity or authorization.
type TraceReference struct {
	TraceID    string    `json:"trace_id"`
	SpanID     string    `json:"span_id"`
	ModuleName string    `json:"module_name"`
	Role       string    `json:"role"`
	InstanceID string    `json:"instance_id"`
	StartedAt  time.Time `json:"started_at"`
}

func validHex(value string, size int) bool {
	if len(value) != size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return false
	}
	for _, v := range decoded {
		if v != 0 {
			return true
		}
	}
	return false
}

func (r TraceReference) Validate() error {
	_, offset := r.StartedAt.Zone()
	if !validHex(r.TraceID, 16) || !validHex(r.SpanID, 8) || !moduleName.MatchString(r.ModuleName) ||
		(r.Role != "backend" && r.Role != "ingress") || !instanceName.MatchString(r.InstanceID) ||
		r.StartedAt.IsZero() || offset != 0 || r.StartedAt.Year() < 0 || r.StartedAt.Year() > 9999 {
		return ErrReference
	}
	return nil
}

// ParseTraceReference accepts exactly six canonical string fields. Duplicate
// members and invalid UTF-8 are rejected before any typed projection.
func ParseTraceReference(raw []byte) (*TraceReference, error) {
	value, err := jsonmap.DecodeObjectStrict(raw, MaxReferenceBytes, 2)
	if err != nil || len(value) != 6 {
		return nil, ErrReference
	}
	fields := []string{"trace_id", "span_id", "module_name", "role", "instance_id", "started_at"}
	values := make([]string, len(fields))
	for i, key := range fields {
		var ok bool
		values[i], ok = value[key].(string)
		if !ok {
			return nil, ErrReference
		}
	}
	at, err := time.Parse(time.RFC3339Nano, values[5])
	if err != nil || at.UTC().Format(time.RFC3339Nano) != values[5] {
		return nil, ErrReference
	}
	result := &TraceReference{values[0], values[1], values[2], values[3], values[4], at.UTC()}
	if result.Validate() != nil {
		return nil, ErrReference
	}
	return result, nil
}
