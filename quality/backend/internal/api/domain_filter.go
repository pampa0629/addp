package api

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ownerDomainFilter(c *gin.Context) (*int64, error) {
	values, present := c.Request.URL.Query()["owner_domain_id"]
	if !present {
		return nil, nil
	}
	if len(values) != 1 || values[0] == "" {
		return nil, fmt.Errorf("invalid owner_domain_id")
	}
	value, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || value < 0 {
		return nil, fmt.Errorf("invalid owner_domain_id")
	}
	return &value, nil
}
