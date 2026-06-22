package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
)

// BindOptionalJSONStrict 解码可选 JSON body，并拒绝未知字段。
func BindOptionalJSONStrict(c *gin.Context, dst interface{}) error {
	if c.Request == nil || c.Request.Body == nil {
		return nil
	}

	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid JSON body: multiple JSON values")
	}
	return nil
}

func IsUnknownJSONFieldError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "json: unknown field")
}
