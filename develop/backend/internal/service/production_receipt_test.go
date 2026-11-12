package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/production"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
)

type productionReaderFixture struct {
	value *repository.PrivateFileProduction
	err   error
}

func (f productionReaderFixture) ReadPrivateProduction(context.Context, uint, string, string) (*repository.PrivateFileProduction, error) {
	return f.value, f.err
}

func TestDevelopProductionReceiptUsesNecessaryActualReads(t *testing.T) {
	facts, _, _, _ := fileInputsFixture(t, true)
	ref := production.Reference{Owner: "develop", ExecutionID: facts.ExecutionID, ResultID: uuid.NewString()}
	value := repository.PrivateFileProduction{Facts: *facts, OperationID: ref.ResultID, Attempt: 1, CommittedAt: time.Now().UTC()}
	for _, source := range facts.Intent.Sources {
		set, _ := plugin.NewQueryReadSet(source.Target)
		value.SourceReads = append(value.SourceReads, repository.PrivateFileSourceRead{EngineVersion: 2, NodeID: source.NodeID, ReadSet: set})
	}
	reader := NewProductionReceiptService(productionReaderFixture{value: &value})
	result, err := reader.ReadCommittedProduction(t.Context(), 3, ref)
	if err != nil || result.Validate() != nil || result.Sources.Kind != production.SourcesDataItems || len(result.Sources.EngineVersions) != 1 || result.Sources.EngineVersions[0].Version != 2 || len(result.Sources.ReadSet.Paths) != 1 || !reflect.DeepEqual(result.Sources.ReadSet.Paths[0], facts.Intent.Sources[0].Target) || result.OperationID != ref.ResultID || !result.CommittedAt.Equal(value.CommittedAt) {
		t.Fatal("unused source affected the independent result", result, err)
	}
	raw, _ := json.Marshal(result)
	for _, secret := range []string{"workflow_definition", "config_json", "manifest", "provider_state", "scope_digest", "lease_token", "connection_info", "authorization_id", "allowed", "sources/B.gpkg"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private or unrelated data escaped receipt", secret)
		}
	}
	value.SourceReads[0].EngineVersion = 0
	unversioned, err := reader.ReadCommittedProduction(t.Context(), 3, ref)
	if err != nil || unversioned.Sources.Kind != production.SourcesUnknown {
		t.Fatal("historical source version was inferred from the current bound scope", unversioned, err)
	}
	// Missing actual evidence remains unknown, even though the config names a
	// source. No declaration or lineage is substituted for a real read session.
	value.SourceReads = nil
	unknown, err := reader.ReadCommittedProduction(t.Context(), 3, ref)
	if err != nil || unknown.Sources.Kind != production.SourcesUnknown || unknown.Sources.ReadSet != nil {
		t.Fatal("missing read evidence was invented", unknown, err)
	}
	set, _ := plugin.NewQueryReadSet(facts.Intent.Sources[1].Target)
	value.SourceReads = []repository.PrivateFileSourceRead{{EngineVersion: 2, NodeID: "a", ReadSet: set}, {EngineVersion: 2, NodeID: "b", ReadSet: set}}
	if _, err := reader.ReadCommittedProduction(t.Context(), 3, ref); err == nil {
		t.Fatal("mismatched actual source was accepted")
	}
	value.SourceReads = nil
	for _, tc := range []struct {
		tenant uint
		ref    production.Reference
	}{{7, ref}, {3, production.Reference{Owner: "transfer", ExecutionID: ref.ExecutionID, ResultID: ref.ResultID}}, {3, production.Reference{Owner: "develop", ExecutionID: ref.ExecutionID, ResultID: uuid.NewString()}}} {
		if _, err := reader.ReadCommittedProduction(t.Context(), tc.tenant, tc.ref); err == nil {
			t.Fatal("mismatched identity accepted")
		}
	}
	if _, err := NewProductionReceiptService(productionReaderFixture{err: errors.New("unavailable")}).ReadCommittedProduction(t.Context(), 3, ref); err == nil {
		t.Fatal("missing original evidence accepted")
	}
}

func TestWorkflowDataSourcesSeparatesDataAndOrdering(t *testing.T) {
	tasks := map[string]map[string]interface{}{
		"a": {"params": map[string]interface{}{}}, "b": {"params": map[string]interface{}{}},
		"merge":     {"params": map[string]interface{}{"inputs": []interface{}{map[string]interface{}{"$ref": "b", "port": "default"}, map[string]interface{}{"nested": map[string]interface{}{"$ref": "a"}}, map[string]interface{}{"$ref": "a"}}}},
		"save":      {"params": map[string]interface{}{"input": map[string]interface{}{"$ref": "merge"}}, "depends_on": []interface{}{"ordering"}},
		"generated": {"params": map[string]interface{}{}, "depends_on": []interface{}{"a"}},
	}
	sources, ok := workflowDataSources(tasks, map[string]bool{"a": true, "b": true}, []string{"save", "generated"})
	if !ok || !reflect.DeepEqual(sources["save"], []string{"a", "b"}) || len(sources["generated"]) != 0 {
		t.Fatal("data dependency traversal changed", sources, ok)
	}
	tasks["a"]["params"] = map[string]interface{}{"input": map[string]interface{}{"$ref": "save"}}
	if _, ok := workflowDataSources(tasks, map[string]bool{"a": true}, []string{"save"}); ok {
		t.Fatal("cyclic data references accepted")
	}
}
