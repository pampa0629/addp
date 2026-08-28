package api

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

func parseOptionalAsOf(c *gin.Context) (time.Time, error) {
	values := c.Request.URL.Query()["as_of"]
	if len(values) > 1 {
		return time.Time{}, fmt.Errorf("duplicate as_of query parameter")
	}
	if len(values) == 0 || values[0] == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, values[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("as_of must be RFC3339")
	}
	return parsed.UTC(), nil
}

func parseOptionalRevisionStatus(c *gin.Context) (string, error) {
	values := c.Request.URL.Query()["status"]
	if len(values) > 1 {
		return "", fmt.Errorf("duplicate status query parameter")
	}
	if len(values) == 0 || values[0] == "" {
		return "", nil
	}
	switch values[0] {
	case "draft", "in_review", "published", "withdrawn":
		return values[0], nil
	default:
		return "", fmt.Errorf("invalid revision status")
	}
}
