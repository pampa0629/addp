package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/preview"
)

func TestProfileSamplerCannotReuseUncheckedPreview(t *testing.T) {
	// Empty resolver would panic if the sampler attempted the old preview read.
	sampler := NewPreviewDataProfileSampleProvider(&preview.PreviewResolver{}, nil)
	result, err := sampler.Sample(context.Background(), &DataProfileTarget{resolved: &preview.PreviewResolverRequest{}}, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, DefaultDataProfileBudget, nil, nil)
	if result != nil || !errors.Is(err, ErrDataProfileSourceAuthorizationRequired) {
		t.Fatalf("unchecked profiling read was allowed: %#v %v", result, err)
	}
}

func TestProfileTargetKeyNormalizesSelection(t *testing.T) {
	actor := dataProfileActor{TenantID: 7, PrincipalID: 9, TenantMembershipID: 12, AuthorizationVersion: 3}
	left := profileTargetKey(actor, " addp://engine/1/item/a ", DataProfileSelection{
		ChildName: " Sheet 1 ", RefPath: "/data.csv/", NestedChildPath: "/nested/table/",
	}, "config")
	right := profileTargetKey(actor, "addp://engine/1/item/a", DataProfileSelection{
		ChildName: "Sheet 1", RefPath: "data.csv", NestedChildPath: "nested/table",
	}, "config")
	if left != right {
		t.Fatalf("normalized target keys differ: %q != %q", left, right)
	}
}
