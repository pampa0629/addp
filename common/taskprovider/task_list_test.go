package taskprovider

import (
	"strings"
	"testing"
)

func TestParseTaskListResponseAcceptsStandardShape(t *testing.T) {
	result, err := ParseTaskListResponse(strings.NewReader(`{"items":[{"id":1}],"total":1,"page":1,"page_size":20}`))
	if err != nil {
		t.Fatalf("ParseTaskListResponse() error = %v, want nil", err)
	}
	if result.Total != 1 || result.Page != 1 || result.PageSize != 20 {
		t.Fatalf("result = %#v, want total/page/page_size", result)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items len = %d, want 1", len(result.Items))
	}
}

func TestParseTaskListResponseRejectsLegacyDataShape(t *testing.T) {
	_, err := ParseTaskListResponse(strings.NewReader(`{"data":[]}`))
	if err == nil {
		t.Fatal("ParseTaskListResponse() error = nil, want missing items")
	}
	if !strings.Contains(err.Error(), "data") {
		t.Fatalf("error = %q, want data", err.Error())
	}
}

func TestParseTaskListResponseRejectsExtraField(t *testing.T) {
	_, err := ParseTaskListResponse(strings.NewReader(`{"items":[],"total":0,"page":1,"page_size":20,"total_pages":0}`))
	if err == nil {
		t.Fatal("ParseTaskListResponse() error = nil, want extra field rejected")
	}
	if !strings.Contains(err.Error(), "total_pages") {
		t.Fatalf("error = %q, want total_pages", err.Error())
	}
}

func TestParseTaskListResponseRejectsNonIntegerPagination(t *testing.T) {
	_, err := ParseTaskListResponse(strings.NewReader(`{"items":[],"total":0.5,"page":1,"page_size":20}`))
	if err == nil {
		t.Fatal("ParseTaskListResponse() error = nil, want non-integer total rejected")
	}
	if !strings.Contains(err.Error(), "total must be an integer") {
		t.Fatalf("error = %q, want total integer", err.Error())
	}
}
