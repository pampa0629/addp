package pq

import (
	"encoding/hex"

	"github.com/emmansun/gmsm/sm3"
)

// UpstreamRevision pins the MIT-licensed source of the SM3 authentication extension.
// Source: https://github.com/iwanlebron/pq/tree/6ff687fe05e4ef24b5d3abcb9e92d5bab94a958b
// Driver body: github.com/lib/pq v1.10.9, the version already used by ADDP.
// Local changes: register highgo (never postgres), add authentication response 13
// from the pinned source, reuse lib/pq public error types, propagate Context
// through startup, and use the existing maintained SM3 implementation.
// LICENSE.md contains the original MIT notices.
const UpstreamRevision = "6ff687fe05e4ef24b5d3abcb9e92d5bab94a958b"

func Sm3ToString(value string) string {
	digest := sm3.Sum([]byte(value))
	return hex.EncodeToString(digest[:])
}
