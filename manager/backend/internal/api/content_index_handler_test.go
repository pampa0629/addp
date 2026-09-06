package api

import (
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
)

type contentIndexProtectionGate struct {
	result projectionstore.GateResult
	calls  int
	target dataprotection.ResourceReference
}

func (g *contentIndexProtectionGate) Gate(_ int64, target dataprotection.ResourceReference, _ time.Time) projectionstore.GateResult {
	g.calls++
	g.target = target
	return g.result
}

func TestContentIndexProtectionLeavesTechnicalMetadataOutsideDataAction(t *testing.T) {
	gate := &contentIndexProtectionGate{result: projectionstore.GateResult{Managed: true}}
	document := commonClient.ManagerContentDocument{
		DocumentID: "fingerprint-1", PayloadKind: commonClient.ManagerContentPayloadTechnicalMetadata,
		EngineID: 9, DataItemType: "table", Name: "persons",
	}
	if err := applyContentIndexProtection(gate, 7, &document); err != nil {
		t.Fatal(err)
	}
	if gate.calls != 0 {
		t.Fatalf("technical metadata performed %d protection lookups", gate.calls)
	}
}

func TestContentIndexProtectionUsesOneLocalMissForUnmanagedContent(t *testing.T) {
	gate := &contentIndexProtectionGate{}
	document := commonClient.ManagerContentDocument{
		DocumentID: "fingerprint-1", PayloadKind: commonClient.ManagerContentPayloadExtractedContent,
		EngineID: 9, DataItemType: "document", Name: "contacts.txt", Content: "13661384499",
	}
	if err := applyContentIndexProtection(gate, 7, &document); err != nil {
		t.Fatal(err)
	}
	if gate.calls != 1 || gate.target.ResourceIdentity != "fingerprint-1" {
		t.Fatalf("gate calls=%d target=%#v", gate.calls, gate.target)
	}
}

func TestContentIndexProtectionFailsClosedForManagedContentWithoutSearchRule(t *testing.T) {
	gate := &contentIndexProtectionGate{result: projectionstore.GateResult{Managed: true, State: dataprotection.ProjectionStateActive}}
	document := commonClient.ManagerContentDocument{
		DocumentID: "fingerprint-1", PayloadKind: commonClient.ManagerContentPayloadExtractedContent,
		EngineID: 9, DataItemType: "document", Name: "contacts.txt", Content: "13661384499",
	}
	if err := applyContentIndexProtection(gate, 7, &document); err == nil {
		t.Fatal("managed extracted content was accepted without a search_index rule")
	}
}

func TestContentIndexProtectionMasksManagedDocumentBeforeIndexWrite(t *testing.T) {
	hash, err := dataprotection.DocumentTextSnapshotHash("正文 13661384499", false)
	if err != nil {
		t.Fatal(err)
	}
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2, ProjectionID: "projection-1", Revision: "00000000000000000001",
		ConsumerOwner: "manager", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "fingerprint-1"},
		SourceSnapshotHash: hash,
		Rules: []dataprotection.Rule{{
			Action: "search_index", Component: dataprotection.DocumentTextComponent(),
			Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmPhoneOccurrencesV1, InvalidValueEffect: dataprotection.EffectSuppress, Parameters: map[string]any{
				"prefix_runes": 3, "suffix_runes": 4, "replacement": "****", "exact_runes": 11, "character_class": "ascii_digit",
			}},
		}},
		ValidFrom: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	gate := &contentIndexProtectionGate{result: projectionstore.GateResult{
		Managed: true, State: dataprotection.ProjectionStateActive, Projections: []dataprotection.Projection{projection},
	}}
	document := commonClient.ManagerContentDocument{
		DocumentID: "fingerprint-1", PayloadKind: commonClient.ManagerContentPayloadExtractedContent,
		EngineID: 9, DataItemType: "document", Name: "contacts.txt",
		Content: "正文 13661384499", ContentPreview: "摘要 13661384499",
		Title: "联系人13661384499", Metadata: map[string]interface{}{"owner": "13661384499"},
	}
	if err := applyContentIndexProtection(gate, 7, &document); err != nil {
		t.Fatal(err)
	}
	if document.Content != "正文 136****4499" || document.ContentPreview != "摘要 136****4499" || document.Title != "联系人136****4499" || document.Metadata["owner"] != "136****4499" {
		t.Fatalf("protected document = %#v", document)
	}
}
