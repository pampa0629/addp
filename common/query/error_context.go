package query

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrorContext is a bounded SQL failure fact, never a database error message.
// SourceDigest and Offset are internal evidence and never cross JSON boundaries.
type ErrorContext struct {
	Object       *ErrorObject    `json:"object,omitempty"`
	Position     *SourcePosition `json:"position,omitempty"`
	SourceDigest string          `json:"-"`
	Offset       *int            `json:"-"`
}

type ErrorObject struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type SourcePosition struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

func SourceDigest(source string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(source))) }

func (c *ErrorContext) Clone() *ErrorContext {
	if c == nil {
		return nil
	}
	copy := *c
	if c.Object != nil {
		value := *c.Object
		copy.Object = &value
	}
	if c.Position != nil {
		value := *c.Position
		copy.Position = &value
	}
	if c.Offset != nil {
		value := *c.Offset
		copy.Offset = &value
	}
	return &copy
}

func (c *ErrorContext) Valid() bool {
	if c == nil || (c.Object == nil && c.Position == nil) {
		return false
	}
	if object := c.Object; object != nil {
		if object.Kind != "table" && object.Kind != "column" && object.Kind != "function" && object.Kind != "operator" {
			return false
		}
		if object.Name == "" || len(object.Name) > 512 || !utf8.ValidString(object.Name) || utf8.RuneCountInString(object.Name) > 256 {
			return false
		}
		for _, r := range object.Name {
			if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
				return false
			}
		}
	}
	if p := c.Position; p != nil && (p.Line < 1 || p.Line > 1_000_000 || p.Column < 1 || p.Column > 1_000_000) {
		return false
	}
	return true
}

// PositionAtByte uses 1-based lines and UTF-16 columns, as Monaco does.
func PositionAtByte(source string, offset int) *SourcePosition {
	if offset < 0 || offset > len(source) || !utf8.ValidString(source[:offset]) {
		return nil
	}
	p := &SourcePosition{Line: 1, Column: 1}
	for _, r := range source[:offset] {
		if r == '\n' {
			p.Line++
			p.Column = 1
		} else if r > 0xffff {
			p.Column += 2
		} else {
			p.Column++
		}
	}
	return p
}

// ByteOffsetAtCharacter converts PostgreSQL's 1-based Unicode character offset.
func ByteOffsetAtCharacter(source string, position int) (int, bool) {
	if position < 1 {
		return 0, false
	}
	index := 1
	for offset := range source {
		if index == position {
			return offset, true
		}
		index++
	}
	return len(source), index == position
}

func NewErrorContext(source, kind, name string, offset int) *ErrorContext {
	c := &ErrorContext{SourceDigest: SourceDigest(source)}
	if name != "" {
		c.Object = &ErrorObject{Kind: kind, Name: name}
		if !c.Valid() {
			c.Object = nil
		}
	}
	if p := PositionAtByte(source, offset); p != nil {
		c.Position = p
		c.Offset = &offset
	}
	if !c.Valid() {
		return nil
	}
	return c
}

// RemapSource only proves identity or removal of leading/trailing whitespace.
// Parameter and template rewrites without a source map deliberately lose position.
func (c *ErrorContext) RemapSource(source, bound string) *ErrorContext {
	copy := c.Clone()
	if copy == nil {
		return nil
	}
	if copy.Position != nil && copy.SourceDigest == SourceDigest(bound) && copy.Offset != nil && strings.TrimSpace(source) == bound {
		offset := *copy.Offset + len(source) - len(strings.TrimLeftFunc(source, unicode.IsSpace))
		copy.Position = PositionAtByte(source, offset)
		copy.Offset = &offset
	} else if source != bound || copy.SourceDigest != SourceDigest(source) {
		copy.Position, copy.Offset = nil, nil
	}
	copy.SourceDigest = SourceDigest(source)
	if !copy.Valid() {
		return nil
	}
	return copy
}
