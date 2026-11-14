package objectstore

import (
	"fmt"
	"net/url"
	"strings"
)

const Manager = "manager"

type Ref struct {
	Area string
	Key  string
	Type string
}

func Parse(value string) (Ref, error) {
	u, err := url.Parse(value)
	if err != nil {
		return Ref{}, fmt.Errorf("invalid object reference: %w", err)
	}
	if u.Scheme != "addp-infra" || u.Host != "object" || u.User != nil || u.Fragment != "" {
		return Ref{}, fmt.Errorf("invalid platform object reference")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 1 || len(q["type"]) != 1 || (q.Get("type") != "object" && q.Get("type") != "prefix") {
		return Ref{}, fmt.Errorf("invalid platform object reference type")
	}
	parts := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	decoded := make([]string, len(parts))
	for i, s := range parts {
		part, err := url.PathUnescape(s)
		if err != nil || part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/\\\x00") {
			return Ref{}, fmt.Errorf("invalid platform object path segment")
		}
		decoded[i] = part
	}
	if len(decoded) < 2 || decoded[0] != Manager && decoded[0] != Standard {
		return Ref{}, fmt.Errorf("invalid platform object area or key")
	}
	return Ref{Area: decoded[0], Key: strings.Join(decoded[1:], "/"), Type: q.Get("type")}, nil
}

func (r Ref) URI() (string, error) {
	u := url.URL{Scheme: "addp-infra", Host: "object", Path: "/" + r.Area + "/" + r.Key, RawQuery: "type=" + r.Type}
	value := u.String()
	_, err := Parse(value)
	return value, err
}

func ObjectURI(area, key string) string {
	value, err := (Ref{area, key, "object"}).URI()
	if err != nil {
		return ""
	}
	return value
}
func PrefixURI(area, key string) string {
	value, err := (Ref{area, strings.TrimSuffix(key, "/"), "prefix"}).URI()
	if err != nil {
		return ""
	}
	return value
}
