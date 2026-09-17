package service

import (
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"testing"
)

func TestStandardInitialCreationAndDraftDocumentLinks(t *testing.T) {
	for _, summary := range []string{"初始创建", "Initial creation"} {
		t.Run(summary, func(t *testing.T) {
			db := setupStandardCleanupTestDB(t)
			refs := repository.NewTenantReferenceRepository(db)
			element, err := NewElementService(repository.NewElementRepository(db), nil, refs, nil).CreateElement(&models.CreateElementRequest{Code: "customer_id", ScopeType: models.StandardScopeTenantCommon, Name: "Customer ID", Definition: "Identifier", DataType: "string", ValueDomainKind: models.ValueDomainUnrestricted}, 7, 1, summary)
			if err != nil {
				t.Fatal(err)
			}
			glossary, err := NewGlossaryService(repository.NewGlossaryRepository(db), refs).CreateGlossary(&models.CreateGlossaryRequest{Code: "customer", ScopeType: models.StandardScopeTenantCommon, Name: "Customer", Definition: "Customer"}, 7, 1, summary)
			if err != nil {
				t.Fatal(err)
			}
			codeSet, err := NewCodeSetService(repository.NewCodeSetRepository(db), refs).CreateCodeSet(7, 1, &models.CreateCodeSetRequest{Code: "status", ScopeType: models.StandardScopeTenantCommon, Name: "Status", Description: "Status", ValueType: "string"}, summary)
			if err != nil {
				t.Fatal(err)
			}
			metric, err := NewMetricService(nil, repository.NewMetricRepository(db), refs, nil).CreateMetric(&models.CreateMetricRequest{Code: "customer_count", ScopeType: models.StandardScopeTenantCommon, Name: "Customer count", Definition: "Count", StatisticalCaliber: "Count customers", MetricType: "atomic"}, 7, 1, summary)
			if err != nil {
				t.Fatal(err)
			}
			svc := &DocumentService{repo: repository.NewDocumentRepository(db), refs: refs}
			doc, err := svc.CreateDocument(&models.CreateDocumentRequest{Code: "reference", ScopeType: models.StandardScopeTenantCommon, Name: "Reference", DocType: "reference"}, 7, 1, summary)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{element.DraftRevision.ChangeSummary, glossary.DraftRevision.ChangeSummary, codeSet.DraftRevision.ChangeSummary, metric.DraftRevision.ChangeSummary, doc.DraftRevision.ChangeSummary} {
				if value != summary {
					t.Fatalf("summary = %q, want %q", value, summary)
				}
			}
			// Reading and maintaining provenance is valid before an element has been published.
			if docs, err := svc.ListByElement(7, element.ID); err != nil || len(docs) != 0 {
				t.Fatalf("empty draft documents: %v %v", docs, err)
			}
			if _, err := svc.ListByElement(8, element.ID); err != repository.ErrInvalidTenantReference {
				t.Fatalf("foreign draft: %v", err)
			}
			parents := []struct {
				name   string
				id     int64
				create func(*models.CreateLinkedDocumentRequest, int64, int64, int64, string) (*models.LinkedDocumentMutationResponse, error)
			}{
				{"element", element.ID, svc.CreateAndLinkElement}, {"glossary", glossary.ID, svc.CreateAndLinkGlossary}, {"metric", metric.ID, svc.CreateAndLinkMetric},
			}
			for _, parent := range parents {
				linked, err := parent.create(&models.CreateLinkedDocumentRequest{Version: 1, CreateDocumentRequest: models.CreateDocumentRequest{Code: parent.name + "_reference", ScopeType: models.StandardScopeTenantCommon, Name: "Reference", DocType: "reference"}}, 7, 1, parent.id, summary)
				if err != nil {
					t.Fatalf("%s: %v", parent.name, err)
				}
				if linked.Document.DraftRevision.ChangeSummary != summary {
					t.Fatalf("linked %s summary = %q", parent.name, linked.Document.DraftRevision.ChangeSummary)
				}
			}
			if docs, err := svc.ListByElement(7, element.ID); err != nil || len(docs) != 1 {
				t.Fatalf("linked draft documents: %v %v", docs, err)
			}
		})
	}
}
