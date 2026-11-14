// Package upgrade converts mutable legacy references once during owner startup.
// Runtime consumers use objectstore.Parse exclusively.
package upgrade

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/addp/common/infra/objectstore"
)

func Reference(value string) (string, error) {
	if !strings.HasPrefix(value, "addp-infra://minio/") {
		return value, nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(u.EscapedPath(), "/manager/") {
		return "", fmt.Errorf("legacy reference has unknown logical area")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 1 || len(q["type"]) != 1 {
		return "", fmt.Errorf("invalid legacy reference type")
	}
	kind := q.Get("type")
	if kind == "directory" {
		kind = "prefix"
	}
	if kind == "prefix" {
		u.Path = strings.TrimSuffix(u.Path, "/")
		u.RawPath = strings.TrimSuffix(u.RawPath, "/")
	}
	u.Host = "object"
	u.RawQuery = "type=" + kind
	ref, err := objectstore.Parse(u.String())
	if err != nil {
		return "", err
	}
	return ref.URI()
}
