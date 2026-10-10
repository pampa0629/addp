package models

import (
	"errors"
	"strconv"
	"strings"
)

var ErrInvalidRuntimeInstanceIDs = errors.New("invalid runtime instance IDs")

// ParseRuntimeInstanceIDs is the shared bounded exact-record query contract.
// These are System record IDs, not process identity strings or inferred hosts.
func ParseRuntimeInstanceIDs(raw string) ([]uint, error) {
	if raw == "" || len(raw) > 2000 {
		return nil, ErrInvalidRuntimeInstanceIDs
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 100 {
		return nil, ErrInvalidRuntimeInstanceIDs
	}
	ids := make([]uint, 0, len(parts))
	seen := map[uint]bool{}
	for _, part := range parts {
		v, err := strconv.ParseUint(part, 10, 63)
		if err != nil || v == 0 || uint64(uint(v)) != v || strconv.FormatUint(v, 10) != part || seen[uint(v)] {
			return nil, ErrInvalidRuntimeInstanceIDs
		}
		seen[uint(v)] = true
		ids = append(ids, uint(v))
	}
	return ids, nil
}
