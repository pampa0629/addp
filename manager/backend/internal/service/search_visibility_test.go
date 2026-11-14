package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/mongodb"
)

type searchVisibilityFixture struct {
	classify func(plugin.EngineCatalogPath) (visible, readable bool)
}

func (f searchVisibilityFixture) CheckManagerSearchRead(_ context.Context, credential string, request authorization.SourceSearchCheckRequest) (*authorization.SourceSearchCheck, error) {
	if credential != "addp_at_test" {
		return nil, fmt.Errorf("unexpected search credential")
	}
	result := &authorization.SourceSearchCheck{ObservedAt: time.Now(), ObservationTag: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	for _, path := range request.Targets {
		visible, readable := true, true
		if f.classify != nil {
			visible, readable = f.classify(path)
		}
		fp := ""
		if visible {
			fp, _ = authorizationfacts.PrimaryCandidateFingerprint(path)
		}
		result.Targets = append(result.Targets, authorization.SourceVisibilityTargetCheck{
			SourceMetadataTargetCheck: authorization.SourceMetadataTargetCheck{Target: path, Visible: visible}, ContentReadable: readable, ItemFingerprint: fp,
		})
	}
	return result, nil
}

func testSearchIdentity(t *testing.T, tenant, engine, item uint, name string) indexedSearchIdentity {
	t.Helper()
	path := plugin.EngineCatalogBranchLeafPath(plugin.DynamicSchemaCatalogModel(), engine, "database", "Outdoor", "collection", "collection", name)
	id, err := authorizationfacts.PrimaryCandidateFingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	return indexedSearchIdentity{ID: id, TenantID: tenant, EngineID: engine, EngineType: "mongodb", Locator: fmt.Sprintf("addp://engine/%d/path/Outdoor/%s?type=collection&item_id=%d", engine, name, item)}
}

func TestSearchIdentityRequiresMatchingFingerprintAndLocator(t *testing.T) {
	doc := testSearchIdentity(t, 7, 11, 82, "Photos")
	if _, ok := doc.catalogPath(); !ok {
		t.Fatal("valid MongoDB identity rejected")
	}
	doc.ID = testSearchIdentity(t, 7, 11, 83, "Persons").ID
	if _, ok := doc.catalogPath(); ok {
		t.Fatal("mismatched locator authorized an index identity")
	}
}
