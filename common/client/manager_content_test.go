package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagerContentClientUsesTenantBearerAndCanonicalRoutes(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.Header.Get("Authorization") != "Bearer tenant-7" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch requests {
		case 1:
			if request.Method != http.MethodPost || request.URL.Path != "/api/v1/manager/runtime/content-documents/batch_update" {
				t.Fatalf("upsert request = %s %s", request.Method, request.URL.String())
			}
			var batch ManagerContentBatch
			if err := json.NewDecoder(request.Body).Decode(&batch); err != nil || len(batch.Documents) != 1 || batch.Documents[0].DocumentID != "fingerprint-1" {
				t.Fatalf("batch = %#v error=%v", batch, err)
			}
		case 2:
			if request.Method != http.MethodDelete || request.URL.Path != "/api/v1/manager/runtime/content-documents" ||
				request.URL.Query().Get("engine_id") != "9" || request.URL.Query().Get("schema") != "public" || request.URL.Query().Get("data_item_type") != "table" {
				t.Fatalf("delete request = %s %s", request.Method, request.URL.String())
			}
		default:
			t.Fatalf("unexpected request %d", requests)
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := NewManagerContentClient(server.URL, ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		return "tenant-7", nil
	}), server.Client()).WithTenantID(7)
	if err := client.UpsertDocuments(context.Background(), []ManagerContentDocument{{
		DocumentID: "fingerprint-1", PayloadKind: ManagerContentPayloadTechnicalMetadata,
		EngineID: 9, DataItemType: "table", Name: "orders",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteDocuments(context.Background(), ManagerContentDeleteScope{EngineID: 9, DataItemType: "table", Schema: "public"}); err != nil {
		t.Fatal(err)
	}
}

func TestManagerContentDocumentSeparatesTechnicalMetadataAndExtractedContent(t *testing.T) {
	technical := ManagerContentDocument{
		DocumentID: "fingerprint-1", PayloadKind: ManagerContentPayloadTechnicalMetadata,
		EngineID: 9, DataItemType: "table", Name: "orders",
		Fields: []ManagerContentField{{Name: "phone", DataType: "string"}},
	}
	if err := technical.Validate(); err != nil {
		t.Fatalf("technical metadata validation failed: %v", err)
	}
	technical.ContentPreview = "13661384499"
	if err := technical.Validate(); err == nil {
		t.Fatal("technical metadata accepted extracted content")
	}
	technical.ContentPreview = ""
	technical.ContentTruncated = true
	if err := technical.Validate(); err == nil {
		t.Fatal("technical metadata accepted a content-derived truncation flag")
	}
	extracted := ManagerContentDocument{
		DocumentID: "fingerprint-2", PayloadKind: ManagerContentPayloadExtractedContent,
		EngineID: 9, DataItemType: "document", Name: "contacts.txt", Content: "13661384499",
	}
	if err := extracted.Validate(); err != nil {
		t.Fatalf("extracted content validation failed: %v", err)
	}
}

func TestManagerContentDocumentRejectsCrossSnapshotFields(t *testing.T) {
	tech := ManagerContentDocument{DocumentID: "id", PayloadKind: ManagerContentPayloadTechnicalMetadata, EngineID: 1, DataItemType: "table", Name: "table", ContentHash: "body-hash"}
	if tech.Validate() == nil {
		t.Fatal("technical snapshot accepted body hash")
	}
	body := ManagerContentDocument{DocumentID: "id", PayloadKind: ManagerContentPayloadExtractedContent, EngineID: 1, DataItemType: "table", Name: "context", Description: "technical comment"}
	if body.Validate() == nil {
		t.Fatal("body snapshot accepted technical fields")
	}
}

func TestManagerContentBatchRejectsInvalidMembersBeforeSending(t *testing.T) {
	doc := ManagerContentDocument{DocumentID: "item", PayloadKind: ManagerContentPayloadTechnicalMetadata, EngineID: 9, DataItemType: "table", Name: "persons"}
	for _, documents := range [][]ManagerContentDocument{nil, {doc, doc}, make([]ManagerContentDocument, ManagerContentBatchLimit+1)} {
		if (ManagerContentBatch{Documents: documents}).Validate() == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	invalid := doc
	invalid.DocumentID = "invalid"
	invalid.Content = "private-value"
	if (ManagerContentBatch{Documents: []ManagerContentDocument{doc, invalid}}).Validate() == nil {
		t.Fatal("one invalid snapshot was accepted with a valid member")
	}
	if err := (ManagerContentBatch{Documents: []ManagerContentDocument{doc}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
