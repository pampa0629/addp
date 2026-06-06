package api

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestSpatialPreviewContractAcceptsKnownCRSWithoutSRID(t *testing.T) {
	t.Parallel()

	definition := &datatype.CRSDefinition{
		ID:                 "ADDP:CRS:test",
		DefinitionEncoding: datatype.CRSDefinitionEncodingESRIWKT,
		Definition:         `PROJCS["CGCS2000_3_Degree_GK_CM_120E"]`,
		Source:             datatype.CRSDefinitionSourceSidecarPRJ,
	}

	contract := spatialPreviewContract("geometry", 0, definition.ID, definition)

	if contract["source_srid"] != 0 || contract["source_crs"] != definition.ID {
		t.Fatalf("source CRS contract = %#v, want srid=0 with custom CRS", contract)
	}
	if contract["source_crs_definition"] != definition {
		t.Fatalf("source_crs_definition = %#v, want original definition", contract["source_crs_definition"])
	}
	if contract["transform_status"] != "not_transformed" || contract["preview_hint"] != "frontend_transform_required" {
		t.Fatalf("transform contract = %#v, want not_transformed/frontend_transform_required", contract)
	}
	if _, ok := contract["target_srid"]; ok {
		t.Fatalf("ordinary preview contract must not emit target_srid: %#v", contract)
	}
	if _, ok := contract["transform_message"]; ok {
		t.Fatalf("transform_message must be absent for known CRS: %#v", contract)
	}
}

func TestSpatialPreviewContractReportsUnknownWhenNoCRSFact(t *testing.T) {
	t.Parallel()

	contract := spatialPreviewContract("geometry", 0, "", nil)

	if _, ok := contract["source_crs"]; ok {
		t.Fatalf("source_crs must be absent for unknown CRS: %#v", contract)
	}
	if contract["transform_status"] != "unknown_crs" || contract["preview_hint"] != "unknown_crs" {
		t.Fatalf("transform contract = %#v, want unknown_crs", contract)
	}
	if _, ok := contract["transform_message"]; ok {
		t.Fatalf("ordinary preview contract must not emit localized transform_message: %#v", contract)
	}
}
