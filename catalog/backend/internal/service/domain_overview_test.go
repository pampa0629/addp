package service

import (
	"testing"

	commonClient "github.com/addp/common/client"
)

func TestProjectDomainOverviewsExcludesOwnerPrivateFields(t *testing.T) {
	parent := int64(7)
	result := projectDomainOverviews([]*commonClient.StandardDomainTree{{
		ID: 7, Name: "Outdoor", Code: "outdoor", Description: "Outdoor data",
		Children: []*commonClient.StandardDomainTree{{ID: 8, Name: "Activity", Code: "activity", ParentID: &parent}},
	}})
	if len(result) != 1 || result[0].ID != "7" || result[0].Description != "Outdoor data" || len(result[0].Children) != 1 ||
		result[0].Children[0].ParentID == nil || *result[0].Children[0].ParentID != "7" {
		t.Fatalf("projected domains = %#v", result)
	}
}
