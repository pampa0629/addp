package authorizationfacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/jsonmap"
)

// ScopeBoundary identifies a whole container. Its children are deliberately
// absent: additions inside the same format-defined root do not change identity.
// It does not authorize arbitrary directory reads or references outside Root.
type ScopeBoundary struct {
	Format      string                   `json:"format"`
	RuleVersion int                      `json:"rule_version"`
	Root        plugin.EngineCatalogPath `json:"root"`
}

func (b *ScopeBoundary) Validate(engineID uint) error {
	if b == nil || b.Format != "filegdb" || b.RuleVersion != 1 || b.Root.EngineID != engineID || !ValidWholeScopeRoot(b.Root) {
		return errors.New("invalid whole container boundary")
	}
	return nil
}

func ValidWholeScopeRoot(root plugin.EngineCatalogPath) bool {
	if len(root.Segments) < 2 || root.Segments[len(root.Segments)-1].Term != plugin.EngineCatalogTermDirectory ||
		root.Segments[len(root.Segments)-1].Kind != plugin.EngineCatalogKindDirectory ||
		!strings.HasSuffix(strings.ToLower(root.Segments[len(root.Segments)-1].Name), ".gdb") {
		return false
	}
	// Reuse the exact filesystem path grammar, without normalizing input.
	leaf := root
	leaf.Segments = append([]plugin.EngineCatalogSegment(nil), root.Segments...)
	leaf.Segments[len(leaf.Segments)-1].Term = plugin.EngineCatalogTermFile
	leaf.Segments[len(leaf.Segments)-1].Kind = plugin.EngineCatalogKindFile
	return validateFileCompositionTarget(leaf) == nil
}

func (b *ScopeBoundary) Clone() *ScopeBoundary {
	if b == nil {
		return nil
	}
	copy := *b
	copy.Root.Segments = append([]plugin.EngineCatalogSegment(nil), b.Root.Segments...)
	return &copy
}

func (b *ScopeBoundary) UnmarshalJSON(raw []byte) error {
	fields, err := jsonmap.DecodeObjectStrict(raw, 16<<10, 64)
	if err != nil || len(fields) != 3 || fields["format"] == nil || fields["rule_version"] == nil || fields["root"] == nil {
		return errors.New("invalid whole container boundary JSON")
	}
	type wire ScopeBoundary
	var value wire
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || (*ScopeBoundary)(&value).Validate(value.Root.EngineID) != nil {
		return errors.New("invalid whole container boundary JSON")
	}
	*b = ScopeBoundary(value)
	return nil
}
