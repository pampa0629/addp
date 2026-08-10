package service

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

type notebookCopilotCatalog struct {
	notebookSessionControlPlaneRecorder
	railwayCount int
}

func (c *notebookCopilotCatalog) ListChildren(
	_ context.Context,
	_ uint,
	_ string,
	request commonClient.NotebookCatalogChildrenRequest,
) ([]commonClient.EngineCatalogEntry, error) {
	segments := request.Path.Segments
	path := func(values ...commonClient.EngineCatalogSegment) commonClient.EngineCatalogPath {
		return commonClient.EngineCatalogPath{Version: "catalog.path/v1", EngineID: request.EngineID, Segments: values}
	}
	server := commonClient.EngineCatalogSegment{Term: "server", Kind: "postgresql", Name: ""}
	public := commonClient.EngineCatalogSegment{Term: "schema", Kind: "schema", Name: "public"}
	if len(segments) == 0 {
		return []commonClient.EngineCatalogEntry{{Name: "PostgreSQL", Term: "server", Kind: "postgresql", Role: "branch", Path: path(server)}}, nil
	}
	if len(segments) == 1 {
		return []commonClient.EngineCatalogEntry{{Name: "public", Term: "schema", Kind: "schema", Role: "branch", Path: path(server, public)}}, nil
	}
	if len(segments) == 2 {
		railwayCount := c.railwayCount
		if railwayCount == 0 {
			railwayCount = 1
		}
		entries := make([]commonClient.EngineCatalogEntry, 0, railwayCount+1)
		for index := 0; index < railwayCount; index++ {
			name := "railway"
			if index > 0 {
				name = fmt.Sprintf("railway_%d", index)
			}
			entries = append(entries, commonClient.EngineCatalogEntry{
				Name: name, Term: "table", Kind: "table", Role: "leaf",
				Path: path(server, public, commonClient.EngineCatalogSegment{Term: "table", Kind: "table", Name: name}),
			})
		}
		entries = append(entries,
			commonClient.EngineCatalogEntry{Name: "farmland", Term: "table", Kind: "table", Role: "leaf", Path: path(server, public, commonClient.EngineCatalogSegment{Term: "table", Kind: "table", Name: "farmland"})},
		)
		return entries, nil
	}
	return nil, nil
}

func TestNotebookCopilotCatalogTraversalReturnsOnlyLeaves(t *testing.T) {
	catalog := &notebookCopilotCatalog{}
	sessions := &NotebookSessionService{catalog: catalog}
	session := &NotebookSession{ID: "session-1", TenantID: 9, SessionAuthorizationID: "authorization-1"}
	leaves, err := sessions.listCatalogLeavesForSession(context.Background(), session, 21, 20)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{leaves[0].Name, leaves[1].Name}
	if !reflect.DeepEqual(got, []string{"railway", "farmland"}) {
		t.Fatalf("leaves = %#v", got)
	}
}

func TestNotebookCopilotCoarseMatchUsesLLMEnglishSearchTerms(t *testing.T) {
	entry := commonClient.EngineCatalogEntry{
		Name: "farmland",
		Path: commonClient.EngineCatalogPath{Segments: []commonClient.EngineCatalogSegment{{Name: "public"}, {Name: "farmland"}}},
	}
	descriptor := commonModels.EngineRuntimeDescriptor{Name: "Business PG", EngineType: "postgresql"}
	if !notebookCandidateMatches(entry, descriptor, []string{"耕地", "farmland", "cropland"}) {
		t.Fatal("English candidate should match multilingual terms supplied by the LLM")
	}
	if notebookCandidateMatches(entry, descriptor, []string{"铁路", "railway"}) {
		t.Fatal("unrelated role must not match")
	}
}

func TestNotebookCopilotCoarseMatchIgnoresEmptyPathNames(t *testing.T) {
	entry := commonClient.EngineCatalogEntry{
		Name: "unrelated-file",
		Path: commonClient.EngineCatalogPath{Segments: []commonClient.EngineCatalogSegment{{Name: ""}, {Name: "manager"}}},
	}
	descriptor := commonModels.EngineRuntimeDescriptor{Name: "Business MinIO", EngineType: "s3"}
	if notebookCandidateMatches(entry, descriptor, []string{"铁路", "railway"}) {
		t.Fatal("empty catalog path names must not match every search term")
	}
}

func TestNotebookCopilotCandidateLimitPreservesEveryInputRole(t *testing.T) {
	catalog := &notebookCopilotCatalog{railwayCount: notebookCopilotMaxCandidates}
	service := &NotebookCopilotService{sessions: &NotebookSessionService{catalog: catalog}}
	session := &NotebookSession{ID: "session-1", TenantID: 9, SessionAuthorizationID: "authorization-1"}
	descriptors := []commonModels.EngineRuntimeDescriptor{{ID: 21, Name: "Business PostgreSQL", EngineType: "postgresql"}}
	intents := []notebookCopilotIntent{
		{Role: "铁路线路范围", SearchQueries: []string{"铁路", "railway"}},
		{Role: "耕地空间范围", SearchQueries: []string{"耕地", "farmland"}},
	}
	candidates, missingRoles, err := service.findCandidates(context.Background(), session, descriptors, intents)
	if err != nil {
		t.Fatal(err)
	}
	if len(missingRoles) != 0 {
		t.Fatalf("missing roles = %#v", missingRoles)
	}
	roles := map[string]int{}
	for _, candidate := range candidates {
		roles[candidate.Role]++
	}
	if roles["铁路线路范围"] == 0 || roles["耕地空间范围"] == 0 {
		t.Fatalf("candidate roles = %#v", roles)
	}
}

func TestNotebookCandidateIDBindsEngineAndNativePath(t *testing.T) {
	path := commonClient.EngineCatalogPath{
		Version: "catalog.path/v1", EngineID: 21,
		Segments: []commonClient.EngineCatalogSegment{{Term: "table", Kind: "table", Name: "farmland"}},
	}
	if notebookCandidateID(21, path) == notebookCandidateID(22, path) {
		t.Fatal("candidate id must bind engine id")
	}
	changed := path
	changed.Segments = append([]commonClient.EngineCatalogSegment(nil), path.Segments...)
	changed.Segments[0].Name = "farmland_history"
	if notebookCandidateID(21, path) == notebookCandidateID(21, changed) {
		t.Fatal("candidate id must bind the complete native path")
	}
}

func TestMergeNotebookIntentQueriesOnlyExpandsMissingRoles(t *testing.T) {
	base := []notebookCopilotIntent{
		{Role: "铁路", SearchQueries: []string{"铁路", "railway"}},
		{Role: "耕地", SearchQueries: []string{"耕地", "cultivated land"}},
	}
	expanded := []notebookCopilotIntent{
		{Role: "耕地", SearchQueries: []string{"farmland", "cultivated land"}},
		{Role: "铁路", SearchQueries: []string{"track"}},
	}
	merged := mergeNotebookIntentQueries(base, expanded, map[string]struct{}{"耕地": {}})
	if len(merged) != 2 {
		t.Fatalf("merged intents = %#v", merged)
	}
	if got := merged[1].SearchQueries; !reflect.DeepEqual(got, []string{"耕地", "cultivated land", "farmland"}) {
		t.Fatalf("expanded queries = %#v", got)
	}
	if got := merged[0].SearchQueries; !reflect.DeepEqual(got, []string{"铁路", "railway"}) {
		t.Fatalf("non-missing role queries changed = %#v", got)
	}
}
