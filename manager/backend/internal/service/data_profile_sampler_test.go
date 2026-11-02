package service

import (
	"context"
	"errors"
	"testing"

	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/preview"
)

func TestProfilePreparationRejectsUnsupportedSourceBeforePreparingPages(t *testing.T) {
	for _, tc := range []struct {
		engine    string
		selection DataProfileSelection
	}{
		{"mongodb", DataProfileSelection{}},
		{"mysql", DataProfileSelection{}},
		{"minio", DataProfileSelection{}},
		{"postgresql", DataProfileSelection{ChildName: "Sheet1"}},
		{"postgresql", DataProfileSelection{RefPath: "data.csv"}},
		{"postgresql", DataProfileSelection{NestedChildPath: "nested/table"}},
	} {
		t.Run(tc.engine+tc.selection.ChildName+tc.selection.RefPath+tc.selection.NestedChildPath, func(t *testing.T) {
			locator, err := resourcetree.ParseURI("addp://engine/2/path/public/fixture?type=table")
			if err != nil {
				t.Fatal(err)
			}
			// No initialized Provider or connection: rejection must precede page preparation.
			sampler := NewPreviewDataProfileSampleProvider(&preview.PreviewResolver{}, nil)
			target := &DataProfileTarget{EngineID: 2, Locator: locator.ToURI(), Selection: tc.selection,
				resolved: &preview.PreviewResolverRequest{Engine: &commonModels.Engine{ID: 2, EngineType: tc.engine}, Locator: locator},
			}
			plan, err := sampler.Prepare(t.Context(), target, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, DefaultDataProfileBudget)
			if plan != nil || !errors.Is(err, ErrDataProfileUnsupported) {
				t.Fatalf("unsupported source entered preparation: %v", err)
			}
		})
	}
}

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
