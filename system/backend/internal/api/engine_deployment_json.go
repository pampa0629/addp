package api

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	"github.com/gin-gonic/gin"
)

// The export declaration has a fixed, bounded nested schema. Token validation
// rejects duplicate keys and case aliases before Go's case-insensitive struct
// decoder can discard that evidence. DisallowUnknownFields then checks ownership
// of every field at its actual nesting level.
func bindEngineDeployment(c *gin.Context, dst interface{}) error {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || len(raw) == 0 || !utf8.Valid(raw) {
		return commonapi.ErrBadRequest
	}
	allowed := map[string]bool{}
	for _, key := range []string{"display_name", "enabled", "inventory", "declared_node_id", "version", "schema", "observed_at", "source", "services", "type", "runtime", "docker_engine_id", "project", "service", "engine_type", "containers", "container_id", "started_at", "restart_count", "published_ports", "address", "port", "container_port", "protocol"} {
		allowed[key] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 12 {
			return commonapi.ErrBadRequest
		}
		token, err := decoder.Token()
		if err != nil {
			return commonapi.ErrBadRequest
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return commonapi.ErrBadRequest
				}
				key, ok := token.(string)
				if !ok || !allowed[key] || seen[key] {
					return commonapi.ErrBadRequest
				}
				seen[key] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
				return commonapi.ErrBadRequest
			}
		case json.Delim('['):
			for decoder.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
				return commonapi.ErrBadRequest
			}
		default:
			if _, delimiter := token.(json.Delim); delimiter {
				return commonapi.ErrBadRequest
			}
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return commonapi.ErrBadRequest
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return commonapi.ErrBadRequest
	}
	return nil
}
