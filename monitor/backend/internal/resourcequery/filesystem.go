package resourcequery

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Dimensions is a closed observation selector, never a physical volume identity.
type Dimensions map[string]string

func (d Definition) Filesystem() bool { return strings.HasPrefix(d.Key, "node.filesystem.") }
func (d Dimensions) Validate() error {
	if len(d) == 0 {
		return nil
	}
	if len(d) != 3 {
		return ErrInvalid
	}
	for _, key := range []string{"device", "mountpoint", "fstype"} {
		value := d[key]
		if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return ErrInvalid
		}
	}
	if !strings.HasPrefix(d["mountpoint"], "/") {
		return ErrInvalid
	}
	return nil
}
func (d Dimensions) Copy() Dimensions {
	out := Dimensions{}
	for k, v := range d {
		out[k] = v
	}
	return out
}
func (d Dimensions) identity() string {
	value, _ := json.Marshal(d)
	return string(value)
}

func filesystemExpression(s Scope, key string, dimensions Dimensions) (value, stamp, presence string) {
	group := "(device,mountpoint,fstype)"
	selector := func(name string) string {
		raw := s.selector(name)
		for _, k := range []string{"device", "mountpoint", "fstype"} {
			if len(dimensions) > 0 {
				raw = strings.TrimSuffix(raw, "}") + "," + k + "=" + strconv.Quote(dimensions[k]) + "}"
			}
		}
		return raw
	}
	gauge := func(name string) (string, string) {
		raw := selector(name)
		guard := " and on" + group + " (count by" + group + "(" + raw + ")==1)"
		return "(max by" + group + "(" + raw + ")" + guard + ")", "(max by" + group + "(timestamp(" + raw + "))" + guard + ")"
	}
	deviceError, es := gauge("node_filesystem_device_error")
	presence = es
	if strings.HasPrefix(key, "node.filesystem.inodes_") {
		total, ts := gauge("node_filesystem_files")
		free, fs := gauge("node_filesystem_files_free")
		guard := " and on" + group + " (" + deviceError + "==0)" +
			" and on" + group + " (" + total + ">0)" +
			" and on" + group + " (" + total + "<=9007199254740991)" +
			" and on" + group + " (" + total + "==floor(" + total + "))" +
			" and on" + group + " (" + free + ">=0)" +
			" and on" + group + " (" + free + "<= on" + group + " " + total + ")" +
			" and on" + group + " (" + free + "==floor(" + free + "))" +
			" and on" + group + " (" + ts + "== on" + group + " " + fs + ")" +
			" and on" + group + " (" + ts + "== on" + group + " " + es + ")"
		used := "(" + total + " - on" + group + " " + free + ")"
		switch key {
		case "node.filesystem.inodes_total":
			value = total
		case "node.filesystem.inodes_free":
			value = free
		case "node.filesystem.inodes_used":
			value = used
		case "node.filesystem.inodes_used_percent":
			value = "(100 * (" + used + " / on" + group + " " + total + "))"
		}
		return "(" + value + guard + ")", "(" + ts + guard + ")", presence
	}
	total, ts := gauge("node_filesystem_size_bytes")
	free, fs := gauge("node_filesystem_free_bytes")
	available, as := gauge("node_filesystem_avail_bytes")
	guard := " and on" + group + " (" + deviceError + "==0)" +
		" and on" + group + " (" + total + ">=0)" + " and on" + group + " (" + free + ">=0)" +
		" and on" + group + " (" + available + ">=0)" + " and on" + group + " (" + free + " <= on" + group + " " + total + ")" +
		" and on" + group + " (" + total + "<+Inf)" + " and on" + group + " (" + free + "<+Inf)" + " and on" + group + " (" + available + "<+Inf)" +
		" and on" + group + " (" + available + " <= on" + group + " " + free + ")" +
		" and on" + group + " (" + ts + " == on" + group + " " + fs + ")" +
		" and on" + group + " (" + ts + " == on" + group + " " + as + ")" +
		" and on" + group + " (" + ts + " == on" + group + " " + es + ")"
	used := "(" + total + " - on" + group + " " + free + ")"
	switch key {
	case "node.filesystem.total_bytes":
		value = total
	case "node.filesystem.free_bytes":
		value = free
	case "node.filesystem.available_bytes":
		value = available
	case "node.filesystem.used_bytes":
		value = used
	case "node.filesystem.used_percent":
		denominator := "(" + used + " + on" + group + " " + available + ")"
		value = "(100 * (" + used + " / on" + group + " " + denominator + "))"
		guard += " and on" + group + " (" + denominator + ">0)"
	}
	return "(" + value + guard + ")", "(" + ts + guard + ")", presence
}
