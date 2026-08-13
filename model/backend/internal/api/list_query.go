package api

import (
	"net/url"
	"strconv"
)

const (
	defaultListPage     = 1
	defaultListPageSize = 20
	maxListPageSize     = 100
)

type modelListQuery struct {
	DomainID *int64
	Page     int
	PageSize int
}

func parseModelListQuery(values url.Values) (modelListQuery, error) {
	query := modelListQuery{Page: defaultListPage, PageSize: defaultListPageSize}

	if raw, exists := values["domain_id"]; exists {
		value, err := parseSinglePositiveInt64(raw)
		if err != nil {
			return modelListQuery{}, err
		}
		query.DomainID = &value
	}
	if raw, exists := values["page"]; exists {
		value, err := parseSinglePositiveInt(raw, 0)
		if err != nil {
			return modelListQuery{}, err
		}
		query.Page = value
	}
	if raw, exists := values["page_size"]; exists {
		value, err := parseSinglePositiveInt(raw, maxListPageSize)
		if err != nil {
			return modelListQuery{}, err
		}
		query.PageSize = value
	}

	return query, nil
}

func parseSinglePositiveInt64(raw []string) (int64, error) {
	if len(raw) != 1 || raw[0] == "" {
		return 0, strconv.ErrSyntax
	}
	value, err := strconv.ParseInt(raw[0], 10, 64)
	if err != nil || value <= 0 {
		return 0, strconv.ErrSyntax
	}
	return value, nil
}

func parseSinglePositiveInt(raw []string, max int) (int, error) {
	value, err := parseSinglePositiveInt64(raw)
	if err != nil || value > int64(^uint(0)>>1) || max > 0 && value > int64(max) {
		return 0, strconv.ErrSyntax
	}
	return int(value), nil
}

func parseOptionalEnum(raw []string, allowed ...string) (string, error) {
	value, err := parseOptionalString(raw)
	if err != nil || value == "" {
		return value, err
	}
	for _, allowedValue := range allowed {
		if value == allowedValue {
			return value, nil
		}
	}
	return "", strconv.ErrSyntax
}

func parseOptionalString(raw []string) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	if len(raw) != 1 {
		return "", strconv.ErrSyntax
	}
	if raw[0] == "" {
		return "", nil
	}
	return raw[0], nil
}
