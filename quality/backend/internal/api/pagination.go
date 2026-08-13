package api

import (
	"fmt"
	"strconv"
	"strings"
)

func pageParams(valuePage, valuePageSize string) (int, int) {
	page, _ := strconv.Atoi(valuePage)
	pageSize, _ := strconv.Atoi(valuePageSize)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func totalPages(total int64, pageSize int) int {
	pages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if pages < 1 {
		return 1
	}
	return pages
}

func optionalPositiveID(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid positive id")
	}
	return id, nil
}

func requiredPositiveID(value string) (int64, error) {
	id, err := optionalPositiveID(value)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid positive id")
	}
	return id, nil
}
